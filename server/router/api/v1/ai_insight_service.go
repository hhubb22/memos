package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/usememos/memos/plugin/ai"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	"github.com/usememos/memos/store"
)

const (
	insightPerspectiveRandom             = "random"
	insightPerspectiveCriticalThinking   = "critical_thinking"
	insightPerspectiveSystemsThinking    = "systems_thinking"
	insightPerspectiveEmotionalPattern   = "emotional_pattern"
	insightPerspectiveCreativeAssoc      = "creative_association"
	insightPerspectiveSocraticQuestion   = "socratic_questioning"
	insightDefaultMemoLimit              = 60
	insightHardMemoLimit                 = 200
	insightSingleMemoContentLimit        = 1200
	insightTotalContentBudget            = 60000
	insightHistoryRetention              = 200
	insightHistoryTrimBatchDelete        = 100
	insightListDefaultPageSize           = 20
	insightListMaxPageSize               = 200
	insightGenerationValidationRetryMax  = 1
	insightGenerationMinimumConclusions  = 1
	insightGenerationMinimumDeepQuestion = 1
)

var insightPerspectiveMap = map[string]insightPerspective{
	insightPerspectiveCriticalThinking: {
		Key:   insightPerspectiveCriticalThinking,
		Title: "批判思维",
		Prompt: "聚焦识别逻辑漏洞、未被检验的假设、叙事偏差。保持直接、克制、可验证。" +
			"不要给行动建议，而是指出值得深挖的认知断点。",
	},
	insightPerspectiveSystemsThinking: {
		Key:   insightPerspectiveSystemsThinking,
		Title: "系统思考",
		Prompt: "聚焦变量之间的反馈回路、滞后效应、结构性约束与长期模式。" +
			"帮助用户从事件跳到系统。",
	},
	insightPerspectiveEmotionalPattern: {
		Key:   insightPerspectiveEmotionalPattern,
		Title: "情绪模式",
		Prompt: "聚焦情绪触发、反复出现的心理脚本、内在冲突与需求表达。" +
			"保持尊重且具体，避免临床诊断措辞。",
	},
	insightPerspectiveCreativeAssoc: {
		Key:   insightPerspectiveCreativeAssoc,
		Title: "创意联想",
		Prompt: "聚焦跨主题关联、隐喻映射、远距联想，提炼新问题与新组合方式。" +
			"避免空泛鸡汤。",
	},
	insightPerspectiveSocraticQuestion: {
		Key:   insightPerspectiveSocraticQuestion,
		Title: "苏格拉底提问",
		Prompt: "聚焦用问题推进思考，追问定义、证据、反例、边界条件与替代解释。" +
			"问题应尖锐且具体。",
	},
}

var insightPerspectiveKeys = []string{
	insightPerspectiveCriticalThinking,
	insightPerspectiveSystemsThinking,
	insightPerspectiveEmotionalPattern,
	insightPerspectiveCreativeAssoc,
	insightPerspectiveSocraticQuestion,
}

// AIClient defines the AI completion behavior used by insight generation.
type AIClient interface {
	GenerateCompletion(ctx context.Context, messages []ai.ChatMessage) (string, error)
}

type insightPerspective struct {
	Key    string
	Title  string
	Prompt string
}

type insightPromptMemo struct {
	Name            string
	OriginalContent string
	PromptContent   string
}

type insightModelOutput struct {
	Summary         string                        `json:"summary"`
	CoreConclusions []*insightModelCoreConclusion `json:"core_conclusions"`
	DeepQuestions   []string                      `json:"deep_questions"`
}

type insightModelCoreConclusion struct {
	Conclusion string                `json:"conclusion"`
	Citation   *insightModelCitation `json:"citation"`
}

type insightModelCitation struct {
	Memo   string `json:"memo"`
	Quote  string `json:"quote"`
	Reason string `json:"reason"`
}

