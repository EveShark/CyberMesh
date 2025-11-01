import { type NextRequest, NextResponse } from "next/server"

import { backendApi } from "@/lib/api"
import { deduplicateBlocks } from "@/lib/validators"

export async function GET(req: NextRequest) {
  try {
    const { searchParams } = new URL(req.url)
    const limit = Number.parseInt(searchParams.get("limit") ?? "20", 10)
    const normalizedLimit = Number.isNaN(limit) ? 20 : Math.min(Math.max(limit, 1), 100)

    const startParam = searchParams.get("start")
    let startValue: number | undefined
    if (startParam !== null) {
      const parsed = Number.parseInt(startParam, 10)
      startValue = Number.isNaN(parsed) ? undefined : Math.max(parsed, 0)
    }

    const blocksResponse = await backendApi.listBlocks(startValue, normalizedLimit)
    const validBlocks = deduplicateBlocks(blocksResponse.blocks)

    return NextResponse.json(
      {
        blocks: validBlocks,
        pagination: {
          ...blocksResponse.pagination,
          returnedCount: validBlocks.length,
        },
      },
      { status: 200 },
    )
  } catch (error) {
    return NextResponse.json(
      {
        error: "backend_blocks_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
