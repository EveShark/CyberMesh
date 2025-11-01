import type { Metadata } from "next"

import { BlocksExplorerContent } from "@/pages-content/blockchain-blocks"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "Block Explorer | CyberMesh",
  description: "Detailed block listings with filters, pagination, and verification shortcuts.",
}

export default function BlocksExplorerPage() {
  return <BlocksExplorerContent />
}
