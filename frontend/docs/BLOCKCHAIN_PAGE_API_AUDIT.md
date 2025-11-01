# Blockchain Activity Page - Backend API Audit

## Overview
This document audits all data requirements for the new `/blockchain` page and maps them to existing backend API endpoints.

---

## 1. METRICS BAR (6 Cards)

### Required Data:
1. **Latest Block Height** - Current blockchain height
2. **Total Transactions** - Cumulative transaction count
3. **Avg Block Time** - Average time between blocks
4. **Success Rate** - Transaction success percentage
5. **Anomaly Count** - Total anomalies detected
6. **Live Status** - System operational status

### Backend API Endpoints:
✅ **Already Available:**
```typescript
// GET /api/v1/stats
{
  chain: {
    height: number                    // → Latest Height
    total_transactions: number        // → Total TXs
    avg_block_time_seconds: number   // → Avg Block Time
  }
}

// GET /api/v1/health
{
  status: "ok" | "degraded"          // → Live Status
}
```

❌ **Missing/Needs Implementation:**
- **Success Rate**: Need to track failed vs successful transactions
- **Anomaly Count**: Need to count transactions with `type: "anomaly_detected"`

### Recommendation:
Add to `/api/v1/stats`:
```go
type StatsSummary struct {
    Chain struct {
        Height               int64   `json:"height"`
        TotalTransactions    int64   `json:"total_transactions"`
        SuccessfulTxs        int64   `json:"successful_txs"`        // NEW
        FailedTxs            int64   `json:"failed_txs"`            // NEW
        AnomalyCount         int64   `json:"anomaly_count"`         // NEW
        AvgBlockTimeSeconds  float64 `json:"avg_block_time_seconds"`
    } `json:"chain"`
}
```

---

## 2. SEARCH & FILTERS

### Required Data:
- **Block Type Filter**: All / Normal / Anomalies / Failed
- **Time Range Filter**: Last 1h, 6h, 24h, 7d
- **Proposer Filter**: List of unique proposers

### Backend API Endpoints:
✅ **Already Available:**
```typescript
// GET /api/v1/blocks?start=X&limit=Y
// Returns blocks with timestamps and proposers
```

❌ **Missing/Needs Implementation:**
- **Proposer List**: Need endpoint to get unique proposers
- **Filter Support**: Add query params to `/blocks` endpoint

### Recommendation:
Enhance `/api/v1/blocks`:
```
GET /api/v1/blocks?start=0&limit=20&type=anomaly&since=3600&proposer=validator-1
```

Add new endpoint:
```
GET /api/v1/validators/proposers
// Returns list of unique proposer addresses
```

---

## 3. BLOCK FEED

### Required Data:
- Block height
- Block hash
- Timestamp
- Proposer address
- Transaction count
- **Anomaly count per block** (NEW)
- **Consensus status** (approved/delayed/failed) (NEW)

### Backend API Endpoints:
✅ **Already Available:**
```typescript
// GET /api/v1/blocks?start=0&limit=20
{
  blocks: [
    {
      height: number
      hash: string
      timestamp: number
      proposer: string
      transaction_count: number
      transactions?: Array<{
        type: string  // Can check for "anomaly_detected"
      }>
    }
  ]
}
```

❌ **Missing/Needs Implementation:**
- **Anomaly Count**: Count transactions where `type === "anomaly_detected"` per block
- **Consensus Status**: Block metadata about consensus (approved/delayed/failed)
- **Consensus Details**: Time taken, validator signatures

### Recommendation:
Enhance `BlockSummary`:
```go
type BlockSummary struct {
    Height           int64  `json:"height"`
    Hash             string `json:"hash"`
    Timestamp        int64  `json:"timestamp"`
    Proposer         string `json:"proposer"`
    TransactionCount int    `json:"transaction_count"`
    AnomalyCount     int    `json:"anomaly_count"`          // NEW - count anomaly txs
    ConsensusStatus  string `json:"consensus_status"`       // NEW - "approved", "delayed", "failed"
    ConsensusTime    float64 `json:"consensus_time_seconds"` // NEW - time to reach consensus
    SignatureCount   int    `json:"signature_count"`        // NEW - number of validator signatures
}
```

---

