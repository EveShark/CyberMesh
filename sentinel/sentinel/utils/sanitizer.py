"""Input sanitization for LLM prompts - redacts sensitive information."""

import re
from typing import List, Set, Optional, Dict, Any
from dataclasses import dataclass

import logging

logger = logging.getLogger(__name__)


@dataclass
class SanitizerConfig:
    """Configuration for input sanitization."""
    max_input_length: int = 4000
    redact_file_paths: bool = True
    redact_internal_ips: bool = True
    redact_usernames: bool = True
    redact_emails: bool = True
    redact_api_keys: bool = True
    redact_passwords: bool = True
    remove_binary: bool = True
    truncation_notice: str = "\n[... content truncated ...]"


class InputSanitizer:
    """
    Sanitizes input before sending to external LLM APIs.
    
    Security measures:
    - Redacts file paths (especially user directories)
    - Redacts internal IP addresses
    - Redacts usernames from paths
    - Redacts potential credentials/API keys
    - Removes binary/non-printable characters
    - Truncates overly long inputs
    - Detects and masks PII patterns
    """
    
    # Patterns for sensitive data
    PATTERNS = {
        # Windows paths with usernames
        "windows_user_path": re.compile(
            r'[A-Za-z]:\\Users\\[^\\/:*?"<>|\s]+',
            re.IGNORECASE
        ),
        # Full Windows paths
        "windows_path": re.compile(
            r'[A-Za-z]:\\(?:[^\\/:*?"<>|\s]+\\)*[^\\/:*?"<>|\s]*',
            re.IGNORECASE
        ),
        # Unix home paths
        "unix_home_path": re.compile(
            r'/home/[a-zA-Z0-9_-]+',
        ),
        # Unix paths
        "unix_path": re.compile(
            r'/(?:[a-zA-Z0-9_.-]+/)*[a-zA-Z0-9_.-]+',
        ),
        # Internal IPs (RFC 1918)
        "internal_ip": re.compile(
            r'\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|'
            r'172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|'
            r'192\.168\.\d{1,3}\.\d{1,3}|'
            r'127\.\d{1,3}\.\d{1,3}\.\d{1,3})\b'
        ),
        # Email addresses
        "email": re.compile(
            r'\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b'
        ),
        # API keys (common patterns)
        "api_key": re.compile(
            r'\b(?:'
            r'[A-Za-z0-9]{32,}|'  # Generic long alphanumeric
            r'sk-[A-Za-z0-9]{32,}|'  # OpenAI style
            r'gsk_[A-Za-z0-9]{32,}|'  # Groq style
            r'pk_[A-Za-z0-9]{32,}|'  # Stripe style
            r'[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}'  # JWT-like
            r')\b'
        ),
        # Passwords in common formats
        "password_field": re.compile(
            r'(?:password|passwd|pwd|secret|token|key|credential)["\s:=]+["\']?[^\s"\']{4,}["\']?',
            re.IGNORECASE
        ),
        # AWS access keys
        "aws_key": re.compile(
            r'\b(?:AKIA|ABIA|ACCA|ASIA)[A-Z0-9]{16}\b'
        ),
        # Credit card numbers (basic)
        "credit_card": re.compile(
            r'\b(?:\d{4}[-\s]?){3}\d{4}\b'
        ),
        # SSN (US)
        "ssn": re.compile(
            r'\b\d{3}-\d{2}-\d{4}\b'
        ),
    }
    
    # Redaction placeholders
    REDACTIONS = {
        "windows_user_path": "[REDACTED_USER_PATH]",
        "windows_path": "[REDACTED_PATH]",
        "unix_home_path": "[REDACTED_HOME]",
        "unix_path": "[REDACTED_PATH]",
        "internal_ip": "[INTERNAL_IP]",
        "email": "[REDACTED_EMAIL]",
        "api_key": "[REDACTED_KEY]",
        "password_field": "[REDACTED_CREDENTIAL]",
        "aws_key": "[REDACTED_AWS_KEY]",
        "credit_card": "[REDACTED_CC]",
        "ssn": "[REDACTED_SSN]",
    }
    
    def __init__(self, config: Optional[SanitizerConfig] = None):
        self.config = config or SanitizerConfig()
        self._redaction_count = 0
    
    def sanitize(self, text: str, context: str = "prompt") -> str:
        """
        Sanitize input text for LLM consumption.
        
        Args:
            text: Raw input text
            context: Context identifier for logging
            
        Returns:
            Sanitized text safe for external API
        """
        if not text:
            return text
        
        original_length = len(text)
        self._redaction_count = 0
        
        # 1. Remove binary/non-printable characters
        if self.config.remove_binary:
            text = self._remove_binary(text)
        
        # 2. Redact sensitive patterns
        if self.config.redact_file_paths:
            text = self._redact_pattern(text, "windows_user_path")
            text = self._redact_pattern(text, "windows_path")
            text = self._redact_pattern(text, "unix_home_path")
        
        if self.config.redact_internal_ips:
            text = self._redact_pattern(text, "internal_ip")
        
        if self.config.redact_emails:
            text = self._redact_pattern(text, "email")
        
        if self.config.redact_api_keys:
            text = self._redact_pattern(text, "api_key")
            text = self._redact_pattern(text, "aws_key")
        
        if self.config.redact_passwords:
            text = self._redact_pattern(text, "password_field")
        
        # Always redact these
        text = self._redact_pattern(text, "credit_card")
        text = self._redact_pattern(text, "ssn")
        
        # 3. Truncate if too long
        if len(text) > self.config.max_input_length:
            text = self._truncate(text)
        
        # Log sanitization stats
        if self._redaction_count > 0:
            logger.debug(
                f"Sanitized {context}: {self._redaction_count} redactions, "
                f"{original_length} -> {len(text)} chars"
            )
        
        return text
    
    def sanitize_dict(self, data: Dict[str, Any], keys_to_sanitize: Optional[Set[str]] = None) -> Dict[str, Any]:
        """
        Sanitize string values in a dictionary.
        
        Args:
            data: Dictionary with potentially sensitive values
            keys_to_sanitize: Specific keys to sanitize (None = all string values)
            
        Returns:
            Dictionary with sanitized values
        """
        result = {}
        default_keys = {"content", "text", "message", "description", "path", "file_path"}
        target_keys = keys_to_sanitize or default_keys
        
        for key, value in data.items():
            if isinstance(value, str):
                if keys_to_sanitize is None or key in target_keys:
                    result[key] = self.sanitize(value, context=key)
                else:
                    result[key] = value
            elif isinstance(value, dict):
                result[key] = self.sanitize_dict(value, keys_to_sanitize)
            elif isinstance(value, list):
                result[key] = [
                    self.sanitize(v, context=key) if isinstance(v, str) else v
                    for v in value
                ]
            else:
                result[key] = value
        
        return result
    
    def _redact_pattern(self, text: str, pattern_name: str) -> str:
        """Redact matches of a named pattern."""
        pattern = self.PATTERNS.get(pattern_name)
        redaction = self.REDACTIONS.get(pattern_name, "[REDACTED]")
        
        if not pattern:
            return text
        
        matches = pattern.findall(text)
        if matches:
            self._redaction_count += len(matches)
            text = pattern.sub(redaction, text)
        
        return text
    
    def _remove_binary(self, text: str) -> str:
        """Remove non-printable and binary characters."""
        # Keep printable ASCII, newlines, tabs
        cleaned = []
        for char in text:
            if char.isprintable() or char in '\n\r\t':
                cleaned.append(char)
            else:
                cleaned.append(' ')
        
        # Collapse multiple spaces
        result = ''.join(cleaned)
        result = re.sub(r' {3,}', '  ', result)
        
        return result
    
    def _truncate(self, text: str) -> str:
        """Truncate text to max length with notice."""
        max_len = self.config.max_input_length - len(self.config.truncation_notice)
        
        # Try to truncate at a sentence boundary
        truncated = text[:max_len]
        
        # Find last sentence end
        for end_char in ['. ', '.\n', '! ', '!\n', '? ', '?\n']:
            last_end = truncated.rfind(end_char)
            if last_end > max_len * 0.7:  # At least 70% of content
                truncated = truncated[:last_end + 1]
                break
        
        return truncated + self.config.truncation_notice
    
    def extract_safe_indicators(self, text: str) -> Dict[str, List[str]]:
        """
        Extract IOCs that are safe to send externally.
        
        Only extracts public indicators, not internal data.
        
        Returns:
            Dict with safe indicators (public IPs, domains, hashes)
        """
        indicators = {
            "public_ips": [],
            "domains": [],
            "urls": [],
            "hashes": [],
        }
        
        # Public IPs (not internal)
        ip_pattern = re.compile(r'\b(?:\d{1,3}\.){3}\d{1,3}\b')
        for ip in ip_pattern.findall(text):
            if not self.PATTERNS["internal_ip"].match(ip):
                indicators["public_ips"].append(ip)
        
        # Domains
        domain_pattern = re.compile(
            r'\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}\b',
            re.IGNORECASE
        )
        indicators["domains"] = list(set(domain_pattern.findall(text)))
        
        # URLs (already somewhat public)
        url_pattern = re.compile(r'https?://[^\s<>"\']+')
        indicators["urls"] = list(set(url_pattern.findall(text)))
        
        # Hashes (MD5, SHA1, SHA256)
        hash_patterns = [
            re.compile(r'\b[a-fA-F0-9]{32}\b'),  # MD5
            re.compile(r'\b[a-fA-F0-9]{40}\b'),  # SHA1
            re.compile(r'\b[a-fA-F0-9]{64}\b'),  # SHA256
        ]
        for pattern in hash_patterns:
            indicators["hashes"].extend(pattern.findall(text))
        indicators["hashes"] = list(set(indicators["hashes"]))
        
        return indicators


