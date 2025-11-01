"use client"

import { useLayoutEffect } from "react"
import type React from "react"

import AppShell from "@/components/app-shell"
import { ThemeProvider } from "@/components/theme-provider"
import { SidebarProvider } from "@/components/ui/sidebar"
import { ModeProvider } from "@/lib/mode-context"

type ClientProvidersProps = {
  children: React.ReactNode
}

export default function ClientProviders({ children }: ClientProvidersProps) {
  useLayoutEffect(() => {
    const placeholder = document.getElementById("root-frame-placeholder")
    if (placeholder?.parentElement) {
      placeholder.parentElement.removeChild(placeholder)
    }
  }, [])

  // Allow theme to be configured via environment variable
  // Options: "light", "dark", or "system" (follows OS preference)
  const defaultTheme = (process.env.NEXT_PUBLIC_DEFAULT_THEME as "light" | "dark" | "system") || "dark"

  return (
    <ThemeProvider attribute="class" defaultTheme={defaultTheme} enableSystem>
      <ModeProvider>
        <SidebarProvider>
          <AppShell>{children}</AppShell>
        </SidebarProvider>
      </ModeProvider>
    </ThemeProvider>
  )
}
