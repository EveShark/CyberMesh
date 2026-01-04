"""Semantic validation for canonical feature schemas."""

import re
from typing import List, Optional, Union
from .contracts import NetworkFlowFeaturesV1, FileFeaturesV1, DNSFeaturesV1


def validate_network_flow_features(features: Union[NetworkFlowFeaturesV1, dict]) -> List[str]:
    """
    Validate semantic correctness of network flow features.
    
    Args:
        features: NetworkFlowFeaturesV1 instance or dict
        
    Returns:
        List of validation error messages (empty if valid)
    """
    errors = []
    
    if isinstance(features, dict):
        f = features
    else:
        f = features.__dict__
    
    if f.get('flow_duration', 0) < 0:
        errors.append("flow_duration cannot be negative")
    
    if f.get('tot_fwd_pkts', 0) < 0:
        errors.append("tot_fwd_pkts cannot be negative")
    
    if f.get('tot_bwd_pkts', 0) < 0:
        errors.append("tot_bwd_pkts cannot be negative")
    
    if f.get('totlen_fwd_pkts', 0) < 0:
        errors.append("totlen_fwd_pkts cannot be negative")
    
    if f.get('totlen_bwd_pkts', 0) < 0:
        errors.append("totlen_bwd_pkts cannot be negative")
    
    flow_byts_s = f.get('flow_byts_s', 0)
    if flow_byts_s < 0:
        errors.append("flow_byts_s cannot be negative")
    
    flow_pkts_s = f.get('flow_pkts_s', 0)
    if flow_pkts_s < 0:
        errors.append("flow_pkts_s cannot be negative")
    
    src_port = f.get('src_port', 0)
    if src_port < 0 or src_port > 65535:
        errors.append(f"src_port must be in [0, 65535], got {src_port}")
    
    dst_port = f.get('dst_port', 0)
    if dst_port < 0 or dst_port > 65535:
        errors.append(f"dst_port must be in [0, 65535], got {dst_port}")
    
    protocol = f.get('protocol', 0)
    if protocol < 0 or protocol > 255:
        errors.append(f"protocol must be in [0, 255], got {protocol}")
    
    src_ip = f.get('src_ip', '')
    if src_ip and not _is_valid_ip(src_ip):
        errors.append(f"invalid src_ip format: {src_ip}")
    
    dst_ip = f.get('dst_ip', '')
    if dst_ip and not _is_valid_ip(dst_ip):
        errors.append(f"invalid dst_ip format: {dst_ip}")
    
    for field in ['fwd_pkt_len_std', 'bwd_pkt_len_std', 'pkt_len_std', 'flow_iat_std']:
        val = f.get(field, 0)
        if val < 0:
            errors.append(f"{field} cannot be negative (std dev)")
    
    return errors


def validate_file_features(features: Union[FileFeaturesV1, dict]) -> List[str]:
    """
    Validate semantic correctness of file features.
    
    Args:
        features: FileFeaturesV1 instance or dict
        
    Returns:
        List of validation error messages (empty if valid)
    """
    errors = []
    
    if isinstance(features, dict):
        f = features
    else:
        f = features.__dict__
    
    sha256 = f.get('sha256', '')
    if not sha256:
        errors.append("sha256 is required")
    elif len(sha256) != 64 or not _is_hex(sha256):
        errors.append(f"sha256 must be 64 hex characters, got {len(sha256)} chars")
    
    sha1 = f.get('sha1')
    if sha1 and (len(sha1) != 40 or not _is_hex(sha1)):
        errors.append(f"sha1 must be 40 hex characters if provided")
    
    md5 = f.get('md5')
    if md5 and (len(md5) != 32 or not _is_hex(md5)):
        errors.append(f"md5 must be 32 hex characters if provided")
    
    file_size = f.get('file_size', 0)
    if file_size < 0:
        errors.append(f"file_size cannot be negative, got {file_size}")
    
    entropy = f.get('entropy', 0)
    if entropy < 0.0 or entropy > 8.0:
        errors.append(f"entropy must be in [0, 8], got {entropy}")
    
    section_entropy_max = f.get('section_entropy_max', 0)
    if section_entropy_max < 0.0 or section_entropy_max > 8.0:
        errors.append(f"section_entropy_max must be in [0, 8], got {section_entropy_max}")
    
    for field in ['import_count', 'export_count', 'section_count', 'strings_count']:
        val = f.get(field, 0)
        if val < 0:
            errors.append(f"{field} cannot be negative")
    
    for field in ['strings_score_command_exec', 'strings_score_cred_theft', 
                  'strings_score_download_exec', 'strings_score_persistence',
                  'strings_score_obfuscation', 'yara_score_packers', 
                  'yara_score_exploits', 'ml_pe_score', 'ml_api_score']:
        val = f.get(field, 0)
        if val < 0.0 or val > 1.0:
            errors.append(f"{field} should be in [0, 1], got {val}")
    
    return errors


