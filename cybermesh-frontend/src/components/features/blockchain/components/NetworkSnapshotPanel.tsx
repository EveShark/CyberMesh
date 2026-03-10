import { NetworkSnapshot } from "@/types/blockchain";
import { Network, AlertTriangle, Zap } from "lucide-react";

interface NetworkSnapshotPanelProps {
  snapshot: NetworkSnapshot;
}

const NetworkSnapshotPanel = ({ snapshot }: NetworkSnapshotPanelProps) => {
  const hasAnomalies = snapshot.blocksAnomalies > 0 || snapshot.totalAnomalies > 0;
  
  return (
    <div className={`rounded-lg p-6 ${hasAnomalies ? "glass-frost border border-status-warning/30" : "glass-frost"}`}>
      <div className="flex items-center gap-2 mb-6">
        <Network className={`h-5 w-5 ${hasAnomalies ? "text-status-warning" : "text-primary"}`} />
        <h3 className="text-lg font-semibold text-foreground">Network Snapshot</h3>
        {hasAnomalies && (
          <span className="ml-auto flex items-center gap-1 px-2 py-0.5 rounded-full bg-status-warning/10 text-status-warning border border-status-warning/30 text-xs">
            <AlertTriangle className="h-3 w-3" />
            Anomalies Detected
          </span>
        )}
      </div>
      
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="text-center p-4 rounded-lg bg-background/30">
          <div className={`text-2xl font-bold ${hasAnomalies ? "text-status-warning" : "text-foreground"}`}>
            {snapshot.blocksAnomalies}
          </div>
          <div className="text-xs text-muted-foreground mt-1">
            Blocks with Anomalies
          </div>
          <div className="text-xs text-muted-foreground/70 mt-1">
            Count derived from latest API batch
          </div>
        </div>
        
        <div className="text-center p-4 rounded-lg bg-background/30">
          <div className={`text-2xl font-bold ${hasAnomalies ? "text-red-400" : "text-foreground"}`}>
            {snapshot.totalAnomalies}
          </div>
          <div className="text-xs text-muted-foreground mt-1">
            Total Anomalies
          </div>
          <div className="text-xs text-muted-foreground/70 mt-1">
            Evidence transactions observed
          </div>
        </div>
        
        <div className="text-center p-4 rounded-lg bg-background/30">
          <div className="flex items-center justify-center gap-1 text-2xl font-bold text-foreground">
            <Zap className="h-5 w-5 text-primary" />
            {snapshot.peerLatencyAvg}
          </div>
          <div className="text-xs text-muted-foreground mt-1">
            Peer Latency
          </div>
          <div className="text-xs text-muted-foreground/70 mt-1">
            Reported by validator network overview
          </div>
        </div>
      </div>
    </div>
  );
};

export default NetworkSnapshotPanel;
