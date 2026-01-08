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

const FUNCTION_NAME = "threats-summary";
const UPSTREAM_PATHS = ["/anomalies", "/anomalies/stats"]; // Combined endpoints
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
      const anomaliesData = results[0] as Record<string, any>;
      // deno-lint-ignore no-explicit-any
      const statsData = results[1] as Record<string, any>;
      
      // Map backend fields to frontend ThreatsData structure
      responseData = {
        data: {
          systemStatus: statsData?.systemStatus ?? "LIVE",
          blockedCount: String(statsData?.blockedCount ?? anomaliesData?.totalBlocked ?? 0),
          updated: new Date().toLocaleTimeString(),
          lifetimeTotal: statsData?.lifetimeTotal ?? anomaliesData?.total ?? 0,
          publishedCount: statsData?.publishedCount ?? 0,
          publishedPercent: statsData?.publishedPercent ?? "0%",
          abstainedCount: statsData?.abstainedCount ?? 0,
          abstainedPercent: statsData?.abstainedPercent ?? "0%",
          avgResponseTime: statsData?.avgResponseTime ?? "0ms",
          validatorCount: statsData?.validatorCount ?? 0,
          quorumSize: statsData?.quorumSize ?? 0,
          severity: {
            critical: { 
              count: statsData?.severity?.critical?.count ?? 0, 
              percent: statsData?.severity?.critical?.percent ?? "0%" 
            },
            high: { 
              count: statsData?.severity?.high?.count ?? 0, 
              percent: statsData?.severity?.high?.percent ?? "0%" 
            },
            medium: { 
              count: statsData?.severity?.medium?.count ?? 0, 
              percent: statsData?.severity?.medium?.percent ?? "0%" 
            },
            low: { 
              count: statsData?.severity?.low?.count ?? 0, 
              percent: statsData?.severity?.low?.percent ?? "0%" 
            },
          },
          breakdown: (anomaliesData?.breakdown ?? statsData?.breakdown ?? []).map((item: Record<string, unknown>) => ({
            type: item.type ?? "unknown",
            severity: item.severity ?? "LOW",
            blocked: item.blocked ?? 0,
            monitored: item.monitored ?? 0,
            blockRate: item.blockRate ?? "0%",
            total: item.total ?? 0,
          })),
          snapshot: {
            totalDetected: statsData?.snapshot?.totalDetected ?? anomaliesData?.total ?? 0,
            publishedSnapshot: statsData?.snapshot?.publishedSnapshot ?? 0,
            abstainedSnapshot: statsData?.snapshot?.abstainedSnapshot ?? 0,
            loopStatus: statsData?.snapshot?.loopStatus ?? "LIVE",
            lastDetection: statsData?.snapshot?.lastDetection ?? new Date().toLocaleTimeString(),
          },
          volumeHistory: (anomaliesData?.volumeHistory ?? statsData?.volumeHistory ?? []).map((point: Record<string, unknown>) => ({
            time: point.time ?? "",
            value: point.value ?? 0,
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
        error: "threats_summary_error",
        message: "Internal server error",
      }),
      { headers: getResponseHeaders(origin, requestId), status: 500 }
    );
  }
});
