"""DNS modality - schema, adapter, and agent for DNS-based threat detection."""

import math
import re
import uuid
import time
from abc import ABC, abstractmethod
from collections import Counter
from dataclasses import dataclass, field
from typing import Dict, List, Any, Optional

from .detection_agent import DetectionAgent
from .contracts import (
    DetectionCandidate,
    CanonicalEvent,
    Modality,
    ThreatType,
)
from ...logging import get_logger

logger = get_logger(__name__)


# =============================================================================
# DNS Canonical Schema
# =============================================================================

@dataclass
class DNSFeaturesV1:
    """
    Canonical schema for DNS query/response features.
    
    Designed for C2 detection, DGA detection, and DNS tunneling analysis.
    """
    # Query info
    query_name: str
    query_type: str  # A, AAAA, TXT, MX, CNAME, etc.
    query_class: str = "IN"
    
    # Response info
    response_code: str = "NOERROR"  # NOERROR, NXDOMAIN, SERVFAIL, etc.
    response_ips: List[str] = field(default_factory=list)
    ttl: int = 0
    
    # Timing
    timestamp: float = 0.0
    response_time_ms: float = 0.0
    
    # Client/Server
    client_ip: str = ""
    server_ip: str = ""
    
    # Domain analysis features
    domain_length: int = 0
    subdomain_count: int = 0
    label_count: int = 0
    max_label_length: int = 0
    avg_label_length: float = 0.0
    
    # Character analysis
    digit_ratio: float = 0.0
    uppercase_ratio: float = 0.0
    special_char_ratio: float = 0.0
    consonant_ratio: float = 0.0
    vowel_ratio: float = 0.0
    
    # Entropy
    domain_entropy: float = 0.0
    
    # TLD info
    tld: str = ""
    registered_domain: str = ""
    
    # Flags
    is_dnssec: bool = False
    is_recursive: bool = True
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "query_name": self.query_name,
            "query_type": self.query_type,
            "query_class": self.query_class,
            "response_code": self.response_code,
            "response_ips": self.response_ips,
            "ttl": self.ttl,
            "timestamp": self.timestamp,
            "response_time_ms": self.response_time_ms,
            "client_ip": self.client_ip,
            "server_ip": self.server_ip,
            "domain_length": self.domain_length,
            "subdomain_count": self.subdomain_count,
            "label_count": self.label_count,
            "max_label_length": self.max_label_length,
            "avg_label_length": self.avg_label_length,
            "digit_ratio": self.digit_ratio,
            "uppercase_ratio": self.uppercase_ratio,
            "special_char_ratio": self.special_char_ratio,
            "consonant_ratio": self.consonant_ratio,
            "vowel_ratio": self.vowel_ratio,
            "domain_entropy": self.domain_entropy,
            "tld": self.tld,
            "registered_domain": self.registered_domain,
            "is_dnssec": self.is_dnssec,
            "is_recursive": self.is_recursive,
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "DNSFeaturesV1":
        return cls(**{k: v for k, v in data.items() if k in cls.__dataclass_fields__})


# =============================================================================
# DNS Adapter
# =============================================================================

