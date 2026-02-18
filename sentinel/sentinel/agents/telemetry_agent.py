"""Minimal telemetry agents for network flow analysis."""

from __future__ import annotations

import ipaddress
import time
from typing import Any, Dict, List, Optional

from .base import BaseAgent
from .state import AnalysisStage, Finding
from ..contracts.schemas import NetworkFlowFeaturesV1
from ..providers.base import AnalysisResult, ThreatLevel, Indicator
from ..threat_intel.enrichment import ThreatEnrichment, IOC, IOCType
from ..logging import get_logger

logger = get_logger(__name__)


class TelemetryRulesAgent(BaseAgent):
    """
    Minimal rules/heuristics agent for NetworkFlowFeaturesV1.
    
    Uses lightweight thresholds for:
    - Packets per second (pps)
    - Bytes per second (bps)
    - SYN/ACK ratio
    - Unique destination ports (if available)
    """

    def __init__(self) -> None:
        self._last = None

    @property
    def name(self) -> str:
        return "telemetry_rules"

    def should_run(self, state: Dict[str, Any]) -> bool:
        return bool(state.get("flow_features"))

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        features: Optional[NetworkFlowFeaturesV1] = state.get("flow_features")
        if not features:
            return {
                "errors": ["No flow features available for telemetry rules"],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        try:
            score = 0.0
            findings: List[Finding] = []
            indicators: List[Dict[str, str]] = []
            reasoning_steps: List[str] = []
            errors: List[str] = []

            pps = features.pps if features.pps is not None else features.flow_pkts_s
            bps = features.bps if features.bps is not None else features.flow_byts_s
            syn_ack_ratio = features.syn_ack_ratio
            if syn_ack_ratio is None:
                if features.ack_flag_cnt > 0:
                    syn_ack_ratio = features.syn_flag_cnt / max(1, features.ack_flag_cnt)

            raw_context = state.get("raw_context") or {}
            source_topic = str(raw_context.get("_source_topic") or "")
            runtime_profile = source_topic.startswith("telemetry.flow")

            if runtime_profile:
                pps_high = 100_000
                pps_medium = 20_000
                bps_high = 50 * 1024 * 1024
                bps_medium = 5 * 1024 * 1024
                syn_ack_threshold = 3
            else:
                pps_high = 1_000_000
                pps_medium = 200_000
                bps_high = 100 * 1024 * 1024
                bps_medium = 20 * 1024 * 1024
                syn_ack_threshold = 10

            # Runtime fallback: if duration-derived rates are zero but counters are present,
            # estimate over a conservative 1s window to avoid false negatives.
            used_rate_fallback = False
            if runtime_profile and features.flow_duration == 0 and (pps <= 0 and bps <= 0):
                fallback_pps = float(max(features.tot_fwd_pkts + features.tot_bwd_pkts, 0))
                fallback_bps = float(max(features.totlen_fwd_pkts + features.totlen_bwd_pkts, 0))
                if fallback_pps > 0 or fallback_bps > 0:
                    pps = fallback_pps
                    bps = fallback_bps
                    used_rate_fallback = True
                    reasoning_steps.append("Duration=0; using conservative 1s fallback rates")

            # Heuristic thresholds (runtime profile for live telemetry, default for offline/CIC features)
            if pps > pps_high:
                score += 0.6
                findings.append(Finding(
                    source=self.name,
                    severity="high",
                    description=f"High PPS detected: {pps:.0f}",
                    confidence=0.8,
                ))
                reasoning_steps.append(f"PPS > {pps_high:,} (high)")
            elif pps > pps_medium:
                score += 0.4
                findings.append(Finding(
                    source=self.name,
                    severity="medium",
                    description=f"Elevated PPS detected: {pps:.0f}",
                    confidence=0.7,
                ))
                reasoning_steps.append(f"PPS > {pps_medium:,} (medium)")

            if bps > bps_high:
                score += 0.4
                findings.append(Finding(
                    source=self.name,
                    severity="high",
                    description=f"High BPS detected: {bps:.0f}",
                    confidence=0.8,
                ))
                reasoning_steps.append(f"BPS > {bps_high / (1024*1024):.0f}MB/s (high)")
            elif bps > bps_medium:
                score += 0.2
                findings.append(Finding(
                    source=self.name,
                    severity="medium",
                    description=f"Elevated BPS detected: {bps:.0f}",
                    confidence=0.7,
                ))
                reasoning_steps.append(f"BPS > {bps_medium / (1024*1024):.0f}MB/s (medium)")

            if syn_ack_ratio is not None and syn_ack_ratio > syn_ack_threshold:
                score += 0.5
                findings.append(Finding(
                    source=self.name,
                    severity="medium",
                    description=f"High SYN/ACK ratio: {syn_ack_ratio:.2f}",
                    confidence=0.7,
                ))
                reasoning_steps.append(f"SYN/ACK ratio > {syn_ack_threshold} (suspicious)")

            # Flow duration edge case
            if features.flow_duration == 0:
                findings.append(Finding(
                    source=self.name,
                    severity="low",
                    description="Flow duration is zero; rates may be unreliable",
                    confidence=0.4,
                ))
                reasoning_steps.append("Duration=0 (degraded rates)")

            if features.unique_dst_ports_batch and features.unique_dst_ports_batch > 500:
                score += 0.4
                findings.append(Finding(
                    source=self.name,
                    severity="high",
                    description=f"High unique destination ports: {features.unique_dst_ports_batch}",
                    confidence=0.75,
                ))
                reasoning_steps.append("Unique dst ports > 500 (scan)")
            elif features.unique_dst_ports_batch and features.unique_dst_ports_batch > 200:
                score += 0.2
                findings.append(Finding(
                    source=self.name,
                    severity="medium",
                    description=f"Elevated unique destination ports: {features.unique_dst_ports_batch}",
                    confidence=0.65,
                ))
                reasoning_steps.append("Unique dst ports > 200 (suspicious)")

            score = min(score, 1.0)
            if score >= 0.7:
                threat_level = ThreatLevel.MALICIOUS
            elif score >= 0.4:
                threat_level = ThreatLevel.SUSPICIOUS
            else:
                threat_level = ThreatLevel.CLEAN

            confidence = min(0.9, 0.5 + score * 0.6)

            if threat_level != ThreatLevel.CLEAN:
                indicators.append({"type": "ip", "value": features.src_ip, "context": "src_ip"})
                indicators.append({"type": "ip", "value": features.dst_ip, "context": "dst_ip"})

            result = AnalysisResult(
                provider_name=self.name,
                provider_version="1.0.0",
                threat_level=threat_level,
                score=score,
                confidence=confidence,
                findings=[f.description for f in findings],
                indicators=[
                    Indicator(type=i["type"], value=i["value"], context=i["context"])
                    for i in indicators
                ],
                latency_ms=(time.perf_counter() - t0) * 1000,
                metadata={
                    "pps": pps,
                    "bps": bps,
                    "syn_ack_ratio": syn_ack_ratio,
                    "unique_dst_ports_batch": features.unique_dst_ports_batch,
                    "runtime_profile": runtime_profile,
                    "rate_fallback_used": used_rate_fallback,
                },
            )

            updates = {
                "static_results": [result],
                "findings": findings,
                "indicators": indicators,
                "reasoning_steps": reasoning_steps,
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
                "errors": errors,
            }
            if features.flow_duration == 0:
                updates["metadata"] = {
                    "degraded": True,
                    "degraded_reasons": ["zero_duration"],
                }

            self._log_complete(updates)
            return updates
        except Exception as exc:  # pylint: disable=broad-except
            logger.error(f"Telemetry rules failed: {exc}")
            return {
                "errors": [f"Telemetry rules failed: {str(exc)}"],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
                "metadata": {"telemetry_rules_error": str(exc)},
            }


class TelemetryThreatIntelAgent(BaseAgent):
    """Threat intel enrichment for telemetry IPs (optional)."""

    def __init__(self, enable_vt: bool = False):
        self._enricher = None
        self._enable_vt = enable_vt

    @property
    def name(self) -> str:
        return "telemetry_threat_intel"

    def _get_enricher(self) -> ThreatEnrichment:
        if self._enricher is None:
            self._enricher = ThreatEnrichment(enable_vt=self._enable_vt)
        return self._enricher

    def should_run(self, state: Dict[str, Any]) -> bool:
        features: Optional[NetworkFlowFeaturesV1] = state.get("flow_features")
        if not features:
            return False
        return bool(features.src_ip or features.dst_ip)

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        features: Optional[NetworkFlowFeaturesV1] = state.get("flow_features")
        if not features:
            return {
                "errors": ["No flow features available for threat intel"],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        src_ip = features.src_ip
        dst_ip = features.dst_ip

        iocs: List[IOC] = []
        for ip in (src_ip, dst_ip):
            if ip and _is_public_ip(ip):
                iocs.append(IOC(type=IOCType.IP, value=ip))

        if not iocs:
            return {
                "reasoning_steps": ["Threat intel skipped: no public IPs"],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
                "metadata": {"threat_intel_skipped": "no_public_ips"},
            }

        try:
            result = self._get_enricher().enrich_iocs(iocs)
        except Exception as exc:  # pylint: disable=broad-except
            logger.error(f"Telemetry threat intel failed: {exc}")
            return {
                "errors": [f"Threat intel failed: {str(exc)}"],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
                "metadata": {"threat_intel_error": str(exc)},
            }

        findings: List[Finding] = []
        indicators: List[Dict[str, str]] = []
        reasoning_steps: List[str] = []

        for ip_intel in result.ip_intel:
            if ip_intel.is_malicious:
                findings.append(Finding(
                    source=self.name,
                    severity="high",
                    description=f"Malicious IP: {ip_intel.ip}",
                    confidence=0.85,
                ))
                indicators.append({
                    "type": "malicious_ip",
                    "value": ip_intel.ip,
                    "context": "threat_intel",
                })

        if result.threat_score > 0:
            reasoning_steps.append(
                f"Threat intel score: {result.threat_score:.2f} "
                f"(sources={len(result.sources_matched)})"
            )

        elapsed = (time.perf_counter() - t0) * 1000

        updates = {
            "findings": findings,
            "indicators": indicators,
            "reasoning_steps": reasoning_steps,
            "threat_intel_result": result.to_dict(),
            "threat_intel_score": result.threat_score,
            "metadata": {"threat_intel_time_ms": elapsed},
            "current_stage": AnalysisStage.DEEP_ANALYSIS,
        }

        self._log_complete(updates)
        return updates


def _is_public_ip(value: str) -> bool:
    try:
        ip_obj = ipaddress.ip_address(value)
    except ValueError:
        return False
    return not (ip_obj.is_private or ip_obj.is_loopback or ip_obj.is_reserved or ip_obj.is_multicast)
