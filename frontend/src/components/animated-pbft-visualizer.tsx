"use client"

import { useMemo } from "react"
import { motion } from "framer-motion"
import { Card, CardContent } from "@/components/ui/card"
import { CheckCircle2, Clock } from "lucide-react"

import { useDashboardData } from "@/hooks/use-dashboard-data"

type VotePhase = "pre-prepare" | "prepare" | "commit" | "decide" | "none"
type VoteTally = { node: string; vote: VotePhase }

const DASHBOARD_REFRESH_MS = 1500

const phases = ["pre-prepare", "prepare", "commit", "decide"] as const
const phaseDescriptions: Record<string, string> = {
  "pre-prepare": "Leader proposes block",
  prepare: "Nodes prepare consensus",
  commit: "Nodes commit to block",
  decide: "Block finalized",
}

const phaseColors: Record<string, string> = {
  "pre-prepare": "var(--chart-1)",
  prepare: "var(--chart-2)",
  commit: "var(--chart-5)",
  decide: "var(--status-healthy)",
}

const votePhaseMap: Record<string, VotePhase> = {
  proposal: "pre-prepare",
  pre_prepare: "pre-prepare",
  prepare: "prepare",
  vote: "prepare",
  precommit: "commit",
  commit: "commit",
  decide: "decide",
  view_change: "none",
}

