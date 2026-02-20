# Sentinel Hybrid Pipeline (File + Telemetry)

## Overview
Sentinel provides a hybrid, multi-agent pipeline with two graphs:
- **FileAnalysisGraph** for file scanning.
- **TelemetryAnalysisGraph** for network flow telemetry.

The default execution is **parallel** with deterministic merge/dedupe. A **sequential** mode exists for baseline validation and regression checks.

## Architecture Summary
**Modality Router**
- FILE → FileAnalysisGraph
- NETWORK_FLOW → TelemetryAnalysisGraph

**FileAnalysisGraph (Hybrid)**
1. Parse file
2. Fast-path (hash/YARA/signatures)
3. Parallel agents (static, script, malware/ML, threat intel, optional LLM)
4. Merge + dedupe
5. Coordinator decision

**TelemetryAnalysisGraph (Minimal Production-Grade)**
1. Ingest telemetry (CSV/JSON/IPFIX JSON)
2. Normalize to `NetworkFlowFeaturesV1`
3. Parallel agents (rules/heuristics + optional threat intel)
4. Merge + dedupe
5. Coordinator decision

## Execution Modes
### File Analysis
Runs the file graph, including optional fast-path and optional LLM reasoning.

### Telemetry Analysis
Accepts CSV/JSON/IPFIX JSON payloads and converts them into `NetworkFlowFeaturesV1` features.

### Kafka Worker Analysis
For streaming telemetry, use:
```bash
python sentinel/scripts/kafka_gateway.py --log-level INFO
```
Key runtime controls:
- `ENABLE_KAFKA=true`
- `KAFKA_INPUT_TOPIC` / `KAFKA_INPUT_TOPICS`
- `KAFKA_TOPIC_SCHEMA_MAP`, `KAFKA_TOPIC_ENCODING_MAP`
- `KAFKA_OUTPUT_TOPIC`, `KAFKA_DLQ_TOPIC`

Important defaults (when not overridden):
- input topic fallback: `telemetry.features.v1`
- output topic fallback: `ai.anomalies.v1`

## CLI Usage (Standalone `main.py`)
File analysis:
```bash
python main.py <path-to-file> \
  --models-path <path-to-models> \
  --enable-llm \
  --no-fast-path \
  --no-threat-intel \
  --no-external-yara-rules
```

Telemetry analysis:
```bash
python main.py <path-to-payload> --telemetry --format json
python main.py <path-to-payload> --telemetry --format csv
python main.py <path-to-payload> --telemetry --format ipfix
```

## Telemetry Inputs (CSV / JSON / IPFIX JSON)
Telemetry ingestion currently supports **JSON or CSV** payloads and **IPFIX JSON** (not raw binary IPFIX).

Accepted JSON shapes:
- List of records: `[ { ... }, { ... } ]`
- Single record object: `{ ... }`
- Wrapper object: `{ "records": [ ... ] }`

Required fields (aliases supported):
- `src_ip` (`source_ip`, `sip`)
- `dst_ip` (`destination_ip`, `dip`)
- `src_port` (`source_port`, `sport`)
- `dst_port` (`destination_port`, `dport`)
- `protocol` (`proto`, `ip_proto`; supports `tcp|udp|icmp` or numeric)
- `tot_fwd_pkts` (`pkts_fwd`, `packets_fwd`, `forward_packets`)
- `tot_bwd_pkts` (`pkts_bwd`, `packets_bwd`, `backward_packets`)
- `totlen_fwd_pkts` (`bytes_fwd`, `bytes_forward`, `forward_bytes`)
- `totlen_bwd_pkts` (`bytes_bwd`, `bytes_backward`, `backward_bytes`)
- `flow_duration` or duration fields (`duration_us`, `duration_ms`, `duration_s`, `duration`)

Optional fields:
- `syn_flag_cnt`, `ack_flag_cnt`, `rst_flag_cnt`
- `flow_byts_s`, `flow_pkts_s` (computed if missing and duration is valid)
- `pps`, `bps`, `syn_ack_ratio`, `unique_dst_ports_batch`

