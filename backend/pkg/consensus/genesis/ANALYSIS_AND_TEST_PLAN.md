# Genesis Coordinator - Deep Analysis & Test Plan

**File:** `backend/pkg/consensus/genesis/coordinator.go`  
**Lines:** 1087  
**Last Analyzed:** 2025-10-14

---

## 1. ARCHITECTURE ANALYSIS

### 1.1 Core Responsibilities

The Genesis Coordinator manages the **zero-trust genesis ceremony** for distributed consensus:

1. **Attestation Collection**: Each validator broadcasts a cryptographically signed `GenesisReady` message
2. **Quorum Formation**: Aggregator (view 0 leader) collects attestations until quorum (2f+1)
3. **Certificate Generation**: Aggregator builds and signs a `GenesisCertificate` 
4. **Certificate Distribution**: Certificate broadcast triggers consensus activation
5. **State Persistence**: Certificate saved to disk for recovery

### 1.2 Key Components

```
┌─────────────────────────────────────────────────────────────┐
│                    Genesis Coordinator                       │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────┐      ┌──────────────┐    ┌──────────────┐│
│  │ Local Ready │─────▶│ Rebroadcast  │───▶│ Attestation  ││
│  │  Message    │      │    Loop      │    │  Collection  ││
│  └─────────────┘      └──────────────┘    └──────────────┘│
│         │                    │                     │        │
│         │                    │                     ▼        │
│         │                    │            ┌──────────────┐ │
│         │                    └───────────▶│   Quorum     │ │
│         │                                 │   Check      │ │
│         │                                 └──────────────┘ │
│         │                                         │        │
│         ▼                                         ▼        │
│  ┌─────────────┐                        ┌──────────────┐ │
│  │ Timestamp   │                        │ Certificate  │ │
│  │ Validation  │                        │  Generation  │ │
│  └─────────────┘                        └──────────────┘ │
│         │                                         │        │
│         │                                         ▼        │
│         │                                ┌──────────────┐ │
│         └───────────────────────────────▶│  Consensus   │ │
│                                          │ Activation   │ │
│                                          └──────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 1.3 State Machine

```
[Init] ──▶ [Broadcasting] ──▶ [Collecting] ──▶ [Quorum Met] ──▶ [Certificate] ──▶ [Active]
              │                    │                                    │
              └────────────────────┘                                    │
              (Rebroadcast Loop)                                        │
                                                                        │
                                    [Restored from Disk] ───────────────┘
