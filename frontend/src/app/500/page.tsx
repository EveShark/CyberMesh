import Link from "next/link"

export default function InternalServerErrorPage() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 bg-background px-6 py-16 text-center text-foreground">
      <div>
        <p className="text-sm uppercase tracking-[0.3em] text-muted-foreground">500</p>
        <h1 className="mt-3 text-3xl font-bold sm:text-4xl">We hit a snag</h1>
        <p className="mt-4 max-w-md text-sm text-muted-foreground">
          An unexpected error occurred while processing your request. Please try again in a moment or reach out to the team if the issue persists.
        </p>
      </div>
      <Link
        href="/"
        className="inline-flex items-center justify-center rounded-md border border-primary bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition hover:bg-primary/90"
      >
        Return to dashboard
      </Link>
    </div>
  )
}