func (s *APIV1Service) GenerateInsight(ctx context.Context, request *v1pb.GenerateInsightRequest) (*v1pb.GenerateInsightResponse, error) {
	currentUser, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user")
	}
	if currentUser == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	aiConfig, err := s.Store.GetInstanceAIConfigSetting(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get AI config")
	}
	if !aiConfig.Enabled {
		return nil, status.Errorf(codes.FailedPrecondition, "AI features are not enabled")
	}
	if aiConfig.ApiKey == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "AI API key is not configured")
	}

	sourceType, sourceFilter, sourceMemoNames, memosForPrompt, truncatedHints, err := s.resolveInsightSourceMemos(ctx, currentUser.ID, request)
	if err != nil {
		return nil, err
	}
	if len(memosForPrompt) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "no memos available for insight")
	}

	perspective, err := resolveInsightPerspective(request.Perspective)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid perspective: %v", err)
	}

	clientFactory := s.AIClientFactory
	if clientFactory == nil {
		clientFactory = func(config ai.Config) AIClient {
			return ai.NewClient(config)
		}
	}
	client := clientFactory(ai.Config{
		APIKey:     aiConfig.ApiKey,
		APIBaseURL: aiConfig.ApiBaseUrl,
		Model:      aiConfig.Model,
	})

	modelOutput, err := s.generateStructuredInsightWithValidation(ctx, client, perspective, memosForPrompt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate structured insight: %v", err)
	}

	citations := buildStoreInsightCitations(modelOutput)
	insightMarkdown := renderInsightMarkdown(modelOutput, truncatedHints)

	report, err := s.Store.CreateInsightReport(ctx, &store.InsightReport{
		CreatorID:         currentUser.ID,
		SourceType:        sourceType,
		SourceFilter:      sourceFilter,
		SourceMemoNames:   sourceMemoNames,
		ResolvedMemoNames: collectResolvedMemoNames(memosForPrompt),
		ResolvedMemoCount: int32(len(memosForPrompt)),
		Perspective:       perspective.Key,
		Summary:           modelOutput.Summary,
		Insight:           insightMarkdown,
		Citations:         citations,
		Model:             aiConfig.Model,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to save insight report")
	}

	if err := s.trimInsightReportHistory(ctx, currentUser.ID); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to trim insight report history")
	}

	reportMessage := convertInsightReportFromStore(report)
	return &v1pb.GenerateInsightResponse{
		Insight: reportMessage.GetInsight(),
		Report:  reportMessage,
	}, nil
}

func (s *APIV1Service) ListInsightReports(ctx context.Context, request *v1pb.ListInsightReportsRequest) (*v1pb.ListInsightReportsResponse, error) {
	parentUserID, err := ExtractUserIDFromName(request.Parent)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid parent: %v", err)
	}

	currentUser, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user")
	}
	if currentUser == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if currentUser.ID != parentUserID && !isSuperUser(currentUser) {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}

	var limit int
	var offset int
	if request.PageToken != "" {
		var pageToken v1pb.PageToken
		if err := unmarshalPageToken(request.PageToken, &pageToken); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid page token: %v", err)
		}
		limit = int(pageToken.Limit)
		offset = int(pageToken.Offset)
	} else {
		limit = int(request.PageSize)
	}
	if limit <= 0 {
		limit = insightListDefaultPageSize
	}
	if limit > insightListMaxPageSize {
		limit = insightListMaxPageSize
	}

	limitPlusOne := limit + 1
	reports, err := s.Store.ListInsightReports(ctx, &store.FindInsightReport{
		CreatorID: &parentUserID,
		Limit:     &limitPlusOne,
		Offset:    &offset,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list insight reports")
	}

	nextPageToken := ""
	if len(reports) == limitPlusOne {
		reports = reports[:limit]
		nextPageToken, err = getPageToken(limit, offset+limit)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to build next page token")
		}
	}

	reportMessages := make([]*v1pb.InsightReport, 0, len(reports))
	for _, report := range reports {
		reportMessages = append(reportMessages, convertInsightReportFromStore(report))
	}

	return &v1pb.ListInsightReportsResponse{
		InsightReports: reportMessages,
		NextPageToken:  nextPageToken,
	}, nil
}

