"use client"

import useSWR from "swr"

import type { DashboardOverviewResponse } from "@/lib/api"
import { rateLimitedJsonFetch } from "@/lib/rate-limited-fetch"

const DASHBOARD_REFRESH_MS = 15000
const DEDUPE_MARGIN_MS = 1000

const fetcher = async (url: string) =>
  rateLimitedJsonFetch<DashboardOverviewResponse>(
    url,
    { cache: "no-store" },
    {
      key: "dashboard-overview",
      minIntervalMs: 250,
    },
  )

export function useDashboardData(refreshInterval = DASHBOARD_REFRESH_MS) {
  const interval = typeof refreshInterval === "number" && refreshInterval > 0 ? refreshInterval : 0
  const dedupingInterval = Math.max(1000, (interval > 0 ? interval : DASHBOARD_REFRESH_MS) - DEDUPE_MARGIN_MS)

  return useSWR<DashboardOverviewResponse>("/api/dashboard/overview", fetcher, {
    refreshInterval: interval > 0 ? interval : undefined,
    dedupingInterval,
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
  })
}
