"""Quick signature matching for common malware patterns.

This module provides fast signature matching for the fastpath.
For comprehensive behavioral detection, see sentinel.signatures module.
"""

import re
from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, List, Optional, Set, Tuple

from ..parsers.base import ParsedFile, FileType
from ..logging import get_logger

logger = get_logger(__name__)

# Import comprehensive signatures for extended detection
try:
    from ..signatures import SignatureDetector as ComprehensiveDetector
    COMPREHENSIVE_AVAILABLE = True
except ImportError:
    COMPREHENSIVE_AVAILABLE = False


class SignatureCategory(str, Enum):
    """Signature categories."""
    DROPPER = "dropper"
    DOWNLOADER = "downloader"
    RAT = "rat"
    RANSOMWARE = "ransomware"
    STEALER = "stealer"
    MINER = "miner"
    BACKDOOR = "backdoor"
    EXPLOIT = "exploit"
    OBFUSCATION = "obfuscation"
    PERSISTENCE = "persistence"
    EVASION = "evasion"
    SUSPICIOUS = "suspicious"


@dataclass
class SignatureMatch:
    """Represents a signature match."""
    signature_id: str
    name: str
    category: SignatureCategory
    severity: str  # low, medium, high, critical
    description: str
    matched_data: List[str] = field(default_factory=list)
    confidence: float = 0.8


