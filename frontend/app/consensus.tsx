"use client"

import { PBFTStatusCards } from "@/components/consensus/pbft-status-cards"
import { SuspiciousNodesTable } from "@/components/consensus/suspicious-nodes-table"
import { ConsensusCharts } from "@/components/consensus/consensus-charts"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { RefreshCw, Settings } from "lucide-react"

export default function ConsensusPage() {
  return (
    <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
      {/* Header */}
      <div className="border-b border-border/50 bg-card/30 backdrop-blur-sm">
        <div className="container mx-auto px-6 py-4">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-2xl font-bold tracking-tight">Consensus</h1>
              <p className="text-muted-foreground mt-1">
                PBFT consensus status, suspicious node detection, and voting metrics
              </p>
            </div>
            <div className="flex items-center gap-3">
              <Badge variant="outline" className="text-sm">
                Active
              </Badge>
              <Button variant="outline" size="sm">
                <RefreshCw className="h-4 w-4 mr-2" />
                Refresh
              </Button>
              <Button variant="outline" size="sm">
                <Settings className="h-4 w-4 mr-2" />
                Settings
              </Button>
            </div>
          </div>
        </div>
      </div>

      <div className="container mx-auto px-6 py-8 space-y-8">
        {/* PBFT Status Cards */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">PBFT Status</h2>
            <p className="text-muted-foreground">Current consensus state and leadership information</p>
          </div>
          <PBFTStatusCards />
        </section>

        {/* Charts Section */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">Consensus Metrics</h2>
            <p className="text-muted-foreground">Proposal counts and voting timeline analysis</p>
          </div>
          <ConsensusCharts />
        </section>

        {/* Suspicious Nodes Section */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">Node Monitoring</h2>
            <p className="text-muted-foreground">Suspicious activity detection and node health status</p>
          </div>
          <SuspiciousNodesTable />
        </section>

        {/* Footer */}
        <div className="flex items-center justify-between pt-6 border-t border-border/50">
          <div className="flex items-center gap-4 text-sm text-muted-foreground">
            <span>Consensus Monitor v2.1.0</span>
            <span>•</span>
            <span>Last updated: {new Date().toLocaleString()}</span>
          </div>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <span>5 nodes active</span>
            <span>•</span>
            <span>PBFT Round 1,247</span>
          </div>
        </div>
      </div>
    </div>
  )
}