func (s *APIV1Service) GetInsightReport(ctx context.Context, request *v1pb.GetInsightReportRequest) (*v1pb.InsightReport, error) {
	parentUserID, reportID, err := ExtractUserAndInsightReportIDFromName(request.Name)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid name: %v", err)
	}

	currentUser, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user")
	}
	if currentUser == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if currentUser.ID != parentUserID && !isSuperUser(currentUser) {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}

	report, err := s.Store.GetInsightReport(ctx, &store.FindInsightReport{
		ID:        &reportID,
		CreatorID: &parentUserID,
		Limit:     intPointer(1),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get insight report")
	}
	if report == nil {
		return nil, status.Errorf(codes.NotFound, "insight report not found")
	}

	return convertInsightReportFromStore(report), nil
}

func (s *APIV1Service) resolveInsightSourceMemos(
	ctx context.Context,
	creatorID int32,
	request *v1pb.GenerateInsightRequest,
) (store.InsightSourceType, string, []string, []*insightPromptMemo, []string, error) {
	trimmedMemoNames := make([]string, 0, len(request.MemoNames))
	for _, memoName := range request.MemoNames {
		if memoName == "" {
			continue
		}
		trimmedMemoNames = append(trimmedMemoNames, memoName)
	}

	sourceFilter := strings.TrimSpace(request.Filter)
	switch {
	case len(trimmedMemoNames) > 0:
		memos, hints, err := s.loadMemosByName(ctx, creatorID, trimmedMemoNames)
		if err != nil {
			return "", "", nil, nil, nil, err
		}
		return store.InsightSourceTypeMemoNames, "", trimmedMemoNames, memos, hints, nil
	case sourceFilter != "":
		memos, hints, effectiveFilter, err := s.loadMemosByFilter(ctx, creatorID, sourceFilter)
		if err != nil {
			return "", "", nil, nil, nil, err
		}
		return store.InsightSourceTypeFilter, effectiveFilter, nil, memos, hints, nil
	default:
		return "", "", nil, nil, nil, status.Errorf(codes.InvalidArgument, "memo_names or filter is required")
	}
}

func (s *APIV1Service) loadMemosByName(ctx context.Context, creatorID int32, memoNames []string) ([]*insightPromptMemo, []string, error) {
	seen := make(map[string]bool, len(memoNames))
	uniqueNames := make([]string, 0, len(memoNames))
	for _, memoName := range memoNames {
		if seen[memoName] {
			continue
		}
		seen[memoName] = true
		uniqueNames = append(uniqueNames, memoName)
	}

	memoRows := make([]*store.Memo, 0, len(uniqueNames))
	for _, memoName := range uniqueNames {
		memoUID, err := ExtractMemoUIDFromName(memoName)
		if err != nil {
			return nil, nil, status.Errorf(codes.InvalidArgument, "invalid memo name: %s", memoName)
		}
		memo, err := s.Store.GetMemo(ctx, &store.FindMemo{
			UID:             &memoUID,
			CreatorID:       &creatorID,
			ExcludeComments: true,
		})
		if err != nil {
			return nil, nil, status.Errorf(codes.Internal, "failed to get memo")
		}
		if memo == nil {
			continue
		}
		memoRows = append(memoRows, memo)
	}

	if len(memoRows) == 0 {
		return nil, nil, status.Errorf(codes.InvalidArgument, "no valid memos found")
	}

	memos, hints := trimMemosForInsightPrompt(memoRows)
	return memos, hints, nil
}

func (s *APIV1Service) loadMemosByFilter(ctx context.Context, creatorID int32, sourceFilter string) ([]*insightPromptMemo, []string, string, error) {
	if err := s.validateFilter(ctx, sourceFilter); err != nil {
		return nil, nil, "", status.Errorf(codes.InvalidArgument, "invalid filter: %v", err)
	}

	creatorGuard := fmt.Sprintf("creator_id == %d", creatorID)
	effectiveFilter := fmt.Sprintf("(%s) && (%s)", sourceFilter, creatorGuard)
	limit := insightHardMemoLimit + 1
	memos, err := s.Store.ListMemos(ctx, &store.FindMemo{
		ExcludeComments: true,
		Filters:         []string{effectiveFilter},
		Limit:           &limit,
	})
	if err != nil {
		return nil, nil, "", status.Errorf(codes.Internal, "failed to list memos by filter")
	}
	if len(memos) == 0 {
		return nil, nil, "", status.Errorf(codes.InvalidArgument, "no memos found for filter")
	}

	promptMemos, hints := trimMemosForInsightPrompt(memos)
	return promptMemos, hints, sourceFilter, nil
}