```

### 1.4 Critical Paths

**Happy Path:**
1. All 5 validators start simultaneously
2. Each broadcasts `GenesisReady` with current timestamp
3. All receive within 5s window
4. Aggregator (Node 1) collects 5/5 attestations
5. Certificate generated and broadcast
6. All validators activate consensus
7. Total time: ~5-10 seconds

**Problem Path (Current Bug):**
1. Validator starts, broadcasts initial `GenesisReady` (timestamp: T0)
2. Rebroadcast loop sends SAME message at T0+5s, T0+10s, T0+15s...
3. Other validators receive stale messages (timestamp > 5s old)
4. Timestamp validation fails: `message timestamp too old`
5. Attestations rejected, quorum never achieved
6. Validator crashes after 90s (startup probe timeout)
7. Restart, repeat cycle

---

## 2. BUG IDENTIFICATION

### 2.1 The Rebroadcast Bug

**Location:** Lines 885-935 (`rebroadcastLoop`)

**The Problem:**
```go
func (c *Coordinator) rebroadcastLoop(ctx context.Context) {
    refresh := c.cfg.ReadyRefreshInterval  // ❌ Not set in .env, defaults to 30s
    for {
        interval := c.rebroadcastInterval()  // Returns 5s
        timer := time.NewTimer(interval)
        select {
        case <-timer.C:
            c.readyMu.Lock()
            ready := c.localReady  // ❌ Reusing OLD message
            c.readyMu.Unlock()
            
            // Only creates NEW message if age > 30s
            if refresh > 0 && time.Since(ready.Timestamp) >= refresh {
                newReady, err := c.issueLocalReady(ctx, "refresh_timer")
                // ... creates fresh message
                ready = newReady
            }
            
            // ❌ BUG: Rebroadcasts old message if age < 30s
            hash := ready.Hash()
            if err := c.host.PublishGenesisMessage(ctx, ready); err != nil {
                // ...
            }
        }
    }
}
```

**Timeline:**
- T=0s: Initial `GenesisReady` broadcast (timestamp: 06:13:17.032)
- T=5s: Rebroadcast same message (still 06:13:17.032)
- T=10s: Rebroadcast same message (still 06:13:17.032)
- T=15s: Rebroadcast same message (still 06:13:17.032)
- T=30s: Finally creates NEW message (timestamp: 06:13:47.032)

**Validation Failure:**
```go
// In messages/encoding.go line 438
if now.Sub(timestamp) > tolerance {  // tolerance = 5s for genesis
    return fmt.Errorf("message timestamp too old: %v (now: %v, skew: %v)",
        timestamp, now, tolerance)
}
```

At T=6s, message is rejected (6s > 5s tolerance)

### 2.2 Configuration Gap

**Missing from .env:**
```bash
GENESIS_READY_REFRESH_INTERVAL=10s  # Should be LESS than tolerance
CONSENSUS_GENESIS_CLOCK_SKEW_TOLERANCE=15m  # Genesis-specific tolerance
```

**Current defaults (from code):**
- `ReadyRefreshInterval`: 30 seconds (line 286, main.go)
- `GenesisClockSkewTolerance`: 15 minutes (line 126, coordinator.go)
- Rebroadcast base interval: 5 seconds (line 144, coordinator.go)
- **BUT timestamp validation uses 5 seconds** (encoding.go line 421)

**The Mismatch:**
- Genesis tolerance should be 15 minutes (line 466, coordinator.go)
- But `encoding.go` validation uses regular `ClockSkewTolerance` which defaults to 5 seconds
- Configuration doesn't propagate correctly

### 2.3 Root Causes

1. **Design Flaw**: Rebroadcast logic assumes old messages are acceptable
2. **Configuration Mismatch**: Genesis tolerance (15m) vs validation tolerance (5s)
3. **Missing Config**: `GENESIS_READY_REFRESH_INTERVAL` not documented or set
4. **Tight Coupling**: Timestamp validation happens in encoding layer, not genesis layer

---

## 3. INFRASTRUCTURE FROM .ENV

### 3.1 Real Infrastructure Available

**CockroachDB:**
```
Host: cybermesh-threats-8958.jxf.gcp-asia-southeast1.cockroachlabs.cloud:26257
Database: cybermesh_threats
User: cybermesh_user
TLS: Required (./certs/root.crt)
```

**Kafka (Confluent Cloud):**
```
Bootstrap: pkc-ldvr1.asia-southeast1.gcp.confluent.cloud:9092
Security: SASL/PLAIN over TLS
Topics: ai.anomalies.v1, ai.evidence.v1, control.commits.v1
```

**Redis (Upstash):**
```
Host: merry-satyr-14777.upstash.io:6379
TLS: Required
```

**GKE Cluster:**
```
Namespace: cybermesh
Pods: validator-0 through validator-4
Service: validator-headless (P2P discovery)
Bootstrap Peers: Pre-configured DNS multiaddrs
```

### 3.2 Validator Configuration

**5 Validators:**
| Node | ID | Name   | Role     | P2P Port | Peer ID (from seed)          |
|------|----|----|----------|----------|------------------------------|
| 0    | 1  | orion  | control  | 8001     | 12D3KooWH5qTumkGgH...        |
| 1    | 2  | lyra   | gateway  | 8002     | 12D3KooWCMJ3v2p327...        |
| 2    | 3  | draco  | storage  | 8003     | 12D3KooWGSTWQmr29b...        |
| 3    | 4  | cygnus | observer | 8004     | 12D3KooWRhGrFFpKic...        |
| 4    | 5  | vela   | ingest   | 8005     | 12D3KooWD2z1iJ1Wnb...        |

**Quorum Requirements:**
- Total validators: 5
- Byzantine tolerance: f = (5-1)/3 = 1
- Required quorum: 2f+1 = 3 (minimum for BFT safety)
- Configured quorum: 4 (shown in API stats)

---

## 4. TEST SCENARIOS

### 4.1 Unit Tests (Isolated)

#### Test 1: Message Freshness Logic
**File:** `coordinator_message_freshness_test.go`

```go
func TestMessageFreshness_StaleRebroadcast(t *testing.T) {
    // GIVEN: Coordinator with refresh=30s, rebroadcast=5s
    cfg := Config{
        ReadyRefreshInterval: 30 * time.Second,
        ClockSkewTolerance:   5 * time.Second,
    }
    coord := setupTestCoordinator(t, cfg)
    
    // WHEN: Initial message broadcast
    ready1, _ := coord.buildLocalReady(context.Background())
    time.Sleep(10 * time.Second)
    
    // AND: Rebroadcast triggered (should use OLD message)
    ready2, _ := coord.buildLocalReady(context.Background())
    
    // THEN: Timestamps should differ (BUG: they don't)
    age := time.Since(ready1.Timestamp)
    assert.Greater(t, age, 10*time.Second)
    
    // EXPECT: ready2 should have FRESH timestamp
    assert.Less(t, time.Since(ready2.Timestamp), 1*time.Second)
}

