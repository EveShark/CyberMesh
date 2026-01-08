// Typed API client with fetch wrapper, AbortController support, performance tracking,
// and request deduplication to prevent duplicate in-flight requests.
// Routes through Supabase Edge Functions for circuit breaker, caching, and rate limiting

import type { AIEngineData } from "@/types/ai-engine";
import type { BlockchainData } from "@/types/blockchain";
import type { NetworkData } from "@/types/network";
import type { ThreatsData } from "@/types/threats";
import type { SystemHealthData } from "@/types/system-health";
import type { DashboardOverviewData } from "@/types/dashboard";
import { recordApiPerformance } from "@/lib/api/performance";
import { rateLimitedFetch, type RateLimitedFetchOptions } from "@/lib/api/rate-limiter";
import { transformApiResponse, transformApiRequest } from "@/lib/utils/json";

// Adapter Imports
import { adaptAIEngine, adaptThreats, adaptNetwork, adaptBlockchain, adaptSystemHealth } from "./adapters";
import type { DashboardOverviewRaw, BlockResponseRaw } from "./types/raw";

// Backend API Response wrapper
interface BackendResponse<T> {
  success: boolean;
  data: T;
  meta?: {
    timestamp: number;
    request_id?: string;
    version?: string;
  };
  error?: {
    code: string;
    message: string;
    details?: Record<string, unknown>;
    request_id?: string;
    timestamp?: number;
  };
}

// API Response types with timestamps
export interface ApiResponse<T> {
  data: T;
  updatedAt: string;
}

export type DashboardOverviewResponse = ApiResponse<DashboardOverviewData>;
export type AIEngineResponse = ApiResponse<AIEngineData>;
export type BlockchainResponse = ApiResponse<BlockchainData>;
export type NetworkResponse = ApiResponse<NetworkData>;
export type ThreatsResponse = ApiResponse<ThreatsData>;
export type SystemHealthResponse = ApiResponse<SystemHealthData>;

// Error context from backend
export interface ApiErrorContext {
  requestId?: string;
  timestamp?: number;
  details?: Record<string, unknown>;
}

// Error type for API errors with detailed information
export class ApiError extends Error {
  public context?: ApiErrorContext;

  constructor(
    message: string,
    public status: number,
    public statusText: string,
    public isTimeout: boolean = false,
    public isNetworkError: boolean = false,
    public isRateLimited: boolean = false,
    context?: ApiErrorContext
  ) {
    super(message);
    this.name = "ApiError";
    this.context = context;
  }
}

/**
 * Get the base URL for API requests.
 * In development: uses relative path for Vite proxy
 * In production: points to the Go Backend (REST API)
 */
const getBaseUrl = (): string => {
  // In development, use relative path so Vite proxy intercepts requests
  if (import.meta.env.DEV) {
    return "/api/v1";
  }

  // In production, use full backend URL
  const backendUrl = import.meta.env.VITE_BACKEND_URL;
  if (!backendUrl) {
    console.warn("VITE_BACKEND_URL is not set, defaulting to https://api.cybermesh.qzz.io:9441");
    return "https://api.cybermesh.qzz.io:9441/api/v1";
  }
  return `${backendUrl}/api/v1`;
};

// Default timeout for requests (15 seconds)
const DEFAULT_TIMEOUT = 60000;

// Rate limiting configuration
const RATE_LIMIT_CONFIG: Partial<RateLimitedFetchOptions> = {
  maxRetries: 3,
  baseDelay: 1000,
  maxDelay: 30000,
  retryStatusCodes: [429, 502, 503, 504],
};

// In-flight request deduplication store
// Prevents duplicate requests when components re-render rapidly
const inFlightRequests = new Map<string, Promise<unknown>>();

/**
 * Get a unique key for deduplication based on method and path
 */
function getRequestKey(method: string, path: string): string {
  return `${method}:${path}`;
}

/**
 * Generic fetch wrapper with error handling, typing, AbortController support,
 * rate limiting, JSON transformation, performance tracking, and request deduplication
 */
async function fetchApi<T>(
  path: string,
  options: RequestInit & {
    signal?: AbortSignal;
    timeout?: number;
    skipTransform?: boolean;
    skipDedup?: boolean;
  } = {}
): Promise<T> {
  const baseUrl = getBaseUrl();
  const url = `${baseUrl}${path}`;
  const { timeout = DEFAULT_TIMEOUT, skipTransform, skipDedup, ...fetchOptions } = options;
  const method = (fetchOptions.method ?? "GET").toUpperCase();

  // Request deduplication for GET requests (idempotent only)
  if (!skipDedup && method === "GET") {
    const requestKey = getRequestKey(method, path);

    // Skip dedup if client provided signal to allow independent abort
    if (!options.signal) {
      const existingRequest = inFlightRequests.get(requestKey);
      if (existingRequest) {
        console.debug(`[API] Deduplicating request: ${path}`);
        return existingRequest as Promise<T>;
      }
    }

    // Create request
    const requestPromise = executeRequest<T>(path, url, timeout, skipTransform, fetchOptions, options.signal);

    // Only store in dedup map if no signal provided
    if (!options.signal) {
      inFlightRequests.set(requestKey, requestPromise);
      requestPromise.finally(() => {
        inFlightRequests.delete(requestKey);
      });
    }

    return requestPromise;
  }

  // Non-GET requests or dedup skipped - execute directly
  return executeRequest<T>(path, url, timeout, skipTransform, fetchOptions, options.signal);
}

