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
  frost: "text-frost",
  fire: "text-fire",
  emerald: "text-emerald-400",
  amber: "text-amber-400",
  destructive: "text-destructive",
};

const statusColorMap = {
  emerald: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
  amber: "bg-amber-500/20 text-amber-400 border-amber-500/30",
  destructive: "bg-destructive/20 text-destructive border-destructive/30",
  frost: "bg-frost/20 text-frost border-frost/30",
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
        {status && (
          <span className={cn(
            "px-2 py-1 rounded-full text-xs font-medium border max-w-[80px] truncate whitespace-nowrap shrink-0",
            statusColorMap[statusColor]
          )}>
            {status}
          </span>
        )}
      </div>
      
      <div className="space-y-3">
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
          className="mt-4 flex items-center gap-2 text-sm text-frost hover:text-frost-glow transition-colors group"
        >
          {actionLabel}
          <ArrowRight className="w-4 h-4 group-hover:translate-x-1 transition-transform" />
        </Link>
      )}
    </div>
  );
};

export default MetricCard;
