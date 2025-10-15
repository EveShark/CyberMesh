import { RecentBlocksTable } from "@/components/ledger/recent-blocks-table"
import { BlockVerificationForm } from "@/components/ledger/block-verification-form"
import { DecisionTimelineChart } from "@/components/ledger/decision-timeline-chart"

export default function LedgerPage() {
  return (
    <div className="min-h-screen p-6 space-y-6">
      <div className="space-y-2">
        <h1 className="text-3xl font-bold text-foreground">Blockchain Ledger</h1>
        <p className="text-muted-foreground">
          Monitor blockchain activity, verify blocks, and track consensus decisions
        </p>
      </div>

      <div className="grid gap-6">
        {/* Recent Blocks Table */}
        <RecentBlocksTable />

        {/* Block Verification and Decision Timeline */}
        <div className="grid lg:grid-cols-2 gap-6">
          <BlockVerificationForm />
          <DecisionTimelineChart />
        </div>
      </div>
    </div>
  )
}
