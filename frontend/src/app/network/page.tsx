import type { Metadata } from "next"
import dynamicImport from "next/dynamic"

const NetworkPageClient = dynamicImport(() => import("./network-page-client"), {
  ssr: false,
})

export const metadata: Metadata = {
  title: "Network & Consensus",
  description: "Live peer telemetry and PBFT coordination status",
}

export const dynamic = "force-dynamic"

export default function NetworkPage() {
  return <NetworkPageClient />
}
