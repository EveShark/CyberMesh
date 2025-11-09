"use client"

import { useMemo, useState } from "react"
import { ChevronLeft, ChevronRight, HelpCircle, Settings as SettingsIcon, Shield } from "lucide-react"
import { usePathname } from "next/navigation"
import Link from "next/link"
import { useMode } from "@/lib/mode-context"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { NAVIGATION_GROUP_LABELS, NAVIGATION_GROUP_SEQUENCE, NAVIGATION_ITEMS } from "@/config/navigation"

export function SidebarNav() {
  const pathname = usePathname()
  const { mode } = useMode()
  const useP2PStyle = process.env.NEXT_PUBLIC_P2P_STYLE !== "false"

  const filteredItems = useMemo(
    () => NAVIGATION_ITEMS.filter((item) => item.modes.includes(mode)),
    [mode]
  )
  const [isCollapsed, setIsCollapsed] = useState(false)

  const groupedSections = useMemo(
    () =>
      NAVIGATION_GROUP_SEQUENCE.map((group) => ({
          group,
          label: NAVIGATION_GROUP_LABELS[group],
          items: filteredItems.filter((item) => item.group === group),
        }))
        .filter((section) => section.items.length > 0),
    [filteredItems]
  )

  if (!useP2PStyle) {
    return (
      <div
        className="flex h-screen w-64 flex-col border-r border-normal bg-card/95 backdrop-blur-md"
        data-testid="sidebar-root"
      >
        <div className="flex items-center gap-3 border-b border-subtle px-4 py-4">
          <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg border border-normal bg-gradient-to-br from-primary/20 to-accent/20">
            <Shield className="h-4 w-4 text-primary" />
          </div>
          <div>
            <h2 className="text-sm font-semibold text-foreground whitespace-nowrap">CyberMesh</h2>
            <p className="text-xs text-muted-foreground whitespace-nowrap">
              {mode === "investor" ? "Investor" : mode === "demo" ? "Demo" : "Operator"}
            </p>
          </div>
        </div>
        <nav className="flex-1 space-y-1 overflow-y-auto py-4">
          {filteredItems.map((item) => {
            const isActive = pathname === item.url
            const Icon = item.icon

            return (
              <Link
                key={item.id}
                href={item.url}
                className={cn(
                  "mx-2 flex items-center gap-3 rounded-lg px-4 py-3 transition-all duration-200",
                  "hover:bg-accent/10",
                  isActive
                    ? "border-l-2 border-l-primary bg-gradient-to-r from-primary/10 to-accent/10 text-foreground"
                    : "text-muted-foreground"
                )}
                data-testid={`nav-item-${item.id}`}
              >
                <Icon className="h-5 w-5 flex-shrink-0" />
                <span className="text-sm font-medium whitespace-nowrap">{item.title}</span>
              </Link>
            )
          })}
        </nav>
        <div className="border-t border-subtle p-4 space-y-2">
          {/* Mode and theme controls removed */}
        </div>
      </div>
    )
  }

  return (
    <TooltipProvider delayDuration={0}>
      <div
        className={cn(
          "relative hidden h-screen flex-col border-r border-sidebar-border bg-sidebar/90 shadow-glow backdrop-blur transition-all duration-300 md:flex",
          isCollapsed ? "w-20" : "w-64"
        )}
        data-testid="sidebar-root"
      >
        <div className="flex h-16 items-center justify-between border-b border-sidebar-border px-4">
          {!isCollapsed && (
            <div className="flex items-center gap-3">
              <div className="h-8 w-2 rounded-full bg-gradient-to-b from-sidebar-primary to-sidebar-accent" />
              <div className="flex flex-col">
                <span className="text-xs font-medium uppercase tracking-widest text-sidebar-foreground/60">CyberMesh</span>
                <span className="text-sm font-semibold text-sidebar-foreground">
                  {mode === "investor" ? "Investor" : mode === "demo" ? "Demo" : "Operator"} mode
                </span>
              </div>
            </div>
          )}
          <Button
            variant="ghost"
            size="icon"
            className="text-sidebar-foreground hover:bg-sidebar-accent/20"
            onClick={() => setIsCollapsed((previous) => !previous)}
            aria-label={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
            data-testid="sidebar-collapse-toggle"
          >
            {isCollapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
          </Button>
        </div>

        <nav className="flex-1 space-y-5 overflow-y-auto px-3 py-4">
          {groupedSections.map((section) => (
            <div key={section.group} className="space-y-2" data-testid={`nav-group-${section.group}`}>
              {!isCollapsed && (
                <div className="flex items-center justify-between px-2 text-xs font-semibold uppercase tracking-wider text-sidebar-foreground/60">
                  <span>{section.label}</span>
                  <span className="text-2xs text-sidebar-foreground/40">{section.items.length}</span>
                </div>
              )}
              <div className="space-y-1">
                {section.items.map((item) => {
                  const isActive = pathname === item.url
                  const Icon = item.icon

                  const linkNode = (
                    <Link
                      key={item.id}
                      href={item.url}
                      className={cn(
                        "relative flex items-center rounded-xl border transition-all duration-200",
                        isCollapsed ? "justify-center px-0 py-3" : "gap-3 px-4 py-3",
                        isActive
                          ? "border-sidebar-primary/40 bg-gradient-to-r from-sidebar-primary/25 via-sidebar-primary/10 to-sidebar-accent/10 text-sidebar-primary shadow-glow"
                          : "border-transparent text-sidebar-foreground/70 hover:border-sidebar-accent/30 hover:bg-sidebar-accent/10 hover:text-sidebar-foreground"
                      )}
                      data-testid={`nav-item-${item.id}`}
                    >
                      <Icon className="h-5 w-5 flex-shrink-0" />
                      {!isCollapsed && <span className="text-sm font-medium">{item.title}</span>}
                      {isActive && !isCollapsed && (
                        <span
                          className="absolute inset-y-2 left-1 w-1 rounded-full bg-gradient-to-b from-sidebar-primary/80 via-sidebar-primary/50 to-sidebar-accent/60"
                          data-testid={`nav-active-indicator-${item.id}`}
                        />
                      )}
                    </Link>
                  )

                  if (isCollapsed) {
                    return (
                      <Tooltip key={item.id} delayDuration={100}>
                        <TooltipTrigger asChild>{linkNode}</TooltipTrigger>
                        <TooltipContent
                          side="right"
                          className="border border-sidebar-border bg-popover text-popover-foreground"
                        >
                          {item.title}
                        </TooltipContent>
                      </Tooltip>
                    )
                  }

                  return linkNode
                })}
              </div>
            </div>
          ))}
        </nav>

        <div className="border-t border-sidebar-border p-4">
          {!isCollapsed ? (
            <div className="space-y-2">
              <div className="flex items-center gap-3 pt-2 text-xs text-sidebar-foreground/60">
                <SettingsIcon className="h-4 w-4" />
                <span>Collapse to focus on visual data.</span>
              </div>
            </div>
          ) : (
            <div className="flex flex-col items-center gap-3 text-sidebar-foreground/70">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="text-sidebar-foreground hover:bg-sidebar-accent/25"
                    onClick={() => setIsCollapsed(false)}
                    aria-label="Expand sidebar for controls"
                      data-testid="sidebar-expand-settings"
                  >
                    <SettingsIcon className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent
                  side="right"
                  className="border border-sidebar-border bg-popover text-popover-foreground"
                >
                  Expand sidebar navigation
                </TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="flex items-center justify-center rounded-full border border-sidebar-border px-2 py-1 text-2xs uppercase tracking-widest">
                    <HelpCircle className="mr-1 h-3.5 w-3.5" />
                    HUD
                  </div>
                </TooltipTrigger>
                <TooltipContent
                  side="right"
                  className="border border-sidebar-border bg-popover text-popover-foreground"
                >
                  Compact nav active
                </TooltipContent>
              </Tooltip>
            </div>
          )}
        </div>
      </div>
    </TooltipProvider>
  )
}
