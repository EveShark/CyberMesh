"""
Telemetry data sources for ML pipeline.

File-based ingestion for Phase 6.1 (Kafka ingestion in Phase 7).
Security-first: Validate paths, limit file sizes, handle errors gracefully.
"""

import json
import glob
from abc import ABC, abstractmethod
from pathlib import Path
from typing import List, Dict, Optional
from ..logging import get_logger


class TelemetrySource(ABC):
    """
    Abstract base class for telemetry data sources.
    
    Implementations:
    - FileTelemetrySource: Read from local files (Phase 6.1)
    - KafkaTelemetrySource: Read from Kafka topics (Phase 7)
    """
    
    @abstractmethod
    def get_network_flows(self, limit: int = 100) -> List[Dict]:
        """
        Retrieve network flow records.
        
        Args:
            limit: Maximum number of flows to return
            
        Returns:
            List of flow dictionaries with keys:
            - src_ip, dst_ip, src_port, dst_port
            - protocol, duration, packets_fwd, packets_bwd
            - bytes_fwd, bytes_bwd, syn_count, ack_count
            - timestamp
        """
        pass
    
    @abstractmethod
    def get_files(self, limit: int = 50) -> List[Dict]:
        """
        Retrieve file/malware metadata.
        
        Args:
            limit: Maximum number of files to return
            
        Returns:
            List of file metadata dictionaries
        """
        pass
    
    @abstractmethod
    def has_data(self) -> bool:
        """Check if source has available data."""
        pass


