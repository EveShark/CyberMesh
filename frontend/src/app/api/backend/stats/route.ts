import { NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

export async function GET() {
  try {
    const stats = await backendApi.getStats()
    return NextResponse.json({ stats }, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "backend_stats_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
