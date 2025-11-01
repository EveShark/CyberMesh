"use client"

import { useEffect, useState } from "react"
import { AlertCircle, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import MetricsBar from "./metrics-bar"
import NetworkGraph from "./network-graph"
import PBFTStatus from "./pbft-status"

interface NodeHealth {
  id: string
  name: string
  status: "healthy" | "warning" | "critical"
  latency: number
  uptime: number
}

interface NetworkData {
  connectedPeers: number
  avgLatency: number
  consensusRound: number
  leaderStability: number
  nodes: NodeHealth[]
  pbftPhase: string
  leader: string
  votingStatus: Record<string, boolean>
}

export default function NetworkPage() {
  const [data, setData] = useState<NetworkData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  const fetchData = async () => {
    try {
      setError(null)
      const response = await fetch("/api/nodes/health")
      if (!response.ok) throw new Error("Failed to fetch")
      const result = await response.json()
      setData(result)
    } catch (err) {
      setError("Failed to load network data. Please try again.")
      console.error("Failed to fetch network data:", err)
    } finally {
      setLoading(false)
      setIsRefreshing(false)
    }
  }

  useEffect(() => {
    fetchData()
    const interval = setInterval(fetchData, 5000)
    return () => clearInterval(interval)
  }, [])

  const handleRefresh = async () => {
    setIsRefreshing(true)
    await fetchData()
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-center">
          <div className="w-8 h-8 border-2 border-primary border-t-transparent rounded-full animate-spin mx-auto mb-3" />
          <p className="text-muted-foreground">Loading network data...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="bg-destructive/10 border border-destructive/30 rounded-lg p-4 flex items-start gap-3">
        <AlertCircle className="w-5 h-5 text-destructive flex-shrink-0 mt-0.5" />
        <div className="flex-1">
          <p className="text-destructive font-medium">{error}</p>
          <Button onClick={handleRefresh} variant="outline" size="sm" className="mt-2 bg-transparent">
            Try Again
          </Button>
        </div>
      </div>
    )
  }

  if (!data) {
    return <div className="text-center py-12 text-destructive">No data available</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <MetricsBar
          connectedPeers={data.connectedPeers}
          avgLatency={data.avgLatency}
          consensusRound={data.consensusRound}
          leaderStability={data.leaderStability}
        />
        <Button
          onClick={handleRefresh}
          disabled={isRefreshing}
          variant="outline"
          size="sm"
          className="flex-shrink-0 bg-transparent"
        >
          <RefreshCw className={`w-4 h-4 ${isRefreshing ? "animate-spin" : ""}`} />
        </Button>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2">
          <NetworkGraph nodes={data.nodes} />
        </div>
        <div>
          <PBFTStatus phase={data.pbftPhase} leader={data.leader} votingStatus={data.votingStatus} />
        </div>
      </div>
    </div>
  )
}
