"use client"

import { useMemo } from "react"
import { PBFTStatusCards } from "@/components/pbft-status-cards"
import { SuspiciousNodesTable } from "@/components/suspicious-nodes-table"
import { ConsensusCharts } from "@/components/consensus-charts"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { RefreshCw, Settings } from "lucide-react"
import { useConsensusData } from "@/hooks/use-consensus-data"
import PbftLiveVisualizer, { type VotePhase } from "@/components/pbft-live-visualizer"
import { PageContainer } from "@/components/page-container"

export default function ConsensusPage() {
  const { data, isLoading, error, refresh } = useConsensusData()

  const totalPeers = useMemo(() => {
    if (!data) return 0
    return Math.max(data.quorumSize ?? 0, data.activePeers ?? 0)
  }, [data])

  const proposalSeries = useMemo(
    () =>
      (data?.proposals ?? []).map((proposal) => ({
        height: proposal.block,
        proposer: proposal.proposer ?? proposal.hash ?? "unknown",
        transactions: Math.max(1, proposal.view ?? 1),
        timestamp: proposal.timestamp,
      })),
    [data?.proposals],
  );

  const voteMetrics = useMemo(
    () =>
      (data?.votes ?? []).map((vote) => ({
        label: vote.type,
        value: vote.count,
      })),
    [data?.votes],
  );

  const suspiciousNodes = useMemo(
    () =>
      (data?.suspiciousNodes ?? []).map((node) => ({
        nodeId: node.id,
        suspicionScore: (node.suspicionScore ?? 0) / 100,
        reason: node.reason ?? "—",
        severity: (node.status === "critical"
          ? "high"
          : node.status === "warning"
            ? "medium"
            : "low") as "high" | "medium" | "low",
        lastSeenHeight: undefined,
      })),
    [data?.suspiciousNodes],
  );

  const pbftStatus = useMemo(() => {
    if (!data) {
      return {
        phase: undefined,
        leader: undefined,
        round: undefined,
        term: undefined,
        votes: [],
        updatedAt: undefined,
      }
    }

    const votePhaseMap: Record<string, VotePhase> = {
      proposal: "pre-prepare",
      pre_prepare: "pre-prepare",
      prepare: "prepare",
      vote: "prepare",
      commit: "commit",
      decide: "decide",
      view_change: "none",
    }

    const expandedVotes = (data.votes ?? []).flatMap((vote) => {
      const phase = votePhaseMap[vote.type] ?? "none"
      const sampleSize = Math.min(vote.count, 12)
      return Array.from({ length: sampleSize }, (_, index) => ({
        node: `${vote.type}-${index + 1}`,
        vote: phase,
      }))
    })

    return {
      phase: (data.phase as VotePhase) ?? undefined,
      leader: data.leader ?? undefined,
      round: data.term,
      term: data.term,
      votes: expandedVotes,
      updatedAt: data.updatedAt,
    }
  }, [data]);

  return (
    <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
      {/* Header */}
      <div className="border-b border-medium bg-card/30 backdrop-blur-sm">
        <PageContainer align="left" className="py-4">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold tracking-tight">Consensus</h1>
              <p className="text-muted-foreground mt-1">
                PBFT consensus status, suspicious node detection, and voting metrics
              </p>
            </div>
            <div className="flex items-center gap-3">
              {error ? (
                <Badge variant="destructive" className="text-sm">
                  Load error
                </Badge>
              ) : null}
              <Badge variant="outline" className="text-sm">
                {isLoading ? "Syncing" : "Live"}
              </Badge>
              <Button variant="outline" size="sm" onClick={() => refresh()} className="hover-scale">
                <RefreshCw className="h-4 w-4 mr-2" />
                Refresh
              </Button>
              <Button variant="outline" size="sm">
                <Settings className="h-4 w-4 mr-2" />
                Settings
              </Button>
            </div>
          </div>
        </PageContainer>
      </div>

      <PageContainer align="left" className="py-8 space-y-8">
        {/* PBFT Status Cards */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">PBFT Status</h2>
            <p className="text-muted-foreground">Current consensus state and leadership information</p>
          </div>
          <PBFTStatusCards
            leader={data?.leader ?? undefined}
            term={data?.term}
            phase={pbftStatus.phase}
            activePeers={data?.activePeers}
            totalPeers={totalPeers}
            quorumSize={data?.quorumSize}
          />
        </section>

        {/* 2-Column Layout: Charts + Nodes with Sidebar */}
        <section className="grid gap-6 lg:grid-cols-[2fr,1fr]">
          {/* Left Column */}
          <div className="space-y-6">
            <div>
              <div className="mb-6">
                <h2 className="text-xl font-semibold text-foreground mb-2">Consensus Metrics</h2>
                <p className="text-muted-foreground">Proposal counts and voting timeline analysis</p>
              </div>
              <ConsensusCharts proposals={proposalSeries} voteMetrics={voteMetrics} />
            </div>

            <div>
              <div className="mb-6">
                <h2 className="text-xl font-semibold text-foreground mb-2">Suspicious Validators</h2>
                <p className="text-muted-foreground">Suspicious activity detection and node health status</p>
              </div>
              <SuspiciousNodesTable nodes={suspiciousNodes} />
            </div>
          </div>
          {/* Right Column - Summary Cards */}
          <div className="space-y-6">
            <div className="glass-card border border-border/30 rounded-lg p-6">
              <h3 className="text-lg font-semibold text-foreground mb-4">PBFT Summary</h3>
              <div className="space-y-4">
                <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
                  <span className="text-sm text-muted-foreground">Current Term</span>
                  <span className="text-2xl font-bold text-foreground">{data?.term ?? "--"}</span>
                </div>
                <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
                  <span className="text-sm text-muted-foreground">Active Peers</span>
                  <span className="text-2xl font-bold text-primary">{data?.activePeers ?? 0}</span>
                </div>
                <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
                  <span className="text-sm text-muted-foreground">Quorum Size</span>
                  <span className="text-2xl font-bold text-foreground">{data?.quorumSize ?? "--"}</span>
                </div>
              </div>
            </div>

            <div className="glass-card border border-border/30 rounded-lg p-6">
              <h3 className="text-lg font-semibold text-foreground mb-4">Leader Status</h3>
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Current Leader</span>
                  <span className="text-xs font-mono text-foreground truncate max-w-[180px]">
                    {data?.leader ?? "Unknown"}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Phase</span>
                  <Badge variant="outline" className="capitalize">
                    {pbftStatus.phase ?? "unknown"}
                  </Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Total Peers</span>
                  <span className="text-sm font-medium text-foreground">{totalPeers}</span>
                </div>
              </div>
            </div>

            <div className="glass-card border border-border/30 rounded-lg p-6">
              <h3 className="text-lg font-semibold text-foreground mb-4">Activity Metrics</h3>
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Proposals</span>
                  <span className="text-sm font-medium text-foreground">
                    {data?.proposals?.length ?? 0}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Votes Recorded</span>
                  <span className="text-sm font-medium text-foreground">
                    {data?.votes?.reduce((sum, v) => sum + v.count, 0) ?? 0}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Suspicious Nodes</span>
                  <Badge variant={suspiciousNodes.length > 0 ? "destructive" : "outline"}>
                    {suspiciousNodes.length}
                  </Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Last Update</span>
                  <span className="text-xs font-mono text-muted-foreground">
                    {pbftStatus.updatedAt ? new Date(pbftStatus.updatedAt).toLocaleTimeString() : "--"}
                  </span>
                </div>
              </div>
            </div>

            <div className="glass-card border border-border/30 rounded-lg p-6">
              <h3 className="text-lg font-semibold text-foreground mb-4">System Info</h3>
              <div className="space-y-2 text-sm">
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Version</span>
                  <span className="text-foreground">v2.1.0</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Monitor Status</span>
                  <Badge variant={isLoading ? "outline" : "default"}>
                    {isLoading ? "Syncing" : "Live"}
                  </Badge>
                </div>
              </div>
            </div>
          </div>
        </section>
      </PageContainer>
      <PageContainer align="left" className="pb-10">
        <PbftLiveVisualizer
          phase={pbftStatus.phase}
          leader={pbftStatus.leader}
          round={pbftStatus.round}
          term={pbftStatus.term}
          votes={pbftStatus.votes}
          updatedAt={pbftStatus.updatedAt}
          isLoading={isLoading}
          error={error}
        />
      </PageContainer>
    </div>
  )
}
