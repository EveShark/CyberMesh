"""PDF file parser."""

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


class PDFParser(Parser):
    """
    Parser for PDF files.
    
    Extracts:
    - PDF metadata
    - Embedded JavaScript
    - URLs and network indicators
    - Embedded files/streams
    - Suspicious objects (OpenAction, Launch, etc.)
    """
    
    SUSPICIOUS_KEYWORDS = [
        "/JavaScript", "/JS", "/OpenAction", "/AA", "/Launch",
        "/EmbeddedFile", "/AcroForm", "/XFA", "/URI", "/SubmitForm",
        "/GoToR", "/GoToE", "/RichMedia", "/ObjStm", "/XObject",
    ]
    
    @property
    def name(self) -> str:
        return "pdf_parser"
    
    @property
    def supported_extensions(self) -> Set[str]:
        return {".pdf"}
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PDF}
    
    def can_parse(self, file_path: str) -> bool:
        try:
            with open(file_path, "rb") as f:
                header = f.read(5)
            return header == b"%PDF-"
        except Exception:
            return False
    
    def parse(self, file_path: str) -> ParsedFile:
        path = Path(file_path)
        errors = []
        
        hashes = compute_multi_hash(file_path)
        file_size = path.stat().st_size
        
        with open(file_path, "rb") as f:
            raw_content = f.read()
        
        metadata = {}
        scripts = []
        urls = []
        ips = []
        domains = []
        embedded_files = []
        strings = []
        raw_features = {}
        
        try:
            pdf_version = self._extract_version(raw_content)
            metadata["pdf_version"] = pdf_version
        except Exception as e:
            errors.append(f"Failed to extract PDF version: {e}")
        
        try:
            from pypdf import PdfReader
            reader = PdfReader(file_path)
            
            metadata["page_count"] = len(reader.pages)
            
            if reader.metadata:
                for key in ["/Title", "/Author", "/Subject", "/Creator", "/Producer"]:
                    if key in reader.metadata:
                        metadata[key.strip("/")] = str(reader.metadata[key])
            
            text_content = []
            for page in reader.pages[:10]:
                try:
                    text = page.extract_text()
                    if text:
                        text_content.append(text)
                except Exception:
                    pass
            
            if text_content:
                full_text = "\n".join(text_content)
                strings = [line.strip() for line in full_text.split("\n") if line.strip()][:500]
                urls.extend(self._extract_urls(full_text))
                ips.extend(self._extract_ips(full_text))
                domains.extend(self._extract_domains(full_text))
                
        except ImportError:
            errors.append("pypdf not installed, using basic parsing")
        except Exception as e:
            errors.append(f"pypdf parsing error: {e}")
        
        try:
            raw_text = raw_content.decode("latin-1", errors="ignore")
            
            suspicious_found = []
            for keyword in self.SUSPICIOUS_KEYWORDS:
                count = raw_text.count(keyword)
                if count > 0:
                    suspicious_found.append(f"{keyword}:{count}")
            
            if suspicious_found:
                metadata["suspicious_keywords"] = suspicious_found
                raw_features["suspicious_keyword_count"] = len(suspicious_found)
            
            js_content = self._extract_javascript(raw_content)
            if js_content:
                scripts.extend(js_content)
                metadata["has_javascript"] = True
                raw_features["javascript_count"] = len(js_content)
            
            raw_urls = self._extract_urls(raw_text)
            urls.extend(raw_urls)
            urls = list(set(urls))[:50]
            
            raw_ips = self._extract_ips(raw_text)
            ips.extend(raw_ips)
            ips = list(set(ips))[:50]
            
            stream_count = raw_text.count("stream")
            obj_count = raw_text.count(" obj")
            metadata["stream_count"] = stream_count
            metadata["object_count"] = obj_count
            
            if "/EmbeddedFile" in raw_text:
                embedded_files.append("embedded_file_detected")
                metadata["has_embedded_files"] = True
            
        except Exception as e:
            errors.append(f"Raw PDF parsing error: {e}")
        
        entropy = self._calculate_entropy(raw_content)
        
        return ParsedFile(
            file_path=str(path.absolute()),
            file_name=path.name,
            file_type=FileType.PDF,
            file_size=file_size,
            hashes=hashes,
            entropy=entropy,
            strings=strings,
            urls=urls,
            ips=ips,
            domains=domains,
            scripts=scripts,
            embedded_files=embedded_files,
            metadata=metadata,
            raw_features=raw_features,
            errors=errors,
        )
    
    def _extract_version(self, content: bytes) -> str:
        """Extract PDF version from header."""
        header = content[:20].decode("ascii", errors="ignore")
        match = re.search(r"%PDF-(\d+\.\d+)", header)
        if match:
            return match.group(1)
        return "unknown"
    
    def _extract_javascript(self, content: bytes) -> List[str]:
        """Extract JavaScript from PDF."""
        js_content = []
        text = content.decode("latin-1", errors="ignore")
        
        js_patterns = [
            r'/JavaScript\s*<<[^>]*>>\s*stream\s*(.*?)\s*endstream',
            r'/JS\s*\((.*?)\)',
            r'/JS\s*<<[^>]*>>\s*stream\s*(.*?)\s*endstream',
        ]
        
        for pattern in js_patterns:
            matches = re.findall(pattern, text, re.DOTALL | re.IGNORECASE)
            for match in matches:
                cleaned = match.strip()[:2000]
                if cleaned and len(cleaned) > 10:
                    js_content.append(cleaned)
        
        return js_content[:10]
    
    def _extract_urls(self, content: str) -> List[str]:
        """Extract URLs from content."""
        url_pattern = r'https?://[^\s\'"<>)\]\\]{5,200}'
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


register_parser(FileType.PDF, PDFParser)
