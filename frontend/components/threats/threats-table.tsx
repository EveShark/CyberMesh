"use client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { AlertTriangle, Shield, Zap, Eye } from "lucide-react"

interface Threat {
  id: string
  time: string
  type: string
  severity: "critical" | "high" | "medium" | "low"
  confidence: number
  source: string
  node: string
  description: string
  enrichment: {
    ip_reputation: string
    geolocation: string
    threat_intel: string
  }
  ml_scores: {
    anomaly_score: number
    risk_score: number
    confidence_score: number
  }
  signatures: string[]
}

interface ThreatsTableProps {
  onThreatSelect: (threat: Threat) => void
}

// TODO: integrate GET /threats/recent
const mockThreats: Threat[] = [
  {
    id: "THR-2025-001",
    time: "2025-01-13 14:32:15",
    type: "SQL Injection",
    severity: "critical",
    confidence: 95,
    source: "192.168.1.45",
    node: "GW-03",
    description: "Attempted SQL injection attack detected on user authentication endpoint",
    enrichment: {
      ip_reputation: "Known malicious IP",
      geolocation: "Unknown/Tor Exit Node",
      threat_intel: "Associated with APT group",
    },
    ml_scores: {
      anomaly_score: 0.92,
      risk_score: 0.88,
      confidence_score: 0.95,
    },
    signatures: ["SQLI-001", "AUTH-BYPASS-003", "TOR-EXIT-NODE"],
  },
  {
    id: "THR-2025-002",
    time: "2025-01-13 14:28:42",
    type: "DDoS",
    severity: "high",
    confidence: 87,
    source: "Multiple IPs",
    node: "GW-01",
    description: "Distributed denial of service attack targeting API endpoints",
    enrichment: {
      ip_reputation: "Botnet cluster",
      geolocation: "Global distribution",
      threat_intel: "Mirai variant detected",
    },
    ml_scores: {
      anomaly_score: 0.85,
      risk_score: 0.79,
      confidence_score: 0.87,
    },
    signatures: ["DDOS-HTTP-001", "BOTNET-MIRAI", "RATE-LIMIT-EXCEEDED"],
  },
  {
    id: "THR-2025-003",
    time: "2025-01-13 14:25:18",
    type: "Brute Force",
    severity: "medium",
    confidence: 78,
    source: "203.45.67.89",
    node: "AI-01",
    description: "Multiple failed authentication attempts detected",
    enrichment: {
      ip_reputation: "Suspicious activity",
      geolocation: "Eastern Europe",
      threat_intel: "Credential stuffing campaign",
    },
    ml_scores: {
      anomaly_score: 0.72,
      risk_score: 0.65,
      confidence_score: 0.78,
    },
    signatures: ["BRUTE-FORCE-001", "AUTH-FAIL-PATTERN", "CREDENTIAL-STUFF"],
  },
  {
    id: "THR-2025-004",
    time: "2025-01-13 14:20:33",
    type: "Malware",
    severity: "high",
    confidence: 91,
    source: "Internal",
    node: "PROC-01",
    description: "Suspicious file execution detected in processing pipeline",
    enrichment: {
      ip_reputation: "Internal system",
      geolocation: "Data Center",
      threat_intel: "Ransomware signature match",
    },
    ml_scores: {
      anomaly_score: 0.89,
      risk_score: 0.84,
      confidence_score: 0.91,
    },
    signatures: ["MALWARE-EXEC-001", "RANSOMWARE-SIG", "FILE-ANOMALY"],
  },
  {
    id: "THR-2025-005",
    time: "2025-01-13 14:15:07",
    type: "Data Exfiltration",
    severity: "critical",
    confidence: 93,
    source: "10.0.2.15",
    node: "CRDB-02",
    description: "Unusual data access patterns detected in customer database",
    enrichment: {
      ip_reputation: "Internal compromised",
      geolocation: "Internal Network",
      threat_intel: "Insider threat indicators",
    },
    ml_scores: {
      anomaly_score: 0.94,
      risk_score: 0.91,
      confidence_score: 0.93,
    },
    signatures: ["DATA-EXFIL-001", "INSIDER-THREAT", "DB-ANOMALY-ACCESS"],
  },
]

const getSeverityIcon = (severity: string) => {
  switch (severity) {
    case "critical":
      return <AlertTriangle className="h-4 w-4 text-red-500" />
    case "high":
      return <Shield className="h-4 w-4 text-orange-500" />
    case "medium":
      return <Zap className="h-4 w-4 text-yellow-500" />
    case "low":
      return <Eye className="h-4 w-4 text-blue-500" />
    default:
      return <Shield className="h-4 w-4 text-gray-500" />
  }
}

const getSeverityBadge = (severity: string) => {
  const variants = {
    critical: "destructive",
    high: "secondary",
    medium: "outline",
    low: "default",
  } as const

  return (
    <Badge variant={variants[severity as keyof typeof variants] || "default"} className="font-medium">
      {severity.toUpperCase()}
    </Badge>
  )
}

export function ThreatsTable({ onThreatSelect }: ThreatsTableProps) {
  return (
    <Card className="glass-card">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <AlertTriangle className="h-5 w-5 text-red-500" />
          Active Threats
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="rounded-md border border-white/10">
          <Table>
            <TableHeader>
              <TableRow className="border-white/10 hover:bg-white/5">
                <TableHead className="text-muted-foreground">ID</TableHead>
                <TableHead className="text-muted-foreground">Time</TableHead>
                <TableHead className="text-muted-foreground">Type</TableHead>
                <TableHead className="text-muted-foreground">Severity</TableHead>
                <TableHead className="text-muted-foreground">Confidence</TableHead>
                <TableHead className="text-muted-foreground">Source</TableHead>
                <TableHead className="text-muted-foreground">Node</TableHead>
                <TableHead className="text-muted-foreground">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {mockThreats.map((threat) => (
                <TableRow
                  key={threat.id}
                  className="border-white/10 hover:bg-white/5 cursor-pointer transition-colors"
                  onClick={() => onThreatSelect(threat)}
                >
                  <TableCell className="font-mono text-sm">{threat.id}</TableCell>
                  <TableCell className="text-sm text-muted-foreground">{threat.time}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      {getSeverityIcon(threat.severity)}
                      <span className="font-medium">{threat.type}</span>
                    </div>
                  </TableCell>
                  <TableCell>{getSeverityBadge(threat.severity)}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <div className="w-16 bg-white/10 rounded-full h-2">
                        <div
                          className="bg-gradient-to-r from-blue-500 to-purple-500 h-2 rounded-full transition-all"
                          style={{ width: `${threat.confidence}%` }}
                        />
                      </div>
                      <span className="text-sm font-medium">{threat.confidence}%</span>
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-sm">{threat.source}</TableCell>
                  <TableCell>
                    <Badge variant="outline" className="font-mono">
                      {threat.node}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-8 w-8 p-0 hover:bg-white/10"
                      onClick={(e) => {
                        e.stopPropagation()
                        onThreatSelect(threat)
                      }}
                    >
                      <Eye className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  )
}

export type { Threat }
