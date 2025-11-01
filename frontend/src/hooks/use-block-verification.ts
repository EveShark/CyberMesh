"use client"

import { useState, useCallback } from "react"

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

  const verify = useCallback(async (identifier: string) => {
    if (!identifier) return

    setIsVerifying(true)
    setResult(null)
    setError(null)

    try {
      const response = await fetch(`/api/ledger/verify?id=${encodeURIComponent(identifier)}`, {
        method: "GET",
        cache: "no-store",
      })

      if (!response.ok) {
        const text = await response.text().catch(() => "")
        let message = text
        try {
          const parsed = JSON.parse(text)
          if (typeof parsed.message === "string") {
            message = parsed.message
          }
        } catch {
          // ignore JSON parse errors
        }
        throw new Error(message || `Verification failed (${response.status})`)
      }

      const verification = (await response.json()) as VerificationResponse
      setResult(verification)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown verification error")
    } finally {
      setIsVerifying(false)
    }
  }, [])

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
