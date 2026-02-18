"""Utilities for merging parallel agent outputs safely."""

from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor, as_completed, TimeoutError, wait
from dataclasses import asdict, is_dataclass
import time
from typing import Any, Dict, Iterable, List, Tuple

from .base import BaseAgent
from .degraded import merge_degraded_reasons
from ..utils.timeouts import resolve_timeout_seconds

LIST_FIELDS = (
    "static_results",
    "ml_results",
    "llm_results",
    "findings",
    "indicators",
    "reasoning_steps",
    "stages_completed",
    "errors",
    "error_codes",
)

BOOL_OR_FIELDS = ("needs_llm_reasoning",)

SCALAR_PASSTHROUGH = (
    "threat_intel_result",
    "threat_intel_score",
)


def run_parallel_agents(
    state: Dict[str, Any],
    agents: Iterable[BaseAgent],
    max_workers: int | None = None,
    timeout_seconds: float | None = None,
) -> Dict[str, Any]:
    """Run eligible agents in parallel and merge their updates."""
    eligible: List[BaseAgent] = []
    for agent in agents:
        if agent is None:
            continue
        if hasattr(agent, "should_run") and not agent.should_run(state):
            continue
        eligible.append(agent)

    if not eligible:
        return {}

    agent_updates: List[Tuple[str, Dict[str, Any]]] = []
    start_times: Dict[str, float] = {}
    timeout = resolve_timeout_seconds(timeout_seconds)

    with ThreadPoolExecutor(max_workers=max_workers or len(eligible)) as executor:
        future_map = {}
        for agent in eligible:
            start_times[agent.name] = time.perf_counter()
            future_map[executor.submit(agent, state)] = agent
        if timeout:
            done, not_done = wait(future_map, timeout=timeout)
            for future in done:
                agent = future_map[future]
                try:
                    update = future.result() or {}
                except Exception as exc:  # pylint: disable=broad-except
                    update = {
                        "errors": [f"Agent {agent.name} failed: {exc}"],
                        "metadata": {"error": str(exc)},
                    }
                elapsed = (time.perf_counter() - start_times.get(agent.name, time.perf_counter())) * 1000
                update["_agent_meta"] = {
                    "time_ms": round(elapsed, 2),
                }
                if update.get("errors"):
                    update["_agent_meta"]["error"] = "; ".join(update.get("errors", []))
                agent_updates.append((agent.name, update))
            for future in not_done:
                agent = future_map[future]
                future.cancel()
                elapsed = timeout * 1000
                update = {
                    "errors": [f"Agent {agent.name} timed out after {timeout:.1f}s"],
                    "metadata": {"error": "timeout"},
                    "_agent_meta": {"time_ms": round(elapsed, 2), "error": "timeout"},
                }
                agent_updates.append((agent.name, update))
        else:
            for future in as_completed(future_map):
                agent = future_map[future]
                try:
                    update = future.result() or {}
                except Exception as exc:  # pylint: disable=broad-except
                    update = {
                        "errors": [f"Agent {agent.name} failed: {exc}"],
                        "metadata": {"error": str(exc)},
                    }
                elapsed = (time.perf_counter() - start_times.get(agent.name, time.perf_counter())) * 1000
                update["_agent_meta"] = {
                    "time_ms": round(elapsed, 2),
                }
                if update.get("errors"):
                    update["_agent_meta"]["error"] = "; ".join(update.get("errors", []))
                agent_updates.append((agent.name, update))

    # Stable merge order for deterministic output
    agent_updates.sort(key=lambda x: x[0])
    return merge_agent_updates(agent_updates)


def run_sequential_agents(
    state: Dict[str, Any],
    agents: Iterable[BaseAgent],
    timeout_seconds: float | None = None,
) -> Dict[str, Any]:
    """Run eligible agents sequentially and merge their updates."""
    eligible: List[BaseAgent] = []
    for agent in agents:
        if agent is None:
            continue
        if hasattr(agent, "should_run") and not agent.should_run(state):
            continue
        eligible.append(agent)

    if not eligible:
        return {}

    agent_updates: List[Tuple[str, Dict[str, Any]]] = []
    timeout = resolve_timeout_seconds(timeout_seconds)

    for agent in eligible:
        start = time.perf_counter()
        try:
            if timeout:
                with ThreadPoolExecutor(max_workers=1) as executor:
                    future = executor.submit(agent, state)
                    update = future.result(timeout=timeout) or {}
            else:
                update = agent(state) or {}
        except TimeoutError:
            update = {
                "errors": [f"Agent {agent.name} timed out after {timeout:.1f}s"],
                "metadata": {"error": "timeout"},
            }
        except Exception as exc:  # pylint: disable=broad-except
            update = {
                "errors": [f"Agent {agent.name} failed: {exc}"],
                "metadata": {"error": str(exc)},
            }
        elapsed = (time.perf_counter() - start) * 1000
        update["_agent_meta"] = {
            "time_ms": round(elapsed, 2),
        }
        if update.get("errors"):
            update["_agent_meta"]["error"] = "; ".join(update.get("errors", []))
        agent_updates.append((agent.name, update))

    # Stable merge order for deterministic output
    agent_updates.sort(key=lambda x: x[0])
    return merge_agent_updates(agent_updates)


