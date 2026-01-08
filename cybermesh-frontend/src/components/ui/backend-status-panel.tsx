import { useState, useEffect, useCallback } from "react";
import { Activity, CheckCircle, XCircle, Clock, RefreshCw, AlertTriangle } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

interface CheckResult {
  status: string;
  latency_ms?: number;
  message?: string;
}

interface ReadinessResponse {
  ready: boolean;
  checks: Record<string, CheckResult>;
  timestamp: number;
  phase?: string;
  warnings?: string[];
}

interface EndpointStatus {
  name: string;
  status: "healthy" | "degraded" | "down" | "checking" | "not_configured";
  latency: number | null;
  message?: string;
}

/**
 * Get the backend API base URL
 */
function getBackendUrl(): string {
  // In development, use Vite proxy (empty string = relative path)
  // In production, use the configured backend URL
  if (import.meta.env.DEV) {
    return "";
  }
  return import.meta.env.VITE_BACKEND_URL || "https://api.cybermesh.qzz.io";
}

/**
 * Format check name for display (e.g., "ai_service" -> "AI Service")
 */
function formatCheckName(name: string): string {
  return name
    .split("_")
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

/**
 * Map backend status to UI status
 */
function mapStatus(status: string): EndpointStatus["status"] {
  switch (status) {
    case "ok":
      return "healthy";
    case "degraded":
    case "warning":
    case "genesis":
    case "single_node":
      return "degraded";
    case "not configured":
      return "not_configured";
    case "not ready":
    case "no_peers":
    case "insufficient":
    default:
      return "down";
  }
}

export const BackendStatusPanel = () => {
  const [endpoints, setEndpoints] = useState<EndpointStatus[]>([]);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [overallUptime, setOverallUptime] = useState<number>(0);
  const [lastChecked, setLastChecked] = useState<Date | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [phase, setPhase] = useState<string>("");

  const checkBackendHealth = useCallback(async () => {
    setIsRefreshing(true);
    setError(null);

    try {
      const response = await fetch(`${getBackendUrl()}/api/v1/ready`, {
        method: "GET",
        headers: {
          "Content-Type": "application/json",
        },
      });

      if (!response.ok) {
        throw new Error(`Backend returned ${response.status}`);
      }

      const data = await response.json();
      const readiness: ReadinessResponse = data.data || data;

      // Convert checks to endpoint statuses
      const statuses: EndpointStatus[] = Object.entries(readiness.checks || {}).map(
        ([name, check]) => ({
          name: formatCheckName(name),
          status: mapStatus(check.status),
          latency: check.latency_ms ?? null,
          message: check.message,
        })
      );

      setEndpoints(statuses);
      setLastChecked(new Date());
      setPhase(readiness.phase || "");

      // Calculate uptime percentage (healthy + degraded = "up")
      const upCount = statuses.filter(
        (s) => s.status === "healthy" || s.status === "degraded"
      ).length;
      const configuredCount = statuses.filter((s) => s.status !== "not_configured").length;
      setOverallUptime(configuredCount > 0 ? Math.round((upCount / configuredCount) * 100) : 0);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to check backend");
      setEndpoints([]);
      setOverallUptime(0);
    } finally {
      setIsRefreshing(false);
    }
  }, []);

  useEffect(() => {
    checkBackendHealth();

    // Check every 30 seconds
    const interval = setInterval(checkBackendHealth, 30000);
    return () => clearInterval(interval);
  }, [checkBackendHealth]);

  const getStatusIcon = (status: EndpointStatus["status"]) => {
    switch (status) {
      case "healthy":
        return <CheckCircle className="h-4 w-4 text-green-500" />;
      case "degraded":
        return <AlertTriangle className="h-4 w-4 text-yellow-500" />;
      case "not_configured":
        return <Clock className="h-4 w-4 text-muted-foreground" />;
      case "down":
        return <XCircle className="h-4 w-4 text-destructive" />;
      case "checking":
        return <RefreshCw className="h-4 w-4 animate-spin text-muted-foreground" />;
    }
  };

  const getLatencyColor = (latency: number | null) => {
    if (latency === null) return "text-muted-foreground";
    if (latency < 100) return "text-green-500";
    if (latency < 500) return "text-yellow-500";
    return "text-destructive";
  };

  const avgLatency = Math.round(
    endpoints.filter((e) => e.latency !== null).reduce((sum, e) => sum + (e.latency || 0), 0) /
      (endpoints.filter((e) => e.latency !== null).length || 1)
  );

  return (
    <Card className="border-border/50">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-sm font-medium">
            <Activity className="h-4 w-4 text-primary" />
            Backend Status
            {phase && (
              <Badge variant="outline" className="ml-2 text-xs">
                {phase}
              </Badge>
            )}
          </CardTitle>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 w-7 p-0"
            onClick={checkBackendHealth}
            disabled={isRefreshing}
          >
            <RefreshCw className={cn("h-3.5 w-3.5", isRefreshing && "animate-spin")} />
            <span className="sr-only">Refresh status</span>
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {error ? (
          <div className="text-center py-4 text-destructive text-sm">
            <XCircle className="h-8 w-8 mx-auto mb-2 opacity-50" />
            {error}
          </div>
        ) : (
          <>
            {/* Summary Stats */}
            <div className="flex items-center gap-4 text-xs">
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground">Uptime:</span>
                <Badge
                  variant={overallUptime === 100 ? "default" : overallUptime >= 75 ? "secondary" : "destructive"}
                  className="px-1.5 py-0 text-xs"
                >
                  {overallUptime}%
                </Badge>
              </div>
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground">Avg Latency:</span>
                <span className={getLatencyColor(avgLatency)}>{avgLatency}ms</span>
              </div>
            </div>

            {/* Endpoint List */}
            <div className="space-y-2">
              {endpoints.length === 0 && !isRefreshing && (
                <div className="text-center py-4 text-muted-foreground text-sm">
                  No checks available
                </div>
              )}
              {isRefreshing && endpoints.length === 0 && (
                <div className="text-center py-4 text-muted-foreground text-sm">
                  <RefreshCw className="h-5 w-5 mx-auto mb-2 animate-spin" />
                  Checking backend...
                </div>
              )}
              {endpoints.map((ep) => (
                <div
                  key={ep.name}
                  className="flex items-center justify-between rounded-md bg-muted/30 px-3 py-2"
                >
                  <div className="flex items-center gap-2">
                    {getStatusIcon(ep.status)}
                    <span className="text-sm">{ep.name}</span>
                  </div>
                  <div className="flex items-center gap-3">
                    {ep.latency !== null && (
                      <span className={cn("text-xs font-mono", getLatencyColor(ep.latency))}>
                        {ep.latency.toFixed(1)}ms
                      </span>
                    )}
                    {ep.message && ep.status !== "healthy" && (
                      <span className="text-xs text-muted-foreground truncate max-w-[150px]" title={ep.message}>
                        {ep.message}
                      </span>
                    )}
                  </div>
                </div>
              ))}
            </div>

            {/* Last updated */}
            {lastChecked && (
              <p className="text-xs text-muted-foreground text-center">
                Last checked: {lastChecked.toLocaleTimeString()}
              </p>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
};
