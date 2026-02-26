import { create } from "@bufbuild/protobuf";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { memoServiceClient } from "@/connect";
import type { GenerateInsightRequest } from "@/types/proto/api/v1/memo_service_pb";
import { GenerateInsightRequestSchema, ListInsightReportsRequestSchema } from "@/types/proto/api/v1/memo_service_pb";

export const insightKeys = {
  all: ["insightReports"] as const,
  lists: () => [...insightKeys.all, "list"] as const,
  list: (parent: string) => [...insightKeys.lists(), parent] as const,
  details: () => [...insightKeys.all, "detail"] as const,
  detail: (name: string) => [...insightKeys.details(), name] as const,
};

export function useGenerateInsight() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (request: Partial<GenerateInsightRequest>) => {
      const response = await memoServiceClient.generateInsight(create(GenerateInsightRequestSchema, request as Record<string, unknown>));
      return response;
    },
    onSuccess: (response) => {
      if (response.report?.creator) {
        queryClient.invalidateQueries({ queryKey: insightKeys.list(response.report.creator) });
      } else {
        queryClient.invalidateQueries({ queryKey: insightKeys.lists() });
      }
    },
  });
}

export function useInsightReports(parent?: string, pageSize = 50) {
  return useQuery({
    queryKey: insightKeys.list(parent || ""),
    queryFn: async () => {
      if (!parent) {
        return { insightReports: [], nextPageToken: "" };
      }
      return await memoServiceClient.listInsightReports(create(ListInsightReportsRequestSchema, { parent, pageSize }));
    },
    enabled: !!parent,
  });
}

export function useInsightReport(name?: string) {
  return useQuery({
    queryKey: insightKeys.detail(name || ""),
    queryFn: async () => {
      if (!name) {
        return null;
      }
      return await memoServiceClient.getInsightReport({ name });
    },
    enabled: !!name,
  });
}
