import { cn } from "@/lib/utils";

// Centralized time window configuration
export const TIME_WINDOW_OPTIONS = ["4h", "8h", "12h", "24h"] as const;
export type TimeWindow = (typeof TIME_WINDOW_OPTIONS)[number];
export type TimeWindowFilter = TimeWindow;
export const DEFAULT_TIME_WINDOW: TimeWindow = "4h";

interface TimeWindowToggleProps {
    value: TimeWindow;
    onChange: (value: TimeWindow) => void;
    className?: string;
}

export function TimeWindowToggle({ value, onChange, className }: TimeWindowToggleProps) {
    return (
        <div className={cn("flex gap-1", className)}>
            {TIME_WINDOW_OPTIONS.map((opt) => (
                <button
                    key={opt}
                    onClick={() => onChange(opt)}
                    className={cn(
                        "px-2 py-1 text-xs rounded-md transition-colors",
                        value === opt
                            ? "bg-primary text-primary-foreground"
                            : "bg-muted/50 text-muted-foreground hover:bg-muted"
                    )}
                >
                    {opt.toUpperCase()}
                </button>
            ))}
        </div>
    );
}

/**
 * Get milliseconds for a time window
 */
export function getTimeWindowMs(window: TimeWindow): number {
    const hours = parseInt(window.replace("h", ""), 10);
    return hours * 60 * 60 * 1000;
}

/**
 * Get human-readable label for a time window
 */
export function getTimeWindowLabel(window: TimeWindow): string {
    const hours = parseInt(window.replace("h", ""), 10);
    return `Last ${hours} hours`;
}

/**
 * Normalize timestamp to milliseconds (handles both seconds and milliseconds input)
 */
function normalizeTimestamp(ts: number): number {
    // Timestamps less than year 2001 in ms are likely in seconds
    return ts < 1_000_000_000_000 ? ts * 1000 : ts;
}

/**
 * Filter items by timestamp based on selected time window
 */
export function filterByTimeWindow<T extends { timestamp: number }>(
    items: T[],
    window: TimeWindow
): T[] {
    const windowMs = getTimeWindowMs(window);
    const cutoff = Date.now() - windowMs;
    return items.filter((item) => normalizeTimestamp(item.timestamp) > cutoff);
}

