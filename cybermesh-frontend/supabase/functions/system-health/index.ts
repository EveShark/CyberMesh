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

const FUNCTION_NAME = "system-health";
const UPSTREAM_PATHS = ["/health", "/ready"]; // Combined endpoints
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
      const healthData = results[0] as Record<string, any>;
      // deno-lint-ignore no-explicit-any
      const readyData = results[1] as Record<string, any>;
      
      // Map backend fields to frontend SystemHealthData structure
      responseData = {
        data: {
          serviceReadiness: readyData?.ready ? "Ready" : "Not Ready",
          aiStatus: healthData?.aiStatus ?? "Unknown",
          updated: new Date().toLocaleTimeString(),
          backendUptime: {
            started: healthData?.uptime?.started ?? healthData?.startedAt ?? "Unknown",
            runtime: healthData?.uptime?.runtime ?? healthData?.runtime ?? "Unknown",
          },
          aiUptime: {
            state: healthData?.ai?.state ?? "unknown",
            loopStatus: healthData?.ai?.loopStatus ?? "Unknown",
            detectionLoopUptime: healthData?.ai?.detectionLoopUptime ?? "Unknown",
          },
          resources: {
            cpu: { avgUtilization: healthData?.resources?.cpu?.avgUtilization ?? "0%" },
            memory: { 
              resident: healthData?.resources?.memory?.resident ?? "0 GB",
              allocated: healthData?.resources?.memory?.allocated ?? null,
            },
          },
          services: (healthData?.services ?? []).map((svc: Record<string, unknown>) => ({
            name: svc.name ?? "Unknown",
            status: svc.status ?? "unknown",
            latency: svc.latency ?? null,
            queryP95: svc.queryP95 ?? null,
            pool: svc.pool ?? null,
            issues: svc.issues ?? "No issues reported",
            version: svc.version ?? null,
            success: svc.success ?? null,
            pending: svc.pending ?? null,
            size: svc.size ?? null,
            oldest: svc.oldest ?? null,
            view: svc.view ?? null,
            round: svc.round ?? null,
            quorum: svc.quorum ?? null,
            peers: svc.peers ?? null,
            throughput: svc.throughput ?? null,
            publishP95: svc.publishP95 ?? null,
            lag: svc.lag ?? null,
            highwater: svc.highwater ?? null,
            clients: svc.clients ?? null,
            errors: svc.errors ?? null,
            txnP95: svc.txnP95 ?? null,
            slowTxns: svc.slowTxns ?? null,
            loop: svc.loop ?? null,
            publishMin: svc.publishMin ?? null,
            detectionsMin: svc.detectionsMin ?? null,
          })),
          pipeline: {
            kafkaProducer: healthData?.pipeline?.kafkaProducer ?? {},
            network: healthData?.pipeline?.network ?? {},
            threatFeed: healthData?.pipeline?.threatFeed ?? {},
          },
          db: {
            cockroach: healthData?.db?.cockroach ?? {},
            redis: healthData?.db?.redis ?? {},
          },
          aiLoop: {
            loopStatus: healthData?.aiLoop?.loopStatus ?? "Unknown",
            avgLatency: healthData?.aiLoop?.avgLatency ?? "0 ms",
            lastIteration: healthData?.aiLoop?.lastIteration ?? "0 ms",
            publishRate: healthData?.aiLoop?.publishRate ?? "0/min",
          },
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
        error: "system_health_error",
        message: "Internal server error",
      }),
      { headers: getResponseHeaders(origin, requestId), status: 500 }
    );
  }
});
