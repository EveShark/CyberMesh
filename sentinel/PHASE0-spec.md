# Phase 0 Specification (Standalone) — Security First

This document locks the Phase 0 design artifacts for standalone operation:
- Canonical event envelope v1
- Modality list v1
- Adapter config format
- Deterministic merge/dedupe rules
- Schema governance

This is intentionally minimal and security‑first.

## Security Principles (Non‑Negotiable)
- Least data: only required fields are normalized; everything else stays in `raw_context`.
- Tenant isolation: every event must carry `tenant_id` or be rejected/quarantined.
- Authenticity: adapter outputs are signed (HMAC) by the ingest layer.
- Validation: strict type and size checks at adapters; malformed payloads are rejected.
- Redaction: PII/secrets must never be logged or sent to LLMs unredacted.

## Canonical Event Envelope v1
**Required fields**
- `event_id` (string): stable identifier; if missing, generate via normalized hash.
- `tenant_id` (string): mandatory for isolation; missing -> reject.
- `timestamp` (RFC3339 string or epoch ms): original event time.
- `schema_version` (string): e.g., `v1`.
- `modality` (enum string): see Modality list v1.
- `source` (string): tool or adapter name (e.g., `zeek`, `falco`).
- `features` (object): normalized fields for the modality.

**Optional fields**
- `ingest_time` (epoch ms): when Sentinel ingested.
- `correlation_id` (string): link multiple events.
- `raw_context` (object): passthrough of unnormalized fields (validated size).
- `signature` (string): HMAC signature of the envelope payload.
- `metadata` (object): adapter version, tool version, warnings.

**Envelope rules**
- `raw_context` is size‑limited and may be truncated.
- Unknown modality -> reject with error.
- Missing required fields -> reject or quarantine (configurable).

## Modality List v1
- `FILE`
- `NETWORK_FLOW`
- `PROCESS_EVENT`
- `AUTH_EVENT`
- `CLOUD_EVENT`
- `SCAN_FINDINGS`
- `DNS_EVENT` (optional, if needed by adapters)

## Adapter Config Format (YAML)
Adapters are config‑driven. No hardcoded tool mappings in core.

**Format (example)**
```yaml
version: v1
adapter: zeek
modality: NETWORK_FLOW
required_fields:
  - src_ip
  - dst_ip
  - src_port
  - dst_port
  - protocol
  - flow_duration
field_map:
  src_ip: id.orig_h
  dst_ip: id.resp_h
  src_port: id.orig_p
  dst_port: id.resp_p
  protocol: proto
  flow_duration: duration
transforms:
  protocol:
    type: enum
    map:
      tcp: 6
      udp: 17
      icmp: 1
limits:
  max_payload_bytes: 1048576
  max_records: 1000
  parse_timeout_ms: 2000
```

**Adapter rules**
- Unknown fields -> `raw_context`.
- Required fields missing -> error.
- Type mismatch -> error.
- Payload over limits -> reject or truncate (configurable).

## Deterministic Merge/Dedupe Rules
- Primary key: `event_id`.
- Fallback: hash of normalized `features` + `modality` + `tenant_id`.
- Conflict resolution: highest severity, then highest confidence.
- Correlation window: 5 minutes (default, per modality configurable).
- Late arrivals: process if inside window; otherwise flag `metadata.late_event=true`.

## Schema Governance
- Schema registry: Git + CI checks (standalone).
- Breaking changes require `schema_version` bump.
- Deprecation window: minimum two releases.
- Adapter conformance tests: required fields must be populated per modality.

## Phase 0 Acceptance
- Envelope v1 approved.
- Modality list v1 approved.
- Adapter config format approved.
- Merge/dedupe rules approved.
- Governance policy approved.
