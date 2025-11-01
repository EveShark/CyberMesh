import { Card } from "@/components/ui/card"
import { CheckCircle2, AlertCircle } from "lucide-react"

import type { VotingStatusEntry } from "@/p2p-consensus/lib/types"

interface PBFTStatusProps {
  phase: string
  leader: string | null
  votingStatus: VotingStatusEntry[]
}

export default function PBFTStatus({ phase, leader, votingStatus }: PBFTStatusProps) {
  const votingCount = votingStatus.filter((entry) => entry.voting).length
  const totalVoters = votingStatus.length || 1
  const votingPercentage = (votingCount / totalVoters) * 100

  return (
    <Card className="glass-card border border-border/40 p-6">
      <h3 className="text-lg font-semibold mb-6 text-foreground">PBFT Status</h3>

      <div className="space-y-6">
        {/* Phase */}
        <div
          className="p-4 rounded-lg border"
          style={{
            backgroundColor: "color-mix(in oklch, var(--color-consensus) 12%, transparent)",
            borderColor: "color-mix(in oklch, var(--color-consensus) 30%, transparent)",
          }}
        >
          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">Current Phase</div>
          <div className="text-2xl font-bold text-consensus capitalize">{phase}</div>
        </div>

        {/* Leader */}
        <div>
          <div className="flex items-center gap-2 mb-2">
            <CheckCircle2 className="w-4 h-4 text-consensus" />
            <div className="text-sm font-medium text-muted-foreground">Leader</div>
          </div>
          <div className="p-3 rounded-lg bg-muted/40 border border-border/40 font-mono text-sm text-foreground break-all">
            {leader ?? "Unknown"}
          </div>
        </div>

        {/* Voting Status */}
        <div>
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <AlertCircle className="w-4 h-4 text-network" />
              <div className="text-sm font-medium text-muted-foreground">Voting Status</div>
            </div>
            <div className="text-sm font-bold text-network">
              {votingCount}/{totalVoters}
            </div>
          </div>
          <div className="w-full bg-muted/40 rounded-full h-2.5 border border-border/40 overflow-hidden">
            <div
              className="h-2.5 rounded-full transition-all duration-500"
              style={{
                width: `${votingPercentage}%`,
                background: "linear-gradient(90deg, var(--color-network), color-mix(in oklch, var(--color-network) 60%, transparent))",
              }}
            ></div>
          </div>
          <div className="text-xs text-muted-foreground mt-2">{votingPercentage.toFixed(0)}% validators voting</div>
        </div>
      </div>
    </Card>
  )
}
