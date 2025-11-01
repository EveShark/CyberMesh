"use client"

import { useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import Sidebar from "@/components/sidebar"
import NetworkPage from "@/components/network-page"
import ConsensusPage from "@/components/consensus-page"

export default function Home() {
  const [activeTab, setActiveTab] = useState("network")

  return (
    <div className="flex h-screen bg-background text-foreground">
      <Sidebar activeTab={activeTab} onTabChange={setActiveTab} />

      {/* Main Content */}
      <main className="flex-1 ml-64 overflow-auto">
        <div className="absolute inset-0 bg-gradient-to-br from-primary/5 via-transparent to-accent/5 pointer-events-none" />

        <div className="p-8 relative z-10">
          {/* Header */}
          <div className="mb-8 animate-slide-in">
            <div className="flex items-center gap-3 mb-2">
              <div className="w-1 h-10 bg-gradient-to-b from-primary to-accent rounded-full" />
              <h1 className="text-5xl font-bold text-balance">Realtime P2P Consensus</h1>
            </div>
            <p className="text-muted-foreground mt-2 ml-5">Byzantine Fault Tolerant Network Monitoring</p>
          </div>

          {/* Content Area */}
          <div className="space-y-6">
            <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
              <TabsList className="grid w-full max-w-md grid-cols-2 bg-card/50 border border-border rounded-xl p-1.5 backdrop-blur-sm">
                <TabsTrigger
                  value="network"
                  className="data-[state=active]:bg-primary data-[state=active]:text-primary-foreground rounded-lg transition-all duration-200"
                >
                  Network Topology
                </TabsTrigger>
                <TabsTrigger
                  value="consensus"
                  className="data-[state=active]:bg-primary data-[state=active]:text-primary-foreground rounded-lg transition-all duration-200"
                >
                  Consensus Details
                </TabsTrigger>
              </TabsList>

              <TabsContent value="network" className="mt-8 animate-in fade-in-50 duration-300">
                <NetworkPage />
              </TabsContent>

              <TabsContent value="consensus" className="mt-8 animate-in fade-in-50 duration-300">
                <ConsensusPage />
              </TabsContent>
            </Tabs>

            {/* Status Footer */}
            <div className="mt-12 pt-6 border-t border-border/50">
              <div className="flex items-center justify-between text-sm text-muted-foreground">
                <div className="flex items-center gap-2">
                  <div className="w-2 h-2 rounded-full bg-primary animate-pulse" />
                  <span>System Status: Operational</span>
                </div>
                <span>Last updated: Real-time</span>
              </div>
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