/**
 * Execute the actual API request (internal function)
 */
async function executeRequest<T>(
  path: string,
  url: string,
  timeout: number,
  skipTransform: boolean | undefined,
  fetchOptions: RequestInit,
  externalSignal?: AbortSignal
): Promise<T> {
  const startTime = performance.now();

  // Create AbortController for timeout if none provided
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), timeout);

  // Use provided signal or our timeout controller
  const signal = externalSignal || controller.signal;

  const defaultHeaders: HeadersInit = {
    "Content-Type": "application/json",
  };

  try {
    const result = await rateLimitedFetch<BackendResponse<T>>(url, {
      ...fetchOptions,
      ...RATE_LIMIT_CONFIG,
      signal,
      headers: {
        ...defaultHeaders,
        ...fetchOptions.headers,
      },
    });

    // Log rate limit headers if present
    if (result.response?.headers) {
      const remaining = result.response.headers.get('X-RateLimit-Remaining');
      const limit = result.response.headers.get('X-RateLimit-Limit');
      const reset = result.response.headers.get('X-RateLimit-Reset');

      if (remaining || limit || reset) {
        console.debug('[API] Rate limit', {
          remaining: remaining ? parseInt(remaining) : undefined,
          limit: limit ? parseInt(limit) : undefined,
          resetAt: reset ? new Date(parseInt(reset) * 1000).toISOString() : undefined,
        });
      }
    }

    // Log retry information if retries occurred
    if (result.retryCount > 0) {
      console.info(
        `[API] Request succeeded after ${result.retryCount} retries`
      );
    }

    clearTimeout(timeoutId);
    const duration = performance.now() - startTime;

    // Check for backend-level success
    if (!result.data.success) {
      recordApiPerformance(path, duration, "error");
      // Preserve error context from backend
      throw new ApiError(
        result.data.error?.message || "Unknown backend error",
        500,
        result.data.error?.code || "INTERNAL_ERROR",
        false,
        false,
        false,
        {
          requestId: result.data.error?.request_id || result.data.meta?.request_id,
          timestamp: result.data.error?.timestamp,
          details: result.data.error?.details,
        }
      );
    }

    // Record successful performance
    recordApiPerformance(path, duration, "success");

    // Unwrap the backend response
    const backendData = result.data.data;

    // Convert meta timestamp (Unix seconds) to ISO string
    const timestamp = result.data.meta?.timestamp
      ? new Date(result.data.meta.timestamp * 1000).toISOString()
      : new Date().toISOString();

    // Transform data before creating response
    const transformedData = !skipTransform
      ? transformApiResponse<T>(backendData)
      : (backendData as T);

    const adaptedResponse = {
      data: transformedData,
      updatedAt: timestamp
    };

    return adaptedResponse as T;
  } catch (error) {
    clearTimeout(timeoutId);
    const duration = performance.now() - startTime;

    // Handle abort/timeout
    if (error instanceof DOMException && error.name === "AbortError") {
      recordApiPerformance(path, duration, "error");
      throw new ApiError(
        "Request timed out",
        0,
        "Timeout",
        true,
        false,
        false
      );
    }

    // Handle network errors
    if (error instanceof TypeError && error.message.includes("fetch")) {
      recordApiPerformance(path, duration, "error");
      throw new ApiError(
        "Network error - please check your connection",
        0,
        "Network Error",
        false,
        true,
        false
      );
    }

    // Handle rate limit errors (from rate-limited-fetch)
    if (error instanceof Error && error.message.includes("Rate limit exceeded")) {
      recordApiPerformance(path, duration, "error");
      throw new ApiError(
        error.message,
        429,
        "Rate Limited",
        false,
        false,
        true
      );
    }

    // Re-throw ApiErrors (already recorded)
    if (error instanceof ApiError) {
      throw error;
    }

    // Handle unknown errors
    recordApiPerformance(path, duration, "error");
    throw new ApiError(
      error instanceof Error ? error.message : "Unknown error occurred",
      0,
      "Unknown",
      false,
      false,
      false
    );
  }
}

/**
 * Create a query function with AbortController support for React Query
 * This allows React Query to cancel requests when components unmount
 */
