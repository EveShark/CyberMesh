import { useMemo } from "react";
import { PieChart, Pie, Cell, Tooltip } from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Shield, AlertTriangle, AlertCircle, Info, ShieldAlert } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { SeverityDistribution, SystemStatus, DataWindow } from "@/types/threats";

interface ThreatSeverityChartProps {
  severity: SeverityDistribution;
  systemStatus: SystemStatus;
  dataWindow?: DataWindow;
}

const SEVERITY_COLORS = {
  critical: "hsl(var(--status-critical))",
  high: "hsl(var(--chart-5))",           // Orange/Fire
  medium: "hsl(var(--status-warning))",  // Amber
  low: "hsl(var(--status-healthy))",     // Green
};

const chartConfig = {
  critical: { label: "Critical", color: SEVERITY_COLORS.critical },
  high: { label: "High", color: SEVERITY_COLORS.high },
  medium: { label: "Medium", color: SEVERITY_COLORS.medium },
  low: { label: "Low", color: SEVERITY_COLORS.low },
};

const ThreatSeverityChart = ({ severity, systemStatus, dataWindow }: ThreatSeverityChartProps) => {
  const isLive = systemStatus === "LIVE";

  const data = useMemo(() => [
    { name: "Critical", value: severity.critical.count, percent: severity.critical.percent, color: SEVERITY_COLORS.critical, key: "critical" },
    { name: "High", value: severity.high.count, percent: severity.high.percent, color: SEVERITY_COLORS.high, key: "high" },
    { name: "Medium", value: severity.medium.count, percent: severity.medium.percent, color: SEVERITY_COLORS.medium, key: "medium" },
    { name: "Low", value: severity.low.count, percent: severity.low.percent, color: SEVERITY_COLORS.low, key: "low" },
  ], [severity]);

  const totalCount = data.reduce((sum, item) => sum + item.value, 0);
  const hasData = isLive && totalCount > 0;

  const criticalPercent = totalCount > 0 ? (severity.critical.count / totalCount) * 100 : 0;
  const highRiskCount = severity.critical.count + severity.high.count;
  const lowRiskCount = severity.medium.count + severity.low.count;

  return (
    <div className="glass-frost rounded-lg p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <ShieldAlert className="h-5 w-5 text-primary" />
          <h3 className="text-lg font-semibold text-foreground">Threat Severity Distribution</h3>
        </div>
        <div className="flex items-center gap-2">
          {dataWindow && dataWindow.itemCount > 0 && (
            <Badge variant="outline" className="text-xs font-normal">
              {dataWindow.itemCount} detections | {dataWindow.timeRangeLabel}
            </Badge>
          )}
          <span className="text-xs text-muted-foreground">
            {isLive ? "Real-time analysis" : "System halted"}
          </span>
        </div>
      </div>

      <p className="text-sm text-muted-foreground">
        Distribution of detected threats by severity level
      </p>


      {/* Legend */}
      <div className="flex flex-wrap items-center justify-center gap-4 text-sm">
        {data.map((item) => (
          <div key={item.key} className="flex items-center gap-2">
            <div
              className="h-3 w-3 rounded-full"
              style={{ backgroundColor: item.color }}
            />
            <span className="text-muted-foreground">{item.name}</span>
          </div>
        ))}
      </div>

      {/* Chart */}
      <div className="h-[250px] w-full">
        {hasData ? (
          <ChartContainer config={chartConfig} className="h-full w-full">
            <PieChart>
              <Pie
                data={data}
                cx="50%"
                cy="50%"
                innerRadius={60}
                outerRadius={90}
                paddingAngle={3}
                dataKey="value"
                strokeWidth={2}
                stroke="hsl(var(--background))"
              >
                {data.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={entry.color} />
                ))}
              </Pie>
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    formatter={(value, name) => (
                      <div className="flex w-full items-center justify-between gap-4">
                        <span className="text-muted-foreground">{name}</span>
                        <span className="font-semibold text-foreground">{value} threats</span>
                      </div>
                    )}
                  />
                }
              />
            </PieChart>
          </ChartContainer>
        ) : (
          <div className="h-full flex items-center justify-center">
            <div className="text-center">
              <div className="w-32 h-32 mx-auto rounded-full border-4 border-muted/30 flex items-center justify-center">
                <Shield className="h-12 w-12 text-muted-foreground/50" />
              </div>
              <p className="mt-4 text-sm text-muted-foreground">
                {isLive ? "No threats detected" : "System halted"}
              </p>
            </div>
          </div>
        )}
      </div>

      {/* Key Insights */}
      {hasData && (
        <div>
          <h4 className="text-sm font-semibold text-foreground mb-3">Key Insights</h4>
          <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-3">
            {/* Critical Rate Card */}
            <Card className="bg-card/40 border border-border/40 p-4 min-h-[110px]">
              <div className="flex items-start justify-between h-full">
                <div className="space-y-1 min-w-0 flex-1">
                  <p className="text-xs text-muted-foreground">Critical Rate</p>
                  <p
                    className="text-2xl font-bold"
                    style={{ color: SEVERITY_COLORS.critical }}
                  >
                    {criticalPercent.toFixed(1)}%
                  </p>
                  <p className="text-xs text-muted-foreground truncate">
                    {severity.critical.count} critical threats
                  </p>
                </div>
                <div
                  className="p-2 rounded-lg flex-shrink-0 ml-2"
                  style={{ backgroundColor: "hsl(var(--status-critical) / 0.1)" }}
                >
                  <AlertCircle className="h-4 w-4" style={{ color: SEVERITY_COLORS.critical }} />
                </div>
              </div>
            </Card>

            {/* High Risk Card */}
            <Card className="bg-card/40 border border-border/40 p-4 min-h-[110px]">
              <div className="flex items-start justify-between h-full">
                <div className="space-y-1 min-w-0 flex-1">
                  <p className="text-xs text-muted-foreground">High Risk</p>
                  <p
                    className="text-2xl font-bold"
                    style={{ color: highRiskCount > 0 ? SEVERITY_COLORS.high : "hsl(var(--muted-foreground))" }}
                  >
                    {highRiskCount.toLocaleString()}
                  </p>
                  <p className="text-xs text-muted-foreground truncate">
                    critical + high severity
                  </p>
                </div>
                <div
                  className="p-2 rounded-lg flex-shrink-0 ml-2"
                  style={{ backgroundColor: "hsl(var(--chart-5) / 0.1)" }}
                >
                  <AlertTriangle className="h-4 w-4" style={{ color: SEVERITY_COLORS.high }} />
                </div>
              </div>
            </Card>

            {/* Low Risk Card */}
            <Card className="bg-card/40 border border-border/40 p-4 min-h-[110px]">
              <div className="flex items-start justify-between h-full">
                <div className="space-y-1 min-w-0 flex-1">
                  <p className="text-xs text-muted-foreground">Low Risk</p>
                  <p
                    className="text-2xl font-bold"
                    style={{ color: lowRiskCount > 0 ? SEVERITY_COLORS.low : "hsl(var(--muted-foreground))" }}
                  >
                    {lowRiskCount.toLocaleString()}
                  </p>
                  <p className="text-xs text-muted-foreground truncate">
                    medium + low severity
                  </p>
                </div>
                <div
                  className="p-2 rounded-lg flex-shrink-0 ml-2"
                  style={{ backgroundColor: "hsl(var(--status-healthy) / 0.1)" }}
                >
                  <Info className="h-4 w-4" style={{ color: SEVERITY_COLORS.low }} />
                </div>
              </div>
            </Card>
          </div>
        </div>
      )}

      {/* Severity Breakdown - Mobile Friendly */}
      {hasData && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          {data.map((item) => (
            <div
              key={item.key}
              className="p-3 rounded-xl border border-transparent"
              style={{
                background: `linear-gradient(135deg, ${item.color}15, ${item.color}05)`,
                borderColor: `${item.color}30`,
              }}
            >
              <div className="flex items-center gap-2 mb-2">
                <div
                  className="w-2 h-2 rounded-full"
                  style={{ backgroundColor: item.color }}
                />
                <span className="text-xs font-medium" style={{ color: item.color }}>
                  {item.name}
                </span>
              </div>
              <p className="text-xl font-bold text-foreground">{item.value.toLocaleString()}</p>
              <p className="text-xs text-muted-foreground">{item.percent}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default ThreatSeverityChart;