class DNSAdapter:
    """
    Adapter for converting raw DNS logs to DNSFeaturesV1.
    
    Supports common DNS log formats and computes derived features
    for DGA and C2 detection.
    """
    
    VOWELS = set('aeiouAEIOU')
    CONSONANTS = set('bcdfghjklmnpqrstvwxyzBCDFGHJKLMNPQRSTVWXYZ')
    
    @property
    def adapter_id(self) -> str:
        return "dns_adapter"
    
    @property
    def source_format(self) -> str:
        return "DNS Logs"
    
    def to_dns_features(self, row: Dict[str, Any]) -> DNSFeaturesV1:
        """
        Convert raw DNS log entry to DNSFeaturesV1.
        
        Args:
            row: Raw DNS log record
            
        Returns:
            DNSFeaturesV1 instance
        """
        # Normalize keys
        normalized = {k.lower().strip(): v for k, v in row.items()}
        
        # Extract query name
        query_name = str(normalized.get('query_name', normalized.get('query', normalized.get('domain', ''))))
        query_name = query_name.rstrip('.').lower()
        
        if not query_name:
            raise ValueError("query_name is required")
        
        # Query type
        query_type = str(normalized.get('query_type', normalized.get('type', normalized.get('qtype', 'A')))).upper()
        
        # Response info
        response_code = str(normalized.get('response_code', normalized.get('rcode', 'NOERROR'))).upper()
        response_ips = normalized.get('response_ips', normalized.get('answers', []))
        if isinstance(response_ips, str):
            response_ips = [response_ips] if response_ips else []
        
        ttl = self._safe_int(normalized.get('ttl', 0))
        
        # Timing
        timestamp = self._safe_float(normalized.get('timestamp', time.time()))
        response_time_ms = self._safe_float(normalized.get('response_time_ms', normalized.get('latency_ms', 0)))
        
        # Client/Server
        client_ip = str(normalized.get('client_ip', normalized.get('src_ip', '')))
        server_ip = str(normalized.get('server_ip', normalized.get('dst_ip', '')))
        
        # Compute domain analysis features
        domain_features = self._analyze_domain(query_name)
        
        # Flags
        is_dnssec = bool(normalized.get('is_dnssec', normalized.get('dnssec', False)))
        is_recursive = bool(normalized.get('is_recursive', normalized.get('rd', True)))
        
        return DNSFeaturesV1(
            query_name=query_name,
            query_type=query_type,
            query_class=str(normalized.get('query_class', 'IN')),
            response_code=response_code,
            response_ips=response_ips,
            ttl=ttl,
            timestamp=timestamp,
            response_time_ms=response_time_ms,
            client_ip=client_ip,
            server_ip=server_ip,
            is_dnssec=is_dnssec,
            is_recursive=is_recursive,
            **domain_features,
        )
    
    def _analyze_domain(self, domain: str) -> Dict[str, Any]:
        """Compute domain analysis features."""
        labels = domain.split('.')
        
        # Basic structure
        domain_length = len(domain)
        label_count = len(labels)
        subdomain_count = max(0, label_count - 2)  # Exclude registered domain and TLD
        
        # Label lengths
        label_lengths = [len(l) for l in labels]
        max_label_length = max(label_lengths) if label_lengths else 0
        avg_label_length = sum(label_lengths) / len(label_lengths) if label_lengths else 0.0
        
        # Character analysis (excluding dots)
        chars = domain.replace('.', '')
        char_count = len(chars)
        
        if char_count > 0:
            digit_count = sum(1 for c in chars if c.isdigit())
            upper_count = sum(1 for c in chars if c.isupper())
            special_count = sum(1 for c in chars if not c.isalnum())
            vowel_count = sum(1 for c in chars if c in self.VOWELS)
            consonant_count = sum(1 for c in chars if c in self.CONSONANTS)
            
            digit_ratio = digit_count / char_count
            uppercase_ratio = upper_count / char_count
            special_char_ratio = special_count / char_count
            vowel_ratio = vowel_count / char_count
            consonant_ratio = consonant_count / char_count
        else:
            digit_ratio = uppercase_ratio = special_char_ratio = 0.0
            vowel_ratio = consonant_ratio = 0.0
        
        # Entropy
        domain_entropy = self._compute_entropy(chars)
        
        # TLD extraction
        tld = labels[-1] if labels else ""
        registered_domain = '.'.join(labels[-2:]) if len(labels) >= 2 else domain
        
        return {
            'domain_length': domain_length,
            'subdomain_count': subdomain_count,
            'label_count': label_count,
            'max_label_length': max_label_length,
            'avg_label_length': avg_label_length,
            'digit_ratio': digit_ratio,
            'uppercase_ratio': uppercase_ratio,
            'special_char_ratio': special_char_ratio,
            'vowel_ratio': vowel_ratio,
            'consonant_ratio': consonant_ratio,
            'domain_entropy': domain_entropy,
            'tld': tld,
            'registered_domain': registered_domain,
        }
    
    def _compute_entropy(self, s: str) -> float:
        """Compute Shannon entropy of a string."""
        if not s:
            return 0.0
        
        freq = Counter(s)
        length = len(s)
        entropy = 0.0
        
        for count in freq.values():
            p = count / length
            if p > 0:
                entropy -= p * math.log2(p)
        
        return entropy
    
    def to_canonical_event(
        self,
        row: Dict[str, Any],
        tenant_id: str,
        source: Optional[str] = None,
    ) -> CanonicalEvent:
        """Convert raw DNS log to CanonicalEvent."""
        features = self.to_dns_features(row)
        
        return CanonicalEvent(
            id=str(uuid.uuid4()),
            timestamp=features.timestamp or time.time(),
            source=source or self.adapter_id,
            tenant_id=tenant_id,
            modality=Modality.DNS,
            features_version="DNSFeaturesV1",
            features=features.to_dict(),
            raw_context={"original_row": row},
        )
    
    @staticmethod
    def _safe_float(value: Any, default: float = 0.0) -> float:
        if value is None:
            return default
        try:
            f = float(value)
            return default if math.isnan(f) or math.isinf(f) else f
        except (ValueError, TypeError):
            return default
    
    @staticmethod
    def _safe_int(value: Any, default: int = 0) -> int:
        if value is None:
            return default
        try:
            return int(float(value))
        except (ValueError, TypeError):
            return default


