"use client"

import { useTheme } from "next-themes"
import { Moon, Sun } from "lucide-react"
import { Button } from "@/components/ui/button"

export function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme()

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
      className="w-full justify-start gap-2 hover:bg-accent/10"
      suppressHydrationWarning
    >
      <span className="flex items-center gap-2" suppressHydrationWarning>
        {resolvedTheme === "dark" ? (
          <>
            <Moon className="h-4 w-4" />
            <span className="text-xs font-medium">Dark Mode</span>
          </>
        ) : (
          <>
            <Sun className="h-4 w-4" />
            <span className="text-xs font-medium">Light Mode</span>
          </>
        )}
      </span>
    </Button>
  )
}
