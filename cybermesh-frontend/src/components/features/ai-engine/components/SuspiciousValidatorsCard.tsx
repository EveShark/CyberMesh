import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ShieldAlert, ShieldCheck, Copy, Check } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { ValidatorsData, Validator } from "@/types/ai-engine";

interface SuspiciousValidatorsCardProps {
  data: ValidatorsData;
}

const SuspiciousValidatorsCard = ({ data }: SuspiciousValidatorsCardProps) => {
  const [copiedHash, setCopiedHash] = useState<string | null>(null);

  const handleCopyHash = (validator: Validator) => {
    navigator.clipboard.writeText(validator.fullHash);
    setCopiedHash(validator.fullHash);
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

  const getSeverityBadge = (severity: Validator["severity"]) => {
    const config = {
      Critical: "bg-destructive/20 text-destructive border-destructive/30",
      High: "bg-orange-500/20 text-orange-400 border-orange-500/30",
      Medium: "bg-amber-500/20 text-amber-400 border-amber-500/30",
      Low: "bg-blue-500/20 text-blue-400 border-blue-500/30",
    };
    return config[severity];
  };

  const hasValidators = data.validators.length > 0;

  return (
    <Card className={`${hasValidators ? "glass-fire" : "glass-frost"} border-border/50 backdrop-blur-xl h-full`}>
      <CardHeader className="pb-3">
        <div className="flex items-center gap-3">
          <div className={`p-2 rounded-lg ${hasValidators ? "bg-destructive/10" : "bg-emerald-500/10"}`}>
            {hasValidators ? (
              <ShieldAlert className="w-5 h-5 text-destructive" />
            ) : (
              <ShieldCheck className="w-5 h-5 text-emerald-400" />
            )}
          </div>
          <div>
            <CardTitle className="text-lg font-semibold text-foreground">Suspicious Validators</CardTitle>
            <p className="text-xs text-muted-foreground mt-0.5">
              {hasValidators ? "Security Anomalies Detected" : "Monitoring Active"}
            </p>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {hasValidators ? (
          <>
            {data.validators.map((validator) => (
              <div
                key={validator.fullHash}
                className="p-4 rounded-lg bg-destructive/5 border border-destructive/20 space-y-3"
              >
                <div className="flex items-center justify-between">
                  <span className="font-semibold text-foreground">{validator.name}</span>
                  <Badge className={getSeverityBadge(validator.severity)}>{validator.severity}</Badge>
                </div>



                <div className="flex items-center gap-2">
                  <span className="text-xs text-muted-foreground font-mono">{validator.hash}</span>
                  <button
                    onClick={() => handleCopyHash(validator)}
                    className="p-1 rounded hover:bg-muted/20 transition-colors"
                  >
                    {copiedHash === validator.fullHash ? (
                      <Check className="w-3 h-3 text-emerald-400" />
                    ) : (
                      <Copy className="w-3 h-3 text-muted-foreground" />
                    )}
                  </button>
                </div>

                <div className="grid grid-cols-2 gap-3 pt-2">
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">Score</p>
                    <p className="text-sm font-semibold text-destructive">{validator.score.toFixed(2)}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">Events</p>
                    <p className="text-sm font-semibold text-foreground">{validator.events}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">Last seen</p>
                    <p className="text-sm font-semibold text-foreground">{formatDate(validator.lastSeen)}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">Threats</p>
                    <div className="flex gap-1 flex-wrap">
                      {validator.threats.map((threat) => (
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
          <div className="p-8 rounded-lg bg-emerald-500/5 border border-emerald-500/20 text-center">
            <ShieldCheck className="w-12 h-12 text-emerald-400 mx-auto mb-3" />
            <p className="text-foreground font-medium">All validators appear healthy</p>
            <p className="text-xs text-muted-foreground mt-1">No suspicious activity detected</p>
          </div>
        )}

        <div className="flex items-center justify-between pt-2 border-t border-border/50">
          <span className="text-sm text-muted-foreground">Network Status</span>
          <span className={`text-sm font-medium ${hasValidators ? "text-amber-400" : "text-emerald-400"}`}>
            {data.networkStatus}
          </span>
        </div>
      </CardContent>
    </Card>
  );
};

export default SuspiciousValidatorsCard;
