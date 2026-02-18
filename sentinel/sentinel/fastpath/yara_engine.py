"""YARA rule engine for fast pattern matching."""

import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Dict, List, Optional, Set

from ..logging import get_logger

logger = get_logger(__name__)

try:
    import yara
    YARA_AVAILABLE = True
except ImportError:
    YARA_AVAILABLE = False
    logger.warning("yara-python not installed, YARA rules disabled")


@dataclass
class YaraMatch:
    """Represents a YARA rule match."""
    rule_name: str
    namespace: str
    tags: List[str] = field(default_factory=list)
    meta: Dict[str, str] = field(default_factory=dict)
    strings: List[tuple] = field(default_factory=list)
    
    @property
    def severity(self) -> str:
        """Get severity from rule metadata."""
        return self.meta.get("severity", "medium")
    
    @property
    def description(self) -> str:
        """Get description from rule metadata."""
        return self.meta.get("description", self.rule_name)
    
    @property
    def is_malware(self) -> bool:
        """Check if rule indicates malware."""
        return "malware" in self.tags or self.meta.get("malware", "false").lower() == "true"


class YaraEngine:
    """
    YARA rule engine for fast malware detection.
    
    Features:
    - Load rules from files or directories
    - Compile rules for fast matching
    - Match against file content or parsed data
    - Track rule statistics
    """
    
    def __init__(self, rules_path: Optional[str] = None, use_external_rules: bool = True):
        """
        Initialize YARA engine.
        
        Args:
            rules_path: Path to YARA rules file or directory
            use_external_rules: If True, load from ./rules/ directory (processed from repos)
        """
        self.rules_path = Path(rules_path) if rules_path else None
        self.use_external_rules = use_external_rules
        self.compiled_rules = None
        self.rule_count = 0
        self.load_errors: List[Dict] = []
        self.rules_by_category: Dict[str, int] = {}
        self._load_rules()
    
    @property
    def available(self) -> bool:
        """Check if YARA is available."""
        return YARA_AVAILABLE and self.compiled_rules is not None
    
    def _load_rules(self) -> None:
        """Load and compile YARA rules."""
        if not YARA_AVAILABLE:
            return
        
        # First try external rules directory (processed from repos)
        if self.use_external_rules:
            rules_dir = Path(__file__).parent.parent.parent / "rules"
            if rules_dir.exists():
                self._load_from_categorized_dir(rules_dir)
                if self.compiled_rules:
                    return
        
        # Fall back to custom rules_path
        if self.rules_path is None:
            default_paths = [
                Path("data/yara_rules"),
                Path("./yara_rules"),
                Path(__file__).parent.parent.parent / "data" / "yara_rules",
            ]
            for path in default_paths:
                if path.exists():
                    self.rules_path = path
                    break
        
        if self.rules_path is None or not self.rules_path.exists():
            logger.info("No YARA rules path found, using built-in rules")
            self._load_builtin_rules()
            return
        
        try:
            if self.rules_path.is_file():
                self.compiled_rules = yara.compile(filepath=str(self.rules_path))
                self.rule_count = 1
            elif self.rules_path.is_dir():
                self._load_from_directory(self.rules_path)
                    
        except yara.Error as e:
            logger.error(f"Failed to compile YARA rules: {e}")
            self._load_builtin_rules()
    
    def _load_from_categorized_dir(self, rules_dir: Path) -> None:
        """Load rules from categorized directory structure (./rules/{category}/)."""
        categories = ["malware", "exploit", "packer", "webshell", "document", "generic"]
        rule_files = {}
        
        for category in categories:
            cat_dir = rules_dir / category
            if not cat_dir.exists():
                continue
            
            cat_count = 0
            for rule_file in cat_dir.glob("*.yar"):
                namespace = f"{category}_{rule_file.stem}"
                try:
                    # Test compile individually first
                    yara.compile(filepath=str(rule_file))
                    rule_files[namespace] = str(rule_file)
                    cat_count += 1
                except yara.Error as e:
                    self.load_errors.append({
                        "file": rule_file.name,
                        "category": category,
                        "error": str(e)[:100],
                    })
            
            for rule_file in cat_dir.glob("*.yara"):
                namespace = f"{category}_{rule_file.stem}"
                try:
                    yara.compile(filepath=str(rule_file))
                    rule_files[namespace] = str(rule_file)
                    cat_count += 1
                except yara.Error as e:
                    self.load_errors.append({
                        "file": rule_file.name,
                        "category": category,
                        "error": str(e)[:100],
                    })
            
            self.rules_by_category[category] = cat_count
        
        if rule_files:
            try:
                self.compiled_rules = yara.compile(filepaths=rule_files)
                self.rule_count = len(rule_files)
                logger.info(
                    f"Loaded {self.rule_count} YARA rule files from categories: "
                    f"{dict(self.rules_by_category)}"
                )
                if self.load_errors:
                    logger.warning(f"{len(self.load_errors)} rules failed to compile")
            except yara.Error as e:
                logger.error(f"Failed to compile combined YARA rules: {e}")
                self._load_builtin_rules()
        else:
            self._load_builtin_rules()
    
    def _load_from_directory(self, directory: Path) -> None:
        """Load all .yar/.yara files from a directory."""
        rule_files = {}
        
        for rule_file in directory.glob("*.yar"):
            namespace = rule_file.stem
            rule_files[namespace] = str(rule_file)
        
        for rule_file in directory.glob("*.yara"):
            namespace = rule_file.stem
            rule_files[namespace] = str(rule_file)
        
        if rule_files:
            self.compiled_rules = yara.compile(filepaths=rule_files)
            self.rule_count = len(rule_files)
            logger.info(f"Loaded {self.rule_count} YARA rule files")
        else:
            self._load_builtin_rules()
    
    def _load_builtin_rules(self) -> None:
        """Load built-in detection rules."""
        if not YARA_AVAILABLE:
            return
        
        builtin_rules = '''
rule Suspicious_PowerShell {
    meta:
        description = "Detects suspicious PowerShell patterns"
        severity = "high"
        malware = "false"
    strings:
        $enc1 = "-enc" ascii nocase
        $enc2 = "-encodedcommand" ascii nocase
        $hidden = "-w hidden" ascii nocase
        $nop = "-nop" ascii nocase
        $bypass = "bypass" ascii nocase
        $iex = "iex" ascii nocase
        $download = "downloadstring" ascii nocase
        $webclient = "net.webclient" ascii nocase
    condition:
        3 of them
}

rule Suspicious_Script_Downloader {
    meta:
        description = "Script downloads and executes remote content"
        severity = "high"
        malware = "false"
    strings:
        $dl1 = "Invoke-WebRequest" ascii nocase
        $dl2 = "wget" ascii nocase
        $dl3 = "curl" ascii nocase
        $dl4 = "DownloadFile" ascii nocase
        $dl5 = "DownloadString" ascii nocase
        $dl6 = "Start-BitsTransfer" ascii nocase
        $exec1 = "Invoke-Expression" ascii nocase
        $exec2 = "iex" ascii nocase
        $exec3 = "cmd.exe" ascii nocase
        $exec4 = "powershell.exe" ascii nocase
    condition:
        any of ($dl*) and any of ($exec*)
}

rule Certutil_Abuse {
    meta:
        description = "Certutil used for downloading or decoding"
        severity = "high"
        malware = "false"
    strings:
        $certutil = "certutil" ascii nocase
        $decode = "-decode" ascii nocase
        $urlcache = "-urlcache" ascii nocase
        $split = "-split" ascii nocase
    condition:
        $certutil and any of ($decode, $urlcache, $split)
}

rule BitsAdmin_Abuse {
    meta:
        description = "BitsAdmin used for downloading"
        severity = "medium"
        malware = "false"
    strings:
        $bitsadmin = "bitsadmin" ascii nocase
        $transfer = "/transfer" ascii nocase
        $download = "/download" ascii nocase
    condition:
        $bitsadmin and any of ($transfer, $download)
}

rule Suspicious_WScript {
    meta:
        description = "WScript shell execution"
        severity = "high"
        malware = "false"
    strings:
        $wscript = "wscript.shell" ascii nocase
        $run = ".run" ascii nocase
        $exec = ".exec" ascii nocase
    condition:
        $wscript and any of ($run, $exec)
}

rule Base64_Encoded_PE {
    meta:
        description = "Base64 encoded PE file"
        severity = "critical"
        malware = "true"
    strings:
        $b64_mz = "TVqQAA" ascii
        $b64_mz2 = "TVpQAA" ascii
        $b64_mz3 = "TVoAAA" ascii
    condition:
        any of them
}

rule Embedded_Executable {
    meta:
        description = "Embedded executable in script"
        severity = "critical"
        malware = "true"
    strings:
        $mz = { 4D 5A }
        $pe = { 50 45 00 00 }
    condition:
        $mz at 0 or ($mz and $pe)
}

rule Suspicious_VBA_Macro {
    meta:
        description = "Suspicious VBA macro patterns"
        severity = "high"
        malware = "false"
    strings:
        $auto1 = "AutoOpen" ascii nocase
        $auto2 = "Auto_Open" ascii nocase
        $auto3 = "Document_Open" ascii nocase
        $auto4 = "Workbook_Open" ascii nocase
        $shell = "Shell" ascii nocase
        $wscript = "WScript" ascii nocase
        $powershell = "powershell" ascii nocase
        $cmd = "cmd.exe" ascii nocase
        $createobj = "CreateObject" ascii nocase
    condition:
        any of ($auto*) and (any of ($shell, $wscript, $powershell, $cmd) or $createobj)
}

rule PDF_JavaScript {
    meta:
        description = "PDF with JavaScript"
        severity = "medium"
        malware = "false"
    strings:
        $pdf = "%PDF"
        $js1 = "/JavaScript" ascii nocase
        $js2 = "/JS" ascii nocase
        $action = "/OpenAction" ascii nocase
        $launch = "/Launch" ascii nocase
    condition:
        $pdf at 0 and (any of ($js*) or $action or $launch)
}

rule Ransomware_Indicators {
    meta:
        description = "Potential ransomware indicators"
        severity = "critical"
        malware = "true"
    strings:
        $ransom1 = "your files have been encrypted" ascii nocase
        $ransom2 = "bitcoin" ascii nocase
        $ransom3 = "decrypt" ascii nocase
        $ransom4 = "ransom" ascii nocase
        $ext1 = ".locked" ascii nocase
        $ext2 = ".encrypted" ascii nocase
        $ext3 = ".crypto" ascii nocase
    condition:
        2 of ($ransom*) or (any of ($ransom*) and any of ($ext*))
}
'''
        
        try:
            self.compiled_rules = yara.compile(source=builtin_rules)
            self.rule_count = 10
            logger.info("Loaded built-in YARA rules")
        except yara.Error as e:
            logger.error(f"Failed to compile built-in rules: {e}")
    
    def match_file(self, file_path: str) -> List[YaraMatch]:
        """
        Match YARA rules against a file.
        
        Args:
            file_path: Path to file to scan
            
        Returns:
            List of YaraMatch objects for matching rules
        """
        if not self.available:
            return []
        
        try:
            matches = self.compiled_rules.match(filepath=file_path, timeout=30)
            return [self._convert_match(m) for m in matches]
        except yara.Error as e:
            logger.error(f"YARA match failed: {e}")
            return []
    
    def match_data(self, data: bytes) -> List[YaraMatch]:
        """
        Match YARA rules against raw data.
        
        Args:
            data: Bytes to scan
            
        Returns:
            List of YaraMatch objects
        """
        if not self.available:
            return []
        
        try:
            matches = self.compiled_rules.match(data=data, timeout=30)
            return [self._convert_match(m) for m in matches]
        except yara.Error as e:
            logger.error(f"YARA match failed: {e}")
            return []
    
    def _convert_match(self, match) -> YaraMatch:
        """Convert yara match to YaraMatch dataclass."""
        # Handle different yara-python API versions
        strings_data = []
        try:
            for s in match.strings[:10]:
                if hasattr(s, 'instances'):
                    # New yara-python API (4.x)
                    for instance in s.instances[:3]:
                        strings_data.append((
                            instance.offset,
                            s.identifier,
                            instance.matched_data.decode("utf-8", errors="replace")
                        ))
                elif isinstance(s, tuple) and len(s) >= 3:
                    # Old tuple-based API
                    strings_data.append((s[0], s[1], s[2].decode("utf-8", errors="replace")))
                else:
                    # Fallback
                    strings_data.append((0, str(s), ""))
        except Exception:
            pass
        
        return YaraMatch(
            rule_name=match.rule,
            namespace=match.namespace,
            tags=list(match.tags),
            meta=dict(match.meta),
            strings=strings_data,
        )
    
    def add_rule(self, rule_source: str, namespace: str = "custom") -> bool:
        """
        Add a custom YARA rule at runtime.
        
        Args:
            rule_source: YARA rule source code
            namespace: Namespace for the rule
            
        Returns:
            True if rule was added successfully
        """
        if not YARA_AVAILABLE:
            return False
        
        try:
            new_rules = yara.compile(source=rule_source)
            logger.info(f"Added custom YARA rule in namespace '{namespace}'")
            return True
        except yara.Error as e:
            logger.error(f"Failed to add YARA rule: {e}")
            return False
    
    def get_stats(self) -> Dict[str, Any]:
        """Get statistics about loaded rules."""
        return {
            "available": self.available,
            "rule_files": self.rule_count,
            "rules_by_category": dict(self.rules_by_category),
            "load_errors": len(self.load_errors),
            "error_details": self.load_errors[:10] if self.load_errors else [],
        }
