import { ConsensusSummary, RiskLevel } from "@/types/network";
import { Shield, TrendingUp, AlertTriangle } from "lucide-react";
import { Badge } from "@/components/ui/badge";

interface ConsensusSummaryFooterProps {
  summary: ConsensusSummary;
}

const ConsensusSummaryFooter = ({ summary }: ConsensusSummaryFooterProps) => {
  const getRiskStyle = (risk: RiskLevel) => {
    switch (risk) {
      case "SAFE":
        return "bg-emerald-500/20 text-emerald-400 border-emerald-500/30";
      case "LOW":
        return "bg-cyan-500/20 text-cyan-400 border-cyan-500/30";
      case "MEDIUM":
        return "bg-amber-500/20 text-amber-400 border-amber-500/30";
      case "HIGH":
      case "CRITICAL":
        return "bg-red-500/20 text-red-400 border-red-500/30";
      default:
        return "bg-muted text-muted-foreground";
    }
  };

  const getRiskDescription = (risk: RiskLevel) => {
    switch (risk) {
      case "SAFE":
        return "Byzantine fault tolerance maintained";
      case "LOW":
        return "Minor network fluctuations detected";
      case "MEDIUM":
        return "Consensus delays possible";
      case "HIGH":
        return "Network stability compromised";
      case "CRITICAL":
        return "Immediate attention required";
      default:
        return "";
    }
  };

  return (
    <div className="glass-frost rounded-xl p-4 border border-border/30">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div className="flex items-center gap-3">
          <Shield className="h-5 w-5 text-emerald-400" />
          <div>
            <span className="text-sm text-muted-foreground">Network health: </span>
            <span className="text-foreground font-medium">
              {summary.activeNodes}/5 active nodes ({summary.operationalCapacity} operational capacity)
            </span>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <TrendingUp className="h-5 w-5 text-cyan-400" />
          <div>
            <span className="text-sm text-muted-foreground">Leader stability: </span>
            <span className="text-foreground font-medium">{summary.leaderStability}</span>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <AlertTriangle className="h-5 w-5 text-amber-400" />
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Consensus risk: </span>
            <Badge variant="outline" className={getRiskStyle(summary.risk)}>
              {summary.risk}
            </Badge>
            <span className="text-sm text-muted-foreground hidden md:inline">
              - {getRiskDescription(summary.risk)}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
};

export default ConsensusSummaryFooter;
