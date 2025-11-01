# P2P & Consensus Dashboard Data Audit

This document captures the current backend (Go API) and AI-service capabilities versus the data expected by the `frontend/p2p and consensus dashboard` components. Use it as the source of truth while wiring the dashboards to real services.

## 1. Backend HTTP API Inventory (`backend/pkg/api`)

| Endpoint | Purpose | Status / Notes |
| --- | --- | --- |
| `GET /api/v1/health` | Liveness (version, timestamp) | ✅ Available |
| `GET /api/v1/ready` | Readiness with detailed `checks` & `details` (consensus, P2P) | ✅ Available (includes `phase`, `p2p_active_peers`, quorum info) |
| `GET /api/v1/metrics` | Prometheus exposition | ✅ Available |
| `GET /api/v1/blocks/latest` | Latest block summary + anomaly counts | ✅ Available |
| `GET /api/v1/blocks?start=&limit=` | Historical block range (requires `start`) | ✅ Available |
| `GET /api/v1/blocks/<height>[?include_txs=true]` | Block by height, optional tx payload | ✅ Available |
| `GET /api/v1/state/root` | Current state root/version | ✅ Available |
| `GET /api/v1/state/<key>[?version=]` | Deterministic state lookup + proof | ✅ Available |
| `GET /api/v1/validators[?status=]` | Validator roster (`Status`, `UptimePercentage`, `ProposedBlocks`) | ✅ Available |
| `GET /api/v1/stats` | `{ chain, consensus, mempool, network }` snapshot | ✅ Available (network stats currently zeroed) |
| `GET /api/v1/anomalies[?limit=&severity=]` | Evidence transactions (JSON payload unparsed) | ✅ Available (fields mostly placeholder) |
| `GET /api/v1/anomalies/stats` | Severity histogram via JSONB filters | ✅ Available |
| `GET /api/v1/p2p/info` | Peer connectivity metadata | ❌ Handler commented out |
| Proposals/votes history, suspicious node scoring, peer latency endpoints | ❌ Not implemented |

### Key Data Already Exposed
- Consensus leader (`/stats.consensus.current_leader`), view/round/quorum.
- Readiness details (`/ready.details`) include `p2p_connected_peers`, `p2p_active_peers`, quorum requirements, activation info.
- Validator uptime/status.
- Block anomaly counts via evidence transaction scanning.

### Gaps
- No structured proposal/vote timeline.
- No PBFT message log or validator voting participation metrics.
- No peer latency/health endpoint; `/stats.network` fields are zero.
- Suspicious node scoring absent (requires combining consensus + AI signals).

## 2. AI Service HTTP API Inventory (`ai-service/src/api/server.py`)

| Endpoint | Purpose | Status / Notes |
| --- | --- | --- |
| `GET /health` | Liveness, uptime | ✅ Available |
| `GET /ready` | Ready flag, producer/consumer state, circuit breaker | ✅ Available |
| `GET /metrics` | Prometheus counters (pipeline latency, engine readiness, etc.) | ✅ Available |
| `GET /detections/stats` | Detection-loop metrics (`detections_total`, `published`, `rate_limited`, `avg_latency_ms`, etc.) | ⚠️ Available but `detections_total` never increments because the detection loop skips `record_pipeline_result` |
| Detection history, severity breakdowns, variant analytics | ❌ Not exposed |

### Pipeline Capabilities
- Kafka producer publishes anomalies/evidence when the detection loop fires.
- Prometheus collector defines engine-specific histograms and gauges.

### Gaps for the Dashboard
- No REST surface for historical detections or per-node suspicious scores.
- Detection metrics omit abstentions/published counts in Prometheus because of missing recorder calls.

## 3. Frontend Dashboard Data Expectations vs Availability

### Consensus Page (`components/consensus-page.tsx`)

| Field | Expected Data | Source Today | Status |
| --- | --- | --- | --- |
| `leader` | Current consensus leader | `/api/v1/stats.consensus.current_leader` | ✅ |
| `term` | View / term number | `/api/v1/stats.consensus.view` | ✅ |
| `phase` | PBFT phase | `/api/v1/ready.phase` or `details.consensus_phase` | ✅ |
| `activePeers` | Active peer count | `/api/v1/ready.details.p2p_active_peers` | ✅ (readiness only) |
| `quorumSize` | 2f+1 threshold | `/api/v1/stats.consensus.quorum_size` | ✅ |
| `proposals[]` | Recent proposal history `{ block, timestamp }` | ❌ Missing backend support |
| `votes[]` | Vote timeline by type | ❌ Missing backend support |
| `suspiciousNodes[]` | Node scoring (`status`, `uptime`, `suspicionScore`) | ❌ Requires new aggregation from backend + AI |

### Network Page (`components/network-page.tsx`)

| Field | Expected Data | Source Today | Status |
| --- | --- | --- | --- |
| `connectedPeers` | Connected peers | `/api/v1/ready.details.p2p_connected_peers` | ✅ (readiness only) |
| `avgLatency` | Network latency (ms) | `/api/v1/stats.network.avg_latency_ms` | ❌ Always 0; needs P2P metrics |
| `consensusRound` | Current round | `/api/v1/stats.consensus.round` | ✅ |
| `leaderStability` | Leader tenure score | ❌ Not tracked |
| `nodes[]` (`status`, `latency`, `uptime`) | Validator health + P2P metrics | Partial: uptime from `/api/v1/validators`, latency/status missing |
| `pbftPhase` | PBFT phase | `/api/v1/ready.phase` | ✅ |
| `leader` | Current leader | `/api/v1/stats.consensus.current_leader` | ✅ |
| `votingStatus` | Validator vote participation map | ❌ Missing |

### Mock API Routes (to replace)
- `/app/api/consensus/overview/route.ts` – currently random values; must call real backend endpoints once implemented.
- `/app/api/nodes/health/route.ts` – also random; replace with backend/AI aggregation.

## 4. Recommended Implementation Steps
1. **Design new backend endpoints** for proposal timeline, vote events, peer metrics, suspicious node scoring. Likely under `/api/v1/consensus/...` and `/api/v1/network/...`.
2. **Expose AI detections** via REST (e.g., `/detections/recent`, `/detections/suspicious-nodes`) and ensure detection loop updates Prometheus counters (`record_pipeline_result`).
3. **Aggregate data** in backend by combining consensus engine state, P2P router metrics, validator records, and AI service detections.
4. **Replace Next.js mock routes** with real fetchers targeting the above endpoints, handling loading/error states.
5. **Update frontend components** to map API payloads directly (remove random client-side generation).

Keep this file updated as backend/AI features are implemented so the dashboard wiring remains aligned with server capabilities.
