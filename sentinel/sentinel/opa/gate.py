"""OPA gate (monitor-only) for standalone policy checks."""

from __future__ import annotations

import json
import os
import time
import hashlib
from dataclasses import dataclass
from typing import Any, Dict, Optional
from urllib.error import URLError, HTTPError
from urllib.request import Request, urlopen


@dataclass
class OPAGateDecision:
    """OPA decision envelope."""
    status: str
    allow: bool
    reason: str
    error: Optional[str] = None
    latency_ms: float = 0.0
    policy: Optional[str] = None
    url: Optional[str] = None
    raw: Optional[Dict[str, Any]] = None

    def to_dict(self) -> Dict[str, Any]:
        return {
            "status": self.status,
            "allow": self.allow,
            "reason": self.reason,
            "error": self.error,
            "latency_ms": round(self.latency_ms, 2),
            "policy": self.policy,
            "url": self.url,
            "raw": self.raw,
        }


class OPAGate:
    """Evaluate policy via OPA HTTP API (fail-open, monitor-only)."""

    def __init__(
        self,
        url: Optional[str] = None,
        policy: Optional[str] = None,
        timeout_ms: Optional[int] = None,
        circuit_failures: Optional[int] = None,
        circuit_open_ms: Optional[int] = None,
        enabled: Optional[bool] = None,
    ):
        self.url = url or os.getenv("SENTINEL_OPA_URL")
        self.policy = policy or os.getenv("SENTINEL_OPA_POLICY", "sentinel/allow")
        self.timeout_ms = timeout_ms or int(os.getenv("SENTINEL_OPA_TIMEOUT_MS", "800"))
        self.circuit_failures = circuit_failures or int(os.getenv("SENTINEL_OPA_CIRCUIT_FAILS", "3"))
        self.circuit_open_ms = circuit_open_ms or int(os.getenv("SENTINEL_OPA_CIRCUIT_OPEN_MS", "30000"))
        self.cache_ttl_ms = int(os.getenv("SENTINEL_OPA_CACHE_TTL_MS", "60000"))
        self._consecutive_failures = 0
        self._circuit_open_until = 0.0
        self._cache: Dict[str, Dict[str, Any]] = {}
        if enabled is None:
            enabled = bool(self.url)
        self.enabled = enabled

    def _cache_key(self, input_data: Dict[str, Any]) -> str:
        digest = hashlib.sha256(
            json.dumps(input_data, sort_keys=True, default=str).encode("utf-8")
        ).hexdigest()
        return f"{self.policy}:{digest}"

    def _cache_get(self, key: str, now: float) -> Optional[Dict[str, Any]]:
        item = self._cache.get(key)
        if not item:
            return None
        ts = float(item.get("ts", 0.0))
        ttl_s = max(0.0, float(self.cache_ttl_ms) / 1000.0)
        if ttl_s and (now - ts) > ttl_s:
            self._cache.pop(key, None)
            return None
        return item

    def _cache_set(self, key: str, decision: OPAGateDecision, now: float) -> None:
        # Store only bounded fields; raw can be large.
        self._cache[key] = {
            "ts": now,
            "allow": bool(decision.allow),
            "reason": str(decision.reason),
        }

    def evaluate(self, input_data: Dict[str, Any]) -> OPAGateDecision:
        """Evaluate input against OPA policy."""
        if not self.enabled or not self.url:
            return OPAGateDecision(
                status="skipped",
                allow=True,
                reason="opa_disabled",
                policy=self.policy,
                url=self.url,
            )

        now = time.perf_counter()
        cache_key = None
        if isinstance(input_data, dict):
            try:
                cache_key = self._cache_key(input_data)
            except Exception:
                cache_key = None

        if self._circuit_open_until and now < self._circuit_open_until:
            # Circuit is open: fail-open without blocking on OPA.
            if cache_key:
                cached = self._cache_get(cache_key, now)
                if cached:
                    return OPAGateDecision(
                        status="cached",
                        allow=bool(cached.get("allow", True)),
                        reason="opa_cached_lkg",
                        error="OPA circuit open (recent failures); used cached decision",
                        latency_ms=0.0,
                        policy=self.policy,
                        url=self.url,
                        raw={"cache": {"reason": cached.get("reason"), "age_ms": round((now - float(cached.get("ts", 0.0))) * 1000, 2)}},
                    )
            return OPAGateDecision(
                status="error",
                allow=True,
                reason="opa_circuit_open",
                error="OPA circuit open (recent failures)",
                latency_ms=0.0,
                policy=self.policy,
                url=self.url,
            )

        if not isinstance(input_data, dict):
            return OPAGateDecision(
                status="error",
                allow=True,
                reason="invalid_input",
                error="OPA input must be a dict",
                policy=self.policy,
                url=self.url,
            )

        endpoint = self._build_endpoint(self.url, self.policy)
        payload = json.dumps({"input": input_data}).encode("utf-8")
        req = Request(
            endpoint,
            data=payload,
            headers={"Content-Type": "application/json"},
            method="POST",
        )

        start = time.perf_counter()
        try:
            with urlopen(req, timeout=self.timeout_ms / 1000.0) as resp:
                raw = json.loads(resp.read().decode("utf-8"))
        except (HTTPError, URLError, ValueError) as exc:
            self._consecutive_failures += 1
            if self.circuit_failures and self._consecutive_failures >= self.circuit_failures:
                self._circuit_open_until = time.perf_counter() + (self.circuit_open_ms / 1000.0)
            if cache_key:
                cached = self._cache_get(cache_key, time.perf_counter())
                if cached:
                    now2 = time.perf_counter()
                    return OPAGateDecision(
                        status="cached",
                        allow=bool(cached.get("allow", True)),
                        reason="opa_cached_lkg",
                        error=f"OPA unavailable ({exc}); used cached decision",
                        latency_ms=(now2 - start) * 1000,
                        policy=self.policy,
                        url=self.url,
                        raw={"cache": {"reason": cached.get("reason"), "age_ms": round((now2 - float(cached.get("ts", 0.0))) * 1000, 2)}},
                    )
            return OPAGateDecision(
                status="error",
                allow=True,
                reason="opa_unavailable",
                error=str(exc),
                latency_ms=(time.perf_counter() - start) * 1000,
                policy=self.policy,
                url=self.url,
            )

        # Success: close circuit and reset failure counters.
        self._consecutive_failures = 0
        self._circuit_open_until = 0.0

        allow, reason = self._interpret_result(raw)
        status = "allow" if allow else "deny"
        decision = OPAGateDecision(
            status=status,
            allow=allow,
            reason=reason,
            latency_ms=(time.perf_counter() - start) * 1000,
            policy=self.policy,
            url=self.url,
            raw=raw,
        )
        if cache_key:
            try:
                self._cache_set(cache_key, decision, time.perf_counter())
            except Exception:
                pass
        return decision

    @staticmethod
    def _build_endpoint(url: str, policy: str) -> str:
        base = url.rstrip("/")
        policy_path = policy.strip().strip("/")
        policy_path = policy_path.replace(".", "/")
        return f"{base}/v1/data/{policy_path}"

    @staticmethod
    def _interpret_result(payload: Dict[str, Any]) -> tuple[bool, str]:
        result = payload.get("result")
        if isinstance(result, bool):
            return result, "opa_boolean_result"
        if isinstance(result, dict):
            if "allow" in result:
                return bool(result.get("allow")), "opa_allow_field"
            if "decision" in result:
                return bool(result.get("decision")), "opa_decision_field"
        return True, "opa_unexpected_result"
