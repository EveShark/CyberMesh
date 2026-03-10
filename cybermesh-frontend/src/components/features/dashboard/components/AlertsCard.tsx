import { forwardRef } from "react";
import { cn } from "@/lib/utils";
import { AlertTriangle } from "lucide-react";

export interface AlertLevel {
  label: string;
  count: number;
  color: string;
}

export interface AlertsData {
  levels: AlertLevel[];
  published: number;
  total: number;
  updated: string;
}

interface AlertsCardProps {
  className?: string;
  data?: AlertsData;
}

const AlertsCard = forwardRef<HTMLDivElement, AlertsCardProps>(
  ({ className, data }, ref) => {
    const levels = data?.levels || [];
    const published = data?.published || 0;
    const total = data?.total || 0;
    const updated = data?.updated || "--";

    return (
      <div
        ref={ref}
        className={cn(
          "rounded-xl p-5 glass-frost transition-all duration-200 border border-destructive/20",
          className
        )}
      >
        <div className="flex items-center gap-3 mb-2">
          <div className="p-2 rounded-lg bg-destructive/10 border border-destructive/20">
            <AlertTriangle className="w-5 h-5 text-destructive" />
          </div>
          <div>
            <h3 className="font-semibold text-foreground">Recent Alerts</h3>
            <p className="text-xs text-muted-foreground">Severity snapshot from AI detections</p>
          </div>
        </div>

        <div className="space-y-2 my-4">
          {levels.map((level) => (
            <div key={level.label} className="flex items-center justify-between gap-3 p-2.5 rounded-lg bg-secondary/30 border border-border/50 min-w-0">
              <span className={cn("text-xs font-medium", level.color)}>
                {level.label}
              </span>
              <span className="text-lg sm:text-xl font-bold text-foreground tabular-nums">
                {level.count.toLocaleString()}
              </span>
            </div>
          ))}
        </div>

        <p className="text-xs text-muted-foreground border-t border-border/50 pt-3">
          Published {published} of {total} detections • Updated {updated}
        </p>
      </div>
    );
  }
);

AlertsCard.displayName = "AlertsCard";

export default AlertsCard;
