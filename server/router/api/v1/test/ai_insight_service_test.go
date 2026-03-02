package test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/plugin/ai"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	storepb "github.com/usememos/memos/proto/gen/store"
	apiv1 "github.com/usememos/memos/server/router/api/v1"
	"github.com/usememos/memos/store"
)

type mockAIClient struct {
	responses []string
	err       error
	callCount int
}

func (m *mockAIClient) GenerateCompletion(_ context.Context, _ []ai.ChatMessage) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if len(m.responses) == 0 {
		return "", nil
	}
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx], nil
}

func setupAIConfig(t *testing.T, ts *TestService, ctx context.Context) {
	_, err := ts.Store.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
		Key: storepb.InstanceSettingKey_AI_CONFIG,
		Value: &storepb.InstanceSetting_AiConfigSetting{
			AiConfigSetting: &storepb.InstanceAIConfigSetting{
				Enabled: true,
				ApiKey:  "test-key",
				Model:   "test-model",
			},
		},
	})
	require.NoError(t, err)
}

func buildInsightOutputJSON(t *testing.T, memoName, quote string) string {
	t.Helper()
	payload := map[string]any{
		"summary": "跨笔记主题清晰，存在重复模式。",
		"core_conclusions": []map[string]any{
			{
				"conclusion": "你在关键决策上依赖稳定的内在规则。",
				"citation": map[string]any{
					"memo":   memoName,
					"quote":  quote,
					"reason": "该句直接体现了你的判断策略。",
				},
			},
		},
		"deep_questions": []string{
			"这个判断规则在什么情境下会失效？",
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(raw)
}

func createMemoForUser(t *testing.T, ts *TestService, ctx context.Context, content string) *v1pb.Memo {
	t.Helper()
	memo, err := ts.Service.CreateMemo(ctx, &v1pb.CreateMemoRequest{
		Memo: &v1pb.Memo{
			Content: content,
		},
	})
	require.NoError(t, err)
	return memo
}

func TestGenerateInsightWithMemoNamesAndHistory(t *testing.T) {
	ctx := context.Background()
	ts := NewTestService(t)
	defer ts.Cleanup()

	user, err := ts.CreateRegularUser(ctx, "insight-user")
	require.NoError(t, err)
	userCtx := ts.CreateUserContext(ctx, user.ID)
	setupAIConfig(t, ts, ctx)

	memoOne := createMemoForUser(t, ts, userCtx, "我通常先拆分问题，再做决策。")
	memoTwo := createMemoForUser(t, ts, userCtx, "我会复盘计划和执行之间的偏差。")

	mockClient := &mockAIClient{
		responses: []string{
			buildInsightOutputJSON(t, memoOne.Name, "先拆分问题，再做决策"),
		},
	}
	ts.Service.AIClientFactory = func(_ ai.Config) apiv1.AIClient {
		return mockClient
	}

	resp, err := ts.Service.GenerateInsight(userCtx, &v1pb.GenerateInsightRequest{
		MemoNames:   []string{memoOne.Name, memoTwo.Name},
		Perspective: "critical_thinking",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Insight)
	require.NotContains(t, resp.Insight, "memos/")
	require.Contains(t, resp.Insight, "先拆分问题，再做决策")
	require.Contains(t, resp.Insight, "该句直接体现了你的判断策略")
	require.NotNil(t, resp.Report)
	require.Equal(t, int32(2), resp.Report.ResolvedMemoCount)
	require.NotEmpty(t, resp.Report.Name)
	require.NotEmpty(t, resp.Report.Citations)
	require.Equal(t, memoOne.Name, resp.Report.Citations[0].Memo)

	listResp, err := ts.Service.ListInsightReports(userCtx, &v1pb.ListInsightReportsRequest{
		Parent: fmt.Sprintf("users/%d", user.ID),
	})
	require.NoError(t, err)
	require.NotEmpty(t, listResp.InsightReports)
	require.Equal(t, resp.Report.Name, listResp.InsightReports[0].Name)

	getResp, err := ts.Service.GetInsightReport(userCtx, &v1pb.GetInsightReportRequest{
		Name: resp.Report.Name,
	})
	require.NoError(t, err)
	require.Equal(t, resp.Report.Name, getResp.Name)
}

func TestGenerateInsightFilterScopedToCurrentUser(t *testing.T) {
	ctx := context.Background()
	ts := NewTestService(t)
	defer ts.Cleanup()

	userOne, err := ts.CreateRegularUser(ctx, "insight-filter-user-one")
	require.NoError(t, err)
	userTwo, err := ts.CreateRegularUser(ctx, "insight-filter-user-two")
	require.NoError(t, err)

	userOneCtx := ts.CreateUserContext(ctx, userOne.ID)
	userTwoCtx := ts.CreateUserContext(ctx, userTwo.ID)
	setupAIConfig(t, ts, ctx)

	memoOne := createMemoForUser(t, ts, userOneCtx, "shared-keyword 这是用户一的笔记内容。")
	_ = createMemoForUser(t, ts, userTwoCtx, "shared-keyword 这是用户二的笔记内容。")

	mockClient := &mockAIClient{
		responses: []string{
			buildInsightOutputJSON(t, memoOne.Name, "shared-keyword"),
		},
	}
	ts.Service.AIClientFactory = func(_ ai.Config) apiv1.AIClient {
		return mockClient
	}

	resp, err := ts.Service.GenerateInsight(userOneCtx, &v1pb.GenerateInsightRequest{
		Filter:      `content.contains("shared-keyword")`,
		Perspective: "systems_thinking",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Report)
	require.Equal(t, int32(1), resp.Report.ResolvedMemoCount)
}

func TestGenerateInsightCitationValidationRetryAndFail(t *testing.T) {
	ctx := context.Background()
	ts := NewTestService(t)
	defer ts.Cleanup()

	user, err := ts.CreateRegularUser(ctx, "insight-retry-user")
	require.NoError(t, err)
	userCtx := ts.CreateUserContext(ctx, user.ID)
	setupAIConfig(t, ts, ctx)

	memo := createMemoForUser(t, ts, userCtx, "真实可引用内容。")

	invalidOutput := buildInsightOutputJSON(t, memo.Name, "不存在的引用片段")
	mockClient := &mockAIClient{
		responses: []string{
			invalidOutput,
			invalidOutput,
		},
	}
	ts.Service.AIClientFactory = func(_ ai.Config) apiv1.AIClient {
		return mockClient
	}

	_, err = ts.Service.GenerateInsight(userCtx, &v1pb.GenerateInsightRequest{
		MemoNames: []string{memo.Name},
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), "citation quote not found")
	require.Equal(t, 2, mockClient.callCount)
}

func TestInsightReportPermissionDeniedForOtherUser(t *testing.T) {
	ctx := context.Background()
	ts := NewTestService(t)
	defer ts.Cleanup()

	owner, err := ts.CreateRegularUser(ctx, "insight-owner-user")
	require.NoError(t, err)
	other, err := ts.CreateRegularUser(ctx, "insight-other-user")
	require.NoError(t, err)

	ownerCtx := ts.CreateUserContext(ctx, owner.ID)
	otherCtx := ts.CreateUserContext(ctx, other.ID)
	setupAIConfig(t, ts, ctx)

	memo := createMemoForUser(t, ts, ownerCtx, "owner memo content")
	mockClient := &mockAIClient{
		responses: []string{
			buildInsightOutputJSON(t, memo.Name, "owner memo content"),
		},
	}
	ts.Service.AIClientFactory = func(_ ai.Config) apiv1.AIClient {
		return mockClient
	}

	resp, err := ts.Service.GenerateInsight(ownerCtx, &v1pb.GenerateInsightRequest{
		MemoNames: []string{memo.Name},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Report)

	_, err = ts.Service.GetInsightReport(otherCtx, &v1pb.GetInsightReportRequest{
		Name: resp.Report.Name,
	})
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	_, err = ts.Service.ListInsightReports(otherCtx, &v1pb.ListInsightReportsRequest{
		Parent: fmt.Sprintf("users/%d", owner.ID),
	})
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestInsightReportRetentionKeepLatest200(t *testing.T) {
	ctx := context.Background()
	ts := NewTestService(t)
	defer ts.Cleanup()

	user, err := ts.CreateRegularUser(ctx, "insight-retention-user")
	require.NoError(t, err)
	userCtx := ts.CreateUserContext(ctx, user.ID)
	setupAIConfig(t, ts, ctx)

	memo := createMemoForUser(t, ts, userCtx, "retention memo content")
	mockClient := &mockAIClient{
		responses: []string{
			buildInsightOutputJSON(t, memo.Name, "retention memo content"),
		},
	}
	ts.Service.AIClientFactory = func(_ ai.Config) apiv1.AIClient {
		return mockClient
	}

	for i := 0; i < 201; i++ {
		_, err := ts.Service.GenerateInsight(userCtx, &v1pb.GenerateInsightRequest{
			MemoNames: []string{memo.Name},
		})
		require.NoError(t, err, fmt.Sprintf("failed at iteration %d", i))
	}

	limit := 500
	reports, err := ts.Store.ListInsightReports(ctx, &store.FindInsightReport{
		CreatorID: &user.ID,
		Limit:     &limit,
	})
	require.NoError(t, err)
	require.Len(t, reports, 200)
}
