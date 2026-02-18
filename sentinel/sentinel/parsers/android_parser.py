"""Android APK parser."""

import math
import re
import zipfile
import xml.etree.ElementTree as ET
from collections import Counter
from pathlib import Path
from typing import Any, Dict, List, Set, Optional

from .base import Parser, ParsedFile, FileType
from .detector import register_parser
from ..utils.hash import compute_multi_hash
from ..utils.errors import ParserError
from ..logging import get_logger

logger = get_logger(__name__)


class AndroidParser(Parser):
    """
    Parser for Android APK files.
    
    Extracts:
    - Package name and version
    - Permissions (especially dangerous ones)
    - Activities, services, receivers
    - Native libraries
    - Certificates
    - URLs and network indicators
    """
    
    DANGEROUS_PERMISSIONS = [
        "android.permission.READ_SMS",
        "android.permission.SEND_SMS",
        "android.permission.RECEIVE_SMS",
        "android.permission.READ_CONTACTS",
        "android.permission.READ_CALL_LOG",
        "android.permission.RECORD_AUDIO",
        "android.permission.CAMERA",
        "android.permission.ACCESS_FINE_LOCATION",
        "android.permission.ACCESS_COARSE_LOCATION",
        "android.permission.READ_EXTERNAL_STORAGE",
        "android.permission.WRITE_EXTERNAL_STORAGE",
        "android.permission.READ_PHONE_STATE",
        "android.permission.CALL_PHONE",
        "android.permission.PROCESS_OUTGOING_CALLS",
        "android.permission.RECEIVE_BOOT_COMPLETED",
        "android.permission.SYSTEM_ALERT_WINDOW",
        "android.permission.BIND_ACCESSIBILITY_SERVICE",
        "android.permission.BIND_DEVICE_ADMIN",
        "android.permission.REQUEST_INSTALL_PACKAGES",
        "android.permission.INTERNET",
    ]
    
    SUSPICIOUS_INTENTS = [
        "android.intent.action.BOOT_COMPLETED",
        "android.intent.action.NEW_OUTGOING_CALL",
        "android.provider.Telephony.SMS_RECEIVED",
        "android.intent.action.SCREEN_OFF",
        "android.intent.action.USER_PRESENT",
    ]
    
    @property
    def name(self) -> str:
        return "android_parser"
    
    @property
    def supported_extensions(self) -> Set[str]:
        return {".apk", ".xapk", ".apks"}
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.ANDROID}
    
    def can_parse(self, file_path: str) -> bool:
        try:
            with zipfile.ZipFile(file_path, 'r') as zf:
                return "AndroidManifest.xml" in zf.namelist() or "classes.dex" in zf.namelist()
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
        permissions = []
        embedded_files = []
        raw_features = {}
        
        try:
            with zipfile.ZipFile(path, 'r') as zf:
                file_list = zf.namelist()
                metadata["file_count"] = len(file_list)
                
                dex_files = [f for f in file_list if f.endswith('.dex')]
                metadata["dex_count"] = len(dex_files)
                
                so_files = [f for f in file_list if f.endswith('.so')]
                metadata["native_libs"] = len(so_files)
                if so_files:
                    metadata["native_lib_names"] = so_files[:10]
                
                manifest_info = self._parse_manifest(zf)
                if manifest_info:
                    metadata.update(manifest_info.get("metadata", {}))
                    permissions = manifest_info.get("permissions", [])
                    
                    dangerous = [p for p in permissions if p in self.DANGEROUS_PERMISSIONS]
                    if dangerous:
                        metadata["dangerous_permissions"] = dangerous
                        raw_features["dangerous_permission_count"] = len(dangerous)
                    
                    if manifest_info.get("receivers"):
                        for receiver in manifest_info["receivers"]:
                            if any(intent in str(receiver) for intent in self.SUSPICIOUS_INTENTS):
                                metadata.setdefault("suspicious_receivers", []).append(receiver)
                
                for dex_file in dex_files[:3]:
                    try:
                        dex_content = zf.read(dex_file)
                        dex_strings = self._extract_dex_strings(dex_content)
                        strings.extend(dex_strings)
                        
                        urls.extend(self._extract_urls("\n".join(dex_strings)))
                        ips.extend(self._extract_ips("\n".join(dex_strings)))
                        domains.extend(self._extract_domains("\n".join(dex_strings)))
                    except Exception as e:
                        errors.append(f"DEX parsing error: {e}")
                
                cert_files = [f for f in file_list if f.startswith("META-INF/") and 
                             (f.endswith(".RSA") or f.endswith(".DSA") or f.endswith(".SF"))]
                if cert_files:
                    metadata["has_signature"] = True
                    metadata["cert_files"] = cert_files
                
                assets = [f for f in file_list if f.startswith("assets/")]
                if assets:
                    metadata["asset_count"] = len(assets)
                    suspicious_assets = [a for a in assets if any(
                        ext in a.lower() for ext in ['.dex', '.jar', '.so', '.apk', '.exe', '.sh']
                    )]
                    if suspicious_assets:
                        metadata["suspicious_assets"] = suspicious_assets
                        
        except zipfile.BadZipFile:
            errors.append("Invalid APK/ZIP file")
        except Exception as e:
            errors.append(f"APK parsing error: {e}")
        
        strings = list(set(strings))[:500]
        urls = list(set(urls))[:50]
        ips = list(set(ips))[:50]
        domains = list(set(domains))[:50]
        
        with open(path, "rb") as f:
            entropy = self._calculate_entropy(f.read())
        
        return ParsedFile(
            file_path=str(path.absolute()),
            file_name=path.name,
            file_type=FileType.ANDROID,
            file_size=file_size,
            hashes=hashes,
            entropy=entropy,
            strings=strings,
            urls=urls,
            ips=ips,
            domains=domains,
            permissions=permissions,
            embedded_files=embedded_files,
            metadata=metadata,
            raw_features=raw_features,
            errors=errors,
        )
    
    def _parse_manifest(self, zf: zipfile.ZipFile) -> Optional[Dict]:
        """Parse AndroidManifest.xml (binary XML format)."""
        result = {"metadata": {}, "permissions": [], "receivers": []}
        
        try:
            manifest_data = zf.read("AndroidManifest.xml")
            
            if manifest_data[:4] == b'\x03\x00\x08\x00':
                return self._parse_binary_manifest(manifest_data)
            else:
                return self._parse_text_manifest(manifest_data.decode("utf-8", errors="ignore"))
                
        except KeyError:
            return None
        except Exception as e:
            logger.warning(f"Manifest parsing error: {e}")
            return None
    
    def _parse_binary_manifest(self, data: bytes) -> Dict:
        """Parse binary Android manifest (basic extraction)."""
        result = {"metadata": {}, "permissions": [], "receivers": []}
        
        text = data.decode("utf-8", errors="ignore")
        
        for perm in self.DANGEROUS_PERMISSIONS + [
            "android.permission.INTERNET",
            "android.permission.ACCESS_NETWORK_STATE",
        ]:
            if perm in text:
                result["permissions"].append(perm)
        
        if "versionCode" in text:
            result["metadata"]["has_version"] = True
        
        for intent in self.SUSPICIOUS_INTENTS:
            if intent in text:
                result["receivers"].append(intent)
        
        return result
    
    def _parse_text_manifest(self, xml_content: str) -> Dict:
        """Parse text XML manifest."""
        result = {"metadata": {}, "permissions": [], "receivers": []}
        
        try:
            root = ET.fromstring(xml_content)
            ns = {"android": "http://schemas.android.com/apk/res/android"}
            
            result["metadata"]["package"] = root.get("package", "unknown")
            result["metadata"]["version_code"] = root.get(
                "{http://schemas.android.com/apk/res/android}versionCode", "unknown"
            )
            
            for perm in root.findall(".//uses-permission"):
                perm_name = perm.get("{http://schemas.android.com/apk/res/android}name")
                if perm_name:
                    result["permissions"].append(perm_name)
            
            for receiver in root.findall(".//receiver"):
                receiver_name = receiver.get("{http://schemas.android.com/apk/res/android}name")
                if receiver_name:
                    result["receivers"].append(receiver_name)
                    
        except ET.ParseError:
            pass
        
        return result
    
    def _extract_dex_strings(self, dex_content: bytes) -> List[str]:
        """Extract strings from DEX file."""
        strings = []
        
        string_pattern = rb'[\x20-\x7e]{6,}'
        matches = re.findall(string_pattern, dex_content)
        
        for match in matches:
            try:
                s = match.decode("utf-8")
                if not s.startswith("Ljava/") and not s.startswith("Landroid/"):
                    strings.append(s)
            except Exception:
                pass
        
        return strings[:1000]
    
    def _extract_urls(self, content: str) -> List[str]:
        """Extract URLs."""
        url_pattern = r'https?://[^\s\'"<>)\]\\]{5,200}'
        urls = re.findall(url_pattern, content)
        return list(set(urls))
    
    def _extract_ips(self, content: str) -> List[str]:
        """Extract IP addresses."""
        ip_pattern = r'\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b'
        ips = re.findall(ip_pattern, content)
        filtered = [ip for ip in ips if not ip.startswith(('0.', '127.', '255.', '10.'))]
        return list(set(filtered))
    
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
        return list(set(domains))
    
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


register_parser(FileType.ANDROID, AndroidParser)
