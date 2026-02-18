"""Office document parser for DOCX, XLSX, PPTX, DOC, XLS, PPT."""

import math
import re
import zipfile
from collections import Counter
from pathlib import Path
from typing import Any, Dict, List, Set, Optional
import xml.etree.ElementTree as ET

from .base import Parser, ParsedFile, FileType
from .detector import register_parser
from ..utils.hash import compute_multi_hash
from ..utils.errors import ParserError
from ..logging import get_logger

logger = get_logger(__name__)


class OfficeParser(Parser):
    """
    Parser for Microsoft Office documents.
    
    Supports:
    - Modern formats: DOCX, XLSX, PPTX (ZIP-based OOXML)
    - Legacy formats: DOC, XLS, PPT (OLE2)
    
    Extracts:
    - Document metadata
    - Embedded macros (VBA)
    - External links and URLs
    - Embedded objects
    - DDE links
    """
    
    VBA_SUSPICIOUS_PATTERNS = [
        ("Shell", "Shell command execution"),
        ("WScript.Shell", "WScript shell access"),
        ("Powershell", "PowerShell execution"),
        ("cmd.exe", "Command prompt execution"),
        ("CreateObject", "COM object creation"),
        ("GetObject", "COM object access"),
        ("AutoOpen", "Auto-execute on open"),
        ("Auto_Open", "Auto-execute on open"),
        ("Document_Open", "Auto-execute on document open"),
        ("Workbook_Open", "Auto-execute on workbook open"),
        ("URLDownloadToFile", "File download API"),
        ("MSXML2.XMLHTTP", "HTTP request capability"),
        ("ADODB.Stream", "File stream operations"),
        ("Environ", "Environment variable access"),
        ("CallByName", "Dynamic method invocation"),
        ("Chr(", "Character code obfuscation"),
        ("ChrW(", "Wide character obfuscation"),
    ]
    
    @property
    def name(self) -> str:
        return "office_parser"
    
    @property
    def supported_extensions(self) -> Set[str]:
        return {".docx", ".xlsx", ".pptx", ".docm", ".xlsm", ".pptm",
                ".doc", ".xls", ".ppt", ".rtf"}
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.OFFICE}
    
    def can_parse(self, file_path: str) -> bool:
        ext = Path(file_path).suffix.lower()
        if ext in self.supported_extensions:
            return True
        
        try:
            with open(file_path, "rb") as f:
                header = f.read(8)
            if header[:4] == b"PK\x03\x04":
                return True
            if header[:8] == b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1":
                return True
        except Exception:
            pass
        
        return False
    
    def parse(self, file_path: str) -> ParsedFile:
        path = Path(file_path)
        errors = []
        
        hashes = compute_multi_hash(file_path)
        file_size = path.stat().st_size
        
        with open(file_path, "rb") as f:
            header = f.read(8)
        
        if header[:4] == b"PK\x03\x04":
            return self._parse_ooxml(path, hashes, file_size, errors)
        elif header[:8] == b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1":
            return self._parse_ole(path, hashes, file_size, errors)
        else:
            errors.append("Unknown Office format")
            return ParsedFile(
                file_path=str(path.absolute()),
                file_name=path.name,
                file_type=FileType.OFFICE,
                file_size=file_size,
                hashes=hashes,
                errors=errors,
            )
    
    def _parse_ooxml(self, path: Path, hashes: Dict, file_size: int, errors: List[str]) -> ParsedFile:
        """Parse OOXML (ZIP-based) Office documents."""
        metadata = {"format": "OOXML"}
        strings = []
        urls = []
        ips = []
        domains = []
        macros = []
        scripts = []
        embedded_files = []
        raw_features = {}
        
        try:
            with zipfile.ZipFile(path, 'r') as zf:
                file_list = zf.namelist()
                metadata["internal_files"] = len(file_list)
                
                core_xml = self._read_zip_file(zf, "docProps/core.xml")
                if core_xml:
                    self._parse_core_metadata(core_xml, metadata)
                
                app_xml = self._read_zip_file(zf, "docProps/app.xml")
                if app_xml:
                    self._parse_app_metadata(app_xml, metadata)
                
                vba_files = [f for f in file_list if "vbaProject" in f.lower() or f.endswith('.bin')]
                if vba_files:
                    metadata["has_macros"] = True
                    raw_features["macro_file_count"] = len(vba_files)
                    
                    for vba_file in vba_files:
                        try:
                            vba_content = zf.read(vba_file)
                            macro_info = self._extract_vba_strings(vba_content)
                            if macro_info:
                                macros.extend(macro_info["strings"])
                                if macro_info.get("suspicious"):
                                    metadata["suspicious_vba"] = macro_info["suspicious"]
                        except Exception as e:
                            errors.append(f"VBA extraction error: {e}")
                
                for doc_file in ["word/document.xml", "xl/sharedStrings.xml", "ppt/presentation.xml"]:
                    content = self._read_zip_file(zf, doc_file)
                    if content:
                        text = self._strip_xml_tags(content)
                        strings.extend(text.split()[:500])
                        urls.extend(self._extract_urls(content))
                
                for rel_file in [f for f in file_list if f.endswith('.rels')]:
                    rel_content = self._read_zip_file(zf, rel_file)
                    if rel_content:
                        ext_links = self._extract_external_links(rel_content)
                        urls.extend(ext_links)
                        if ext_links:
                            metadata["has_external_links"] = True
                
                for f in file_list:
                    if "/embeddings/" in f or "/oleObject" in f:
                        embedded_files.append(f)
                
                if embedded_files:
                    metadata["embedded_objects"] = len(embedded_files)
                    
        except zipfile.BadZipFile:
            errors.append("Invalid ZIP/OOXML file")
        except Exception as e:
            errors.append(f"OOXML parsing error: {e}")
        
        urls = list(set(urls))[:50]
        
        with open(path, "rb") as f:
            entropy = self._calculate_entropy(f.read())
        
        return ParsedFile(
            file_path=str(path.absolute()),
            file_name=path.name,
            file_type=FileType.OFFICE,
            file_size=file_size,
            hashes=hashes,
            entropy=entropy,
            strings=strings[:500],
            urls=urls,
            ips=ips,
            domains=domains,
            macros=macros,
            scripts=scripts,
            embedded_files=embedded_files,
            metadata=metadata,
            raw_features=raw_features,
            errors=errors,
        )
    
    def _parse_ole(self, path: Path, hashes: Dict, file_size: int, errors: List[str]) -> ParsedFile:
        """Parse OLE2 (legacy) Office documents."""
        metadata = {"format": "OLE2"}
        macros = []
        urls = []
        raw_features = {}
        
        try:
            import olefile
            
            with olefile.OleFileIO(path) as ole:
                metadata["ole_streams"] = len(ole.listdir())
                
                if ole.exists("Macros") or ole.exists("_VBA_PROJECT_CUR"):
                    metadata["has_macros"] = True
                
                for entry in ole.listdir():
                    stream_path = "/".join(entry)
                    if any(vba in stream_path.lower() for vba in ["vba", "macro", "module"]):
                        try:
                            data = ole.openstream(entry).read()
                            macro_info = self._extract_vba_strings(data)
                            if macro_info:
                                macros.extend(macro_info["strings"])
                                if macro_info.get("suspicious"):
                                    metadata["suspicious_vba"] = macro_info["suspicious"]
                        except Exception:
                            pass
                            
        except ImportError:
            errors.append("olefile not installed, limited OLE2 parsing")
            
            with open(path, "rb") as f:
                content = f.read()
            
            if b"VBA" in content or b"Macros" in content:
                metadata["has_macros"] = True
                macro_info = self._extract_vba_strings(content)
                if macro_info:
                    macros.extend(macro_info["strings"])
                    if macro_info.get("suspicious"):
                        metadata["suspicious_vba"] = macro_info["suspicious"]
                        
        except Exception as e:
            errors.append(f"OLE2 parsing error: {e}")
        
        with open(path, "rb") as f:
            entropy = self._calculate_entropy(f.read())
        
        return ParsedFile(
            file_path=str(path.absolute()),
            file_name=path.name,
            file_type=FileType.OFFICE,
            file_size=file_size,
            hashes=hashes,
            entropy=entropy,
            macros=macros,
            urls=urls,
            metadata=metadata,
            raw_features=raw_features,
            errors=errors,
        )
    
    def _read_zip_file(self, zf: zipfile.ZipFile, path: str) -> Optional[str]:
        """Read a file from ZIP archive."""
        try:
            return zf.read(path).decode("utf-8", errors="ignore")
        except KeyError:
            return None
        except Exception:
            return None
    
    def _parse_core_metadata(self, xml_content: str, metadata: Dict) -> None:
        """Parse core.xml for document metadata."""
        try:
            root = ET.fromstring(xml_content)
            ns = {
                "cp": "http://schemas.openxmlformats.org/package/2006/metadata/core-properties",
                "dc": "http://purl.org/dc/elements/1.1/",
                "dcterms": "http://purl.org/dc/terms/",
            }
            
            for tag, key in [("dc:title", "title"), ("dc:creator", "author"),
                            ("dc:subject", "subject"), ("cp:lastModifiedBy", "last_modified_by")]:
                elem = root.find(tag, ns)
                if elem is not None and elem.text:
                    metadata[key] = elem.text
        except Exception:
            pass
    
    def _parse_app_metadata(self, xml_content: str, metadata: Dict) -> None:
        """Parse app.xml for application metadata."""
        try:
            root = ET.fromstring(xml_content)
            ns = {"ep": "http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"}
            
            app_elem = root.find("ep:Application", ns)
            if app_elem is not None and app_elem.text:
                metadata["application"] = app_elem.text
        except Exception:
            pass
    
    def _extract_vba_strings(self, content: bytes) -> Optional[Dict]:
        """Extract readable strings and suspicious patterns from VBA content."""
        result = {"strings": [], "suspicious": []}
        
        try:
            text = content.decode("latin-1", errors="ignore")
        except Exception:
            text = str(content)
        
        strings = re.findall(r'[\x20-\x7e]{4,}', text)
        result["strings"] = strings[:100]
        
        for pattern, description in self.VBA_SUSPICIOUS_PATTERNS:
            if pattern.lower() in text.lower():
                result["suspicious"].append(description)
        
        return result if result["strings"] or result["suspicious"] else None
    
    def _extract_external_links(self, xml_content: str) -> List[str]:
        """Extract external links from relationship files."""
        links = []
        
        url_pattern = r'Target="(https?://[^"]+)"'
        matches = re.findall(url_pattern, xml_content)
        links.extend(matches)
        
        external_pattern = r'TargetMode="External"[^>]*Target="([^"]+)"'
        matches = re.findall(external_pattern, xml_content)
        links.extend(matches)
        
        return links
    
    def _strip_xml_tags(self, xml_content: str) -> str:
        """Remove XML tags to get plain text."""
        return re.sub(r'<[^>]+>', ' ', xml_content)
    
    def _extract_urls(self, content: str) -> List[str]:
        """Extract URLs from content."""
        url_pattern = r'https?://[^\s\'"<>)\]\\]{5,200}'
        urls = re.findall(url_pattern, content)
        return list(set(urls))
    
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


register_parser(FileType.OFFICE, OfficeParser)
