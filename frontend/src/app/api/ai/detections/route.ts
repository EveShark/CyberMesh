import { NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

export async function GET() {
  try {
    const [history, suspicious] = await Promise.all([
      backendApi.getAIDetectionHistory(),
      backendApi.getAISuspiciousNodes(),
    ])

    return NextResponse.json({ history, suspicious }, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "ai_detections_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
