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
      color: "text-status-healthy",
    },
    {
      label: "Avg Ping RTT",
      value: telemetry.networkAverageLatencyMs ? `${telemetry.networkAverageLatencyMs} ms` : "--",
      icon: Clock,
      color: telemetry.networkAverageLatencyMs > 2000 ? "text-status-warning" : "text-primary",
    },
    {
      label: "Consensus Round",
      value: telemetry.consensusRound?.toLocaleString() ?? "--",
      icon: Hash,
      color: "text-primary",
    },
    {
      label: "Leader Stability",
      value: telemetry.leaderStability !== undefined ? `${telemetry.leaderStability}%` : "--",
      icon: TrendingUp,
      color: telemetry.leaderStability < 50 ? "text-status-warning" : "text-status-healthy",
    },
    {
      label: "Inbound Rate",
      value: formatValue(telemetry.inboundRate),
      icon: ArrowDownToLine,
      color: "text-primary",
    },
    {
      label: "Outbound Rate",
      value: formatValue(telemetry.outboundRate),
      icon: ArrowUpFromLine,
      color: "text-primary",
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
            <span className="relative flex h-2 w-2 rounded-full bg-status-healthy">
              <span className="absolute inline-flex h-full w-full rounded-full bg-status-healthy/35"></span>
            </span>
            <span className="text-lg font-semibold text-foreground">{metric.value}</span>
          </div>
        </div>
      ))}
    </div>
  );
};

export default TelemetryStrip;