## 4. DECISION TIMELINE TAB (Chart)

### Required Data:
- Last 20 blocks consensus decisions
- Per block: Approved count, Rejected count, Timeout count
- Approval rate percentage
- Timeout count
- Overall consensus health

### Backend API Endpoints:
❌ **Missing - Needs Full Implementation**

### Recommendation:
Create new endpoint:
```
GET /api/v1/consensus/timeline?blocks=20
```

Response:
```json
{
  "timeline": [
    {
      "block_height": 2081,
      "approved": 5,
      "rejected": 0,
      "timeout": 0,
      "consensus_time": 1.2
    }
  ],
  "summary": {
    "approval_rate": 0.987,
    "total_timeouts": 2,
    "health": "excellent"
  }
}
```

**Backend Work Required:**
- Track consensus votes per block (approved/rejected/timeout)
- Store in block metadata or separate consensus log table
- Calculate statistics from consensus history

---

## 5. BLOCK DETAILS TAB

### Required Data:
- Full block information (hash, parent_hash, state_root, etc.)
- All transactions in the block (full list)
- **Consensus signatures** (which validators signed) (NEW)
- Block size

### Backend API Endpoints:
✅ **Partially Available:**
```typescript
// GET /api/v1/blocks/{height}?include_txs=true
{
  height: number
  hash: string
  parent_hash: string
  state_root: string
  timestamp: number
  proposer: string
  transaction_count: number
  size_bytes: number
  transactions: Array<{
    hash: string
    type: string
    size_bytes: number
  }>
}
```

❌ **Missing/Needs Implementation:**
- **Consensus Signatures**: List of validator signatures for this block

### Recommendation:
Add to `BlockSummary` (when `include_txs=true`):
```go
type BlockSummary struct {
    // ... existing fields
    Signatures []ValidatorSignature `json:"signatures,omitempty"`  // NEW
}

type ValidatorSignature struct {
    ValidatorID string `json:"validator_id"`
    PublicKey   string `json:"public_key"`
    Signature   string `json:"signature"`
    Timestamp   int64  `json:"timestamp"`
    Status      string `json:"status"` // "approved", "rejected"
}
```

**Backend Work:**
- Store validator signatures in block metadata
- Include in response when requested

---

## 6. VERIFICATION TAB

### Required Data:
- Block hash validation
- Merkle root verification
- Validator signatures verification
- Chain continuity check
- Timestamp validity

### Backend API Endpoints:
❌ **Missing - Needs Full Implementation**

### Recommendation:
Create new endpoint:
```
POST /api/v1/blocks/verify
```

Request:
```json
{
  "block_hash": "0x...",
  "block_height": 2081
}
```

Response:
```json
{
  "verified": true,
  "block_height": 2081,
  "block_hash": "0x...",
  "checks": {
    "hash_integrity": true,
    "merkle_root": true,
    "signatures": true,
    "chain_continuity": true,
    "timestamp": true
  },
  "details": {
    "signatures_verified": 3,
    "signatures_total": 3,
    "proposer": "0x...",
    "timestamp": "2025-10-28T10:45:32Z"
  },
  "message": "Block verified successfully"
}
```

**Backend Work:**
- Implement cryptographic hash verification
- Verify merkle root against transactions
- Check validator signatures
- Validate parent hash chain
- Check timestamp is within acceptable range

---

## 7. ANOMALY FEED TAB

### Required Data:
- Recent anomalies (last 50-100)
- Anomaly type (ddos, malware, phishing, etc.)
- Severity (critical, high, medium, low)
- Source IP/domain
- Block height where detected
- Confidence score
- Description

### Backend API Endpoints:
✅ **Partially Available:**
```typescript
// GET /api/v1/blocks?start=0&limit=100&include_txs=true
// Filter transactions where type === "anomaly_detected"
```

❌ **Missing/Needs Better Structure:**
- Anomaly transactions need structured payload
- Severity classification
- Source information
- Detailed descriptions

