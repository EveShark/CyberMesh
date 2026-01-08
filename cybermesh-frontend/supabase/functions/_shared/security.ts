// Shared security utilities for edge functions

// Allowed origins for CORS - restrict to specific domains
export const ALLOWED_ORIGINS = [
  "https://cybermesh.app",
  "https://www.cybermesh.app",
  "https://preview.cybermesh.app",
  // GKE deployment domains
  "https://cybermesh.qzz.io",
  "https://www.cybermesh.qzz.io",
  "http://cybermesh.qzz.io",
  /^https?:\/\/.*\.qzz\.io$/,
  // Webcontainer sandbox domains
  /^https:\/\/[a-z0-9-]+\.webcontainer\.io$/,
  /^https:\/\/[a-z0-9-]+\.stackblitz\.io$/,
  // Local development
  "http://localhost:5173",
  "http://localhost:3000",
  "http://127.0.0.1:5173",
  "http://127.0.0.1:3000",
];

// Check if origin is allowed
export function isOriginAllowed(origin: string | null): boolean {
  if (!origin) return false;
  
  for (const allowed of ALLOWED_ORIGINS) {
    if (typeof allowed === "string") {
      if (origin === allowed) return true;
    } else if (allowed instanceof RegExp) {
      if (allowed.test(origin)) return true;
    }
  }
  return false;
}

// Generate CORS headers based on origin
export function getCorsHeaders(origin: string | null): Record<string, string> {
  // In development/preview environments, be more permissive
  // For production, restrict to known origins
  const allowedOrigin = isOriginAllowed(origin) ? origin : "*";
  
  return {
    "Access-Control-Allow-Origin": allowedOrigin as string,
    "Access-Control-Allow-Headers": "authorization, x-client-info, apikey, content-type, x-request-id",
    "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
    "Access-Control-Expose-Headers": "x-request-id, x-cache-status",
    "Vary": "Origin",
  };
}

// Security headers for all responses
export const securityHeaders = {
  "X-Content-Type-Options": "nosniff",
  "X-Frame-Options": "DENY",
  "X-XSS-Protection": "1; mode=block",
  "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
  "Referrer-Policy": "strict-origin-when-cross-origin",
  "Permissions-Policy": "geolocation=(), microphone=(), camera=()",
};

// Generate unique request ID
export function generateRequestId(): string {
  const timestamp = Date.now().toString(36);
  const randomPart = Math.random().toString(36).substring(2, 10);
  return `req_${timestamp}_${randomPart}`;
}

// In-memory rate limiting store
const rateLimitMap = new Map<string, { count: number; resetTime: number }>();

// Rate limiting configuration
interface RateLimitConfig {
  maxRequests: number;
  windowMs: number;
}

// Default rate limit: 60 requests per minute
const DEFAULT_RATE_LIMIT: RateLimitConfig = {
  maxRequests: 60,
  windowMs: 60 * 1000,
};

// Check rate limit
export function checkRateLimit(
  identifier: string,
  config: RateLimitConfig = DEFAULT_RATE_LIMIT
): { allowed: boolean; remaining: number; resetInSec: number } {
  const now = Date.now();
  const entry = rateLimitMap.get(identifier);
  
  // Clean up old entries periodically (every 1000 entries)
  if (rateLimitMap.size > 1000) {
    for (const [key, value] of rateLimitMap.entries()) {
      if (now > value.resetTime) {
        rateLimitMap.delete(key);
      }
    }
  }
  
  if (!entry || now > entry.resetTime) {
    rateLimitMap.set(identifier, { count: 1, resetTime: now + config.windowMs });
    return { 
      allowed: true, 
      remaining: config.maxRequests - 1, 
      resetInSec: Math.ceil(config.windowMs / 1000) 
    };
  }
  
  if (entry.count >= config.maxRequests) {
    const resetInSec = Math.ceil((entry.resetTime - now) / 1000);
    return { allowed: false, remaining: 0, resetInSec };
  }
  
  entry.count++;
  return { 
    allowed: true, 
    remaining: config.maxRequests - entry.count, 
    resetInSec: Math.ceil((entry.resetTime - now) / 1000) 
  };
}

// Get client identifier from request (IP-based)
export function getClientIdentifier(req: Request): string {
  // Try various headers for client IP
  const forwardedFor = req.headers.get("x-forwarded-for");
  const realIp = req.headers.get("x-real-ip");
  const cfConnectingIp = req.headers.get("cf-connecting-ip");
  
  // Use the first available IP or fall back to a hash of user-agent
  const ip = forwardedFor?.split(",")[0]?.trim() || realIp || cfConnectingIp;
  
  if (ip) return ip;
  
  // Fallback to user-agent hash if no IP available
  const userAgent = req.headers.get("user-agent") || "unknown";
  return `ua_${hashString(userAgent)}`;
}

