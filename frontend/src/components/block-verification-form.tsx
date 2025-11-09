"use client"

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { CheckCircle, Search, XCircle } from "lucide-react"

import { useBlockVerification } from "@/hooks/use-block-verification"

export function BlockVerificationForm() {
  const [blockId, setBlockId] = useState("")
  const { verify, isVerifying, result, error, reset } = useBlockVerification()

  const handleVerify = async () => {
    if (!blockId.trim()) return
    reset()
    await verify(blockId.trim())
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
              placeholder="Enter block hash or height"
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

        {result?.found && (
          <Alert
            className="border-status-healthy"
          >
            <div className="flex items-center gap-2">
              <CheckCircle className="h-4 w-4 text-status-healthy" />
              <AlertDescription className="text-foreground">
                {result.message}
                {result.block ? ` • Height ${result.block.height} • Proposer ${result.block.proposer}` : ""}
              </AlertDescription>
            </div>
          </Alert>
        )}
        {error ? (
          <Alert className="border-status-critical">
            <div className="flex items-center gap-2">
              <XCircle className="h-4 w-4 text-status-critical" />
              <AlertDescription className="text-foreground">{error}</AlertDescription>
            </div>
          </Alert>
        ) : null}
      </CardContent>
    </Card>
  )
}
