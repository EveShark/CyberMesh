"use client"

import { useMemo } from "react"

import { useDashboardData } from "@/hooks/use-dashboard-data"

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

function mapStatus(status: "healthy" | "warning" | "critical"): "healthy" | "degraded" | "down" {
  if (status === "healthy") return "healthy"
  if (status === "warning") return "degraded"
  return "down"
}

const ROLE_CYCLE: NodeHealth["role"][] = ["GW", "PROC", "PROC", "CRDB", "AI"]

export function useHealth(pollMs = 2000) {
  const { data: dashboard, error, isLoading } = useDashboardData(pollMs)

  const network = dashboard?.network

  const peersByNode = useMemo(() => {
    if (!network?.edges) return new Map<string, Set<string>>()
    const map = new Map<string, Set<string>>()
    for (const edge of network.edges) {
      const { source, target } = edge
      if (!source || !target) continue
      if (!map.has(source)) map.set(source, new Set<string>())
      if (!map.has(target)) map.set(target, new Set<string>())
      map.get(source)?.add(target)
      map.get(target)?.add(source)
    }
    return map
  }, [network?.edges])

  const mapped: HealthPayload | undefined = useMemo(() => {
    if (!network?.nodes?.length) return undefined
    const nodes = network.nodes.slice(0, 5).map((node, index) => {
      const peers = peersByNode.get(node.id)
      return {
        id: node.id,
        role: ROLE_CYCLE[index % ROLE_CYCLE.length],
        status: mapStatus(node.status),
        peers: peers ? Array.from(peers) : [],
      }
    })

    return {
      nodes,
      updatedAt: network.updated_at,
    }
  }, [network, peersByNode])

  const primaryLoading = !dashboard && isLoading

  return { data: mapped, error, isLoading: primaryLoading }
}
