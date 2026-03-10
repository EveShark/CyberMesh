import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Play, Pause, HelpCircle } from "lucide-react";
import { LoopStatusData } from "@/types/ai-engine";

interface LoopStatusCardProps {
  data: LoopStatusData;
}

const LoopStatusCard = ({ data }: LoopStatusCardProps) => {
  const formatValue = (value: number | string | null, suffix = "") => {
    if (value === null || value === undefined) return "--";
    return `${value}${suffix}`;
  };

  const getStatusConfig = () => {
    switch (data.status) {
      case "Running":
        return {
          icon: Play,
          iconBg: "bg-status-healthy/10",
          iconColor: "text-status-healthy",
          badgeBg: "bg-status-healthy/10",
          badgeText: "text-status-healthy",
          badgeBorder: "border-status-healthy/30",
        };
      case "Stopped":
        return {
          icon: Pause,
          iconBg: "bg-muted/20",
          iconColor: "text-muted-foreground",
          badgeBg: "bg-muted/20",
          badgeText: "text-muted-foreground",
          badgeBorder: "border-muted/30",
        };
      default:
        return {
          icon: HelpCircle,
          iconBg: "bg-status-warning/10",
          iconColor: "text-status-warning",
          badgeBg: "bg-status-warning/10",
          badgeText: "text-status-warning",
          badgeBorder: "border-status-warning/30",
        };
    }
  };

  const config = getStatusConfig();
  const StatusIcon = config.icon;

  const metrics = [
    { label: "Detections/min", value: formatValue(data.detectionsPerMin) },
    { label: "Publish Success", value: formatValue(data.publishSuccess, "%") },
    { label: "Publish to candidate ratio", value: formatValue(data.publishToCandidateRatio) },
    { label: "Since last iteration", value: formatValue(data.lastIteration) },
    { label: "Since last detection", value: formatValue(data.sinceLastIteration) },
  ];

  return (
    <Card className="glass-frost border-border/60">
      <CardHeader className="pb-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className={`p-2 rounded-lg ${config.iconBg}`}>
              <StatusIcon className={`w-5 h-5 ${config.iconColor}`} />
            </div>
            <CardTitle className="text-lg font-semibold tracking-tight text-foreground">Loop Status</CardTitle>
          </div>
          <Badge className={`${config.badgeBg} ${config.badgeText} ${config.badgeBorder} hover:${config.badgeBg}`}>
            {data.status}
          </Badge>
        </div>
        <p className="text-sm text-muted-foreground mt-1">{data.statusMessage}</p>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          {metrics.map((metric, index) => (
            <div key={index} className="space-y-1">
              <p className="text-xs text-muted-foreground">{metric.label}</p>
              <p className="text-sm font-semibold text-foreground">{metric.value}</p>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
};

export default LoopStatusCard;
