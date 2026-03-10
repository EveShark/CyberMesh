import React, { useState, useEffect, useMemo } from 'react';
import { Loader2, TrendingUp, Activity, GitCommit, RefreshCw, BarChart3, Zap } from 'lucide-react';
import { LineChart, Line, AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';

// Mock data generators
const generateVoteData = () => {
  const data = [];
  const now = Date.now();
  for (let i = 10; i >= 0; i--) {
    const timestamp = now - i * 30000; // 30 second intervals
    data.push({
      type: 'proposal',
      count: Math.floor(Math.random() * 10) + 5,
      timestamp
    });
    data.push({
      type: 'vote',
      count: Math.floor(Math.random() * 25) + 15,
      timestamp
    });
    data.push({
      type: 'commit',
      count: Math.floor(Math.random() * 8) + 3,
      timestamp
    });
    if (Math.random() > 0.7) {
      data.push({
        type: 'view_change',
        count: Math.floor(Math.random() * 3) + 1,
        timestamp
      });
    }
  }
  return data;
};

const generateProposalData = () => {
  const data = [];
  const now = Date.now();
  for (let i = 0; i < 50; i++) {
    data.push({
      block: 1000 + i,
      view: 1,
      timestamp: now - (50 - i) * 120000, // 2 min intervals
      proposer: `node-${(i % 5) + 1}`,
      hash: `0x${Math.random().toString(16).slice(2, 10)}`
    });
  }
  return data;
};

// Custom Tooltip Component
function CustomTooltip({ active, payload, label }) {
  if (active && payload && payload.length) {
    return (
      <div className="backdrop-blur-xl bg-slate-800/95 rounded-lg border border-white/20 p-3 shadow-2xl">
        <p className="text-xs text-slate-300 font-semibold mb-2">{label}</p>
        {payload.map((entry, index) => (
          <div key={index} className="flex items-center justify-between gap-4 text-xs">
            <span className="flex items-center gap-2">
              <div className="w-2 h-2 rounded-full" style={{ backgroundColor: entry.color }}></div>
              <span className="text-slate-400">{entry.name}</span>
            </span>
            <span className="font-bold text-white">{entry.value}</span>
          </div>
        ))}
      </div>
    );
  }
  return null;
}

// Vote Timeline Component
function VoteTimeline({ votes, isLoading }) {
  const chartData = useMemo(() => {
    if (!votes || votes.length === 0) return [];

    const voteMap = new Map();

    votes.forEach((v) => {
      if (!voteMap.has(v.timestamp)) {
        voteMap.set(v.timestamp, { timestamp: v.timestamp, proposals: 0, votes: 0, commits: 0, view_changes: 0 });
      }

      const entry = voteMap.get(v.timestamp);
      const type = v.type.toLowerCase();

      if (type.includes('proposal')) {
        entry.proposals += v.count;
      } else if (type.includes('commit')) {
        entry.commits += v.count;
      } else if (type.includes('view')) {
        entry.view_changes += v.count;
      } else {
        entry.votes += v.count;
      }
    });

    return Array.from(voteMap.values())
      .sort((a, b) => a.timestamp - b.timestamp)
      .map((entry) => ({
        ...entry,
        time: new Date(entry.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
      }));
  }, [votes]);

  const currentActivity = useMemo(() => {
    if (chartData.length === 0) {
      return { proposals: 0, votes: 0, commits: 0, view_changes: 0, total: 0 };
    }
    const last = chartData[chartData.length - 1];
    return {
      ...last,
      total: last.proposals + last.votes + last.commits + last.view_changes
    };
  }, [chartData]);

  const totalActivity = useMemo(() => {
    return chartData.reduce((sum, entry) => 
      sum + entry.proposals + entry.votes + entry.commits + entry.view_changes, 0
    );
  }, [chartData]);

  if (isLoading) {
    return (
      <div className="backdrop-blur-xl bg-slate-800/60 rounded-xl lg:rounded-2xl border border-white/10 shadow-2xl">
        <div className="p-4 lg:p-6 border-b border-white/10">
          <h3 className="text-base lg:text-lg font-semibold text-white flex items-center gap-2">
            <Activity className="h-5 w-5 text-blue-400" />
            Vote Timeline
          </h3>
          <p className="text-xs lg:text-sm text-slate-400">Consensus activity (30s buckets)</p>
        </div>
        <div className="p-4 lg:p-6">
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="text-center space-y-3">
              <Loader2 className="h-6 w-6 animate-spin text-blue-400 mx-auto" />
              <p className="text-sm text-slate-400">Loading vote data...</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  const hasData = chartData.length > 0;

  return (
    <div className="backdrop-blur-xl bg-slate-800/60 rounded-xl lg:rounded-2xl border border-white/10 shadow-2xl">
      <div className="p-4 lg:p-6 border-b border-white/10">
        <h3 className="text-base lg:text-lg font-semibold text-white flex items-center gap-2">
          <Activity className="h-5 w-5 text-blue-400" />
          Vote Timeline
        </h3>
        <p className="text-xs lg:text-sm text-slate-400">Consensus activity over time</p>
      </div>
      <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
        {hasData ? (
          <>
            {/* Stats Cards */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <div className="p-3 rounded-lg bg-gradient-to-br from-emerald-500/10 to-emerald-500/5 border border-emerald-500/20">
                <div className="flex items-center gap-2 mb-1">
                  <GitCommit className="h-4 w-4 text-emerald-400" />
                  <p className="text-xs text-emerald-400 font-medium">Proposals</p>
                </div>
                <p className="text-xl lg:text-2xl font-bold text-white">{currentActivity.proposals}</p>
                <p className="text-xs text-slate-400">last 30s</p>
              </div>
              
              <div className="p-3 rounded-lg bg-gradient-to-br from-blue-500/10 to-blue-500/5 border border-blue-500/20">
                <div className="flex items-center gap-2 mb-1">
                  <Zap className="h-4 w-4 text-blue-400" />
                  <p className="text-xs text-blue-400 font-medium">Votes</p>
                </div>
                <p className="text-xl lg:text-2xl font-bold text-white">{currentActivity.votes}</p>
                <p className="text-xs text-slate-400">last 30s</p>
              </div>
              
              <div className="p-3 rounded-lg bg-gradient-to-br from-amber-500/10 to-amber-500/5 border border-amber-500/20">
                <div className="flex items-center gap-2 mb-1">
                  <BarChart3 className="h-4 w-4 text-amber-400" />
                  <p className="text-xs text-amber-400 font-medium">Commits</p>
                </div>
                <p className="text-xl lg:text-2xl font-bold text-white">{currentActivity.commits}</p>
                <p className="text-xs text-slate-400">last 30s</p>
              </div>
              
              <div className="p-3 rounded-lg bg-gradient-to-br from-red-500/10 to-red-500/5 border border-red-500/20">
                <div className="flex items-center gap-2 mb-1">
                  <RefreshCw className="h-4 w-4 text-red-400" />
                  <p className="text-xs text-red-400 font-medium">View Changes</p>
                </div>
                <p className="text-xl lg:text-2xl font-bold text-white">{currentActivity.view_changes}</p>
                <p className="text-xs text-slate-400">last 30s</p>
              </div>
            </div>

            {/* Chart */}
            <div className="rounded-xl bg-slate-900/50 border border-white/5 p-4">
              <ResponsiveContainer width="100%" height={300}>
                <AreaChart data={chartData}>
                  <defs>
                    <linearGradient id="colorProposals" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#10b981" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#10b981" stopOpacity={0}/>
                    </linearGradient>
                    <linearGradient id="colorVotes" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#3b82f6" stopOpacity={0}/>
                    </linearGradient>
                    <linearGradient id="colorCommits" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#f59e0b" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#f59e0b" stopOpacity={0}/>
                    </linearGradient>
                    <linearGradient id="colorViewChanges" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#ef4444" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#ef4444" stopOpacity={0}/>
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="rgba(148, 163, 184, 0.1)" vertical={false} />
                  <XAxis
                    dataKey="time"
                    stroke="#64748b"
                    fontSize={10}
                    tickLine={false}
                    axisLine={false}
                    interval="preserveStartEnd"
                  />
                  <YAxis
                    stroke="#64748b"
                    fontSize={11}
                    tickLine={false}
                    axisLine={false}
                    allowDecimals={false}
                  />
                  <Tooltip content={<CustomTooltip />} />
                  <Area
                    type="monotone"
                    dataKey="proposals"
                    stackId="1"
                    stroke="#10b981"
                    strokeWidth={2}
                    fill="url(#colorProposals)"
                    name="Proposals"
                  />
                  <Area
                    type="monotone"
                    dataKey="votes"
                    stackId="1"
                    stroke="#3b82f6"
                    strokeWidth={2}
                    fill="url(#colorVotes)"
                    name="Votes"
                  />
                  <Area
                    type="monotone"
                    dataKey="commits"
                    stackId="1"
                    stroke="#f59e0b"
                    strokeWidth={2}
                    fill="url(#colorCommits)"
                    name="Commits"
                  />
                  <Area
                    type="monotone"
                    dataKey="view_changes"
                    stackId="1"
                    stroke="#ef4444"
                    strokeWidth={2}
                    fill="url(#colorViewChanges)"
                    name="View Changes"
                  />
                </AreaChart>
              </ResponsiveContainer>
            </div>

            {/* Summary */}
            <div className="p-4 rounded-xl bg-slate-700/30 border border-white/10">
              <div className="flex items-center justify-between flex-wrap gap-2">
                <p className="text-sm text-slate-300 font-medium">Total Activity</p>
                <p className="text-2xl font-bold text-white">{totalActivity} messages</p>
              </div>
              <p className="text-xs text-slate-400 mt-1">
                Tracking {chartData.length} time buckets over {Math.floor(chartData.length * 30 / 60)} minutes
              </p>
            </div>
          </>
        ) : (
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="text-center space-y-3">
              <Activity className="h-12 w-12 text-slate-600 mx-auto" />
              <p className="text-sm text-slate-400">No vote activity data available</p>
              <p className="text-xs text-slate-500">Waiting for consensus activity...</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// Proposal Chart Component
function ProposalChart({ proposals, isLoading }) {
  const chartData = useMemo(() => {
    if (!proposals || proposals.length === 0) return [];

    const groupedByHour = {};

    proposals.forEach((p) => {
      const date = new Date(p.timestamp);
      const hourKey = `${date.getHours().toString().padStart(2, '0')}:00`;
      groupedByHour[hourKey] = (groupedByHour[hourKey] || 0) + 1;
    });

    return Object.entries(groupedByHour)
      .map(([hour, count]) => ({ hour, count }))
      .sort((a, b) => a.hour.localeCompare(b.hour));
  }, [proposals]);

  const stats = useMemo(() => {
    if (!proposals || proposals.length === 0) {
      return { total: 0, peakRate: 0, avgRate: 0 };
    }

    const total = proposals.length;
    const counts = chartData.map((d) => d.count);
    const maxCount = counts.length > 0 ? Math.max(...counts) : 0;
    const timeSpanHours = chartData.length || 1;
    const avgRate = (total / timeSpanHours).toFixed(1);

    return {
      total,
      peakRate: maxCount,
      avgRate,
    };
  }, [proposals, chartData]);

  if (isLoading) {
    return (
      <div className="backdrop-blur-xl bg-slate-800/60 rounded-xl lg:rounded-2xl border border-white/10 shadow-2xl">
        <div className="p-4 lg:p-6 border-b border-white/10">
          <h3 className="text-base lg:text-lg font-semibold text-white flex items-center gap-2">
            <TrendingUp className="h-5 w-5 text-emerald-400" />
            Proposal Activity
          </h3>
          <p className="text-xs lg:text-sm text-slate-400">Recent proposal submissions</p>
        </div>
        <div className="p-4 lg:p-6">
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="text-center space-y-3">
              <Loader2 className="h-6 w-6 animate-spin text-emerald-400 mx-auto" />
              <p className="text-sm text-slate-400">Loading proposal data...</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  const hasData = chartData.length > 0;

  return (
    <div className="backdrop-blur-xl bg-slate-800/60 rounded-xl lg:rounded-2xl border border-white/10 shadow-2xl">
      <div className="p-4 lg:p-6 border-b border-white/10">
        <h3 className="text-base lg:text-lg font-semibold text-white flex items-center gap-2">
          <TrendingUp className="h-5 w-5 text-emerald-400" />
          Proposal Activity
        </h3>
        <p className="text-xs lg:text-sm text-slate-400">
          {proposals.length > 0 ? `Recent ${proposals.length} proposals` : 'Recent proposal submissions'}
        </p>
      </div>
      <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
        {hasData ? (
          <>
            {/* Stats Cards */}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 lg:gap-4">
              <div className="p-4 rounded-xl bg-gradient-to-br from-emerald-500/10 to-emerald-500/5 border border-emerald-500/20">
                <p className="text-xs text-emerald-400 font-medium mb-1">Total Proposals</p>
                <p className="text-3xl lg:text-4xl font-bold text-white">{stats.total}</p>
                <p className="text-xs text-slate-400 mt-1">all time</p>
              </div>
              <div className="p-4 rounded-xl bg-gradient-to-br from-blue-500/10 to-blue-500/5 border border-blue-500/20">
                <p className="text-xs text-blue-400 font-medium mb-1">Peak Rate</p>
                <p className="text-3xl lg:text-4xl font-bold text-white">{stats.peakRate}</p>
                <p className="text-xs text-slate-400 mt-1">per hour</p>
              </div>
              <div className="p-4 rounded-xl bg-gradient-to-br from-purple-500/10 to-purple-500/5 border border-purple-500/20">
                <p className="text-xs text-purple-400 font-medium mb-1">Average Rate</p>
                <p className="text-3xl lg:text-4xl font-bold text-white">{stats.avgRate}</p>
                <p className="text-xs text-slate-400 mt-1">per hour</p>
              </div>
            </div>

            {/* Chart */}
            <div className="rounded-xl bg-slate-900/50 border border-white/5 p-4">
              <ResponsiveContainer width="100%" height={300}>
                <LineChart data={chartData}>
                  <defs>
                    <linearGradient id="lineGradient" x1="0" y1="0" x2="1" y2="0">
                      <stop offset="0%" stopColor="#10b981" />
                      <stop offset="100%" stopColor="#3b82f6" />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="rgba(148, 163, 184, 0.1)" vertical={false} />
                  <XAxis
                    dataKey="hour"
                    stroke="#64748b"
                    fontSize={11}
                    tickLine={false}
                    axisLine={false}
                  />
                  <YAxis
                    stroke="#64748b"
                    fontSize={11}
                    tickLine={false}
                    axisLine={false}
                    allowDecimals={false}
                  />
                  <Tooltip content={<CustomTooltip />} />
                  <Line
                    type="monotone"
                    dataKey="count"
                    stroke="url(#lineGradient)"
                    strokeWidth={3}
                    dot={{ fill: '#10b981', r: 5, strokeWidth: 2, stroke: '#0f172a' }}
                    activeDot={{ r: 7, strokeWidth: 2, stroke: '#10b981' }}
                    name="Proposals"
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>

            {/* Insights */}
            <div className="p-4 rounded-xl bg-slate-700/30 border border-white/10">
              <div className="flex items-start gap-3">
                <div className="p-2 rounded-lg bg-emerald-500/10 border border-emerald-500/20">
                  <TrendingUp className="h-5 w-5 text-emerald-400" />
                </div>
                <div className="flex-1">
                  <p className="text-sm font-semibold text-white mb-1">Network Performance</p>
                  <p className="text-xs text-slate-400">
                    {stats.avgRate > 5 
                      ? 'High proposal throughput indicates active consensus participation'
                      : 'Moderate proposal activity - network operating within normal parameters'}
                  </p>
                </div>
              </div>
            </div>
          </>
        ) : (
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="text-center space-y-3">
              <BarChart3 className="h-12 w-12 text-slate-600 mx-auto" />
              <p className="text-sm text-slate-400">No proposal data available</p>
              <p className="text-xs text-slate-500">Waiting for consensus activity...</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// Main Dashboard
export default function ConsensusCharts() {
  const [voteData, setVoteData] = useState(null);
  const [proposalData, setProposalData] = useState(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    setTimeout(() => {
      setVoteData(generateVoteData());
      setProposalData(generateProposalData());
      setIsLoading(false);
    }, 1500);

    const interval = setInterval(() => {
      setVoteData(generateVoteData());
      setProposalData(generateProposalData());
    }, 5000);

    return () => clearInterval(interval);
  }, []);

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-900 via-slate-800 to-slate-900 p-4 lg:p-8">
      <div className="max-w-7xl mx-auto space-y-6 lg:space-y-8">
        {/* Header */}
        <div className="text-center space-y-2">
          <h1 className="text-3xl lg:text-5xl font-bold bg-gradient-to-r from-emerald-400 via-blue-400 to-purple-400 bg-clip-text text-transparent">
            Consensus Activity Dashboard
          </h1>
          <p className="text-slate-400 text-sm lg:text-lg">
            Real-time Byzantine consensus metrics and proposal tracking
          </p>
        </div>

        {/* Vote Timeline */}
        <VoteTimeline votes={voteData} isLoading={isLoading} />

        {/* Proposal Chart */}
        <ProposalChart proposals={proposalData} isLoading={isLoading} />

        {/* Footer */}
        <div className="text-center pb-4">
          <div className="inline-flex items-center gap-2 px-3 lg:px-4 py-2 rounded-full bg-slate-800/60 border border-white/10 text-xs text-slate-400">
            <div className="w-2 h-2 bg-emerald-400 rounded-full animate-pulse"></div>
            Live • Updates every 5 seconds
          </div>
        </div>
      </div>
    </div>
  );
}