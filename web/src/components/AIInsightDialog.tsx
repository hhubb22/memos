import { timestampDate } from "@bufbuild/protobuf/wkt";
import { LoaderIcon, RefreshCwIcon, SparklesIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "react-hot-toast";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import MemoContent from "@/components/MemoContent";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import useCurrentUser from "@/hooks/useCurrentUser";
import { useGenerateInsight, useInsightReport, useInsightReports } from "@/hooks/useInsightQueries";
import { useUserStats } from "@/hooks/useUserQueries";
import { handleError } from "@/lib/error";
import { cn } from "@/lib/utils";
import type { InsightReport } from "@/types/proto/api/v1/memo_service_pb";
import { useTranslate } from "@/utils/i18n";

const INSIGHT_CONFIG_KEY = "memos-ai-insight-config-v1";

const PERSPECTIVE_VALUES = [
  "random",
  "critical_thinking",
  "systems_thinking",
  "emotional_pattern",
  "creative_association",
  "socratic_questioning",
] as const;

const TIME_RANGE_VALUES = ["7d", "30d", "1y", "all"] as const;

type InsightTab = "generate" | "history";
type ScopeMode = "memo_names" | "filter";
type TimeRange = (typeof TIME_RANGE_VALUES)[number];

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultMemoNames?: string[];
  defaultFilter?: string;
  defaultMode?: ScopeMode;
}

interface StoredInsightConfig {
  perspective: string;
  timeRange: TimeRange;
  selectedTags: string[];
  keywords: string;
}

const defaultStoredConfig: StoredInsightConfig = {
  perspective: "random",
  timeRange: "30d",
  selectedTags: [],
  keywords: "",
};

const loadStoredConfig = (): StoredInsightConfig => {
  try {
    const raw = localStorage.getItem(INSIGHT_CONFIG_KEY);
    if (!raw) {
      return defaultStoredConfig;
    }
    const parsed = JSON.parse(raw) as Partial<StoredInsightConfig>;
    return {
      perspective: parsed.perspective || defaultStoredConfig.perspective,
      timeRange: parsed.timeRange || defaultStoredConfig.timeRange,
      selectedTags: Array.isArray(parsed.selectedTags) ? parsed.selectedTags : [],
      keywords: parsed.keywords || "",
    };
  } catch {
    return defaultStoredConfig;
  }
};

const escapeFilterString = (value: string): string => value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');

const buildCustomFilter = (timeRange: TimeRange, selectedTags: string[], keywords: string): string => {
  const conditions: string[] = [];
  const now = Math.floor(Date.now() / 1000);

  if (timeRange !== "all") {
    const secondsByRange: Record<Exclude<TimeRange, "all">, number> = {
      "7d": 7 * 24 * 60 * 60,
      "30d": 30 * 24 * 60 * 60,
      "1y": 365 * 24 * 60 * 60,
    };
    conditions.push(`created_ts >= ${now - secondsByRange[timeRange]}`);
  }

  if (selectedTags.length > 0) {
    const tagExpressions = selectedTags.map((tag) => `tag in ["${escapeFilterString(tag)}"]`);
    conditions.push(tagExpressions.length === 1 ? tagExpressions[0] : `(${tagExpressions.join(" || ")})`);
  }

  const terms = keywords
    .split(/\s+/)
    .map((term) => term.trim())
    .filter(Boolean);
  for (const term of terms) {
    conditions.push(`content.contains("${escapeFilterString(term)}")`);
  }

  return conditions.join(" && ");
};

const combineFilters = (baseFilter: string, customFilter: string): string => {
  if (baseFilter && customFilter) {
    return `(${baseFilter}) && (${customFilter})`;
  }
  return baseFilter || customFilter;
};

const sanitizeInsightContent = (content: string, hasCitations: boolean, sourceMemoLabel: string): string => {
  let sanitized = content;

  if (hasCitations) {
    const citationSectionPatterns = [
      /(?:^|\n)#{1,6}\s*引用依据\s*\n[\s\S]*$/u,
      /(?:^|\n)#{1,6}\s*Citations?\s*\n[\s\S]*$/iu,
      /(?:^|\n)#{1,6}\s*References?\s*\n[\s\S]*$/iu,
      /(?:^|\n)引用依据\s*\n[\s\S]*$/u,
      /(?:^|\n)Citations?\s*\n[\s\S]*$/iu,
      /(?:^|\n)References?\s*\n[\s\S]*$/iu,
    ];

    for (const pattern of citationSectionPatterns) {
      sanitized = sanitized.replace(pattern, "");
    }
  }

  sanitized = sanitized.replace(/`?memos\/[A-Za-z0-9_-]+`?/g, sourceMemoLabel);
  return sanitized.trim();
};

