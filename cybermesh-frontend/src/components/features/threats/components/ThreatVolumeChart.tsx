import { useMemo } from "react";
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  ResponsiveContainer,
} from "recharts";
import { Card } from "@/components/ui/card";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Activity, TrendingUp, TrendingDown, Zap, BarChart3 } from "lucide-react";
import type { VolumeDataPoint, SystemStatus } from "@/types/threats";

interface ThreatVolumeChartProps {
  volumeHistory: VolumeDataPoint[];
  systemStatus: SystemStatus;
}

const VOLUME_COLOR = "hsl(var(--status-critical))";
const PEAK_COLOR = "hsl(var(--status-warning))";
const AVG_COLOR = "hsl(var(--chart-1))";

const chartConfig = {
  value: {
    label: "Detections/s",
    color: VOLUME_COLOR,
  },
};

const ThreatVolumeChart = ({ volumeHistory, systemStatus }: ThreatVolumeChartProps) => {
  const isLive = systemStatus === "LIVE";
  const hasData = isLive && volumeHistory.length > 0;

  const stats = useMemo(() => {
    if (!hasData) return { current: 0, peak: 0, avg: 0, total: 0, trend: 0 };

    const values = volumeHistory.map(v => v.value);
    // Round total to avoid floating-point display issues (e.g., 4.000000000000001)
    const total = Math.round(values.reduce((sum, v) => sum + v, 0) * 10) / 10;
    const avg = total / values.length;
    const peak = Math.max(...values);
    const current = values[values.length - 1] || 0;

    // Calculate trend (comparing last 3 to previous 3)
    const recentAvg = values.slice(-3).reduce((s, v) => s + v, 0) / 3;
    const previousAvg = values.slice(-6, -3).reduce((s, v) => s + v, 0) / 3 || recentAvg;
    const trend = previousAvg > 0 ? ((recentAvg - previousAvg) / previousAvg) * 100 : 0;

    return { current, peak, avg, total, trend };
  }, [volumeHistory, hasData]);

  return (
    <div className="glass-frost rounded-lg p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Activity className="h-5 w-5 text-primary" />
          <h3 className="text-lg font-semibold text-foreground">Threat Volume</h3>
        </div>
        <span className="text-xs text-muted-foreground">
          {isLive ? "Detections per second" : "System halted"}
        </span>
      </div>

      <p className="text-sm text-muted-foreground">
        Real-time threat detection rate over time
      </p>

      {/* Legend */}
      <div className="flex items-center justify-center gap-6 text-sm">
        <div className="flex items-center gap-2">
          <div
            className="h-3 w-3 rounded-full"
            style={{ backgroundColor: VOLUME_COLOR }}
          />
          <span className="text-muted-foreground">Detection Rate</span>
        </div>
      </div>

      {/* Chart */}
      <div className="h-[250px]">
        {hasData ? (
          <ChartContainer config={chartConfig} className="h-full w-full">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart
                data={volumeHistory}
                margin={{ top: 10, right: 10, left: 0, bottom: 0 }}
              >
                <defs>
                  <linearGradient id="threatVolumeGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={VOLUME_COLOR} stopOpacity={0.35} />
                    <stop offset="95%" stopColor={VOLUME_COLOR} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid
                  strokeDasharray="3 3"
                  stroke="hsl(var(--muted-foreground) / 0.2)"
                  vertical={false}
                />
                <XAxis
                  dataKey="time"
                  stroke="hsl(var(--muted-foreground))"
                  fontSize={11}
                  tickLine={false}
                  axisLine={false}
                  tick={{ fill: "hsl(var(--muted-foreground))" }}
                />
                <YAxis
                  stroke="hsl(var(--muted-foreground))"
                  fontSize={11}
                  tickLine={false}
                  axisLine={false}
                  tick={{ fill: "hsl(var(--muted-foreground))" }}
                  tickFormatter={(value) => `${value}`}
                />
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      formatter={(value) => (
                        <div className="flex w-full items-center justify-between gap-4">
                          <span className="text-muted-foreground">Volume</span>
                          <span className="font-semibold text-foreground">{value} detections/s</span>
                        </div>
                      )}
                      labelFormatter={(label) => (
                        <span className="text-xs uppercase tracking-wide text-muted-foreground">{label}</span>
                      )}
                    />
                  }
                />
                <Area
                  type="monotone"
                  dataKey="value"
                  stroke={VOLUME_COLOR}
                  strokeWidth={2}
                  fill="url(#threatVolumeGradient)"
                  dot={{ fill: VOLUME_COLOR, strokeWidth: 2, r: 3 }}
                  activeDot={{ r: 5, strokeWidth: 2 }}
                />
              </AreaChart>
            </ResponsiveContainer>
          </ChartContainer>
        ) : (
          <div className="h-full flex items-center justify-center">
            <div className="text-center">
              <div className="w-full h-24 border-b-2 border-dashed border-muted/30 relative flex items-center justify-center">
                <BarChart3 className="h-12 w-12 text-muted-foreground/30" />
              </div>
              <p className="mt-4 text-sm text-muted-foreground">
                {isLive ? "No detection history" : "System halted"}
              </p>
            </div>
          </div>
        )}
      </div>

      {/* Key Insights */}
      {hasData && (
        <div>
          <h4 className="text-sm font-semibold text-foreground mb-3">Key Insights</h4>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-4">
            {/* Current Rate Card */}
            <Card className="bg-card/40 border border-border/40 p-4 min-h-[110px]">
              <div className="flex items-start justify-between h-full">
                <div className="space-y-1 min-w-0 flex-1">
                  <p className="text-xs text-muted-foreground">Current</p>
                  <p
                    className="text-2xl font-bold"
                    style={{ color: VOLUME_COLOR }}
                  >
                    {stats.current}
                  </p>
                  <p className="text-xs text-muted-foreground truncate">detections/s</p>
                </div>
                <div
                  className="p-2 rounded-lg flex-shrink-0 ml-2"
                  style={{ backgroundColor: "hsl(var(--status-critical) / 0.1)" }}
                >
                  <Zap className="h-4 w-4" style={{ color: VOLUME_COLOR }} />
                </div>
              </div>
            </Card>

            {/* Peak Rate Card */}
            <Card className="bg-card/40 border border-border/40 p-4 min-h-[110px]">
              <div className="flex items-start justify-between h-full">
                <div className="space-y-1 min-w-0 flex-1">
                  <p className="text-xs text-muted-foreground">Peak</p>
                  <p
                    className="text-2xl font-bold"
                    style={{ color: PEAK_COLOR }}
                  >
                    {stats.peak}
                  </p>
                  <p className="text-xs text-muted-foreground truncate">max detections/s</p>
                </div>
                <div
                  className="p-2 rounded-lg flex-shrink-0 ml-2"
                  style={{ backgroundColor: "hsl(var(--status-warning) / 0.1)" }}
                >
                  <TrendingUp className="h-4 w-4" style={{ color: PEAK_COLOR }} />
                </div>
              </div>
            </Card>

            {/* Average Rate Card */}
            <Card className="bg-card/40 border border-border/40 p-4 min-h-[110px]">
              <div className="flex items-start justify-between h-full">
                <div className="space-y-1 min-w-0 flex-1">
                  <p className="text-xs text-muted-foreground">Average</p>
                  <p
                    className="text-2xl font-bold"
                    style={{ color: AVG_COLOR }}
                  >
                    {stats.avg.toFixed(1)}
                  </p>
                  <p className="text-xs text-muted-foreground truncate">detections/s</p>
                </div>
                <div
                  className="p-2 rounded-lg flex-shrink-0 ml-2"
                  style={{ backgroundColor: "hsl(var(--chart-1) / 0.1)" }}
                >
                  <Activity className="h-4 w-4" style={{ color: AVG_COLOR }} />
                </div>
              </div>
            </Card>

            {/* Trend Card */}
            <Card className="bg-card/40 border border-border/40 p-4 min-h-[110px]">
              <div className="flex items-start justify-between h-full">
                <div className="space-y-1 min-w-0 flex-1">
                  <p className="text-xs text-muted-foreground">Trend</p>
                  <p
                    className="text-2xl font-bold"
                    style={{
                      color: stats.trend > 0
                        ? VOLUME_COLOR
                        : stats.trend < 0
                          ? "hsl(var(--status-healthy))"
                          : "hsl(var(--muted-foreground))"
                    }}
                  >
                    {stats.trend > 0 ? "+" : ""}{stats.trend.toFixed(1)}%
                  </p>
                  <p className="text-xs text-muted-foreground truncate">vs previous period</p>
                </div>
                <div
                  className="p-2 rounded-lg flex-shrink-0 ml-2"
                  style={{
                    backgroundColor: stats.trend > 0
                      ? "hsl(var(--status-critical) / 0.1)"
                      : "hsl(var(--status-healthy) / 0.1)"
                  }}
                >
                  {stats.trend >= 0 ? (
                    <TrendingUp className="h-4 w-4" style={{ color: stats.trend > 0 ? VOLUME_COLOR : "hsl(var(--muted-foreground))" }} />
                  ) : (
                    <TrendingDown className="h-4 w-4" style={{ color: "hsl(var(--status-healthy))" }} />
                  )}
                </div>
              </div>
            </Card>
          </div>
        </div>
      )}

      {/* Summary Footer */}
      {hasData && (
        <div className="grid gap-4 lg:grid-cols-2">
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Detection Summary</p>
            <p className="mt-2 text-sm text-muted-foreground">
              <span className="font-semibold text-foreground">{stats.total}</span> total detections across{" "}
              <span className="font-semibold text-foreground">{volumeHistory.length}</span> time intervals
            </p>
          </div>
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Volume Status</p>
            <p className="mt-2 text-sm">
              {stats.current > stats.avg * 1.5 ? (
                <span style={{ color: VOLUME_COLOR }}>Above average - elevated threat activity</span>
              ) : stats.current < stats.avg * 0.5 ? (
                <span style={{ color: "hsl(var(--status-healthy))" }}>Below average - reduced activity</span>
              ) : (
                <span className="text-muted-foreground">Normal range - monitoring active</span>
              )}
            </p>
          </div>
        </div>
      )}
    </div>
  );
};

export default ThreatVolumeChart;