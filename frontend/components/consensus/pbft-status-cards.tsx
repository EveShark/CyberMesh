"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Crown, Users, Clock, Activity } from "lucide-react"

// TODO: integrate GET /consensus/status
const mockPBFTStatus = {
  leader: "Orion",
  term: 1247,
  phase: "COMMIT",
  activePeers: 5,
  totalPeers: 5,
}

export function PBFTStatusCards() {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
      {/* Leader Card */}
      <Card className="glass-card border-0 hover:shadow-lg transition-all duration-300">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">Current Leader</CardTitle>
          <Crown className="h-4 w-4 text-amber-500" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold text-foreground">{mockPBFTStatus.leader}</div>
          <Badge variant="secondary" className="mt-2 bg-amber-500/10 text-amber-600 border-amber-500/20">
            Active • 3/5 quorum • 1 Byzantine tolerated
          </Badge>
        </CardContent>
      </Card>

      {/* Term Card */}
      <Card className="glass-card border-0 hover:shadow-lg transition-all duration-300">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">Current Term</CardTitle>
          <Clock className="h-4 w-4 text-blue-500" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold text-foreground">#{mockPBFTStatus.term}</div>
          <p className="text-xs text-muted-foreground mt-2">+12 from previous</p>
        </CardContent>
      </Card>

      {/* Phase Card */}
      <Card className="glass-card border-0 hover:shadow-lg transition-all duration-300">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">Current Phase</CardTitle>
          <Activity className="h-4 w-4 text-emerald-500" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold text-foreground">{mockPBFTStatus.phase}</div>
          <Badge variant="secondary" className="mt-2 bg-emerald-500/10 text-emerald-600 border-emerald-500/20">
            In Progress
          </Badge>
        </CardContent>
      </Card>

      {/* Active Peers Card */}
      <Card className="glass-card border-0 hover:shadow-lg transition-all duration-300">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">Active Peers</CardTitle>
          <Users className="h-4 w-4 text-purple-500" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold text-foreground">
            {mockPBFTStatus.activePeers}/{mockPBFTStatus.totalPeers}
          </div>
          <p className="text-xs text-muted-foreground mt-2">
            {Math.round((mockPBFTStatus.activePeers / mockPBFTStatus.totalPeers) * 100)}% consensus
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
