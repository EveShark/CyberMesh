"use client"

import { AlertCircle, Loader2, RefreshCw } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useConsensusData } from "@/hooks/use-consensus-data"
import { useNetworkData } from "@/hooks/use-network-data"
import MetricsBar from "@/p2p-consensus/components/metrics-bar"
import NetworkGraph from "@/p2p-consensus/components/network-graph"
import ProposalChart from "@/p2p-consensus/components/proposal-chart"
import VoteTimeline from "@/p2p-consensus/components/vote-timeline"
import ConsensusCards from "@/p2p-consensus/components/consensus-cards"
import { PageContainer } from "@/components/page-container"

export default function NetworkPageClient() {
  const {
    data: networkData,
    isLoading: networkLoading,
    error: networkError,
    refresh: refreshNetwork,
  } = useNetworkData()

  const {
    data: consensusData,
    isLoading: consensusLoading,
    error: consensusError,
    refresh: refreshConsensus,
  } = useConsensusData()

  const isLoading = networkLoading || consensusLoading
  const combinedError = (networkError as Error | undefined) || (consensusError as Error | undefined) || null
  const networkNodes = networkData?.nodes ?? []
  const networkEdges = networkData?.edges ?? []
  // Suspicious nodes table removed per request

  const networkGraphError = networkError instanceof Error ? networkError : null

  const handleRefresh = () => {
    refreshNetwork()
    refreshConsensus()
  }

  return (
    <div className="min-h-screen">
      <PageContainer align="left" variant="compact" className="py-6 lg:py-8 space-y-8">
        <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-3xl font-bold text-foreground">Network &amp; Consensus</h1>
            <p className="text-muted-foreground">Live peer telemetry and PBFT coordination status</p>
          </div>
          <div className="flex items-center gap-3">
            {isLoading ? (
              <Badge variant="outline" className="flex items-center gap-1">
                <Loader2 className="h-3 w-3 animate-spin" /> Syncing
              </Badge>
            ) : null}
            {combinedError ? (
              <Badge variant="destructive" className="flex items-center gap-1">
                <AlertCircle className="h-3 w-3" /> Live data degraded
              </Badge>
            ) : null}
            <Button variant="outline" size="sm" onClick={handleRefresh}>
              <RefreshCw className="mr-2 h-4 w-4" /> Refresh
            </Button>
          </div>
        </header>

        {combinedError ? (
          <div className="flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
            <AlertCircle className="h-5 w-5" />
            <div>
              <p className="font-medium">Unable to fetch latest metrics</p>
              <p className="text-destructive/80">
                {combinedError instanceof Error ? combinedError.message : "Unknown error"}
              </p>
            </div>
          </div>
        ) : null}

        {/* Network Topology Section */}
        <section className="space-y-6">
          <div className="space-y-1">
            <h2 className="text-xl font-semibold text-foreground">Network Topology</h2>
            <p className="text-sm text-muted-foreground">Peer connectivity, latency, and leader stability</p>
          </div>

          {networkData ? (
            <MetricsBar
              connectedPeers={networkData.connectedPeers}
              expectedPeers={networkData.expectedPeers}
              avgLatency={networkData.averageLatencyMs}
              consensusRound={networkData.consensusRound}
              leaderStability={networkData.leaderStability}
              inboundRateBps={networkData.inboundRateBps}
              outboundRateBps={networkData.outboundRateBps}
            />
          ) : (
            <div className="rounded-lg border border-dashed border-border/60 p-6 text-center text-sm text-muted-foreground">
              {networkLoading ? "Collecting network telemetry…" : "Network metrics are not yet available."}
            </div>
          )}

          <NetworkGraph nodes={networkNodes} edges={networkEdges} isLoading={networkLoading} error={networkGraphError} />
        </section>

        {/* Consensus Details */}
        <section className="space-y-4">
          <div className="space-y-1">
            <h2 className="text-xl font-semibold text-foreground">Consensus Details</h2>
            <p className="text-sm text-muted-foreground">Leader rotation, quorum readiness, and validator health</p>
          </div>

          {consensusData ? (
            <ConsensusCards
              leader={consensusData.leader ?? "Unknown"}
              term={consensusData.term}
              phase={consensusData.phase}
              activePeers={consensusData.activePeers}
              quorumSize={consensusData.quorumSize}
            />
          ) : (
            <div className="rounded-lg border border-dashed border-border/60 p-6 text-center text-sm text-muted-foreground">
              {consensusLoading ? "Waiting for consensus metrics…" : "Consensus overview unavailable."}
            </div>
          )}

          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <ProposalChart proposals={consensusData?.proposals ?? []} />
            <VoteTimeline votes={consensusData?.votes ?? []} />
          </div>
          {/* SuspiciousNodesTable removed */}
        </section>
        {/* Summary cards removed per request */}
      </PageContainer>
    </div>
  )
}