# =============================================================================
# DNS Agent
# =============================================================================

class DNSAgent(DetectionAgent):
    """
    DNS threat detection agent for C2 and DGA detection.
    
    Uses heuristics based on domain features to identify:
    - Domain Generation Algorithm (DGA) domains
    - Command & Control (C2) communication
    - DNS tunneling attempts
    """
    
    # DGA detection thresholds
    DGA_ENTROPY_THRESHOLD = 3.5
    DGA_LENGTH_THRESHOLD = 20
    DGA_DIGIT_RATIO_THRESHOLD = 0.3
    DGA_CONSONANT_VOWEL_RATIO_THRESHOLD = 4.0
    
    # Suspicious query types for tunneling
    TUNNEL_QUERY_TYPES = {'TXT', 'NULL', 'CNAME', 'MX'}
    
    def __init__(self, config: Optional[Dict[str, Any]] = None):
        self._config = config or {}
        self._entropy_threshold = self._config.get('entropy_threshold', self.DGA_ENTROPY_THRESHOLD)
        self._length_threshold = self._config.get('length_threshold', self.DGA_LENGTH_THRESHOLD)
    
    @property
    def agent_id(self) -> str:
        return "cybermesh.dns"
    
    def input_modalities(self) -> List[Modality]:
        return [Modality.DNS]
    
    def analyze(self, event: CanonicalEvent) -> List[DetectionCandidate]:
        """
        Analyze DNS event for threats.
        
        Args:
            event: CanonicalEvent with modality=DNS
            
        Returns:
            List of DetectionCandidate objects
        """
        if event.modality != Modality.DNS:
            raise ValueError(f"DNSAgent only supports DNS modality, got {event.modality.value}")
        
        if event.features_version != "DNSFeaturesV1":
            raise ValueError(f"DNSAgent requires DNSFeaturesV1, got {event.features_version}")
        
        features = event.features
        candidates = []
        
        # Check for DGA indicators
        dga_result = self._check_dga(features)
        if dga_result:
            candidates.append(dga_result)
        
        # Check for DNS tunneling indicators
        tunnel_result = self._check_tunneling(features)
        if tunnel_result:
            candidates.append(tunnel_result)
        
        # Check for C2 indicators
        c2_result = self._check_c2(features)
        if c2_result:
            candidates.append(c2_result)
        
        return candidates
    
    def _check_dga(self, features: Dict[str, Any]) -> Optional[DetectionCandidate]:
        """Check for DGA domain indicators."""
        score = 0.0
        findings = []
        
        # High entropy
        entropy = features.get('domain_entropy', 0)
        if entropy > self._entropy_threshold:
            score += 0.3
            findings.append(f"High entropy: {entropy:.2f}")
        
        # Long domain
        length = features.get('domain_length', 0)
        if length > self._length_threshold:
            score += 0.2
            findings.append(f"Long domain: {length} chars")
        
        # High digit ratio
        digit_ratio = features.get('digit_ratio', 0)
        if digit_ratio > self.DGA_DIGIT_RATIO_THRESHOLD:
            score += 0.2
            findings.append(f"High digit ratio: {digit_ratio:.2f}")
        
        # Unusual consonant/vowel ratio
        consonant_ratio = features.get('consonant_ratio', 0)
        vowel_ratio = features.get('vowel_ratio', 0.001)
        cv_ratio = consonant_ratio / vowel_ratio if vowel_ratio > 0 else 0
        if cv_ratio > self.DGA_CONSONANT_VOWEL_RATIO_THRESHOLD:
            score += 0.2
            findings.append(f"Unusual consonant/vowel ratio: {cv_ratio:.2f}")
        
        # Many subdomains
        subdomain_count = features.get('subdomain_count', 0)
        if subdomain_count > 3:
            score += 0.1
            findings.append(f"Many subdomains: {subdomain_count}")
        
        if score >= 0.4:
            return DetectionCandidate(
                agent_id=self.agent_id,
                signal_id="dga_detector",
                threat_type=ThreatType.C2_COMMUNICATION,
                raw_score=min(score, 1.0),
                calibrated_score=min(score, 1.0),
                confidence=min(score + 0.2, 1.0),
                features={
                    'entropy': entropy,
                    'domain_length': length,
                    'digit_ratio': digit_ratio,
                },
                findings=findings,
                indicators=[{
                    'type': 'domain',
                    'value': features.get('query_name', ''),
                    'tags': ['dga', 'suspicious'],
                }],
                metadata={'detection_type': 'dga'},
            )
        
        return None
    
    def _check_tunneling(self, features: Dict[str, Any]) -> Optional[DetectionCandidate]:
        """Check for DNS tunneling indicators."""
        score = 0.0
        findings = []
        
        # Suspicious query type
        query_type = features.get('query_type', 'A')
        if query_type in self.TUNNEL_QUERY_TYPES:
            score += 0.3
            findings.append(f"Suspicious query type: {query_type}")
        
        # Very long subdomain (potential encoded data)
        max_label = features.get('max_label_length', 0)
        if max_label > 50:
            score += 0.4
            findings.append(f"Very long label: {max_label} chars")
        
        # High entropy in long domain
        entropy = features.get('domain_entropy', 0)
        length = features.get('domain_length', 0)
        if entropy > 4.0 and length > 30:
            score += 0.3
            findings.append(f"High entropy long domain: {entropy:.2f}")
        
        if score >= 0.4:
            return DetectionCandidate(
                agent_id=self.agent_id,
                signal_id="tunnel_detector",
                threat_type=ThreatType.POLICY_VIOLATION,
                raw_score=min(score, 1.0),
                calibrated_score=min(score, 1.0),
                confidence=min(score + 0.1, 1.0),
                features={
                    'query_type': query_type,
                    'max_label_length': max_label,
                    'entropy': entropy,
                },
                findings=findings,
                indicators=[{
                    'type': 'domain',
                    'value': features.get('query_name', ''),
                    'tags': ['tunneling', 'data_exfiltration'],
                }],
                metadata={'detection_type': 'tunneling'},
            )
        
        return None
    
    def _check_c2(self, features: Dict[str, Any]) -> Optional[DetectionCandidate]:
        """Check for C2 communication indicators."""
        score = 0.0
        findings = []
        
        # NXDOMAIN with DGA-like domain
        response_code = features.get('response_code', '')
        entropy = features.get('domain_entropy', 0)
        
        if response_code == 'NXDOMAIN' and entropy > 3.0:
            score += 0.4
            findings.append(f"NXDOMAIN with high entropy: {entropy:.2f}")
        
        # Very low TTL (potential fast-flux)
        ttl = features.get('ttl', 0)
        if 0 < ttl < 60:
            score += 0.2
            findings.append(f"Very low TTL: {ttl}s")
        
        # Multiple response IPs (fast-flux indicator)
        response_ips = features.get('response_ips', [])
        if len(response_ips) > 5:
            score += 0.2
            findings.append(f"Many response IPs: {len(response_ips)}")
        
        if score >= 0.4:
            return DetectionCandidate(
                agent_id=self.agent_id,
                signal_id="c2_detector",
                threat_type=ThreatType.C2_COMMUNICATION,
                raw_score=min(score, 1.0),
                calibrated_score=min(score, 1.0),
                confidence=min(score + 0.1, 1.0),
                features={
                    'response_code': response_code,
                    'ttl': ttl,
                    'response_ip_count': len(response_ips),
                },
                findings=findings,
                indicators=[{
                    'type': 'domain',
                    'value': features.get('query_name', ''),
                    'tags': ['c2', 'suspicious'],
                }],
                metadata={'detection_type': 'c2'},
            )
        
        return None
    
    def get_metadata(self) -> Dict[str, Any]:
        """Get agent metadata."""
        base = super().get_metadata()
        base.update({
            'entropy_threshold': self._entropy_threshold,
            'length_threshold': self._length_threshold,
            'detectors': ['dga', 'tunneling', 'c2'],
        })
        return base
