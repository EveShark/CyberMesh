// Typed API client with fetch wrapper, AbortController support, performance tracking,
// and request deduplication to prevent duplicate in-flight requests.
// Routes through Supabase Edge Functions for circuit breaker, caching, and rate limiting  

import type { DashboardOverviewData } from "@/mocks/dashboard";
import type { AIEngineData } from "@/mocks/ai-engine";
import type { BlockchainData } from "@/types/blockchain";
import type { NetworkData } from "@/types/network";
import type { ThreatsData } from "@/types/threats";
import type { SystemHealthData } from "@/types/system-health";

// Adapter Imports
import { adaptAIEngine, adaptThreats, adaptNetwork, adaptBlockchain } from "./adapters";
import type { DashboardOverviewRaw } from "./types/raw";

import { transformApiResponse, transformApiRequest } from "./transform";
import { rateLimitedFetch, RATE_LIMIT_CONFIG } from "@/lib/api/rate-limiter";
import { recordApiPerformance } from "./performance";

// ... rest of existing imports and code ...

// Response Types
type ApiResponse<T> = { data: T; updatedAt: string };
type DashboardOverviewResponse = ApiResponse<DashboardOverviewData>;
type AIEngineResponse = ApiResponse<AIEngineData>;
type BlockchainResponse = ApiResponse<BlockchainData>;
type NetworkResponse = ApiResponse<NetworkData>;
type ThreatsResponse = ApiResponse<ThreatsData>;
type SystemHealthResponse = ApiResponse<SystemHealthData>;

// ... keep all existing fetchApi implementation ...

/**
 * API client with typed methods for each endpoint
 * Automatically adapts backend DTOs to frontend types
 */
export const apiClient = {
  get: <T>(path: string, signal?: AbortSignal): Promise<T> =>
    fetchApi<T>(path, { method: "GET", signal }),

  post: <T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> => {
    const transformedBody = transformApiRequest(body);
    return fetchApi<T>(path, {
      method: "POST",
      body: JSON.stringify(transformedBody),
      signal,
    });
  },

  dashboard: {
    getOverview: async (signal?: AbortSignal): Promise<DashboardOverviewResponse> => {
      // For now, dashboard returns raw data - adapt if needed
      return apiClient.get("/dashboard/overview", signal);
    }
  },

  aiEngine: {
    getStatus: async (signal?: AbortSignal): Promise<AIEngineResponse> => {
      const raw = await fetchApi<DashboardOverviewRaw>("/dashboard/overview", { method: "GET", signal });
      return {
        data: adaptAIEngine(raw.data.ai),
        updatedAt: new Date().toISOString()
      };
    },
  },

  blockchain: {
    getData: async (signal?: AbortSignal): Promise<BlockchainResponse> => {
      const raw = await fetchApi<DashboardOverviewRaw>("/dashboard/overview", { method: "GET", signal });
      return {
        data: adaptBlockchain(raw.data.blocks),
        updatedAt: new Date().toISOString()
      };
    },
  },

  network: {
    getStatus: async (signal?: AbortSignal): Promise<NetworkResponse> => {
      const raw = await fetchApi<DashboardOverviewRaw>("/dashboard/overview", { method: "GET", signal });
      return {
        data: adaptNetwork(raw.data.network),
        updatedAt: new Date().toISOString()
      };
    },
  },

  threats: {
    getSummary: async (signal?: AbortSignal): Promise<ThreatsResponse> => {
      const raw = await fetchApi<DashboardOverviewRaw>("/dashboard/overview", { method: "GET", signal });
      return {
        data: adaptThreats(raw.data.threats, raw.data.validators?.total || 5),
        updatedAt: new Date().toISOString()
      };
    },
  },

  systemHealth: {
    getStatus: (signal?: AbortSignal): Promise<SystemHealthResponse> =>
      apiClient.get("/health", signal),
  },
};

export default apiClient;
