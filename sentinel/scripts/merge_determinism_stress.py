"""Stress test merge determinism under high concurrency.

This focuses on `merge_utils.run_parallel_agents` and its dedupe/merge behavior.
It uses harness-only agents that intentionally emit findings/indicators in
non-deterministic order to ensure the merged output remains deterministic.
"""

from __future__ import annotations

import argparse
import json
import os
import random
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Tuple

import sys

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))

from sentinel.agents.base import BaseAgent
from sentinel.agents.merge_utils import run_parallel_agents


@dataclass
class _DummyFinding:
    source: str
    severity: str
    description: str
    confidence: float
    metadata: Dict[str, Any]


class ChaoticAgent(BaseAgent):
    """Agent that emits duplicate/conflicting findings in random order."""

    def __init__(self, name: str, seed: int, n: int) -> None:
        self._name = name
        self._seed = seed
        self._n = n
        self._base_findings: List[Any] = []
        self._base_indicators: List[Dict[str, str]] = []
        self._build_base_payloads()

    @property
    def name(self) -> str:
        return self._name

    def _build_base_payloads(self) -> None:
        # Build deterministic content once; calls will only reorder it.
        rnd = random.Random(self._seed)
        base_desc = [
            "suspicious outbound transfer",
            "prompt injection pattern",
            "unexpected tool schema change",
            "high pps detected",
        ]
        severities = ["low", "medium", "high", "critical"]

        findings: List[Any] = []
        indicators: List[Dict[str, str]] = []
        for i in range(self._n):
            desc = rnd.choice(base_desc)
            sev = rnd.choice(severities)
            conf = rnd.random()
            if i % 2 == 0:
                findings.append(
                    {
                        "source": self.name,
                        "severity": sev,
                        "description": desc,
                        "confidence": conf,
                        "metadata": {"i": i},
                    }
                )
            else:
                findings.append(_DummyFinding(self.name, sev, desc, conf, {"i": i}))

            if rnd.random() < 0.5:
                indicators.append({"type": "ip", "value": f"10.0.0.{rnd.randint(1, 10)}", "context": "dst"})

        self._base_findings = findings
        self._base_indicators = indicators

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        # Reorder deterministically based on iteration to simulate agents that
        # build outputs from sets/dicts where ordering may vary.
        rnd = random.Random(self._seed + int(state.get("iteration", 0)))
        findings = list(self._base_findings)
        indicators = list(self._base_indicators)
        rnd.shuffle(findings)
        rnd.shuffle(indicators)
        return {"findings": findings, "indicators": indicators, "errors": [], "metadata": {"agent": self.name}}


def _stable_view(update: Dict[str, Any]) -> Dict[str, Any]:
    # Only stable keys.
    out: Dict[str, Any] = {}
    for k in ("findings", "indicators", "errors"):
        if k in update:
            out[k] = update.get(k)
    meta = update.get("metadata") or {}
    if isinstance(meta, dict) and "degraded" in meta:
        out["metadata"] = {"degraded": meta.get("degraded"), "degraded_reasons": meta.get("degraded_reasons")}
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description="Merge determinism stress test")
    ap.add_argument("--agents", type=int, default=25, help="Number of agents")
    ap.add_argument("--findings-per-agent", type=int, default=50, help="Findings per agent")
    ap.add_argument("--iterations", type=int, default=50, help="Repetitions to verify determinism")
    ap.add_argument("--out-json", default=str(BASE_DIR / "reports" / "merge_determinism_stress.json"))
    ap.add_argument("--out-md", default=str(BASE_DIR / "reports" / "merge_determinism_stress.md"))
    args = ap.parse_args()

    seed = int(os.getenv("SENTINEL_STRESS_SEED", "1337"))
    agents: List[BaseAgent] = []
    for i in range(args.agents):
        agents.append(ChaoticAgent(name=f"chaotic_{i:02d}", seed=seed + i * 7, n=args.findings_per_agent))

    mismatches: List[Dict[str, Any]] = []
    t0 = time.perf_counter()
    baseline = None
    for it in range(args.iterations):
        state = {"iteration": it}
        merged = run_parallel_agents(state, agents, timeout_seconds=5)
        view = _stable_view(merged)
        blob = json.dumps(view, sort_keys=True, default=str)
        if baseline is None:
            baseline = blob
        elif blob != baseline:
            mismatches.append({"iteration": it})
            # Keep going to see if flakiness is frequent.

    elapsed_ms = (time.perf_counter() - t0) * 1000
    ok = len(mismatches) == 0

    payload = {
        "ok": ok,
        "agents": args.agents,
        "findings_per_agent": args.findings_per_agent,
        "iterations": args.iterations,
        "elapsed_ms": round(elapsed_ms, 2),
        "mismatch_count": len(mismatches),
        "mismatches": mismatches[:10],
    }

    out_json = Path(args.out_json)
    out_md = Path(args.out_md)
    out_json.parent.mkdir(parents=True, exist_ok=True)
    out_json.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    out_md.write_text(
        "\n".join(
            [
                "# Merge Determinism Stress",
                "",
                f"- ok: {ok}",
                f"- agents: {args.agents}",
                f"- findings_per_agent: {args.findings_per_agent}",
                f"- iterations: {args.iterations}",
                f"- mismatch_count: {len(mismatches)}",
                f"- elapsed_ms: {payload['elapsed_ms']}",
            ]
        ),
        encoding="utf-8",
    )
    return 0 if ok else 2


if __name__ == "__main__":
    raise SystemExit(main())
