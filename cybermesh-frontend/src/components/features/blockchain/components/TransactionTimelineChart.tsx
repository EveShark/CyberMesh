import { useMemo, useState } from "react";
import { TimelineDataPoint } from "@/types/blockchain";
import { Card } from "@/components/ui/card";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, ResponsiveContainer } from "recharts";
import { Activity, TrendingUp, TrendingDown, Minus } from "lucide-react";
import { TimeWindowToggle, type TimeWindow, getTimeWindowLabel, getTimeWindowMs, DEFAULT_TIME_WINDOW } from "@/components/ui/time-window-toggle";

interface TransactionTimelineChartProps {
  data: TimelineDataPoint[];
}

const chartConfig = {
  approved: {
    label: "Approved",
    color: "hsl(var(--status-healthy))",
  },
  rejected: {
    label: "Rejected",
    color: "hsl(var(--status-warning))",
  },
};

const APPROVED_COLOR = "hsl(var(--status-healthy))";
const REJECTED_COLOR = "hsl(var(--status-warning))";

const TransactionTimelineChart = ({ data }: TransactionTimelineChartProps) => {
  const [timeWindow, setTimeWindow] = useState<TimeWindow>(DEFAULT_TIME_WINDOW);

  const filteredData = useMemo(() => {
    if (!data || data.length === 0) return [];

    const windowMs = getTimeWindowMs(timeWindow);
    const cutoff = Date.now() - windowMs;

    return data.filter((point) => {
      const ts = new Date(point.time).getTime();
      return !isNaN(ts) && ts > cutoff;
    });
  }, [data, timeWindow]);

  const formattedData = useMemo(() => {
    return filteredData.map(item => ({
      ...item,
      time: item.time.split("T").pop()?.replace("Z", "") || item.time,
    }));
  }, [filteredData]);

  const totals = useMemo(() => {
    return filteredData.reduce(
      (acc, point) => {
        acc.approved += point.approved ?? 0;
        acc.rejected += point.rejected ?? 0;
        acc.total += (point.approved ?? 0) + (point.rejected ?? 0);
        return acc;
      },
      { approved: 0, rejected: 0, total: 0 }
    );
  }, [filteredData]);

  const approvalRate = totals.total > 0 ? (totals.approved / totals.total) * 100 : 0;

  // Format number - show whole numbers without decimals
  const formatNumber = (n: number) => Number.isInteger(n) ? n.toString() : n.toFixed(1);

  if (data.length === 0) {
    return (
      <div className="glass-frost rounded-lg p-6">
        <div className="flex items-center gap-2 mb-4">
          <Activity className="h-5 w-5 text-primary" />
          <h3 className="text-lg font-semibold text-foreground">Transaction Analysis Timeline</h3>
        </div>
        <div className="h-64 flex items-center justify-center text-muted-foreground">
          No transaction data available
        </div>
      </div>
    );
  }

  return (
    <div className="glass-frost rounded-lg p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Activity className="h-5 w-5 text-primary" />
          <h3 className="text-lg font-semibold text-foreground">Transaction Analysis Timeline</h3>
        </div>
        <TimeWindowToggle value={timeWindow} onChange={setTimeWindow} />
      </div>

      <p className="text-sm text-muted-foreground">
        {filteredData.length} data points | {getTimeWindowLabel(timeWindow)}
      </p>

      {/* Legend */}
      <div className="flex items-center justify-center gap-6 text-sm">
        <div className="flex items-center gap-2">
          <div
            className="h-3 w-3 rounded-full"
            style={{ backgroundColor: APPROVED_COLOR }}
          />
          <span className="text-muted-foreground">Approved (Normal)</span>
        </div>
        <div className="flex items-center gap-2">
          <div
            className="h-3 w-3 rounded-full"
            style={{ backgroundColor: REJECTED_COLOR }}
          />
          <span className="text-muted-foreground">Rejected (Anomaly)</span>
        </div>
      </div>

      {/* Chart */}
      <ChartContainer config={chartConfig} className="h-64 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={formattedData} margin={{ top: 5, right: 5, left: 5, bottom: 5 }}>
            <defs>
              <linearGradient id="approvedGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={APPROVED_COLOR} stopOpacity={0.4} />
                <stop offset="95%" stopColor={APPROVED_COLOR} stopOpacity={0.05} />
              </linearGradient>
              <linearGradient id="rejectedGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={REJECTED_COLOR} stopOpacity={0.4} />
                <stop offset="95%" stopColor={REJECTED_COLOR} stopOpacity={0.05} />
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
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  formatter={(value, name) => {
                    const label = name === "approved" ? "Approved" : name === "rejected" ? "Rejected" : String(name);
                    return (
                      <div className="flex w-full items-center justify-between gap-4">
                        <span className="text-muted-foreground">{label}</span>
                        <span className="font-semibold text-foreground">{value as number}</span>
                      </div>
                    );
                  }}
                  labelFormatter={(label) => (
                    <span className="text-xs uppercase tracking-wide text-muted-foreground">{label}</span>
                  )}
                />
              }
            />
            <Area
              type="monotone"
              dataKey="approved"
              stroke={APPROVED_COLOR}
              strokeWidth={2}
              fill="url(#approvedGradient)"
              dot={false}
              activeDot={{ r: 5 }}
              name="Approved"
            />
            <Area
              type="monotone"
              dataKey="rejected"
              stroke={REJECTED_COLOR}
              strokeWidth={2}
              fill="url(#rejectedGradient)"
              dot={false}
              activeDot={{ r: 5 }}
              name="Rejected"
            />
          </AreaChart>
        </ResponsiveContainer>
      </ChartContainer>

      {/* Key Insights */}
      <div>
        <h4 className="text-sm font-semibold text-foreground mb-3">Key Insights</h4>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          {/* Approval Rate Card */}
          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Approval Rate</p>
                <p
                  className="text-2xl font-bold"
                  style={{ color: APPROVED_COLOR }}
                >
                  {approvalRate.toFixed(1)}%
                </p>
                <p className="text-xs text-muted-foreground">
                  {formatNumber(totals.approved)} of {formatNumber(totals.total)} total
                </p>
              </div>
              <div
                className="p-2 rounded-lg"
                style={{ backgroundColor: "hsl(var(--status-healthy) / 0.1)" }}
              >
                <TrendingUp className="h-4 w-4" style={{ color: APPROVED_COLOR }} />
              </div>
            </div>
          </Card>

          {/* Rejections Card */}
          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Rejections</p>
                <p
                  className="text-2xl font-bold"
                  style={{ color: totals.rejected > 0.5 ? REJECTED_COLOR : "hsl(var(--muted-foreground))" }}
                >
                  {formatNumber(totals.rejected)}
                </p>
                <p className="text-xs text-muted-foreground">
                  in window
                </p>
              </div>
              <div
                className="p-2 rounded-lg"
                style={{ backgroundColor: "hsl(var(--status-warning) / 0.1)" }}
              >
                {totals.rejected > 0.5 ? (
                  <TrendingDown className="h-4 w-4" style={{ color: REJECTED_COLOR }} />
                ) : (
                  <Minus className="h-4 w-4 text-muted-foreground" />
                )}
              </div>
            </div>
          </Card>

          {/* Total Transactions Card */}
          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Total Analyzed</p>
                <p className="text-2xl font-bold text-foreground">
                  {formatNumber(totals.total)}
                </p>
                <p className="text-xs text-muted-foreground">
                  in window
                </p>
              </div>
              <div
                className="p-2 rounded-lg"
                style={{ backgroundColor: "hsl(var(--primary) / 0.1)" }}
              >
                <Activity className="h-4 w-4 text-primary" />
              </div>
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
};

export default TransactionTimelineChart;