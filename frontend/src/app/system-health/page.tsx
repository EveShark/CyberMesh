import dynamicImport from "next/dynamic"
import type { Metadata } from "next"

const SystemHealthPageClient = dynamicImport(() => import("./system-health-page-client"), { ssr: false })

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "System Health - CyberMesh",
  description: "Operational telemetry for consensus, storage, Kafka, and AI service",
}

export default function SystemHealthPage() {
  return <SystemHealthPageClient />
}
