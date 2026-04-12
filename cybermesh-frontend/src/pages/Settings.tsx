import { Helmet } from "react-helmet-async";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { isDemoMode, setDemoMode } from "@/config/demo-mode";
import { useState, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { FlaskConical, Wifi, Info, Loader2, ShieldCheck } from "lucide-react";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import { useAuth } from "@/lib/auth/AuthProvider";
import { Button } from "@/components/ui/button";
import { Link } from "react-router-dom";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ROUTES } from "@/config/routes";

const Settings = () => {
  const [demoEnabled, setDemoEnabled] = useState(isDemoMode());
  const [isSwitching, setIsSwitching] = useState(false);
  const [isUpdatingAccess, setIsUpdatingAccess] = useState(false);
  const [isSigningOut, setIsSigningOut] = useState(false);
  const demoLocked = import.meta.env.VITE_DEMO_MODE === "true";
  const queryClient = useQueryClient();
  const { authEnabled, session, selectAccess, startLogout } = useAuth();

  const handleDemoToggle = useCallback(async (checked: boolean) => {
    if (isSwitching || demoLocked) return;

    setIsSwitching(true);

    // Update state immediately for UI feedback
    setDemoEnabled(checked);

    // Set the new mode in localStorage
    setDemoMode(checked);

    // Clear all cached data
    queryClient.clear();

    // Brief delay for visual feedback
    await new Promise(resolve => setTimeout(resolve, 300));

    toast.success(checked ? "Demo mode enabled" : "Live mode enabled", {
      description: checked
        ? "Now using sample data for demonstration"
        : "Now fetching live data from the API",
    });

    // Navigate to dashboard to trigger fresh data fetch with new mode
    window.location.href = "/dashboard";

    setIsSwitching(false);
  }, [queryClient, isSwitching, demoLocked]);

  const handleAccessChange = useCallback(async (value: string) => {
    if (isUpdatingAccess) return;

    setIsUpdatingAccess(true);
    try {
      await selectAccess(value === "__none__" ? "" : value);
      queryClient.clear();
      toast.success("Access context updated", {
        description: value === "__none__" ? "Cleared the active access selection" : `Active access set to ${value}`,
      });
    } catch (error) {
      toast.error("Failed to update access context", {
        description: error instanceof Error ? error.message : "Unexpected error",
      });
    } finally {
      setIsUpdatingAccess(false);
    }
  }, [isUpdatingAccess, queryClient, selectAccess]);

  const handleLogout = useCallback(async () => {
    if (isSigningOut) return;
    setIsSigningOut(true);
    try {
      await startLogout();
    } finally {
      setIsSigningOut(false);
    }
  }, [isSigningOut, startLogout]);

  return (
    <>
      <Helmet>
        <title>Settings | CyberMesh</title>
        <meta name="description" content="Configure CyberMesh application settings" />
      </Helmet>

      <div className="p-4 md:p-6 lg:p-8 space-y-8">
        <div className="space-y-1.5">
          <h1 className="text-3xl md:text-4xl font-display font-bold tracking-tight text-primary">Settings</h1>
          <p className="text-sm md:text-base text-muted-foreground">Configure your CyberMesh experience</p>
        </div>

        <div className="grid gap-6 max-w-3xl">
          <Card>
            <CardHeader className="pb-4">
              <CardTitle className="flex items-center gap-2 text-xl">
                <ShieldCheck className="h-5 w-5 text-primary" />
                Authentication
              </CardTitle>
              <CardDescription className="text-sm leading-relaxed">
                Hosted session state and active access context
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {!authEnabled ? (
                <div className="rounded-lg border border-border/60 bg-muted/30 p-4 text-sm text-muted-foreground">
                  Hosted authentication is not enabled for this deployment.
                </div>
              ) : (
                <>
                  <div className="grid gap-3 md:grid-cols-2">
                    <div className="rounded-lg border border-border/60 bg-muted/20 p-4">
                      <p className="text-xs uppercase tracking-wider text-muted-foreground">Principal</p>
                      <p className="mt-2 font-medium break-all">{session?.principal_id || "Not signed in"}</p>
                      <p className="mt-1 text-sm text-muted-foreground">{session?.email || session?.preferred_username || "No profile details"}</p>
                    </div>
                    <div className="rounded-lg border border-border/60 bg-muted/20 p-4">
                      <p className="text-xs uppercase tracking-wider text-muted-foreground">Auth Source</p>
                      <p className="mt-2 font-medium">{session?.auth_source || "Unavailable"}</p>
                      <p className="mt-1 text-sm text-muted-foreground">Principal type: {session?.principal_type || "unknown"}</p>
                    </div>
                  </div>

                  {session?.active_delegation && (
                    <div className="rounded-lg border border-primary/20 bg-primary/5 p-4">
                      <p className="text-xs uppercase tracking-wider text-muted-foreground">Active Delegation</p>
                      <p className="mt-2 font-medium">
                        {session.active_delegation.break_glass ? "Break-glass session" : "Delegated support session"}
                      </p>
                      <p className="mt-1 text-sm text-muted-foreground">
                        Scope: {session.active_delegation.access_ids.join(", ")}
                      </p>
                      <p className="mt-1 text-sm text-muted-foreground">
                        Expires: {new Date(session.active_delegation.expires_at).toLocaleString()}
                      </p>
                    </div>
                  )}

                  <div className="space-y-3">
                    <Label htmlFor="active-access">Active Access</Label>
                    <Select
                      value={session?.active_access_id || "__none__"}
                      onValueChange={handleAccessChange}
                      disabled={isUpdatingAccess}
                    >
                      <SelectTrigger id="active-access">
                        <SelectValue placeholder="Select access context" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__none__">No active access</SelectItem>
                        {(session?.allowed_access_ids || []).map((accessId) => (
                          <SelectItem key={accessId} value={accessId}>
                            {accessId}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">
                      Allowed access IDs: {(session?.allowed_access_ids || []).length > 0 ? session?.allowed_access_ids.join(", ") : "none"}
                    </p>
                  </div>

                  <div className="flex flex-wrap gap-3">
                    <Button asChild variant="secondary">
                      <Link to={ROUTES.DELEGATIONS}>Manage delegations</Link>
                    </Button>
                    <Button
                      variant="outline"
                      onClick={handleLogout}
                      disabled={isSigningOut}
                    >
                      {isSigningOut ? "Signing out..." : "Sign out"}
                    </Button>
                  </div>
                </>
              )}
            </CardContent>
          </Card>

          {/* Data Mode Card */}
          <Card>
            <CardHeader className="pb-4">
              <CardTitle className="flex items-center gap-2 text-xl">
                <FlaskConical className="h-5 w-5 text-primary" />
                Data Mode
              </CardTitle>
              <CardDescription className="text-sm leading-relaxed">
                Choose between live API data or preview mode data
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-7">
              {/* Toggle */}
              <div className="flex items-center justify-between gap-6">
                <div className="space-y-0.5">
                  <Label htmlFor="demo-mode" className="text-base font-medium">
                    Demo Mode
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    Use sample data without API calls
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  {isSwitching && (
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  )}
                  <Switch
                    id="demo-mode"
                    checked={demoEnabled}
                    onCheckedChange={handleDemoToggle}
                    disabled={isSwitching || demoLocked}
                  />
                </div>
              </div>
              {demoLocked && (
                <p className="text-xs text-muted-foreground">
                  Demo mode is locked by environment configuration for this deployment.
                </p>
              )}

              {/* Current Status */}
              <div className={cn(
                "flex items-center gap-3 p-4 rounded-lg border transition-all duration-300",
                demoEnabled
                  ? "bg-accent/10 border-accent/30"
                  : "bg-status-healthy/10 border-status-healthy/20"
              )}>
                {demoEnabled ? (
                  <>
                    <FlaskConical className="h-5 w-5 text-primary" />
                    <div>
                      <p className="font-medium text-primary">Demo Mode Active</p>
                      <p className="text-sm text-muted-foreground">
                        Displaying preview data for this environment
                      </p>
                    </div>
                  </>
                ) : (
                  <>
                    <Wifi className="h-5 w-5 text-status-healthy" />
                    <div>
                      <p className="font-medium text-status-healthy">Live Mode Active</p>
                      <p className="text-sm text-muted-foreground">
                        Fetching real-time data from the API
                      </p>
                    </div>
                  </>
                )}
              </div>

              {/* Info */}
              <div className="flex items-start gap-2 text-sm text-muted-foreground">
                <Info className="h-4 w-4 mt-0.5 flex-shrink-0" />
                <p className="leading-relaxed">
                  Switching modes will redirect you to the dashboard with fresh data.
                </p>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </>
  );
};

export default Settings;
