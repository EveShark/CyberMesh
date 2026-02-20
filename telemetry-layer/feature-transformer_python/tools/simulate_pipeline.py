import json
import time
import sys
from pathlib import Path
from typing import List, Dict, Any

base_dir = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(base_dir))

from schema import load_flow_schema, load_cic_schema  # noqa: E402
from transformer import transform_flow, _validate_required, _dlq_envelope, _to_db_row  # noqa: E402
from ml.features_flow import FlowFeatureExtractor  # noqa: E402


def _sample_flows() -> List[Dict[str, Any]]:
    now = int(time.time())
    return [
        {
            "schema": "flow.v1",
            "ts": now,
            "tenant_id": "default",
            "src_ip": "10.0.0.1",
            "dst_ip": "10.0.0.2",
            "src_port": 12345,
            "dst_port": 443,
            "proto": 6,
            "flow_id": "f1",
            "bytes_fwd": 12000,
            "bytes_bwd": 3400,
            "pkts_fwd": 15,
            "pkts_bwd": 8,
            "duration_ms": 5000,
        },
        {
            "schema": "flow.v1",
            "ts": now,
            "tenant_id": "default",
            "src_ip": "2001:db8::1",
            "dst_ip": "2001:db8::2",
            "src_port": 53,
            "dst_port": 5353,
            "proto": 17,
            "flow_id": "f2",
            "bytes_fwd": 500,
            "bytes_bwd": 900,
            "pkts_fwd": 4,
            "pkts_bwd": 6,
            "duration_ms": 1000,
        },
        {
            "schema": "flow.v1",
            "ts": now,
            "tenant_id": "default",
            "src_ip": "10.0.0.3",
            "dst_ip": "10.0.0.4",
            "src_port": 1,
            "dst_port": 1,
            "proto": 1,
            "flow_id": "f3",
            "bytes_fwd": 0,
            "bytes_bwd": 0,
            "pkts_fwd": 0,
            "pkts_bwd": 0,
            "duration_ms": 0,
        },
        {
            "schema": "flow.v1",
            "ts": now,
            "tenant_id": "default",
            "src_ip": "10.0.0.5",
            "dst_ip": "10.0.0.6",
            "src_port": 22,
            "dst_port": 22,
            "proto": 6,
            "bytes_fwd": 100,
            "bytes_bwd": 200,
            "pkts_fwd": 1,
            "pkts_bwd": 2,
            "duration_ms": 2000,
        },  # missing flow_id (invalid)
        {
            "schema": "bad.v1",
            "ts": now,
            "tenant_id": "default",
        },  # invalid schema
    ]


def run() -> None:
    flow_schema = load_flow_schema()
    cic_schema = load_cic_schema()
    feature_names = FlowFeatureExtractor.FEATURE_COLUMNS

    flows = _sample_flows()
    dlq = []
    rows = []

    min_coverage = 0.45
    for flow in flows:
        try:
            if flow.get("schema") != flow_schema.name:
                raise ValueError("invalid flow schema")
            _validate_required(flow, flow_schema.required)
            event = transform_flow(flow, feature_names, mask_enabled=True)
            _validate_required(event, cic_schema.required)
            if float(event.get("feature_coverage", 0.0)) < min_coverage:
                raise ValueError("feature coverage below threshold")
            cols, vals = _to_db_row(event, feature_names)
            rows.append((cols, vals))
        except Exception as exc:
            env = _dlq_envelope("SIMULATED_ERROR", str(exc), json.dumps(flow).encode("utf-8"))
            dlq.append(env)

    print(f"valid_events={len(rows)} invalid_events={len(dlq)}")
    if rows:
        print(f"sample_row_cols={len(rows[0][0])} flow_id={rows[0][1][7]}")
    if dlq:
        print(f"dlq_sample={json.dumps(dlq[0])}")


if __name__ == "__main__":
    run()
