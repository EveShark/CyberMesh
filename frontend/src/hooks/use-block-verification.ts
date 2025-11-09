"use client"

import { useState, useCallback } from "react"

import { useDashboardData } from "./use-dashboard-data"

interface VerificationResponse {
  found: boolean
  message: string
  block?: {
    height: number
    hash: string
    proposer: string
    timestamp: number
  }
}

export function useBlockVerification() {
  const [isVerifying, setIsVerifying] = useState(false)
  const [result, setResult] = useState<VerificationResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const { data: dashboard, mutate } = useDashboardData(15000)

  const verify = useCallback(async (identifier: string) => {
    if (!identifier) return

    setIsVerifying(true)
    setResult(null)
    setError(null)

    try {
      const snapshot = dashboard ?? (await mutate())
      if (!snapshot) {
        throw new Error("Dashboard snapshot unavailable")
      }

      const blocks = snapshot.blocks?.recent ?? []
      if (blocks.length === 0) {
        throw new Error("No blocks available in snapshot")
      }

      const normalized = identifier.toLowerCase()
      let match = null

      const numericHeight = Number.parseInt(identifier, 10)
      if (!Number.isNaN(numericHeight)) {
        match = blocks.find((block) => block.height === numericHeight) ?? null
      }

      if (!match) {
        match = blocks.find((block) => block.hash.toLowerCase() === normalized) ?? null
      }

      if (!match) {
        setResult(null)
        setError("Block not found in recent snapshot")
        return
      }

      setResult({
        found: true,
        message: `Block ${match.height} verified`,
        block: {
          height: match.height,
          hash: match.hash,
          proposer: match.proposer,
          timestamp: match.timestamp,
        },
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown verification error")
    } finally {
      setIsVerifying(false)
    }
  }, [dashboard, mutate])

  return {
    verify,
    isVerifying,
    result,
    error,
    reset: () => {
      setResult(null)
      setError(null)
    },
  }
}