// Simple string hash function
function hashString(str: string): string {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    const char = str.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash; // Convert to 32bit integer
  }
  return Math.abs(hash).toString(36);
}

// Get all response headers including request ID and rate limit info
export function getResponseHeaders(
  origin: string | null,
  requestId: string,
  rateLimitInfo?: { remaining: number; resetInSec: number }
): Record<string, string> {
  const headers: Record<string, string> = {
    ...getCorsHeaders(origin),
    ...securityHeaders,
    "Content-Type": "application/json",
    "X-Request-ID": requestId,
  };
  
  if (rateLimitInfo) {
    headers["X-RateLimit-Remaining"] = rateLimitInfo.remaining.toString();
    headers["X-RateLimit-Reset"] = rateLimitInfo.resetInSec.toString();
  }
  
  return headers;
}

// Create rate limited response (429)
export function createRateLimitResponse(
  origin: string | null,
  requestId: string,
  retryAfterSec: number
): Response {
  const headers = getResponseHeaders(origin, requestId);
  headers["Retry-After"] = retryAfterSec.toString();
  
  return new Response(
    JSON.stringify({ 
      error: "rate_limit_exceeded", 
      message: "Too many requests",
      retryAfter: retryAfterSec,
    }),
    { headers, status: 429 }
  );
}

// Create error response
export function createErrorResponse(
  origin: string | null,
  requestId: string,
  status: number,
  message: string,
  errorCode?: string
): Response {
  return new Response(
    JSON.stringify({ 
      error: errorCode || "internal_error", 
      message 
    }),
    { headers: getResponseHeaders(origin, requestId), status }
  );
}

// Log request with request ID for audit trail
export function logRequest(
  functionName: string,
  requestId: string,
  clientId: string,
  method: string,
  additionalInfo?: Record<string, unknown>
): void {
  console.log(JSON.stringify({
    timestamp: new Date().toISOString(),
    function: functionName,
    requestId,
    clientId,
    method,
    ...additionalInfo,
  }));
}

// Log response with request ID
export function logResponse(
  functionName: string,
  requestId: string,
  status: number,
  additionalInfo?: Record<string, unknown>
): void {
  console.log(JSON.stringify({
    timestamp: new Date().toISOString(),
    function: functionName,
    requestId,
    status,
    ...additionalInfo,
  }));
}

// ============= NEW: Method enforcement =============
export function isMethodAllowed(method: string, allowed: string[] = ["GET", "OPTIONS"]): boolean {
  return allowed.includes(method.toUpperCase());
}

// ============= NEW: In-memory cache utilities =============
interface CacheEntry {
  data: unknown;
  timestamp: number;
}
const cacheStore = new Map<string, CacheEntry>();

export function getCachedData(key: string, ttlMs: number): { data: unknown; hit: boolean } | null {
  const entry = cacheStore.get(key);
  if (!entry) return null;
  const isValid = Date.now() - entry.timestamp < ttlMs;
  if (!isValid) {
    cacheStore.delete(key);
    return null;
  }
  return { data: entry.data, hit: true };
}

export function setCacheData(key: string, data: unknown): void {
  cacheStore.set(key, { data, timestamp: Date.now() });
}

// ============= NEW: Cache-Control headers =============
export function getCacheControlHeaders(): Record<string, string> {
  return {
    "Cache-Control": "public, max-age=0, s-maxage=5, stale-while-revalidate=25",
  };
}

// ============= NEW: Circuit Breaker Pattern =============
interface CircuitBreakerState {
  failures: number;
  lastFailureTime: number;
  state: "closed" | "open" | "half-open";
}

interface CircuitBreakerConfig {
  failureThreshold: number;    // Number of failures before opening circuit
  resetTimeoutMs: number;      // Time to wait before trying again (half-open)
  successThreshold: number;    // Successes needed in half-open to close
}

const DEFAULT_CIRCUIT_CONFIG: CircuitBreakerConfig = {
  failureThreshold: 5,         // Open after 5 consecutive failures
  resetTimeoutMs: 30000,       // Try again after 30 seconds
  successThreshold: 2,         // Close after 2 successes in half-open
};