Notes:
- Negative `flow_duration` is rejected.
- Ports must be in `0–65535`.
- IPFIX adapter expects UTF‑8 JSON; binary IPFIX is not supported.

## Sequential Baseline Mode
Sequential execution is provided for baseline validation.

Enable via CLI:
```bash
python main.py <path> --sequential
```

Enable via env:
```bash
set SENTINEL_SEQUENTIAL=1
```

## Fast‑Path Behavior
Fast‑path can short‑circuit full analysis if hashes/YARA/signatures return a definitive verdict.

Controls:
- `--no-fast-path` disables the fast path.
- `--no-external-yara-rules` disables external YARA rules (built‑in only).

## Degraded Mode & Error Metadata
When optional components are unavailable, Sentinel returns a degraded marker in `metadata`:
- `degraded: true|false`
- `degraded_reasons: [ ... ]`

Possible reasons (current):
- `fast_path_unavailable`
- `yara_unavailable`
- `hash_db_unavailable`
- `signatures_unavailable`
- `ml_models_unavailable`
- `threat_intel_failed`
- `zero_duration` (telemetry flow duration is zero)

## Agent Execution Model
Default: parallel execution with deterministic merge/dedupe.
Baseline: sequential execution with the same merge/dedupe rules.

LLM reasoning runs only when:
- `needs_llm_reasoning` is true, and
- an LLM provider is available (Groq → GLM → Ollama fallback chain).

## Agent Coverage Map (Current)

| Capability | Concrete runtime class/path | Status |
|---|---|---|
| Static file analysis | `StaticAnalysisAgent` | Implemented |
| Script analysis | `ScriptAgent` | Implemented |
| Malware/ML | `MalwareAgent`, `MLAnalysisAgent` | Implemented |
| File threat intel | `ThreatIntelAgent` | Implemented |
| Telemetry flow analysis | `FlowAgent`, `TelemetryThreatIntelAgent` | Implemented |
| Process/event analysis | `ProcessAgent` | Implemented |
| Scanner findings | `ScannerFindingsAgent` | Implemented |
| Rules-hit analysis | `RulesHitAgent` | Implemented |
| Sequence risk | `SequenceRiskAgent` | Implemented |
| MCP runtime controls | `MCPRuntimeControlsAgent` | Implemented |
| Exfil/DLP | `ExfilDLPAgent` | Implemented |
| Resilience | `ResilienceAgent` | Implemented |
| Identity-specific agent | (no dedicated class yet) | Pending dedicated implementation |
| Cloud IAM-specific agent | (no dedicated class yet) | Pending dedicated implementation |

## Output Format
Both file and telemetry flows return a dict with these core fields:
- `threat_level`, `confidence`, `final_score`
- `findings`, `indicators`, `reasoning_steps`, `final_reasoning`
- `errors`, `metadata`, `analysis_time_ms`

File analysis includes:
- `file_path`, `parsed_file`, `static_results`, `ml_results`, `llm_results`

Telemetry analysis includes:
- `flow_features`, `raw_context`, `static_results`, `ml_results`, `llm_results`

The standalone CLI normalizes enums and dataclasses into JSON‑serializable output.

## Library Integration
File analysis:
```python
from sentinel.agents import AnalysisEngine

engine = AnalysisEngine(
    enable_llm=False,
    enable_threat_intel=True,
    enable_fast_path=True,
    use_external_rules=True,
)
result = engine.analyze("path/to/file")
```

Telemetry analysis:
```python
from sentinel.agents import TelemetryAnalysisEngine
from sentinel.telemetry.adapters import parse_json

features_list, errors = parse_json(payload_text)
engine = TelemetryAnalysisEngine(enable_threat_intel=True)
result = engine.analyze_flow(features_list[0])
```

Modality router:
```python
from sentinel.agents import SentinelOrchestrator
from sentinel.contracts import CanonicalEvent, Modality

orchestrator = SentinelOrchestrator(enable_threat_intel=True)

file_event = CanonicalEvent(
    modality=Modality.FILE,
    features={},
    raw_context={"file_path": "path/to/file"},
)
result = orchestrator.analyze_event(file_event)
```
