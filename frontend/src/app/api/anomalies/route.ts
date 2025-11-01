import { type NextRequest, NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

export async function GET(req: NextRequest) {
  try {
    const { searchParams } = new URL(req.url)
    const severity = searchParams.get("severity") || undefined
    const limitParam = searchParams.get("limit")
    const limit = limitParam ? Number.parseInt(limitParam, 10) : 100

    const [anomalies, stats] = await Promise.all([
      backendApi.getAnomalies(limit, severity === "all" ? undefined : severity ?? undefined),
      backendApi.getAnomalyStats(),
    ])

    return NextResponse.json({ anomalies, stats }, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "backend_anomalies_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
