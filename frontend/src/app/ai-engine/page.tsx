import type { Metadata } from "next"

import AiEnginePageContent from "@/pages-content/ai-engine"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "AI Engine - CyberMesh",
  description: "Real-time detection metrics, variant performance, and AI suspicious node insights.",
}

export default function AIEnginePage() {
  return <AiEnginePageContent />
}
