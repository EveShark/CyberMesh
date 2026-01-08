/// <reference types="https://esm.sh/@supabase/functions-js/src/edge-runtime.d.ts" />

import {
  generateRequestId,
  getClientIdentifier,
  checkRateLimit,
  getResponseHeaders,
  createRateLimitResponse,
  logRequest,
  logResponse,
  getCorsHeaders,
  securityHeaders,
  isMethodAllowed,
  createMethodNotAllowedResponse,
  getCachedData,
  setCacheData,
  getCacheControlHeaders,
  fetchUpstreamParallel,
  createUpstreamErrorResponse,
  createCircuitBreakerResponse,
} from "../_shared/security.ts";

const FUNCTION_NAME = "blockchain-data";
const UPSTREAM_PATHS = ["/blocks", "/stats"]; // Combined endpoints
const CACHE_KEY = `${FUNCTION_NAME}:cache`;
const CACHE_TTL_MS = 5000; // 5 seconds

Deno.serve(async (req) => {
  const requestId = generateRequestId();
  const origin = req.headers.get("origin");
  const clientId = getClientIdentifier(req);
  const backendBase = Deno.env.get("BACKEND_API_BASE");

  // Handle CORS preflight requests
  if (req.method === "OPTIONS") {
    return new Response(null, { 
      headers: { ...getCorsHeaders(origin), ...securityHeaders } 
    });
  }

  // Method enforcement (GET only)
  if (!isMethodAllowed(req.method)) {
    logResponse(FUNCTION_NAME, requestId, 405, { reason: "method_not_allowed" });
    return createMethodNotAllowedResponse(origin, requestId);
  }

  // Log incoming request
  logRequest(FUNCTION_NAME, requestId, clientId, req.method, {});

  // Check rate limit
  const rateLimit = checkRateLimit(`${FUNCTION_NAME}:${clientId}`);
  if (!rateLimit.allowed) {
    logResponse(FUNCTION_NAME, requestId, 429, { reason: "rate_limit_exceeded" });
    return createRateLimitResponse(origin, requestId, rateLimit.resetInSec);
  }

  try {
    let responseData: unknown;
    let cacheStatus = "MISS";

    // Always fetch from backend - demo mode is handled by frontend
    if (!backendBase) {
      logResponse(FUNCTION_NAME, requestId, 503, { error: "BACKEND_API_BASE not configured" });
      return new Response(
        JSON.stringify({
          error: "backend_unavailable",
          message: "Backend API is not configured. Enable demo mode in the frontend or configure BACKEND_API_BASE.",
          retryAfter: 30,
        }),
        { headers: getResponseHeaders(origin, requestId), status: 503 }
      );
    }

    // Check cache first
    const cached = getCachedData(CACHE_KEY, CACHE_TTL_MS);
    if (cached) {
      responseData = cached.data;
      cacheStatus = "HIT";
    } else {
      // Parallel fetch both endpoints with retry logic + circuit breaker
      const urls = UPSTREAM_PATHS.map(path => `${backendBase}${path}`);
      const { results, errors, circuitOpen } = await fetchUpstreamParallel(urls);

      if (circuitOpen) {
        logResponse(FUNCTION_NAME, requestId, 503, { errors, circuitOpen: true });
        return createCircuitBreakerResponse(origin, requestId, FUNCTION_NAME, 30);
      }

      if (errors.length > 0) {
        logResponse(FUNCTION_NAME, requestId, 502, { errors });
        return createUpstreamErrorResponse(origin, requestId, FUNCTION_NAME, errors.join("; "));
      }

      // Transform backend response to match frontend expected structure
      // deno-lint-ignore no-explicit-any
      const blocksData = results[0] as Record<string, any>;
      // deno-lint-ignore no-explicit-any
      const statsData = results[1] as Record<string, any>;
      
      // Map backend fields to frontend BlockchainData structure
      responseData = {
        data: {
          updated: new Date().toLocaleTimeString(),
          metrics: {
            latestHeight: statsData?.latestHeight ?? blocksData?.latestHeight ?? 0,
            totalTransactions: statsData?.totalTransactions ?? 0,
            avgBlockTime: statsData?.avgBlockTime ?? "0s",
            avgBlockSize: statsData?.avgBlockSize ?? "0 KB",
            successRate: statsData?.successRate ?? "100.0%",
            pendingTxs: statsData?.pendingTxs ?? 0,
            mempoolSize: statsData?.mempoolSize ?? "0 KB",
          },
          ledger: {
            snapshotHeight: statsData?.snapshotHeight ?? "--",
            stateVersion: statsData?.stateVersion ?? 0,
            rollingBlockTime: statsData?.rollingBlockTime ?? "0 s",
            rollingBlockSize: statsData?.rollingBlockSize ?? "0 KB",
            stateRoot: statsData?.stateRoot ?? "--",
            lastBlockHash: statsData?.lastBlockHash ?? "--",
            snapshotTime: statsData?.snapshotTime ?? "--",
            reputationChanges: statsData?.reputationChanges ?? 0,
            policyUpdates: statsData?.policyUpdates ?? 0,
            quarantineUpdates: statsData?.quarantineUpdates ?? 0,
          },
          timelineData: blocksData?.timeline ?? statsData?.timelineData ?? [],
          networkSnapshot: {
            blocksAnomalies: statsData?.blocksAnomalies ?? 0,
            totalAnomalies: statsData?.totalAnomalies ?? 0,
            peerLatencyAvg: statsData?.peerLatencyAvg ?? "0 ms",
          },
          selectedBlock: blocksData?.selectedBlock ?? null,
          latestBlocks: (blocksData?.blocks ?? blocksData?.latestBlocks ?? []).map((block: Record<string, unknown>) => ({
            height: block.height ?? 0,
            time: block.time ?? block.timestamp ?? "",
            txs: block.txs ?? `${block.txCount ?? 0}tx`,
            hash: block.hash ?? "",
            proposer: block.proposer ?? "",
          })),
        },
        updatedAt: new Date().toISOString(),
      };

      setCacheData(CACHE_KEY, responseData);
    }

    const headers = {
      ...getResponseHeaders(origin, requestId, {
        remaining: rateLimit.remaining,
        resetInSec: rateLimit.resetInSec,
      }),
      ...getCacheControlHeaders(),
      "X-Cache-Status": cacheStatus,
    };

    logResponse(FUNCTION_NAME, requestId, 200, { cacheStatus });

    return new Response(JSON.stringify(responseData), {
      headers,
      status: 200,
    });
  } catch (error) {
    logResponse(FUNCTION_NAME, requestId, 500, { 
      error: error instanceof Error ? error.message : "Unknown error" 
    });
    return new Response(
      JSON.stringify({
        error: "blockchain_data_error",
        message: "Internal server error",
      }),
      { headers: getResponseHeaders(origin, requestId), status: 500 }
    );
  }
});