def validate_dns_features(features: Union[DNSFeaturesV1, dict]) -> List[str]:
    """
    Validate semantic correctness of DNS features.
    
    Args:
        features: DNSFeaturesV1 instance or dict
        
    Returns:
        List of validation error messages (empty if valid)
    """
    errors = []
    
    if isinstance(features, dict):
        f = features
    else:
        f = features.__dict__
    
    query_name = f.get('query_name', '')
    if not query_name:
        errors.append("query_name is required")
    
    domain_length = f.get('domain_length', 0)
    if domain_length < 0:
        errors.append(f"domain_length cannot be negative, got {domain_length}")
    
    domain_entropy = f.get('domain_entropy', 0)
    if domain_entropy < 0.0:
        errors.append(f"domain_entropy cannot be negative, got {domain_entropy}")
    
    subdomain_count = f.get('subdomain_count', 0)
    if subdomain_count < 0:
        errors.append(f"subdomain_count cannot be negative")
    
    for field in ['digit_ratio', 'consonant_ratio', 'vowel_ratio']:
        val = f.get(field, 0)
        if val < 0.0 or val > 1.0:
            errors.append(f"{field} must be in [0, 1], got {val}")
    
    response_code = f.get('response_code', 0)
    if response_code < 0 or response_code > 23:
        errors.append(f"response_code should be in [0, 23], got {response_code}")
    
    for field in ['ttl_min', 'ttl_max', 'answer_count']:
        val = f.get(field, 0)
        if val < 0:
            errors.append(f"{field} cannot be negative")
    
    client_ip = f.get('client_ip')
    if client_ip and not _is_valid_ip(client_ip):
        errors.append(f"invalid client_ip format: {client_ip}")
    
    server_ip = f.get('server_ip')
    if server_ip and not _is_valid_ip(server_ip):
        errors.append(f"invalid server_ip format: {server_ip}")
    
    return errors


def _is_valid_ip(ip: str) -> bool:
    """Check if string is a valid IPv4 or IPv6 address."""
    if not ip or ip == '0.0.0.0':
        return True
    
    ipv4_pattern = r'^(\d{1,3}\.){3}\d{1,3}$'
    if re.match(ipv4_pattern, ip):
        parts = ip.split('.')
        return all(0 <= int(p) <= 255 for p in parts)
    
    ipv6_pattern = r'^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}$'
    if re.match(ipv6_pattern, ip):
        return True
    
    return False


def _is_hex(s: str) -> bool:
    """Check if string contains only hex characters."""
    return all(c in '0123456789abcdefABCDEF' for c in s)


def sanitize_network_flow_features(features: dict) -> dict:
    """
    Sanitize network flow features by clamping invalid values.
    
    Args:
        features: Raw feature dictionary
        
    Returns:
        Sanitized feature dictionary
    """
    result = dict(features)
    
    for field in ['flow_duration', 'tot_fwd_pkts', 'tot_bwd_pkts', 
                  'totlen_fwd_pkts', 'totlen_bwd_pkts', 'flow_byts_s', 'flow_pkts_s']:
        if field in result and result[field] < 0:
            result[field] = 0
    
    if 'src_port' in result:
        result['src_port'] = max(0, min(65535, int(result['src_port'])))
    
    if 'dst_port' in result:
        result['dst_port'] = max(0, min(65535, int(result['dst_port'])))
    
    if 'protocol' in result:
        result['protocol'] = max(0, min(255, int(result['protocol'])))
    
    return result


def sanitize_file_features(features: dict) -> dict:
    """
    Sanitize file features by clamping invalid values.
    
    Args:
        features: Raw feature dictionary
        
    Returns:
        Sanitized feature dictionary
    """
    result = dict(features)
    
    if 'entropy' in result:
        result['entropy'] = max(0.0, min(8.0, float(result['entropy'])))
    
    if 'section_entropy_max' in result:
        result['section_entropy_max'] = max(0.0, min(8.0, float(result['section_entropy_max'])))
    
    if 'file_size' in result and result['file_size'] < 0:
        result['file_size'] = 0
    
    for field in ['import_count', 'export_count', 'section_count', 'strings_count']:
        if field in result and result[field] < 0:
            result[field] = 0
    
    return result
