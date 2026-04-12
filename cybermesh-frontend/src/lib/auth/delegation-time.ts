export function formatDelegationCountdown(expiresAt?: string, nowMs = Date.now()): string {
  if (!expiresAt) {
    return "unknown remaining time";
  }
  const expiresMs = new Date(expiresAt).getTime();
  if (Number.isNaN(expiresMs)) {
    return "unknown remaining time";
  }
  const diffMs = expiresMs - nowMs;
  if (diffMs <= 0) {
    return "expired";
  }

  const totalMinutes = Math.floor(diffMs / 60_000);
  const days = Math.floor(totalMinutes / 1_440);
  const hours = Math.floor((totalMinutes % 1_440) / 60);
  const minutes = totalMinutes % 60;

  if (days > 0) {
    return `${days}d ${hours}h remaining`;
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m remaining`;
  }
  return `${Math.max(minutes, 1)}m remaining`;
}
