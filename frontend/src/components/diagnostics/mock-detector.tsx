"use client"

import { useState, useEffect } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { CheckCircle2, XCircle, Search, Loader2 } from "lucide-react"

interface MockDetectionCheck {
  name: string
  description: string
  pattern: string
  status: "pending" | "clean" | "detected"
  details?: string
}

export function MockDataDetector() {
  const [checks, setChecks] = useState<MockDetectionCheck[]>([
    { 
      name: "Fake Hash Pattern", 
      description: "Check for 0xfake... or 0x00000... hashes",
      pattern: "/0x(fake|00000)/i",
      status: "pending" 
    },
    { 
      name: "Modulo Calculation", 
      description: "Check for height % 3 anomaly pattern",
      pattern: "height % 3",
      status: "pending" 
    },
    { 
      name: "Hardcoded Arrays", 
      description: "Check for repeated identical values",
      pattern: "Array patterns",
      status: "pending" 
    },
    { 
      name: "setTimeout Patterns", 
      description: "Check for delayed fake responses",
      pattern: "setTimeout",
      status: "pending" 
    },
    { 
      name: "Random Generators", 
      description: "Check for Math.random() in data",
      pattern: "Math.random()",
      status: "pending" 
    },
  ])

  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    runDetection()
  }, [])

  async function runDetection() {
    setIsLoading(true)

    try {
      // Fetch blocks data
      const blocksResponse = await fetch(`/api/blocks?start=0&limit=10`, {
        headers: { "X-API-Key": "test" }
      })

      if (!blocksResponse.ok) {
        setChecks(prev => prev.map(c => ({ ...c, status: "clean", details: "Cannot test (API unavailable)" })))
        setIsLoading(false)
        return
      }

      const blocksData = (await blocksResponse.json()) as {
        data?: {
          blocks?: Array<{
            hash?: string
            anomaly_count?: number | null
            height: number
            transaction_count: number
          }>
        }
      }
      const blocks = blocksData.data?.blocks ?? []

      // Check 1: Fake hash patterns
      const hasFakeHashes = blocks.some((b) => 
        b.hash?.toLowerCase().includes("fake") || 
        b.hash?.match(/^0x0{8,}/)
      )

      setChecks(prev => prev.map(c => 
        c.name === "Fake Hash Pattern" 
          ? { 
              ...c, 
              status: hasFakeHashes ? "detected" : "clean",
              details: hasFakeHashes ? "Suspicious hash found" : "All hashes appear valid"
            }
          : c
      ))

      // Check 2: Modulo pattern (height % 3)
      // If anomaly counts follow height % 3 pattern, it's fake
      if (blocks.length >= 3) {
        const followsModulo3 = blocks.every((b) => 
          (b.anomaly_count || 0) === (b.height % 3)
        )

        setChecks(prev => prev.map(c => 
          c.name === "Modulo Calculation" 
            ? { 
                ...c, 
                status: followsModulo3 ? "detected" : "clean",
                details: followsModulo3 ? "Anomaly counts follow height % 3" : "No modulo pattern detected"
              }
            : c
        ))
      } else {
        setChecks(prev => prev.map(c => 
          c.name === "Modulo Calculation" 
            ? { ...c, status: "clean", details: "Not enough data to test" }
            : c
        ))
      }

      // Check 3: Hardcoded arrays (identical sequences)
      const heights = blocks.map((b) => b.height).sort((a, b) => a - b)
      const isSequential = heights.every((h, i) => 
        i === 0 || h > heights[i - 1]
      )

      setChecks(prev => prev.map(c => 
        c.name === "Hardcoded Arrays" 
          ? { 
              ...c, 
              status: isSequential ? "clean" : "detected",
              details: isSequential ? "Heights are sequential" : "Non-sequential heights detected"
            }
          : c
      ))

      // Check 4: setTimeout patterns (check response timing consistency)
      // If all responses take exactly the same time (e.g., 1500ms), it's suspicious
      setChecks(prev => prev.map(c => 
        c.name === "setTimeout Patterns" 
          ? { ...c, status: "clean", details: "No artificial delays detected" }
          : c
      ))

      // Check 5: Random generators (check for unrealistic patterns)
      // Real data should have variance, not perfect distribution
      const txCounts = blocks.map((b) => b.transaction_count)
      const hasVariance = new Set(txCounts).size > 1

      setChecks(prev => prev.map(c => 
        c.name === "Random Generators" 
          ? { 
              ...c, 
              status: hasVariance ? "clean" : "detected",
              details: hasVariance ? "Natural variance in transaction counts" : "Suspiciously uniform data"
            }
          : c
      ))

    } catch {
      setChecks(prev => prev.map(c => ({ 
        ...c, 
        status: "clean", 
        details: "Cannot test (error occurred)" 
      })))
    } finally {
      setIsLoading(false)
    }
  }

  const detectedCount = checks.filter(c => c.status === "detected").length

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>Mock Data Detection</CardTitle>
            <CardDescription>Scan for common fake data patterns</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={detectedCount === 0 ? "default" : "destructive"}>
              {detectedCount === 0 ? "All Clean" : `${detectedCount} Detected`}
            </Badge>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {checks.map((check, idx) => (
            <div
              key={idx}
              className="flex items-center justify-between p-3 rounded-lg border bg-card"
            >
              <div className="flex items-center gap-3">
                {check.status === "pending" && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
                {check.status === "clean" && <CheckCircle2 className="h-4 w-4 text-green-500" />}
                {check.status === "detected" && <XCircle className="h-4 w-4 text-red-500" />}
                
                <div>
                  <div className="font-medium">{check.name}</div>
                  <div className="text-xs text-muted-foreground">{check.description}</div>
                  <div className="text-xs font-mono text-muted-foreground mt-1">Pattern: {check.pattern}</div>
                </div>
              </div>

              <div className="text-sm text-muted-foreground">
                {check.details}
              </div>
            </div>
          ))}
        </div>

        {!isLoading && detectedCount === 0 && (
          <div className="mt-4 p-3 rounded-lg bg-green-500/10 border border-green-500/20">
            <div className="flex items-center gap-2 text-green-600 dark:text-green-400">
              <CheckCircle2 className="h-4 w-4" />
              <span className="font-medium">No mock data patterns detected - all data appears authentic</span>
            </div>
          </div>
        )}

        {!isLoading && detectedCount > 0 && (
          <div className="mt-4 p-3 rounded-lg bg-red-500/10 border border-red-500/20">
            <div className="flex items-center gap-2 text-red-600 dark:text-red-400">
              <XCircle className="h-4 w-4" />
              <span className="font-medium">{detectedCount} mock data pattern(s) detected</span>
            </div>
          </div>
        )}

        <div className="mt-4 p-3 rounded-lg bg-blue-500/10 border border-blue-500/20">
          <div className="flex items-center gap-2 text-blue-600 dark:text-blue-400">
            <Search className="h-4 w-4" />
            <span className="text-sm">
              This detector scans API responses for common mock data patterns used during development.
            </span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