class FileTelemetrySource(TelemetrySource):
    """
    File-based telemetry source.
    
    Reads network flows and file metadata from JSON files in configured directories.
    
    Directory structure:
    data/telemetry/
    ├── flows/
    │   ├── benign/*.json
    │   └── ddos/*.json
    └── files/
        ├── benign/*.json
        └── malware/*.json
    
    Security:
    - Validates paths (no directory traversal)
    - Limits file sizes (max 10MB per file)
    - Handles malformed JSON gracefully
    - Logs all file access
    """
    
    MAX_FILE_SIZE = 10 * 1024 * 1024  # 10MB
    
    def __init__(self, flows_path: str, files_path: str):
        """
        Initialize file-based telemetry source.
        
        Args:
            flows_path: Path to network flows directory
            files_path: Path to files directory
            
        Raises:
            ValueError: If paths invalid or don't exist
        """
        self.flows_path = Path(flows_path).resolve()
        self.files_path = Path(files_path).resolve()
        self.logger = get_logger(__name__)
        
        # Validate paths exist
        if not self.flows_path.exists():
            self.logger.warning(f"Flows path does not exist: {self.flows_path}")
        if not self.files_path.exists():
            self.logger.warning(f"Files path does not exist: {self.files_path}")
        
        # State for polling (tracks position in files)
        self._file_index = 0
        self._all_files = []
        self._files_scanned = False
        
        self.logger.info(
            f"Initialized FileTelemetrySource: flows={self.flows_path}, files={self.files_path}"
        )
    
    def get_network_flows(self, limit: int = 100) -> List[Dict]:
        """
        Load network flow data from JSON files.
        
        Searches for .json files in:
        - flows/benign/
        - flows/ddos/
        - flows/ (root)
        
        Args:
            limit: Maximum flows to return
            
        Returns:
            List of flow dictionaries
        """
        flows = []
        
        # Search patterns (benign, ddos, root)
        patterns = [
            self.flows_path / "benign" / "*.json",
            self.flows_path / "ddos" / "*.json",
            self.flows_path / "*.json",
        ]
        
        for pattern in patterns:
            for file_path in glob.glob(str(pattern)):
                if len(flows) >= limit:
                    break
                
                try:
                    flows.extend(self._load_json_file(file_path, limit - len(flows)))
                except Exception as e:
                    self.logger.error(
                        f"Failed to load flows from {file_path}: {e}",
                        exc_info=True
                    )
        
        self.logger.info(f"Loaded {len(flows)} network flows")
        return flows[:limit]
    
    def get_files(self, limit: int = 50) -> List[Dict]:
        """
        Load file/malware metadata from JSON files.
        
        Searches for .json files in:
        - files/benign/
        - files/malware/
        - files/ (root)
        
        Args:
            limit: Maximum files to return
            
        Returns:
            List of file metadata dictionaries
        """
        files = []
        
        # Search patterns
        patterns = [
            self.files_path / "benign" / "*.json",
            self.files_path / "malware" / "*.json",
            self.files_path / "*.json",
        ]
        
        for pattern in patterns:
            for file_path in glob.glob(str(pattern)):
                if len(files) >= limit:
                    break
                
                try:
                    files.extend(self._load_json_file(file_path, limit - len(files)))
                except Exception as e:
                    self.logger.error(
                        f"Failed to load files from {file_path}: {e}",
                        exc_info=True
                    )
        
        self.logger.info(f"Loaded {len(files)} file metadata records")
        return files[:limit]
    
    def has_data(self) -> bool:
        """
        Check if any data files exist.
        
        Returns:
            True if at least one .json file found
        """
        # Check flows
        flow_patterns = [
            self.flows_path / "benign" / "*.json",
            self.flows_path / "ddos" / "*.json",
            self.flows_path / "*.json",
        ]
        
        for pattern in flow_patterns:
            if glob.glob(str(pattern)):
                return True
        
        # Check files
        file_patterns = [
            self.files_path / "benign" / "*.json",
            self.files_path / "malware" / "*.json",
            self.files_path / "*.json",
        ]
        
        for pattern in file_patterns:
            if glob.glob(str(pattern)):
                return True
        
        return False
    
    def poll(self, batch_size: int = 1000) -> Optional[List[Dict]]:
        """
        Poll for next batch of telemetry data (for continuous detection loop).
        
        Reads files incrementally, returning batch_size flows at a time.
        Cycles through all available files, then returns None when exhausted.
        
        Args:
            batch_size: Number of flows to return per poll
        
        Returns:
            List of flow dictionaries, or None if no more data
        """
        # Scan for files on first call
        if not self._files_scanned:
            self._scan_files()
        
        # No files found
        if not self._all_files:
            return None
        
        # Exhausted all files, reset and return None
        if self._file_index >= len(self._all_files):
            self.logger.debug("Poll: Exhausted all telemetry files, cycling back")
            self._file_index = 0  # Cycle back to start
            return None
        
        # Load next file
        file_path = self._all_files[self._file_index]
        self._file_index += 1
        
        try:
            flows = self._load_json_file(file_path, batch_size)
            if flows:
                self.logger.debug(f"Poll: Loaded {len(flows)} flows from {file_path}")
                return flows
            else:
                # Empty file, try next
                return self.poll(batch_size)
        except Exception as e:
            self.logger.error(f"Poll: Failed to load {file_path}: {e}")
            # Try next file
            return self.poll(batch_size)
    
    def _scan_files(self) -> None:
        """Scan directories and build list of all telemetry files."""
        patterns = [
            self.flows_path / "benign" / "*.json",
            self.flows_path / "ddos" / "*.json",
            self.flows_path / "*.json",
        ]
        
        all_files = []
        for pattern in patterns:
            all_files.extend(glob.glob(str(pattern)))
        
        self._all_files = sorted(all_files)  # Sort for deterministic order
        self._files_scanned = True
        
        self.logger.info(f"Scanned {len(self._all_files)} telemetry files")
    
    def _load_json_file(self, file_path: str, limit: int) -> List[Dict]:
        """
        Load JSON file with security checks.
        
        Args:
            file_path: Path to JSON file
            limit: Maximum records to load
            
        Returns:
            List of dictionaries from JSON
            
        Raises:
            ValueError: If file too large or path invalid
            json.JSONDecodeError: If JSON malformed
        """
        path = Path(file_path).resolve()
        
        # Security: Validate path (no traversal outside telemetry dirs)
        if not (str(path).startswith(str(self.flows_path)) or 
                str(path).startswith(str(self.files_path))):
            raise ValueError(f"Invalid file path (directory traversal): {path}")
        
        # Check file size
        file_size = path.stat().st_size
        if file_size > self.MAX_FILE_SIZE:
            raise ValueError(
                f"File too large: {file_size} bytes (max {self.MAX_FILE_SIZE})"
            )
        
        # Load JSON
        with open(path, 'r', encoding='utf-8') as f:
            data = json.load(f)
        
        # Handle both single dict and list of dicts
        if isinstance(data, dict):
            return [data]
        elif isinstance(data, list):
            return data[:limit]
        else:
            raise ValueError(f"Invalid JSON structure (expected dict or list): {type(data)}")
