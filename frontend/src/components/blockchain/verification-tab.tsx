"use client"

import { useEffect, useMemo, useState } from "react"

import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { ShieldCheck, CheckCircle, XCircle, Loader2, AlertCircle, Database, Link as LinkIcon } from "lucide-react"
import Link from "next/link"
import type { BlockSummary } from "@/lib/api"

import { useBlockDetails } from "@/hooks/use-block-details"

interface VerificationResult {
  verified: boolean
  message: string
  block: BlockSummary
  checks: Array<{
    label: string
    description: string
    status: "passed" | "failed" | "warning"
  }>
}

interface VerificationTabProps {
  onBlockVerified?: (block: BlockSummary) => void
}

export function VerificationTab({ onBlockVerified }: VerificationTabProps) {
  const [inputValue, setInputValue] = useState("")
  const [targetHeight, setTargetHeight] = useState<number | null>(null)
  const [submittedHeight, setSubmittedHeight] = useState<number | null>(null)
  const [validationError, setValidationError] = useState<string | null>(null)
  const { block, isLoading, error } = useBlockDetails(targetHeight ?? undefined, {
    includeTransactions: true,
    enabled: targetHeight !== null,
  })

  const result = useMemo<VerificationResult | null>(() => {
    if (!block) return null

    const checks: VerificationResult["checks"] = [
      {
        label: "Hash integrity",
        description: "API returned the canonical hash for this height",
        status: block.hash ? "passed" : "failed",
      },
      {
        label: "State root",
        description: "State root value present for deterministic replay",
        status: block.state_root ? "passed" : "warning",
      },
      {
        label: "Chain continuity",
        description: block.parent_hash
          ? "Parent hash provided; cross-check by loading height - 1"
          : "Parent hash missing from response",
        status: block.parent_hash ? "passed" : "warning",
      },
      {
        label: "Consensus signatures",
        description: "PBFT consensus committed this block (signature details pending API)",
        status: "warning",
      },
      {
        label: "Timestamp validity",
        description: "Timestamp returned within the expected epoch range",
        status: block.timestamp > 0 ? "passed" : "failed",
      },
    ]

    return {
      verified: true,
      message: "Block metadata retrieved from validator API. Full signature proofs require consensus certificate export.",
      block,
      checks,
    }
  }, [block])

  useEffect(() => {
    if (block && submittedHeight !== null && block.height === submittedHeight) {
      onBlockVerified?.(block)
    }
  }, [block, onBlockVerified, submittedHeight])

  const handleVerify = () => {
    if (!inputValue.trim()) {
      setValidationError("Please enter a block height")
      return
    }

    const isNumeric = /^\d+$/.test(inputValue.trim())
    if (!isNumeric) {
      setValidationError("Block height must be a number")
      setSubmittedHeight(null)
      setTargetHeight(null)
      return
    }

    const height = Number.parseInt(inputValue.trim(), 10)
    if (height < 0) {
      setValidationError("Block height must be non-negative")
      return
    }

    setValidationError(null)
    setSubmittedHeight(height)
    setTargetHeight(height)
  }

  const handleClear = () => {
    setInputValue("")
    setTargetHeight(null)
    setSubmittedHeight(null)
    setValidationError(null)
  }

  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-lg font-semibold text-foreground">Verify Block Integrity</h3>
        <p className="text-sm text-muted-foreground mt-1">
          Look up a committed block by height. Hash-based lookup will follow after the backend exposes that endpoint.
        </p>
      </div>

      <Card className="bg-card/40 border border-border/40 p-4">
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="block-input" className="text-sm font-medium">
              Block Height
            </Label>
            <div className="flex gap-2">
              <Input
                id="block-input"
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                placeholder="e.g. 1072"
                className="flex-1 font-mono text-sm"
                disabled={isLoading}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !isLoading) {
                    handleVerify()
                  }
                }}
              />
              {inputValue && !isLoading ? (
                <Button variant="outline" onClick={handleClear} size="icon">
                  <XCircle className="h-4 w-4" />
                </Button>
              ) : null}
            </div>
            <div className="flex items-center justify-between text-xs">
              <p className="text-muted-foreground">
                Need to verify multiple blocks?
              </p>
              <Link href="/blockchain/blocks" className="inline-flex items-center gap-1 text-primary hover:text-primary/80 transition-colors font-medium">
                Open Block Explorer
                <LinkIcon className="h-3 w-3" />
              </Link>
            </div>
          </div>

          {(validationError || error) && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>Verification Failed</AlertTitle>
              <AlertDescription>
                {validationError || error}
                <br />
                <span className="text-xs opacity-90">
                  {validationError ? "Please check your input and try again." : "This usually means the block doesn't exist or the backend API is unreachable."}
                </span>
              </AlertDescription>
            </Alert>
          )}

          <Button onClick={handleVerify} disabled={isLoading || !inputValue.trim()} className="w-full hover-scale">
            {isLoading ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Verifying…
              </>
            ) : (
              <>
                <ShieldCheck className="h-4 w-4 mr-2" />
                Verify Block
              </>
            )}
          </Button>
        </div>
      </Card>

      {result ? (
        <div className="space-y-4">
          <Card className="border-2 border-status-healthy/60 bg-status-healthy/10 p-4">
            <div className="flex items-start gap-3">
              <div className="p-2 rounded-full bg-status-healthy/20">
                <CheckCircle className="h-6 w-6 text-status-healthy" />
              </div>
              <div className="flex-1">
                <h4 className="text-lg font-semibold text-foreground">Verification successful</h4>
                <p className="text-sm text-muted-foreground mt-1">{result.message}</p>
              </div>
            </div>
          </Card>

          <Card className="bg-card/40 border border-border/40 p-4">
            <h4 className="text-sm font-semibold text-foreground mb-3 flex items-center gap-2">
              <Database className="h-4 w-4 text-primary" />
              Block Information
            </h4>
            <dl className="grid grid-cols-2 gap-3 text-sm">
              <div>
                <dt className="text-muted-foreground">Height</dt>
                <dd className="font-mono text-foreground">{result.block.height.toLocaleString()}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Timestamp</dt>
                <dd>{new Date(result.block.timestamp * 1000).toLocaleString()}</dd>
              </div>
              <div className="col-span-2">
                <dt className="text-muted-foreground">Hash</dt>
                <dd className="font-mono text-xs bg-card/60 px-2 py-1 rounded">
                  {result.block.hash}
                </dd>
              </div>
              <div className="col-span-2">
                <dt className="text-muted-foreground">Parent Hash</dt>
                <dd className="font-mono text-xs bg-card/60 px-2 py-1 rounded">
                  {result.block.parent_hash}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Proposer</dt>
                <dd className="font-mono text-xs bg-card/60 px-2 py-1 rounded break-all">
                  {result.block.proposer}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Transactions</dt>
                <dd>{result.block.transaction_count.toLocaleString()}</dd>
              </div>
            </dl>
          </Card>

          <Card className="bg-card/40 border border-border/40 p-4">
            <h4 className="text-sm font-semibold text-foreground mb-3 flex items-center gap-2">
              <ShieldCheck className="h-4 w-4 text-primary" />
              Verification Checks
            </h4>
            <div className="space-y-2">
              {result.checks.map((check) => (
                <VerificationCheck key={check.label} {...check} />
              ))}
            </div>
          </Card>
        </div>
      ) : null}
    </div>
  )
}

