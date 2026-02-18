"""PE (Portable Executable) file parser for Windows executables."""

import math
import re
from collections import Counter
from pathlib import Path
from typing import Any, Dict, List, Optional, Set

from .base import Parser, ParsedFile, FileType
from .detector import register_parser
from ..utils.hash import compute_multi_hash
from ..utils.errors import ParserError
from ..logging import get_logger

logger = get_logger(__name__)


class PEParser(Parser):
    """
    Parser for PE files (EXE, DLL, SYS).
    
    Extracts:
    - PE headers and metadata
    - Import/export tables
    - Section information
    - Strings and indicators
    - Entropy analysis
    """
    
    @property
    def name(self) -> str:
        return "pe_parser"
    
    @property
    def supported_extensions(self) -> Set[str]:
        return {".exe", ".dll", ".sys", ".scr", ".ocx", ".drv"}
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE}
    
    def can_parse(self, file_path: str) -> bool:
        try:
            with open(file_path, "rb") as f:
                header = f.read(2)
            return header == b"MZ"
        except Exception:
            return False
    
    def parse(self, file_path: str) -> ParsedFile:
        path = Path(file_path)
        errors = []
        
        try:
            import pefile
        except ImportError:
            raise ParserError("pefile library not installed. Run: pip install pefile")
        
        hashes = compute_multi_hash(file_path)
        file_size = path.stat().st_size
        
        imports = []
        exports = []
        sections = []
        strings = []
        urls = []
        ips = []
        domains = []
        metadata = {}
        raw_features = {}
        
        pe = None
        try:
            pe = pefile.PE(file_path)
            
            metadata["machine"] = hex(pe.FILE_HEADER.Machine)
            metadata["timestamp"] = pe.FILE_HEADER.TimeDateStamp
            metadata["characteristics"] = hex(pe.FILE_HEADER.Characteristics)
            metadata["subsystem"] = pe.OPTIONAL_HEADER.Subsystem if hasattr(pe, 'OPTIONAL_HEADER') else None
            metadata["dll"] = bool(pe.FILE_HEADER.Characteristics & 0x2000)
            metadata["entry_point"] = hex(pe.OPTIONAL_HEADER.AddressOfEntryPoint) if hasattr(pe, 'OPTIONAL_HEADER') else None
            
            if hasattr(pe, 'DIRECTORY_ENTRY_IMPORT'):
                for entry in pe.DIRECTORY_ENTRY_IMPORT:
                    dll_name = entry.dll.decode('utf-8', errors='ignore')
                    for imp in entry.imports:
                        if imp.name:
                            func_name = imp.name.decode('utf-8', errors='ignore')
                            imports.append(f"{dll_name}:{func_name}")
            
            if hasattr(pe, 'DIRECTORY_ENTRY_EXPORT'):
                for exp in pe.DIRECTORY_ENTRY_EXPORT.symbols:
                    if exp.name:
                        exports.append(exp.name.decode('utf-8', errors='ignore'))
            
            for section in pe.sections:
                section_name = section.Name.decode('utf-8', errors='ignore').rstrip('\x00')
                section_entropy = section.get_entropy()
                section_data = {
                    "name": section_name,
                    "virtual_address": hex(section.VirtualAddress),
                    "virtual_size": section.Misc_VirtualSize,
                    "raw_size": section.SizeOfRawData,
                    "entropy": round(section_entropy, 4),
                    "characteristics": hex(section.Characteristics),
                }
                sections.append(section_data)
            
            raw_features["import_count"] = len(imports)
            raw_features["export_count"] = len(exports)
            raw_features["section_count"] = len(sections)
            raw_features["avg_section_entropy"] = (
                sum(s["entropy"] for s in sections) / len(sections) if sections else 0
            )
            
            suspicious_imports = self._get_suspicious_imports(imports)
            raw_features["suspicious_import_count"] = len(suspicious_imports)
            metadata["suspicious_imports"] = suspicious_imports
            
        except pefile.PEFormatError as e:
            errors.append(f"PE parse error: {e}")
        except Exception as e:
            errors.append(f"Unexpected PE parse error: {e}")
        finally:
            if pe:
                pe.close()
        
        try:
            with open(file_path, "rb") as f:
                content = f.read()
            
            strings = self._extract_strings(content)
            urls = self._extract_urls(content)
            ips = self._extract_ips(content)
            domains = self._extract_domains(content)
            
        except Exception as e:
            errors.append(f"String extraction error: {e}")
        
        entropy = self._calculate_file_entropy(file_path)
        
        return ParsedFile(
            file_path=str(path.absolute()),
            file_name=path.name,
            file_type=FileType.PE,
            file_size=file_size,
            hashes=hashes,
            entropy=entropy,
            strings=strings[:1000],
            urls=urls,
            ips=ips,
            domains=domains,
            imports=imports,
            exports=exports,
            sections=sections,
            metadata=metadata,
            raw_features=raw_features,
            errors=errors,
        )
    
    def _extract_strings(self, content: bytes, min_length: int = 4) -> List[str]:
        """Extract printable strings from binary content."""
        ascii_pattern = rb'[\x20-\x7e]{' + str(min_length).encode() + rb',}'
        ascii_strings = re.findall(ascii_pattern, content)
        
        strings = []
        for s in ascii_strings[:2000]:
            try:
                decoded = s.decode('ascii')
                if not decoded.isspace():
                    strings.append(decoded)
            except Exception:
                pass
        
        return strings
    
    def _extract_urls(self, content: bytes) -> List[str]:
        """Extract URLs from content."""
        url_pattern = rb'https?://[^\s\x00<>"\']{5,200}'
        urls = []
        for match in re.findall(url_pattern, content):
            try:
                urls.append(match.decode('utf-8', errors='ignore'))
            except Exception:
                pass
        return list(set(urls))[:50]
    
    def _extract_ips(self, content: bytes) -> List[str]:
        """Extract IP addresses from content."""
        ip_pattern = rb'\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b'
        ips = []
        for match in re.findall(ip_pattern, content):
            try:
                ip = match.decode('utf-8')
                if not ip.startswith(('0.', '127.', '255.')):
                    ips.append(ip)
            except Exception:
                pass
        return list(set(ips))[:50]
    
    def _extract_domains(self, content: bytes) -> List[str]:
        """Extract domain names from content."""
        domain_pattern = rb'\b[a-zA-Z0-9][-a-zA-Z0-9]{0,62}(?:\.[a-zA-Z0-9][-a-zA-Z0-9]{0,62})+\b'
        domains = []
        for match in re.findall(domain_pattern, content):
            try:
                domain = match.decode('utf-8').lower()
                if '.' in domain and len(domain) > 4:
                    tld = domain.split('.')[-1]
                    if len(tld) >= 2 and tld.isalpha():
                        domains.append(domain)
            except Exception:
                pass
        return list(set(domains))[:50]
    
    def _calculate_file_entropy(self, file_path: str) -> float:
        """Calculate Shannon entropy of file."""
        try:
            with open(file_path, "rb") as f:
                data = f.read()
            
            if not data:
                return 0.0
            
            counter = Counter(data)
            length = len(data)
            entropy = 0.0
            
            for count in counter.values():
                if count > 0:
                    p = count / length
                    entropy -= p * math.log2(p)
            
            return round(entropy, 4)
        except Exception:
            return 0.0
    
    def _get_suspicious_imports(self, imports: List[str]) -> List[str]:
        """Identify suspicious API imports."""
        suspicious_apis = {
            "createremotethread", "virtualallocex", "writeprocessmemory",
            "ntcreatethreadex", "rtlcreateuserthreadex", "queueuserapc",
            "setwindowshookex", "createprocess", "shellexecute",
            "winexec", "loadlibrary", "getprocaddress", "virtualprotect",
            "internetopen", "internetconnect", "httpopen", "urldownload",
            "wsastartup", "socket", "connect", "send", "recv",
            "createfile", "writefile", "readfile", "deletefile",
            "regsetvalue", "regcreatekey", "cryptencrypt", "cryptdecrypt",
            "adjusttokenprivileges", "lookupprivilegevalue", "openprocesstoken",
            "isdebuggerpresent", "checkremotedebuggerpresent", "outputdebugstring",
            "gettickcount", "queryperformancecounter", "sleep",
        }
        
        found = []
        for imp in imports:
            func_name = imp.split(":")[-1].lower()
            if any(sus in func_name for sus in suspicious_apis):
                found.append(imp)
        
        return found


register_parser(FileType.PE, PEParser)
