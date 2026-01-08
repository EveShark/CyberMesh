import { useRef, useCallback, useState } from "react";

interface AdaptivePollingOptions {
  baseInterval: number;
  maxInterval?: number;
  unchangedThreshold?: number;
  backoffMultiplier?: number;
}

interface AdaptivePollingResult {
  currentInterval: number;
  unchangedCount: number;
  onDataReceived: (dataHash: string) => void;
  resetInterval: () => void;
}

/**
 * Hook for adaptive polling that increases interval when data hasn't changed.
 * Reduces API load when data is stable.
 */
export function useAdaptivePolling(options: AdaptivePollingOptions): AdaptivePollingResult {
  const {
    baseInterval,
    maxInterval = 60000, // 60s max
    unchangedThreshold = 3, // Increase after 3 unchanged polls
    backoffMultiplier = 1.5,
  } = options;

  const [currentInterval, setCurrentInterval] = useState(baseInterval);
  const [unchangedCount, setUnchangedCount] = useState(0);
  const lastDataHashRef = useRef<string | null>(null);

  const onDataReceived = useCallback(
    (dataHash: string) => {
      if (lastDataHashRef.current === dataHash) {
        // Data unchanged
        setUnchangedCount((prev) => {
          const newCount = prev + 1;
          if (newCount >= unchangedThreshold) {
            setCurrentInterval((prevInterval) => {
              const newInterval = Math.min(
                prevInterval * backoffMultiplier,
                maxInterval
              );
              if (newInterval !== prevInterval) {
                console.info(
                  `[Adaptive Polling] Increasing interval to ${Math.round(newInterval / 1000)}s after ${newCount} unchanged polls`
                );
              }
              return newInterval;
            });
          }
          return newCount;
        });
      } else {
        // Data changed - reset to base interval
        if (lastDataHashRef.current !== null && currentInterval !== baseInterval) {
          console.info(
            `[Adaptive Polling] Data changed, resetting interval to ${baseInterval / 1000}s`
          );
        }
        lastDataHashRef.current = dataHash;
        setUnchangedCount(0);
        setCurrentInterval(baseInterval);
      }
    },
    [baseInterval, maxInterval, unchangedThreshold, backoffMultiplier, currentInterval]
  );

  const resetInterval = useCallback(() => {
    setCurrentInterval(baseInterval);
    setUnchangedCount(0);
    lastDataHashRef.current = null;
  }, [baseInterval]);

  return {
    currentInterval,
    unchangedCount,
    onDataReceived,
    resetInterval,
  };
}

/**
 * Simple hash function for comparing data equality
 */
export function hashData(data: unknown): string {
  try {
    return JSON.stringify(data);
  } catch {
    return String(Date.now());
  }
}
