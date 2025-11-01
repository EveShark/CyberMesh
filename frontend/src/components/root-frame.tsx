export function RootFrame() {
  return (
    <div
      id="root-frame-placeholder"
      className="flex min-h-screen"
      suppressHydrationWarning
    >
      <main className="flex-1 overflow-auto" suppressHydrationWarning />
    </div>
  )
}
