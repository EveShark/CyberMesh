/**
 * API Types - Types for API communication
 * 
 * This file consolidates all API-related types including:
 * - Generic API response wrappers
 * - Backend API types (Go backend)
 * - Transformation utilities
 */

// Re-export all backend API types
export * from './backend-api';

// Generic API response wrapper
export interface ApiResponse<T> {
  data: T;
  updatedAt: string;
}

// API error structure
export interface ApiErrorResponse {
  error: string;
  message: string;
  details?: unknown;
}

// Pagination types
export interface PaginatedRequest {
  page?: number;
  limit?: number;
  offset?: number;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  limit: number;
  hasMore: boolean;
}
