import { Activity, Clock, Shield, Users } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import type { ThreatSnapshot, SystemStatus } from "@/types/threats";

interface ThreatSnapshotPanelProps {
  snapshot: ThreatSnapshot;
  systemStatus: SystemStatus;
  validatorCount: number;
  quorumSize: number;
}

const ThreatSnapshotPanel = ({
  snapshot,
  systemStatus,
  validatorCount,
  quorumSize,
}: ThreatSnapshotPanelProps) => {
  const isLive = systemStatus === "LIVE";
  const displayValue = (value: number | string) => (isLive ? value : "--");

  return (
    <Card className="glass-frost border-border/50">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
          <Shield className="h-4 w-4" />
          Recent snapshot (last 500 detections)
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {/* Stats Grid */}
          <div className="space-y-3">
            <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Detection Stats
            </h4>
            <div className="grid grid-cols-3 gap-3">
              <div className="p-3 rounded-lg bg-muted/10 text-center">
                <div className="text-xl font-mono font-bold text-foreground">
                  {displayValue(snapshot.totalDetected.toLocaleString())}
                </div>
                <div className="text-xs text-muted-foreground">Detected</div>
              </div>
              <div className="p-3 rounded-lg bg-emerald-500/10 text-center">
                <div className="text-xl font-mono font-bold text-emerald-400">
                  {displayValue(snapshot.publishedSnapshot.toLocaleString())}
                </div>
                <div className="text-xs text-muted-foreground">Published</div>
              </div>
              <div className="p-3 rounded-lg bg-amber-500/10 text-center">
                <div className="text-xl font-mono font-bold text-amber-400">
                  {displayValue(snapshot.abstainedSnapshot.toLocaleString())}
                </div>
                <div className="text-xs text-muted-foreground">Abstained</div>
              </div>
            </div>
          </div>

          {/* Detection Status Box */}
          <div className="space-y-3">
            <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1">
              <Activity className="h-3 w-3" />
              Detection Status
            </h4>
            <div className="p-4 rounded-lg bg-muted/10 space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">Loop Status</span>
                <Badge
                  variant="outline"
                  className={
                    snapshot.loopStatus === "LIVE"
                      ? "bg-emerald-500/20 text-emerald-400 border-emerald-500/30"
                      : "bg-muted/20 text-muted-foreground border-muted/30"
                  }
                >
                  {isLive && snapshot.loopStatus === "LIVE" && (
                    <span className="relative flex h-2 w-2 mr-2">
                      <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                      <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
                    </span>
                  )}
                  {isLive ? snapshot.loopStatus : "HALTED"}
                </Badge>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground flex items-center gap-1">
                  <Users className="h-3 w-3" />
                  Validators
                </span>
                <span className="font-mono text-foreground">{displayValue(validatorCount)}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">Quorum Size</span>
                <span className="font-mono text-primary">{displayValue(quorumSize)}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground flex items-center gap-1">
                  <Clock className="h-3 w-3" />
                  Last Detection
                </span>
                <span className="font-mono text-foreground">
                  {isLive ? snapshot.lastDetection : "--:--:--"}
                </span>
              </div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default ThreatSnapshotPanel;