export default function AnimatedPbftVisualizer() {
  const { data: dashboard, error, isLoading } = useDashboardData(DASHBOARD_REFRESH_MS)

  const consensus = dashboard?.consensus

  const currentPhase = (consensus?.phase as VotePhase) ?? "pre-prepare"
  const currentPhaseIndex = phases.indexOf(currentPhase as (typeof phases)[number])

  const voteTallies = useMemo<VoteTally[]>(() => {
    const entries = consensus?.votes ?? []
    const tallies: VoteTally[] = []
    entries.forEach((vote) => {
      const phase = votePhaseMap[vote.type] ?? "none"
      const sampleSize = Math.min(vote.count, 12)
      for (let i = 0; i < sampleSize; i += 1) {
        tallies.push({ node: `${vote.type}-${i + 1}`, vote: phase })
      }
    })
    return tallies
  }, [consensus?.votes])

  const voteCounts = useMemo(() => {
    const counts: Record<(typeof phases)[number], number> = {
      "pre-prepare": 0,
      prepare: 0,
      commit: 0,
      decide: 0,
    }
    ;(consensus?.votes ?? []).forEach((vote) => {
      const phase = votePhaseMap[vote.type]
      if (phase && phase !== "none") {
        counts[phase as (typeof phases)[number]] += vote.count
      }
    })
    return counts
  }, [consensus?.votes])

  const totalVotes = useMemo(() => {
    const tallySum = (Object.values(voteCounts) as number[]).reduce((sum, value) => sum + value, 0)
    return Math.max(consensus?.quorum_size ?? 0, tallySum, voteTallies.length, 1)
  }, [consensus?.quorum_size, voteCounts, voteTallies.length])

  const leaderLabel = consensus?.leader ?? consensus?.leader_id ?? "—"
  const updatedAt = consensus?.updated_at ? new Date(consensus.updated_at).toLocaleTimeString() : null
  const primaryLoading = !dashboard && isLoading

  return (
    <Card className="glass-card overflow-hidden">
      <CardContent className="p-6">
        {/* Header */}
        <div className="text-center mb-8">
          <h3 className="text-xl font-semibold text-foreground">PBFT Consensus Flow</h3>
          <p className="text-sm text-muted-foreground mt-2">
            {primaryLoading
              ? "Loading consensus state..."
              : error
                ? "Error loading consensus"
                : `Term ${consensus?.term ?? "—"} • Leader: ${leaderLabel}${
                    updatedAt ? ` • Updated ${updatedAt}` : ""
                  }`}
          </p>
        </div>

        {/* Phase Flow Diagram */}
        <div className="mb-8">
          <div className="flex items-center justify-between gap-2 md:gap-4">
            {phases.map((phase, idx) => {
              const isActive = idx === currentPhaseIndex
              const isCompleted = idx < currentPhaseIndex
              const phaseVotes = voteCounts[phase] ?? 0
              const votePercentage = totalVotes > 0 ? (phaseVotes / totalVotes) * 100 : 0

              return (
                <motion.div
                  key={phase}
                  className="flex-1 flex flex-col items-center"
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: idx * 0.1 }}
                >
                  {/* Phase circle */}
                  <motion.div
                    className="relative mb-3"
                    animate={{
                      scale: isActive ? 1.15 : 1,
                    }}
                    transition={{ duration: 0.3 }}
                  >
                    {/* Glow effect for active phase */}
                    {isActive && (
                      <motion.div
                        className="absolute inset-0 rounded-full"
                        style={{
                          backgroundColor: phaseColors[phase],
                          opacity: 0.2,
                        }}
                        animate={{
                          scale: [1, 1.3],
                          opacity: [0.3, 0],
                        }}
                        transition={{
                          duration: 1.5,
                          repeat: Number.POSITIVE_INFINITY,
                        }}
                      />
                    )}

                    {/* Main circle */}
                    <motion.div
                      className="w-12 h-12 rounded-full flex items-center justify-center border-2 relative z-10"
                      style={{
                        borderColor: phaseColors[phase],
                        backgroundColor: isActive
                          ? phaseColors[phase]
                          : isCompleted
                            ? phaseColors[phase]
                            : "var(--background)",
                      }}
                      animate={{
                        boxShadow: isActive ? `0 0 16px ${phaseColors[phase]}` : "0 0 0px rgba(0,0,0,0)",
                      }}
                      transition={{ duration: 0.3 }}
                    >
                      {isCompleted ? (
                        <CheckCircle2 className="w-6 h-6 text-background" />
                      ) : isActive ? (
                        <motion.div
                          animate={{ rotate: 360 }}
                          transition={{ duration: 2, repeat: Number.POSITIVE_INFINITY, ease: "linear" }}
                        >
                          <Clock className="w-6 h-6 text-background" />
                        </motion.div>
                      ) : (
                        <span className="text-xs font-bold text-muted-foreground">{idx + 1}</span>
                      )}
                    </motion.div>
                  </motion.div>

                  {/* Phase label */}
                  <div className="text-center">
                    <p className="text-xs font-semibold text-foreground capitalize">{phase}</p>
                    <p className="text-2xs text-muted-foreground mt-1">{phaseDescriptions[phase]}</p>
                  </div>

                  {/* Vote progress bar */}
                  <motion.div
                    className="w-full mt-3 h-1 bg-muted rounded-full overflow-hidden"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    transition={{ delay: idx * 0.1 + 0.2 }}
                  >
                    <motion.div
                      className="h-full rounded-full"
                      style={{ backgroundColor: phaseColors[phase] }}
                      initial={{ width: 0 }}
                      animate={{ width: `${votePercentage}%` }}
                      transition={{ duration: 0.5, delay: idx * 0.1 + 0.3 }}
                    />
                  </motion.div>

                  {/* Vote count */}
                  <p className="text-xs text-muted-foreground mt-2">
                    {phaseVotes}/{totalVotes} votes
                  </p>
                </motion.div>
              )
            })}
          </div>

          {/* Connecting lines */}
          <div className="flex items-center justify-between gap-2 md:gap-4 mt-6 px-6">
            {phases.slice(0, -1).map((_, idx) => (
              <motion.div
                key={`line-${idx}`}
                className="flex-1 h-0.5 bg-gradient-to-r from-muted to-muted"
                initial={{ opacity: 0 }}
                animate={{ opacity: idx < currentPhaseIndex ? 1 : 0.3 }}
                transition={{ duration: 0.3 }}
              />
            ))}
          </div>
        </div>

        {/* Status Cards */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <motion.div
            className="rounded-lg border bg-background/60 p-4 text-center"
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.4 }}
          >
            <p className="text-xs text-muted-foreground">Current Phase</p>
            <p className="font-semibold text-foreground capitalize mt-1">{currentPhase}</p>
          </motion.div>

          <motion.div
            className="rounded-lg border bg-background/60 p-4 text-center"
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.5 }}
          >
            <p className="text-xs text-muted-foreground">Leader</p>
            <p className="font-semibold text-foreground mt-1 text-sm truncate">
              {leaderLabel && leaderLabel.length > 10 ? `${leaderLabel.slice(0, 8)}…` : leaderLabel}
            </p>
          </motion.div>

          <motion.div
            className="rounded-lg border bg-background/60 p-4 text-center"
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.6 }}
          >
            <p className="text-xs text-muted-foreground">Round</p>
            <p className="font-semibold text-foreground mt-1">{consensus?.term ?? "—"}</p>
          </motion.div>

          <motion.div
            className="rounded-lg border bg-background/60 p-4 text-center"
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.7 }}
          >
            <p className="text-xs text-muted-foreground">Consensus</p>
            <p className="font-semibold text-status-healthy mt-1">Active</p>
          </motion.div>
        </div>
      </CardContent>
    </Card>
  )
}
