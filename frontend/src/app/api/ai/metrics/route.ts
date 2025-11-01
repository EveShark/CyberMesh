import { NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

export async function GET() {
  try {
    const metrics = await backendApi.getAIMetrics()
    return NextResponse.json({ metrics }, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "ai_metrics_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
