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
    <header className="relative overflow-hidden rounded-2xl border border-border/70 bg-card/95 shadow-sm p-6 md:p-8 mb-6">
      <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-accent/50 to-transparent" />
      <div className="relative z-10 flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex items-center gap-4">
          <div className="p-3 rounded-xl bg-accent/10 border border-accent/25">
            <Server className="w-6 h-6 text-primary" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-display font-bold tracking-tight text-primary">
              CyberMesh <span className="text-accent">System Health</span>
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
            className="border-border/70 bg-background hover:bg-accent/10 gap-2"
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
