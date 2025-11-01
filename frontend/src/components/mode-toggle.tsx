"use client"

import { useMode } from "@/lib/mode-context"
import { Button } from "@/components/ui/button"
import { Zap, Eye, Settings, ChevronDown } from "lucide-react"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export function ModeToggle() {
  const { mode, setMode } = useMode()

  const modes = [
    { id: "investor", label: "Investor Mode", icon: Zap, description: "Curated investor view" },
    { id: "demo", label: "Demo Mode", icon: Eye, description: "Full feature showcase" },
    { id: "operator", label: "Operator Mode", icon: Settings, description: "All technical pages" },
  ] as const

  const currentMode = modes.find((m) => m.id === mode)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="w-full justify-between border-primary/30 hover:bg-primary/10 bg-primary/5 text-foreground"
          data-testid="mode-toggle-trigger"
        >
          <div className="flex items-center gap-2">
            {currentMode?.icon && <currentMode.icon className="h-4 w-4" />}
            <span className="text-xs font-medium">{currentMode?.label}</span>
          </div>
          <ChevronDown className="h-3 w-3 opacity-50" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56 bg-card/95 backdrop-blur-md border-primary/20 shadow-2xl z-50">
        <DropdownMenuLabel className="text-xs font-semibold uppercase tracking-wider text-foreground">
          Dashboard Mode
        </DropdownMenuLabel>
        <DropdownMenuSeparator className="bg-border/30" />
        {modes.map((m) => (
          <DropdownMenuItem
            key={m.id}
            onClick={() => setMode(m.id)}
            className={`cursor-pointer gap-3 transition-all duration-150 ${
              mode === m.id ? "bg-primary/20 text-foreground border-l-2 border-primary" : "hover:bg-accent/10"
            }`}
            data-testid={`mode-option-${m.id}`}
          >
            <m.icon className="h-4 w-4" />
            <div className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">{m.label}</span>
              <span className="text-xs text-muted-foreground">{m.description}</span>
            </div>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
