import { RefreshCw, Blocks } from "lucide-react";
import { Button } from "@/components/ui/button";

interface BlockchainHeaderProps {
  updated: string;
  isRefreshing?: boolean;
  onRefresh?: () => void;
}

const BlockchainHeader = ({ updated, isRefreshing, onRefresh }: BlockchainHeaderProps) => {
  return (
    <header className="relative overflow-hidden rounded-2xl glass-frost border border-border/50 p-6 md:p-8 mb-6">
      {/* Animated mesh background */}
      <div className="absolute inset-0 opacity-20">
        <div className="absolute top-0 left-1/4 w-72 h-72 bg-frost/30 rounded-full blur-3xl animate-pulse" />
        <div className="absolute bottom-0 right-1/4 w-64 h-64 bg-primary/20 rounded-full blur-3xl animate-pulse delay-1000" />
      </div>

      <div className="relative z-10 flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex items-center gap-4">
          <div className="p-3 rounded-lg glass-frost frost-glow">
            <Blocks className="w-6 h-6 text-frost" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-bold text-foreground">
              CyberMesh <span className="text-gradient">Blockchain</span>
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
            className="glass-frost border-border/50 hover:bg-accent/20 gap-2"
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
