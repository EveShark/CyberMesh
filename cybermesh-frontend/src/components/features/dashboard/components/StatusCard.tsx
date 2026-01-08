import { cn } from "@/lib/utils";
import { LucideIcon } from "lucide-react";

interface StatusMetric {
  label: string;
  value: string | number;
  highlight?: boolean;
}

interface StatusCardProps {
  title: string;
  icon: LucideIcon;
  status: "ready" | "running" | "active" | "healthy" | "warning" | "error";
  metrics: StatusMetric[];
  className?: string;
  variant?: "frost" | "fire" | "default";
}

const statusStyles = {
  ready: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
  running: "bg-frost/20 text-frost border-frost/30",
  active: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
  healthy: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
  warning: "bg-amber-500/20 text-amber-400 border-amber-500/30",
  error: "bg-destructive/20 text-destructive border-destructive/30",
};

const StatusCard = ({ title, icon: Icon, status, metrics, className, variant = "frost" }: StatusCardProps) => {
  const glassClass = variant === "fire" ? "glass-fire" : variant === "frost" ? "glass-frost" : "glass";
  
  return (
    <div className={cn(
      "rounded-xl p-5 transition-all duration-300 hover:scale-[1.02]",
      glassClass,
      className
    )}>
      <div className="flex items-start justify-between mb-4">
        <div className="flex items-center gap-3">
          <div className={cn(
            "p-2 rounded-lg",
            variant === "fire" ? "bg-fire/10" : "bg-frost/10"
          )}>
            <Icon className={cn(
              "w-5 h-5",
              variant === "fire" ? "text-fire" : "text-frost"
            )} />
          </div>
          <h3 className="font-semibold text-foreground">{title}</h3>
        </div>
        <span className={cn(
          "px-2 py-1 rounded-full text-xs font-medium border uppercase max-w-[80px] truncate whitespace-nowrap shrink-0",
          statusStyles[status]
        )}>
          {status}
        </span>
      </div>
      
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        {metrics.map((metric, index) => (
          <div key={index} className="space-y-1">
            <p className="text-xs text-muted-foreground">{metric.label}</p>
            <p className={cn(
              "text-sm font-medium",
              metric.highlight ? "text-frost" : "text-foreground"
            )}>
              {metric.value}
            </p>
          </div>
        ))}
      </div>
    </div>
  );
};

export default StatusCard;
