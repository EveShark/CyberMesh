import { useQuery } from "@tanstack/react-query";
import { apiClient, type SystemHealthResponse } from "@/lib/api";
import { useVisibilityPause } from "@/hooks/common/use-visibility-pause";
import { getMockSystemHealthData } from "@/mocks/system-health";
import { systemHealthResponseSchema, validateApiResponse } from "@/lib/api-validation";
import { isDemoMode } from "@/config/demo-mode";

interface UseSystemHealthDataOptions {
  pollingInterval?: number;
  enabled?: boolean;
}

export const useSystemHealthData = (options: UseSystemHealthDataOptions = {}) => {
  const { pollingInterval = 20000, enabled: baseEnabled = true } = options;

  // Pause polling when tab is hidden
  const { enabled } = useVisibilityPause(baseEnabled);

  // In demo mode, return mock data immediately without API calls
  const demoMode = isDemoMode();

  return useQuery<SystemHealthResponse>({
    queryKey: ["system-health"],
    queryFn: demoMode
      ? async () => ({ data: getMockSystemHealthData(), updatedAt: new Date().toISOString() })
      : async ({ signal }) => {
        const response = await apiClient.systemHealth.getStatus(signal);
        validateApiResponse(systemHealthResponseSchema, response, 'system-health');
        return response;
      },
    refetchInterval: demoMode ? 2000 : pollingInterval,
    enabled: demoMode ? true : enabled,
    staleTime: demoMode ? 0 : 15000,
    gcTime: 1000 * 60 * 5,
    placeholderData: (previousData) => previousData,
  });
};
