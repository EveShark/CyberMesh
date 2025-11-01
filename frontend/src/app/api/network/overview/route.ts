import { NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

export async function GET() {
  try {
    const overview = await backendApi.getNetworkOverview()
    return NextResponse.json(overview, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "network_overview_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
