import { QueryClient, QueryCache, MutationCache } from "@tanstack/react-query";
import { toast } from "sonner";
import { isDemoMode } from "@/config/demo-mode";

// Store refetch callbacks for retry buttons
const refetchCallbacks = new Map<string, () => void>();

export const registerRefetch = (key: string, refetch: () => void) => {
  refetchCallbacks.set(key, refetch);
};

export const unregisterRefetch = (key: string) => {
  refetchCallbacks.delete(key);
};

export const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error, query) => {
      // Skip error toasts in demo mode - should never happen but just in case
      if (isDemoMode()) return;
      
      const queryKey = Array.isArray(query.queryKey) ? query.queryKey[0] : query.queryKey;
      const keyStr = typeof queryKey === "string" ? queryKey : JSON.stringify(queryKey);
      
      // Show toast with retry button only after all retries exhausted
      if (query.state.dataUpdateCount > 0 || query.state.fetchFailureCount >= 3) {
        toast.error("Failed to fetch data", {
          description: error.message,
          action: {
            label: "Retry",
            onClick: () => {
              const refetch = refetchCallbacks.get(keyStr);
              if (refetch) {
                refetch();
              } else {
                // Fallback: invalidate the query
                queryClient.invalidateQueries({ queryKey: query.queryKey });
              }
            },
          },
          duration: 8000,
        });
      }
    },
  }),
  mutationCache: new MutationCache({
    onError: (error, _variables, _context, mutation) => {
      // Skip error toasts in demo mode
      if (isDemoMode()) return;
      
      toast.error("Operation failed", {
        description: error.message,
        action: mutation.options.onError
          ? undefined
          : {
              label: "Retry",
              onClick: () => mutation.execute(mutation.state.variables),
            },
        duration: 8000,
      });
    },
  }),
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        // Don't retry on 4xx errors (client errors)
        if (error instanceof Error) {
          const message = error.message.toLowerCase();
          if (message.includes("400") || message.includes("401") || 
              message.includes("403") || message.includes("404")) {
            return false;
          }
        }
        
        // Max 3 retries
        if (failureCount >= 3) {
          return false;
        }
        
        return true;
      },
      // Exponential backoff: 1s, 2s, 4s (capped at 30s)
      retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, 30000),
      staleTime: 5000,
      // Garbage collect unused queries after 5 minutes
      gcTime: 1000 * 60 * 5,
      // Cancel requests when component unmounts (enabled by default in v5)
      networkMode: "online",
      // Disable automatic refetch on window focus - we handle visibility-based polling manually
      refetchOnWindowFocus: false,
      // Refetch on reconnect for offline recovery
      refetchOnReconnect: true,
    },
    mutations: {
      // Cancel mutations on unmount
      networkMode: "online",
    },
  },
});
