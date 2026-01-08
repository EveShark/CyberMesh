import { Wifi, WifiOff, Clock, AlertCircle, FlaskConical } from "lucide-react";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { isDemoMode } from "@/config/demo-mode";

interface ConnectionStatusProps {
  isOnline: boolean;
  lastSyncTimeFormatted: string;
  syncError: boolean;
  isFetching: boolean;
}

export const ConnectionStatus = ({
  isOnline,
  lastSyncTimeFormatted,
  syncError,
  isFetching,
}: ConnectionStatusProps) => {
  // Demo mode - show distinct demo indicator
  const inDemoMode = isDemoMode();
  
  // Determine status and styling
  const getStatusConfig = () => {
    // Demo mode takes priority
    if (inDemoMode) {
      return {
        icon: FlaskConical,
        label: "Demo",
        color: "text-violet-400",
        bgColor: "bg-violet-500/10",
        borderColor: "border-violet-500/20",
        pulseColor: "bg-violet-500",
        showPulse: false,
      };
    }
    
    if (!isOnline) {
      return {
        icon: WifiOff,
        label: "Offline",
        color: "text-destructive",
        bgColor: "bg-destructive/10",
        borderColor: "border-destructive/20",
        pulseColor: "bg-destructive",
        showPulse: true,
      };
    }
    
    if (syncError) {
      return {
        icon: AlertCircle,
        label: "Sync Error",
        color: "text-yellow-500",
        bgColor: "bg-yellow-500/10",
        borderColor: "border-yellow-500/20",
        pulseColor: "bg-yellow-500",
        showPulse: false,
      };
    }
    
    if (isFetching) {
      return {
        icon: Clock,
        label: "Syncing",
        color: "text-frost",
        bgColor: "bg-frost/10",
        borderColor: "border-frost/20",
        pulseColor: "bg-frost",
        showPulse: false,
      };
    }
    
    return {
      icon: Wifi,
      label: "Connected",
      color: "text-status-healthy",
      bgColor: "bg-status-healthy/10",
      borderColor: "border-status-healthy/20",
      pulseColor: "bg-status-healthy",
      showPulse: true,
    };
  };

  const config = getStatusConfig();
  const Icon = config.icon;

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            className={cn(
              "flex items-center gap-2 px-2.5 py-1.5 rounded-full border backdrop-blur-sm transition-all cursor-default",
              config.bgColor,
              config.borderColor
            )}
          >
            {/* Status indicator dot */}
            <span className="relative flex h-2 w-2">
              {config.showPulse && (
                <span
                  className={cn(
                    "animate-ping absolute inline-flex h-full w-full rounded-full opacity-75",
                    config.pulseColor
                  )}
                />
              )}
              <span
                className={cn(
                  "relative inline-flex rounded-full h-2 w-2",
                  config.pulseColor
                )}
              />
            </span>
            
            {/* Icon and label - always visible, compact on mobile */}
            <div className="flex items-center gap-1">
              <Icon className={cn("h-3 w-3", config.color)} />
              <span className={cn("text-[10px] sm:text-xs font-medium", config.color)}>
                {config.label}
              </span>
            </div>
          </div>
        </TooltipTrigger>
        <TooltipContent side="bottom" className="text-xs">
          <div className="space-y-1">
            <div className="font-medium">{config.label}</div>
            {inDemoMode ? (
              <div className="text-muted-foreground">
                Using mock data
              </div>
            ) : (
              <div className="text-muted-foreground">
                Last sync: {lastSyncTimeFormatted}
              </div>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
};

export default ConnectionStatus;