func resolveInsightPerspective(input string) (insightPerspective, error) {
	key := strings.TrimSpace(strings.ToLower(input))
	if key == "" || key == insightPerspectiveRandom {
		return randomInsightPerspective(), nil
	}
	p, ok := insightPerspectiveMap[key]
	if !ok {
		return insightPerspective{}, errors.Errorf("unsupported perspective %q", input)
	}
	return p, nil
}

func randomInsightPerspective() insightPerspective {
	seed := time.Now().UnixNano()
	random := rand.New(rand.NewSource(seed))
	key := insightPerspectiveKeys[random.Intn(len(insightPerspectiveKeys))]
	return insightPerspectiveMap[key]
}

func trimMemosForInsightPrompt(memos []*store.Memo) ([]*insightPromptMemo, []string) {
	if len(memos) == 0 {
		return nil, nil
	}

	hints := []string{}
	if len(memos) > insightHardMemoLimit {
		hints = append(hints, fmt.Sprintf("已触发硬上限，仅分析前 %d 条笔记。", insightHardMemoLimit))
		memos = memos[:insightHardMemoLimit]
	}
	if len(memos) > insightDefaultMemoLimit {
		hints = append(hints, fmt.Sprintf("已按默认上限截断，仅分析前 %d 条笔记。", insightDefaultMemoLimit))
		memos = memos[:insightDefaultMemoLimit]
	}

	result := make([]*insightPromptMemo, 0, len(memos))
	totalRuneCount := 0
	singleTruncatedCount := 0
	totalBudgetTruncated := false

	for _, memo := range memos {
		original := memo.Content
		if original == "" {
			continue
		}

		promptContent, wasSingleTrimmed := truncateByRune(original, insightSingleMemoContentLimit)
		if wasSingleTrimmed {
			singleTruncatedCount++
		}

		remaining := insightTotalContentBudget - totalRuneCount
		if remaining <= 0 {
			totalBudgetTruncated = true
			break
		}
		promptContent, wasBudgetTrimmed := truncateByRune(promptContent, remaining)
		if wasBudgetTrimmed {
			totalBudgetTruncated = true
		}
		if promptContent == "" {
			continue
		}

		result = append(result, &insightPromptMemo{
			Name:            fmt.Sprintf("%s%s", MemoNamePrefix, memo.UID),
			OriginalContent: original,
			PromptContent:   promptContent,
		})
		totalRuneCount += len([]rune(promptContent))
		if wasBudgetTrimmed {
			break
		}
	}

	if singleTruncatedCount > 0 {
		hints = append(hints, fmt.Sprintf("有 %d 条笔记按单条 %d 字上限截断。", singleTruncatedCount, insightSingleMemoContentLimit))
	}
	if totalBudgetTruncated {
		hints = append(hints, fmt.Sprintf("已触发总字符预算上限（%d 字）。", insightTotalContentBudget))
	}

	return result, hints
}

func truncateByRune(content string, limit int) (string, bool) {
	if limit <= 0 {
		return "", len(content) > 0
	}
	runes := []rune(content)
	if len(runes) <= limit {
		return content, false
	}
	return string(runes[:limit]), true
}

func (s *APIV1Service) generateStructuredInsightWithValidation(
	ctx context.Context,
	client AIClient,
	perspective insightPerspective,
	memos []*insightPromptMemo,
) (*insightModelOutput, error) {
	lastErr := errors.New("unknown error")
	for attempt := 0; attempt <= insightGenerationValidationRetryMax; attempt++ {
		rawOutput, err := client.GenerateCompletion(ctx, buildInsightPromptMessages(perspective, memos, attempt, lastErr))
		if err != nil {
			lastErr = err
			continue
		}

		output, err := parseInsightModelOutput(rawOutput)
		if err != nil {
			lastErr = err
			continue
		}

		if err := validateInsightModelOutput(output, memos); err != nil {
			lastErr = err
			continue
		}

		return output, nil
	}

	return nil, lastErr
}

