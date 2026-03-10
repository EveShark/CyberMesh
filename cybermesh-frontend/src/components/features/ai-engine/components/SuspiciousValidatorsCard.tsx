import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ShieldAlert, ShieldCheck, Copy, Check } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { SuspiciousEntitiesData, SuspiciousEntity } from "@/types/ai-engine";

interface SuspiciousValidatorsCardProps {
  data: SuspiciousEntitiesData;
}

const SuspiciousValidatorsCard = ({ data }: SuspiciousValidatorsCardProps) => {
  const [copiedHash, setCopiedHash] = useState<string | null>(null);

  const handleCopyHash = (entity: SuspiciousEntity) => {
    navigator.clipboard.writeText(entity.fullHash);
    setCopiedHash(entity.fullHash);
    toast.success("Hash copied to clipboard");
    setTimeout(() => setCopiedHash(null), 2000);
  };

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleString("en-US", {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: true,
    });
  };

  const getSeverityBadge = (severity: SuspiciousEntity["severity"]) => {
    const config = {
      Critical: "bg-destructive/20 text-destructive border-destructive/30",
      High: "bg-status-warning/15 text-status-warning border-status-warning/35",
      Medium: "bg-status-warning/10 text-status-warning border-status-warning/30",
      Low: "bg-accent/10 text-primary border-accent/30",
    };
    return config[severity];
  };

  const hasEntities = data.entities.length > 0;

  return (
    <Card className={`glass-frost border-border/60 h-full ${hasEntities ? "border-destructive/20" : ""}`}>
      <CardHeader className="pb-4">
        <div className="flex items-center gap-3">
          <div className={`p-2 rounded-lg ${hasEntities ? "bg-destructive/10" : "bg-emerald-500/10"}`}>
            {hasEntities ? (
              <ShieldAlert className="w-5 h-5 text-destructive" />
            ) : (
              <ShieldCheck className="w-5 h-5 text-status-healthy" />
            )}
          </div>
          <div>
            <CardTitle className="text-lg font-semibold tracking-tight text-foreground">Suspicious Entities</CardTitle>
            <p className="text-xs leading-relaxed text-muted-foreground mt-0.5">
              {hasEntities ? "Security anomalies detected" : "Monitoring active"}
            </p>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0 space-y-4">
        {hasEntities ? (
          <>
            {data.entities.map((entity) => (
              <div
                key={entity.fullHash}
                className="p-4 md:p-5 rounded-lg bg-destructive/5 border border-destructive/20 space-y-3.5"
              >
                <div className="flex items-center justify-between">
                  <span className="text-sm md:text-base font-semibold text-foreground">{entity.name}</span>
                  <div className="flex items-center gap-2">
                    <Badge variant="outline" className="text-xs border-border/50 text-muted-foreground">{entity.kindLabel}</Badge>
                    <Badge className={getSeverityBadge(entity.severity)}>{entity.severity}</Badge>
                  </div>
                </div>



                <div className="flex items-center gap-2">
                  <span className="text-xs text-muted-foreground font-mono">{entity.hash}</span>
                  <button
                    onClick={() => handleCopyHash(entity)}
                    className="p-1 rounded hover:bg-muted/20 transition-colors"
                  >
                    {copiedHash === entity.fullHash ? (
                      <Check className="w-3 h-3 text-status-healthy" />
                    ) : (
                      <Copy className="w-3 h-3 text-muted-foreground" />
                    )}
                  </button>
                </div>

                <div className="grid grid-cols-2 gap-3 pt-2.5">
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">Score</p>
                    <p className="text-sm font-semibold text-destructive">{entity.score.toFixed(2)}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">Events</p>
                    <p className="text-sm font-semibold text-foreground">{entity.events}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">Last seen</p>
                    <p className="text-sm font-semibold text-foreground">{formatDate(entity.lastSeen)}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">Threats</p>
                    <div className="flex gap-1 flex-wrap">
                      {entity.threats.map((threat) => (
                        <Badge key={threat} variant="outline" className="text-xs border-destructive/30 text-destructive">
                          {threat}
                        </Badge>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </>
        ) : (
          <div className="p-8 rounded-lg bg-status-healthy/5 border border-status-healthy/20 text-center">
            <ShieldCheck className="w-12 h-12 text-status-healthy mx-auto mb-3" />
            <p className="text-foreground font-medium">No suspicious entities detected</p>
            <p className="text-xs text-muted-foreground mt-1">No suspicious activity detected</p>
          </div>
        )}

        <div className="flex items-center justify-between pt-2 border-t border-border/50">
          <span className="text-sm text-muted-foreground">Network Status</span>
          <span className={`text-sm font-medium ${hasEntities ? "text-status-warning" : "text-status-healthy"}`}>
            {data.networkStatus}
          </span>
        </div>
      </CardContent>
    </Card>
  );
};

export default SuspiciousValidatorsCard;