func TestMessageFreshness_RefreshTriggered(t *testing.T) {
    // GIVEN: Coordinator with refresh=10s
    cfg := Config{
        ReadyRefreshInterval: 10 * time.Second,
    }
    coord := setupTestCoordinator(t, cfg)
    
    // WHEN: Message age exceeds refresh interval
    ready1, _ := coord.buildLocalReady(context.Background())
    time.Sleep(11 * time.Second)
    
    // AND: Rebroadcast loop executes
    ready2, _ := coord.buildLocalReady(context.Background())
    
    // THEN: New message should be created
    assert.NotEqual(t, ready1.Hash(), ready2.Hash())
    assert.Less(t, time.Since(ready2.Timestamp), 1*time.Second)
}
```

#### Test 2: Timestamp Validation
**File:** `coordinator_timestamp_test.go`

```go
func TestTimestampValidation_WithinTolerance(t *testing.T) {
    // GIVEN: Coordinator with 5s tolerance
    cfg := Config{
        ClockSkewTolerance: 5 * time.Second,
    }
    coord := setupTestCoordinator(t, cfg)
    
    // WHEN: Message received 3s after creation
    ready := createValidGenesisReady(t)
    time.Sleep(3 * time.Second)
    
    // THEN: Should be accepted
    coord.OnGenesisReady(context.Background(), ready)
    assert.Equal(t, 1, len(coord.attestations))
}

func TestTimestampValidation_ExceedsTolerance(t *testing.T) {
    // GIVEN: Coordinator with 5s tolerance
    cfg := Config{
        ClockSkewTolerance: 5 * time.Second,
    }
    coord := setupTestCoordinator(t, cfg)
    
    // WHEN: Message received 7s after creation
    ready := createValidGenesisReady(t)
    time.Sleep(7 * time.Second)
    
    // THEN: Should be REJECTED
    coord.OnGenesisReady(context.Background(), ready)
    assert.Equal(t, 0, len(coord.attestations))
}

func TestTimestampValidation_GenesisToleranceOverride(t *testing.T) {
    // GIVEN: Coordinator with genesis-specific tolerance
    cfg := Config{
        ClockSkewTolerance:        5 * time.Second,
        GenesisClockSkewTolerance: 15 * time.Minute,
    }
    coord := setupTestCoordinator(t, cfg)
    
    // WHEN: Message received 2 minutes after creation
    ready := createValidGenesisReady(t)
    time.Sleep(2 * time.Minute)
    
    // THEN: Should be ACCEPTED (genesis tolerance applies)
    coord.OnGenesisReady(context.Background(), ready)
    assert.Equal(t, 1, len(coord.attestations))
}
```

#### Test 3: Quorum Calculation
**File:** `coordinator_quorum_test.go`

```go
func TestQuorumCalculation_5Validators(t *testing.T) {
    // GIVEN: 5 validators
    validatorSet := createValidatorSet(t, 5)
    cfg := Config{RequiredQuorum: 0} // Auto-calculate
    coord := setupTestCoordinatorWithValidators(t, cfg, validatorSet)
    
    // THEN: Required quorum should be 2f+1 = 3
    assert.Equal(t, 3, coord.requirement)
}

func TestQuorumCalculation_ExplicitOverride(t *testing.T) {
    // GIVEN: 5 validators with explicit quorum
    validatorSet := createValidatorSet(t, 5)
    cfg := Config{RequiredQuorum: 4}
    coord := setupTestCoordinatorWithValidators(t, cfg, validatorSet)
    
    // THEN: Should use explicit value
    assert.Equal(t, 4, coord.requirement)
}

func TestQuorumAchievement_ExactQuorum(t *testing.T) {
    // GIVEN: Coordinator requiring 3 attestations
    coord := setupTestCoordinator(t, Config{RequiredQuorum: 3})
    
    // WHEN: Exactly 3 attestations received
    for i := 0; i < 3; i++ {
        ready := createGenesisReadyForValidator(t, i)
        coord.OnGenesisReady(context.Background(), ready)
    }
    
    // THEN: Certificate should be generated
    assert.NotNil(t, coord.certificate)
}

func TestQuorumAchievement_BelowQuorum(t *testing.T) {
    // GIVEN: Coordinator requiring 3 attestations
    coord := setupTestCoordinator(t, Config{RequiredQuorum: 3})
    
    // WHEN: Only 2 attestations received
    for i := 0; i < 2; i++ {
        ready := createGenesisReadyForValidator(t, i)
        coord.OnGenesisReady(context.Background(), ready)
    }
    
    // THEN: Certificate should NOT be generated
    assert.Nil(t, coord.certificate)
}
```

#### Test 4: Rebroadcast Exponential Backoff
**File:** `coordinator_backoff_test.go`

```go
func TestRebroadcastBackoff_InitialInterval(t *testing.T) {
    // GIVEN: Coordinator with base=5s, max=60s
    coord := setupTestCoordinator(t, Config{
        ReadyRefreshInterval: 0, // Disable refresh
    })
    
    // WHEN: First rebroadcast
    interval1 := coord.rebroadcastInterval()
    
    // THEN: Should use base interval
    assert.Equal(t, 5*time.Second, interval1)
}