def merge_agent_updates(
    agent_updates: List[Tuple[str, Dict[str, Any]]],
) -> Dict[str, Any]:
    """Merge updates from multiple agents with dedupe and safe metadata handling."""
    merged: Dict[str, Any] = {
        "metadata": {"agents": {}},
        "needs_llm_reasoning": False,
    }

    for agent_name, update in agent_updates:
        if not isinstance(update, dict):
            update = {"errors": [f"Agent {agent_name} returned non-dict update"]}
        agent_meta = update.pop("_agent_meta", {}) or {}

        # List fields: extend
        for key in LIST_FIELDS:
            values = update.get(key)
            if values:
                merged.setdefault(key, [])
                merged[key].extend(values)

        # Boolean OR fields
        for key in BOOL_OR_FIELDS:
            if update.get(key):
                merged[key] = True

        # Scalar passthrough (only set once)
        for key in SCALAR_PASSTHROUGH:
            if key in update and key not in merged:
                merged[key] = update[key]

        # Preserve per-agent metadata without collisions
        meta = update.get("metadata")
        agent_bucket = merged["metadata"]["agents"].setdefault(agent_name, {})
        if agent_meta:
            agent_bucket.update(agent_meta)
        if meta:
            agent_bucket["details"] = meta
            for key, value in meta.items():
                if key == "agents":
                    continue
                merged["metadata"][key] = value

        # Degraded: any agent error
        if update.get("errors"):
            current = merged["metadata"].get("degraded_reasons", [])
            merged["metadata"]["degraded_reasons"] = merge_degraded_reasons(
                current,
                ["agent_error"],
            )
            merged["metadata"]["degraded"] = True

        # Degraded: unsupported file type
        if update.get("errors") and any(
            isinstance(e, str) and e.startswith("ERR_UNSUPPORTED_FILETYPE:")
            for e in (update.get("errors") or [])
        ):
            current = merged["metadata"].get("degraded_reasons", [])
            merged["metadata"]["degraded_reasons"] = merge_degraded_reasons(
                current,
                ["unsupported_filetype"],
            )
            merged["metadata"]["degraded"] = True

        # Degraded: threat intel failure
        if "threat_intel" in agent_name and update.get("errors"):
            current = merged["metadata"].get("degraded_reasons", [])
            merged["metadata"]["degraded_reasons"] = merge_degraded_reasons(
                current,
                ["threat_intel_failed"],
            )
            merged["metadata"]["degraded"] = True

    # Dedupe findings and indicators
    if "findings" in merged:
        merged["findings"] = _dedupe_findings(merged["findings"])
    if "indicators" in merged:
        merged["indicators"] = _dedupe_indicators(merged["indicators"])

    return merged


def _dedupe_findings(findings: List[Any]) -> List[Any]:
    """
    Dedupe findings by description with conflict resolution.

    Conflict resolution: prefer highest severity, then highest confidence.
    Preserves sources in metadata for merged findings.
    """
    order = {"low": 0, "medium": 1, "high": 2, "critical": 3}
    buckets: Dict[Any, Any] = {}

    for finding in findings:
        source, severity, description, confidence, meta, is_dataclass_item = _extract_finding(finding)
        key = description or _finding_key(finding)

        existing = buckets.get(key)
        if not existing:
            _merge_finding_meta(finding, source, severity, meta, is_dataclass_item)
            buckets[key] = finding
            continue

        ex_source, ex_sev, ex_desc, ex_conf, ex_meta, ex_is_dataclass = _extract_finding(existing)
        current_rank = order.get(ex_sev, 0)
        incoming_rank = order.get(severity, 0)
        should_replace = incoming_rank > current_rank or (
            incoming_rank == current_rank and (confidence or 0.0) > (ex_conf or 0.0)
        )

        if should_replace:
            _merge_finding_meta(finding, source, severity, meta, is_dataclass_item, existing=existing)
            buckets[key] = finding
        else:
            _merge_finding_meta(existing, source, severity, ex_meta, ex_is_dataclass, existing=finding)

    # Deterministic ordering: dict insertion order depends on first-seen order of keys.
    # First-seen can vary if upstream agents build findings from sets/dicts.
    # Sort by stable attributes to keep output deterministic across parallel runs.
    deduped = list(buckets.values())
    deduped.sort(key=_finding_sort_key)
    return deduped


