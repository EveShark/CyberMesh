"use client"

import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ThreatsTable, type Threat } from "@/components/threats/threats-table"
import { ThreatDetailDrawer } from "@/components/threats/threat-detail-drawer"
import { ThreatCharts } from "@/components/threats/threat-charts"
import { AlertTriangle, RefreshCw, Download } from "lucide-react"

export default function ThreatsPage() {
  const [selectedThreat, setSelectedThreat] = useState<Threat | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)

  const handleThreatSelect = (threat: Threat) => {
    setSelectedThreat(threat)
    setDrawerOpen(true)
  }

  const handleDrawerClose = () => {
    setDrawerOpen(false)
    setSelectedThreat(null)
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
      {/* Header */}
      <div className="border-b border-border/50 bg-card/30 backdrop-blur-sm">
        <div className="container mx-auto px-6 py-4">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-2xl font-bold tracking-tight">Threats</h1>
              <p className="text-muted-foreground mt-1">Real-time threat monitoring and analysis dashboard</p>
            </div>
            <div className="flex items-center gap-3">
              <Badge variant="destructive" className="animate-pulse">
                <AlertTriangle className="h-3 w-3 mr-1" />
                LIVE
              </Badge>
              <Button variant="outline" size="sm">
                <RefreshCw className="h-4 w-4 mr-2" />
                Refresh
              </Button>
              <Button variant="outline" size="sm">
                <Download className="h-4 w-4 mr-2" />
                Export
              </Button>
            </div>
          </div>
        </div>
      </div>

      <div className="container mx-auto px-6 py-8 space-y-8">
        {/* Charts Section */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">Threat Analytics</h2>
            <p className="text-muted-foreground">Severity distribution and threat volume trends</p>
          </div>
          <ThreatCharts />
        </section>

        {/* Threats Table */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">Active Threats</h2>
            <p className="text-muted-foreground">Click on any threat to view detailed analysis</p>
          </div>
          <ThreatsTable onThreatSelect={handleThreatSelect} />
        </section>

        {/* Threat Detail Drawer */}
        <ThreatDetailDrawer threat={selectedThreat} open={drawerOpen} onClose={handleDrawerClose} />

        {/* Footer */}
        <div className="flex items-center justify-between pt-6 border-t border-border/50">
          <div className="flex items-center gap-4 text-sm text-muted-foreground">
            <span>Security Center v2.1.0</span>
            <span>•</span>
            <span>Last updated: {new Date().toLocaleString()}</span>
          </div>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <span>Monitoring 5 nodes</span>
            <span>•</span>
            <span>5 active threats</span>
          </div>
        </div>
      </div>
    </div>
  )
}
