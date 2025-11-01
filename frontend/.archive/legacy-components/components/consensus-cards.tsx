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
    { label: "Leader", value: leader, icon: Crown, color: "from-primary to-accent" },
    { label: "Term/View", value: term, icon: Hash, color: "from-accent to-primary" },
    { label: "Phase", value: phase, icon: Zap, color: "from-primary to-secondary" },
    { label: "Active Peers", value: activePeers, icon: Users, color: "from-secondary to-accent" },
    { label: "Quorum Size", value: quorumSize, icon: Shield, color: "from-accent to-secondary" },
  ]

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
      {cards.map((card, idx) => {
        const Icon = card.icon
        return (
          <Card
            key={idx}
            className="bg-card/50 border border-border/50 backdrop-blur-sm p-5 hover:border-primary/50 transition-all duration-300 hover:shadow-lg hover:shadow-primary/10 group"
          >
            <div className="flex items-start justify-between mb-3">
              <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{card.label}</div>
              <Icon className="w-4 h-4 text-primary/60 group-hover:text-primary transition-colors" />
            </div>
            <div className="text-lg font-bold text-foreground truncate">
              {typeof card.value === "string" ? (
                <span className="font-mono text-sm text-primary">{card.value.slice(0, 12)}...</span>
              ) : (
                <span className="bg-gradient-to-r from-primary to-accent bg-clip-text text-transparent">
                  {card.value}
                </span>
              )}
            </div>
          </Card>
        )
      })}
    </div>
  )
}
