import { NextResponse } from "next/server"

// Mock data generator for network nodes
function generateNodeHealth() {
  const nodeNames = ["Node-A", "Node-B", "Node-C", "Node-D", "Node-E"]
  const statuses = ["healthy", "warning", "critical"] as const

  return {
    connectedPeers: Math.floor(Math.random() * 3) + 4,
    avgLatency: Math.floor(Math.random() * 50) + 20,
    consensusRound: Math.floor(Math.random() * 1000) + 500,
    leaderStability: Math.floor(Math.random() * 20) + 80,
    nodes: nodeNames.map((name, i) => ({
      id: `node-${i}`,
      name,
      status: statuses[Math.floor(Math.random() * statuses.length)],
      latency: Math.floor(Math.random() * 100) + 10,
      uptime: Math.floor(Math.random() * 30) + 70,
    })),
    pbftPhase: ["pre-prepare", "prepare", "commit"][Math.floor(Math.random() * 3)],
    leader: `Node-${String.fromCharCode(65 + Math.floor(Math.random() * 5))}`,
    votingStatus: {
      "node-0": Math.random() > 0.2,
      "node-1": Math.random() > 0.2,
      "node-2": Math.random() > 0.2,
      "node-3": Math.random() > 0.2,
      "node-4": Math.random() > 0.2,
    },
  }
}

export async function GET() {
  try {
    const data = generateNodeHealth()
    return NextResponse.json(data)
  } catch (error) {
    console.error("Error fetching node health:", error)
    return NextResponse.json({ error: "Failed to fetch node health data" }, { status: 500 })
  }
}