### Recommendation:
Enhance transaction structure:
```go
type Transaction struct {
    Hash      string          `json:"hash"`
    Type      string          `json:"type"`  // "anomaly_detected"
    Payload   json.RawMessage `json:"payload"`
    Timestamp int64           `json:"timestamp"`
}

// For type="anomaly_detected"
type AnomalyPayload struct {
    AnomalyType  string  `json:"anomaly_type"`  // "ddos", "malware", etc.
    Severity     string  `json:"severity"`      // "critical", "high", "medium", "low"
    Source       string  `json:"source"`        // IP or domain
    Confidence   float64 `json:"confidence"`    // 0.0-1.0
    Title        string  `json:"title"`
    Description  string  `json:"description"`
    Evidence     []byte  `json:"evidence,omitempty"`
}
```

Or create dedicated endpoint:
```
GET /api/v1/anomalies?limit=100&severity=critical
```

---

## SUMMARY OF BACKEND WORK NEEDED

### ✅ Already Working (Can Use Now):
1. `/api/v1/stats` - Basic chain stats (height, total TXs, avg block time)
2. `/api/v1/blocks` - Block list with pagination
3. `/api/v1/blocks/{height}` - Individual block details
4. `/api/v1/health` - System status

### 🟡 Needs Enhancement:
1. **`/api/v1/stats`** - Add success_rate, anomaly_count
2. **`/api/v1/blocks`** - Add anomaly_count, consensus_status per block
3. **`/api/v1/blocks/{height}`** - Add validator signatures

### ❌ Needs New Implementation:
1. **`GET /api/v1/consensus/timeline`** - Consensus decision history
2. **`POST /api/v1/blocks/verify`** - Block integrity verification
3. **`GET /api/v1/validators/proposers`** - List unique proposers
4. **`GET /api/v1/anomalies`** - Structured anomaly feed

---

## PRIORITY IMPLEMENTATION ORDER

### Phase 1 - Critical (Connect to Real Data):
1. ✅ Use existing `/blocks` endpoint for block feed
2. ✅ Use existing `/stats` endpoint for metrics
3. 🔧 Add `anomaly_count` field to `BlockSummary`
4. 🔧 Add `consensus_status` field to `BlockSummary`

### Phase 2 - Enhanced Features:
1. 🔧 Implement `/blocks/verify` endpoint
2. 🔧 Add validator signatures to block details
3. 🔧 Track consensus decisions per block

### Phase 3 - Analytics:
1. 🔧 Implement `/consensus/timeline` endpoint
2. 🔧 Create `/anomalies` structured endpoint
3. 🔧 Add filtering support to `/blocks`

---

## CURRENT MOCK DATA TO REPLACE

### Files Using Mock Data:
1. **`blockchain/page.tsx`** - 100 sample blocks with fake hashes
2. **`decision-timeline-tab.tsx`** - 20 sample consensus decisions
3. **`block-details-tab.tsx`** - Sample transactions and signatures
4. **`verification-tab.tsx`** - Simulated verification results
5. **`anomaly-feed-tab.tsx`** - 7 sample anomalies

### When Backend Ready:
Replace `useEffect` mock data with:
```typescript
// Replace this:
useEffect(() => {
  const sampleBlocks = Array.from(...)
  setBlocks(sampleBlocks)
}, [])

// With this:
useEffect(() => {
  const fetchBlocks = async () => {
    const response = await api.backend.listBlocks(0, 100)
    setBlocks(response.blocks)
  }
  fetchBlocks()
}, [])
```

---

## API CALL EXAMPLES (When Implemented)

```typescript
// Fetch blocks for feed
const { blocks } = await api.backend.listBlocks(0, 20)

// Get metrics for metrics bar
const stats = await api.backend.getStats()

// Verify block integrity
const verification = await fetch('/api/v1/blocks/verify', {
  method: 'POST',
  body: JSON.stringify({ block_hash: hash })
})

// Get consensus timeline
const timeline = await fetch('/api/v1/consensus/timeline?blocks=20')

// Get anomalies
const anomalies = await fetch('/api/v1/anomalies?limit=50')
```

---

## CONCLUSION

**Ready to Use Now:**
- Block feed (basic data)
- Metrics bar (partial data)
- Block details (without signatures)

**Backend Work Required:**
- Add anomaly counts to blocks
- Add consensus status tracking
- Implement verification endpoint
- Create consensus timeline endpoint
- Structure anomaly data properly

**Estimated Backend Work:** 2-3 days for critical features, 1-2 weeks for full analytics
