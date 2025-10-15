"use client"

import { NodeStatusGrid } from "@/components/overview/node-status-grid"
import { ClusterStatusTiles } from "@/components/overview/cluster-status-tiles"
import { ServiceHealthGrid } from "@/components/overview/service-health-grid"
import { RecentThreatsTable } from "@/components/overview/recent-threats-table"
import { TrafficSparkline } from "@/components/overview/traffic-sparkline"
import { Card, CardContent } from "@/components/ui/card"

export default function OverviewPage() {
  return (
    <div className="min-h-screen p-6 space-y-6">
      {/* Header */}
      <div className="space-y-2">
        <h1 className="text-3xl font-bold text-foreground">System Status</h1>
        <p className="text-muted-foreground">Monitor cluster health, node performance, and service availability</p>
      </div>

      {/* Main Content */}
      <div className="space-y-12">
        {/* System Status Overview */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">Cluster Overview</h2>
            <p className="text-muted-foreground">Real-time cluster health and node performance metrics</p>
          </div>

          <div className="space-y-8">
            <ClusterStatusTiles />
            <NodeStatusGrid />
          </div>
        </section>

        {/* Service Health */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">Service Health</h2>
            <p className="text-muted-foreground">Track the health and performance of critical services</p>
          </div>

          <ServiceHealthGrid />
        </section>

        {/* Security & Traffic */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">Security & Performance</h2>
            <p className="text-muted-foreground">Monitor threats, traffic patterns, and system performance metrics</p>
          </div>

          <div className="grid grid-cols-1 xl:grid-cols-2 gap-8">
            <div>
              <RecentThreatsTable />
            </div>
            <div>
              <TrafficSparkline />
            </div>
          </div>
        </section>

        {/* System Information */}
        <section>
          <Card className="glass-card">
            <CardContent className="p-6">
              <div className="grid grid-cols-1 md:grid-cols-4 gap-6 text-center">
                <div>
                  <p className="text-2xl font-bold text-status-healthy">99.9%</p>
                  <p className="text-sm text-muted-foreground">System Uptime</p>
                </div>
                <div>
                  <p className="text-2xl font-bold text-foreground">5</p>
                  <p className="text-sm text-muted-foreground">Active Nodes</p>
                </div>
                <div>
                  <p className="text-2xl font-bold text-foreground">5</p>
                  <p className="text-sm text-muted-foreground">Services</p>
                </div>
                <div>
                  <p className="text-2xl font-bold text-status-warning">2</p>
                  <p className="text-sm text-muted-foreground">Active Investigations</p>
                </div>
              </div>
            </CardContent>
          </Card>
        </section>
      </div>
    </div>
  )
}
