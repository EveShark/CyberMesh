import { RefreshCw, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import type { SystemStatus } from "@/types/threats";

interface ThreatsHeaderProps {
  systemStatus: SystemStatus;
  blockedCount: string;
  onRefresh: () => void;
  isRefreshing?: boolean;
}

const ThreatsHeader = ({ 
  systemStatus, 
  blockedCount, 
  onRefresh, 
  isRefreshing = false 
}: ThreatsHeaderProps) => {
  const isLive = systemStatus === "LIVE";

  return (
    <header className="relative overflow-hidden rounded-2xl glass-frost border border-border/50 p-6 md:p-8 mb-6">
      {/* Animated mesh background */}
      <div className="absolute inset-0 opacity-20">
        <div className="absolute top-0 left-1/4 w-72 h-72 bg-fire/30 rounded-full blur-3xl animate-pulse" />
        <div className="absolute bottom-0 right-1/4 w-64 h-64 bg-destructive/20 rounded-full blur-3xl animate-pulse delay-1000" />
      </div>

      <div className="relative z-10 flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex items-center gap-4">
          <div className="p-3 rounded-lg glass-fire fire-glow">
            <Shield className="w-6 h-6 text-fire" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-bold text-foreground">
              CyberMesh <span className="text-gradient-fire">Threats</span>
            </h1>
            <p className="text-muted-foreground max-w-xl mt-1">
              Real-time threat monitoring and analysis dashboard
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          {/* Status Badge */}
          <Badge
            variant="outline"
            className={
              isLive
                ? "bg-emerald-500/20 text-emerald-400 border-emerald-500/30"
                : "bg-muted/20 text-muted-foreground border-muted/30"
            }
          >
            {isLive && (
              <span className="relative flex h-2 w-2 mr-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
              </span>
            )}
            {systemStatus}
          </Badge>

          {/* Blocked Counter */}
          <div className="px-3 py-1.5 rounded-lg glass-frost">
            <span className="text-xs text-muted-foreground mr-2">Blocked:</span>
            <span className={`font-mono font-bold ${isLive ? "text-destructive" : "text-muted-foreground"}`}>
              {isLive ? blockedCount : "--"}
            </span>
          </div>

          {/* Refresh Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={onRefresh}
            disabled={isRefreshing}
            className="glass-frost border-border/50 hover:bg-accent/20 gap-2"
          >
            <RefreshCw className={`h-4 w-4 ${isRefreshing ? "animate-spin" : ""}`} />
            <span className="hidden sm:inline">Refresh</span>
          </Button>
        </div>
      </div>
    </header>
  );
};

export default ThreatsHeader;
