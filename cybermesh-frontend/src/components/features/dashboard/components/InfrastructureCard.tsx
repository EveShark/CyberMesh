import { cn } from "@/lib/utils";
import { Server, Database, HardDrive, Cpu, Clock, Layers, Container, LucideIcon } from "lucide-react";

export interface InfrastructureItem {
  name: string;
  icon: string;
  status: "active" | "warning" | "error";
  details: string;
}

export interface InfrastructureData {
  items: InfrastructureItem[];
  memory: string;
  uptime: string;
}

interface InfrastructureCardProps {
  className?: string;
  data?: InfrastructureData;
}

const iconMap: Record<string, LucideIcon> = {
  Server,
  Database,
  HardDrive,
  Layers,
  Container,
  Cpu,
};

const statusColors = {
  active: "bg-emerald-500",
  warning: "bg-amber-500",
  error: "bg-destructive",
};

const InfrastructureCard = ({ className, data }: InfrastructureCardProps) => {
  const items = data?.items || [];
  const memory = data?.memory || "--";
  const uptime = data?.uptime || "--";

  return (
    <div className={cn(
      "rounded-xl p-5 glass-frost transition-all duration-300",
      className
    )}>
      <div className="flex items-center gap-3 mb-5">
        <div className="p-2 rounded-lg bg-frost/10">
          <Server className="w-5 h-5 text-frost" />
        </div>
        <h3 className="font-semibold text-foreground">Infrastructure</h3>
      </div>

      <div className="flex flex-wrap gap-4">
        {items.map((item) => {
          const IconComponent = iconMap[item.icon] || Server;
          return (
            <div key={item.name} className="flex-1 min-w-[140px] p-3 rounded-lg bg-secondary/30 border border-border/50">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2 overflow-hidden">
                  <div className="p-1.5 rounded-md bg-frost/10 shrink-0">
                    <IconComponent className="w-3.5 h-3.5 text-frost" />
                  </div>
                  <span className="text-xs font-medium text-foreground whitespace-nowrap">{item.name}</span>
                </div>
                <span className={cn(
                  "w-1.5 h-1.5 rounded-full shrink-0 ml-2",
                  statusColors[item.status]
                )} />
              </div>
              <p className="text-xs text-muted-foreground whitespace-nowrap">{item.details}</p>
            </div>
          );
        })}

        <div className="flex-1 min-w-[140px] p-3 rounded-lg bg-secondary/30 border border-border/50">
          <div className="flex items-center gap-2 mb-2">
            <Cpu className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium text-foreground whitespace-nowrap">Memory</span>
          </div>
          <p className="text-xs text-muted-foreground">{memory}</p>
        </div>

        <div className="flex-1 min-w-[140px] p-3 rounded-lg bg-secondary/30 border border-border/50">
          <div className="flex items-center gap-2 mb-2">
            <Clock className="w-4 h-4 text-muted-foreground" />
            <span className="text-sm font-medium text-foreground whitespace-nowrap">System Uptime</span>
          </div>
          <p className="text-xs text-muted-foreground">{uptime}</p>
        </div>
      </div>
    </div>
  );
};

export default InfrastructureCard;
