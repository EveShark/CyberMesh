"use client"

import { useEffect, useState } from "react"
import { AlertCircle, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import ConsensusCards from "./consensus-cards"
import ProposalChart from "./proposal-chart"
import VoteTimeline from "./vote-timeline"
import SuspiciousNodesTable from "./suspicious-nodes-table"

interface ConsensusData {
  leader: string
  term: number
  phase: string
  activePeers: number
  quorumSize: number
  proposals: Array<{ block: number; timestamp: number }>
  votes: Array<{ type: string; count: number; timestamp: number }>
  suspiciousNodes: Array<{ id: string; status: string; uptime: number; suspicionScore: number }>
}

export default function ConsensusPage() {
  const [data, setData] = useState<ConsensusData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  const fetchData = async () => {
    try {
      setError(null)
      const response = await fetch("/api/consensus/overview")
      if (!response.ok) throw new Error("Failed to fetch")
      const result = await response.json()
      setData(result)
    } catch (err) {
      setError("Failed to load consensus data. Please try again.")
      console.error("Failed to fetch consensus data:", err)
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
          <p className="text-muted-foreground">Loading consensus data...</p>
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
        <div className="flex-1">
          <ConsensusCards
            leader={data.leader}
            term={data.term}
            phase={data.phase}
            activePeers={data.activePeers}
            quorumSize={data.quorumSize}
          />
        </div>
        <Button
          onClick={handleRefresh}
          disabled={isRefreshing}
          variant="outline"
          size="sm"
          className="flex-shrink-0 ml-4 bg-transparent"
        >
          <RefreshCw className={`w-4 h-4 ${isRefreshing ? "animate-spin" : ""}`} />
        </Button>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <ProposalChart proposals={data.proposals} />
        <VoteTimeline votes={data.votes} />
      </div>

      <SuspiciousNodesTable nodes={data.suspiciousNodes} />
    </div>
  )
}
