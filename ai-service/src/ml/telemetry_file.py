"""File telemetry source for file-based event ingestion."""

import os
import time
import logging
import hashlib
from uuid import uuid4
from pathlib import Path
from typing import Dict, Any, Iterator, List, Optional
from dataclasses import dataclass

from .agents.contracts import CanonicalEvent, Modality

logger = logging.getLogger(__name__)


@dataclass
class FileEvent:
    """Raw file event before conversion to CanonicalEvent."""
    path: str
    sha256: Optional[str] = None
    file_size: Optional[int] = None
    file_name: Optional[str] = None
    tenant_id: str = "default"
    timestamp: Optional[float] = None
    metadata: Optional[Dict[str, Any]] = None


class FileTelemetrySource:
    """
    Telemetry source for file-based events.
    
    Supports two modes:
    1. Directory watching - monitors a directory for new files
    2. Queue consumption - processes file events from an in-memory queue
    
    Produces CanonicalEvents with modality=FILE for processing by
    SentinelFileAgent or other file-analysis agents.
    """
    
    def __init__(
        self,
        watch_path: Optional[str] = None,
        poll_interval: float = 1.0,
        compute_hash: bool = True,
        file_extensions: Optional[List[str]] = None,
        max_file_size: int = 100 * 1024 * 1024,  # 100MB
        tenant_id: str = "default",
    ):
        """
        Initialize file telemetry source.
        
        Args:
            watch_path: Directory to watch for new files (optional)
            poll_interval: Seconds between directory scans
            compute_hash: Whether to compute SHA256 hash of files
            file_extensions: List of extensions to process (None = all)
            max_file_size: Maximum file size to process in bytes
            tenant_id: Default tenant ID for events
        """
        self.watch_path = Path(watch_path) if watch_path else None
        self.poll_interval = poll_interval
        self.compute_hash = compute_hash
        self.file_extensions = set(ext.lower().lstrip('.') for ext in (file_extensions or []))
        self.max_file_size = max_file_size
        self.tenant_id = tenant_id
        
        self._seen_files: set = set()
        self._queue: List[FileEvent] = []
        self._running = False
    
    def submit(self, event: FileEvent) -> None:
        """
        Submit a file event to the queue for processing.
        
        Args:
            event: FileEvent to process
        """
        self._queue.append(event)
    
    def submit_path(
        self,
        path: str,
        tenant_id: Optional[str] = None,
        metadata: Optional[Dict[str, Any]] = None,
    ) -> None:
        """
        Submit a file path for processing.
        
        Args:
            path: Path to file
            tenant_id: Optional tenant ID override
            metadata: Optional additional metadata
        """
        event = FileEvent(
            path=path,
            tenant_id=tenant_id or self.tenant_id,
            timestamp=time.time(),
            metadata=metadata,
        )
        self.submit(event)
    
    def __iter__(self) -> Iterator[FileEvent]:
        """
        Iterate over file events.
        
        Yields file events from:
        1. The submission queue (if any)
        2. New files in watch_path (if configured)
        """
        # First, yield any queued events
        while self._queue:
            yield self._queue.pop(0)
        
        # Then scan watch directory if configured
        if self.watch_path and self.watch_path.exists():
            for event in self._scan_directory():
                yield event
    
    def _scan_directory(self) -> Iterator[FileEvent]:
        """Scan watch directory for new files."""
        if not self.watch_path or not self.watch_path.exists():
            return
        
        try:
            for entry in self.watch_path.iterdir():
                if not entry.is_file():
                    continue
                
                # Skip already-seen files
                file_key = (str(entry), entry.stat().st_mtime)
                if file_key in self._seen_files:
                    continue
                
                # Check extension filter
                if self.file_extensions:
                    ext = entry.suffix.lower().lstrip('.')
                    if ext not in self.file_extensions:
                        continue
                
                # Check file size
                try:
                    stat = entry.stat()
                    if stat.st_size > self.max_file_size:
                        logger.warning(f"Skipping file {entry}: size {stat.st_size} exceeds max {self.max_file_size}")
                        continue
                except OSError as e:
                    logger.warning(f"Cannot stat file {entry}: {e}")
                    continue
                
                # Mark as seen
                self._seen_files.add(file_key)
                
                # Create event
                yield FileEvent(
                    path=str(entry),
                    file_name=entry.name,
                    file_size=stat.st_size,
                    tenant_id=self.tenant_id,
                    timestamp=time.time(),
                )
        
        except PermissionError as e:
            logger.error(f"Permission denied accessing {self.watch_path}: {e}")
        except Exception as e:
            logger.error(f"Error scanning directory {self.watch_path}: {e}")
    
    def to_canonical_event(
        self,
        event: FileEvent,
        source: str = "file_telemetry",
    ) -> CanonicalEvent:
        """
        Convert a FileEvent to a CanonicalEvent.
        
        Args:
            event: FileEvent to convert
            source: Source identifier for the event
            
        Returns:
            CanonicalEvent with modality=FILE
        """
        # Compute hash if needed and not already provided
        sha256 = event.sha256
        file_size = event.file_size
        file_name = event.file_name
        
        if os.path.exists(event.path):
            if not file_name:
                file_name = os.path.basename(event.path)
            
            if not file_size:
                try:
                    file_size = os.path.getsize(event.path)
                except OSError:
                    file_size = 0
            
            if not sha256 and self.compute_hash:
                sha256 = self._compute_sha256(event.path)
        
        raw_context = {
            "file_path": event.path,
            "file_name": file_name or os.path.basename(event.path),
            "file_size": file_size or 0,
        }
        
        if sha256:
            raw_context["sha256"] = sha256
        
        if event.metadata:
            raw_context["metadata"] = event.metadata
        
        return CanonicalEvent(
            id=str(uuid4()),
            timestamp=event.timestamp or time.time(),
            source=source,
            tenant_id=event.tenant_id,
            modality=Modality.FILE,
            features_version="FileFeaturesV1",
            features={},  # SentinelFileAgent will populate features after analysis
            raw_context=raw_context,
        )
    
    def to_canonical_events_batch(
        self,
        events: List[FileEvent],
        source: str = "file_telemetry",
    ) -> List[CanonicalEvent]:
        """
        Convert a batch of FileEvents to CanonicalEvents.
        
        Args:
            events: List of FileEvents
            source: Source identifier
            
        Returns:
            List of CanonicalEvents
        """
        canonical_events = []
        for event in events:
            try:
                canonical = self.to_canonical_event(event, source)
                canonical_events.append(canonical)
            except Exception as e:
                logger.warning(f"Failed to convert file event {event.path}: {e}")
                continue
        return canonical_events
    
    @staticmethod
    def _compute_sha256(path: str, chunk_size: int = 8192) -> str:
        """Compute SHA256 hash of a file."""
        sha256_hash = hashlib.sha256()
        try:
            with open(path, "rb") as f:
                for chunk in iter(lambda: f.read(chunk_size), b""):
                    sha256_hash.update(chunk)
            return sha256_hash.hexdigest()
        except (IOError, OSError) as e:
            logger.warning(f"Failed to compute hash for {path}: {e}")
            return ""
    
    def get_pending_count(self) -> int:
        """Return number of pending events in queue."""
        return len(self._queue)
    
    def clear_seen(self) -> None:
        """Clear the set of seen files to reprocess them."""
        self._seen_files.clear()
