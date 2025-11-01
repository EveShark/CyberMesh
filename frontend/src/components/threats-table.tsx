"use client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { AlertTriangle, Zap, Eye, Bug, Network, FileWarning, Lock } from "lucide-react"
import { ThreatEmptyState } from "./threat-empty-state"

export interface ThreatAggregate {
  threatType: string
  severity: "critical" | "high" | "medium" | "low"
  published: number
  abstained: number
  total: number
}

interface ThreatsTableProps {
  threats: ThreatAggregate[]
  onThreatSelect: (threat: ThreatAggregate) => void
  detectionLoopRunning?: boolean
  lastDetectionTime?: Date
  validatorCount?: number
}

function getThreatTypeIcon(threatType: string) {
  const type = threatType.toLowerCase()
  if (type.includes("ddos") || type.includes("dos")) {
    return <Zap className="h-4 w-4 text-red-500" />
  }
  if (type.includes("malware")) {
    return <Bug className="h-4 w-4 text-orange-500" />
  }
  if (type.includes("intrusion")) {
    return <Lock className="h-4 w-4 text-red-600" />
  }
  if (type.includes("policy")) {
    return <FileWarning className="h-4 w-4 text-yellow-500" />
  }
  if (type.includes("anomaly")) {
    return <Network className="h-4 w-4 text-blue-500" />
  }
  return <AlertTriangle className="h-4 w-4 text-muted-foreground" />
}

function getSeverityBadge(severity: ThreatAggregate["severity"]) {
  const variants = {
    critical: "destructive",
    high: "secondary",
    medium: "outline",
    low: "default",
  } as const

  return (
    <Badge variant={variants[severity] ?? "default"} className="font-medium">
      {severity.toUpperCase()}
    </Badge>
  )
}

export function ThreatsTable({ 
  threats, 
  onThreatSelect, 
  detectionLoopRunning, 
  lastDetectionTime, 
  validatorCount 
}: ThreatsTableProps) {
  // Show empty state if no threats
  if (threats.length === 0) {
    return (
      <ThreatEmptyState
        detectionLoopRunning={detectionLoopRunning}
        lastScanTime={lastDetectionTime}
        validatorCount={validatorCount}
      />
    )
  }

  return (
    <Card className="glass-card">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <AlertTriangle className="h-5 w-5 text-red-500" />
          Threat Breakdown
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="rounded-md border border-white/10 overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="border-white/10">
                <TableHead className="text-muted-foreground">Threat Type</TableHead>
                <TableHead className="text-muted-foreground">Severity</TableHead>
                <TableHead className="text-muted-foreground" title="AI recommended for blocking">
                  Blocked
                </TableHead>
                <TableHead className="text-muted-foreground" title="AI marked uncertain">
                  Monitored
                </TableHead>
                <TableHead className="text-muted-foreground" title="Percentage of threats blocked">
                  Block Rate
                </TableHead>
                <TableHead className="text-muted-foreground">Total</TableHead>
                <TableHead className="text-muted-foreground">Details</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {threats.map((threat) => {
                const publishRate = threat.total > 0 ? (threat.published / threat.total) * 100 : 0
                return (
                  <TableRow
                    key={threat.threatType}
                    className="border-white/10 hover:bg-white/5 cursor-pointer transition-colors"
                    onClick={() => onThreatSelect(threat)}
                  >
                    <TableCell>
                      <div className="flex items-center gap-3">
                        {getThreatTypeIcon(threat.threatType)}
                        <span className="font-medium capitalize">{threat.threatType.replaceAll("_", " ")}</span>
                      </div>
                    </TableCell>
                    <TableCell>{getSeverityBadge(threat.severity)}</TableCell>
                    <TableCell className="font-mono text-sm">{threat.published.toFixed(0)}</TableCell>
                    <TableCell className="font-mono text-sm">{threat.abstained.toFixed(0)}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <div className="w-20 bg-white/10 rounded-full h-2">
                          <div
                            className="bg-gradient-to-r from-blue-500 to-purple-500 h-2 rounded-full transition-all"
                            style={{ width: `${Math.min(100, Math.max(0, publishRate))}%` }}
                          />
                        </div>
                        <span className="text-sm font-medium">{publishRate.toFixed(1)}%</span>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-sm">{threat.total.toFixed(0)}</TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-8 w-8 p-0 hover:bg-white/10"
                        onClick={(event) => {
                          event.stopPropagation()
                          onThreatSelect(threat)
                        }}
                      >
                        <Eye className="h-4 w-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  )
}
