"""File upload handling and validation."""

import os
import tempfile
import shutil
from typing import Optional, Tuple, BinaryIO
from pathlib import Path
from fastapi import UploadFile

from ..config import get_settings
from ..core.security import (
    sanitize_filename,
    validate_file,
    get_magic_type,
    compute_file_hash,
    compute_hash_from_bytes,
)
from ..core.exceptions import FileTooLargeError, InvalidFileTypeError


class FileHandler:
    """Handle file uploads securely."""
    
    def __init__(self):
        self.settings = get_settings()
        self.temp_dir = Path(tempfile.gettempdir()) / "sentinel_uploads"
        self.temp_dir.mkdir(exist_ok=True)
    
    async def process_upload(
        self,
        file: UploadFile,
    ) -> Tuple[str, str, int, str]:
        """
        Process uploaded file with validation.
        
        Args:
            file: FastAPI UploadFile object
            
        Returns:
            Tuple of (temp_path, file_hash, file_size, file_type)
            
        Raises:
            FileTooLargeError: If file exceeds size limit
            InvalidFileTypeError: If file type is not supported
        """
        # Read file in chunks to avoid memory issues
        safe_filename = sanitize_filename(file.filename or "unknown")
        temp_path = self.temp_dir / safe_filename
        
        file_size = 0
        file_header = b""
        
        try:
            with open(temp_path, "wb") as f:
                while True:
                    chunk = await file.read(8192)
                    if not chunk:
                        break
                    
                    # Capture first bytes for magic detection
                    if len(file_header) < 16:
                        file_header += chunk[:16 - len(file_header)]
                    
                    file_size += len(chunk)
                    
                    # Check size limit during upload
                    if file_size > self.settings.max_file_size_bytes:
                        # Clean up and raise error
                        f.close()
                        temp_path.unlink(missing_ok=True)
                        raise FileTooLargeError(
                            file_size,
                            self.settings.max_file_size_bytes,
                        )
                    
                    f.write(chunk)
            
            # Validate file
            is_valid, error = validate_file(
                file.filename or "unknown",
                file_size,
                file_header,
                self.settings.max_file_size_bytes,
            )
            
            if not is_valid:
                temp_path.unlink(missing_ok=True)
                raise InvalidFileTypeError(error or "Unknown error")
            
            # Compute hash
            file_hash = compute_file_hash(str(temp_path))
            
            # Detect file type
            file_type = get_magic_type(file_header)
            
            return str(temp_path), file_hash, file_size, file_type
            
        except (FileTooLargeError, InvalidFileTypeError):
            raise
        except Exception as e:
            # Clean up on any error
            if temp_path.exists():
                temp_path.unlink(missing_ok=True)
            raise
    
    def cleanup(self, file_path: str) -> None:
        """
        Clean up temporary file after scanning.
        
        Args:
            file_path: Path to temporary file
        """
        try:
            path = Path(file_path)
            if path.exists() and str(path).startswith(str(self.temp_dir)):
                path.unlink()
        except Exception:
            pass  # Ignore cleanup errors
    
    def cleanup_old_files(self, max_age_seconds: int = 3600) -> int:
        """
        Clean up old temporary files.
        
        Args:
            max_age_seconds: Maximum age of files to keep
            
        Returns:
            Number of files deleted
        """
        import time
        
        deleted = 0
        now = time.time()
        
        try:
            for file_path in self.temp_dir.iterdir():
                if file_path.is_file():
                    age = now - file_path.stat().st_mtime
                    if age > max_age_seconds:
                        file_path.unlink()
                        deleted += 1
        except Exception:
            pass
        
        return deleted


def format_file_size(size_bytes: int) -> str:
    """Format file size in human readable format."""
    for unit in ['B', 'KB', 'MB', 'GB']:
        if size_bytes < 1024:
            return f"{size_bytes:.1f} {unit}"
        size_bytes /= 1024
    return f"{size_bytes:.1f} TB"


# Global file handler instance
file_handler = FileHandler()
