"use client"

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { CheckCircle, XCircle, Search } from "lucide-react"

export function BlockVerificationForm() {
  const [blockId, setBlockId] = useState("")
  const [verificationResult, setVerificationResult] = useState<{
    status: "success" | "error" | null
    message: string
  }>({ status: null, message: "" })
  const [isVerifying, setIsVerifying] = useState(false)

  const handleVerify = async () => {
    if (!blockId.trim()) return

    setIsVerifying(true)

    // Mock verification logic
    setTimeout(() => {
      // Simulate random verification results
      const isValid = Math.random() > 0.3
      setVerificationResult({
        status: isValid ? "success" : "error",
        message: isValid
          ? `Block ${blockId} is valid and confirmed on the blockchain.`
          : `Block ${blockId} verification failed. Block not found or corrupted.`,
      })
      setIsVerifying(false)
    }, 1500)

    // TODO: integrate GET /blockchain/verify/{id}
  }

  return (
    <Card className="glass-card">
      <CardHeader>
        <CardTitle className="text-xl font-semibold text-foreground">Block Verification</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="blockId" className="text-foreground">
            Block ID
          </Label>
          <div className="flex gap-2">
            <Input
              id="blockId"
              placeholder="Enter block ID (e.g., 0x1a2b3c4d)"
              value={blockId}
              onChange={(e) => setBlockId(e.target.value)}
              className="font-mono"
            />
            <Button
              onClick={handleVerify}
              disabled={!blockId.trim() || isVerifying}
              className="bg-primary hover:bg-primary/90"
            >
              {isVerifying ? (
                <div className="animate-spin h-4 w-4 border-2 border-white border-t-transparent rounded-full" />
              ) : (
                <Search className="h-4 w-4" />
              )}
              {isVerifying ? "Verifying..." : "Verify"}
            </Button>
          </div>
        </div>

        {verificationResult.status && (
          <Alert
            className={verificationResult.status === "success" ? "border-status-healthy" : "border-status-critical"}
          >
            <div className="flex items-center gap-2">
              {verificationResult.status === "success" ? (
                <CheckCircle className="h-4 w-4 text-status-healthy" />
              ) : (
                <XCircle className="h-4 w-4 text-status-critical" />
              )}
              <AlertDescription className="text-foreground">{verificationResult.message}</AlertDescription>
            </div>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}
