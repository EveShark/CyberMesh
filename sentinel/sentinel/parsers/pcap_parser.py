"""PCAP network capture parser."""

import math
import re
import struct
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any, Dict, List, Set, Tuple

from .base import Parser, ParsedFile, FileType
from .detector import register_parser
from ..utils.hash import compute_multi_hash
from ..utils.errors import ParserError
from ..logging import get_logger

logger = get_logger(__name__)


class PCAPParser(Parser):
    """
    Parser for PCAP/PCAPNG network capture files.
    
    Extracts:
    - Packet statistics
    - IP addresses (source/destination)
    - DNS queries
    - HTTP requests
    - Suspicious patterns
    """
    
    PCAP_MAGIC = b'\xa1\xb2\xc3\xd4'
    PCAP_MAGIC_SWAPPED = b'\xd4\xc3\xb2\xa1'
    PCAPNG_MAGIC = b'\x0a\x0d\x0d\x0a'
    
    SUSPICIOUS_PORTS = {
        4444: "Metasploit default",
        5555: "Android ADB",
        6666: "IRC backdoor",
        6667: "IRC",
        31337: "Elite backdoor",
        12345: "NetBus",
        27374: "SubSeven",
        1337: "Common backdoor",
        8080: "HTTP Proxy",
        3389: "RDP",
        22: "SSH",
        23: "Telnet",
        445: "SMB",
        139: "NetBIOS",
    }
    
    @property
    def name(self) -> str:
        return "pcap_parser"
    
    @property
    def supported_extensions(self) -> Set[str]:
        return {".pcap", ".pcapng", ".cap"}
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PCAP}
    
    def can_parse(self, file_path: str) -> bool:
        try:
            with open(file_path, "rb") as f:
                header = f.read(4)
            return header in (self.PCAP_MAGIC, self.PCAP_MAGIC_SWAPPED, self.PCAPNG_MAGIC)
        except Exception:
            return False
    
    def parse(self, file_path: str) -> ParsedFile:
        path = Path(file_path)
        errors = []
        
        hashes = compute_multi_hash(file_path)
        file_size = path.stat().st_size
        
        metadata = {}
        strings = []
        urls = []
        ips = []
        domains = []
        raw_features = {}
        
        with open(file_path, "rb") as f:
            header = f.read(4)
            f.seek(0)
            content = f.read()
        
        if header in (self.PCAP_MAGIC, self.PCAP_MAGIC_SWAPPED):
            pcap_data = self._parse_pcap(content, errors)
        elif header == self.PCAPNG_MAGIC:
            pcap_data = self._parse_pcapng(content, errors)
        else:
            errors.append("Unknown PCAP format")
            pcap_data = {}
        
        if pcap_data:
            metadata.update(pcap_data.get("metadata", {}))
            ips = pcap_data.get("ips", [])
            domains = pcap_data.get("domains", [])
            urls = pcap_data.get("urls", [])
            strings = pcap_data.get("strings", [])
            raw_features = pcap_data.get("raw_features", {})
        
        entropy = self._calculate_entropy(content)
        
        return ParsedFile(
            file_path=str(path.absolute()),
            file_name=path.name,
            file_type=FileType.PCAP,
            file_size=file_size,
            hashes=hashes,
            entropy=entropy,
            strings=strings,
            urls=urls,
            ips=ips,
            domains=domains,
            metadata=metadata,
            raw_features=raw_features,
            errors=errors,
        )
    
    def _parse_pcap(self, content: bytes, errors: List[str]) -> Dict:
        """Parse classic PCAP format."""
        result = {
            "metadata": {"format": "PCAP"},
            "ips": [],
            "domains": [],
            "urls": [],
            "strings": [],
            "raw_features": {},
        }
        
        try:
            header = content[:24]
            magic = struct.unpack("<I", header[:4])[0]
            
            if magic == 0xa1b2c3d4:
                byte_order = "<"
            elif magic == 0xd4c3b2a1:
                byte_order = ">"
            else:
                errors.append("Invalid PCAP magic number")
                return result
            
            version_major, version_minor, _, _, snaplen, linktype = struct.unpack(
                f"{byte_order}HHIIII", header[4:24]
            )
            
            result["metadata"]["version"] = f"{version_major}.{version_minor}"
            result["metadata"]["snaplen"] = snaplen
            result["metadata"]["linktype"] = linktype
            
            offset = 24
            packet_count = 0
            src_ips = set()
            dst_ips = set()
            ports = defaultdict(int)
            
            while offset + 16 <= len(content):
                try:
                    ts_sec, ts_usec, incl_len, orig_len = struct.unpack(
                        f"{byte_order}IIII", content[offset:offset+16]
                    )
                    
                    packet_data = content[offset+16:offset+16+incl_len]
                    
                    ip_info = self._extract_ip_info(packet_data, linktype)
                    if ip_info:
                        if ip_info.get("src_ip"):
                            src_ips.add(ip_info["src_ip"])
                        if ip_info.get("dst_ip"):
                            dst_ips.add(ip_info["dst_ip"])
                        if ip_info.get("src_port"):
                            ports[ip_info["src_port"]] += 1
                        if ip_info.get("dst_port"):
                            ports[ip_info["dst_port"]] += 1
                    
                    offset += 16 + incl_len
                    packet_count += 1
                    
                    if packet_count >= 10000:
                        break
                        
                except struct.error:
                    break
            
            result["metadata"]["packet_count"] = packet_count
            result["ips"] = list(src_ips | dst_ips)[:100]
            
            suspicious_ports = []
            for port, count in ports.items():
                if port in self.SUSPICIOUS_PORTS:
                    suspicious_ports.append(f"{port}:{self.SUSPICIOUS_PORTS[port]}({count})")
            
            if suspicious_ports:
                result["metadata"]["suspicious_ports"] = suspicious_ports
                result["raw_features"]["suspicious_port_count"] = len(suspicious_ports)
            
            result["metadata"]["unique_ips"] = len(result["ips"])
            
            text = content.decode("latin-1", errors="ignore")
            result["domains"] = self._extract_domains(text)
            result["urls"] = self._extract_urls(text)
            
            http_hosts = re.findall(r'Host:\s*([^\r\n]+)', text)
            if http_hosts:
                result["metadata"]["http_hosts"] = list(set(http_hosts))[:20]
            
            dns_queries = self._extract_dns_queries(content)
            if dns_queries:
                result["metadata"]["dns_queries"] = dns_queries[:50]
                result["domains"].extend(dns_queries)
                result["domains"] = list(set(result["domains"]))[:100]
                
        except Exception as e:
            errors.append(f"PCAP parsing error: {e}")
        
        return result
    
    def _parse_pcapng(self, content: bytes, errors: List[str]) -> Dict:
        """Parse PCAPNG format (basic)."""
        result = {
            "metadata": {"format": "PCAPNG"},
            "ips": [],
            "domains": [],
            "urls": [],
            "strings": [],
            "raw_features": {},
        }
        
        try:
            text = content.decode("latin-1", errors="ignore")
            result["ips"] = self._extract_ips(text)
            result["domains"] = self._extract_domains(text)
            result["urls"] = self._extract_urls(text)
            
            http_hosts = re.findall(r'Host:\s*([^\r\n]+)', text)
            if http_hosts:
                result["metadata"]["http_hosts"] = list(set(http_hosts))[:20]
                
        except Exception as e:
            errors.append(f"PCAPNG parsing error: {e}")
        
        return result
    
    def _extract_ip_info(self, packet: bytes, linktype: int) -> Dict:
        """Extract IP information from packet."""
        result = {}
        
        try:
            if linktype == 1:
                if len(packet) < 14:
                    return result
                ip_start = 14
            elif linktype == 0:
                ip_start = 4
            else:
                return result
            
            if ip_start >= len(packet):
                return result
            
            version = (packet[ip_start] >> 4) & 0xF
            
            if version == 4 and len(packet) >= ip_start + 20:
                ihl = (packet[ip_start] & 0xF) * 4
                protocol = packet[ip_start + 9]
                
                src_ip = ".".join(str(b) for b in packet[ip_start+12:ip_start+16])
                dst_ip = ".".join(str(b) for b in packet[ip_start+16:ip_start+20])
                
                result["src_ip"] = src_ip
                result["dst_ip"] = dst_ip
                result["protocol"] = protocol
                
                if protocol in (6, 17) and len(packet) >= ip_start + ihl + 4:
                    src_port, dst_port = struct.unpack(
                        ">HH", packet[ip_start+ihl:ip_start+ihl+4]
                    )
                    result["src_port"] = src_port
                    result["dst_port"] = dst_port
                    
        except Exception:
            pass
        
        return result
    
    def _extract_dns_queries(self, content: bytes) -> List[str]:
        """Extract DNS query names from packet data."""
        queries = []
        
        dns_pattern = rb'\x00\x01\x00\x01'
        
        text = content.decode("latin-1", errors="ignore")
        domain_pattern = r'[a-zA-Z0-9][-a-zA-Z0-9]{0,62}(?:\.[a-zA-Z0-9][-a-zA-Z0-9]{0,62})+'
        matches = re.findall(domain_pattern, text)
        
        for match in matches:
            if len(match) > 4 and '.' in match:
                tld = match.split('.')[-1].lower()
                if tld.isalpha() and len(tld) >= 2:
                    queries.append(match.lower())
        
        return list(set(queries))
    
    def _extract_urls(self, content: str) -> List[str]:
        """Extract URLs."""
        url_pattern = r'https?://[^\s\'"<>)\]\\]{5,200}'
        urls = re.findall(url_pattern, content)
        return list(set(urls))[:100]
    
    def _extract_ips(self, content: str) -> List[str]:
        """Extract IP addresses."""
        ip_pattern = r'\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b'
        ips = re.findall(ip_pattern, content)
        filtered = [ip for ip in ips if not ip.startswith(('0.', '127.', '255.', '224.'))]
        return list(set(filtered))[:100]
    
    def _extract_domains(self, content: str) -> List[str]:
        """Extract domain names."""
        domain_pattern = r'\b[a-zA-Z0-9][-a-zA-Z0-9]{0,62}(?:\.[a-zA-Z0-9][-a-zA-Z0-9]{0,62})+\b'
        domains = []
        for match in re.findall(domain_pattern, content):
            domain = match.lower()
            if '.' in domain and len(domain) > 4:
                tld = domain.split('.')[-1]
                if len(tld) >= 2 and tld.isalpha():
                    domains.append(domain)
        return list(set(domains))[:100]
    
    def _calculate_entropy(self, content: bytes) -> float:
        """Calculate Shannon entropy."""
        if not content:
            return 0.0
        
        counter = Counter(content)
        length = len(content)
        entropy = 0.0
        
        for count in counter.values():
            if count > 0:
                p = count / length
                entropy -= p * math.log2(p)
        
        return round(entropy, 4)


register_parser(FileType.PCAP, PCAPParser)
