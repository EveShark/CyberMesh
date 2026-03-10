import { RefreshCw, Blocks } from "lucide-react";
import { Button } from "@/components/ui/button";

interface BlockchainHeaderProps {
  updated: string;
  isRefreshing?: boolean;
  onRefresh?: () => void;
}

const BlockchainHeader = ({ updated, isRefreshing, onRefresh }: BlockchainHeaderProps) => {
  return (
    <header className="relative overflow-hidden rounded-2xl border border-border/70 bg-card/95 shadow-sm p-6 md:p-8 mb-6">
      <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-accent/50 to-transparent" />
      <div className="relative z-10 flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex items-center gap-4">
          <div className="p-3 rounded-xl bg-accent/10 border border-accent/25">
            <Blocks className="w-6 h-6 text-primary" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-display font-bold tracking-tight text-primary">
              CyberMesh <span className="text-accent">Blockchain</span>
            </h1>
            <p className="text-muted-foreground max-w-xl mt-1">
              Live block production, anomaly detection, and proposer telemetry
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">
            Updated {updated ? formatUpdatedTime(updated) : "--"}
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={onRefresh}
            disabled={isRefreshing}
            className="border-border/70 bg-background hover:bg-accent/10 gap-2"
          >
            <RefreshCw className={`h-4 w-4 ${isRefreshing ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        </div>
      </div>
    </header>
  );
};

/**
 * Format ISO timestamp to human-readable format (e.g., "7:52:48 PM")
 */
function formatUpdatedTime(timestamp: string): string {
  try {
    const date = new Date(timestamp);
    if (isNaN(date.getTime())) return timestamp;
    return date.toLocaleTimeString();
  } catch {
    return timestamp;
  }
}

export default BlockchainHeader;
