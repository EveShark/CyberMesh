/**
 * Security utilities for input validation and sanitization
 * These utilities provide defense-in-depth for the frontend
 */

// List of known disposable email domains (subset - extend as needed)
const DISPOSABLE_EMAIL_DOMAINS = new Set([
  'mailinator.com',
  'guerrillamail.com',
  'tempmail.com',
  '10minutemail.com',
  'throwaway.email',
  'fakeinbox.com',
  'trashmail.com',
  'maildrop.cc',
  'yopmail.com',
  'temp-mail.org',
]);

/**
 * Sanitize user input by removing potentially dangerous characters
 * This is a defense-in-depth measure - backend should also validate
 */
export function sanitizeInput(input: string, maxLength = 1000): string {
  if (typeof input !== 'string') {
    return '';
  }
  
  // Trim and limit length
  let sanitized = input.trim().slice(0, maxLength);
  
  // Remove null bytes and control characters (except newlines and tabs)
  sanitized = sanitized.replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, '');
  
  return sanitized;
}

/**
 * Escape HTML special characters to prevent XSS
 * Use this when displaying user-generated content
 */
export function escapeHtml(text: string): string {
  if (typeof text !== 'string') {
    return '';
  }
  
  const htmlEntities: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#039;',
    '/': '&#x2F;',
    '`': '&#x60;',
    '=': '&#x3D;',
  };
  
  return text.replace(/[&<>"'`=/]/g, (char) => htmlEntities[char] || char);
}

/**
 * Validate email format and optionally check for disposable domains
 */
export function isValidEmail(
  email: string, 
  options: { blockDisposable?: boolean } = {}
): { valid: boolean; error?: string } {
  if (typeof email !== 'string') {
    return { valid: false, error: 'Email must be a string' };
  }
  
  const trimmed = email.trim().toLowerCase();
  
  // Check length
  if (trimmed.length === 0) {
    return { valid: false, error: 'Email is required' };
  }
  
  if (trimmed.length > 254) {
    return { valid: false, error: 'Email is too long' };
  }
  
  // RFC 5322 compliant email regex
  const emailRegex = /^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;
  
  if (!emailRegex.test(trimmed)) {
    return { valid: false, error: 'Invalid email format' };
  }
  
  // Check for disposable email domains
  if (options.blockDisposable) {
    const domain = trimmed.split('@')[1];
    if (domain && DISPOSABLE_EMAIL_DOMAINS.has(domain)) {
      return { valid: false, error: 'Disposable email addresses are not allowed' };
    }
  }
  
  return { valid: true };
}

/**
 * Validate and sanitize a URL
 * Returns null if invalid
 */
export function sanitizeUrl(url: string): string | null {
  if (typeof url !== 'string') {
    return null;
  }
  
  try {
    const parsed = new URL(url);
    
    // Only allow http and https protocols
    if (!['http:', 'https:'].includes(parsed.protocol)) {
      return null;
    }
    
    // Block javascript: and data: URIs that might slip through
    if (parsed.href.toLowerCase().includes('javascript:') || 
        parsed.href.toLowerCase().includes('data:')) {
      return null;
    }
    
    return parsed.href;
  } catch {
    return null;
  }
}

/**
 * Check if a string contains potential script injection
 */
export function containsScriptInjection(text: string): boolean {
  if (typeof text !== 'string') {
    return false;
  }
  
  const lowerText = text.toLowerCase();
  
  // Check for common XSS patterns
  const dangerousPatterns = [
    /<script/i,
    /javascript:/i,
    /on\w+\s*=/i, // onclick=, onerror=, etc.
    /data:/i,
    /<iframe/i,
    /<object/i,
    /<embed/i,
    /<svg.*onload/i,
    /expression\s*\(/i, // CSS expression()
    /url\s*\(\s*['"]?\s*javascript/i,
  ];
  
  return dangerousPatterns.some(pattern => pattern.test(lowerText));
}

/**
 * Rate limit check helper for client-side rate limiting
 * Uses localStorage to track request timestamps
 */
export function checkClientRateLimit(
  key: string, 
  maxRequests: number, 
  windowMs: number
): { allowed: boolean; retryAfterMs?: number } {
  const storageKey = `rate_limit_${key}`;
  const now = Date.now();
  
  try {
    const stored = localStorage.getItem(storageKey);
    let timestamps: number[] = stored ? JSON.parse(stored) : [];
    
    // Remove expired timestamps
    timestamps = timestamps.filter(ts => now - ts < windowMs);
    
    if (timestamps.length >= maxRequests) {
      const oldestTimestamp = timestamps[0];
      const retryAfterMs = windowMs - (now - oldestTimestamp);
      return { allowed: false, retryAfterMs };
    }
    
    // Add current timestamp
    timestamps.push(now);
    localStorage.setItem(storageKey, JSON.stringify(timestamps));
    
    return { allowed: true };
  } catch {
    // If localStorage fails, allow the request
    return { allowed: true };
  }
}

/**
 * Generate a simple request ID for logging/tracking
 */
export function generateRequestId(): string {
  return `${Date.now()}-${Math.random().toString(36).substring(2, 9)}`;
}

/**
 * Sanitize error messages before displaying to users
 * Removes potentially sensitive information
 */
export function sanitizeErrorMessage(error: unknown): string {
  const defaultMessage = 'An unexpected error occurred. Please try again.';
  
  if (!error) {
    return defaultMessage;
  }
  
  if (error instanceof Error) {
    const message = error.message;
    
    // Don't expose internal error details
    const sensitivePatterns = [
      /sql/i,
      /database/i,
      /query/i,
      /connection/i,
      /timeout/i,
      /internal server/i,
      /stack trace/i,
      /at\s+\w+\s+\(/i, // Stack trace lines
      /node_modules/i,
      /\.ts:|\.js:/i, // File paths
    ];
    
    if (sensitivePatterns.some(pattern => pattern.test(message))) {
      return defaultMessage;
    }
    
    // Return safe error messages
    if (message.length < 200 && !message.includes('\n')) {
      return message;
    }
  }
  
  return defaultMessage;
}
