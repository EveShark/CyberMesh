"""File parsers for different file types."""

from .base import Parser, ParsedFile, FileType
from .detector import detect_file_type, get_parser_for_file

from . import pe_parser
from . import script_parser
from . import pdf_parser
from . import office_parser
from . import android_parser
from . import pcap_parser

__all__ = [
    "Parser",
    "ParsedFile",
    "FileType",
    "detect_file_type",
    "get_parser_for_file",
]
