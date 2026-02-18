"""Script file parser for PS1, JS, VBS, BAT, etc."""

import math
import re
from collections import Counter
from pathlib import Path
from typing import Any, Dict, List, Set

from .base import Parser, ParsedFile, FileType
from .detector import register_parser
from ..utils.hash import compute_multi_hash
from ..utils.errors import ParserError
from ..logging import get_logger

logger = get_logger(__name__)


class ScriptParser(Parser):
    """
    Parser for script files (PS1, JS, VBS, BAT, etc.).
    
    Extracts:
    - Script content
    - URLs, IPs, domains
    - Suspicious patterns
    - Obfuscation indicators
    """
    
    @property
    def name(self) -> str:
        return "script_parser"
    
    @property
    def supported_extensions(self) -> Set[str]:
        return {".ps1", ".psm1", ".psd1", ".js", ".jse", ".vbs", ".vbe", 
                ".wsf", ".bat", ".cmd", ".sh", ".py"}
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.SCRIPT}
    
    def can_parse(self, file_path: str) -> bool:
        ext = Path(file_path).suffix.lower()
        return ext in self.supported_extensions
    
    def parse(self, file_path: str) -> ParsedFile:
        path = Path(file_path)
        errors = []
        
        hashes = compute_multi_hash(file_path)
        file_size = path.stat().st_size
        
        try:
            with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
                content = f.read()
        except Exception as e:
            errors.append(f"Failed to read file: {e}")
            content = ""
        
        lines = content.split("\n")
        strings = [line.strip() for line in lines if line.strip()]
        
        urls = self._extract_urls(content)
        ips = self._extract_ips(content)
        domains = self._extract_domains(content)
        
        scripts = [content] if content else []
        
        metadata = {
            "line_count": len(lines),
            "char_count": len(content),
            "extension": path.suffix.lower(),
        }
        
        raw_features = {}
        
        suspicious = self._find_suspicious_patterns(content, path.suffix.lower())
        if suspicious:
            metadata["suspicious_patterns"] = suspicious
            raw_features["suspicious_pattern_count"] = len(suspicious)
        
        obfuscation = self._detect_obfuscation(content)
        if obfuscation:
            metadata["obfuscation_indicators"] = obfuscation
            raw_features["obfuscation_score"] = len(obfuscation) / 5.0
        
        entropy = self._calculate_entropy(content)
        
        return ParsedFile(
            file_path=str(path.absolute()),
            file_name=path.name,
            file_type=FileType.SCRIPT,
            file_size=file_size,
            hashes=hashes,
            entropy=entropy,
            strings=strings[:500],
            urls=urls,
            ips=ips,
            domains=domains,
            scripts=scripts,
            metadata=metadata,
            raw_features=raw_features,
            errors=errors,
        )
    
    def _extract_urls(self, content: str) -> List[str]:
        """Extract URLs from script content."""
        url_pattern = r'https?://[^\s\'"<>)\]]+' 
        urls = re.findall(url_pattern, content)
        return list(set(urls))[:50]
    
    def _extract_ips(self, content: str) -> List[str]:
        """Extract IP addresses."""
        ip_pattern = r'\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b'
        ips = re.findall(ip_pattern, content)
        filtered = [ip for ip in ips if not ip.startswith(('0.', '127.', '255.'))]
        return list(set(filtered))[:50]
    
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
        return list(set(domains))[:50]
    
    def _find_suspicious_patterns(self, content: str, extension: str) -> List[str]:
        """Find suspicious patterns based on script type."""
        found = []
        content_lower = content.lower()
        
        common_suspicious = [
            ("invoke-expression", "Code execution via Invoke-Expression"),
            ("iex(", "Code execution via IEX"),
            ("invoke-webrequest", "Web request (potential download)"),
            ("downloadstring", "Download and execute pattern"),
            ("downloadfile", "File download"),
            ("start-process", "Process execution"),
            ("new-object net.webclient", "WebClient instantiation"),
            ("-encodedcommand", "Encoded command execution"),
            ("-enc ", "Encoded command shorthand"),
            ("bypass", "Potential security bypass"),
            ("-nop", "No profile execution"),
            ("-w hidden", "Hidden window execution"),
            ("frombase64string", "Base64 decoding"),
            ("convertto-securestring", "Secure string conversion"),
            ("cmd.exe", "Command prompt execution"),
            ("powershell.exe", "PowerShell invocation"),
            ("wscript.shell", "WScript shell access"),
            ("shell.application", "Shell application access"),
            ("eval(", "Dynamic code evaluation"),
            ("exec(", "Code execution"),
            ("system(", "System command execution"),
        ]
        
        for pattern, description in common_suspicious:
            if pattern in content_lower:
                found.append(description)
        
        return found
    
    def _detect_obfuscation(self, content: str) -> List[str]:
        """Detect obfuscation techniques."""
        indicators = []
        
        base64_pattern = r'[A-Za-z0-9+/]{50,}={0,2}'
        if re.search(base64_pattern, content):
            indicators.append("Base64 encoded content detected")
        
        if content.count('+') > 50 or content.count('`') > 20:
            indicators.append("String concatenation obfuscation")
        
        char_pattern = r'\[char\]\d+|chr\(\d+\)'
        if len(re.findall(char_pattern, content.lower())) > 5:
            indicators.append("Character code obfuscation")
        
        if content.count('^') > 20:
            indicators.append("Escape character obfuscation (batch)")
        
        replace_count = content.lower().count('.replace(') + content.lower().count('-replace')
        if replace_count > 5:
            indicators.append("Multiple string replacements")
        
        return indicators
    
    def _calculate_entropy(self, content: str) -> float:
        """Calculate Shannon entropy of content."""
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


register_parser(FileType.SCRIPT, ScriptParser)
