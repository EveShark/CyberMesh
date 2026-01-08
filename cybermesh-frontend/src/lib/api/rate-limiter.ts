/**
 * Rate-Limited Fetch Wrapper
 * 
 * Provides retry/backoff logic for handling rate limits (429) and service unavailable (503) errors.
 * Designed to work seamlessly with both Edge Functions and Go backend.
 */

export interface RateLimitedFetchOptions extends RequestInit {
  maxRetries?: number;
  baseDelay?: number;
  maxDelay?: number;
  retryStatusCodes?: number[];
}

export interface RateLimitedFetchResult<T> {
  data: T;
  retryCount: number;
  totalDuration: number;
  response?: Response;
}

/**
 * Default retry configuration
 */
const DEFAULT_CONFIG = {
  maxRetries: 3,
  baseDelay: 1000, // 1 second
  maxDelay: 30000, // 30 seconds max
  retryStatusCodes: [429, 503, 502, 504], // Rate limit, Service Unavailable, Bad Gateway, Gateway Timeout
};

/**
 * Calculate exponential backoff delay with jitter
 */
function calculateBackoff(attempt: number, baseDelay: number, maxDelay: number): number {
  // Exponential backoff: baseDelay * 2^attempt
  const exponentialDelay = baseDelay * Math.pow(2, attempt);

  // Add jitter (±25% randomization to prevent thundering herd)
  const jitter = exponentialDelay * 0.25 * (Math.random() * 2 - 1);

  // Cap at maxDelay
  return Math.min(exponentialDelay + jitter, maxDelay);
}

/**
 * Extract retry-after header value (in milliseconds)
 */
function getRetryAfter(response: Response): number | null {
  const retryAfter = response.headers.get('Retry-After');

  if (!retryAfter) return null;

  // Check if it's a number (seconds)
  const seconds = parseInt(retryAfter, 10);
  if (!isNaN(seconds)) {
    return seconds * 1000;
  }

  // Check if it's a date
  const date = new Date(retryAfter);
  if (!isNaN(date.getTime())) {
    return Math.max(0, date.getTime() - Date.now());
  }

  return null;
}

/**
 * Sleep for a specified duration
 */
function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Rate-limited fetch with exponential backoff and retry logic
 * 
 * @param url - The URL to fetch
 * @param options - Fetch options with retry configuration
 * @returns Promise with the response data and retry metadata
 * 
 * @example
 * ```typescript
 * const result = await rateLimitedFetch<ApiResponse>('https://api.example.com/data', {
 *   method: 'GET',
 *   maxRetries: 5,
 *   baseDelay: 2000,
 * });
 * console.log(`Success after ${result.retryCount} retries`);
 * ```
 */
export async function rateLimitedFetch<T>(
  url: string,
  options: RateLimitedFetchOptions = {}
): Promise<RateLimitedFetchResult<T>> {
  const {
    maxRetries = DEFAULT_CONFIG.maxRetries,
    baseDelay = DEFAULT_CONFIG.baseDelay,
    maxDelay = DEFAULT_CONFIG.maxDelay,
    retryStatusCodes = DEFAULT_CONFIG.retryStatusCodes,
    ...fetchOptions
  } = options;

  const startTime = performance.now();
  let lastError: Error | null = null;

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      const response = await fetch(url, fetchOptions);

      // Check if we should retry this status code
      if (retryStatusCodes.includes(response.status)) {
        if (attempt < maxRetries) {
          // Get delay from Retry-After header or calculate exponential backoff
          const retryAfterMs = getRetryAfter(response);
          const delay = retryAfterMs ?? calculateBackoff(attempt, baseDelay, maxDelay);

          console.warn(
            `[RateLimitedFetch] ${response.status} on attempt ${attempt + 1}/${maxRetries + 1}. ` +
            `Retrying in ${Math.round(delay)}ms...`
          );

          await sleep(delay);
          continue;
        }

        // Max retries reached for rate limit
        throw new Error(
          `Rate limit exceeded after ${maxRetries + 1} attempts. ` +
          `Last status: ${response.status} ${response.statusText}`
        );
      }

      // Non-retryable error status
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      // Success!
      const data = await response.json() as T;
      const totalDuration = performance.now() - startTime;

      return {
        data,
        retryCount: attempt,
        totalDuration,
        response,
      };
    } catch (error) {
      lastError = error instanceof Error ? error : new Error(String(error));

      // Network errors - retry if we have attempts left
      if (
        error instanceof TypeError &&
        error.message.includes('fetch') &&
        attempt < maxRetries
      ) {
        const delay = calculateBackoff(attempt, baseDelay, maxDelay);
        console.warn(
          `[RateLimitedFetch] Network error on attempt ${attempt + 1}/${maxRetries + 1}. ` +
          `Retrying in ${Math.round(delay)}ms...`
        );
        await sleep(delay);
        continue;
      }

      // If it's not a network error or we're out of retries, throw
      if (attempt >= maxRetries) {
        throw lastError;
      }
    }
  }

  // Should never reach here, but TypeScript needs this
  throw lastError ?? new Error('Unknown error in rateLimitedFetch');
}

/**
 * Create a rate-limited fetcher function for use with data fetching libraries
 * 
 * @param config - Default configuration for all requests
 * @returns A fetcher function compatible with SWR/React Query
 * 
 * @example
 * ```typescript
 * const fetcher = createRateLimitedFetcher({ maxRetries: 5 });
 * const { data } = useSWR('/api/data', fetcher);
 * ```
 */
export function createRateLimitedFetcher(
  config: Partial<RateLimitedFetchOptions> = {}
) {
  return async <T>(url: string): Promise<T> => {
    const result = await rateLimitedFetch<T>(url, config);
    return result.data;
  };
}

export default rateLimitedFetch;
