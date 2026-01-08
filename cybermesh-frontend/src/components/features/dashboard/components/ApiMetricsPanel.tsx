import { useState, useEffect } from "react";
import { Activity, Clock, AlertTriangle, TrendingUp, ChevronDown, ChevronUp, RefreshCw } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { getAllMetrics, clearMetrics } from "@/lib/performance-metrics";

interface EndpointMetrics {
  avgLatency: number;
  minLatency: number;
  maxLatency: number;
  p95Latency: number;
  totalRequests: number;
  errorCount: number;
  slowRequestCount: number;
}

export function ApiMetricsPanel() {
  const [isOpen, setIsOpen] = useState(false);
  const [metrics, setMetrics] = useState<Map<string, EndpointMetrics>>(new Map());
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());

  // Refresh metrics every 5 seconds when open
  useEffect(() => {
    const updateMetrics = () => {
      setMetrics(getAllMetrics());
      setLastUpdated(new Date());
    };

    updateMetrics();

    if (isOpen) {
      const interval = setInterval(updateMetrics, 5000);
      return () => clearInterval(interval);
    }
  }, [isOpen]);

  const handleClearMetrics = () => {
    clearMetrics();
    setMetrics(new Map());
  };

  // Calculate totals
  const totalRequests = Array.from(metrics.values()).reduce((sum, m) => sum + m.totalRequests, 0);
  const totalErrors = Array.from(metrics.values()).reduce((sum, m) => sum + m.errorCount, 0);
  const avgLatency = metrics.size > 0
    ? Array.from(metrics.values()).reduce((sum, m) => sum + m.avgLatency, 0) / metrics.size
    : 0;
  const errorRate = totalRequests > 0 ? (totalErrors / totalRequests) * 100 : 0;

  const getLatencyColor = (latency: number) => {
    if (latency < 200) return "text-status-healthy";
    if (latency < 500) return "text-status-warning";
    return "text-status-critical";
  };

  const getLatencyBadge = (latency: number) => {
    if (latency < 200) return "default";
    if (latency < 500) return "secondary";
    return "destructive";
  };

  return (
    <Card className="border-border/50 bg-card/50 backdrop-blur-sm">
      <Collapsible open={isOpen} onOpenChange={setIsOpen}>
        <CollapsibleTrigger asChild>
          <CardHeader className="cursor-pointer hover:bg-muted/30 transition-colors rounded-t-lg">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-primary/10">
                  <Activity className="h-4 w-4 text-primary" />
                </div>
                <div>
                  <CardTitle className="text-sm font-medium">API Performance</CardTitle>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {totalRequests} requests • {avgLatency.toFixed(0)}ms avg
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2 sm:gap-3">
                {errorRate > 0 && (
                  <Badge variant="destructive" className="text-xs">
                    {errorRate.toFixed(1)}% errors
                  </Badge>
                )}
                {isOpen ? (
                  <ChevronUp className="h-4 w-4 text-muted-foreground" />
                ) : (
                  <ChevronDown className="h-4 w-4 text-muted-foreground" />
                )}
              </div>
            </div>
          </CardHeader>
        </CollapsibleTrigger>

        <CollapsibleContent>
          <CardContent className="pt-0 space-y-4">
            {/* Summary Stats */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <div className="p-3 rounded-lg bg-muted/30 border border-border/30">
                <div className="flex items-center gap-2 mb-1">
                  <TrendingUp className="h-3 w-3 text-muted-foreground" />
                  <span className="text-xs text-muted-foreground">Requests</span>
                </div>
                <p className="text-lg font-semibold">{totalRequests}</p>
              </div>
              <div className="p-3 rounded-lg bg-muted/30 border border-border/30">
                <div className="flex items-center gap-2 mb-1">
                  <Clock className="h-3 w-3 text-muted-foreground" />
                  <span className="text-xs text-muted-foreground">Avg Latency</span>
                </div>
                <p className={`text-lg font-semibold ${getLatencyColor(avgLatency)}`}>
                  {avgLatency.toFixed(0)}ms
                </p>
              </div>
              <div className="p-3 rounded-lg bg-muted/30 border border-border/30">
                <div className="flex items-center gap-2 mb-1">
                  <AlertTriangle className="h-3 w-3 text-muted-foreground" />
                  <span className="text-xs text-muted-foreground">Errors</span>
                </div>
                <p className={`text-lg font-semibold ${totalErrors > 0 ? "text-status-critical" : "text-status-healthy"}`}>
                  {totalErrors}
                </p>
              </div>
              <div className="p-3 rounded-lg bg-muted/30 border border-border/30">
                <div className="flex items-center gap-2 mb-1">
                  <Activity className="h-3 w-3 text-muted-foreground" />
                  <span className="text-xs text-muted-foreground">Endpoints</span>
                </div>
                <p className="text-lg font-semibold">{metrics.size}</p>
              </div>
            </div>

            {/* Per-Endpoint Metrics */}
            {metrics.size > 0 && (
              <div className="space-y-2">
                <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                  Endpoint Details
                </h4>
                <div className="space-y-2 max-h-64 overflow-y-auto">
                  {Array.from(metrics.entries()).map(([endpoint, m]) => (
                    <div
                      key={endpoint}
                      className="p-3 rounded-lg bg-muted/20 border border-border/20 space-y-2"
                    >
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <code className="text-xs font-mono text-foreground/80 truncate max-w-[120px] sm:max-w-none">
                          {endpoint.replace("/", "")}
                        </code>
                        <div className="flex items-center gap-2">
                          <Badge variant={getLatencyBadge(m.avgLatency)} className="text-xs">
                            {m.avgLatency.toFixed(0)}ms avg
                          </Badge>
                          {m.errorCount > 0 && (
                            <Badge variant="destructive" className="text-xs">
                              {m.errorCount} errors
                            </Badge>
                          )}
                        </div>
                      </div>
                      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs">
                        <div>
                          <span className="text-muted-foreground">Min:</span>{" "}
                          <span className="font-medium">{m.minLatency.toFixed(0)}ms</span>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Max:</span>{" "}
                          <span className="font-medium">{m.maxLatency.toFixed(0)}ms</span>
                        </div>
                        <div>
                          <span className="text-muted-foreground">P95:</span>{" "}
                          <span className="font-medium">{m.p95Latency.toFixed(0)}ms</span>
                        </div>
                        <div>
                          <span className="text-muted-foreground">Reqs:</span>{" "}
                          <span className="font-medium">{m.totalRequests}</span>
                        </div>
                      </div>
                      {/* Latency bar visualization */}
                      <Progress
                        value={Math.min((m.avgLatency / 1000) * 100, 100)}
                        className="h-1"
                      />
                    </div>
                  ))}
                </div>
              </div>
            )}

            {metrics.size === 0 && (
              <div className="text-center py-6 text-muted-foreground">
                <Activity className="h-8 w-8 mx-auto mb-2 opacity-50" />
                <p className="text-sm">No metrics recorded yet</p>
                <p className="text-xs">API metrics will appear here as requests are made</p>
              </div>
            )}

            {/* Footer */}
            <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2 pt-2 border-t border-border/30">
              <span className="text-xs text-muted-foreground">
                Last updated: {lastUpdated.toLocaleTimeString()}
              </span>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleClearMetrics}
                className="text-xs h-7"
              >
                <RefreshCw className="h-3 w-3 mr-1" />
                Clear Metrics
              </Button>
            </div>
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  );
}

export default ApiMetricsPanel;
