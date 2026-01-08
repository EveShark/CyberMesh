/**
 * Main lib exports - Re-exports from organized modules
 * 
 * New structure:
 * - src/lib/api/ - API client, mail, validation, rate limiting, performance
 * - src/lib/utils/ - cn, json transforms, security utilities
 */

// API exports
export * from './api';

// Utils exports  
export * from './utils';
