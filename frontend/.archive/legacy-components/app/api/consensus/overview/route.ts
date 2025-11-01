import { NextResponse } from "next/server"

// Mock data generator for consensus overview
function generateConsensusData() {
  const now = Date.now()

  return {
    leader: `Node-${String.fromCharCode(65 + Math.floor(Math.random() * 5))}`,
    term: Math.floor(Math.random() * 100) + 1,
    phase: ["pre-prepare", "prepare", "commit"][Math.floor(Math.random() * 3)],
    activePeers: Math.floor(Math.random() * 3) + 4,
    quorumSize: 4,
    proposals: Array.from({ length: 8 }, (_, i) => ({
      block: 1000 - i,
      timestamp: now - i * 5000,
    })),
    votes: [
      { type: "pre-prepare", count: Math.floor(Math.random() * 5) + 1, timestamp: now - 2000 },
      { type: "prepare", count: Math.floor(Math.random() * 5) + 1, timestamp: now - 1000 },
      { type: "commit", count: Math.floor(Math.random() * 5) + 1, timestamp: now },
    ],
    suspiciousNodes: [
      {
        id: "node-2",
        status: "delayed-response",
        uptime: 65,
        suspicionScore: Math.floor(Math.random() * 40) + 60,
      },
      {
        id: "node-4",
        status: "inconsistent-votes",
        uptime: 72,
        suspicionScore: Math.floor(Math.random() * 30) + 40,
      },
    ],
  }
}

export async function GET() {
  try {
    const data = generateConsensusData()
    return NextResponse.json(data)
  } catch (error) {
    console.error("Error fetching consensus overview:", error)
    return NextResponse.json({ error: "Failed to fetch consensus data" }, { status: 500 })
  }
}
