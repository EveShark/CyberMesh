"use client"

import { Shield, AlertTriangle, Network, BarChart3, Database, Activity } from "lucide-react"
import { usePathname } from "next/navigation"
import Link from "next/link"
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
    title: "Overview",
    url: "/",
    icon: BarChart3,
    description: "System status and metrics",
  },
  {
    title: "Threats",
    url: "/threats",
    icon: AlertTriangle,
    description: "Security monitoring",
  },
  {
    title: "Consensus",
    url: "/consensus",
    icon: Network,
    description: "PBFT status and nodes",
  },
  {
    title: "Ledger",
    url: "/ledger",
    icon: Database,
    description: "Blockchain verification",
  },
  {
    title: "Metrics",
    url: "/metrics",
    icon: Activity,
    description: "Performance monitoring",
  },
]

export function SidebarNav() {
  const pathname = usePathname()

  return (
    <Sidebar className="glass-card border-r border-border/30">
      <SidebarHeader className="border-b border-border/20 p-6">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-primary/20 to-accent/20 border border-border/30">
            <Shield className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-foreground">Security Ops</h2>
            <p className="text-xs text-muted-foreground">Operator Dashboard</p>
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
              {navigationItems.map((item) => (
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
        <div className="text-xs text-muted-foreground text-center">v2.1.0 • Secure Mode</div>
      </SidebarFooter>
    </Sidebar>
  )
}
