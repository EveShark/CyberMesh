import type { Metadata } from "next"

import ThreatsContent from "@/pages-content/threats"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "Threat Intelligence | CyberMesh",
  description: "Live threat detections, severity trends, and validator telemetry across the cluster.",
}

export default function ThreatsPage() {
  return <ThreatsContent />
}
