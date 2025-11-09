import { NextResponse } from "next/server"

import { backendApi, type DashboardOverviewResponse } from "@/lib/api"

const CACHE_TTL_MS = 5000

let cached: { data: DashboardOverviewResponse; expires: number } | null = null

const cacheStats = {
  hits: 0,
  misses: 0,
  errors: 0,
}

function recordCacheEvent(event: "hit" | "miss" | "error") {
  cacheStats[event] += 1
  if (process.env.NODE_ENV !== "production") {
    console.debug("[dashboard-cache]", event, cacheStats[event])
  }
  if (typeof globalThis !== "undefined") {
    ;(globalThis as typeof globalThis & { __CYBERMESH_CACHE_STATS__?: typeof cacheStats }).__CYBERMESH_CACHE_STATS__ = cacheStats
  }
}

export async function GET() {
  try {
    const now = Date.now()
    if (cached && cached.expires > now) {
      recordCacheEvent("hit")
      const response = NextResponse.json(cached.data, { status: 200 })
      response.headers.set("X-Cache-Status", "HIT")
      response.headers.set("X-Cache-Ttl", String(Math.max(0, cached.expires - now)))
      return response
    }

    recordCacheEvent("miss")
    const overview = await backendApi.getDashboardOverview()
    cached = {
      data: overview,
      expires: now + CACHE_TTL_MS,
    }

    const response = NextResponse.json(overview, { status: 200 })
    response.headers.set("X-Cache-Status", "MISS")
    response.headers.set("X-Cache-Ttl", String(CACHE_TTL_MS))
    return response
  } catch (error) {
    recordCacheEvent("error")
    return NextResponse.json(
      {
        error: "dashboard_overview_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
