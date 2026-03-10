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
  ready: "bg-status-healthy/10 text-status-healthy border-status-healthy/30",
  running: "bg-accent/10 text-primary border-accent/30",
  active: "bg-status-healthy/10 text-status-healthy border-status-healthy/30",
  healthy: "bg-status-healthy/10 text-status-healthy border-status-healthy/30",
  warning: "bg-status-warning/10 text-status-warning border-status-warning/30",
  error: "bg-destructive/20 text-destructive border-destructive/30",
};

const StatusCard = ({ title, icon: Icon, status, metrics, className, variant = "frost" }: StatusCardProps) => {
  const glassClass = variant === "fire" ? "glass-fire" : variant === "frost" ? "glass-frost" : "glass";
  
  return (
    <div className={cn(
      "rounded-xl p-5 md:p-6 transition-all duration-200 hover:border-accent/40",
      glassClass,
      className
    )}>
      <div className="flex items-start justify-between gap-3 mb-5">
        <div className="flex items-center gap-3">
          <div className={cn(
            "p-2 rounded-lg",
            "bg-accent/10 border border-accent/20"
          )}>
            <Icon className={cn(
              "w-5 h-5",
              "text-primary"
            )} />
          </div>
          <h3 className="text-base font-semibold tracking-tight text-foreground">{title}</h3>
        </div>
        <span className={cn(
          "px-2 py-1 rounded-full text-[11px] font-medium border uppercase tracking-wide max-w-[96px] truncate whitespace-nowrap shrink-0",
          statusStyles[status]
        )}>
          {status}
        </span>
      </div>
      
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-3">
        {metrics.map((metric, index) => (
          <div key={index} className="space-y-1.5">
            <p className="text-xs text-muted-foreground">{metric.label}</p>
            <p className={cn(
              "text-sm font-medium",
              metric.highlight ? "text-primary" : "text-foreground"
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
