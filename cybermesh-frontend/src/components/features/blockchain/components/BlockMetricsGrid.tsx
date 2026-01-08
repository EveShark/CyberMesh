import { BlockMetrics } from "@/types/blockchain";
import { Blocks, ArrowUpDown, Clock, HardDrive, CheckCircle, Timer, Database } from "lucide-react";

interface BlockMetricsGridProps {
  metrics: BlockMetrics;
}

const MetricCard = ({ 
  icon: Icon, 
  label, 
  value, 
  sublabel 
}: { 
  icon: React.ElementType; 
  label: string; 
  value: string | number | null; 
  sublabel?: string;
}) => (
  <div className="glass-frost rounded-lg p-4">
    <div className="flex items-center gap-2 text-muted-foreground mb-2">
      <Icon className="h-4 w-4" />
      <span className="text-xs font-medium">{label}</span>
    </div>
    <div className="text-xl sm:text-2xl font-bold text-foreground">
      {value !== null && value !== undefined ? value.toLocaleString() : "--"}
    </div>
    {sublabel && (
      <p className="text-xs text-muted-foreground mt-1">{sublabel}</p>
    )}
  </div>
);

const BlockMetricsGrid = ({ metrics }: BlockMetricsGridProps) => {
  const metricItems = [
    { 
      icon: Blocks, 
      label: "Latest Height", 
      value: metrics.latestHeight,
    },
    { 
      icon: ArrowUpDown, 
      label: "Total Transactions", 
      value: metrics.totalTransactions,
      sublabel: "Cumulative transactions observed"
    },
    { 
      icon: Clock, 
      label: "Avg Block Time", 
      value: metrics.avgBlockTime,
      sublabel: "Backend-reported average block time"
    },
    { 
      icon: HardDrive, 
      label: "Avg Block Size", 
      value: metrics.avgBlockSize,
      sublabel: "Mean block payload size (last 100 blocks)"
    },
    { 
      icon: CheckCircle, 
      label: "Success Rate", 
      value: metrics.successRate,
      sublabel: "Backend execution success rate"
    },
    { 
      icon: Timer, 
      label: "Pending Transactions", 
      value: metrics.pendingTxs,
      sublabel: "Transactions waiting in mempool"
    },
    { 
      icon: Database, 
      label: "Mempool Size", 
      value: metrics.mempoolSize,
      sublabel: "Total bytes queued for inclusion"
    },
  ];

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
      {metricItems.map((item) => (
        <MetricCard key={item.label} {...item} />
      ))}
    </div>
  );
};

export default BlockMetricsGrid;
