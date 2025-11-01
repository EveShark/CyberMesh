"use client"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { ThreatAggregate } from "./threats-table"
import { Cross2Icon } from "@radix-ui/react-icons"
import { TrendingUp, Shield, AlertTriangle, Info, CheckCircle, XCircle } from "lucide-react"

interface ThreatDetailDrawerProps {
  threat: ThreatAggregate | null
  open: boolean
  onClose: () => void
  lastDetectionTime?: Date
}

export function ThreatDetailDrawer({ threat, open, onClose, lastDetectionTime }: ThreatDetailDrawerProps) {
  if (!threat) return null

  const publishRate = threat.total > 0 ? (threat.published / threat.total) * 100 : 0

  return (
    <Sheet open={open} onOpenChange={onClose}>
      <SheetContent className="w-[520px] sm:w-[640px] glass-card border-white/10">
        <SheetHeader className="space-y-4">
          <div className="flex items-center justify-between">
            <SheetTitle className="text-xl font-semibold capitalize">{threat.threatType.replaceAll("_", " ")}</SheetTitle>
            <Button variant="ghost" size="sm" onClick={onClose} className="h-8 w-8 p-0 hover:bg-white/10">
              <Cross2Icon className="h-4 w-4" />
            </Button>
          </div>
          <SheetDescription className="text-left">
            <div className="flex items-center gap-3">
              <Badge variant={threat.severity === "critical" ? "destructive" : "outline"}>{threat.severity.toUpperCase()}</Badge>
              <span className="text-xs text-muted-foreground">
                Last detection {lastDetectionTime ? lastDetectionTime.toLocaleString() : "unknown"}
              </span>
            </div>
          </SheetDescription>
        </SheetHeader>

        <div className="mt-6 space-y-4">
          {/* Detection Metrics Card */}
          <Card className="glass-card border-white/10">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <TrendingUp className="h-4 w-4 text-blue-500" /> Detection Statistics
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <CheckCircle className="h-3.5 w-3.5 text-green-500" />
                    <p className="text-xs text-muted-foreground">Blocked</p>
                  </div>
                  <p className="text-2xl font-bold text-foreground">{threat.published.toFixed(0)}</p>
                  <p className="text-xs text-muted-foreground">AI recommended action</p>
                </div>
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <XCircle className="h-3.5 w-3.5 text-yellow-500" />
                    <p className="text-xs text-muted-foreground">Monitored</p>
                  </div>
                  <p className="text-2xl font-bold text-foreground">{threat.abstained.toFixed(0)}</p>
                  <p className="text-xs text-muted-foreground">AI marked uncertain</p>
                </div>
              </div>
              
              <Separator className="bg-white/10" />
              
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Block Rate</span>
                  <span className="text-sm font-semibold text-foreground">{publishRate.toFixed(1)}%</span>
                </div>
                <div className="w-full bg-white/10 rounded-full h-2">
                  <div
                    className={`h-2 rounded-full transition-all ${
                      publishRate > 70 ? "bg-gradient-to-r from-green-500 to-emerald-500" : "bg-gradient-to-r from-yellow-500 to-orange-500"
                    }`}
                    style={{ width: `${Math.min(100, Math.max(0, publishRate))}%` }}
                  />
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">Total Detections</span>
                  <span className="text-xs font-medium text-foreground">{threat.total.toFixed(0)}</span>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* AI Confidence Card */}
          <Card className="glass-card border-white/10">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <Shield className="h-4 w-4 text-purple-500" /> AI Confidence Analysis
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <div className="flex items-start gap-3">
                <div className={`mt-0.5 p-1.5 rounded ${publishRate > 70 ? "bg-green-500/10" : "bg-yellow-500/10"}`}>
                  {publishRate > 70 ? (
                    <CheckCircle className="h-4 w-4 text-green-500" />
                  ) : (
                    <AlertTriangle className="h-4 w-4 text-yellow-500" />
                  )}
                </div>
                <div className="space-y-1">
                  <p className="font-medium text-foreground">
                    {publishRate > 70 ? "High Confidence" : "Moderate Confidence"}
                  </p>
                  <p className="text-muted-foreground text-xs leading-relaxed">
                    {publishRate > 70
                      ? `Strong agreement from AI with ${publishRate.toFixed(1)}% block rate. These detections are highly reliable and recommended for automated blocking.`
                      : `Moderate agreement from AI with ${publishRate.toFixed(1)}% block rate. Review abstentions to understand edge cases and improve detection accuracy.`}
                  </p>
                </div>
              </div>
              
              <Separator className="bg-white/10" />
              
              <div className="flex items-start gap-3">
                <div className="mt-0.5 p-1.5 rounded bg-blue-500/10">
                  <Info className="h-4 w-4 text-blue-500" />
                </div>
                <div className="space-y-1">
                  <p className="font-medium text-foreground">Recommended Action</p>
                  <p className="text-muted-foreground text-xs leading-relaxed">
                    Monitor <span className="font-medium text-foreground">{threat.threatType.replaceAll("_", " ")}</span> patterns
                    and adjust detection thresholds based on false positive feedback from validators.
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Placeholder for future features */}
          <div className="p-4 rounded-lg border border-white/10 bg-white/5">
            <p className="text-xs text-muted-foreground text-center">
              <span className="font-medium">Coming Soon:</span> Validator consensus breakdown, response time metrics, and
              detailed threat timeline will appear here once backend APIs are ready.
            </p>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
