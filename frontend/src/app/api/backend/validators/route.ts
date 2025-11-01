import { NextRequest, NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

export async function GET(request: NextRequest) {
  try {
    const { searchParams } = new URL(request.url)
    const status = searchParams.get("status") as "active" | "inactive" | null

    const validators = await backendApi.listValidators(status === "active" || status === "inactive" ? status : undefined)

    return NextResponse.json({ validators }, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "backend_validators_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
