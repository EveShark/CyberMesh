import { RefreshCw, Activity, Server } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { SystemHealthData, getStatusColors } from "@/types/system-health";

interface SystemHealthHeaderProps {
  data: SystemHealthData;
  onRefresh?: () => void;
  isRefreshing?: boolean;
}

const SystemHealthHeader = ({ data, onRefresh, isRefreshing = false }: SystemHealthHeaderProps) => {
  // Null guard - should not render without data
  if (!data) return null;
  
  const readinessColors = getStatusColors(data.serviceReadiness);
  const aiStatusColors = getStatusColors(data.aiStatus);

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
            <Server className="w-6 h-6 text-frost" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-bold text-foreground">
              CyberMesh <span className="text-gradient">System Health</span>
            </h1>
            <p className="text-muted-foreground max-w-xl mt-1">
              Overall subsystem status reported by the backend
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          {/* Main Status Badge */}
          <Badge className={`${readinessColors} px-4 py-2 text-sm font-semibold border`}>
            <Activity className="w-4 h-4 mr-2" />
            {data.serviceReadiness}
          </Badge>

          {/* AI Status Badge */}
          <Badge className={`${aiStatusColors} px-4 py-2 text-sm font-semibold border`}>
            {data.aiStatus}
          </Badge>

          {/* Updated Timestamp */}
          <span className="text-sm text-muted-foreground">
            Updated {data.updated}
          </span>

          {/* Refresh Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={onRefresh}
            disabled={isRefreshing}
            className="glass-frost border-border/50 hover:bg-accent/20 gap-2"
          >
            <RefreshCw className={`w-4 h-4 ${isRefreshing ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        </div>
      </div>
    </header>
  );
};

export default SystemHealthHeader;
