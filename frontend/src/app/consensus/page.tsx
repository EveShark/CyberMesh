import type { Metadata } from "next"
import ConsensusComponent from "@/pages-content/consensus"

export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "Consensus - Security Dashboard",
  description: "PBFT consensus status and node monitoring",
}

export default function Page() {
  return <ConsensusComponent />
}
