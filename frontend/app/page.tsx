import type { Metadata } from "next"
import OverviewPage from "./overview"

export const metadata: Metadata = {
  title: "Overview - Security Dashboard",
  description: "System status and security monitoring overview",
}

export default function HomePage() {
  return <OverviewPage />
}
