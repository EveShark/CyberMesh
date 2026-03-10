import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { RefreshCw, AlertTriangle, CheckCircle, HelpCircle } from "lucide-react";
import { DetectionLoopMetrics } from "@/types/ai-engine";

interface DetectionLoopCardProps {
  data: DetectionLoopMetrics;
}

const DetectionLoopCard = ({ data }: DetectionLoopCardProps) => {
  const formatValue = (value: number | string | null, suffix = "") => {
    if (value === null || value === undefined) return "--";
    return `${value}${suffix}`;
  };

  const getHealthConfig = () => {
    switch (data.health) {
      case "Healthy":
        return {
          icon: CheckCircle,
          iconColor: "text-status-healthy",
          badgeBg: "bg-status-healthy/10",
          badgeText: "text-status-healthy",
          badgeBorder: "border-status-healthy/30",
        };
      case "Degraded":
        return {
          icon: AlertTriangle,
          iconColor: "text-status-warning",
          badgeBg: "bg-status-warning/10",
          badgeText: "text-status-warning",
          badgeBorder: "border-status-warning/30",
        };
      default:
        return {
          icon: HelpCircle,
          iconColor: "text-muted-foreground",
          badgeBg: "bg-muted/20",
          badgeText: "text-muted-foreground",
          badgeBorder: "border-muted/30",
        };
    }
  };

  const healthConfig = getHealthConfig();
  const HealthIcon = healthConfig.icon;

  const metrics = [
    { label: "Status", value: data.status },
    { label: "Interval", value: formatValue(data.interval) },
    { label: "Avg Latency", value: formatValue(data.avgLatency, "ms"), warning: data.health === "Degraded" },
    { label: "Last Latency", value: formatValue(data.lastLatency, "ms") },
    { label: "Since Detection", value: formatValue(data.sinceDetection) },
    { label: "Cache Age", value: formatValue(data.cacheAge) },
  ];

  return (
    <Card className="glass-frost border-border/60">
      <CardHeader className="pb-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-primary/10">
              <RefreshCw className="w-5 h-5 text-primary" />
            </div>
            <CardTitle className="text-lg font-semibold tracking-tight text-foreground">Detection Loop Metrics</CardTitle>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0 space-y-5">
        <div className="grid grid-cols-2 md:grid-cols-3 gap-x-4 gap-y-3">
          {metrics.map((metric, index) => (
            <div key={index} className="space-y-1">
              <p className="text-xs text-muted-foreground">{metric.label}</p>
              <p className={`text-sm font-semibold ${metric.warning ? "text-status-warning" : "text-foreground"}`}>
                {metric.value}
              </p>
            </div>
          ))}
        </div>

        <div className="pt-3 border-t border-border/50 space-y-3">
          <div className="flex items-center gap-2">
            <HealthIcon className={`w-4 h-4 ${healthConfig.iconColor}`} />
            <Badge className={`${healthConfig.badgeBg} ${healthConfig.badgeText} ${healthConfig.badgeBorder}`}>
              {data.health}
            </Badge>
            {data.health === "Degraded" && data.avgLatency !== null && (
              <span className="text-xs text-muted-foreground">
                avg {data.avgLatency}ms, target &lt;{data.targetLatency}ms
              </span>
            )}
            {data.health === "Unknown" && (
              <span className="text-xs text-muted-foreground">target &lt;{data.targetLatency}ms</span>
            )}
          </div>

          <div className="flex items-center justify-between">
            <span className="text-xs text-muted-foreground">Iterations Count</span>
            <span className="text-sm font-bold text-foreground">{data.iterationsCount.toLocaleString()}</span>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default DetectionLoopCard;
