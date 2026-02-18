# Phase 3 Schema Notes (Standalone)

Purpose: capture the schema direction and external standards to align with
before implementing Phase 3 agents.

## Standards to align with
- OpenTelemetry Logs/Events (event name, timestamps, severity, attributes)
- Elastic Common Schema (event.* categorization, rule.*, user.*)
- CloudEvents envelope (id, source, type, specversion)
- MCP official spec/SDK for tool-call fields (do not guess)

## Phase 3 Agents (from PRD)
- Sequence Risk Agent
- MCP Runtime Controls Agent
- Exfil/DLP Agent
- Resilience Agent

## Design approach
1) Define canonical schemas in Sentinel first.
2) Keep fields realistic and standards-aligned.
3) Use synthetic payloads only for tests once schemas are fixed.
4) Later: map telemetry-layer outputs to these schemas via adapters.

## Key edge cases to cover
Sequence Risk:
- Out-of-order or partial chains
- Concurrent chains sharing tools
- Duplicate event_ids / replays

MCP Runtime Controls:
- New MCP server appears
- Tool schema changes
- Excessive arg size or encoded payloads
- Tool output contains instructions (prompt injection)

Exfil/DLP:
- Chunked or encoded exfil
- "Allowed" destinations used for exfil
- Missing or wrong data classification

Resilience:
- Agent timeouts, missing telemetry
- Sensor blind spots
- Clock skew between event time and observed time

## Next steps
- Extract MCP fields from official spec/SDK
- Draft Phase 3 schemas in sentinel/contracts/schemas.py
- Define adapter mappings for telemetry-layer
- Add unit + integration tests with synthetic fixtures

## References
- OpenTelemetry Logs data model (event name, severity, attributes)
- Elastic Common Schema (event.* categorization, user.*, rule.*)
- CloudEvents spec (id, source, type, specversion)
- MCP spec (JSON-RPC messages, tools list/call fields)

## Field grounding (OTel / ECS / CloudEvents)
OpenTelemetry LogRecord fields to align with:
- Timestamp, ObservedTimestamp
- SeverityText, SeverityNumber
- Body
- Attributes
- TraceId, SpanId
- Resource, InstrumentationScope

ECS event categorization fields to align with:
- event.kind
- event.category
- event.type
- event.outcome

CloudEvents required context attributes:
- specversion
- id
- source
- type

## MCP field grounding (v1)
MCP runtime events must align to JSON‑RPC envelopes and MCP tool methods:
- JSON‑RPC: `jsonrpc`, `id`, `method`, `params`, `result`, `error` (code/message/data)
- JSON‑RPC IDs must be string/number and not null; notifications omit `id`
- Tool list: `tools/list` response includes `tools[]` with `name`, `description`, `inputSchema`
- Tool list: 2025‑06‑18 spec adds tool `annotations` (and optional presentation fields)
- Tool call: `tools/call` request includes `params.name` and `params.arguments`
- Tool list change notifications: `notifications/tools/list_changed` (gated by `listChanged` capability)
These map into `MCPRuntimeEventV1` fields (method, direction, jsonrpc/id, params/result/error, tools, cursor/next_cursor, list_changed).

Spec revision used for v1 grounding:
- MCP 2025‑11‑25 (basic messages, JSON‑RPC envelope)
- MCP 2025‑06‑18 (tools list/call + list_changed capability)
