import { Link } from "react-router-dom";
import { AlertTriangle, ShieldCheck } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ROUTES } from "@/config/routes";
import { useAuth } from "@/lib/auth/AuthProvider";
import { formatDelegationCountdown } from "@/lib/auth/delegation-time";

function formatExpiry(expiresAt?: string): string {
  if (!expiresAt) {
    return "unknown expiry";
  }
  const date = new Date(expiresAt);
  if (Number.isNaN(date.getTime())) {
    return expiresAt;
  }
  return date.toLocaleString();
}

export default function DelegationBanner() {
  const { session } = useAuth();
  const delegation = session?.active_delegation;

  if (!delegation) {
    return null;
  }

  const breakGlass = delegation.break_glass;
  const accessSummary = delegation.access_ids.length > 0 ? delegation.access_ids.join(", ") : "no access scope";

  return (
    <div className={breakGlass ? "border-b border-amber-500/30 bg-amber-500/10" : "border-b border-primary/20 bg-primary/5"}>
      <div className="px-4 py-3 md:px-6 flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="flex items-start gap-3">
          <div className={breakGlass ? "rounded-full bg-amber-500/15 p-2 text-amber-300" : "rounded-full bg-primary/10 p-2 text-primary"}>
            {breakGlass ? <AlertTriangle className="h-4 w-4" /> : <ShieldCheck className="h-4 w-4" />}
          </div>
          <div className="space-y-1">
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-sm font-medium text-foreground">
                {breakGlass ? "Break-glass delegation active" : "Delegated support access active"}
              </p>
              <Badge variant={breakGlass ? "destructive" : "secondary"}>
                {breakGlass ? "Break-glass" : "Delegated"}
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground">Access scope: {accessSummary}</p>
            <p className="text-xs text-muted-foreground">Expires: {formatExpiry(delegation.expires_at)}</p>
            <p className="text-xs text-muted-foreground">Countdown: {formatDelegationCountdown(delegation.expires_at)}</p>
          </div>
        </div>
        <Button asChild variant="outline" size="sm">
          <Link to={ROUTES.DELEGATIONS}>Manage delegation</Link>
        </Button>
      </div>
    </div>
  );
}