def create_safe_prompt(
    template: str,
    file_info: Dict[str, Any],
    findings: List[str],
    sanitizer: Optional[InputSanitizer] = None
) -> str:
    """
    Create a sanitized prompt for LLM analysis.
    
    Args:
        template: Prompt template with placeholders
        file_info: File metadata (will be sanitized)
        findings: List of findings (will be sanitized)
        sanitizer: Sanitizer instance (creates default if None)
        
    Returns:
        Safe prompt string
    """
    if sanitizer is None:
        sanitizer = InputSanitizer()
    
    # Sanitize file info
    safe_file_info = {
        "type": file_info.get("file_type", "unknown"),
        "size": file_info.get("file_size", 0),
        "entropy": file_info.get("entropy", 0),
        "name": sanitizer.sanitize(
            file_info.get("file_name", "unknown"),
            context="filename"
        ),
    }
    
    # Sanitize findings
    safe_findings = [
        sanitizer.sanitize(f, context="finding")
        for f in findings[:20]  # Limit findings
    ]
    
    # Build prompt
    prompt = template.format(
        file_type=safe_file_info["type"],
        file_size=safe_file_info["size"],
        entropy=safe_file_info["entropy"],
        file_name=safe_file_info["name"],
        findings="\n".join(f"- {f}" for f in safe_findings),
    )
    
    return sanitizer.sanitize(prompt, context="final_prompt")
