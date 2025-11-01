import { NextResponse } from "next/server"

import { backendApi, parsePrometheusText } from "@/lib/api"

export async function GET() {
  try {
    const text = await backendApi.getMetricsText()
    const samples = parsePrometheusText(text)

    return NextResponse.json(
      {
        raw: text,
        samples,
      },
      { status: 200 },
    )
  } catch (error) {
    return NextResponse.json(
      {
        error: "backend_metrics_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
