import type { LucideIcon } from "lucide-react"
import {
  Activity,
  AlertTriangle,
  BarChart3,
  Database,
  Network,
  Zap,
} from "lucide-react"

import type { DashboardMode } from "@/lib/mode-context"

export type NavigationGroup = "main" | "ai" | "system" | "security" | "admin"

export interface NavigationConfigItem {
  id: string
  title: string
  url: string
  icon: LucideIcon
  group: NavigationGroup
  modes: DashboardMode[]
}

export const NAVIGATION_GROUP_LABELS: Record<NavigationGroup, string> = {
  main: "Overview",
  ai: "AI Systems",
  system: "Operations",
  security: "Security",
  admin: "Administration",
}

export const NAVIGATION_GROUP_SEQUENCE: NavigationGroup[] = [
  "main",
  "ai",
  "system",
  "security",
  "admin",
]

export const NAVIGATION_ITEMS: NavigationConfigItem[] = [
  {
    id: "dashboard",
    title: "Dashboard",
    url: "/",
    icon: BarChart3,
    group: "main",
    modes: ["investor", "demo", "operator"],
  },
  {
    id: "ai-engine",
    title: "AI Engine",
    url: "/ai-engine",
    icon: Zap,
    group: "ai",
    modes: ["investor", "demo", "operator"],
  },
  {
    id: "network",
    title: "Network",
    url: "/network",
    icon: Network,
    group: "system",
    modes: ["investor", "demo", "operator"],
  },
  {
    id: "system-health",
    title: "System Health",
    url: "/system-health",
    icon: Activity,
    group: "system",
    modes: ["investor", "demo", "operator"],
  },
  {
    id: "threats",
    title: "Threats",
    url: "/threats",
    icon: AlertTriangle,
    group: "security",
    modes: ["demo", "operator"],
  },
  {
    id: "blockchain",
    title: "Blockchain Activity",
    url: "/blockchain",
    icon: Database,
    group: "security",
    modes: ["demo", "operator"],
  },
]

export function getNavigationItemsForMode(mode: DashboardMode): NavigationConfigItem[] {
  return NAVIGATION_ITEMS.filter((item) => item.modes.includes(mode))
}
