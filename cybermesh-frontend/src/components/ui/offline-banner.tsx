import { WifiOff, Wifi } from "lucide-react";
import { useOnlineStatus } from "@/hooks/common/use-online-status";
import { cn } from "@/lib/utils";

export const OfflineBanner = () => {
  const { isOnline, wasOffline } = useOnlineStatus();

  if (isOnline && !wasOffline) return null;

  return (
    <div
      className={cn(
        "fixed top-14 left-0 right-0 z-40 mx-4 mt-2 rounded-lg border flex items-center justify-center gap-2 py-2 px-4 text-sm font-medium transition-all duration-300 shadow-sm",
        !isOnline 
          ? "bg-destructive/10 text-destructive border-destructive/30" 
          : "bg-status-healthy/10 text-status-healthy border-status-healthy/30"
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
