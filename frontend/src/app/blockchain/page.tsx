import type { Metadata } from "next"

import BlockchainActivityContent from "@/pages-content/blockchain"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "Blockchain Activity | CyberMesh",
  description: "Live block production, anomaly signals, and proposer performance across the mesh.",
}

export default function BlockchainPage() {
  return <BlockchainActivityContent />
}
