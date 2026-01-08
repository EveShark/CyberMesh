import { LedgerSnapshot } from "@/types/blockchain";
import { Database, GitBranch, Shield } from "lucide-react";

interface LedgerSnapshotPanelProps {
  ledger: LedgerSnapshot;
}

const StatItem = ({ label, value }: { label: string; value: string | number | null }) => (
  <div className="flex justify-between items-center py-2 border-b border-border/50 last:border-0">
    <span className="text-sm text-muted-foreground">{label}</span>
    <span className="text-sm font-medium text-foreground font-mono">
      {value !== null && value !== undefined ? value : "--"}
    </span>
  </div>
);

const LedgerSnapshotPanel = ({ ledger }: LedgerSnapshotPanelProps) => {
  return (
    <div className="glass-frost rounded-lg p-6">
      <div className="flex items-center gap-2 mb-6">
        <Database className="h-5 w-5 text-frost" />
        <h3 className="text-lg font-semibold text-foreground">Ledger Snapshot</h3>
      </div>
      
      <div className="grid md:grid-cols-2 gap-6">
        {/* Snapshot State */}
        <div>
          <div className="flex items-center gap-2 mb-3">
            <GitBranch className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium text-muted-foreground">Snapshot State</span>
          </div>
          <div className="space-y-1">
            <StatItem label="Snapshot Height" value={ledger.snapshotHeight} />
            <StatItem label="State Version" value={ledger.stateVersion} />
            <StatItem label="State Root" value={ledger.stateRoot} />
            <StatItem label="Last Block Hash" value={ledger.lastBlockHash} />
            <StatItem label="Snapshot Captured" value={ledger.snapshotTime} />
          </div>
        </div>
        
        {/* Rolling Metrics & Governance */}
        <div>
          <div className="flex items-center gap-2 mb-3">
            <Shield className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium text-muted-foreground">Rolling Metrics & Governance</span>
          </div>
          <div className="space-y-1">
            <StatItem label="Avg Block Time" value={ledger.rollingBlockTime} />
            <StatItem label="Avg Block Size" value={ledger.rollingBlockSize} />
            <StatItem label="Reputation Changes" value={ledger.reputationChanges} />
            <StatItem label="Policy Updates" value={ledger.policyUpdates} />
            <StatItem label="Quarantine Updates" value={ledger.quarantineUpdates} />
          </div>
        </div>
      </div>
    </div>
  );
};

export default LedgerSnapshotPanel;
