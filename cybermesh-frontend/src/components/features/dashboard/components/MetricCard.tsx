import { cn } from "@/lib/utils";
import { LucideIcon, ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";

interface MetricItem {
  label: string;
  value: string | number;
  highlight?: boolean;
  color?: "frost" | "fire" | "emerald" | "amber" | "destructive";
}

interface MetricCardProps {
  title: string;
  icon: LucideIcon;
  status?: string;
  statusColor?: "emerald" | "amber" | "destructive" | "frost";
  metrics: MetricItem[];
  actionLabel?: string;
  href?: string;
  className?: string;
  variant?: "frost" | "fire" | "default";
}

const colorMap = {
  frost: "text-primary",
  fire: "text-primary",
  emerald: "text-status-healthy",
  amber: "text-status-warning",
  destructive: "text-destructive",
};

const statusColorMap = {
  emerald: "bg-status-healthy/10 text-status-healthy border-status-healthy/30",
  amber: "bg-status-warning/10 text-status-warning border-status-warning/30",
  destructive: "bg-destructive/20 text-destructive border-destructive/30",
  frost: "bg-accent/10 text-primary border-accent/30",
};

const MetricCard = ({ 
  title, 
  icon: Icon, 
  status, 
  statusColor = "emerald",
  metrics, 
  actionLabel, 
  href,
  className, 
  variant = "frost" 
}: MetricCardProps) => {
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
        {status && (
          <span className={cn(
            "px-2 py-1 rounded-full text-[11px] font-medium tracking-wide border max-w-[96px] truncate whitespace-nowrap shrink-0",
            statusColorMap[statusColor]
          )}>
            {status}
          </span>
        )}
      </div>
      
      <div className="space-y-3.5">
        {metrics.map((metric, index) => (
          <div key={index} className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">{metric.label}</span>
            <span className={cn(
              "text-sm font-medium",
              metric.color ? colorMap[metric.color] : "text-foreground"
            )}>
              {metric.value}
            </span>
          </div>
        ))}
      </div>

      {actionLabel && href && (
        <Link 
          to={href}
          className="mt-5 inline-flex items-center gap-2 text-sm font-medium text-primary hover:text-accent transition-colors group"
        >
          {actionLabel}
          <ArrowRight className="w-4 h-4 group-hover:translate-x-1 transition-transform" />
        </Link>
      )}
    </div>
  );
};

export default MetricCard;
