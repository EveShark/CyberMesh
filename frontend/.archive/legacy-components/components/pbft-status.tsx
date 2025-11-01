import { Card } from "@/components/ui/card"
import { CheckCircle2, AlertCircle } from "lucide-react"

interface PBFTStatusProps {
  phase: string
  leader: string
  votingStatus: Record<string, boolean>
}

export default function PBFTStatus({ phase, leader, votingStatus }: PBFTStatusProps) {
  const votingCount = Object.values(votingStatus).filter(Boolean).length
  const totalVoters = Object.keys(votingStatus).length
  const votingPercentage = (votingCount / totalVoters) * 100

  return (
    <Card className="bg-card/50 border border-border/50 backdrop-blur-sm p-6 hover:border-primary/30 transition-all duration-300">
      <h3 className="text-lg font-semibold mb-6 text-foreground">PBFT Status</h3>

      <div className="space-y-6">
        {/* Phase */}
        <div className="p-4 rounded-lg bg-primary/10 border border-primary/20">
          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">Current Phase</div>
          <div className="text-2xl font-bold text-primary capitalize">{phase}</div>
        </div>

        {/* Leader */}
        <div>
          <div className="flex items-center gap-2 mb-2">
            <CheckCircle2 className="w-4 h-4 text-primary" />
            <div className="text-sm font-medium text-muted-foreground">Leader</div>
          </div>
          <div className="p-3 rounded-lg bg-muted/30 border border-border/50 font-mono text-sm text-foreground break-all">
            {leader}
          </div>
        </div>

        {/* Voting Status */}
        <div>
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <AlertCircle className="w-4 h-4 text-accent" />
              <div className="text-sm font-medium text-muted-foreground">Voting Status</div>
            </div>
            <div className="text-sm font-bold text-accent">
              {votingCount}/{totalVoters}
            </div>
          </div>
          <div className="w-full bg-muted/50 rounded-full h-2.5 border border-border/50 overflow-hidden">
            <div
              className="bg-gradient-to-r from-primary to-accent h-2.5 rounded-full transition-all duration-500"
              style={{ width: `${votingPercentage}%` }}
            ></div>
          </div>
          <div className="text-xs text-muted-foreground mt-2">{votingPercentage.toFixed(0)}% validators voting</div>
        </div>
      </div>
    </Card>
  )
}
