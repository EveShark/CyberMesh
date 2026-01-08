/**
 * Adapter Utility Functions
 * Common formatting and transformation helpers
 */

/**
 * Format seconds to human readable string (e.g., "0.8s", "2.5m")
 */
export function formatSeconds(seconds?: number | null): string | null {
  if (seconds === undefined || seconds === null || isNaN(seconds)) return null;
  if (seconds < 60) return seconds.toFixed(1) + "s";
  if (seconds < 3600) return (seconds / 60).toFixed(1) + "m";
  return (seconds / 3600).toFixed(1) + "h";
}

/**
 * Format bytes to human readable string (e.g., "1.2 KB", "3.5 MB")
 */
export function formatBytes(bytes?: number | null): string | null {
  if (bytes === undefined || bytes === null || isNaN(bytes)) return null;
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  return (bytes / (1024 * 1024 * 1024)).toFixed(1) + " GB";
}

/**
 * Format number with thousands separator
 */
export function formatNumber(num?: number | null): string | null {
  if (num === undefined || num === null || isNaN(num)) return null;
  return num.toLocaleString();
}

/**
 * Format percentage
 */
export function formatPercent(ratio?: number | null, decimals: number = 1): string | null {
  if (ratio === undefined || ratio === null || isNaN(ratio)) return null;
  return (ratio * 100).toFixed(decimals) + "%";
}

/**
 * Format rate per second to human readable
 */
export function formatRate(bps?: number | null): string | null {
  if (bps === undefined || bps === null || isNaN(bps)) return null;
  if (bps < 1024) return bps.toFixed(1) + " B/s";
  if (bps < 1024 * 1024) return (bps / 1024).toFixed(1) + " KB/s";
  return (bps / (1024 * 1024)).toFixed(1) + " MB/s";
}

/**
 * Format milliseconds to human readable string
 */
export function formatMs(ms?: number | null): string | null {
  if (ms === undefined || ms === null || isNaN(ms)) return null;
  if (ms < 1000) return ms.toFixed(0) + "ms";
  return (ms / 1000).toFixed(2) + "s";
}

/**
 * Truncate hash for display
 */
export function truncateHash(hash?: string | null, length: number = 8): string | null {
  if (!hash) return null;
  if (hash.length <= length * 2 + 3) return hash;
  return hash.slice(0, length) + "..." + hash.slice(-length);
}

/**
 * Get severity from score
 */
export function getSeverityFromScore(score: number): "Critical" | "High" | "Medium" | "Low" {
  if (score >= 0.8) return "Critical";
  if (score >= 0.6) return "High";
  if (score >= 0.4) return "Medium";
  return "Low";
}

/**
 * Format timestamp to time string
 */
export function formatTime(timestamp?: number | null): string | null {
  if (timestamp === undefined || timestamp === null) return null;
  const date = new Date(timestamp * 1000);
  return date.toLocaleTimeString();
}
