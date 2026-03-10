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
    <header className="relative overflow-hidden rounded-2xl border border-border/70 bg-card/95 shadow-sm p-6 md:p-8 mb-6">
      <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-accent/50 to-transparent" />
      <div className="relative z-10 flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex items-center gap-4">
          <div className="p-3 rounded-xl bg-accent/10 border border-accent/25">
            <Shield className="w-6 h-6 text-primary" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-display font-bold tracking-tight text-primary">
              CyberMesh <span className="text-accent">Threats</span>
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
                ? "bg-status-healthy/10 text-status-healthy border-status-healthy/30"
                : "bg-muted/50 text-muted-foreground border-border"
            }
          >
            {isLive && (
              <span className="relative flex h-2 w-2 mr-2">
                <span className="absolute inline-flex h-full w-full rounded-full bg-status-healthy/35" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-status-healthy" />
              </span>
            )}
            {systemStatus}
          </Badge>

          {/* Blocked Counter */}
          <div className="px-3 py-1.5 rounded-lg border border-border/70 bg-background">
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
            className="border-border/70 bg-background hover:bg-accent/10 gap-2"
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
