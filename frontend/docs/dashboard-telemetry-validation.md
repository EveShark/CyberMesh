# Dashboard Telemetry Validation Checklist

## Backend API
- `curl -s http://localhost:9441/api/v1/stats | jq '.backend.readiness.checks'`
  - Expect non-empty map with `storage`, `state`, `mempool`, `kafka`, `redis`, `p2p_quorum` keys.
- `curl -s http://localhost:9441/api/v1/metrics | rg 'cybermesh_api_requests_total'`
  - Counter should increase after hitting any API route; `cybermesh_api_request_errors_total` increases only on 4xx/5xx responses.
- `curl -s http://localhost:9441/api/v1/metrics | rg 'kafka_producer_publish_success_total'`
  - Success counter > 0 once Kafka publishes; failure counter should remain 0 in healthy runs.

## Network Overview
- `curl -s http://localhost:9441/api/v1/network/overview | jq '.average_latency_ms'`
  - Value should be > 0 once peers respond to pings (latency probe runs every 30s).
- `curl -s http://localhost:9441/api/v1/network/overview | jq '.connected_peers'`
  - Matches sidebar KPI count and “Active Peers” card on `/overview`.
- Browser: `/overview` Network Health card should show both peers and latency; `/network` latency chart should no longer display `0 ms`.

## Consensus Timeline
- `curl -s http://localhost:9441/api/v1/consensus/overview | jq '.data.proposals | length'`
  - Should reflect recent persisted proposals (<=16 entries).
- `curl -s http://localhost:9441/api/v1/consensus/overview | jq '.data.votes[].type' | sort | uniq -c`
  - Expect `proposal`, `vote`, `commit`, and `view_change` records; counts align with persisted events (view changes fallback to engine metrics).
- Frontend `/network` “Vote Timeline” chart should render stacked areas instead of placeholder state.

## Infrastructure Widget
- `curl -s http://localhost:9441/api/v1/metrics | rg 'redis_pool_hits_total'`
  - Hits/misses counters populate Redis health badge.
- `curl -s http://localhost:9441/api/v1/metrics | rg 'cockroach_pool_open_connections'`
  - Gauge > 0 once database connections open; badge flips to “Active.”
- `/overview` Infrastructure widget should show Kafka/Redis/Cockroach statuses with contextual counts (success/fail, hits/misses, open connections).

## UI Regression Checks
- `/overview`
  - Backend readiness hero card displays last check timestamp and uptime (non-`--`).
  - Trend strip latency delta shows numeric value (not `--`).
- `/network`
  - Peer list reflects latency probe values.
- `/metrics`
  - API Throughput KPI updates when exercising endpoints (manually trigger success/error requests).
