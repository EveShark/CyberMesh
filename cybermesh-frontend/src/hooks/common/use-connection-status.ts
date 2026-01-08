import { useState, useEffect, useCallback, useRef } from "react";

interface SyncStatus {
  isOnline: boolean;
  lastSyncTime: Date | null;
  lastSyncTimeFormatted: string;
  syncError: boolean;
}

/**
 * Hook that tracks connection status and last successful API sync time.
 * Integrates with React Query to detect successful data fetches.
 */
export const useConnectionStatus = () => {
  const [status, setStatus] = useState<SyncStatus>({
    isOnline: typeof navigator !== "undefined" ? navigator.onLine : true,
    lastSyncTime: null,
    lastSyncTimeFormatted: "Never",
    syncError: false,
  });
  
  const intervalRef = useRef<NodeJS.Timeout | null>(null);

  // Format time relative to now
  const formatRelativeTime = useCallback((date: Date): string => {
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffSecs = Math.floor(diffMs / 1000);
    
    if (diffSecs < 5) return "Just now";
    if (diffSecs < 60) return `${diffSecs}s ago`;
    
    const diffMins = Math.floor(diffSecs / 60);
    if (diffMins < 60) return `${diffMins}m ago`;
    
    const diffHours = Math.floor(diffMins / 60);
    return `${diffHours}h ago`;
  }, []);

  // Update the formatted time every 10 seconds
  useEffect(() => {
    intervalRef.current = setInterval(() => {
      if (status.lastSyncTime) {
        setStatus(prev => ({
          ...prev,
          lastSyncTimeFormatted: formatRelativeTime(prev.lastSyncTime!),
        }));
      }
    }, 10000);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [status.lastSyncTime, formatRelativeTime]);

  // Handle online/offline events
  useEffect(() => {
    const handleOnline = () => {
      setStatus(prev => ({ ...prev, isOnline: true }));
    };

    const handleOffline = () => {
      setStatus(prev => ({ ...prev, isOnline: false }));
    };

    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);

    return () => {
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
    };
  }, []);

  // Function to record successful sync
  const recordSuccessfulSync = useCallback(() => {
    const now = new Date();
    setStatus(prev => ({
      ...prev,
      lastSyncTime: now,
      lastSyncTimeFormatted: formatRelativeTime(now),
      syncError: false,
    }));
  }, [formatRelativeTime]);

  // Function to record sync error
  const recordSyncError = useCallback(() => {
    setStatus(prev => ({
      ...prev,
      syncError: true,
    }));
  }, []);

  return {
    ...status,
    recordSuccessfulSync,
    recordSyncError,
  };
};

export default useConnectionStatus;
