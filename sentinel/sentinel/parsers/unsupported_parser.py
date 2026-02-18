"""Parser for unsupported-but-detected file types (v1).

Security-first behavior:
- Never raise; always returns a ParsedFile with an explicit error.
- Keeps behavior deterministic and operator-friendly.
"""

from __future__ import annotations

from pathlib import Path
from typing import Set

from .base import Parser, ParsedFile, FileType


class UnsupportedParser(Parser):
    """Generic parser that marks a file as unsupported."""

    def __init__(self, file_type: FileType):
        self._file_type = file_type

    @property
    def name(self) -> str:
        return "unsupported_parser"

    @property
    def supported_extensions(self) -> Set[str]:
        return set()

    @property
    def supported_types(self) -> Set[FileType]:
        return {self._file_type}

    def parse(self, file_path: str) -> ParsedFile:
        path = Path(file_path)
        size = 0
        try:
            size = path.stat().st_size
        except Exception:
            size = 0

        return ParsedFile(
            file_path=str(path),
            file_name=path.name,
            file_type=self._file_type,
            file_size=size,
            errors=[f"ERR_UNSUPPORTED_FILETYPE: no parser implemented for {self._file_type.value}"],
        )

    def can_parse(self, file_path: str) -> bool:
        # This parser is only used when the file type has already been detected.
        return True
