"use client"

import * as React from "react"
import { useState } from "react"
import { Card } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TrendingUp, FileText, ShieldCheck, AlertTriangle } from "lucide-react"
import { DecisionTimelineTab } from "./decision-timeline-tab"
import { BlockDetailsTab } from "./block-details-tab"
import { VerificationTab } from "./verification-tab"
import { AnomalyFeedTab } from "./anomaly-feed-tab"
import type { BlockSummary } from "@/lib/api"

interface InsightsPanelProps {
  activeTab?: string
  onTabChange?: (tab: string) => void
  selectedBlock?: BlockSummary | null
}

export function InsightsPanel({ activeTab = "timeline", onTabChange, selectedBlock }: InsightsPanelProps) {
  const [currentTab, setCurrentTab] = useState(activeTab)

  // Sync with parent's activeTab prop
  React.useEffect(() => {
    setCurrentTab(activeTab)
  }, [activeTab])

  const handleTabChange = (value: string) => {
    setCurrentTab(value)
    onTabChange?.(value)
  }

  const handleVerifyClick = () => {
    setCurrentTab("verify")
    onTabChange?.("verify")
  }

  return (
    <Card className="glass-card border border-border/40 overflow-hidden">
      <Tabs value={currentTab} onValueChange={handleTabChange} className="w-full">
        <TabsList className="w-full grid grid-cols-4 bg-card/60 border-b border-border/40 rounded-none p-0">
          <TabsTrigger
            value="timeline"
            className="rounded-none data-[state=active]:bg-primary/10 data-[state=active]:border-b-2 data-[state=active]:border-primary transition-all"
          >
            <TrendingUp className="h-4 w-4 mr-2" />
            <span className="hidden sm:inline">Timeline</span>
          </TabsTrigger>
          <TabsTrigger
            value="details"
            className="rounded-none data-[state=active]:bg-primary/10 data-[state=active]:border-b-2 data-[state=active]:border-primary transition-all"
          >
            <FileText className="h-4 w-4 mr-2" />
            <span className="hidden sm:inline">Details</span>
          </TabsTrigger>
          <TabsTrigger
            value="verify"
            className="rounded-none data-[state=active]:bg-primary/10 data-[state=active]:border-b-2 data-[state=active]:border-primary transition-all"
          >
            <ShieldCheck className="h-4 w-4 mr-2" />
            <span className="hidden sm:inline">Verify</span>
          </TabsTrigger>
          <TabsTrigger
            value="anomalies"
            className="rounded-none data-[state=active]:bg-primary/10 data-[state=active]:border-b-2 data-[state=active]:border-primary transition-all"
          >
            <AlertTriangle className="h-4 w-4 mr-2" />
            <span className="hidden sm:inline">Anomalies</span>
          </TabsTrigger>
        </TabsList>

        <div className="p-6">
          <TabsContent value="timeline" className="mt-0">
            <DecisionTimelineTab />
          </TabsContent>

          <TabsContent value="details" className="mt-0">
            <BlockDetailsTab block={selectedBlock} onVerifyClick={handleVerifyClick} />
          </TabsContent>

          <TabsContent value="verify" className="mt-0">
            <VerificationTab />
          </TabsContent>

          <TabsContent value="anomalies" className="mt-0">
            <AnomalyFeedTab />
          </TabsContent>
        </div>
      </Tabs>
    </Card>
  )
}
