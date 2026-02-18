"""Degraded-mode rules and helpers."""

from __future__ import annotations

from typing import Dict, Iterable, List

DEGRADED_REASONS: Dict[str, str] = {
    "fast_path_unavailable": "Fast path router not initialized",
    "yara_unavailable": "YARA engine unavailable",
    "hash_db_unavailable": "Hash lookup database unavailable",
    "signatures_unavailable": "Signature matcher unavailable or empty",
    "models_path_missing": "Models path not available; ML providers disabled",
    "ml_models_unavailable": "ML providers not loaded",
    "threat_intel_failed": "Threat intel enrichment failed",
    "zero_duration": "Flow duration is zero; derived rates may be unreliable",
    "agent_error": "One or more agents returned errors",
    "unsupported_filetype": "File type detected but parser not implemented",
}


def merge_degraded_reasons(
    base: Iterable[str] | None,
    extra: Iterable[str] | None,
) -> List[str]:
    """Merge and dedupe degraded reasons while preserving order."""
    merged: List[str] = []
    for reason in list(base or []) + list(extra or []):
        if reason in DEGRADED_REASONS and reason not in merged:
            merged.append(reason)
    return merged


def build_degraded_metadata(reasons: Iterable[str] | None) -> Dict[str, object]:
    """Build degraded metadata block."""
    reasons_list = merge_degraded_reasons(reasons, [])
    return {
        "degraded": bool(reasons_list),
        "degraded_reasons": reasons_list,
    }
