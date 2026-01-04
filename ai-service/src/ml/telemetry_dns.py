"""DNS telemetry source for DNS log ingestion."""

import os
import re
import time
import json
import logging
from uuid import uuid4
from pathlib import Path
from typing import Dict, Any, Iterator, List, Optional
from dataclasses import dataclass, field

from .agents.contracts import CanonicalEvent, Modality, DNSFeaturesV1

logger = logging.getLogger(__name__)


@dataclass
class DNSLogEntry:
    """Raw DNS log entry before conversion to CanonicalEvent."""
    query_name: str
    query_type: str = "A"
    response_code: int = 0
    client_ip: Optional[str] = None
    server_ip: Optional[str] = None
    timestamp: Optional[float] = None
    tenant_id: str = "default"
    answers: List[str] = field(default_factory=list)
    ttl: Optional[int] = None
    raw_line: Optional[str] = None


class DNSTelemetrySource:
    """
    Telemetry source for DNS log events.
    
    Supports multiple DNS log formats:
    - Zeek (Bro) DNS logs
    - PassiveDNS feeds
    - Generic JSON DNS logs
    - Syslog-formatted DNS logs
    
    Produces CanonicalEvents with modality=DNS for processing by
    DNSAgent or other DNS-analysis agents.
    """
    
    # Zeek DNS log field indices (tab-separated)
    ZEEK_FIELDS = {
        'ts': 0,
        'uid': 1,
        'id.orig_h': 2,
        'id.orig_p': 3,
        'id.resp_h': 4,
        'id.resp_p': 5,
        'proto': 6,
        'trans_id': 7,
        'rtt': 8,
        'query': 9,
        'qclass': 10,
        'qclass_name': 11,
        'qtype': 12,
        'qtype_name': 13,
        'rcode': 14,
        'rcode_name': 15,
        'AA': 16,
        'TC': 17,
        'RD': 18,
        'RA': 19,
        'Z': 20,
        'answers': 21,
        'TTLs': 22,
        'rejected': 23,
    }
    
    # Response code mappings
    RCODE_MAP = {
        'NOERROR': 0, 'FORMERR': 1, 'SERVFAIL': 2, 'NXDOMAIN': 3,
        'NOTIMP': 4, 'REFUSED': 5, 'YXDOMAIN': 6, 'YXRRSET': 7,
        'NXRRSET': 8, 'NOTAUTH': 9, 'NOTZONE': 10,
    }
    
    def __init__(
        self,
        log_path: Optional[str] = None,
        log_format: str = "zeek",
        poll_interval: float = 1.0,
        tenant_id: str = "default",
        follow: bool = False,
    ):
        """
        Initialize DNS telemetry source.
        
        Args:
            log_path: Path to DNS log file (optional)
            log_format: Log format ('zeek', 'json', 'syslog', 'pdns')
            poll_interval: Seconds between log file checks
            tenant_id: Default tenant ID for events
            follow: Whether to follow the log file (tail -f style)
        """
        self.log_path = Path(log_path) if log_path else None
        self.log_format = log_format.lower()
        self.poll_interval = poll_interval
        self.tenant_id = tenant_id
        self.follow = follow
        
        self._queue: List[DNSLogEntry] = []
        self._file_position: int = 0
        self._running = False
    
    def submit(self, entry: DNSLogEntry) -> None:
        """
        Submit a DNS log entry to the queue for processing.
        
        Args:
            entry: DNSLogEntry to process
        """
        self._queue.append(entry)
    
    def submit_query(
        self,
        query_name: str,
        query_type: str = "A",
        response_code: int = 0,
        client_ip: Optional[str] = None,
        tenant_id: Optional[str] = None,
    ) -> None:
        """
        Submit a DNS query for processing.
        
        Args:
            query_name: DNS query name (domain)
            query_type: Query type (A, AAAA, MX, etc.)
            response_code: DNS response code (0=NOERROR, 3=NXDOMAIN, etc.)
            client_ip: Client IP address
            tenant_id: Optional tenant ID override
        """
        entry = DNSLogEntry(
            query_name=query_name,
            query_type=query_type,
            response_code=response_code,
            client_ip=client_ip,
            tenant_id=tenant_id or self.tenant_id,
            timestamp=time.time(),
        )
        self.submit(entry)
    
    def __iter__(self) -> Iterator[DNSLogEntry]:
        """
        Iterate over DNS log entries.
        
        Yields entries from:
        1. The submission queue (if any)
        2. Log file (if configured)
        """
        # First, yield any queued entries
        while self._queue:
            yield self._queue.pop(0)
        
        # Then read from log file if configured
        if self.log_path and self.log_path.exists():
            for entry in self._read_log_file():
                yield entry
    
    def _read_log_file(self) -> Iterator[DNSLogEntry]:
        """Read and parse DNS log file."""
        if not self.log_path or not self.log_path.exists():
            return
        
        try:
            with open(self.log_path, 'r', encoding='utf-8', errors='ignore') as f:
                # Seek to last position if following
                if self.follow and self._file_position > 0:
                    f.seek(self._file_position)
                
                for line in f:
                    line = line.strip()
                    if not line or line.startswith('#'):
                        continue
                    
                    entry = self._parse_line(line)
                    if entry:
                        yield entry
                
                # Save position for next read
                self._file_position = f.tell()
        
        except PermissionError as e:
            logger.error(f"Permission denied reading {self.log_path}: {e}")
        except Exception as e:
            logger.error(f"Error reading DNS log {self.log_path}: {e}")
    
    def _parse_line(self, line: str) -> Optional[DNSLogEntry]:
        """Parse a single log line based on format."""
        try:
            if self.log_format == "zeek":
                return self._parse_zeek_line(line)
            elif self.log_format == "json":
                return self._parse_json_line(line)
            elif self.log_format == "pdns":
                return self._parse_pdns_line(line)
            else:
                logger.warning(f"Unknown log format: {self.log_format}")
                return None
        except Exception as e:
            logger.debug(f"Failed to parse line: {e}")
            return None
    
    def _parse_zeek_line(self, line: str) -> Optional[DNSLogEntry]:
        """Parse Zeek DNS log line (tab-separated)."""
        fields = line.split('\t')
        
        if len(fields) < 15:
            return None
        
        # Get timestamp
        try:
            ts = float(fields[self.ZEEK_FIELDS['ts']])
        except (ValueError, IndexError):
            ts = time.time()
        
        # Get query name
        query = fields[self.ZEEK_FIELDS['query']] if len(fields) > self.ZEEK_FIELDS['query'] else ''
        if query == '-' or not query:
            return None
        
        # Get query type
        qtype_name = fields[self.ZEEK_FIELDS['qtype_name']] if len(fields) > self.ZEEK_FIELDS['qtype_name'] else 'A'
        if qtype_name == '-':
            qtype_name = 'A'
        
        # Get response code
        rcode_name = fields[self.ZEEK_FIELDS['rcode_name']] if len(fields) > self.ZEEK_FIELDS['rcode_name'] else 'NOERROR'
        rcode = self.RCODE_MAP.get(rcode_name, 0)
        
        # Get IPs
        client_ip = fields[self.ZEEK_FIELDS['id.orig_h']] if len(fields) > self.ZEEK_FIELDS['id.orig_h'] else None
        server_ip = fields[self.ZEEK_FIELDS['id.resp_h']] if len(fields) > self.ZEEK_FIELDS['id.resp_h'] else None
        
        # Get answers and TTLs
        answers = []
        ttl = None
        if len(fields) > self.ZEEK_FIELDS['answers']:
            answers_str = fields[self.ZEEK_FIELDS['answers']]
            if answers_str != '-':
                answers = answers_str.split(',')
        
        if len(fields) > self.ZEEK_FIELDS['TTLs']:
            ttls_str = fields[self.ZEEK_FIELDS['TTLs']]
            if ttls_str != '-':
                try:
                    ttl = int(float(ttls_str.split(',')[0]))
                except (ValueError, IndexError):
                    pass
        
        return DNSLogEntry(
            query_name=query,
            query_type=qtype_name,
            response_code=rcode,
            client_ip=client_ip if client_ip != '-' else None,
            server_ip=server_ip if server_ip != '-' else None,
            timestamp=ts,
            tenant_id=self.tenant_id,
            answers=answers,
            ttl=ttl,
            raw_line=line,
        )
    
    def _parse_json_line(self, line: str) -> Optional[DNSLogEntry]:
        """Parse JSON-formatted DNS log line."""
        try:
            data = json.loads(line)
        except json.JSONDecodeError:
            return None
        
        query_name = data.get('query', data.get('qname', data.get('domain', '')))
        if not query_name:
            return None
        
        query_type = data.get('query_type', data.get('qtype', data.get('type', 'A')))
        
        rcode = data.get('response_code', data.get('rcode', 0))
        if isinstance(rcode, str):
            rcode = self.RCODE_MAP.get(rcode.upper(), 0)
        
        return DNSLogEntry(
            query_name=query_name,
            query_type=str(query_type),
            response_code=int(rcode),
            client_ip=data.get('client_ip', data.get('src_ip')),
            server_ip=data.get('server_ip', data.get('dst_ip')),
            timestamp=data.get('timestamp', data.get('ts', time.time())),
            tenant_id=data.get('tenant_id', self.tenant_id),
            answers=data.get('answers', []),
            ttl=data.get('ttl'),
            raw_line=line,
        )
    
    def _parse_pdns_line(self, line: str) -> Optional[DNSLogEntry]:
        """Parse PassiveDNS format line."""
        # PassiveDNS format: timestamp||query_ip||response_ip||rr_class||query||rr_type||answer||ttl||count
        parts = line.split('||')
        
        if len(parts) < 7:
            return None
        
        try:
            ts = float(parts[0])
        except ValueError:
            ts = time.time()
        
        return DNSLogEntry(
            query_name=parts[4],
            query_type=parts[5],
            response_code=0,  # pDNS typically only logs successful resolutions
            client_ip=parts[1] if parts[1] != '-' else None,
            server_ip=parts[2] if parts[2] != '-' else None,
            timestamp=ts,
            tenant_id=self.tenant_id,
            answers=[parts[6]] if parts[6] else [],
            ttl=int(parts[7]) if len(parts) > 7 and parts[7].isdigit() else None,
            raw_line=line,
        )
    
    def to_canonical_event(
        self,
        entry: DNSLogEntry,
        source: str = "dns_telemetry",
    ) -> CanonicalEvent:
        """
        Convert a DNSLogEntry to a CanonicalEvent.
        
        Args:
            entry: DNSLogEntry to convert
            source: Source identifier for the event
            
        Returns:
            CanonicalEvent with modality=DNS
        """
        # Compute DNS features
        features = self._compute_dns_features(entry)
        
        raw_context = {
            "query_name": entry.query_name,
            "query_type": entry.query_type,
            "response_code": entry.response_code,
            "answers": entry.answers,
        }
        
        if entry.client_ip:
            raw_context["client_ip"] = entry.client_ip
        if entry.server_ip:
            raw_context["server_ip"] = entry.server_ip
        if entry.ttl is not None:
            raw_context["ttl"] = entry.ttl
        
        return CanonicalEvent(
            id=str(uuid4()),
            timestamp=entry.timestamp or time.time(),
            source=source,
            tenant_id=entry.tenant_id,
            modality=Modality.DNS,
            features_version="DNSFeaturesV1",
            features=features.to_dict(),
            raw_context=raw_context,
        )
    
    def to_canonical_events_batch(
        self,
        entries: List[DNSLogEntry],
        source: str = "dns_telemetry",
    ) -> List[CanonicalEvent]:
        """
        Convert a batch of DNSLogEntries to CanonicalEvents.
        
        Args:
            entries: List of DNSLogEntries
            source: Source identifier
            
        Returns:
            List of CanonicalEvents
        """
        canonical_events = []
        for entry in entries:
            try:
                canonical = self.to_canonical_event(entry, source)
                canonical_events.append(canonical)
            except Exception as e:
                logger.warning(f"Failed to convert DNS entry {entry.query_name}: {e}")
                continue
        return canonical_events
    
    def _compute_dns_features(self, entry: DNSLogEntry) -> DNSFeaturesV1:
        """Compute DNS features for a log entry."""
        domain = entry.query_name.lower().rstrip('.')
        
        # Domain length
        domain_length = len(domain)
        
        # Subdomain analysis
        parts = domain.split('.')
        subdomain_count = max(0, len(parts) - 2)  # Exclude TLD and SLD
        subdomain_lengths = [len(p) for p in parts[:-2]] if len(parts) > 2 else []
        subdomain_length_max = max(subdomain_lengths) if subdomain_lengths else 0
        subdomain_length_mean = sum(subdomain_lengths) / len(subdomain_lengths) if subdomain_lengths else 0.0
        
        # Character analysis
        digits = sum(1 for c in domain if c.isdigit())
        vowels = sum(1 for c in domain.lower() if c in 'aeiou')
        consonants = sum(1 for c in domain.lower() if c.isalpha() and c not in 'aeiou')
        special = sum(1 for c in domain if not c.isalnum() and c != '.')
        
        total_chars = max(1, len(domain.replace('.', '')))
        digit_ratio = digits / total_chars
        vowel_ratio = vowels / total_chars
        consonant_ratio = consonants / total_chars
        
        # Domain entropy
        domain_entropy = self._compute_entropy(domain.replace('.', ''))
        
        # TTL features
        ttl_min = entry.ttl if entry.ttl is not None else 0
        ttl_max = entry.ttl if entry.ttl is not None else 0
        
        return DNSFeaturesV1(
            query_name=entry.query_name,
            query_type=entry.query_type,
            response_code=entry.response_code,
            domain_length=domain_length,
            domain_entropy=domain_entropy,
            subdomain_count=subdomain_count,
            subdomain_length_max=subdomain_length_max,
            subdomain_length_mean=subdomain_length_mean,
            digit_ratio=digit_ratio,
            consonant_ratio=consonant_ratio,
            vowel_ratio=vowel_ratio,
            special_char_count=special,
            ttl_min=ttl_min,
            ttl_max=ttl_max,
            answer_count=len(entry.answers),
            client_ip=entry.client_ip,
            server_ip=entry.server_ip,
            timestamp=entry.timestamp,
        )
    
    @staticmethod
    def _compute_entropy(s: str) -> float:
        """Compute Shannon entropy of a string."""
        if not s:
            return 0.0
        
        from collections import Counter
        import math
        
        freq = Counter(s)
        total = len(s)
        entropy = 0.0
        
        for count in freq.values():
            p = count / total
            if p > 0:
                entropy -= p * math.log2(p)
        
        return entropy
    
    def get_pending_count(self) -> int:
        """Return number of pending entries in queue."""
        return len(self._queue)
    
    def reset_position(self) -> None:
        """Reset file position to re-read from beginning."""
        self._file_position = 0
