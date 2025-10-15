"use client"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { X, AlertTriangle, Shield, MapPin, Globe, Brain, FileText } from "lucide-react"
import type { Threat } from "./threats-table"

interface ThreatDetailDrawerProps {
  threat: Threat | null
  open: boolean
  onClose: () => void
}

export function ThreatDetailDrawer({ threat, open, onClose }: ThreatDetailDrawerProps) {
  if (!threat) return null

  // TODO: integrate GET /threats/{id}

  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case "critical":
        return "text-red-500"
      case "high":
        return "text-orange-500"
      case "medium":
        return "text-yellow-500"
      case "low":
        return "text-blue-500"
      default:
        return "text-gray-500"
    }
  }

  return (
    <Sheet open={open} onOpenChange={onClose}>
      <SheetContent className="w-[600px] sm:w-[800px] glass-card border-white/10">
        <SheetHeader className="space-y-4">
          <div className="flex items-center justify-between">
            <SheetTitle className="text-xl font-semibold">Threat Details</SheetTitle>
            <Button variant="ghost" size="sm" onClick={onClose} className="h-8 w-8 p-0 hover:bg-white/10">
              <X className="h-4 w-4" />
            </Button>
          </div>
          <SheetDescription className="text-left">
            <div className="flex items-center gap-3">
              <Badge variant="outline" className="font-mono">
                {threat.id}
              </Badge>
              <Badge variant={threat.severity === "critical" ? "destructive" : "secondary"} className="font-medium">
                {threat.severity.toUpperCase()}
              </Badge>
              <span className="text-sm text-muted-foreground">{threat.time}</span>
            </div>
          </SheetDescription>
        </SheetHeader>

        <div className="mt-6">
          <Tabs defaultValue="overview" className="w-full">
            <TabsList className="grid w-full grid-cols-4 bg-white/5">
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="enrichment">Enrichment</TabsTrigger>
              <TabsTrigger value="ml-scores">ML/Math</TabsTrigger>
              <TabsTrigger value="signatures">Signatures</TabsTrigger>
            </TabsList>

            <TabsContent value="overview" className="space-y-4 mt-6">
              <Card className="glass-card border-white/10">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <AlertTriangle className={`h-5 w-5 ${getSeverityColor(threat.severity)}`} />
                    Threat Overview
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="text-sm font-medium text-muted-foreground">Type</label>
                      <p className="text-lg font-semibold">{threat.type}</p>
                    </div>
                    <div>
                      <label className="text-sm font-medium text-muted-foreground">Source</label>
                      <p className="font-mono">{threat.source}</p>
                    </div>
                    <div>
                      <label className="text-sm font-medium text-muted-foreground">Affected Node</label>
                      <Badge variant="outline" className="font-mono">
                        {threat.node}
                      </Badge>
                    </div>
                    <div>
                      <label className="text-sm font-medium text-muted-foreground">Confidence</label>
                      <div className="flex items-center gap-2 mt-1">
                        <Progress value={threat.confidence} className="flex-1" />
                        <span className="text-sm font-medium">{threat.confidence}%</span>
                      </div>
                    </div>
                  </div>
                  <div>
                    <label className="text-sm font-medium text-muted-foreground">Description</label>
                    <p className="mt-1 text-sm leading-relaxed">{threat.description}</p>
                  </div>
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="enrichment" className="space-y-4 mt-6">
              <Card className="glass-card border-white/10">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <Globe className="h-5 w-5 text-blue-500" />
                    Threat Intelligence
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="grid gap-4">
                    <div className="flex items-start gap-3 p-3 rounded-lg bg-white/5">
                      <Shield className="h-5 w-5 text-orange-500 mt-0.5" />
                      <div>
                        <h4 className="font-medium">IP Reputation</h4>
                        <p className="text-sm text-muted-foreground mt-1">{threat.enrichment.ip_reputation}</p>
                      </div>
                    </div>
                    <div className="flex items-start gap-3 p-3 rounded-lg bg-white/5">
                      <MapPin className="h-5 w-5 text-green-500 mt-0.5" />
                      <div>
                        <h4 className="font-medium">Geolocation</h4>
                        <p className="text-sm text-muted-foreground mt-1">{threat.enrichment.geolocation}</p>
                      </div>
                    </div>
                    <div className="flex items-start gap-3 p-3 rounded-lg bg-white/5">
                      <AlertTriangle className="h-5 w-5 text-red-500 mt-0.5" />
                      <div>
                        <h4 className="font-medium">Threat Intelligence</h4>
                        <p className="text-sm text-muted-foreground mt-1">{threat.enrichment.threat_intel}</p>
                      </div>
                    </div>
                  </div>
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="ml-scores" className="space-y-4 mt-6">
              <Card className="glass-card border-white/10">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <Brain className="h-5 w-5 text-purple-500" />
                    Machine Learning Scores
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-6">
                  <div className="space-y-4">
                    <div>
                      <div className="flex justify-between items-center mb-2">
                        <label className="text-sm font-medium">Anomaly Score</label>
                        <span className="text-sm font-mono">{(threat.ml_scores.anomaly_score * 100).toFixed(1)}%</span>
                      </div>
                      <Progress value={threat.ml_scores.anomaly_score * 100} className="h-2" />
                    </div>
                    <div>
                      <div className="flex justify-between items-center mb-2">
                        <label className="text-sm font-medium">Risk Score</label>
                        <span className="text-sm font-mono">{(threat.ml_scores.risk_score * 100).toFixed(1)}%</span>
                      </div>
                      <Progress value={threat.ml_scores.risk_score * 100} className="h-2" />
                    </div>
                    <div>
                      <div className="flex justify-between items-center mb-2">
                        <label className="text-sm font-medium">Confidence Score</label>
                        <span className="text-sm font-mono">
                          {(threat.ml_scores.confidence_score * 100).toFixed(1)}%
                        </span>
                      </div>
                      <Progress value={threat.ml_scores.confidence_score * 100} className="h-2" />
                    </div>
                  </div>
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="signatures" className="space-y-4 mt-6">
              <Card className="glass-card border-white/10">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <FileText className="h-5 w-5 text-cyan-500" />
                    Detection Signatures
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="grid gap-2">
                    {threat.signatures.map((signature, index) => (
                      <div key={index} className="flex items-center gap-2 p-2 rounded-md bg-white/5">
                        <Badge variant="outline" className="font-mono text-xs">
                          {signature}
                        </Badge>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            </TabsContent>
          </Tabs>
        </div>
      </SheetContent>
    </Sheet>
  )
}
