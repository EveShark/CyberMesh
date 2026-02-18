"""
Feature builder - Creates FileFeaturesV1 from Sentinel ParsedFile and analysis results.
"""

from typing import List, Optional

from ..contracts.schemas import FileFeaturesV1
from ..parsers.base import ParsedFile
from ..providers.base import AnalysisResult, ThreatLevel


def build_file_features(
    parsed_file: ParsedFile,
    analysis_results: Optional[List[AnalysisResult]] = None,
) -> FileFeaturesV1:
    """
    Build canonical FileFeaturesV1 from ParsedFile and optional analysis results.
    
    Args:
        parsed_file: Parsed file from Sentinel parsers
        analysis_results: Optional list of provider analysis results
        
    Returns:
        FileFeaturesV1 with populated fields
    """
    # Base features from parsed file
    features = FileFeaturesV1(
        sha256=parsed_file.hashes.get("sha256", ""),
        file_name=parsed_file.file_name,
        file_size=parsed_file.file_size,
        file_type=parsed_file.file_type.value,
        entropy=parsed_file.entropy,
        sha1=parsed_file.hashes.get("sha1"),
        md5=parsed_file.hashes.get("md5"),
        section_count=len(parsed_file.sections),
        import_count=len(parsed_file.imports),
        export_count=len(parsed_file.exports),
        strings_count=len(parsed_file.strings),
    )
    
    # Extract section entropy stats if available
    if parsed_file.sections:
        entropies = [s.get("entropy", 0.0) for s in parsed_file.sections if "entropy" in s]
        if entropies:
            features.section_entropy_max = max(entropies)
            features.section_entropy_mean = sum(entropies) / len(entropies)
    
    # Extract threat intel indicators from parsed file
    features.ti_ip_count = len(parsed_file.ips)
    features.ti_domain_count = len(parsed_file.domains)
    features.ti_url_count = len(parsed_file.urls)
    
    # Enrich from analysis results if provided
    if analysis_results:
        _enrich_from_results(features, analysis_results)
    
    return features


def _enrich_from_results(
    features: FileFeaturesV1,
    results: List[AnalysisResult],
) -> None:
    """
    Enrich features from analysis results in-place.
    
    Extracts scores and findings from various provider types.
    """
    for result in results:
        provider_name = result.provider_name.lower()
        
        # ML scores
        if "malware_pe" in provider_name or "pe_ml" in provider_name:
            features.ml_pe_score = result.score
        elif "malware_api" in provider_name or "api_ml" in provider_name:
            features.ml_api_score = result.score
        
        # Strings analyzer scores
        elif "strings" in provider_name:
            features.strings_suspicious_count = len([
                f for f in result.findings if "suspicious" in f.lower()
            ])
            # Extract category scores from metadata if available
            if result.metadata:
                features.strings_score_command_exec = result.metadata.get(
                    "command_execution_score", 0.0
                )
                features.strings_score_cred_theft = result.metadata.get(
                    "credential_theft_score", 0.0
                )
                features.strings_score_download_exec = result.metadata.get(
                    "download_execute_score", 0.0
                )
                features.strings_score_persistence = result.metadata.get(
                    "persistence_score", 0.0
                )
                features.strings_score_obfuscation = result.metadata.get(
                    "obfuscation_score", 0.0
                )
        
        # YARA matches
        elif "yara" in provider_name:
            if result.metadata:
                matched_rules = result.metadata.get("matched_rules", [])
                features.yara_match_count = len(matched_rules)
                features.yara_rule_names = matched_rules
        
        # Threat intel results
        elif any(x in provider_name for x in ["intel", "vt", "abuse", "crowdsec", "feodo"]):
            # Check for C2/botnet indicators
            for finding in result.findings:
                finding_lower = finding.lower()
                if "c2" in finding_lower or "command and control" in finding_lower:
                    features.ti_is_c2 = True
                if "botnet" in finding_lower:
                    features.ti_is_botnet = True
            
            # Extract abuse score
            if result.metadata:
                abuse_score = result.metadata.get("abuse_confidence_score", 0)
                if abuse_score > features.ti_abuse_score_max:
                    features.ti_abuse_score_max = float(abuse_score)
                
                # CrowdSec behaviors
                behaviors = result.metadata.get("behaviors", [])
                if behaviors:
                    features.ti_crowdsec_behaviors.extend(behaviors)
            
            # Count malicious indicators
            for indicator in result.indicators:
                if indicator.type == "ip" and indicator.confidence > 0.7:
                    features.ti_ip_malicious_count += 1
                elif indicator.type == "domain" and indicator.confidence > 0.7:
                    features.ti_domain_malicious_count += 1
                elif indicator.type == "url" and indicator.confidence > 0.7:
                    features.ti_url_malicious_count += 1
