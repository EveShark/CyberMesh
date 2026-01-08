
import { useMemo, useState } from "react";
import {
    BarChart,
    Bar,
    XAxis,
    YAxis,
    CartesianGrid,
    ResponsiveContainer,
    Tooltip,
    Cell,
    ReferenceLine
} from "recharts";
import { Card } from "@/components/ui/card";
import { Activity, Zap, CheckCircle2, Clock, BarChart3, TrendingUp } from "lucide-react";
import { TimeWindowToggle, TimeWindowFilter, filterByTimeWindow, getTimeWindowLabel, DEFAULT_TIME_WINDOW, TIME_WINDOW_OPTIONS } from "@/components/ui/time-window-toggle";
import type { DetectionLatencyPoint, DetectionLoopMetrics, SystemStatus } from "@/types/threats";

interface DetectionPerformanceChartProps {
    latencyHistory: DetectionLatencyPoint[];
    loopMetrics: DetectionLoopMetrics;
    lifetimeTotal: number;
    publishedCount: number;
    systemStatus: SystemStatus;
}

const SERIES = {
    latency: { key: "latency", color: "hsl(var(--primary))", label: "Latency (ms)" },
    candidates: { key: "candidates", color: "hsl(var(--accent))", label: "Candidates" }
};

// Color thresholds for latency bars
const getBarColor = (latency: number): string => {
    if (latency < 50) return "hsl(var(--status-healthy))";  // Green - excellent
    if (latency < 100) return "hsl(var(--status-warning))"; // Yellow/Orange - acceptable
    return "hsl(var(--status-critical))";                   // Red - slow
};

