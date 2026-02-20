# Telemetry Protobuf Contracts (v1)

## Versioning Rules
- **Backward compatible only**: add new optional fields; never change field meaning.
- **No field reuse**: once a field number is used, it is reserved forever.
- **No renames**: keep names stable; add new fields instead of reusing.
- **Enum growth**: append new enum values at the end.
- **Schema IDs**: keep `schema` values stable (`flow.v1`, `cic.v1`, `dlq.v1`).

## Notes
- `feature_mask` is stored as **bytes** (hex when logged as text).
- `flow_id` is required and should be stable for de‑duplication.
- `metrics_known=false` indicates bytes/packets were not observed.
