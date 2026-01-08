/**
 * API Module - Centralized exports for API-related functionality
 */

// Client and core API
export { apiClient, ApiError, createQueryFn } from './client';
export type {
  ApiResponse,
  DashboardOverviewResponse,
  AIEngineResponse,
  BlockchainResponse,
  NetworkResponse,
  ThreatsResponse,
  SystemHealthResponse,
} from './client';

// Mail API
export { mailApi, sendContactForm, sendEmailCapture, MailApiError, contactFormSchema, emailCaptureSchema } from './mail';
export type { ContactFormData } from './mail';

// Query client
export { queryClient, registerRefetch, unregisterRefetch } from './query-client';

// Rate limiter
export { rateLimitedFetch, createRateLimitedFetcher } from './rate-limiter';
export type { RateLimitedFetchOptions, RateLimitedFetchResult } from './rate-limiter';

// Validation schemas
export {
  validateApiResponse,
  validateWithFallback,
  blockchainDataSchema,
  blockchainResponseSchema,
  threatsDataSchema,
  threatsResponseSchema,
  systemHealthDataSchema,
  systemHealthResponseSchema,
} from './validation';
export type { ValidationResult } from './validation';

// Performance tracking
export { recordApiPerformance, getEndpointMetrics, getAllMetrics, logMetricsSummary, clearMetrics } from './performance';
