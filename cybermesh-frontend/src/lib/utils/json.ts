/**
 * JSON Transformation Utilities
 * 
 * Converts between Go's snake_case JSON conventions and TypeScript's camelCase.
 * Handles nested objects, arrays, and preserves null/undefined values.
 */

type JsonValue = string | number | boolean | null | undefined | JsonObject | JsonArray;
type JsonObject = { [key: string]: JsonValue };
type JsonArray = JsonValue[];

/**
 * Convert a snake_case string to camelCase
 * 
 * @example
 * snakeToCamelCase('user_profile_data') // 'userProfileData'
 * snakeToCamelCase('api_response_v2') // 'apiResponseV2'
 */
export function snakeToCamelCase(str: string): string {
  return str.replace(/_([a-z0-9])/g, (_, char) => char.toUpperCase());
}

/**
 * Convert a camelCase string to snake_case
 * 
 * @example
 * camelToSnakeCase('userProfileData') // 'user_profile_data'
 * camelToSnakeCase('apiResponseV2') // 'api_response_v2'
 */
export function camelToSnakeCase(str: string): string {
  return str.replace(/[A-Z]/g, letter => `_${letter.toLowerCase()}`);
}

/**
 * Recursively transform all object keys from snake_case to camelCase
 * Handles nested objects and arrays.
 * 
 * @example
 * snakeToCamel({ user_name: 'john', profile_data: { created_at: '2024-01-01' } })
 * // { userName: 'john', profileData: { createdAt: '2024-01-01' } }
 */
export function snakeToCamel<T = unknown>(obj: unknown): T {
  // Handle null/undefined
  if (obj === null || obj === undefined) {
    return obj as T;
  }

  // Handle arrays
  if (Array.isArray(obj)) {
    return obj.map(item => snakeToCamel(item)) as T;
  }

  // Handle Date objects (preserve as-is)
  if (obj instanceof Date) {
    return obj as T;
  }

  // Handle plain objects
  if (typeof obj === 'object') {
    const result: JsonObject = {};
    
    for (const [key, value] of Object.entries(obj as JsonObject)) {
      const camelKey = snakeToCamelCase(key);
      result[camelKey] = snakeToCamel(value);
    }
    
    return result as T;
  }

  // Primitives pass through unchanged
  return obj as T;
}

/**
 * Recursively transform all object keys from camelCase to snake_case
 * Handles nested objects and arrays.
 * 
 * @example
 * camelToSnake({ userName: 'john', profileData: { createdAt: '2024-01-01' } })
 * // { user_name: 'john', profile_data: { created_at: '2024-01-01' } }
 */
export function camelToSnake<T = unknown>(obj: unknown): T {
  // Handle null/undefined
  if (obj === null || obj === undefined) {
    return obj as T;
  }

  // Handle arrays
  if (Array.isArray(obj)) {
    return obj.map(item => camelToSnake(item)) as T;
  }

  // Handle Date objects (preserve as-is)
  if (obj instanceof Date) {
    return obj as T;
  }

  // Handle plain objects
  if (typeof obj === 'object') {
    const result: JsonObject = {};
    
    for (const [key, value] of Object.entries(obj as JsonObject)) {
      const snakeKey = camelToSnakeCase(key);
      result[snakeKey] = camelToSnake(value);
    }
    
    return result as T;
  }

  // Primitives pass through unchanged
  return obj as T;
}

/**
 * Transform API response from Go backend (snake_case) to TypeScript (camelCase)
 * This is the primary function to use when receiving data from the Go API.
 */
export function transformApiResponse<T>(response: unknown): T {
  return snakeToCamel<T>(response);
}

/**
 * Transform request body from TypeScript (camelCase) to Go backend (snake_case)
 * This is the primary function to use when sending data to the Go API.
 */
export function transformApiRequest<T>(body: unknown): T {
  return camelToSnake<T>(body);
}

/**
 * Check if a string is in snake_case format
 */
export function isSnakeCase(str: string): boolean {
  return /^[a-z][a-z0-9]*(_[a-z0-9]+)*$/.test(str);
}

/**
 * Check if a string is in camelCase format
 */
export function isCamelCase(str: string): boolean {
  return /^[a-z][a-zA-Z0-9]*$/.test(str);
}

export default {
  snakeToCamel,
  camelToSnake,
  snakeToCamelCase,
  camelToSnakeCase,
  transformApiResponse,
  transformApiRequest,
  isSnakeCase,
  isCamelCase,
};
