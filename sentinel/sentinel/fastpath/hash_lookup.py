"""Hash-based lookup for known malware and clean files."""

import json
import time
from dataclasses import dataclass
from enum import Enum
from pathlib import Path
from typing import Dict, List, Optional, Set

from ..logging import get_logger

logger = get_logger(__name__)


class HashVerdict(str, Enum):
    """Hash lookup verdict."""
    KNOWN_MALWARE = "known_malware"
    KNOWN_CLEAN = "known_clean"
    UNKNOWN = "unknown"


@dataclass
class HashLookupResult:
    """Result of hash lookup."""
    verdict: HashVerdict
    hash_value: str
    hash_type: str
    source: str = ""
    malware_family: str = ""
    confidence: float = 1.0
    metadata: Dict = None
    
    def __post_init__(self):
        if self.metadata is None:
            self.metadata = {}


class HashLookup:
    """
    Hash-based malware lookup.
    
    Features:
    - Local hash database (JSON-based)
    - Known malware hash sets
    - Known clean/whitelist hashes
    - Extensible for API integration (VirusTotal, etc.)
    """
    
    def __init__(
        self,
        malware_hashes_path: Optional[str] = None,
        clean_hashes_path: Optional[str] = None,
    ):
        """
        Initialize hash lookup.
        
        Args:
            malware_hashes_path: Path to malware hashes JSON
            clean_hashes_path: Path to clean/whitelist hashes JSON
        """
        self.malware_hashes: Dict[str, Dict] = {}
        self.clean_hashes: Set[str] = set()
        
        self._load_hashes(malware_hashes_path, clean_hashes_path)
        self._load_builtin_hashes()
    
    def _load_hashes(
        self,
        malware_path: Optional[str],
        clean_path: Optional[str]
    ) -> None:
        """Load hash databases from files."""
        if malware_path:
            path = Path(malware_path)
            if path.exists():
                try:
                    with open(path, "r") as f:
                        data = json.load(f)
                    self.malware_hashes.update(data.get("hashes", {}))
                    logger.info(f"Loaded {len(self.malware_hashes)} malware hashes")
                except Exception as e:
                    logger.error(f"Failed to load malware hashes: {e}")
        
        if clean_path:
            path = Path(clean_path)
            if path.exists():
                try:
                    with open(path, "r") as f:
                        data = json.load(f)
                    self.clean_hashes.update(data.get("hashes", []))
                    logger.info(f"Loaded {len(self.clean_hashes)} clean hashes")
                except Exception as e:
                    logger.error(f"Failed to load clean hashes: {e}")
    
    def _load_builtin_hashes(self) -> None:
        """Load built-in known malware hashes (for demo/testing)."""
        # These are example hashes - in production, load from threat intel feeds
        builtin_malware = {
            # EICAR test file
            "275a021bbfb6489e54d471899f7db9d1663fc695ec2fe2a2c4538aabf651fd0f": {
                "family": "EICAR-Test-File",
                "source": "builtin",
                "severity": "test",
            },
            # WannaCry sample hash (example)
            "ed01ebfbc9eb5bbea545af4d01bf5f1071661840480439c6e5babe8e080e41aa": {
                "family": "WannaCry",
                "source": "builtin",
                "severity": "critical",
            },
        }
        
        for hash_val, info in builtin_malware.items():
            if hash_val not in self.malware_hashes:
                self.malware_hashes[hash_val] = info
    
    def lookup(self, file_hash: str, hash_type: str = "sha256") -> HashLookupResult:
        """
        Look up a file hash.
        
        Args:
            file_hash: Hash value to look up
            hash_type: Type of hash (sha256, md5, sha1)
            
        Returns:
            HashLookupResult with verdict
        """
        file_hash = file_hash.lower()
        
        # Check malware database
        if file_hash in self.malware_hashes:
            info = self.malware_hashes[file_hash]
            return HashLookupResult(
                verdict=HashVerdict.KNOWN_MALWARE,
                hash_value=file_hash,
                hash_type=hash_type,
                source=info.get("source", "local"),
                malware_family=info.get("family", "Unknown"),
                confidence=1.0,
                metadata=info,
            )
        
        # Check clean/whitelist
        if file_hash in self.clean_hashes:
            return HashLookupResult(
                verdict=HashVerdict.KNOWN_CLEAN,
                hash_value=file_hash,
                hash_type=hash_type,
                source="whitelist",
                confidence=1.0,
            )
        
        return HashLookupResult(
            verdict=HashVerdict.UNKNOWN,
            hash_value=file_hash,
            hash_type=hash_type,
        )
    
    def lookup_multiple(
        self,
        hashes: Dict[str, str]
    ) -> Dict[str, HashLookupResult]:
        """
        Look up multiple hashes.
        
        Args:
            hashes: Dict of hash_type -> hash_value
            
        Returns:
            Dict of hash_type -> HashLookupResult
        """
        results = {}
        for hash_type, hash_value in hashes.items():
            results[hash_type] = self.lookup(hash_value, hash_type)
        return results
    
    def add_malware_hash(
        self,
        file_hash: str,
        family: str = "Unknown",
        source: str = "user",
        metadata: Optional[Dict] = None
    ) -> None:
        """Add a hash to the malware database."""
        self.malware_hashes[file_hash.lower()] = {
            "family": family,
            "source": source,
            **(metadata or {}),
        }
    
    def add_clean_hash(self, file_hash: str) -> None:
        """Add a hash to the clean/whitelist."""
        self.clean_hashes.add(file_hash.lower())
    
    def save_database(self, output_path: str) -> None:
        """Save current hash database to file."""
        data = {
            "malware_hashes": self.malware_hashes,
            "clean_hashes": list(self.clean_hashes),
            "version": "1.0",
        }
        
        with open(output_path, "w") as f:
            json.dump(data, f, indent=2)
        
        logger.info(f"Saved hash database to {output_path}")
    
    @property
    def stats(self) -> Dict:
        """Get database statistics."""
        return {
            "malware_hashes": len(self.malware_hashes),
            "clean_hashes": len(self.clean_hashes),
        }
