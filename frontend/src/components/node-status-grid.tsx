"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

interface NodeStatusGridProps {
  validators?: Array<{
    id: string
    status: string
    public_key: string
    uptime_percentage?: number
    proposed_blocks?: number
    joined_at_height?: number
  }>
  isLoading?: boolean
  error?: Error | null
}

function formatValidatorStatus(status: string) {
  switch (status) {
    case "active":
      return { label: "Active", variant: "default" as const, className: "bg-status-healthy text-white" }
    case "inactive":
      return { label: "Inactive", variant: "secondary" as const, className: "" }
    default:
      return { label: status ?? "Unknown", variant: "outline" as const, className: "" }
  }
}

export function NodeStatusGrid({ validators, isLoading, error }: NodeStatusGridProps) {
  if (isLoading) {
    return <div className="text-center py-8">Loading validator status…</div>
  }

  if (error) {
    return <div className="text-center py-8 text-destructive">Failed to load validator status</div>
  }

  const items = validators ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Validator Status</h2>
        <Badge variant="outline" className="text-sm">
          {items.length} validators
        </Badge>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        {items.map((validator) => {
          const statusInfo = formatValidatorStatus(validator.status)
          const shortId = `${validator.id.slice(0, 6)}…${validator.id.slice(-4)}`
          const uptime =
            validator.uptime_percentage !== undefined ? `${validator.uptime_percentage.toFixed(1)}%` : "--"
          const proposedBlocks = validator.proposed_blocks !== undefined ? validator.proposed_blocks : null

          return (
            <Card key={validator.id} className="glass-card hover:shadow-lg transition-all duration-200">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm font-medium">Validator {shortId}</CardTitle>
                  <Badge
                    variant={statusInfo.variant}
                    className={cn("text-xs capitalize", statusInfo.className)}
                  >
                    {statusInfo.label}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-3 text-sm">
                <div>
                  <p className="text-muted-foreground">Public Key</p>
                  <p className="font-mono text-xs break-all">{validator.public_key}</p>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Uptime</span>
                  <span className="font-medium">{uptime}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Proposed Blocks</span>
                  <span className="font-medium">{proposedBlocks !== null ? proposedBlocks : "--"}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Joined Height</span>
                  <span className="font-medium">{validator.joined_at_height ?? "--"}</span>
                </div>
              </CardContent>
            </Card>
          )
        })}
      </div>
    </div>
  )
}
