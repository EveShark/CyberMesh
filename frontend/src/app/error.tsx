"use client"

import { useEffect } from "react"

type ErrorProps = {
  error: Error & { digest?: string }
  reset: () => void
}

export default function Error({ error, reset }: ErrorProps) {
  useEffect(() => {
    console.error("Application error:", error)
  }, [error])

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 bg-background px-6 py-16 text-center text-foreground">
      <div>
        <p className="text-sm uppercase tracking-wide text-muted-foreground">Unexpected error</p>
        <h1 className="mt-2 text-3xl font-bold sm:text-4xl">Something went wrong</h1>
        <p className="mt-4 max-w-md text-sm text-muted-foreground">
          We couldn&apos;t complete your request. You can try again or return to the dashboard while we investigate.
        </p>
        {error?.digest ? (
          <p className="mt-3 text-xs text-muted-foreground">Reference: {error.digest}</p>
        ) : null}
      </div>
      <div className="flex flex-col gap-3 sm:flex-row">
        <button
          type="button"
          onClick={reset}
          className="inline-flex items-center justify-center rounded-md border border-primary bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition hover:bg-primary/90"
        >
          Try again
        </button>
        <a
          href="/"
          className="inline-flex items-center justify-center rounded-md border border-border px-4 py-2 text-sm font-medium text-foreground transition hover:bg-accent/20"
        >
          Back to dashboard
        </a>
      </div>
    </div>
  )
}
