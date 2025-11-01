"use client"

import type { ReactNode } from "react"
import { SidebarNav } from "@/components/sidebar-nav"

export interface AppShellProps {
  children: ReactNode
}

export default function AppShell({ children }: AppShellProps) {
  return (
    <div className="flex min-h-screen" data-app-shell>
      <SidebarNav />
      <main className="flex-1 overflow-auto">{children}</main>
    </div>
  )
}

export { AppShell as ClientAppShell }