func TestRebroadcastBackoff_ExponentialIncrease(t *testing.T) {
    // GIVEN: Coordinator tracking rebroadcasts
    coord := setupTestCoordinator(t, Config{})
    
    // WHEN: Multiple rebroadcasts of SAME message
    ready := createValidGenesisReady(t)
    hash := ready.Hash()
    
    coord.noteRebroadcast(hash)
    interval1 := coord.rebroadcastInterval()
    
    coord.noteRebroadcast(hash)
    interval2 := coord.rebroadcastInterval()
    
    coord.noteRebroadcast(hash)
    interval3 := coord.rebroadcastInterval()
    
    // THEN: Should double each time
    assert.Equal(t, 5*time.Second, interval1)
    assert.Equal(t, 10*time.Second, interval2)
    assert.Equal(t, 20*time.Second, interval3)
}

func TestRebroadcastBackoff_ResetOnNewMessage(t *testing.T) {
    // GIVEN: Coordinator with backoff at max
    coord := setupTestCoordinator(t, Config{})
    
    ready1 := createValidGenesisReady(t)
    for i := 0; i < 5; i++ {
        coord.noteRebroadcast(ready1.Hash())
    }
    intervalBefore := coord.rebroadcastInterval()
    assert.Equal(t, 60*time.Second, intervalBefore) // Max
    
    // WHEN: New message hash appears
    ready2 := createValidGenesisReady(t) // Different nonce
    coord.noteRebroadcast(ready2.Hash())
    
    // THEN: Should reset to base
    intervalAfter := coord.rebroadcastInterval()
    assert.Equal(t, 5*time.Second, intervalAfter)
}
```

---

### 4.2 Integration Tests (Multi-Node)

#### Test 5: 5-Node Genesis Ceremony (Happy Path)
**File:** `coordinator_integration_test.go`

```go
func TestIntegration_5Node_GenesisSuccess(t *testing.T) {
    // GIVEN: 5 validators with real infrastructure
    cluster := setupRealCluster(t, ClusterConfig{
        Validators:   5,
        UseCockroachDB: true,
        UseKafka:     true,
        UseRedis:     false, // Not needed for genesis
    })
    defer cluster.Teardown()
    
    // WHEN: All validators start simultaneously
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    results := make(chan error, 5)
    for i := 0; i < 5; i++ {
        go func(nodeID int) {
            err := cluster.Validators[nodeID].Start(ctx)
            results <- err
        }(i)
    }
    
    // THEN: All should achieve genesis within 30s
    for i := 0; i < 5; i++ {
        err := <-results
        assert.NoError(t, err, "Validator %d failed", i)
    }
    
    // AND: All should have same certificate
    cert := cluster.Validators[0].GetGenesisCertificate()
    for i := 1; i < 5; i++ {
        assert.Equal(t, cert, cluster.Validators[i].GetGenesisCertificate())
    }
    
    // AND: Certificate should have 5 attestations
    assert.Equal(t, 5, len(cert.Attestations))
}
```

#### Test 6: Delayed Validator Join
**File:** `coordinator_integration_delayed_test.go`

```go
func TestIntegration_DelayedValidator(t *testing.T) {
    // GIVEN: 4 validators start immediately
    cluster := setupRealCluster(t, ClusterConfig{Validators: 5})
    defer cluster.Teardown()
    
    ctx := context.Background()
    
    // Start first 4 validators
    for i := 0; i < 4; i++ {
        go cluster.Validators[i].Start(ctx)
    }
    
    // Wait for genesis certificate (should succeed with 4/5)
    time.Sleep(10 * time.Second)
    cert := cluster.Validators[0].GetGenesisCertificate()
    assert.NotNil(t, cert)
    assert.Equal(t, 4, len(cert.Attestations))
    
    // WHEN: 5th validator joins late
    time.Sleep(2 * time.Minute)
    go cluster.Validators[4].Start(ctx)
    
    // THEN: Should receive certificate from peers
    time.Sleep(5 * time.Second)
    lateCert := cluster.Validators[4].GetGenesisCertificate()
    assert.Equal(t, cert.Hash(), lateCert.Hash())
}
```

#### Test 7: Clock Skew Between Validators
**File:** `coordinator_integration_clock_skew_test.go`

```go
func TestIntegration_ClockSkew_Acceptable(t *testing.T) {
    // GIVEN: 5 validators with clock differences
    cluster := setupRealCluster(t, ClusterConfig{
        Validators: 5,
        ClockSkews: []time.Duration{
            0,              // Validator 0: accurate
            2 * time.Second,  // Validator 1: +2s
            -3 * time.Second, // Validator 2: -3s
            1 * time.Second,  // Validator 3: +1s
            -2 * time.Second, // Validator 4: -2s
        },
    })
    defer cluster.Teardown()
    
    // WHEN: All validators start
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    for i := 0; i < 5; i++ {
        go cluster.Validators[i].StartWithClockSkew(ctx, cluster.ClockSkews[i])
    }
    
    // THEN: Should achieve genesis despite clock differences
    time.Sleep(15 * time.Second)
    for i := 0; i < 5; i++ {
        cert := cluster.Validators[i].GetGenesisCertificate()
        assert.NotNil(t, cert, "Validator %d missing certificate", i)
    }
}