// In-memory circuit breaker store (per endpoint)
const circuitBreakerStore = new Map<string, CircuitBreakerState>();

function getCircuitState(key: string): CircuitBreakerState {
  if (!circuitBreakerStore.has(key)) {
    circuitBreakerStore.set(key, {
      failures: 0,
      lastFailureTime: 0,
      state: "closed",
    });
  }
  return circuitBreakerStore.get(key)!;
}

function recordSuccess(key: string, config: CircuitBreakerConfig = DEFAULT_CIRCUIT_CONFIG): void {
  const state = getCircuitState(key);
  
  if (state.state === "half-open") {
    state.failures = Math.max(0, state.failures - 1);
    if (state.failures <= config.failureThreshold - config.successThreshold) {
      state.state = "closed";
      state.failures = 0;
      console.log(`[CircuitBreaker] ${key}: CLOSED (recovered)`);
    }
  } else {
    state.failures = 0;
    state.state = "closed";
  }
}

function recordFailure(key: string, config: CircuitBreakerConfig = DEFAULT_CIRCUIT_CONFIG): void {
  const state = getCircuitState(key);
  state.failures++;
  state.lastFailureTime = Date.now();
  
  if (state.failures >= config.failureThreshold) {
    state.state = "open";
    console.log(`[CircuitBreaker] ${key}: OPEN (${state.failures} failures)`);
  }
}

function isCircuitOpen(key: string, config: CircuitBreakerConfig = DEFAULT_CIRCUIT_CONFIG): boolean {
  const state = getCircuitState(key);
  
  if (state.state === "closed") {
    return false;
  }
  
  if (state.state === "open") {
    const timeSinceFailure = Date.now() - state.lastFailureTime;
    if (timeSinceFailure >= config.resetTimeoutMs) {
      state.state = "half-open";
      console.log(`[CircuitBreaker] ${key}: HALF-OPEN (trying recovery)`);
      return false; // Allow one request through
    }
    return true; // Circuit is open, reject request
  }
  
  // half-open: allow request through
  return false;
}

export function getCircuitBreakerStatus(key: string): { state: string; failures: number; canRetryIn?: number } {
  const state = getCircuitState(key);
  const result: { state: string; failures: number; canRetryIn?: number } = {
    state: state.state,
    failures: state.failures,
  };
  
  if (state.state === "open") {
    const timeSinceFailure = Date.now() - state.lastFailureTime;
    const remaining = DEFAULT_CIRCUIT_CONFIG.resetTimeoutMs - timeSinceFailure;
    if (remaining > 0) {
      result.canRetryIn = Math.ceil(remaining / 1000);
    }
  }
  
  return result;
}

// ============= NEW: Retry configuration =============
interface RetryConfig {
  maxRetries: number;
  baseDelayMs: number;
  maxDelayMs: number;
}

const DEFAULT_RETRY_CONFIG: RetryConfig = {
  maxRetries: 3,
  baseDelayMs: 1000, // 1s, 2s, 4s exponential backoff
  maxDelayMs: 4000,
};

// Sleep utility for retry delays
function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

// Calculate exponential backoff delay
function getBackoffDelay(attempt: number, config: RetryConfig): number {
  const delay = config.baseDelayMs * Math.pow(2, attempt);
  return Math.min(delay, config.maxDelayMs);
}

// Check if error is retryable
function isRetryableError(status: number | null, errorMessage?: string): boolean {
  // Retry on 5xx errors, timeouts, and network errors
  if (status !== null && status >= 500 && status < 600) return true;
  if (status === 429) return true; // Rate limited by upstream
  if (errorMessage?.includes("timeout")) return true;
  if (errorMessage?.includes("network")) return true;
  if (errorMessage?.includes("ECONNREFUSED")) return true;
  if (errorMessage?.includes("ECONNRESET")) return true;
  return false;
}