func buildInsightPromptMessages(
	perspective insightPerspective,
	memos []*insightPromptMemo,
	attempt int,
	lastErr error,
) []ai.ChatMessage {
	systemPrompt := fmt.Sprintf(`你是一位“%s”视角的思考教练。

目标：
1) 识别跨笔记的高价值模式，而非逐条复述；
2) 给出有证据支撑的核心结论；
3) 生成能推动反思的问题。

风格要求：
- %s
- 语言与用户笔记语言保持一致。
- 不给行动清单，不做诊断，不编造来源。

输出约束（必须严格遵守）：
- 只输出一个 JSON 对象，不要 Markdown，不要解释，不要代码块围栏。
- JSON 结构如下（字段名必须一致）：
{
  "summary": "string",
  "core_conclusions": [
    {
      "conclusion": "string",
      "citation": {
        "memo": "memos/{memo}",
        "quote": "string",
        "reason": "string"
      }
    }
  ],
  "deep_questions": ["string"]
}

引用硬约束：
- 每个 core_conclusions 项都必须有 citation。
- citation.quote 必须是原文中的连续文本片段，不可改写，不可拼接。
- citation.memo 必须引用给定的 memo 名称之一。`, perspective.Title, perspective.Prompt)

	var userBuilder strings.Builder
	userBuilder.WriteString("以下是待分析笔记（仅可引用这些来源）：\n")
	for index, memo := range memos {
		userBuilder.WriteString(fmt.Sprintf("\n[%d] %s\n%s\n", index+1, memo.Name, memo.PromptContent))
	}
	if attempt > 0 {
		userBuilder.WriteString(fmt.Sprintf("\n上一次输出未通过校验，错误：%v。请重新输出完全合规 JSON。", lastErr))
	}

	return []ai.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userBuilder.String()},
	}
}

func parseInsightModelOutput(rawOutput string) (*insightModelOutput, error) {
	content := strings.TrimSpace(rawOutput)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	if output, err := unmarshalInsightModelOutput(content); err == nil {
		return output, nil
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return nil, errors.New("model output is not valid JSON object")
	}

	return unmarshalInsightModelOutput(content[start : end+1])
}

func unmarshalInsightModelOutput(content string) (*insightModelOutput, error) {
	output := &insightModelOutput{}
	if err := json.Unmarshal([]byte(content), output); err != nil {
		return nil, errors.Wrap(err, "failed to parse model output")
	}
	return output, nil
}

func validateInsightModelOutput(output *insightModelOutput, memos []*insightPromptMemo) error {
	if output == nil {
		return errors.New("empty model output")
	}
	if strings.TrimSpace(output.Summary) == "" {
		return errors.New("summary is required")
	}
	if len(output.CoreConclusions) < insightGenerationMinimumConclusions {
		return errors.New("at least one core conclusion is required")
	}
	if len(output.DeepQuestions) < insightGenerationMinimumDeepQuestion {
		return errors.New("at least one deep question is required")
	}

	contentByMemo := make(map[string]string, len(memos))
	for _, memo := range memos {
		contentByMemo[memo.Name] = memo.OriginalContent
	}

	for index, conclusion := range output.CoreConclusions {
		if conclusion == nil {
			return errors.Errorf("core_conclusions[%d] is nil", index)
		}
		if strings.TrimSpace(conclusion.Conclusion) == "" {
			return errors.Errorf("core_conclusions[%d].conclusion is required", index)
		}
		if conclusion.Citation == nil {
			return errors.Errorf("core_conclusions[%d].citation is required", index)
		}

		citation := conclusion.Citation
		if strings.TrimSpace(citation.Memo) == "" {
			return errors.Errorf("core_conclusions[%d].citation.memo is required", index)
		}
		if strings.TrimSpace(citation.Quote) == "" {
			return errors.Errorf("core_conclusions[%d].citation.quote is required", index)
		}
		if strings.TrimSpace(citation.Reason) == "" {
			return errors.Errorf("core_conclusions[%d].citation.reason is required", index)
		}

		originalContent, ok := contentByMemo[citation.Memo]
		if !ok {
			return errors.Errorf("core_conclusions[%d] references unknown memo %q", index, citation.Memo)
		}
		if !strings.Contains(originalContent, citation.Quote) {
			return errors.Errorf("core_conclusions[%d] citation quote not found in source memo", index)
		}
	}

	return nil
}

