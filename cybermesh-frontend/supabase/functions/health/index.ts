import "https://deno.land/x/xhr@0.1.0/mod.ts";
import { serve } from "https://deno.land/std@0.168.0/http/server.ts";

import {
  generateRequestId,
  getClientIdentifier,
  checkRateLimit,
  getResponseHeaders,
  createRateLimitResponse,
  createErrorResponse,
  logRequest,
  logResponse,
  getCorsHeaders,
  securityHeaders,
} from "../_shared/security.ts";

const FUNCTION_NAME = "health";

interface HealthResponse {
  status: 'ok' | 'error';
  timestamp: string;
  version: string;
  mode: 'edge' | 'proxy';
  requestId: string;
  backend?: {
    status: 'ok' | 'error' | 'unreachable';
    latency_ms?: number;
  };
}

interface ReadinessResponse {
  ready: boolean;
  timestamp: string;
  requestId: string;
  checks: {
    edge_function: boolean;
    go_backend?: boolean;
  };
}

serve(async (req) => {
  const requestId = generateRequestId();
  const origin = req.headers.get("origin");
  const clientId = getClientIdentifier(req);

  // Handle CORS preflight requests
  if (req.method === 'OPTIONS') {
    return new Response(null, { 
      headers: { ...getCorsHeaders(origin), ...securityHeaders } 
    });
  }

  const url = new URL(req.url);
  const path = url.pathname.split('/').pop();
  const backendBase = Deno.env.get('BACKEND_API_BASE');

  // Log incoming request
  logRequest(FUNCTION_NAME, requestId, clientId, req.method, { path });

  // Check rate limit
  const rateLimit = checkRateLimit(`${FUNCTION_NAME}:${clientId}`);
  if (!rateLimit.allowed) {
    logResponse(FUNCTION_NAME, requestId, 429, { reason: "rate_limit_exceeded" });
    return createRateLimitResponse(origin, requestId, rateLimit.resetInSec);
  }

  try {
    if (path === 'health') {
      const response: HealthResponse = {
        status: 'ok',
        timestamp: new Date().toISOString(),
        version: '1.0.0',
        mode: backendBase ? 'proxy' : 'edge',
        requestId,
      };

      // If Go backend is configured, check its health too
      if (backendBase) {
        const startTime = Date.now();
        try {
          const backendHealth = await fetch(`${backendBase}/health`, {
            method: 'GET',
            signal: AbortSignal.timeout(5000), // 5s timeout
          });
          response.backend = {
            status: backendHealth.ok ? 'ok' : 'error',
            latency_ms: Date.now() - startTime,
          };
        } catch (error) {
          console.error('[health] Backend health check failed:', error);
          response.backend = {
            status: 'unreachable',
            latency_ms: Date.now() - startTime,
          };
        }
      }

      logResponse(FUNCTION_NAME, requestId, 200, { status: response.status });

      return new Response(JSON.stringify(response), {
        headers: getResponseHeaders(origin, requestId, {
          remaining: rateLimit.remaining,
          resetInSec: rateLimit.resetInSec,
        }),
        status: 200,
      });
    }

    if (path === 'ready') {
      const checks: ReadinessResponse['checks'] = {
        edge_function: true,
      };

      // If Go backend is configured, check its readiness too
      if (backendBase) {
        try {
          const backendReady = await fetch(`${backendBase}/ready`, {
            method: 'GET',
            signal: AbortSignal.timeout(5000),
          });
          checks.go_backend = backendReady.ok;
        } catch (error) {
          console.error('[health] Backend readiness check failed:', error);
          checks.go_backend = false;
        }
      }

      const allReady = Object.values(checks).every(Boolean);
      const response: ReadinessResponse = {
        ready: allReady,
        timestamp: new Date().toISOString(),
        requestId,
        checks,
      };

      logResponse(FUNCTION_NAME, requestId, allReady ? 200 : 503, { ready: response.ready });

      return new Response(JSON.stringify(response), {
        headers: getResponseHeaders(origin, requestId, {
          remaining: rateLimit.remaining,
          resetInSec: rateLimit.resetInSec,
        }),
        status: allReady ? 200 : 503,
      });
    }

    // Unknown endpoint
    logResponse(FUNCTION_NAME, requestId, 404, { reason: "not_found" });
    return createErrorResponse(origin, requestId, 404, "Not found");
  } catch (error) {
    logResponse(FUNCTION_NAME, requestId, 500, { 
      error: error instanceof Error ? error.message : "Unknown error" 
    });
    return createErrorResponse(origin, requestId, 500, "Internal server error");
  }
});
