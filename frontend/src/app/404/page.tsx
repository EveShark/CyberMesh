export const dynamic = "force-dynamic"

export default function NotFoundPage() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 bg-background px-6 py-16 text-center text-foreground">
      <div>
        <p className="text-sm uppercase tracking-wide text-muted-foreground">404</p>
        <h1 className="mt-2 text-3xl font-bold sm:text-4xl">Page not found</h1>
        <p className="mt-4 max-w-md text-sm text-muted-foreground">
          The page you were looking for was not found. Please return to the dashboard.
        </p>
      </div>
      <a
        href="/"
        className="inline-flex items-center justify-center rounded-md border border-primary bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition hover:bg-primary/90"
      >
        Go to dashboard
      </a>
    </div>
  )
}
