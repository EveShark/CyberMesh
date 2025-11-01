"use client"

import { useEffect } from "react"

type GlobalErrorProps = {
  error: Error & { digest?: string }
  reset: () => void
}

export default function GlobalError({ error, reset }: GlobalErrorProps) {
  useEffect(() => {
    console.error("Global error boundary caught an error:", error)
  }, [error])

  return (
    <html lang="en">
      <body className="bg-background text-foreground">
        <div className="flex min-h-screen flex-col items-center justify-center gap-6 px-6 py-16 text-center">
          <div>
            <p className="text-sm uppercase tracking-wide text-muted-foreground">Critical failure</p>
            <h1 className="mt-2 text-3xl font-bold sm:text-4xl">CyberMesh is unavailable</h1>
            <p className="mt-4 max-w-md text-sm text-muted-foreground">
              An unrecoverable error occurred during rendering. Please try reloading the application. If the issue
              persists, contact the platform administrator.
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
              Reload application
            </button>
          </div>
        </div>
      </body>
    </html>
  )
}