def _finding_key(finding: Any) -> Tuple[str, str, str]:
    if is_dataclass(finding):
        finding = asdict(finding)
    if isinstance(finding, dict):
        return (
            str(finding.get("source", "")),
            str(finding.get("severity", "")),
            str(finding.get("description", "")),
        )
    # Object with attributes
    source = getattr(finding, "source", "")
    severity = getattr(finding, "severity", "")
    description = getattr(finding, "description", "")
    if source or severity or description:
        return (str(source), str(severity), str(description))
    return ("", "", str(finding))


def _extract_finding(finding: Any) -> tuple[str, str, str, float, Dict[str, Any], bool]:
    if is_dataclass(finding):
        return (
            str(getattr(finding, "source", "")),
            str(getattr(finding, "severity", "")),
            str(getattr(finding, "description", "")),
            float(getattr(finding, "confidence", 0.0) or 0.0),
            getattr(finding, "metadata", {}) or {},
            True,
        )
    if isinstance(finding, dict):
        meta = finding.get("metadata") or {}
        return (
            str(finding.get("source", "")),
            str(finding.get("severity", "")),
            str(finding.get("description", "")),
            float(finding.get("confidence", 0.0) or 0.0),
            meta,
            False,
        )
    source = getattr(finding, "source", "")
    severity = getattr(finding, "severity", "")
    description = getattr(finding, "description", "")
    confidence = getattr(finding, "confidence", 0.0)
    meta = getattr(finding, "metadata", {}) or {}
    return (str(source), str(severity), str(description), float(confidence or 0.0), meta, False)


def _merge_finding_meta(
    target: Any,
    source: str,
    severity: str,
    meta: Dict[str, Any],
    is_dataclass_item: bool,
    existing: Any | None = None,
) -> None:
    if not source and not severity:
        return
    if is_dataclass_item:
        meta_dict = meta
    elif isinstance(target, dict):
        meta_dict = target.setdefault("metadata", {})
    else:
        meta_dict = meta

    sources = meta_dict.get("sources", [])
    if source:
        if source not in sources:
            sources.append(source)
    if existing is not None:
        ex_source, ex_sev, _, _, _, _ = _extract_finding(existing)
        if ex_source and ex_source not in sources:
            sources.append(ex_source)
    meta_dict["sources"] = sources

    sev_list = meta_dict.get("severity_conflicts", [])
    if severity and severity not in sev_list:
        sev_list.append(severity)
    if existing is not None:
        _, ex_sev, _, _, _, _ = _extract_finding(existing)
        if ex_sev and ex_sev not in sev_list:
            sev_list.append(ex_sev)
    meta_dict["severity_conflicts"] = sev_list

    # Deterministic ordering for metadata lists (prevents flakiness under parallelism).
    try:
        meta_dict["sources"] = sorted(set(meta_dict.get("sources", [])))
    except Exception:
        pass
    try:
        order = {"low": 0, "medium": 1, "high": 2, "critical": 3}
        uniq = sorted(set(meta_dict.get("severity_conflicts", [])), key=lambda s: order.get(str(s), 0))
        meta_dict["severity_conflicts"] = uniq
    except Exception:
        pass

    if is_dataclass_item:
        target.metadata = meta_dict


def _dedupe_indicators(indicators: List[Any]) -> List[Any]:
    """Dedupe indicators by (type, value, context) with fallback to string."""
    seen = set()
    result = []
    for indicator in indicators:
        key = _indicator_key(indicator)
        if key in seen:
            continue
        seen.add(key)
        result.append(indicator)
    # Deterministic ordering.
    result.sort(key=_indicator_key)
    return result


def _finding_sort_key(finding: Any) -> Tuple[str, str, str, float]:
    """Stable sort key for findings."""
    source, severity, description, confidence, _, _ = _extract_finding(finding)
    # Normalize confidence to a stable float.
    try:
        conf = float(confidence or 0.0)
    except Exception:
        conf = 0.0
    return (str(description or ""), str(source or ""), str(severity or ""), -conf)


def _indicator_key(indicator: Any) -> Tuple[str, str, str]:
    if isinstance(indicator, dict):
        return (
            str(indicator.get("type", "")),
            str(indicator.get("value", "")),
            str(indicator.get("context", "")),
        )
    ind_type = getattr(indicator, "type", "")
    value = getattr(indicator, "value", "")
    context = getattr(indicator, "context", "")
    if ind_type or value or context:
        return (str(ind_type), str(value), str(context))
    return ("", "", str(indicator))
