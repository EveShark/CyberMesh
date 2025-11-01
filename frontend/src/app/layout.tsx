import type React from "react"
import type { Metadata } from "next"
import { Inter, JetBrains_Mono } from "next/font/google"
import "./globals.css"
import { Suspense } from "react"

import { Providers } from "./providers"

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
})

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
  display: "swap",
})

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "CyberMesh - Distributed Threat Validation Platform",
  description: "Byzantine Fault Tolerant consensus for validated security intelligence across distributed nodes",
  generator: "CyberMesh",
}

const isP2PStyleEnabled = process.env.NEXT_PUBLIC_P2P_STYLE !== "false"
const dataStyle = isP2PStyleEnabled ? "p2p" : "classic"

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en" className={`${inter.variable} ${jetbrainsMono.variable} antialiased`} data-style={dataStyle}>
      <body className="bg-background text-foreground font-sans">
        <Providers>
          <Suspense fallback={<div>Loading...</div>}>{children}</Suspense>
        </Providers>
      </body>
    </html>
  )
}
