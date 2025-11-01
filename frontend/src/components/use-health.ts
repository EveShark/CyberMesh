"use client"

import useSWR from "swr"

import type { NetworkOverview } from "@/p2p-consensus/lib/types"

export type NodeHealth = {
  id: string
  role: "AI" | "PROC" | "GW" | "CRDB"
  status: "healthy" | "degraded" | "down"
  peers?: string[]
}

export type HealthPayload = {
  nodes: NodeHealth[] // expected 5 nodes total
  updatedAt: string
}

const fetcher = (url: string) => fetch(url, { cache: "no-store" }).then((r) => {
  if (!r.ok) throw new Error(`Request failed: ${r.status}`)
  return r.json()
})

function mapStatus(status: "healthy" | "warning" | "critical"): "healthy" | "degraded" | "down" {
  if (status === "healthy") return "healthy"
  if (status === "warning") return "degraded"
  return "down"
}

const ROLE_CYCLE: NodeHealth["role"][] = ["GW", "PROC", "PROC", "CRDB", "AI"]

export function useHealth(pollMs = 2000) {
  const { data, error, isLoading } = useSWR<NetworkOverview>("/api/network/overview", fetcher, {
    refreshInterval: pollMs,
    revalidateOnFocus: false,
  })

  const mapped: HealthPayload | undefined = data
    ? {
        nodes: data.nodes.slice(0, 5).map((node, index) => ({
          id: node.id,
          role: ROLE_CYCLE[index % ROLE_CYCLE.length],
          status: mapStatus(node.status),
          peers: [],
        })),
        updatedAt: data.updatedAt,
      }
    : undefined

  return { data: mapped, error, isLoading }
}