func TestIntegration_ClockSkew_Excessive(t *testing.T) {
    // GIVEN: 5 validators, one with excessive clock skew
    cluster := setupRealCluster(t, ClusterConfig{
        Validators: 5,
        ClockSkews: []time.Duration{
            0,
            0,
            0,
            0,
            10 * time.Minute, // ❌ Validator 4: +10 minutes (exceeds 5s tolerance)
        },
    })
    defer cluster.Teardown()
    
    // WHEN: All validators start
    ctx := context.Background()
    for i := 0; i < 5; i++ {
        go cluster.Validators[i].StartWithClockSkew(ctx, cluster.ClockSkews[i])
    }
    
    // THEN: Genesis should succeed with 4/5 validators
    time.Sleep(15 * time.Second)
    cert := cluster.Validators[0].GetGenesisCertificate()
    assert.NotNil(t, cert)
    assert.Equal(t, 4, len(cert.Attestations))
    
    // AND: Validator 4 should be excluded
    validatorIDs := extractValidatorIDs(cert.Attestations)
    assert.NotContains(t, validatorIDs, cluster.Validators[4].ID)
}
```

---

### 4.3 Regression Tests (Bug Reproduction)

#### Test 8: Stale Message Rebroadcast Bug
**File:** `coordinator_regression_stale_message_test.go`

```go
func TestRegression_StaleMessageRebroadcast_Bug(t *testing.T) {
    // REPRODUCE THE EXACT BUG FROM GKE LOGS
    
    // GIVEN: Coordinator with default config (refresh=30s)
    cfg := Config{
        ReadyRefreshInterval:     30 * time.Second,
        ClockSkewTolerance:       5 * time.Second,  // ❌ BUG: Too strict
        GenesisClockSkewTolerance: 15 * time.Minute, // Not used correctly
    }
    coord := setupTestCoordinator(t, cfg)
    
    // WHEN: Initial broadcast at T=0
    ctx := context.Background()
    ready1, _ := coord.buildLocalReady(ctx)
    coord.recordLocalReady(ctx, ready1, "initial")
    
    timestamp1 := ready1.Timestamp
    t.Logf("Initial broadcast timestamp: %v", timestamp1)
    
    // AND: Rebroadcast triggered at T=5s (still within 30s refresh)
    time.Sleep(6 * time.Second)
    
    coord.readyMu.Lock()
    ready2 := coord.localReady // ❌ BUG: Reusing OLD message
    coord.readyMu.Unlock()
    
    timestamp2 := ready2.Timestamp
    age := time.Since(timestamp2)
    
    t.Logf("Rebroadcast timestamp: %v", timestamp2)
    t.Logf("Message age: %v", age)
    
    // THEN: Message is TOO OLD for validation
    assert.Equal(t, timestamp1, timestamp2, "BUG: Timestamp should be DIFFERENT")
    assert.Greater(t, age, 5*time.Second, "BUG: Message exceeds tolerance")
    
    // AND: Other validators REJECT this message
    mockValidator := setupMockValidator(t, cfg)
    err := mockValidator.validateGenesisReady(ready2)
    
    // EXPECT: "message timestamp too old"
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "timestamp too old")
}

