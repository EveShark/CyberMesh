export const dynamic = "force-dynamic"

export default function NotFound() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 bg-background px-6 py-16 text-center text-foreground">
      <div>
        <p className="text-sm uppercase tracking-wide text-muted-foreground">404</p>
        <h1 className="mt-2 text-3xl font-bold sm:text-4xl">Page not found</h1>
        <p className="mt-4 max-w-md text-sm text-muted-foreground">
          The page you are looking for couldn&apos;t be located. It may have been moved or removed.
        </p>
      </div>
      <div className="flex flex-col gap-3 sm:flex-row">
        <a
          href="/"
          className="inline-flex items-center justify-center rounded-md border border-primary bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition hover:bg-primary/90"
        >
          Go to dashboard
        </a>
        <a
          href="/network"
          className="inline-flex items-center justify-center rounded-md border border-border px-4 py-2 text-sm font-medium text-foreground transition hover:bg-accent/20"
        >
          View network status
        </a>
      </div>
    </div>
  )
}