export function createQueryFn<T>(path: string) {
  return ({ signal }: { signal?: AbortSignal } = {}): Promise<T> =>
    fetchApi<T>(path, { method: "GET", signal });
}

/**
// Typed API client with fetch wrapper, AbortController support, performance tracking,
// and request deduplication to prevent duplicate in-flight requests.
// Routes through Supabase Edge Functions for circuit breaker, caching, and rate limiting

import type { DashboardOverviewData } from "@/mocks/dashboard";
import { mockAIEngineData, type AIEngineData } from "@/mocks/ai-engine";
import type { BlockchainData } from "@/types/blockchain";
import type { NetworkData } from "@/types/network";
import type { ThreatsData } from "@/types/threats";
import type { SystemHealthData } from "@/types/system-health";
import { recordApiPerformance } from "@/lib/api/performance";
import { rateLimitedFetch, type RateLimitedFetchOptions } from "@/lib/api/rate-limiter";
import { transformApiResponse, transformApiRequest } from "@/lib/utils/json";

// Adapter Imports
import { adaptAIEngine, adaptThreats, adaptNetwork, adaptBlockchain, adaptSystemHealth } from "./adapters";
import type { DashboardOverviewRaw } from "./types/raw";

/**
 * API client with typed methods for each endpoint
 * No versioning in paths (e.g., /dashboard-overview, not /v1/dashboard-overview)
 */
export const apiClient = {
  /**
   * GET request helper
   */
  get: <T>(path: string, signal?: AbortSignal): Promise<T> =>
    fetchApi<T>(path, { method: "GET", signal }),

  /**
   * POST request helper (transforms request body to snake_case)
   */
  post: <T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> => {
    const transformedBody = transformApiRequest(body);
    return fetchApi<T>(path, {
      method: "POST",
      body: JSON.stringify(transformedBody),
      signal,
    });
  },

  // Specific endpoint methods for type safety (with signal support)
  dashboard: {
    getOverview: (signal?: AbortSignal): Promise<DashboardOverviewResponse> =>
      fetchApi<DashboardOverviewResponse>("/dashboard/overview", { method: "GET", signal, skipTransform: true }),
  },

  aiEngine: {
    getStatus: async (signal?: AbortSignal): Promise<AIEngineResponse> => {
      const raw = await fetchApi<ApiResponse<DashboardOverviewRaw>>("/dashboard/overview", { method: "GET", signal, skipTransform: true });
      return { data: adaptAIEngine(raw.data?.ai), updatedAt: new Date().toISOString() };
    },
  },

  blockchain: {
    getData: async (signal?: AbortSignal): Promise<BlockchainResponse> => {
      const raw = await fetchApi<ApiResponse<DashboardOverviewRaw>>("/dashboard/overview", { method: "GET", signal, skipTransform: true });
      return { data: adaptBlockchain(raw.data?.blocks, raw.data?.ledger), updatedAt: new Date().toISOString() };
    },
    getBlocks: async (limit: number = 100, signal?: AbortSignal): Promise<{ blocks: BlockResponseRaw[], total: number }> => {
      try {
        const raw = await fetchApi<ApiResponse<{ blocks: BlockResponseRaw[], total: number }>>(`/blocks?limit=${limit}`, { method: "GET", signal, skipTransform: true });
        return { blocks: raw.data?.blocks || [], total: raw.data?.total || 0 };
      } catch (error) {
        // Fallback: if /blocks doesn't exist, return empty
        console.warn('[API] /blocks endpoint not available or failed:', error);
        return { blocks: [], total: 0 };
      }
    },
  },

  network: {
    getStatus: async (signal?: AbortSignal): Promise<NetworkResponse> => {
      const raw = await fetchApi<ApiResponse<DashboardOverviewRaw>>("/dashboard/overview", { method: "GET", signal, skipTransform: true });
      return { data: adaptNetwork(raw.data?.network, raw.data?.blocks?.metrics, raw.data?.consensus), updatedAt: new Date().toISOString() };
    },
  },

  threats: {
    getSummary: async (signal?: AbortSignal): Promise<ThreatsResponse> => {
      const raw = await fetchApi<ApiResponse<DashboardOverviewRaw>>("/dashboard/overview", { method: "GET", signal, skipTransform: true });
      return { data: adaptThreats(raw.data?.threats, raw.data?.validators?.total || 5, raw.data?.ai?.history), updatedAt: new Date().toISOString() };
    },
  },

  systemHealth: {
    getStatus: async (signal?: AbortSignal): Promise<SystemHealthResponse> => {
      const raw = await fetchApi<ApiResponse<DashboardOverviewRaw>>("/dashboard/overview", { method: "GET", signal, skipTransform: true });
      return { data: adaptSystemHealth(raw.data), updatedAt: new Date().toISOString() };
    },
  },
};

export default apiClient;