func buildStoreInsightCitations(output *insightModelOutput) []store.InsightCitation {
	citationSet := make(map[string]bool)
	citations := make([]store.InsightCitation, 0, len(output.CoreConclusions))
	for _, conclusion := range output.CoreConclusions {
		if conclusion == nil || conclusion.Citation == nil {
			continue
		}
		c := conclusion.Citation
		key := c.Memo + "\x00" + c.Quote + "\x00" + c.Reason
		if citationSet[key] {
			continue
		}
		citationSet[key] = true
		citations = append(citations, store.InsightCitation{
			Memo:   c.Memo,
			Quote:  c.Quote,
			Reason: c.Reason,
		})
	}
	return citations
}

func renderInsightMarkdown(output *insightModelOutput, truncatedHints []string) string {
	var builder strings.Builder
	builder.WriteString("## 核心总结\n\n")
	builder.WriteString(strings.TrimSpace(output.Summary))
	builder.WriteString("\n\n## 关键结论\n\n")
	for index, conclusion := range output.CoreConclusions {
		if conclusion == nil || conclusion.Citation == nil {
			continue
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n", index+1, strings.TrimSpace(conclusion.Conclusion)))
		builder.WriteString(fmt.Sprintf("   - 引用：`%s`「%s」\n", conclusion.Citation.Memo, strings.TrimSpace(conclusion.Citation.Quote)))
		builder.WriteString(fmt.Sprintf("   - 依据：%s\n", strings.TrimSpace(conclusion.Citation.Reason)))
	}

	builder.WriteString("\n## 深刻问题\n\n")
	for index, question := range output.DeepQuestions {
		trimmed := strings.TrimSpace(question)
		if trimmed == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n", index+1, trimmed))
	}

	if len(truncatedHints) > 0 {
		builder.WriteString("\n> 输入裁剪提示：")
		builder.WriteString(strings.Join(truncatedHints, "；"))
	}

	return strings.TrimSpace(builder.String())
}

func collectResolvedMemoNames(memos []*insightPromptMemo) []string {
	names := make([]string, 0, len(memos))
	for _, memo := range memos {
		names = append(names, memo.Name)
	}
	return names
}

func convertInsightReportFromStore(report *store.InsightReport) *v1pb.InsightReport {
	if report == nil {
		return nil
	}

	citations := make([]*v1pb.InsightCitation, 0, len(report.Citations))
	for _, citation := range report.Citations {
		citations = append(citations, &v1pb.InsightCitation{
			Memo:   citation.Memo,
			Quote:  citation.Quote,
			Reason: citation.Reason,
		})
	}

	return &v1pb.InsightReport{
		Name:              fmt.Sprintf("%s%d/%s%d", UserNamePrefix, report.CreatorID, InsightReportNamePrefix, report.ID),
		Creator:           fmt.Sprintf("%s%d", UserNamePrefix, report.CreatorID),
		CreateTime:        timestamppb.New(time.Unix(report.CreatedTs, 0)),
		Perspective:       report.Perspective,
		Summary:           report.Summary,
		Insight:           report.Insight,
		SourceFilter:      report.SourceFilter,
		SourceMemoNames:   report.SourceMemoNames,
		ResolvedMemoCount: report.ResolvedMemoCount,
		Citations:         citations,
	}
}

func (s *APIV1Service) trimInsightReportHistory(ctx context.Context, creatorID int32) error {
	offset := insightHistoryRetention
	for {
		limit := insightHistoryTrimBatchDelete
		oldReports, err := s.Store.ListInsightReports(ctx, &store.FindInsightReport{
			CreatorID: &creatorID,
			Limit:     &limit,
			Offset:    &offset,
		})
		if err != nil {
			return err
		}
		if len(oldReports) == 0 {
			return nil
		}
		for _, report := range oldReports {
			if err := s.Store.DeleteInsightReport(ctx, &store.DeleteInsightReport{ID: report.ID}); err != nil {
				return err
			}
		}
		if len(oldReports) < limit {
			return nil
		}
	}
}

func intPointer(v int) *int {
	return &v
}