func TestRegression_StaleMessageRebroadcast_Fixed(t *testing.T) {
    // VERIFY THE FIX
    
    // GIVEN: Coordinator with SHORT refresh interval
    cfg := Config{
        ReadyRefreshInterval:     10 * time.Second, // ✅ FIX: Less than tolerance
        ClockSkewTolerance:       15 * time.Second,
        GenesisClockSkewTolerance: 15 * time.Minute,
    }
    coord := setupTestCoordinator(t, cfg)
    
    // WHEN: Initial broadcast at T=0
    ctx := context.Background()
    ready1, _ := coord.buildLocalReady(ctx)
    coord.recordLocalReady(ctx, ready1, "initial")
    
    timestamp1 := ready1.Timestamp
    
    // AND: Rebroadcast triggered at T=11s (exceeds refresh)
    time.Sleep(11 * time.Second)
    
    // Simulate rebroadcast logic
    if time.Since(ready1.Timestamp) >= cfg.ReadyRefreshInterval {
        ready2, _ := coord.buildLocalReady(ctx) // ✅ Creates NEW message
        coord.recordLocalReady(ctx, ready2, "refresh")
        
        timestamp2 := ready2.Timestamp
        age := time.Since(timestamp2)
        
        // THEN: Message should be FRESH
        assert.NotEqual(t, timestamp1, timestamp2)
        assert.Less(t, age, 1*time.Second)
        
        // AND: Other validators should ACCEPT
        mockValidator := setupMockValidator(t, cfg)
        err := mockValidator.validateGenesisReady(ready2)
        assert.NoError(t, err)
    }
}
```

#### Test 9: Validator-0 Crash Loop Reproduction
**File:** `coordinator_regression_crash_loop_test.go`

```go
func TestRegression_Validator0_CrashLoop(t *testing.T) {
    // REPRODUCE EXACT SCENARIO FROM GKE
    
    // GIVEN: 5 validators, validator-0 repeatedly crashes
    cluster := setupRealCluster(t, ClusterConfig{
        Validators: 5,
        NodeConfig: map[int]NodeConfig{
            0: {
                EnableCrashAfter: 90 * time.Second, // Startup probe timeout
                MaxRestarts:      27,               // Observed in GKE
            },
        },
    })
    defer cluster.Teardown()
    
    // WHEN: All validators start
    ctx := context.Background()
    
    restartCounts := make(map[int]int)
    
    // Validator 0 keeps restarting
    go func() {
        for restartCounts[0] < 27 {
            v0 := cluster.Validators[0]
            _ = v0.Start(ctx)
            
            // After 90s, crashes (no genesis achieved)
            time.Sleep(90 * time.Second)
            v0.Crash()
            
            restartCounts[0]++
            t.Logf("Validator 0 restart #%d", restartCounts[0])
            
            // Recreate validator (simulates pod restart)
            cluster.Validators[0] = cluster.CreateValidator(0)
        }
    }()
    
    // Other validators running normally
    for i := 1; i < 5; i++ {
        go cluster.Validators[i].Start(ctx)
    }
    
    // THEN: After 27 restarts, validator-0 in CrashLoopBackOff
    time.Sleep(45 * time.Minute) // 27 * 90s
    
    assert.Equal(t, 27, restartCounts[0])
    assert.False(t, cluster.Validators[0].IsRunning())
    
    // AND: Other validators may have achieved genesis (without validator-0)
    // Depends on whether 4 validators is enough for quorum
    cert := cluster.Validators[1].GetGenesisCertificate()
    if cluster.Config.RequiredQuorum <= 4 {
        assert.NotNil(t, cert)
        assert.LessOrEqual(t, len(cert.Attestations), 4)
    }
}
```

#### Test 10: Genesis Stall at Height 0
**File:** `coordinator_regression_height_zero_test.go`

```go
func TestRegression_ConsensusStallAtHeightZero(t *testing.T) {
    // REPRODUCE: Consensus never starts, height=0, mempool growing
    
    // GIVEN: 5 validators with genesis stall
    cluster := setupRealCluster(t, ClusterConfig{
        Validators: 5,
        EnableKafka: true, // Transactions coming in
    })
    defer cluster.Teardown()
    
    // AND: Bug present (stale message rebroadcast)
    cluster.SetGenesisConfig(Config{
        ReadyRefreshInterval: 30 * time.Second, // ❌ Too long
        ClockSkewTolerance:   5 * time.Second,  // ❌ Too strict
    })
    
    // WHEN: All validators start
    ctx := context.Background()
    for i := 0; i < 5; i++ {
        go cluster.Validators[i].Start(ctx)
    }
    
    // AND: Kafka produces transactions
    cluster.InjectKafkaMessages(100, "ai.anomalies.v1")
    
    // THEN: After 5 minutes, consensus still at height 0
    time.Sleep(5 * time.Minute)
    
    for i := 0; i < 5; i++ {
        height := cluster.Validators[i].GetBlockHeight()
        assert.Equal(t, uint64(0), height, "Validator %d should be stuck at height 0", i)
    }
    
    // AND: Mempool is growing (transactions admitted but not committed)
    for i := 0; i < 5; i++ {
        mempoolSize := cluster.Validators[i].GetMempoolSize()
        assert.Greater(t, mempoolSize, 50, "Validator %d should have backed-up mempool", i)
    }
    
    // AND: View number is high (many view changes)
    view := cluster.Validators[1].GetCurrentView()
    assert.Greater(t, view, uint64(50), "Should have many failed view changes")
}
```

---

### 4.4 Performance Tests

#### Test 11: Genesis Ceremony Latency
**File:** `coordinator_performance_test.go`

```go
func TestPerformance_GenesisLatency_5Nodes(t *testing.T) {
    // GIVEN: 5 validators
    cluster := setupRealCluster(t, ClusterConfig{Validators: 5})
    defer cluster.Teardown()
    
    // WHEN: All start simultaneously
    startTime := time.Now()
    
    ctx := context.Background()
    for i := 0; i < 5; i++ {
        go cluster.Validators[i].Start(ctx)
    }
    
    // Wait for genesis completion
    cluster.WaitForGenesis(30 * time.Second)
    
    duration := time.Since(startTime)
    
    // THEN: Should complete within 15 seconds
    assert.Less(t, duration, 15*time.Second)
    
    t.Logf("Genesis ceremony completed in %v", duration)
}

