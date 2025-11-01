"use client"

import { Shield, AlertTriangle, Network, BarChart3, Database, Activity, Server, Zap } from "lucide-react"
import { usePathname } from "next/navigation"
import Link from "next/link"
import { useMode } from "@/lib/mode-context"
import { ModeToggle } from "./mode-toggle"
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarHeader,
  SidebarFooter,
} from "@/components/ui/sidebar"
import { cn } from "@/lib/utils"

const navigationItems = [
  {
    title: "Dashboard",
    url: "/",
    icon: BarChart3,
    description: "System overview",
    modes: ["investor", "demo", "operator"],
  },
  {
    title: "AI Engine",
    url: "/ai-engine",
    icon: Zap,
    description: "Detection performance",
    modes: ["investor", "demo", "operator"],
  },
  {
    title: "Network & Consensus",
    url: "/network",
    icon: Network,
    description: "P2P mesh & PBFT",
    modes: ["investor", "demo", "operator"],
  },
  {
    title: "System Health",
    url: "/system-health",
    icon: Activity,
    description: "Infrastructure status",
    modes: ["investor", "demo", "operator"],
  },
  {
    title: "Threats",
    url: "/threats",
    icon: AlertTriangle,
    description: "Security monitoring",
    modes: ["demo", "operator"],
  },
  {
    title: "Ledger",
    url: "/ledger",
    icon: Server,
    description: "Blockchain verification",
    modes: ["demo", "operator"],
  },
  {
    title: "Blocks",
    url: "/blocks",
    icon: Database,
    description: "Kafka anomalies",
    modes: ["demo", "operator"],
  },
  {
    title: "Metrics",
    url: "/metrics",
    icon: Activity,
    description: "Performance metrics",
    modes: ["demo", "operator"],
  },
]

export function SidebarNav() {
  const pathname = usePathname()
  const { mode } = useMode()

  const filteredItems = navigationItems.filter((item) => item.modes.includes(mode))

  return (
    <Sidebar className="glass-card border-r border-border/30">
      <SidebarHeader className="border-b border-border/20 p-6">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-primary/20 to-accent/20 border border-border/30">
              <Shield className="h-5 w-5 text-primary" />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-foreground">CyberMesh</h2>
              <p className="text-xs text-muted-foreground">
                {mode === "investor" ? "Investor View" : mode === "demo" ? "Demo View" : "Operator View"}
              </p>
            </div>
          </div>
          <div className="ml-auto">
            <ModeToggle />
          </div>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel className="text-muted-foreground text-xs font-medium uppercase tracking-wider px-3 py-2">
            Navigation
          </SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {filteredItems.map((item) => (
                <SidebarMenuItem key={item.title}>
                  <SidebarMenuButton
                    asChild
                    className={cn(
                      "w-full justify-start gap-3 px-3 py-2.5 text-sm font-medium transition-all duration-200",
                      "hover:bg-accent/10 hover:text-foreground",
                      pathname === item.url
                        ? "bg-gradient-to-r from-primary/10 to-accent/10 text-foreground border-r-2 border-primary"
                        : "text-muted-foreground",
                    )}
                  >
                    <Link href={item.url}>
                      <item.icon className="h-4 w-4" />
                      <div className="flex flex-col items-start">
                        <span>{item.title}</span>
                        <span className="text-xs opacity-70">{item.description}</span>
                      </div>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="border-t border-border/20 p-4">
        <div className="text-xs text-muted-foreground text-center">
          v2.1.0 • {mode.charAt(0).toUpperCase() + mode.slice(1)} Mode
        </div>
      </SidebarFooter>
    </Sidebar>
  )
}
