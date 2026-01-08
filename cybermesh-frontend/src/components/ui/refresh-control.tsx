import { RefreshCw } from "lucide-react";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useQueryClient } from "@tanstack/react-query";
import { useState, useCallback } from "react";
import { isDemoMode } from "@/config/demo-mode";

interface RefreshControlProps {
  lastSyncTimeFormatted: string;
}

export const RefreshControl = ({ lastSyncTimeFormatted }: RefreshControlProps) => {
  const queryClient = useQueryClient();
  const [isRefreshing, setIsRefreshing] = useState(false);
  const inDemoMode = isDemoMode();

  const handleRefresh = useCallback(async () => {
    if (isRefreshing) return;
    
    setIsRefreshing(true);
    
    // Invalidate all queries to trigger a refetch
    await queryClient.invalidateQueries();
    
    // Brief delay to show the spinning animation
    setTimeout(() => {
      setIsRefreshing(false);
    }, 600);
  }, [queryClient, isRefreshing]);

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            onClick={handleRefresh}
            disabled={isRefreshing}
            className={cn(
              "flex items-center gap-2 px-2.5 py-1.5 rounded-full border backdrop-blur-sm transition-all",
              "bg-muted/50 border-border/50 hover:bg-muted hover:border-border",
              "disabled:opacity-50 disabled:cursor-not-allowed"
            )}
          >
            <RefreshCw 
              className={cn(
                "h-3.5 w-3.5 text-muted-foreground transition-transform",
                isRefreshing && "animate-spin"
              )} 
            />
            <span className="hidden md:inline text-xs text-muted-foreground">
              {lastSyncTimeFormatted}
            </span>
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom" className="text-xs">
          <div className="space-y-1">
            <div className="font-medium">
              {isRefreshing ? "Refreshing..." : "Refresh data"}
            </div>
            {!inDemoMode && (
              <div className="text-muted-foreground">
                Last update: {lastSyncTimeFormatted}
              </div>
            )}
            {inDemoMode && (
              <div className="text-muted-foreground">
                Using demo data
              </div>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
};

export default RefreshControl;
