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
  fetchUpstream,
  createUpstreamErrorResponse,
  createCircuitBreakerResponse,
} from "../_shared/security.ts";

const FUNCTION_NAME = "network-status";
const UPSTREAM_PATH = "/network/overview";
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
      // Fetch from Go backend with retry logic (1s, 2s, 4s exponential backoff) + circuit breaker
      const { data, error, attempts, circuitOpen } = await fetchUpstream(`${backendBase}${UPSTREAM_PATH}`);

      if (circuitOpen) {
        logResponse(FUNCTION_NAME, requestId, 503, { error, circuitOpen: true });
        return createCircuitBreakerResponse(origin, requestId, FUNCTION_NAME, 30);
      }

      if (error) {
        logResponse(FUNCTION_NAME, requestId, 502, { error, attempts });
        return createUpstreamErrorResponse(origin, requestId, FUNCTION_NAME, error, attempts);
      }

      setCacheData(CACHE_KEY, data);
      responseData = data;
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
        error: "network_status_error",
        message: "Internal server error",
      }),
      { headers: getResponseHeaders(origin, requestId), status: 500 }
    );
  }
});