func TestPerformance_RebroadcastBandwidth(t *testing.T) {
    // GIVEN: Coordinator with active rebroadcast
    coord := setupTestCoordinator(t, Config{})
    
    // WHEN: Running for 5 minutes
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    
    messagesSent := 0
    bytesSent := 0
    
    // Mock message publisher
    coord.host = &mockHost{
        publishFunc: func(msg types.Message) error {
            messagesSent++
            bytesSent += len(encodeMessage(msg))
            return nil
        },
    }
    
    go coord.rebroadcastLoop(ctx)
    <-ctx.Done()
    
    // THEN: Should not exceed reasonable bandwidth
    t.Logf("Messages sent: %d", messagesSent)
    t.Logf("Bytes sent: %d KB", bytesSent/1024)
    
    // Expected: ~60 rebroadcasts (5min / 5s) * ~500 bytes = 30 KB
    assert.Less(t, messagesSent, 100)
    assert.Less(t, bytesSent, 100*1024) // 100 KB max
}
```

---

## 5. TEST INFRASTRUCTURE SETUP

### 5.1 Test Cluster Setup

```go
// File: coordinator_test_helpers.go

type ClusterConfig struct {
    Validators      int
    UseCockroachDB bool
    UseKafka       bool
    UseRedis       bool
    ClockSkews     []time.Duration
    NodeConfig     map[int]NodeConfig
}

type NodeConfig struct {
    EnableCrashAfter time.Duration
    MaxRestarts      int
}

type TestCluster struct {
    Config     ClusterConfig
    Validators []*TestValidator
    DB         *sql.DB
    Kafka      *testKafkaCluster
    Redis      *testRedisClient
}

func setupRealCluster(t *testing.T, cfg ClusterConfig) *TestCluster {
    t.Helper()
    
    cluster := &TestCluster{Config: cfg}
    
    // Setup real CockroachDB
    if cfg.UseCockroachDB {
        dsn := os.Getenv("DB_DSN")
        if dsn == "" {
            t.Skip("DB_DSN not set, skipping real DB test")
        }
        db, err := sql.Open("postgres", dsn)
        require.NoError(t, err)
        cluster.DB = db
    }
    
    // Setup real Kafka
    if cfg.UseKafka {
        brokers := os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
        if brokers == "" {
            t.Skip("KAFKA_BOOTSTRAP_SERVERS not set, skipping real Kafka test")
        }
        kafka := setupKafka(t, brokers)
        cluster.Kafka = kafka
    }
    
    // Setup real Redis
    if cfg.UseRedis {
        redisHost := os.Getenv("REDIS_HOST")
        if redisHost == "" {
            t.Skip("REDIS_HOST not set, skipping real Redis test")
        }
        redis := setupRedis(t, redisHost)
        cluster.Redis = redis
    }
    
    // Create validators
    cluster.Validators = make([]*TestValidator, cfg.Validators)
    for i := 0; i < cfg.Validators; i++ {
        cluster.Validators[i] = cluster.CreateValidator(i)
    }
    
    return cluster
}

func (c *TestCluster) CreateValidator(nodeID int) *TestValidator {
    // Load validator config from .env
    validatorID := types.ValidatorID{} // Derive from NODE_ID
    
    // Load signing key
    keyHex := os.Getenv(fmt.Sprintf("CRYPTO_SIGNING_KEY_HEX_%d", nodeID+1))
    
    // Create validator with real components
    return &TestValidator{
        ID:       nodeID,
        NodeID:   validatorID,
        SignKey:  loadSigningKey(keyHex),
        DB:       c.DB,
        Kafka:    c.Kafka,
        Config:   loadValidatorConfig(nodeID),
    }
}
```

### 5.2 Test Execution Plan

**Phase 1: Unit Tests (Week 1)**
- Implement Tests 1-4
- Run locally with mocks
- Verify all test scenarios pass

**Phase 2: Integration Tests (Week 2)**
- Setup real infrastructure access
- Implement Tests 5-7
- Run on GKE with real validators

**Phase 3: Regression Tests (Week 3)**
- Implement Tests 8-10
- Reproduce exact bug scenarios
- Verify fix effectiveness

**Phase 4: Performance Tests (Week 4)**
- Implement Test 11
- Baseline performance metrics
- Identify optimization opportunities

### 5.3 CI/CD Integration

```yaml
# .github/workflows/genesis-tests.yml
name: Genesis Coordinator Tests

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - name: Run Unit Tests
        run: go test -v ./backend/pkg/consensus/genesis/... -tags=unit
  
  integration-tests:
    runs-on: ubuntu-latest
    needs: unit-tests
    steps:
      - uses: actions/checkout@v3
      - name: Setup Infrastructure
        run: |
          # Start local CockroachDB, Kafka, Redis
          docker-compose -f test/docker-compose.test.yml up -d
      - name: Run Integration Tests
        env:
          DB_DSN: ${{ secrets.TEST_DB_DSN }}
          KAFKA_BOOTSTRAP_SERVERS: ${{ secrets.TEST_KAFKA_BROKERS }}
        run: go test -v ./backend/pkg/consensus/genesis/... -tags=integration
  
  regression-tests:
    runs-on: ubuntu-latest
    needs: integration-tests
    steps:
      - uses: actions/checkout@v3
      - name: Run Regression Tests
        run: go test -v ./backend/pkg/consensus/genesis/... -tags=regression
