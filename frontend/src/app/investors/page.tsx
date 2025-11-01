import type { Metadata } from "next"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "Investors - CyberMesh",
  description: "Legacy investor landing content has been archived.",
}

export default function InvestorsPage() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center px-6 py-12 text-center">
      <div className="space-y-4">
        <h1 className="text-2xl font-semibold text-foreground">Investor microsite archived</h1>
        <p className="text-sm text-muted-foreground max-w-lg">
          The previous investor-focused microsite has been moved to <code>Archive/legacy-landing</code>. Please refer to
          that directory if you need the historical content.
        </p>
      </div>
    </div>
  )
}
