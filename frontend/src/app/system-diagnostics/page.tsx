import type { Metadata } from "next"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "System Diagnostics - CyberMesh",
  description: "Legacy diagnostics dashboard has been archived.",
}

export default function SystemDiagnosticsPage() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center px-6 py-12 text-center">
      <div className="space-y-4">
        <h1 className="text-2xl font-semibold text-foreground">System diagnostics archived</h1>
        <p className="text-sm text-muted-foreground max-w-lg">
          The earlier system diagnostics UI now lives in <code>Archive/legacy-system-diagnostics</code>. Refer to that
          folder for historical code paths.
        </p>
      </div>
    </div>
  )
}
