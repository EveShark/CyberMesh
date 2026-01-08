import { useQuery } from "@tanstack/react-query";
import { apiClient, type DashboardOverviewResponse } from "@/lib/api";
import { useVisibilityPause } from "@/hooks/common/use-visibility-pause";
import { getMockDashboardData } from "@/mocks/dashboard";
import { isDemoMode } from "@/config/demo-mode";

interface UseDashboardDataOptions {
  pollingInterval?: number;
  enabled?: boolean;
}

export const useDashboardData = (options: UseDashboardDataOptions = {}) => {
  const { pollingInterval = 15000, enabled: baseEnabled = true } = options;

  // Pause polling when tab is hidden
  const { enabled } = useVisibilityPause(baseEnabled);

  // In demo mode, return mock data immediately without API calls
  const demoMode = isDemoMode();

  return useQuery<DashboardOverviewResponse>({
    queryKey: ["dashboard-overview"],
    queryFn: demoMode
      ? async () => ({ data: getMockDashboardData(), updatedAt: new Date().toISOString() })
      : async ({ signal }) => {
        const response = await apiClient.dashboard.getOverview(signal);
        return response;
      },
    refetchInterval: demoMode ? 3000 : pollingInterval,
    enabled: demoMode ? true : enabled,
    staleTime: demoMode ? Infinity : 10000,
    gcTime: 1000 * 60 * 5,
    placeholderData: (previousData) => previousData,
  });
};
