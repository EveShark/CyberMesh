import { NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

const severityMap: Record<string, "critical" | "high" | "medium" | "low"> = {
  ddos: "critical",
  dos: "high",
  malware: "high",
  anomaly: "medium",
  network_intrusion: "critical",
  policy_violation: "medium",
}

export async function GET() {
  try {
    const [aiMetrics, history] = await Promise.all([
      backendApi.getAIMetrics().catch(() => null),
      backendApi.getAIDetectionHistory({ limit: 200 }).catch(() => null),
    ])

    const byThreatType = new Map<
      string,
      {
        threatType: string
        published: number
        abstained: number
        total: number
      }
    >()

    const detections = history?.detections ?? []

    for (const detection of detections) {
      const threatType = detection.threat_type || "unknown"
      const entry = byThreatType.get(threatType) ?? {
        threatType,
        published: 0,
        abstained: 0,
        total: 0,
      }

      if (detection.should_publish) {
        entry.published += 1
      } else {
        entry.abstained += 1
      }
      entry.total += 1
      byThreatType.set(threatType, entry)
    }

    const severityBuckets = {
      critical: 0,
      high: 0,
      medium: 0,
      low: 0,
    }

    const threatBreakdown = Array.from(byThreatType.values()).map((entry) => {
      const severity = severityMap[entry.threatType] || "medium"
      severityBuckets[severity] += entry.total
      return {
        ...entry,
        severity,
      }
    })

    const publishedTotal = threatBreakdown.reduce((sum, entry) => sum + entry.published, 0)
    const abstainedTotal = threatBreakdown.reduce((sum, entry) => sum + entry.abstained, 0)

    return NextResponse.json(
      {
        timestamp: Date.now(),
        detectionLoop: aiMetrics?.loop ?? null,
        breakdown: {
          threatTypes: threatBreakdown,
          severity: severityBuckets,
          totals: {
            published: publishedTotal,
            abstained: abstainedTotal,
            overall: publishedTotal + abstainedTotal,
          },
        },
        metrics: aiMetrics,
      },
      { status: 200 },
    )
  } catch (error) {
    return NextResponse.json(
      {
        error: "threat_metrics_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
