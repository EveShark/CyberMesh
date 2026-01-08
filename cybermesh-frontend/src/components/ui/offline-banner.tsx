import { WifiOff, Wifi } from "lucide-react";
import { useOnlineStatus } from "@/hooks/common/use-online-status";
import { cn } from "@/lib/utils";

export const OfflineBanner = () => {
  const { isOnline, wasOffline } = useOnlineStatus();

  if (isOnline && !wasOffline) return null;

  return (
    <div
      className={cn(
        "fixed top-14 left-0 right-0 z-40 flex items-center justify-center gap-2 py-2 px-4 text-sm font-medium transition-all duration-300",
        !isOnline 
          ? "bg-destructive/90 text-destructive-foreground backdrop-blur-sm" 
          : "bg-status-healthy/90 text-primary-foreground backdrop-blur-sm"
      )}
    >
      {!isOnline ? (
        <>
          <WifiOff className="h-4 w-4" />
          <span>You're offline. Some features may be unavailable.</span>
        </>
      ) : (
        <>
          <Wifi className="h-4 w-4" />
          <span>Connection restored</span>
        </>
      )}
    </div>
  );
};