```

---

## 6. RECOMMENDATIONS

### 6.1 Immediate Fixes (Critical)

1. **Fix Rebroadcast Logic**
   - **File:** `coordinator.go` lines 914-920
   - **Change:** Always create fresh messages OR set refresh interval < tolerance
   ```go
   // Option A: Always fresh
   newReady, err := c.issueLocalReady(ctx, "rebroadcast")
   
   // Option B: Short refresh
   if refresh <= 0 || refresh > c.cfg.ClockSkewTolerance {
       refresh = c.cfg.ClockSkewTolerance / 2
   }
   ```

2. **Add Missing Config**
   - Add to `.env`:
   ```bash
   GENESIS_READY_REFRESH_INTERVAL=10s
   CONSENSUS_GENESIS_CLOCK_SKEW_TOLERANCE=15m
   ```

3. **Update Timestamp Validation**
   - **File:** `messages/encoding.go` line 420-427
   - **Change:** Use `GenesisClockSkewTolerance` for genesis messages
   ```go
   case *GenesisReady, *GenesisCertificate:
       tolerance = e.config.GenesisClockSkewTolerance  // Use genesis-specific tolerance
   ```

### 6.2 Architecture Improvements (Medium Priority)

1. **Separate Timestamp Validation**
   - Move genesis-specific validation to coordinator
   - Don't rely on encoding layer for business logic

2. **Add Telemetry**
   - Expose rebroadcast metrics
   - Track message age distribution
   - Monitor quorum achievement time

3. **Improve Logging**
   - Log when messages are rejected due to age
   - Track certificate propagation path
   - Add structured fields for debugging

### 6.3 Testing Strategy

1. **Automated Regression Testing**
   - Run Test 8-10 on every PR
   - Prevent similar bugs in future

2. **Chaos Engineering**
   - Random validator crashes
   - Network partitions
   - Clock skew injection

3. **Performance Benchmarks**
   - Baseline genesis latency
   - Track over time
   - Alert on regressions

---

## 7. APPENDIX

### 7.1 Key Metrics to Monitor

| Metric | Target | Alert Threshold |
|--------|--------|----------------|
| Genesis Latency | < 15s | > 30s |
| Rebroadcast Count | < 6 | > 20 |
| Timestamp Rejections | 0 | > 5 |
| Quorum Achievement | 100% | < 80% |
| Certificate Size | < 10 KB | > 50 KB |

### 7.2 Debug Commands

```bash
# Check validator readiness
kubectl exec -n cybermesh validator-0 -- curl -s localhost:9441/api/v1/ready

# Get genesis stats
kubectl exec -n cybermesh validator-0 -- curl -s localhost:9441/api/v1/stats | jq '.consensus'

# Check message timestamps
kubectl logs -n cybermesh validator-0 | grep "genesis ready" | jq '.timestamp'

# Monitor rebroadcasts
kubectl logs -n cybermesh validator-0 -f | grep "rebroadcast"
```

### 7.3 Expected Log Patterns

**Healthy Genesis:**
```
T+0s: "broadcasting local genesis ready"
T+2s: "genesis ready attestation accepted" (from peer 1)
T+3s: "genesis ready attestation accepted" (from peer 2)
T+4s: "genesis ready attestation accepted" (from peer 3)
T+5s: "quorum check - quorum achieved"
T+5s: "building genesis certificate"
T+6s: "genesis ceremony complete"
```

**Buggy Genesis:**
```
T+0s: "broadcasting local genesis ready"
T+5s: "[GENESIS] rebroadcast successful"
T+7s: "ready attestation expired" (from other validators)
T+10s: "[GENESIS] rebroadcast successful"
T+12s: "ready attestation expired"
... (repeats until crash at T+90s)
```

---

**End of Test Plan**
