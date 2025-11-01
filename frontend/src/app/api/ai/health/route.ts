export const dynamic = "force-dynamic"
export const revalidate = 0

import { NextResponse } from "next/server"

import { aiApi } from "@/lib/api"

export async function GET() {
  try {
    const [health, ready] = await Promise.all([aiApi.getHealth(), aiApi.getReady()])
    return NextResponse.json({ health, ready }, { status: 200 })
  } catch (error) {
    console.warn("AI health probe failed", error)
    return NextResponse.json(
      {
        health: null,
        ready: null,
        degraded: true,
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 200 },
    )
  }
}
