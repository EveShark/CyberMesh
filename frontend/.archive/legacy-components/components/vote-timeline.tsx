"use client"

import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from "recharts"
import { Card } from "@/components/ui/card"

interface VoteTimelineProps {
  votes: Array<{ type: string; count: number; timestamp: number }>
}

export default function VoteTimeline({ votes }: VoteTimelineProps) {
  const chartData = votes.slice(-16).map((v, i) => ({
    time: `T${i}`,
    commits: v.type === "commit" ? v.count : 0,
    viewChanges: v.type === "view_change" ? v.count : 0,
    proposals: v.type === "proposal" ? v.count : 0,
  }))

  return (
    <Card className="bg-card/50 border border-border/50 backdrop-blur-sm p-6 hover:border-primary/30 transition-all duration-300">
      <h3 className="text-lg font-semibold mb-4 text-foreground">Vote Timeline</h3>
      <ResponsiveContainer width="100%" height={300}>
        <LineChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="rgba(148, 163, 184, 0.1)" vertical={false} />
          <XAxis dataKey="time" stroke="rgba(148, 163, 184, 0.5)" style={{ fontSize: "12px" }} />
          <YAxis stroke="rgba(148, 163, 184, 0.5)" />
          <Tooltip
            contentStyle={{
              backgroundColor: "rgba(15, 23, 42, 0.9)",
              border: "1px solid rgba(0, 217, 255, 0.3)",
              borderRadius: "8px",
            }}
            labelStyle={{ color: "#f1f5f9" }}
            cursor={{ stroke: "rgba(0, 217, 255, 0.2)" }}
          />
          <Legend wrapperStyle={{ paddingTop: "16px" }} />
          <Line type="monotone" dataKey="commits" stroke="rgba(0, 217, 255, 0.8)" strokeWidth={2.5} dot={false} />
          <Line type="monotone" dataKey="viewChanges" stroke="rgba(245, 158, 11, 0.8)" strokeWidth={2.5} dot={false} />
          <Line type="monotone" dataKey="proposals" stroke="rgba(16, 185, 129, 0.8)" strokeWidth={2.5} dot={false} />
        </LineChart>
      </ResponsiveContainer>
    </Card>
  )
}
