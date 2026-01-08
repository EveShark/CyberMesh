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
          iconColor: "text-emerald-400",
          badgeBg: "bg-emerald-500/20",
          badgeText: "text-emerald-400",
          badgeBorder: "border-emerald-500/30",
        };
      case "Degraded":
        return {
          icon: AlertTriangle,
          iconColor: "text-amber-400",
          badgeBg: "bg-amber-500/20",
          badgeText: "text-amber-400",
          badgeBorder: "border-amber-500/30",
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
    <Card className="glass-frost border-border/50 backdrop-blur-xl">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-primary/10">
              <RefreshCw className="w-5 h-5 text-primary" />
            </div>
            <CardTitle className="text-lg font-semibold text-foreground">Detection Loop Metrics</CardTitle>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-3 gap-4">
          {metrics.map((metric, index) => (
            <div key={index} className="space-y-1">
              <p className="text-xs text-muted-foreground">{metric.label}</p>
              <p className={`text-sm font-semibold ${metric.warning ? "text-amber-400" : "text-foreground"}`}>
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
