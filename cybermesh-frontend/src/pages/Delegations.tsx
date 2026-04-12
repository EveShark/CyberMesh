import { useCallback, useEffect, useMemo, useState } from "react";
import { Helmet } from "react-helmet-async";
import { toast } from "sonner";
import { AlertTriangle, Loader2, ShieldCheck, ShieldPlus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useAuth } from "@/lib/auth/AuthProvider";
import { formatDelegationCountdown } from "@/lib/auth/delegation-time";
import {
  approveDelegation,
  createDelegation,
  fetchDelegations,
  revokeDelegation,
  type DelegationRecord,
} from "@/lib/auth/session";

type ApprovalInputState = Record<string, string>;
type RevokeReasonState = Record<string, string>;

function toLocalInputValue(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function normalizeDateTime(value: string): string | undefined {
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }
  const date = new Date(trimmed);
  if (Number.isNaN(date.getTime())) {
    return undefined;
  }
  return date.toISOString();
}

function formatDateTime(value?: string): string {
  if (!value) {
    return "Not set";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function delegationTone(item: DelegationRecord): "default" | "secondary" | "destructive" | "outline" {
  switch (item.status) {
    case "active":
      return item.break_glass ? "destructive" : "secondary";
    case "revoked":
    case "expired":
    case "rejected":
      return "outline";
    default:
      return "default";
  }
}

function DelegationCard({
  item,
  canApprove,
  approvalReference,
  revokeReason,
  pendingAction,
  onApprovalReferenceChange,
  onRevokeReasonChange,
  onApprove,
  onRevoke,
}: {
  item: DelegationRecord;
  canApprove: boolean;
  approvalReference: string;
  revokeReason: string;
  pendingAction: string | null;
  onApprovalReferenceChange: (delegationId: string, value: string) => void;
  onRevokeReasonChange: (delegationId: string, value: string) => void;
  onApprove: (item: DelegationRecord) => Promise<void>;
  onRevoke: (item: DelegationRecord) => Promise<void>;
}) {
  const isPending = pendingAction === item.delegation_id;
  const approvalValue = approvalReference || item.approval_reference || "";

  return (
    <Card className="border-border/60">
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="space-y-1">
            <CardTitle className="text-base break-all">{item.delegation_id}</CardTitle>
            <CardDescription>
              Principal: <span className="font-medium text-foreground">{item.principal_id}</span>
            </CardDescription>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={delegationTone(item)}>{item.status}</Badge>
            {item.break_glass && <Badge variant="destructive">Break-glass</Badge>}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 md:grid-cols-2">
          <div>
            <p className="text-xs uppercase tracking-wider text-muted-foreground">Reason</p>
            <p className="mt-1 text-sm">{item.reason_text}</p>
            <p className="text-xs text-muted-foreground">{item.reason_code}</p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-muted-foreground">Access scope</p>
            <p className="mt-1 text-sm break-all">{item.access_ids.join(", ") || "No access IDs"}</p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-muted-foreground">Starts</p>
            <p className="mt-1 text-sm">{formatDateTime(item.starts_at)}</p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-muted-foreground">Expires</p>
            <p className="mt-1 text-sm">{formatDateTime(item.expires_at)}</p>
          </div>
        </div>

        {(item.break_glass || canApprove) && (
          <div className="grid gap-2">
            <Label htmlFor={`approval-reference-${item.delegation_id}`}>Approval reference</Label>
            <Input
              id={`approval-reference-${item.delegation_id}`}
              value={approvalValue}
              onChange={(event) => onApprovalReferenceChange(item.delegation_id, event.target.value)}
              placeholder={item.break_glass ? "Required for break-glass approval" : "Optional approval reference"}
            />
          </div>
        )}

        {(item.status === "pending" || item.status === "active") && (
          <div className="grid gap-2">
            <Label htmlFor={`revoke-reason-${item.delegation_id}`}>Revoke note</Label>
            <Input
              id={`revoke-reason-${item.delegation_id}`}
              value={revokeReason}
              onChange={(event) => onRevokeReasonChange(item.delegation_id, event.target.value)}
              placeholder="Reason recorded in audit trail"
            />
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          {canApprove && item.status === "pending" && (
            <Button onClick={() => void onApprove(item)} disabled={isPending}>
              {isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : "Approve"}
            </Button>
          )}
          {(item.status === "pending" || item.status === "active") && (
            <Button variant="outline" onClick={() => void onRevoke(item)} disabled={isPending}>
              {isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : "Revoke"}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export default function Delegations() {
  const { session, refreshSession } = useAuth();
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [myDelegations, setMyDelegations] = useState<DelegationRecord[]>([]);
  const [adminDelegations, setAdminDelegations] = useState<DelegationRecord[]>([]);
  const [approvalReferences, setApprovalReferences] = useState<ApprovalInputState>({});
  const [revokeReasons, setRevokeReasons] = useState<RevokeReasonState>({});
  const [accessIdsInput, setAccessIdsInput] = useState("");
  const [reasonCode, setReasonCode] = useState("support_session");
  const [reasonText, setReasonText] = useState("");
  const [startsAt, setStartsAt] = useState("");
  const [expiresAt, setExpiresAt] = useState(toLocalInputValue(new Date(Date.now() + 60 * 60 * 1000)));
  const [breakGlass, setBreakGlass] = useState(false);
  const [approvalReference, setApprovalReference] = useState("");

  const canManageDelegations = session?.capabilities.can_manage_delegations ?? false;
  const breakGlassEnabled = session?.capabilities.break_glass_enabled ?? false;

  const loadDelegations = useCallback(async () => {
    setLoading(true);
    try {
      const own = await fetchDelegations("self");
      setMyDelegations(own.delegations);

      if (canManageDelegations) {
        const all = await fetchDelegations("all");
        setAdminDelegations(all.delegations);
      } else {
        setAdminDelegations([]);
      }
    } catch (error) {
      toast.error("Failed to load delegation state", {
        description: error instanceof Error ? error.message : "Unexpected error",
      });
    } finally {
      setLoading(false);
    }
  }, [canManageDelegations]);

  useEffect(() => {
    void loadDelegations();
  }, [loadDelegations]);

  useEffect(() => {
    const interval = window.setInterval(() => {
      void loadDelegations();
    }, 60_000);
    return () => window.clearInterval(interval);
  }, [loadDelegations]);

  const pendingApprovals = useMemo(
    () => adminDelegations.filter((item) => item.status === "pending"),
    [adminDelegations],
  );

  const handleCreate = useCallback(async () => {
    if (submitting) {
      return;
    }
    const accessIds = accessIdsInput
      .split(",")
      .map((value) => value.trim())
      .filter(Boolean);
    const normalizedExpiry = normalizeDateTime(expiresAt);
    const normalizedStart = normalizeDateTime(startsAt);

    if (accessIds.length === 0) {
      toast.error("At least one access ID is required");
      return;
    }
    if (!normalizedExpiry) {
      toast.error("A valid expiry time is required");
      return;
    }
    if (!reasonText.trim()) {
      toast.error("Reason text is required");
      return;
    }
    if (breakGlass && !breakGlassEnabled) {
      toast.error("Break-glass is disabled for this deployment");
      return;
    }

    setSubmitting(true);
    try {
      await createDelegation({
        access_ids: accessIds,
        reason_code: reasonCode.trim() || (breakGlass ? "break_glass_override" : "support_session"),
        reason_text: reasonText.trim(),
        starts_at: normalizedStart,
        expires_at: normalizedExpiry,
        break_glass: breakGlass,
        approval_reference: approvalReference.trim() || undefined,
      });
      toast.success(breakGlass ? "Break-glass request submitted" : "Delegation request submitted");
      setAccessIdsInput("");
      setReasonText("");
      setStartsAt("");
      setExpiresAt(toLocalInputValue(new Date(Date.now() + 60 * 60 * 1000)));
      setBreakGlass(false);
      setApprovalReference("");
      await refreshSession();
      await loadDelegations();
    } catch (error) {
      toast.error("Failed to submit delegation request", {
        description: error instanceof Error ? error.message : "Unexpected error",
      });
    } finally {
      setSubmitting(false);
    }
  }, [accessIdsInput, approvalReference, breakGlass, breakGlassEnabled, expiresAt, loadDelegations, reasonCode, reasonText, refreshSession, startsAt, submitting]);

  const handleApprove = useCallback(async (item: DelegationRecord) => {
    setPendingAction(item.delegation_id);
    try {
      await approveDelegation(item.delegation_id, {
        approval_reference: (approvalReferences[item.delegation_id] || item.approval_reference || "").trim() || undefined,
      });
      toast.success(item.break_glass ? "Break-glass delegation approved" : "Delegation approved");
      await refreshSession();
      await loadDelegations();
    } catch (error) {
      toast.error("Failed to approve delegation", {
        description: error instanceof Error ? error.message : "Unexpected error",
      });
    } finally {
      setPendingAction(null);
    }
  }, [approvalReferences, loadDelegations, refreshSession]);

  const handleRevoke = useCallback(async (item: DelegationRecord) => {
    setPendingAction(item.delegation_id);
    try {
      await revokeDelegation(item.delegation_id, {
        reason_code: item.break_glass ? "break_glass_revoke" : "support_revoke",
        reason_text: (revokeReasons[item.delegation_id] || "Support session complete").trim(),
      });
      toast.success("Delegation revoked");
      await refreshSession();
      await loadDelegations();
    } catch (error) {
      toast.error("Failed to revoke delegation", {
        description: error instanceof Error ? error.message : "Unexpected error",
      });
    } finally {
      setPendingAction(null);
    }
  }, [loadDelegations, refreshSession, revokeReasons]);

  return (
    <>
      <Helmet>
        <title>Delegations | CyberMesh</title>
        <meta name="description" content="Manage support delegation and break-glass workflows" />
      </Helmet>

      <div className="p-4 md:p-6 lg:p-8 space-y-8">
        <div className="space-y-1.5">
          <h1 className="text-3xl md:text-4xl font-display font-bold tracking-tight text-primary">Delegations</h1>
          <p className="text-sm md:text-base text-muted-foreground">
            Request temporary support access, approve delegated sessions, and track break-glass activity.
          </p>
        </div>

        {session?.active_delegation && (
          <Card className={session.active_delegation.break_glass ? "border-amber-500/40 bg-amber-500/5" : "border-primary/20 bg-primary/5"}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-xl">
                {session.active_delegation.break_glass ? <AlertTriangle className="h-5 w-5 text-amber-400" /> : <ShieldCheck className="h-5 w-5 text-primary" />}
                {session.active_delegation.break_glass ? "Break-glass session active" : "Delegated support session active"}
              </CardTitle>
              <CardDescription>
                Current access scope: {session.active_delegation.access_ids.join(", ")}. Expires {formatDateTime(session.active_delegation.expires_at)}.
              </CardDescription>
              <p className="text-sm text-muted-foreground">
                Countdown: {formatDelegationCountdown(session.active_delegation.expires_at)}
              </p>
            </CardHeader>
          </Card>
        )}

        <div className="grid gap-6 xl:grid-cols-[minmax(0,420px)_1fr]">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-xl">
                <ShieldPlus className="h-5 w-5 text-primary" />
                Request delegation
              </CardTitle>
              <CardDescription>
                One session, one reason, one expiry window. Break-glass stays gated and auditable.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="delegation-access-ids">Access IDs</Label>
                <Input
                  id="delegation-access-ids"
                  value={accessIdsInput}
                  onChange={(event) => setAccessIdsInput(event.target.value)}
                  placeholder="acme-prod, beta-prod"
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="delegation-reason-code">Reason code</Label>
                <Input
                  id="delegation-reason-code"
                  value={reasonCode}
                  onChange={(event) => setReasonCode(event.target.value)}
                  placeholder="support_session"
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="delegation-reason-text">Reason</Label>
                <Textarea
                  id="delegation-reason-text"
                  value={reasonText}
                  onChange={(event) => setReasonText(event.target.value)}
                  placeholder="Describe the customer issue or operational need."
                />
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="delegation-starts-at">Starts at</Label>
                  <Input
                    id="delegation-starts-at"
                    type="datetime-local"
                    value={startsAt}
                    onChange={(event) => setStartsAt(event.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="delegation-expires-at">Expires at</Label>
                  <Input
                    id="delegation-expires-at"
                    type="datetime-local"
                    value={expiresAt}
                    onChange={(event) => setExpiresAt(event.target.value)}
                  />
                </div>
              </div>

              <div className="rounded-lg border border-border/60 p-4 space-y-3">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <Label htmlFor="break-glass-toggle" className="text-sm font-medium">Break-glass</Label>
                    <p className="text-xs text-muted-foreground">
                      Restricted override path with stronger approval and audit requirements.
                    </p>
                  </div>
                  <Switch
                    id="break-glass-toggle"
                    checked={breakGlass}
                    onCheckedChange={setBreakGlass}
                    disabled={!breakGlassEnabled}
                  />
                </div>
                {!breakGlassEnabled && (
                  <p className="text-xs text-muted-foreground">
                    Break-glass is disabled in the current deployment configuration.
                  </p>
                )}
                <div className="space-y-2">
                  <Label htmlFor="approval-reference">Approval reference</Label>
                  <Input
                    id="approval-reference"
                    value={approvalReference}
                    onChange={(event) => setApprovalReference(event.target.value)}
                    placeholder={breakGlass ? "Required before approval" : "Optional ticket or approval reference"}
                  />
                </div>
              </div>

              <Button onClick={() => void handleCreate()} disabled={submitting} className="w-full">
                {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : "Submit delegation request"}
              </Button>
            </CardContent>
          </Card>

          <div className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle className="text-xl">My delegations</CardTitle>
                <CardDescription>
                  Current and historical requests tied to this session principal.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                {loading ? (
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Loader2 className="h-4 w-4 animate-spin" />
                    Loading delegation state...
                  </div>
                ) : myDelegations.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No delegation requests found for this principal.</p>
                ) : (
                  myDelegations.map((item) => (
                    <DelegationCard
                      key={item.delegation_id}
                      item={item}
                      canApprove={false}
                      approvalReference={approvalReferences[item.delegation_id] || ""}
                      revokeReason={revokeReasons[item.delegation_id] || ""}
                      pendingAction={pendingAction}
                      onApprovalReferenceChange={(delegationId, value) => setApprovalReferences((current) => ({ ...current, [delegationId]: value }))}
                      onRevokeReasonChange={(delegationId, value) => setRevokeReasons((current) => ({ ...current, [delegationId]: value }))}
                      onApprove={handleApprove}
                      onRevoke={handleRevoke}
                    />
                  ))
                )}
              </CardContent>
            </Card>

            {canManageDelegations && (
              <Card>
                <CardHeader>
                  <CardTitle className="text-xl">Approval queue</CardTitle>
                  <CardDescription>
                    Pending delegation requests visible to `control_lease_admin`.
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  {loading ? (
                    <div className="flex items-center gap-2 text-sm text-muted-foreground">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Loading approval queue...
                    </div>
                  ) : pendingApprovals.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No pending delegation approvals right now.</p>
                  ) : (
                    pendingApprovals.map((item) => (
                      <DelegationCard
                        key={item.delegation_id}
                        item={item}
                        canApprove
                        approvalReference={approvalReferences[item.delegation_id] || ""}
                        revokeReason={revokeReasons[item.delegation_id] || ""}
                        pendingAction={pendingAction}
                        onApprovalReferenceChange={(delegationId, value) => setApprovalReferences((current) => ({ ...current, [delegationId]: value }))}
                        onRevokeReasonChange={(delegationId, value) => setRevokeReasons((current) => ({ ...current, [delegationId]: value }))}
                        onApprove={handleApprove}
                        onRevoke={handleRevoke}
                      />
                    ))
                  )}
                </CardContent>
              </Card>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
