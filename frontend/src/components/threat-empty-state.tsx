"use client"

import { Shield, Activity, CheckCircle } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"

interface ThreatEmptyStateProps {
  detectionLoopRunning?: boolean
  lastScanTime?: Date
  validatorCount?: number
}

export function ThreatEmptyState({ detectionLoopRunning = false, lastScanTime, validatorCount }: ThreatEmptyStateProps) {
  return (
    <Card className="glass-card border-white/10">
      <CardContent className="p-12">
        <div className="flex flex-col items-center justify-center text-center space-y-6">
          {/* Icon */}
          <div className="relative">
            <div className="absolute inset-0 bg-gradient-to-br from-green-500/20 to-blue-500/20 rounded-full blur-2xl" />
            <div className="relative p-6 rounded-full bg-gradient-to-br from-green-500/10 to-blue-500/10 border border-white/10">
              <Shield className="h-16 w-16 text-green-500" />
            </div>
          </div>

          {/* Main message */}
          <div className="space-y-2">
            <h3 className="text-2xl font-semibold text-foreground">No threats detected yet</h3>
            <p className="text-muted-foreground max-w-md">
              Your system is protected and actively monitoring. Threats will appear here when detected by the AI service.
            </p>
          </div>

          {/* Status indicators */}
          <div className="flex flex-col gap-3 text-sm">
            <div className="flex items-center gap-2">
              {detectionLoopRunning ? (
                <>
                  <Activity className="h-4 w-4 text-green-500 animate-pulse" />
                  <span className="text-foreground font-medium">Live Monitoring: ACTIVE</span>
                </>
              ) : (
                <>
                  <Activity className="h-4 w-4 text-yellow-500" />
                  <span className="text-muted-foreground">Detection Loop: PAUSED</span>
                </>
              )}
            </div>

            {lastScanTime && (
              <div className="flex items-center gap-2">
                <CheckCircle className="h-4 w-4 text-blue-500" />
                <span className="text-muted-foreground">
                  Last scan: <span className="text-foreground font-medium">{lastScanTime.toLocaleTimeString()}</span>
                </span>
              </div>
            )}

            {validatorCount !== undefined && (
              <div className="flex items-center gap-2">
                <Shield className="h-4 w-4 text-purple-500" />
                <span className="text-muted-foreground">
                  Validators active: <span className="text-foreground font-medium">{validatorCount}</span>
                </span>
              </div>
            )}
          </div>

          {/* Additional info */}
          <div className="pt-4 border-t border-white/10 w-full max-w-md">
            <p className="text-xs text-muted-foreground">
              The AI detection service is continuously analyzing network traffic and system behavior. Detected threats will
              undergo Byzantine Fault Tolerant consensus validation before being displayed.
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