# Signature definitions
SIGNATURES: Dict[str, Dict] = {
    # Droppers/Downloaders
    "SIG_PWSH_DOWNLOAD_EXEC": {
        "name": "PowerShell Download & Execute",
        "category": SignatureCategory.DOWNLOADER,
        "severity": "high",
        "description": "PowerShell downloads and executes remote content",
        "patterns": [
            (r"(IEX|Invoke-Expression)\s*\(.*\.(DownloadString|DownloadData)", re.I),
            (r"(New-Object\s+Net\.WebClient).*Download", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    "SIG_CERTUTIL_DOWNLOAD": {
        "name": "Certutil Download",
        "category": SignatureCategory.DOWNLOADER,
        "severity": "high",
        "description": "Certutil abused for downloading files",
        "patterns": [
            (r"certutil\s+.*-urlcache.*-split.*http", re.I),
            (r"certutil\s+.*-urlcache.*http", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    "SIG_BITSADMIN_DOWNLOAD": {
        "name": "BitsAdmin Download",
        "category": SignatureCategory.DOWNLOADER,
        "severity": "medium",
        "description": "BitsAdmin used for file download",
        "patterns": [
            (r"bitsadmin\s+/transfer\s+.*http", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    
    # Obfuscation
    "SIG_BASE64_COMMAND": {
        "name": "Base64 Encoded Command",
        "category": SignatureCategory.OBFUSCATION,
        "severity": "high",
        "description": "Command uses Base64 encoding to hide payload",
        "patterns": [
            (r"-enc(odedcommand)?\s+[A-Za-z0-9+/=]{20,}", re.I),
            (r"FromBase64String\s*\(\s*['\"][A-Za-z0-9+/=]{20,}", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    "SIG_STRING_CONCAT_OBFUSCATION": {
        "name": "String Concatenation Obfuscation",
        "category": SignatureCategory.OBFUSCATION,
        "severity": "medium",
        "description": "Excessive string concatenation to evade detection",
        "patterns": [
            (r"(\+\s*['\"][^'\"]{1,3}['\"]){5,}", 0),
            (r"(\.\s*['\"][^'\"]{1,3}['\"]){5,}", 0),
        ],
        "file_types": {FileType.SCRIPT},
    },
    "SIG_CHAR_CODE_OBFUSCATION": {
        "name": "Character Code Obfuscation",
        "category": SignatureCategory.OBFUSCATION,
        "severity": "medium",
        "description": "Uses character codes to build strings",
        "patterns": [
            (r"(\[char\]\d+\s*\+\s*){3,}", re.I),
            (r"(chr\(\d+\)\s*[+&]\s*){3,}", re.I),
            (r"String\.fromCharCode\s*\(\s*\d+\s*(,\s*\d+\s*){5,}\)", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    
    # Persistence
    "SIG_REGISTRY_PERSISTENCE": {
        "name": "Registry Persistence",
        "category": SignatureCategory.PERSISTENCE,
        "severity": "high",
        "description": "Creates registry run key for persistence",
        "patterns": [
            (r"(HKLM|HKCU).*\\Software\\Microsoft\\Windows\\CurrentVersion\\Run", re.I),
            (r"New-ItemProperty.*CurrentVersion\\Run", re.I),
            (r"Set-ItemProperty.*CurrentVersion\\Run", re.I),
        ],
        "file_types": {FileType.SCRIPT, FileType.PE},
    },
    "SIG_SCHEDULED_TASK": {
        "name": "Scheduled Task Creation",
        "category": SignatureCategory.PERSISTENCE,
        "severity": "medium",
        "description": "Creates scheduled task for persistence",
        "patterns": [
            (r"schtasks\s+/create", re.I),
            (r"New-ScheduledTask", re.I),
            (r"Register-ScheduledTask", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    
    # Evasion
    "SIG_AMSI_BYPASS": {
        "name": "AMSI Bypass Attempt",
        "category": SignatureCategory.EVASION,
        "severity": "critical",
        "description": "Attempts to bypass AMSI protection",
        "patterns": [
            (r"amsi.*bypass", re.I),
            (r"AmsiScanBuffer", re.I),
            (r"\[Ref\]\.Assembly\.GetType.*amsi", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    "SIG_DISABLE_DEFENDER": {
        "name": "Windows Defender Tampering",
        "category": SignatureCategory.EVASION,
        "severity": "critical",
        "description": "Attempts to disable Windows Defender",
        "patterns": [
            (r"Set-MpPreference\s+-Disable", re.I),
            (r"DisableRealtimeMonitoring", re.I),
            (r"Remove-MpThreat", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    
    # Credential theft
    "SIG_MIMIKATZ": {
        "name": "Mimikatz Indicator",
        "category": SignatureCategory.STEALER,
        "severity": "critical",
        "description": "Contains Mimikatz-related strings",
        "patterns": [
            (r"mimikatz", re.I),
            (r"sekurlsa::logonpasswords", re.I),
            (r"lsadump::sam", re.I),
            (r"kerberos::golden", re.I),
        ],
        "file_types": {FileType.SCRIPT, FileType.PE},
    },
    "SIG_CREDENTIAL_DUMP": {
        "name": "Credential Dumping",
        "category": SignatureCategory.STEALER,
        "severity": "critical",
        "description": "Attempts to dump credentials",
        "patterns": [
            (r"procdump.*lsass", re.I),
            (r"comsvcs\.dll.*MiniDump", re.I),
            (r"Get-Process\s+lsass", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    
    # RAT/Backdoor
    "SIG_REVERSE_SHELL": {
        "name": "Reverse Shell",
        "category": SignatureCategory.BACKDOOR,
        "severity": "critical",
        "description": "Creates reverse shell connection",
        "patterns": [
            (r"TCPClient\s*\(\s*['\"][^'\"]+['\"]\s*,\s*\d+", re.I),
            (r"New-Object\s+Net\.Sockets\.TCPClient", re.I),
            (r"/bin/(ba)?sh\s+-i\s+>&", 0),
            (r"nc\s+-e\s+/bin/(ba)?sh", 0),
        ],
        "file_types": {FileType.SCRIPT},
    },
    
    # Ransomware indicators
    "SIG_RANSOMWARE_NOTE": {
        "name": "Ransomware Note",
        "category": SignatureCategory.RANSOMWARE,
        "severity": "critical",
        "description": "Contains ransomware note patterns",
        "patterns": [
            (r"your\s+(files|documents)\s+(have\s+been|are)\s+encrypted", re.I),
            (r"pay\s+.*\s*(bitcoin|btc|monero|xmr)", re.I),
            (r"decrypt.*\$\d+", re.I),
        ],
        "file_types": None,  # All types
    },
    "SIG_SHADOW_DELETE": {
        "name": "Volume Shadow Copy Deletion",
        "category": SignatureCategory.RANSOMWARE,
        "severity": "critical",
        "description": "Deletes volume shadow copies",
        "patterns": [
            (r"vssadmin\s+delete\s+shadows", re.I),
            (r"wmic\s+shadowcopy\s+delete", re.I),
            (r"bcdedit.*recoveryenabled.*no", re.I),
        ],
        "file_types": {FileType.SCRIPT},
    },
    
    # Office/Macro specific
    "SIG_MACRO_AUTOEXEC": {
        "name": "Auto-Execute Macro",
        "category": SignatureCategory.SUSPICIOUS,
        "severity": "medium",
        "description": "Macro with auto-execute capability",
        "patterns": [
            (r"(Auto_?Open|Document_?Open|Workbook_?Open)", re.I),
        ],
        "file_types": {FileType.OFFICE},
    },
    "SIG_MACRO_SHELL": {
        "name": "Macro Shell Execution",
        "category": SignatureCategory.DROPPER,
        "severity": "high",
        "description": "Macro executes shell commands",
        "patterns": [
            (r"Shell\s*\(\s*['\"].*\.(exe|cmd|bat|ps1)", re.I),
            (r"WScript\.Shell.*\.Run", re.I),
        ],
        "file_types": {FileType.OFFICE},
    },
}


class SignatureMatcher:
    """
    Quick signature matching engine.
    
    Matches pre-defined patterns against file content
    for rapid malware identification.
    """
    
    def __init__(self, custom_signatures: Optional[Dict] = None):
        """
        Initialize signature matcher.
        
        Args:
            custom_signatures: Additional signatures to load
        """
        self.signatures = SIGNATURES.copy()
        if custom_signatures:
            self.signatures.update(custom_signatures)
        
        # Pre-compile patterns
        self._compiled: Dict[str, List[re.Pattern]] = {}
        for sig_id, sig in self.signatures.items():
            patterns = []
            for pattern, flags in sig.get("patterns", []):
                try:
                    patterns.append(re.compile(pattern, flags))
                except re.error as e:
                    logger.error(f"Invalid regex in {sig_id}: {e}")
            self._compiled[sig_id] = patterns
    
    def match(self, parsed_file: ParsedFile) -> List[SignatureMatch]:
        """
        Match signatures against a parsed file.
        
        Args:
            parsed_file: Parsed file to scan
            
        Returns:
            List of SignatureMatch objects
        """
        matches = []
        
        # Build content to scan
        content_parts = []
        content_parts.extend(parsed_file.strings[:1000])
        content_parts.extend(parsed_file.scripts)
        content_parts.extend(parsed_file.macros)
        
        content = "\n".join(content_parts)
        
        for sig_id, sig in self.signatures.items():
            # Check file type filter
            allowed_types = sig.get("file_types")
            if allowed_types and parsed_file.file_type not in allowed_types:
                continue
            
            # Match patterns
            matched_data = []
            patterns = self._compiled.get(sig_id, [])
            
            for pattern in patterns:
                for match in pattern.finditer(content):
                    matched_data.append(match.group()[:100])
                    if len(matched_data) >= 5:
                        break
                
                if matched_data:
                    break
            
            if matched_data:
                matches.append(SignatureMatch(
                    signature_id=sig_id,
                    name=sig["name"],
                    category=sig["category"],
                    severity=sig["severity"],
                    description=sig["description"],
                    matched_data=matched_data,
                    confidence=0.85,
                ))
        
        return matches
    
    def match_content(
        self,
        content: str,
        file_type: Optional[FileType] = None
    ) -> List[SignatureMatch]:
        """
        Match signatures against raw content.
        
        Args:
            content: String content to scan
            file_type: Optional file type filter
            
        Returns:
            List of SignatureMatch objects
        """
        matches = []
        
        for sig_id, sig in self.signatures.items():
            allowed_types = sig.get("file_types")
            if file_type and allowed_types and file_type not in allowed_types:
                continue
            
            matched_data = []
            patterns = self._compiled.get(sig_id, [])
            
            for pattern in patterns:
                for match in pattern.finditer(content):
                    matched_data.append(match.group()[:100])
                    if len(matched_data) >= 5:
                        break
            
            if matched_data:
                matches.append(SignatureMatch(
                    signature_id=sig_id,
                    name=sig["name"],
                    category=sig["category"],
                    severity=sig["severity"],
                    description=sig["description"],
                    matched_data=matched_data,
                    confidence=0.85,
                ))
        
        return matches
    
    def add_signature(
        self,
        sig_id: str,
        name: str,
        patterns: List[Tuple[str, int]],
        category: SignatureCategory,
        severity: str,
        description: str,
        file_types: Optional[Set[FileType]] = None
    ) -> None:
        """Add a custom signature at runtime."""
        self.signatures[sig_id] = {
            "name": name,
            "category": category,
            "severity": severity,
            "description": description,
            "patterns": patterns,
            "file_types": file_types,
        }
        
        # Compile patterns
        compiled = []
        for pattern, flags in patterns:
            try:
                compiled.append(re.compile(pattern, flags))
            except re.error:
                pass
        self._compiled[sig_id] = compiled
    
    @property
    def signature_count(self) -> int:
        """Get number of loaded signatures."""
        return len(self.signatures)
    
    def match_comprehensive(self, file_path: str) -> List[SignatureMatch]:
        """
        Run comprehensive signature detection from sentinel.signatures module.
        
        Args:
            file_path: Path to file to scan
            
        Returns:
            List of SignatureMatch objects (converted from comprehensive format)
        """
        if not COMPREHENSIVE_AVAILABLE:
            return []
        
        try:
            detector = ComprehensiveDetector()
            matches = detector.detect_file(file_path)
            
            # Convert to fastpath SignatureMatch format
            results = []
            for m in matches:
                # Map comprehensive category to fastpath category
                cat_map = {
                    "persistence": SignatureCategory.PERSISTENCE,
                    "defense_evasion": SignatureCategory.EVASION,
                    "credential_access": SignatureCategory.STEALER,
                    "lateral_movement": SignatureCategory.BACKDOOR,
                    "command_and_control": SignatureCategory.BACKDOOR,
                    "exfiltration": SignatureCategory.STEALER,
                    "ransomware": SignatureCategory.RANSOMWARE,
                    "ai_llm_attacks": SignatureCategory.SUSPICIOUS,
                }
                cat = cat_map.get(m.signature.category.value, SignatureCategory.SUSPICIOUS)
                
                results.append(SignatureMatch(
                    signature_id=f"COMP_{m.signature.name}",
                    name=m.signature.name,
                    category=cat,
                    severity=m.signature.severity,
                    description=m.signature.description,
                    matched_data=[m.matched_text[:100]],
                    confidence=0.9,
                ))
            
            return results
        except Exception as e:
            logger.error(f"Comprehensive signature detection failed: {e}")
            return []
