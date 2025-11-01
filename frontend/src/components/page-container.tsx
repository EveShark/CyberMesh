"use client"

import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

interface PageContainerProps {
  children: ReactNode
  className?: string
  align?: "left" | "center"
  variant?: "default" | "compact" | "fullBleed"
}

export function PageContainer({ children, className, align = "left", variant = "default" }: PageContainerProps) {
  const getPadding = () => {
    if (variant === "fullBleed") {
      // Full-bleed: mobile/tablet get padding, but desktop (xl+) goes edge-to-edge
      return align === "center"
        ? "px-4 sm:px-6 lg:px-8 xl:px-0 2xl:px-0"
        : "px-4 sm:px-6 lg:px-8 xl:px-0 2xl:px-0"
    }
    if (variant === "compact") {
      return align === "center"
        ? "px-4 sm:px-6 lg:px-8 xl:px-4 2xl:px-6"
        : "px-4 sm:px-6 lg:px-8 xl:px-4 2xl:px-6"
    }
    return align === "center"
      ? "px-4 sm:px-6 lg:px-8 xl:px-12 2xl:px-16"
      : "px-4 sm:px-6 lg:px-8 xl:px-10 2xl:px-12"
  }
  
  const base =
    align === "center"
      ? `w-full max-w-[1920px] mx-auto ${getPadding()}`
      : `w-full ${getPadding()}` // flush-left: fill width, no max-w, no mx-auto
  return (
    <div
      className={cn(base, className)}
    >
      {children}
    </div>
  )
}
