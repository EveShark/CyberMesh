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
          iconBg: "bg-emerald-500/10",
          iconColor: "text-emerald-400",
          badgeBg: "bg-emerald-500/20",
          badgeText: "text-emerald-400",
          badgeBorder: "border-emerald-500/30",
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
          iconBg: "bg-amber-500/10",
          iconColor: "text-amber-400",
          badgeBg: "bg-amber-500/20",
          badgeText: "text-amber-400",
          badgeBorder: "border-amber-500/30",
        };
    }
  };

  const config = getStatusConfig();
  const StatusIcon = config.icon;

  const metrics = [
    { label: "Detections/min", value: formatValue(data.detectionsPerMin) },
    { label: "Publish Success", value: formatValue(data.publishSuccess, "%") },
    { label: "Publish to candidate ratio", value: formatValue(data.publishToCandidateRatio) },
    { label: "Last iteration", value: formatValue(data.lastIteration) },
    { label: "Since last iteration", value: formatValue(data.sinceLastIteration) },
  ];

  return (
    <Card className="glass-frost border-border/50 backdrop-blur-xl">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className={`p-2 rounded-lg ${config.iconBg}`}>
              <StatusIcon className={`w-5 h-5 ${config.iconColor}`} />
            </div>
            <CardTitle className="text-lg font-semibold text-foreground">Loop Status</CardTitle>
          </div>
          <Badge className={`${config.badgeBg} ${config.badgeText} ${config.badgeBorder} hover:${config.badgeBg}`}>
            {data.status}
          </Badge>
        </div>
        <p className="text-sm text-muted-foreground mt-1">{data.statusMessage}</p>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-4">
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
