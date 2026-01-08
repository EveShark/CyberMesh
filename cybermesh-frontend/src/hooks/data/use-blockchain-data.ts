import { useQuery } from "@tanstack/react-query";
import { apiClient, type BlockchainResponse } from "@/lib/api";
import { useVisibilityPause } from "@/hooks/common/use-visibility-pause";
import { getMockBlockchainRaw, mockLedgerRaw } from "@/mocks/blockchain";
import { isDemoMode } from "@/config/demo-mode";
import { adaptBlocksFromApi, adaptBlockchain } from "@/lib/api/adapters/blockchain.adapter";

interface UseBlockchainDataOptions {
  pollingInterval?: number;
  enabled?: boolean;
  blockLimit?: number;
}

export const useBlockchainData = (options: UseBlockchainDataOptions = {}) => {
  const { pollingInterval = 15000, enabled: baseEnabled = true, blockLimit = 100 } = options;

  // Pause polling when tab is hidden
  const { enabled } = useVisibilityPause(baseEnabled);

  // In demo mode, return mock data immediately without API calls
  const demoMode = isDemoMode();

  return useQuery<BlockchainResponse>({
    queryKey: ["blockchain-data", blockLimit],
    queryFn: demoMode
      ? async () => {
        const raw = getMockBlockchainRaw();
        const adapted = adaptBlockchain(raw, mockLedgerRaw);
        return { data: adapted, updatedAt: new Date().toISOString() };
      }
      : async ({ signal }) => {
        // Fetch both dashboard overview and extended block list
        const [dashboardResponse, blocksData] = await Promise.all([
          apiClient.blockchain.getData(signal),
          apiClient.blockchain.getBlocks(blockLimit, signal)
        ]);

        // If we got extended blocks, use them; otherwise keep dashboard blocks
        if (blocksData.blocks && blocksData.blocks.length > 0) {
          console.log(`[Blockchain] Using ${blocksData.blocks.length} blocks from /blocks endpoint`);
          const adaptedBlocks = adaptBlocksFromApi(blocksData.blocks);
          dashboardResponse.data.latestBlocks = adaptedBlocks;
        } else {
          console.log(`[Blockchain] /blocks returned empty, using ${dashboardResponse.data.latestBlocks.length} blocks from dashboard`);
        }

        return dashboardResponse;
      },
    refetchInterval: demoMode ? 10000 : pollingInterval,
    enabled: demoMode ? true : enabled,
    staleTime: demoMode ? 0 : 10000,
    gcTime: 1000 * 60 * 5,
    placeholderData: (previousData) => previousData,
  });
};
