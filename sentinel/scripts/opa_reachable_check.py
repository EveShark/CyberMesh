"""Phase 4: OPA reachable check (gateway-style).

Starts a local fake OPA server, points SENTINEL_OPA_URL at it, then runs the
Sentinel gateway (`main.py`) on a small corpus and verifies:
- metadata.opa.status is allow/deny (not skipped/error)
- latency_ms is present and > 0

This is a harness-only script.
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import Any, Dict, Tuple


BASE_DIR = Path(__file__).resolve().parents[1]


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


class _OPAHandler(BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802
        # Minimal OPA response: allow everything but record that it was evaluated.
        length = int(self.headers.get("Content-Length", "0"))
        _ = self.rfile.read(length) if length else b""
        payload = {"result": {"allow": True}}
        body = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
        return


def _start_server(port: int) -> Tuple[HTTPServer, threading.Thread]:
    server = HTTPServer(("127.0.0.1", port), _OPAHandler)

    def _run():
        server.serve_forever(poll_interval=0.1)

    t = threading.Thread(target=_run, daemon=True)
    t.start()
    return server, t


def _read_one_result(ndjson_path: Path) -> Dict[str, Any]:
    for raw in ndjson_path.read_text(encoding="utf-8", errors="ignore").splitlines():
        if not raw.strip():
            continue
        obj = json.loads(raw)
        if isinstance(obj, dict):
            return obj
    return {}


def main() -> int:
    ap = argparse.ArgumentParser(description="OPA reachable check (fake local OPA)")
    ap.add_argument(
        "--input",
        default=str(BASE_DIR / "testdata" / "clean" / "phase3-events" / "action_events.jsonl"),
        help="Input corpus to run via main.py (adapter mode).",
    )
    ap.add_argument(
        "--out",
        default=str(BASE_DIR / "reports" / "opa_reachable_action.ndjson"),
        help="NDJSON results output path.",
    )
    ap.add_argument("--max-events", type=int, default=5, help="Max events to analyze")
    args = ap.parse_args()

    port = _free_port()
    server, _t = _start_server(port)
    try:
        env = os.environ.copy()
        env["SENTINEL_OPA_URL"] = f"http://127.0.0.1:{port}"
        env["SENTINEL_OPA_POLICY"] = "sentinel/allow"
        env["SENTINEL_OPA_TIMEOUT_MS"] = "500"

        cmd = [
            "python",
            str(BASE_DIR / "main.py"),
            str(args.input),
            "--adapter-spec",
            str(BASE_DIR / "sentinel" / "config" / "adapter_action_event.json"),
            "--adapter-format",
            "ndjson",
            "--tenant-id",
            "phase4",
            "--max-events",
            str(args.max_events),
            "--out",
            str(args.out),
            "--no-threat-intel",
            "--no-fast-path",
        ]

        proc = subprocess.run(cmd, cwd=str(BASE_DIR), env=env, capture_output=True, text=True, timeout=120)
        if proc.returncode != 0:
            print(proc.stdout)
            print(proc.stderr)
            return 2

        result = _read_one_result(Path(args.out))
        meta = (result.get("metadata") or {}) if isinstance(result, dict) else {}
        opa = meta.get("opa") if isinstance(meta, dict) else None
        ok = isinstance(opa, dict) and opa.get("status") in ("allow", "deny") and float(opa.get("latency_ms") or 0) > 0

        report = {
            "ok": ok,
            "opa": opa,
            "output_file": str(args.out),
        }
        out_json = BASE_DIR / "reports" / "opa_reachable_summary.json"
        out_md = BASE_DIR / "reports" / "opa_reachable_summary.md"
        out_json.write_text(json.dumps(report, indent=2, sort_keys=True), encoding="utf-8")
        out_md.write_text(
            "\n".join(
                [
                    "# OPA Reachable Check",
                    "",
                    f"- ok: {ok}",
                    f"- output: `{args.out}`",
                    f"- opa.status: `{(opa or {}).get('status') if isinstance(opa, dict) else None}`",
                    f"- opa.latency_ms: `{(opa or {}).get('latency_ms') if isinstance(opa, dict) else None}`",
                ]
            ),
            encoding="utf-8",
        )
        return 0 if ok else 2
    finally:
        server.shutdown()


if __name__ == "__main__":
    raise SystemExit(main())

