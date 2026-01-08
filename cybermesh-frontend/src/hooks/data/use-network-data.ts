import { useQuery } from "@tanstack/react-query";
import { apiClient, type NetworkResponse } from "@/lib/api";
import { useVisibilityPause } from "@/hooks/common/use-visibility-pause";
import { getMockNetworkRaw, mockBlockMetricsRaw } from "@/mocks/network";
import { adaptNetwork } from "@/lib/api/adapters/network.adapter";
import { isDemoMode } from "@/config/demo-mode";

interface UseNetworkDataOptions {
  pollingInterval?: number;
  enabled?: boolean;
}

export const useNetworkData = (options: UseNetworkDataOptions = {}) => {
  const { pollingInterval = 15000, enabled: baseEnabled = true } = options;

  // Pause polling when tab is hidden
  const { enabled } = useVisibilityPause(baseEnabled);

  // In demo mode, return mock data immediately without API calls
  const demoMode = isDemoMode();

  return useQuery<NetworkResponse>({
    queryKey: ["network-status"],
    queryFn: demoMode
      ? async () => {
        const raw = getMockNetworkRaw();
        const adapted = adaptNetwork(raw, mockBlockMetricsRaw);
        return { data: adapted, updatedAt: new Date().toISOString() };
      }
      : async ({ signal }) => {
        const response = await apiClient.network.getStatus(signal);
        return response;
      },
    refetchInterval: demoMode ? 2000 : pollingInterval,
    enabled: demoMode ? true : enabled,
    staleTime: demoMode ? Infinity : 10000,
    gcTime: 1000 * 60 * 5,
    placeholderData: (previousData) => previousData,
  });
};
