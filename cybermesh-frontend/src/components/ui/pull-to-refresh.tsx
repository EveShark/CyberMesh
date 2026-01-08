import { RefreshCw } from "lucide-react";
import { cn } from "@/lib/utils";

interface PullToRefreshIndicatorProps {
  pullDistance: number;
  isRefreshing: boolean;
  progress: number;
  threshold?: number;
}

export const PullToRefreshIndicator = ({
  pullDistance,
  isRefreshing,
  progress,
  threshold = 80,
}: PullToRefreshIndicatorProps) => {
  if (pullDistance === 0 && !isRefreshing) return null;

  return (
    <div 
      className="absolute left-0 right-0 flex items-center justify-center z-30 pointer-events-none"
      style={{ 
        top: Math.min(pullDistance - 40, threshold - 40),
        opacity: Math.min(progress * 1.5, 1),
      }}
    >
      <div 
        className={cn(
          "flex items-center justify-center w-10 h-10 rounded-full bg-background/90 border border-border shadow-lg backdrop-blur-sm transition-transform",
          isRefreshing && "animate-spin"
        )}
        style={{
          transform: isRefreshing ? undefined : `rotate(${progress * 360}deg)`,
        }}
      >
        <RefreshCw className={cn(
          "h-5 w-5 transition-colors",
          progress >= 1 ? "text-frost" : "text-muted-foreground"
        )} />
      </div>
    </div>
  );
};
