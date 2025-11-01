import { NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

export async function GET() {
  try {
    const [health, readiness, stats] = await Promise.all([
      backendApi.getHealth(),
      backendApi.getReady(),
      backendApi.getStats(),
    ])

    return NextResponse.json(
      {
        health,
        readiness,
        stats,
      },
      { status: 200 },
    )
  } catch (error) {
    return NextResponse.json(
      {
        error: "backend_health_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
