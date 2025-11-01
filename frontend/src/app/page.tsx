import type { Metadata } from "next"
import OverviewPage from "@/pages-content/overview"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "Overview - Security Dashboard",
  description: "System status and security monitoring overview",
}

export default function HomePage() {
  return <OverviewPage />
}
