import { useQuery } from "@tanstack/react-query";
import { apiClient, type ThreatsResponse } from "@/lib/api";
import { useVisibilityPause } from "@/hooks/common/use-visibility-pause";
import { getMockData } from "@/mocks/threats";
import { adaptThreats } from "@/lib/api/adapters/threats.adapter";
import { threatsResponseSchema, validateApiResponse } from "@/lib/api-validation";
import { isDemoMode } from "@/config/demo-mode";

interface UseThreatsDataOptions {
  pollingInterval?: number;
  enabled?: boolean;
}

export const useThreatsData = (options: UseThreatsDataOptions = {}) => {
  const { pollingInterval = 15000, enabled: baseEnabled = true } = options;

  // Pause polling when tab is hidden
  const { enabled } = useVisibilityPause(baseEnabled);

  // In demo mode, return mock data immediately without API calls
  const demoMode = isDemoMode();

  return useQuery<ThreatsResponse>({
    queryKey: ["threats-summary"],
    queryFn: demoMode
      ? async () => {
        const { raw, history } = getMockData();
        return { data: adaptThreats(raw, 5, history), updatedAt: new Date().toISOString() };
      }
      : async ({ signal }) => {
        const response = await apiClient.threats.getSummary(signal);
        validateApiResponse(threatsResponseSchema, response, 'threats-summary');
        return response;
      },
    refetchInterval: demoMode ? 5000 : pollingInterval,
    enabled: demoMode ? true : enabled,
    staleTime: demoMode ? 0 : 10000,
    gcTime: 1000 * 60 * 5,
    placeholderData: (previousData) => previousData,
  });
};
