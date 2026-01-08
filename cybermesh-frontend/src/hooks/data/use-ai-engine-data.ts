import { useQuery } from "@tanstack/react-query";
import { apiClient, type AIEngineResponse } from "@/lib/api";
import { useVisibilityPause } from "@/hooks/common/use-visibility-pause";
import { getMockAIEngineRaw } from "@/mocks/ai-engine";
import { adaptAIEngine } from "@/lib/api/adapters/ai-engine.adapter";
import { isDemoMode } from "@/config/demo-mode";

interface UseAIEngineDataOptions {
  pollingInterval?: number;
  enabled?: boolean;
}

export const useAIEngineData = (options: UseAIEngineDataOptions = {}) => {
  const { pollingInterval = 10000, enabled: baseEnabled = true } = options;

  // Pause polling when tab is hidden
  const { enabled } = useVisibilityPause(baseEnabled);

  // In demo mode, return mock data immediately without API calls
  const demoMode = isDemoMode();

  return useQuery<AIEngineResponse>({
    queryKey: ["ai-engine-status"],
    queryFn: demoMode
      ? async () => {
        const raw = getMockAIEngineRaw();
        return { data: adaptAIEngine(raw), updatedAt: new Date().toISOString() };
      }
      : async ({ signal }) => {
        const response = await apiClient.aiEngine.getStatus(signal);
        return response;
      },
    refetchInterval: demoMode ? 5000 : pollingInterval,
    enabled: demoMode ? true : enabled,
    staleTime: demoMode ? 0 : 8000,
    gcTime: 1000 * 60 * 5,
    placeholderData: (previousData) => previousData,
  });
};
