import { Telemetry } from "@/types/network";
import { Users, Clock, Hash, TrendingUp, ArrowDownToLine, ArrowUpFromLine } from "lucide-react";

interface TelemetryStripProps {
  telemetry: Telemetry;
}

const TelemetryStrip = ({ telemetry }: TelemetryStripProps) => {
  const formatValue = (value: string | number | undefined | null): string => {
    if (value === undefined || value === null || value === "") return "--";
    return String(value);
  };

  const metrics = [
    {
      label: "Connected Peers",
      value: formatValue(telemetry.connectedPeers),
      icon: Users,
      color: "text-emerald-400",
    },
    {
      label: "Avg Latency",
      value: telemetry.avgLatency ? `${telemetry.avgLatency} ms` : "--",
      icon: Clock,
      color: telemetry.avgLatency > 2000 ? "text-amber-400" : "text-cyan-400",
    },
    {
      label: "Consensus Round",
      value: telemetry.consensusRound?.toLocaleString() ?? "--",
      icon: Hash,
      color: "text-purple-400",
    },
    {
      label: "Leader Stability",
      value: telemetry.leaderStability !== undefined ? `${telemetry.leaderStability}%` : "--",
      icon: TrendingUp,
      color: telemetry.leaderStability < 50 ? "text-amber-400" : "text-emerald-400",
    },
    {
      label: "Inbound Rate",
      value: formatValue(telemetry.inboundRate),
      icon: ArrowDownToLine,
      color: "text-cyan-400",
    },
    {
      label: "Outbound Rate",
      value: formatValue(telemetry.outboundRate),
      icon: ArrowUpFromLine,
      color: "text-cyan-400",
    },
  ];

  return (
    <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
      {metrics.map((metric) => (
        <div
          key={metric.label}
          className="glass-frost rounded-lg p-3 border border-border/30"
        >
          <div className="flex items-center gap-2 mb-1">
            <metric.icon className={`h-4 w-4 ${metric.color}`} />
            <span className="text-xs text-muted-foreground truncate">{metric.label}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
              <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
            </span>
            <span className="text-lg font-semibold text-foreground">{metric.value}</span>
          </div>
        </div>
      ))}
    </div>
  );
};

export default TelemetryStrip;