function AIInsightDialog({ open, onOpenChange, defaultMemoNames = [], defaultFilter = "", defaultMode }: Props) {
  const t = useTranslate();
  const { i18n } = useTranslation();
  const currentUser = useCurrentUser();
  const { data: currentUserStats } = useUserStats(currentUser?.name);
  const { data: historyData, isLoading: isHistoryLoading } = useInsightReports(currentUser?.name, 100);
  const [activeTab, setActiveTab] = useState<InsightTab>("generate");
  const [scopeMode, setScopeMode] = useState<ScopeMode>(defaultMode || (defaultMemoNames.length > 0 ? "memo_names" : "filter"));
  const [perspective, setPerspective] = useState<string>(defaultStoredConfig.perspective);
  const [timeRange, setTimeRange] = useState<TimeRange>(defaultStoredConfig.timeRange);
  const [selectedTags, setSelectedTags] = useState<string[]>(defaultStoredConfig.selectedTags);
  const [keywords, setKeywords] = useState<string>(defaultStoredConfig.keywords);
  const [latestReport, setLatestReport] = useState<InsightReport>();
  const [latestInsight, setLatestInsight] = useState<string>("");
  const [selectedHistoryName, setSelectedHistoryName] = useState<string>("");
  const { data: selectedHistoryReport, isLoading: isHistoryReportLoading } = useInsightReport(selectedHistoryName || undefined);
  const generateInsight = useGenerateInsight();

  const perspectiveOptions = useMemo(
    () => [
      { value: "random", label: t("insight.perspective.random") },
      { value: "critical_thinking", label: t("insight.perspective.critical-thinking") },
      { value: "systems_thinking", label: t("insight.perspective.systems-thinking") },
      { value: "emotional_pattern", label: t("insight.perspective.emotional-pattern") },
      { value: "creative_association", label: t("insight.perspective.creative-association") },
      { value: "socratic_questioning", label: t("insight.perspective.socratic-questioning") },
    ],
    [t],
  );

  const timeRangeOptions = useMemo(
    () => [
      { value: "7d" as TimeRange, label: t("insight.time-range.last-7-days") },
      { value: "30d" as TimeRange, label: t("insight.time-range.last-30-days") },
      { value: "1y" as TimeRange, label: t("insight.time-range.last-1-year") },
      { value: "all" as TimeRange, label: t("insight.time-range.all") },
    ],
    [t],
  );

  const localizedPerspectiveLabel = (value: string): string => {
    switch (value) {
      case "random":
        return t("insight.perspective.random");
      case "critical_thinking":
        return t("insight.perspective.critical-thinking");
      case "systems_thinking":
        return t("insight.perspective.systems-thinking");
      case "emotional_pattern":
        return t("insight.perspective.emotional-pattern");
      case "creative_association":
        return t("insight.perspective.creative-association");
      case "socratic_questioning":
        return t("insight.perspective.socratic-questioning");
      default:
        return value;
    }
  };

  const renderReport = (report?: InsightReport, fallbackInsight = "") => {
    const content = report?.insight || fallbackInsight;
    if (!content) {
      return null;
    }

    const sanitizedContent = sanitizeInsightContent(content, Boolean(report?.citations?.length), t("insight.citation.source-memo-label"));

    return (
      <div className="w-full space-y-4">
        {report?.summary && (
          <div className="rounded-md border border-border bg-muted/30 px-3 py-2 text-sm text-foreground">
            <span className="font-medium">{t("insight.citation.summary-label")} </span>
            {report.summary}
          </div>
        )}
        <MemoContent content={sanitizedContent} contentClassName="text-sm leading-relaxed" />
        {report?.citations?.length ? (
          <div className="w-full border-t border-border pt-4 space-y-2">
            <p className="text-sm font-medium">{t("insight.citation.title")}</p>
            <div className="space-y-2">
              {report.citations.map((citation, idx) => (
                <div key={`${citation.memo}-${idx}`} className="rounded-md border border-border/70 bg-muted/20 p-2 text-xs space-y-1">
                  <div className="flex items-center gap-2">
                    <Link to={`/${citation.memo}`} className="text-amber-600 hover:underline">
                      {t("insight.citation.source-memo", { index: idx + 1 })}
                    </Link>
                  </div>
                  <p className="text-foreground italic">“{citation.quote}”</p>
                  <p className="text-muted-foreground">{citation.reason}</p>
                </div>
              ))}
            </div>
          </div>
        ) : null}
      </div>
    );
  };

  const tagCandidates = useMemo(() => {
    const entries = Object.entries(currentUserStats?.tagCount || {});
    return entries
      .sort((a, b) => b[1] - a[1])
      .slice(0, 30)
      .map(([tag]) => tag);
  }, [currentUserStats?.tagCount]);

  const historyReports = historyData?.insightReports || [];
  const activeHistoryReport = selectedHistoryReport || historyReports.find((report) => report.name === selectedHistoryName);
  const isGenerating = generateInsight.isPending;

  useEffect(() => {
    const config = loadStoredConfig();
    const normalizedPerspective = PERSPECTIVE_VALUES.includes(config.perspective as (typeof PERSPECTIVE_VALUES)[number])
      ? config.perspective
      : defaultStoredConfig.perspective;
    const normalizedTimeRange = TIME_RANGE_VALUES.includes(config.timeRange) ? config.timeRange : defaultStoredConfig.timeRange;

    setPerspective(normalizedPerspective);
    setTimeRange(normalizedTimeRange);
    setSelectedTags(config.selectedTags);
    setKeywords(config.keywords);
  }, []);

  useEffect(() => {
    const config: StoredInsightConfig = { perspective, timeRange, selectedTags, keywords };
    localStorage.setItem(INSIGHT_CONFIG_KEY, JSON.stringify(config));
  }, [perspective, timeRange, selectedTags, keywords]);

  useEffect(() => {
    if (!open) {
      return;
    }
    setActiveTab("generate");
    setScopeMode(defaultMode || (defaultMemoNames.length > 0 ? "memo_names" : "filter"));
  }, [open, defaultMemoNames.length, defaultMode]);

  useEffect(() => {
    if (!open || selectedHistoryName || historyReports.length === 0) {
      return;
    }
    setSelectedHistoryName(historyReports[0].name);
  }, [open, selectedHistoryName, historyReports]);

  const toggleTag = (tag: string) => {
    setSelectedTags((prev) => (prev.includes(tag) ? prev.filter((item) => item !== tag) : [...prev, tag]));
  };

  const handleGenerate = async () => {
    const customFilter = buildCustomFilter(timeRange, selectedTags, keywords);
    const finalFilter = combineFilters(defaultFilter, customFilter);

    if (scopeMode === "memo_names" && defaultMemoNames.length === 0) {
      toast.error(t("insight.error.no-memo-set"));
      return;
    }
    if (scopeMode === "filter" && !finalFilter) {
      toast.error(t("insight.error.no-filter"));
      return;
    }

    try {
      const response = await generateInsight.mutateAsync({
        memoNames: scopeMode === "memo_names" ? defaultMemoNames : [],
        filter: scopeMode === "filter" ? finalFilter : "",
        perspective,
      });
      setLatestReport(response.report);
      setLatestInsight(response.insight);
      toast.success(t("insight.message.generated"));
    } catch (error: unknown) {
      await handleError(error, toast.error, { context: "Generate insight" });
    }
  };

  const regenerate = async () => {
    await handleGenerate();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent size="2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <SparklesIcon className="w-5 h-5 text-amber-500" />
            {t("insight.title")}
          </DialogTitle>
          <DialogDescription>{t("insight.description")}</DialogDescription>
        </DialogHeader>

        <div className="w-full flex items-center gap-2 border-b border-border pb-3">
          <Button
            variant={activeTab === "generate" ? "default" : "outline"}
            className={cn("h-8", activeTab === "generate" ? "bg-amber-500 hover:bg-amber-500/90 text-black" : "")}
            onClick={() => setActiveTab("generate")}
          >
            {t("insight.tab.generate")}
          </Button>
          <Button variant={activeTab === "history" ? "default" : "outline"} className="h-8" onClick={() => setActiveTab("history")}>
            {t("insight.tab.history")}
          </Button>
        </div>

        {activeTab === "generate" ? (
          <div className="w-full space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground uppercase tracking-wide">{t("insight.form.scope-mode")}</p>
                <div className="flex flex-wrap gap-2">
                  {defaultMemoNames.length > 0 ? (
                    <Button
                      size="sm"
                      variant={scopeMode === "memo_names" ? "default" : "outline"}
                      onClick={() => setScopeMode("memo_names")}
                      className="h-7"
                    >
                      {t("insight.form.current-with-relations", { count: defaultMemoNames.length })}
                    </Button>
                  ) : null}
                  <Button
                    size="sm"
                    variant={scopeMode === "filter" ? "default" : "outline"}
                    onClick={() => setScopeMode("filter")}
                    className="h-7"
                  >
                    {t("insight.form.custom-scope")}
                  </Button>
                </div>
              </div>
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground uppercase tracking-wide">{t("insight.form.perspective")}</p>
                <select
                  value={perspective}
                  onChange={(event) => setPerspective(event.target.value)}
                  className="w-full h-8 rounded-md border border-border bg-background px-2 text-sm"
                >
                  {perspectiveOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground uppercase tracking-wide">{t("insight.form.keywords")}</p>
                <Input
                  placeholder={t("insight.form.keywords-placeholder")}
                  value={keywords}
                  onChange={(event) => setKeywords(event.target.value)}
                />
              </div>
            </div>

            {scopeMode === "filter" ? (
              <div className="space-y-3 rounded-md border border-border bg-muted/20 p-3">
                <div className="flex flex-col md:flex-row md:items-center gap-3">
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground uppercase tracking-wide">{t("insight.form.time-range")}</p>
                    <div className="flex flex-wrap gap-2">
                      {timeRangeOptions.map((option) => (
                        <Button
                          key={option.value}
                          size="sm"
                          variant={timeRange === option.value ? "default" : "outline"}
                          onClick={() => setTimeRange(option.value)}
                          className="h-7"
                        >
                          {option.label}
                        </Button>
                      ))}
                    </div>
                  </div>
                  {defaultFilter ? (
                    <div className="text-xs text-muted-foreground">
                      {t("insight.form.included-filter")} <code className="font-mono">{defaultFilter}</code>
                    </div>
                  ) : null}
                </div>
                {tagCandidates.length > 0 ? (
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground uppercase tracking-wide">{t("insight.form.tags")}</p>
                    <div className="flex flex-wrap gap-2">
                      {tagCandidates.map((tag) => (
                        <Button
                          key={tag}
                          size="sm"
                          variant={selectedTags.includes(tag) ? "default" : "outline"}
                          className="h-7"
                          onClick={() => toggleTag(tag)}
                        >
                          #{tag}
                        </Button>
                      ))}
                    </div>
                  </div>
                ) : null}
              </div>
            ) : null}

            <div className="flex justify-end gap-2">
              <Button onClick={handleGenerate} disabled={isGenerating} className="gap-2">
                {isGenerating ? <LoaderIcon className="w-4 h-4 animate-spin" /> : <SparklesIcon className="w-4 h-4" />}
                {t("insight.action.generate")}
              </Button>
              {(latestReport || latestInsight) && !isGenerating ? (
                <Button variant="outline" onClick={regenerate} className="gap-2">
                  <RefreshCwIcon className="w-4 h-4" />
                  {t("insight.action.regenerate")}
                </Button>
              ) : null}
            </div>

            <div className="min-h-[260px]">
              {isGenerating ? (
                <div className="flex flex-col items-center justify-center py-12 gap-3">
                  <LoaderIcon className="w-8 h-8 text-amber-500 animate-spin" />
                  <p className="text-sm text-muted-foreground">{t("insight.status.analyzing")}</p>
                </div>
              ) : (
                renderReport(latestReport, latestInsight) || (
                  <div className="text-sm text-muted-foreground py-8">{t("insight.status.empty")}</div>
                )
              )}
            </div>
          </div>
        ) : (
          <div className="w-full grid grid-cols-1 md:grid-cols-[280px_1fr] gap-4 min-h-[360px]">
            <div className="rounded-md border border-border overflow-auto max-h-[60vh]">
              {isHistoryLoading ? (
                <div className="py-8 flex items-center justify-center text-sm text-muted-foreground gap-2">
                  <LoaderIcon className="w-4 h-4 animate-spin" />
                  {t("insight.status.history-loading")}
                </div>
              ) : historyReports.length === 0 ? (
                <div className="py-8 text-center text-sm text-muted-foreground">{t("insight.status.history-empty")}</div>
              ) : (
                <div className="divide-y divide-border">
                  {historyReports.map((report) => (
                    <button
                      type="button"
                      key={report.name}
                      onClick={() => setSelectedHistoryName(report.name)}
                      className={cn(
                        "w-full text-left px-3 py-2 hover:bg-muted/40 transition-colors",
                        selectedHistoryName === report.name ? "bg-muted/40" : "",
                      )}
                    >
                      <p className="text-sm font-medium truncate">
                        {report.summary || localizedPerspectiveLabel(report.perspective) || report.name}
                      </p>
                      <p className="text-xs text-muted-foreground mt-1">
                        {report.createTime ? timestampDate(report.createTime).toLocaleString(i18n.language) : "-"} ·{" "}
                        {t("insight.history-memo-count", { count: report.resolvedMemoCount })}
                      </p>
                    </button>
                  ))}
                </div>
              )}
            </div>

            <div className="rounded-md border border-border p-3 overflow-auto max-h-[60vh]">
              {isHistoryReportLoading ? (
                <div className="py-8 flex items-center justify-center text-sm text-muted-foreground gap-2">
                  <LoaderIcon className="w-4 h-4 animate-spin" />
                  {t("insight.status.report-loading")}
                </div>
              ) : (
                renderReport(activeHistoryReport) || (
                  <div className="text-sm text-muted-foreground">{t("insight.status.history-select")}</div>
                )
              )}
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

export default AIInsightDialog;
