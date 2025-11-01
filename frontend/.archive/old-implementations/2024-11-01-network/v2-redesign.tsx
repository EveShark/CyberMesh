"use client"

import { AlertCircle, Loader2, RefreshCw } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useConsensusData } from "@/hooks/use-consensus-data"
import { useNetworkData } from "@/hooks/use-network-data"
import MetricsBar from "@/p2p-consensus/components/metrics-bar"
import NetworkGraph from "@/p2p-consensus/components/network-graph"
import PBFTStatus from "@/p2p-consensus/components/pbft-status"
import ProposalChart from "@/p2p-consensus/components/proposal-chart"
import VoteTimeline from "@/p2p-consensus/components/vote-timeline"
import ConsensusCards from "@/p2p-consensus/components/consensus-cards"

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

  const networkGraphError = networkError instanceof Error ? networkError : null

  const handleRefresh = () => {
    refreshNetwork()
    refreshConsensus()
  }

  return (
    <div className="min-h-screen w-full">
      {/* Header - Full Width */}
      <header className="border-b border-border/30 bg-card/20 backdrop-blur-sm px-6 py-4">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
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
        </div>

        {combinedError ? (
          <div className="flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive mt-4">
            <AlertCircle className="h-5 w-5" />
            <div>
              <p className="font-medium">Unable to fetch latest metrics</p>
              <p className="text-destructive/80">
                {combinedError instanceof Error ? combinedError.message : "Unknown error"}
              </p>
            </div>
          </div>
        ) : null}
      </header>

      {/* Metrics Bar - Full Width */}
      <section className="bg-card/40 backdrop-blur-sm border-b border-border/30 p-6">
        <div>
          <h2 className="text-xl font-semibold text-foreground mb-1">Network Topology</h2>
          <p className="text-sm text-muted-foreground mb-6">Peer connectivity, latency, and leader stability</p>
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
      </section>

      {/* Network Graph - Full Width */}
      <section className="bg-card/40 backdrop-blur-sm border-b border-border/30 p-6">
        <NetworkGraph nodes={networkNodes} edges={networkEdges} isLoading={networkLoading} error={networkGraphError} />
      </section>

      {/* Consensus Details */}
      <section className="bg-card/40 backdrop-blur-sm border-b border-border/30 p-6">
        <div className="mb-6">
          <h2 className="text-xl font-semibold text-foreground mb-1">Consensus Details</h2>
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

        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2 mt-6">
          <ProposalChart proposals={consensusData?.proposals ?? []} />
          <VoteTimeline votes={consensusData?.votes ?? []} />
        </div>
      </section>

      {/* Bottom Cards - 3 Columns, Edge to Edge */}
      <section className="grid grid-cols-1 lg:grid-cols-3 gap-px bg-border/20 p-px">
        <div className="bg-card/40 backdrop-blur-sm p-6">
          <h3 className="text-lg font-semibold text-foreground mb-4">Network Status</h3>
          <div className="space-y-4">
            <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
              <span className="text-sm text-muted-foreground">Connected Peers</span>
              <span className="text-2xl font-bold text-foreground">
                {networkData?.connectedPeers ?? 0}/{networkData?.totalPeers ?? 0}
              </span>
            </div>
            <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
              <span className="text-sm text-muted-foreground">Avg Latency</span>
              <span className="text-2xl font-bold text-primary">
                {networkData?.averageLatencyMs !== undefined
                  ? `${networkData.averageLatencyMs.toFixed(0)}ms`
                  : "--"}
              </span>
            </div>
            <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
              <span className="text-sm text-muted-foreground">Leader Stability</span>
              <span className="text-2xl font-bold text-foreground">
                {networkData?.leaderStability !== undefined
                  ? `${networkData.leaderStability.toFixed(1)}%`
                  : "--"}
              </span>
            </div>
          </div>
        </div>

        <div className="bg-card/40 backdrop-blur-sm p-6">
          <h3 className="text-lg font-semibold text-foreground mb-4">PBFT State</h3>
          <PBFTStatus
            phase={consensusData?.phase ?? "unknown"}
            leader={consensusData?.leader ?? networkData?.leader ?? "Unknown"}
            votingStatus={networkData?.votingStatus ?? []}
          />
        </div>

        <div className="bg-card/40 backdrop-blur-sm p-6">
          <h3 className="text-lg font-semibold text-foreground mb-4">Consensus Info</h3>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Current Term</span>
              <span className="text-sm font-medium text-foreground">{consensusData?.term ?? "--"}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Phase</span>
              <span className="text-sm font-medium text-foreground capitalize">
                {consensusData?.phase ?? "unknown"}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Active Peers</span>
              <span className="text-sm font-medium text-foreground">{consensusData?.activePeers ?? 0}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Quorum Size</span>
              <span className="text-sm font-medium text-foreground">{consensusData?.quorumSize ?? "--"}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Consensus Round</span>
              <span className="text-sm font-medium text-foreground">{networkData?.consensusRound ?? "--"}</span>
            </div>
          </div>
        </div>
      </section>
    </div>
  )
}
