"use client"

import { AlertCircle, RefreshCw } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { PageContainer } from "@/components/page-container"
import { useConsensusData } from "@/hooks/use-consensus-data"
import { useNetworkData } from "@/hooks/use-network-data"
import MetricsBar from "@/p2p-consensus/components/metrics-bar"
import NetworkGraph from "@/p2p-consensus/components/network-graph"
import ProposalChart from "@/p2p-consensus/components/proposal-chart"
import VoteTimeline from "@/p2p-consensus/components/vote-timeline"
import ValidatorStatusTable from "@/p2p-consensus/components/validator-status-table"

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
    <PageContainer align="left" className="py-6 lg:py-8 space-y-6">
      {/* Header */}
      <header className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="space-y-2">
          <h1 className="text-3xl font-bold text-foreground">Network & Consensus</h1>
          <p className="text-sm text-muted-foreground">
            Real-time P2P topology, 5-node cluster visualization, and PBFT consensus status
          </p>
        </div>
        <div className="flex items-center gap-3">
          {combinedError && (
            <Badge variant="destructive" className="flex items-center gap-1 text-xs">
              <AlertCircle className="h-3 w-3" /> Sync error
            </Badge>
          )}
          <Button size="sm" variant="outline" onClick={handleRefresh} disabled={isLoading}>
            <RefreshCw className="mr-2 h-4 w-4" /> Refresh
          </Button>
        </div>
      </header>

      {combinedError && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 flex items-start gap-3 text-sm text-destructive">
          <AlertCircle className="h-5 w-5" />
          <div>
            <p className="font-medium">Unable to fetch latest metrics</p>
            <p className="text-destructive/80">{combinedError.message}</p>
          </div>
        </div>
      )}

      {/* 6 Network KPIs - full-width */}
      <section className="w-full">
        {networkData && (
          <MetricsBar
            connectedPeers={networkData.connectedPeers}
            expectedPeers={networkData.expectedPeers}
            avgLatency={networkData.averageLatencyMs}
            consensusRound={networkData.consensusRound}
            leaderStability={networkData.leaderStability}
            inboundRateBps={networkData.inboundRateBps}
            outboundRateBps={networkData.outboundRateBps}
          />
        )}
      </section>

      {/* 5-Node Cluster Topology */}
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle>5-Node Cluster Topology</CardTitle>
          <CardDescription>Real-time P2P connections and validator roles</CardDescription>
        </CardHeader>
        <CardContent className="p-6">
          <NetworkGraph
            nodes={networkNodes}
            edges={networkEdges}
            leader={networkData?.leader ?? undefined}
            leaderId={networkData?.leaderId ?? undefined}
            isLoading={networkLoading}
            error={networkGraphError}
          />
        </CardContent>
      </Card>

      {/* Proposal Series & Vote Timeline - Side by Side */}
      <section className="grid gap-6 xl:grid-cols-2">
        <ProposalChart proposals={consensusData?.proposals ?? []} isLoading={consensusLoading} />
        <VoteTimeline votes={consensusData?.votes ?? []} isLoading={consensusLoading} />
      </section>

      {/* Validator Status Table */}
      <ValidatorStatusTable
        nodes={networkNodes}
        leader={networkData?.leader}
        leaderId={networkData?.leaderId ?? null}
        leaderStability={networkData?.leaderStability}
        isLoading={networkLoading}
      />
    </PageContainer>
  )
}
