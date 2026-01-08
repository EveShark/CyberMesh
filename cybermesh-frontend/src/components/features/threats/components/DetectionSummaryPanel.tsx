import { Activity, Clock, Users, Zap } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { SystemStatus } from "@/types/threats";

interface DetectionSummaryPanelProps {
  systemStatus: SystemStatus;
  lifetimeTotal: number;
  publishedCount: number;
  publishedPercent: string;
  abstainedCount: number;
  abstainedPercent: string;
  avgResponseTime: string;
  validatorCount: number;
  quorumSize: number;
  updated: string;
}

const DetectionSummaryPanel = ({
  systemStatus,
  lifetimeTotal,
  publishedCount,
  publishedPercent,
  abstainedCount,
  abstainedPercent,
  avgResponseTime,
  validatorCount,
  quorumSize,
  updated,
}: DetectionSummaryPanelProps) => {
  const isLive = systemStatus === "LIVE";
  const displayValue = (value: number | string) => (isLive ? value : "--");

  return (
    <Card className="glass-frost border-border/50">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
          <Activity className="h-4 w-4" />
          Real-time threat detection metrics and system performance
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {/* Lifetime Totals */}
          <div className="space-y-3 min-w-0">
            <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Lifetime Totals
            </h4>
            <div className="space-y-2">
              <div className="flex justify-between items-baseline gap-2">
                <span className="text-xs text-muted-foreground shrink-0">Detected</span>
                <span className="font-mono font-bold text-foreground text-sm truncate">
                  {displayValue(lifetimeTotal.toLocaleString())}
                </span>
              </div>
              <div className="flex justify-between items-baseline gap-2">
                <span className="text-xs text-muted-foreground shrink-0">Published</span>
                <span className="font-mono text-emerald-400 text-sm truncate">
                  {displayValue(`${publishedCount.toLocaleString()} (${publishedPercent})`)}
                </span>
              </div>
              <div className="flex justify-between items-baseline gap-2">
                <span className="text-xs text-muted-foreground shrink-0">Abstained</span>
                <span className="font-mono text-amber-400 text-sm truncate">
                  {displayValue(`${abstainedCount.toLocaleString()} (${abstainedPercent})`)}
                </span>
              </div>
            </div>
          </div>

          {/* Performance */}
          <div className="space-y-3 min-w-0">
            <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1">
              <Zap className="h-3 w-3" />
              Performance
            </h4>
            <div className="space-y-2">
              <div>
                <div className="text-xl sm:text-2xl font-mono font-bold text-primary">
                  {displayValue(avgResponseTime)}
                </div>
                <div className="text-xs text-muted-foreground">Avg Response Time</div>
              </div>
              <div className="text-xs text-muted-foreground/70">
                Detection to execution
              </div>
            </div>
          </div>

          {/* Consensus */}
          <div className="space-y-3 min-w-0">
            <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1">
              <Users className="h-3 w-3" />
              Consensus
            </h4>
            <div className="space-y-2">
              <div className="flex justify-between items-baseline gap-2">
                <span className="text-xs text-muted-foreground shrink-0">Validators</span>
                <span className="font-mono font-bold text-foreground text-sm">
                  {displayValue(validatorCount)}
                </span>
              </div>
              <div className="flex justify-between items-baseline gap-2">
                <span className="text-xs text-muted-foreground shrink-0">Quorum</span>
                <span className="font-mono text-primary text-sm">
                  {displayValue(quorumSize)}
                </span>
              </div>
            </div>
          </div>

          {/* Last Updated */}
          <div className="space-y-3 min-w-0">
            <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1">
              <Clock className="h-3 w-3" />
              Last Updated
            </h4>
            <div className="text-base sm:text-lg font-mono text-foreground">
              {isLive ? updated : "--:--:--"}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default DetectionSummaryPanel;
