import type { Metadata } from "next"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "Metrics - CyberMesh",
  description: "Legacy metrics dashboard has been archived.",
}

export default function MetricsPage() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center px-6 py-12 text-center">
      <div className="space-y-4">
        <h1 className="text-2xl font-semibold text-foreground">Legacy metrics dashboard archived</h1>
        <p className="text-sm text-muted-foreground max-w-lg">
          The previous metrics experience lives under <code>Archive/legacy-metrics</code>. Refer to that directory for
          historical code paths while the new backend-driven dashboards are finalized.
        </p>
      </div>
    </div>
  )
}