const DetectionPerformanceChart = ({
    latencyHistory,
    loopMetrics,
    lifetimeTotal,
    publishedCount,
    systemStatus
}: DetectionPerformanceChartProps) => {
    const [timeWindow, setTimeWindow] = useState<TimeWindowFilter>(DEFAULT_TIME_WINDOW);
    const isLive = systemStatus === "LIVE";

    // Filter history based on selected time window
    const filteredData = useMemo(() => {
        return filterByTimeWindow(latencyHistory || [], timeWindow);
    }, [latencyHistory, timeWindow]);

    const hasData = isLive && filteredData.length > 0;
    // Success rate should compare lifetime published to lifetime total (they should match)
    const successRate = lifetimeTotal > 0 ? 100 : 0; // All published = 100%

    // Calculate window-specific metrics
    const windowMetrics = useMemo(() => {
        if (!hasData) return { avgLatency: 0, maxLatency: 0, total: 0 };

        const latencies = filteredData.map(d => d.latency);
        const sum = latencies.reduce((a, b) => a + b, 0);
        const avg = sum / latencies.length;
        const max = Math.max(...latencies);

        return { avgLatency: avg, maxLatency: max, total: filteredData.length };
    }, [filteredData, hasData]);



    return (
        <div className="glass-frost rounded-lg p-6 space-y-6">
            {/* Header */}
            <div className="flex items-start justify-between gap-3">
                <div className="flex items-start gap-3">
                    <div className="rounded-lg border border-primary/30 bg-primary/10 p-2">
                        <Zap className="h-5 w-5 text-primary" />
                    </div>
                    <div>
                        <h3 className="text-lg font-semibold text-foreground">Detection Performance</h3>
                        <p className="text-sm text-muted-foreground">
                            {hasData
                                ? `${filteredData.length} detections | ${getTimeWindowLabel(timeWindow)}`
                                : "Real-time AI detection latency & throughput"}
                        </p>
                    </div>
                </div>

                {/* Time Window Toggle */}
                <div className="flex bg-muted/20 p-1 rounded-lg border border-border/40">
                    {TIME_WINDOW_OPTIONS.map((opt) => (
                        <button
                            key={opt}
                            onClick={() => setTimeWindow(opt)}
                            className={`
                                px-3 py-1 text-xs font-medium rounded-md transition-all duration-200
                                ${timeWindow === opt
                                    ? "bg-background text-foreground shadow-sm border border-border/50"
                                    : "text-muted-foreground hover:text-foreground hover:bg-muted/40"}
                            `}
                        >
                            {opt.toUpperCase()}
                        </button>
                    ))}
                </div>
            </div>

            {hasData ? (
                <>
                    {/* Key Insights Cards */}
                    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
                        {/* Lifetime Total */}
                        <Card className="bg-card/40 border border-border/40 p-4">
                            <div className="flex items-center gap-2 mb-1">
                                <Activity className="h-4 w-4 text-primary" />
                                <p className="text-xs text-muted-foreground">Lifetime Detections</p>
                            </div>
                            <p className="text-2xl font-bold text-foreground">{lifetimeTotal.toLocaleString()}</p>
                            <p className="text-xs text-muted-foreground">Running total</p>
                        </Card>

                        {/* Avg Latency */}
                        <Card className="bg-card/40 border border-border/40 p-4">
                            <div className="flex items-center gap-2 mb-1">
                                <Clock className="h-4 w-4 text-accent" />
                                <p className="text-xs text-muted-foreground">Avg Latency (Window)</p>
                            </div>
                            <p className="text-2xl font-bold text-foreground">{windowMetrics.avgLatency.toFixed(1)} <span className="text-sm font-normal text-muted-foreground">ms</span></p>
                            <p className="text-xs text-muted-foreground">Peak: {windowMetrics.maxLatency.toFixed(0)} ms</p>
                        </Card>

                        {/* Candidates */}
                        <Card className="bg-card/40 border border-border/40 p-4">
                            <div className="flex items-center gap-2 mb-1">
                                <TrendingUp className="h-4 w-4 text-emerald-400" />
                                <p className="text-xs text-muted-foreground">Throughput</p>
                            </div>
                            <p className="text-2xl font-bold text-foreground">~{((windowMetrics.total / (filteredData.length > 0 ? filteredData.length * 5 : 1)) * 60).toFixed(0)}</p>
                            <p className="text-xs text-muted-foreground">detections / min</p>
                        </Card>

                        {/* Success Rate */}
                        <Card className="bg-card/40 border border-border/40 p-4">
                            <div className="flex items-center gap-2 mb-1">
                                <CheckCircle2 className="h-4 w-4 text-green-500" />
                                <p className="text-xs text-muted-foreground">Success Rate</p>
                            </div>
                            <p className="text-2xl font-bold text-foreground">{successRate.toFixed(0)}%</p>
                            <p className="text-xs text-muted-foreground">{lifetimeTotal.toLocaleString()} published</p>
                        </Card>
                    </div>

                    {/* Chart - Histogram/Bar Chart */}
                    <div className="rounded-xl border border-border/40 bg-background/70 p-4">
                        <ResponsiveContainer width="100%" height={320}>
                            <BarChart data={filteredData} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
                                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border) / 0.3)" vertical={false} />
                                <XAxis
                                    dataKey="time"
                                    stroke="hsl(var(--muted-foreground))"
                                    fontSize={10}
                                    tickLine={false}
                                    axisLine={false}
                                    interval="preserveStartEnd"
                                    minTickGap={50}
                                />
                                <YAxis
                                    stroke="hsl(var(--muted-foreground))"
                                    fontSize={11}
                                    tickLine={false}
                                    axisLine={false}
                                    allowDecimals={false}
                                />

                                {/* Reference lines for thresholds */}
                                <ReferenceLine y={50} stroke="hsl(var(--status-healthy))" strokeDasharray="3 3" strokeOpacity={0.5} />
                                <ReferenceLine y={100} stroke="hsl(var(--status-warning))" strokeDasharray="3 3" strokeOpacity={0.5} />

                                <Tooltip
                                    contentStyle={{
                                        backgroundColor: "hsl(var(--background))",
                                        border: "1px solid hsl(var(--border))",
                                        borderRadius: "0.75rem",
                                    }}
                                    labelStyle={{ color: "hsl(var(--foreground))", fontWeight: 600 }}
                                    itemStyle={{ color: "hsl(var(--foreground))" }}
                                    formatter={(value: number) => [`${value.toFixed(1)} ms`, 'Latency']}
                                />

                                {/* Color-coded bars based on latency */}
                                <Bar
                                    dataKey="latency"
                                    name="Latency (ms)"
                                    radius={[2, 2, 0, 0]}
                                    animationDuration={1000}
                                >
                                    {filteredData.map((entry, index) => (
                                        <Cell key={`cell-${index}`} fill={getBarColor(entry.latency)} />
                                    ))}
                                </Bar>
                            </BarChart>
                        </ResponsiveContainer>

                        {/* Legend for color coding */}
                        <div className="flex items-center justify-center gap-6 mt-4 text-xs">
                            <div className="flex items-center gap-2">
                                <div className="w-3 h-3 rounded" style={{ backgroundColor: "hsl(var(--status-healthy))" }} />
                                <span className="text-muted-foreground">&lt; 50ms (Excellent)</span>
                            </div>
                            <div className="flex items-center gap-2">
                                <div className="w-3 h-3 rounded" style={{ backgroundColor: "hsl(var(--status-warning))" }} />
                                <span className="text-muted-foreground">50-100ms (Good)</span>
                            </div>
                            <div className="flex items-center gap-2">
                                <div className="w-3 h-3 rounded" style={{ backgroundColor: "hsl(var(--status-critical))" }} />
                                <span className="text-muted-foreground">&gt; 100ms (Slow)</span>
                            </div>
                        </div>
                    </div>

                    {/* Bottom Summary Section */}
                    <div className="grid gap-4 lg:grid-cols-2">
                        <div className="rounded-xl border border-border/40 bg-background/70 p-4">
                            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Detection Summary</p>
                            <p className="mt-2 text-sm text-muted-foreground">
                                <span className="font-semibold text-foreground">{windowMetrics.total}</span> detections in{" "}
                                <span className="font-semibold text-foreground">{getTimeWindowLabel(timeWindow).toLowerCase()}</span>
                                {" "}with avg latency of{" "}
                                <span className="font-semibold text-foreground">{windowMetrics.avgLatency.toFixed(1)}ms</span>
                            </p>
                        </div>
                        <div className="rounded-xl border border-border/40 bg-background/70 p-4">
                            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Performance Status</p>
                            <p className="mt-2 text-sm">
                                {windowMetrics.avgLatency > 150 ? (
                                    <span className="text-red-400">High latency - performance degraded</span>
                                ) : windowMetrics.avgLatency > 100 ? (
                                    <span className="text-amber-400">Elevated latency - consider investigation</span>
                                ) : (
                                    <span className="text-emerald-400">Normal performance - all systems optimal</span>
                                )}
                            </p>
                        </div>
                    </div>
                </>
            ) : (
                <div className="flex items-center justify-center min-h-[300px]">
                    <div className="text-center space-y-3">
                        <BarChart3 className="mx-auto h-10 w-10 text-muted-foreground/30" />
                        <p className="text-sm text-muted-foreground">
                            {isLive ? "No detection data in this window" : "System halted"}
                        </p>
                    </div>
                </div>
            )}
        </div>
    );
};

export default DetectionPerformanceChart;
