import { NextRequest, NextResponse } from "next/server"

import { backendApi } from "@/lib/api"

export async function GET(req: NextRequest) {
  const { searchParams } = new URL(req.url)
  const id = searchParams.get("id")?.trim()

  if (!id) {
    return NextResponse.json({ found: false, message: "Missing block identifier" }, { status: 400 })
  }

  try {
    const numericHeight = Number.parseInt(id, 10)
    if (!Number.isNaN(numericHeight)) {
      const block = await backendApi.getBlockByHeight(numericHeight, false)
      return NextResponse.json(
        {
          found: true,
          message: `Block ${block.height} verified`,
          block: {
            height: block.height,
            hash: block.hash,
            proposer: block.proposer,
            timestamp: block.timestamp,
          },
        },
        { status: 200 },
      )
    }

    const list = await backendApi.listBlocks(0, 50)
    const match = list.blocks.find((block) => block.hash.toLowerCase() === id.toLowerCase())

    if (match) {
      return NextResponse.json(
        {
          found: true,
          message: `Block ${match.hash} verified`,
          block: {
            height: match.height,
            hash: match.hash,
            proposer: match.proposer,
            timestamp: match.timestamp,
          },
        },
        { status: 200 },
      )
    }

    return NextResponse.json({ found: false, message: "Block not found in recent history" }, { status: 404 })
  } catch (error) {
    return NextResponse.json(
      {
        found: false,
        message: error instanceof Error ? error.message : "Verification error",
      },
      { status: 502 },
    )
  }
}
