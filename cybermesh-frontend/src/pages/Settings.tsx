import { Helmet } from "react-helmet-async";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { isDemoMode, setDemoMode } from "@/config/demo-mode";
import { useState, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { FlaskConical, Wifi, Info, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

const Settings = () => {
  const [demoEnabled, setDemoEnabled] = useState(isDemoMode());
  const [isSwitching, setIsSwitching] = useState(false);
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const handleDemoToggle = useCallback(async (checked: boolean) => {
    if (isSwitching) return;

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
  }, [queryClient, navigate, isSwitching]);

  return (
    <>
      <Helmet>
        <title>Settings | CyberMesh</title>
        <meta name="description" content="Configure CyberMesh application settings" />
      </Helmet>

      <div className="p-4 md:p-6 space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
          <p className="text-muted-foreground">Configure your CyberMesh experience</p>
        </div>

        <div className="grid gap-6 max-w-2xl">
          {/* Data Mode Card */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <FlaskConical className="h-5 w-5 text-violet-400" />
                Data Mode
              </CardTitle>
              <CardDescription>
                Choose between live API data or demo mode with sample data
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {/* Toggle */}
              <div className="flex items-center justify-between">
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
                    disabled={isSwitching}
                  />
                </div>
              </div>

              {/* Current Status */}
              <div className={cn(
                "flex items-center gap-3 p-4 rounded-lg border transition-all duration-300",
                demoEnabled
                  ? "bg-violet-500/10 border-violet-500/20"
                  : "bg-status-healthy/10 border-status-healthy/20"
              )}>
                {demoEnabled ? (
                  <>
                    <FlaskConical className="h-5 w-5 text-violet-400" />
                    <div>
                      <p className="font-medium text-violet-400">Demo Mode Active</p>
                      <p className="text-sm text-muted-foreground">
                        Displaying sample data for demonstration purposes
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
                <p>
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