// ============= NEW: Upstream fetch with timeout, retry, and circuit breaker =============
export async function fetchUpstream(
  url: string,
  timeoutMs: number = 10000,
  retryConfig: RetryConfig = DEFAULT_RETRY_CONFIG
): Promise<{ data: unknown; error?: string; attempts: number; circuitOpen?: boolean }> {
  // Extract hostname for circuit breaker key
  const circuitKey = new URL(url).origin;
  
  // Check circuit breaker first
  if (isCircuitOpen(circuitKey)) {
    const status = getCircuitBreakerStatus(circuitKey);
    console.log(`[fetchUpstream] Circuit OPEN for ${circuitKey}, retry in ${status.canRetryIn}s`);
    return { 
      data: null, 
      error: `Circuit breaker open - backend unavailable (retry in ${status.canRetryIn}s)`, 
      attempts: 0,
      circuitOpen: true 
    };
  }
  
  let lastError: string | undefined;
  let attempts = 0;

  for (let attempt = 0; attempt <= retryConfig.maxRetries; attempt++) {
    attempts = attempt + 1;
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeoutMs);

    try {
      const response = await fetch(url, {
        method: "GET",
        headers: { "Accept": "application/json" },
        signal: controller.signal,
      });
      clearTimeout(timeoutId);

      if (!response.ok) {
        const errorMsg = `Backend returned ${response.status}`;
        
        // Check if we should retry
        if (isRetryableError(response.status, undefined) && attempt < retryConfig.maxRetries) {
          const delay = getBackoffDelay(attempt, retryConfig);
          console.log(`[fetchUpstream] Retry ${attempt + 1}/${retryConfig.maxRetries} after ${delay}ms for ${url}`);
          await sleep(delay);
          lastError = errorMsg;
          continue;
        }
        
        // Record failure for circuit breaker
        recordFailure(circuitKey);
        return { data: null, error: errorMsg, attempts };
      }

      // Success - record for circuit breaker
      recordSuccess(circuitKey);
      const data = await response.json();
      return { data, attempts };
    } catch (err) {
      clearTimeout(timeoutId);
      
      let errorMsg: string;
      if (err instanceof DOMException && err.name === "AbortError") {
        errorMsg = "Request timeout";
      } else {
        errorMsg = err instanceof Error ? err.message : "Unknown error";
      }
      
      // Check if we should retry
      if (isRetryableError(null, errorMsg) && attempt < retryConfig.maxRetries) {
        const delay = getBackoffDelay(attempt, retryConfig);
        console.log(`[fetchUpstream] Retry ${attempt + 1}/${retryConfig.maxRetries} after ${delay}ms for ${url} - ${errorMsg}`);
        await sleep(delay);
        lastError = errorMsg;
        continue;
      }
      
      // Record failure for circuit breaker
      recordFailure(circuitKey);
      return { data: null, error: errorMsg, attempts };
    }
  }

  // Max retries exceeded - record failure
  recordFailure(circuitKey);
  return { data: null, error: lastError || "Max retries exceeded", attempts };
}

// ============= NEW: Create circuit breaker response (503) =============
export function createCircuitBreakerResponse(
  origin: string | null,
  requestId: string,
  functionName: string,
  retryAfterSec: number
): Response {
  const headers = getResponseHeaders(origin, requestId);
  headers["Retry-After"] = retryAfterSec.toString();
  
  return new Response(
    JSON.stringify({
      error: `${functionName.replace(/-/g, "_")}_circuit_open`,
      message: "Service temporarily unavailable - backend circuit breaker open",
      retryAfter: retryAfterSec,
    }),
    { headers, status: 503 }
  );
}

// ============= NEW: Parallel fetch for combined endpoints =============
export async function fetchUpstreamParallel(
  urls: string[],
  timeoutMs: number = 10000
): Promise<{ results: unknown[]; errors: string[]; circuitOpen?: boolean }> {
  const promises = urls.map(url => fetchUpstream(url, timeoutMs));
  const responses = await Promise.all(promises);
  
  const results: unknown[] = [];
  const errors: string[] = [];
  let circuitOpen = false;
  
  for (const res of responses) {
    if (res.circuitOpen) {
      circuitOpen = true;
    }
    if (res.error) errors.push(res.error);
    results.push(res.data);
  }
  
  return { results, errors, circuitOpen };
}

// ============= NEW: Create upstream error response (502) =============
export function createUpstreamErrorResponse(
  origin: string | null,
  requestId: string,
  functionName: string,
  errorDetail?: string,
  attempts?: number
): Response {
  const errorCode = `${functionName.replace(/-/g, "_")}_error`;
  return new Response(
    JSON.stringify({
      error: errorCode,
      message: "Upstream backend request failed",
      detail: errorDetail,
      attempts: attempts || 1,
    }),
    { headers: getResponseHeaders(origin, requestId), status: 502 }
  );
}

// ============= NEW: Create method not allowed response (405) =============
export function createMethodNotAllowedResponse(
  origin: string | null,
  requestId: string
): Response {
  return new Response(
    JSON.stringify({
      error: "method_not_allowed",
      message: "Only GET requests are allowed",
    }),
    { headers: getResponseHeaders(origin, requestId), status: 405 }
  );
}