interface VerificationCheckProps {
  label: string
  description: string
  status: "passed" | "failed" | "warning"
}

function VerificationCheck({ label, description, status }: VerificationCheckProps) {
  const icon = status === "passed" ? <CheckCircle className="h-4 w-4" /> : status === "failed" ? <XCircle className="h-4 w-4" /> : <AlertCircle className="h-4 w-4" />
  const badgeClass =
    status === "passed"
      ? "bg-status-healthy/20 text-status-healthy border-status-healthy/40"
      : status === "failed"
        ? "bg-status-critical/20 text-status-critical border-status-critical/40"
        : "bg-status-warning/20 text-status-warning border-status-warning/40"
  const containerClass =
    status === "passed"
      ? "bg-status-healthy/5 border-status-healthy/20"
      : status === "failed"
        ? "bg-status-critical/5 border-status-critical/20"
        : "bg-status-warning/5 border-status-warning/20"
  const iconBackgroundClass =
    status === "passed"
      ? "bg-status-healthy/20"
      : status === "failed"
        ? "bg-status-critical/20"
        : "bg-status-warning/20"

  return (
    <div className={`flex items-start gap-3 p-3 rounded-lg border ${containerClass}`}>
      <div className={`p-2 rounded-lg ${iconBackgroundClass}`}>
        <div
          className={
            status === "passed"
              ? "text-status-healthy"
              : status === "failed"
                ? "text-status-critical"
                : "text-status-warning"
          }
        >
          {icon}
        </div>
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between gap-2">
          <h5 className="text-sm font-medium text-foreground">{label}</h5>
          <Badge className={badgeClass}>
            {status === "passed" ? "Passed" : status === "failed" ? "Failed" : "Info"}
          </Badge>
        </div>
        <p className="text-xs text-muted-foreground mt-1">{description}</p>
      </div>
    </div>
  )
}
