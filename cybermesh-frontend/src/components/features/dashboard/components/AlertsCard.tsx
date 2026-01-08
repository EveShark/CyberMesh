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
          "rounded-xl p-5 glass-fire transition-all duration-300",
          className
        )}
      >
        <div className="flex items-center gap-3 mb-2">
          <div className="p-2 rounded-lg bg-fire/10">
            <AlertTriangle className="w-5 h-5 text-fire" />
          </div>
          <div>
            <h3 className="font-semibold text-foreground">Recent Alerts</h3>
            <p className="text-xs text-muted-foreground">Severity snapshot from AI detections</p>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3 my-4">
          {levels.map((level) => (
            <div key={level.label} className="text-center p-3 rounded-lg bg-secondary/30 border border-border/50 min-w-0">
              <p className="text-2xl font-bold text-foreground">{level.count}</p>
              <span className={cn(
                "inline-block mt-1 px-2 py-0.5 rounded text-xs font-medium",
                level.color
              )}>
                {level.label}
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
