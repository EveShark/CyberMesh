import type React from "react"
import type { Metadata } from "next"
import { GeistSans } from "geist/font/sans"
import { GeistMono } from "geist/font/mono"
import { Analytics } from "@vercel/analytics/next"
import "./globals.css"
import { SidebarProvider } from "@/components/ui/sidebar"
import { SidebarNav } from "@/components/dashboard/sidebar-nav"
import { Suspense } from "react"
import { ThemeProvider } from "@/components/theme-provider"

export const metadata: Metadata = {
  title: "v0 App",
  description: "Created with v0",
  generator: "v0.app",
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable} antialiased`}>
      <body className="bg-background text-foreground">
        <ThemeProvider attribute="class" defaultTheme="dark" enableSystem={false}>
          <SidebarProvider>
            <Suspense fallback={<div>Loading...</div>}>
              <div className="flex min-h-screen">
                <SidebarNav />
                <main className="flex-1 overflow-auto">{children}</main>
              </div>
            </Suspense>
          </SidebarProvider>
          <Analytics />
        </ThemeProvider>
      </body>
    </html>
  )
}
