import { Card } from "@/components/ui/card"
import { Crown, Hash, Zap, Users, Shield } from "lucide-react"

interface ConsensusCardsProps {
  leader: string
  term: number
  phase: string
  activePeers: number
  quorumSize: number
}

export default function ConsensusCards({ leader, term, phase, activePeers, quorumSize }: ConsensusCardsProps) {
  const cards = [
    { label: "Leader", value: leader, icon: Crown },
    { label: "Term/View", value: term, icon: Hash },
    { label: "Phase", value: phase, icon: Zap },
    { label: "Active Peers", value: activePeers, icon: Users },
    { label: "Quorum Size", value: quorumSize, icon: Shield },
  ]

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-5 gap-4">
      {cards.map((card, idx) => {
        const Icon = card.icon
        return (
        <Card
          key={idx}
          className="glass-card border border-border/40 p-5 transition-all duration-300 hover:shadow-glow"
        >
          <div className="flex items-start justify-between mb-3">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{card.label}</span>
            <Icon className="w-4 h-4 text-network/80" />
          </div>
          <div className="text-lg font-semibold text-foreground font-mono truncate">
            {typeof card.value === "string" ? (card.value || "—") : card.value}
          </div>
          </Card>
        )
      })}
    </div>
  )
}
