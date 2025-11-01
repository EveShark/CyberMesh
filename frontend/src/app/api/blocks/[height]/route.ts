import { NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

interface RouteParams {
  params: {
    height: string
  }
}

export async function GET(_: Request, { params }: RouteParams) {
  try {
    const height = Number.parseInt(params.height, 10)
    if (Number.isNaN(height) || height < 0) {
      return NextResponse.json(
        {
          error: "invalid_height",
          message: "Height must be a non-negative integer",
        },
        { status: 400 },
      )
    }

    const block = await backendApi.getBlockByHeight(height, true)
    return NextResponse.json({ block }, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "backend_block_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
