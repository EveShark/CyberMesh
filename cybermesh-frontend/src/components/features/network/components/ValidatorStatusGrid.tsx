import { ValidatorStatus } from "@/types/network";
import { Crown, Clock, Activity, Eye } from "lucide-react";
import { Badge } from "@/components/ui/badge";

interface ValidatorStatusGridProps {
  validators: ValidatorStatus[];
  leader: string;
}

const ValidatorStatusGrid = ({ validators, leader }: ValidatorStatusGridProps) => {
  const getStatusColor = (status: string) => {
    switch (status) {
      case "Healthy":
        return "bg-emerald-500/20 text-emerald-400 border-emerald-500/30";
      case "Warning":
        return "bg-amber-500/20 text-amber-400 border-amber-500/30";
      case "Critical":
        return "bg-red-500/20 text-red-400 border-red-500/30";
      default:
        return "bg-muted text-muted-foreground";
    }
  };

  return (
    <div className="glass-frost rounded-xl p-6 border border-border/30">
      <div className="mb-4">
        <h3 className="text-lg font-semibold text-foreground">Validator Status</h3>
        <p className="text-sm text-muted-foreground">5-node cluster health and Byzantine fault tolerance</p>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5 gap-4">
        {validators.map((validator) => {
          const isLeader = validator.name === leader;
          
          return (
            <div
              key={validator.name}
              className={`rounded-lg p-4 border transition-all ${
                isLeader 
                  ? "bg-amber-500/5 border-amber-500/30" 
                  : "bg-background/30 border-border/30"
              }`}
            >
              <div className="flex items-center gap-2 mb-3">
                {isLeader && <Crown className="h-4 w-4 text-amber-400" />}
                <span className="font-semibold text-foreground">{validator.name}</span>
              </div>
              
              {validator.hash && (
                <p className="text-xs text-muted-foreground mb-2 font-mono truncate">
                  {validator.hash}
                </p>
              )}

              <Badge variant="outline" className={`mb-3 ${getStatusColor(validator.status)}`}>
                {validator.status}
              </Badge>

              <div className="space-y-2 text-xs">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Clock className="h-3 w-3" />
                  <span>Latency:</span>
                  <span className="text-cyan-400 ml-auto">{validator.latency}</span>
                </div>
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Eye className="h-3 w-3" />
                  <span>Last seen:</span>
                  <span className="text-foreground ml-auto">{validator.lastSeen}</span>
                </div>
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Activity className="h-3 w-3" />
                  <span>Role:</span>
                  <span className={`ml-auto ${isLeader ? "text-amber-400" : "text-purple-400"}`}>
                    {isLeader ? "leader" : validator.role}
                  </span>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default ValidatorStatusGrid;
