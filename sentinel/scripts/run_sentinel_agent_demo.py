#!/usr/bin/env python3
"""
Demo script for SentinelAgent detection pipeline.

Usage:
    python scripts/run_sentinel_agent_demo.py <file_path>
    python scripts/run_sentinel_agent_demo.py --test
"""

import sys
import json
from pathlib import Path

# Add project root to path
sys.path.insert(0, str(Path(__file__).parent.parent))

from sentinel.agents import (
    SentinelAgent,
    EnsembleVoter,
    build_file_event_from_path,
    build_file_features,
)
from sentinel.providers.registry import ProviderRegistry
from sentinel.providers.static import EntropyProvider, StringsProvider
from sentinel.parsers import get_parser_for_file
from sentinel.logging import configure_logging, get_logger

configure_logging(level="INFO")
logger = get_logger(__name__)


def setup_registry() -> ProviderRegistry:
    """Set up provider registry with available providers."""
    registry = ProviderRegistry()
    registry.register(EntropyProvider(), baseline_weight=0.6)
    registry.register(StringsProvider(), baseline_weight=0.7)
    
    # Try to load ML provider
    models_path = Path("data/models")
    if models_path.exists():
        try:
            from sentinel.providers.ml import MalwarePEProvider
            ml_provider = MalwarePEProvider(str(models_path))
            registry.register(ml_provider, baseline_weight=0.85)
            logger.info("ML provider loaded")
        except Exception as e:
            logger.warning(f"Could not load ML provider: {e}")
    
    return registry


def run_demo(file_path: str, profile: str = "balanced") -> dict:
    """
    Run the full SentinelAgent detection path.
    
    Args:
        file_path: Path to file to analyze
        profile: Detection profile ("security_first", "balanced", "low_fp")
        
    Returns:
        Dict with demo results
    """
    logger.info(f"=== SentinelAgent Detection Demo ===")
    logger.info(f"File: {file_path}")
    logger.info(f"Profile: {profile}")
    
    # 1. Setup
    registry = setup_registry()
    agent = SentinelAgent(registry)
    voter = EnsembleVoter.from_config_file()
    
    logger.info(f"Agent: {agent.agent_id}")
    logger.info(f"Providers: {list(registry.providers.keys())}")
    
    # 2. Build CanonicalEvent from file path
    logger.info("\n--- Step 1: Build CanonicalEvent ---")
    event = build_file_event_from_path(
        file_path=file_path,
        tenant_id="demo-tenant",
        source="demo_script",
    )
    logger.info(f"Event ID: {event.id}")
    logger.info(f"Modality: {event.modality.value}")
    logger.info(f"Features version: {event.features_version}")
    
    # 3. Run SentinelAgent.analyze()
    logger.info("\n--- Step 2: SentinelAgent.analyze() ---")
    candidates = agent.analyze(event)
    logger.info(f"Candidates returned: {len(candidates)}")
    
    for i, c in enumerate(candidates):
        logger.info(f"  [{i+1}] {c.agent_id} / {c.signal_id}")
        logger.info(f"      Threat: {c.threat_type.value}, Score: {c.calibrated_score:.3f}, Confidence: {c.confidence:.3f}")
        if c.findings:
            logger.info(f"      Findings: {c.findings[:2]}...")
    
    # 4. Run EnsembleVoter.decide()
    logger.info(f"\n--- Step 3: EnsembleVoter.decide(profile={profile}) ---")
    decision = voter.decide(candidates, profile=profile)
    
    logger.info(f"Should Publish: {decision.should_publish}")
    logger.info(f"Threat Type: {decision.threat_type.value}")
    logger.info(f"Final Score: {decision.final_score:.3f}")
    logger.info(f"Confidence: {decision.confidence:.3f}")
    logger.info(f"LLR: {decision.llr:.3f}")
    if decision.abstention_reason:
        logger.info(f"Abstention: {decision.abstention_reason}")
    
    # 5. Summary
    logger.info("\n=== Demo Complete ===")
    
    return {
        "file_path": file_path,
        "event_id": event.id,
        "candidates_count": len(candidates),
        "candidates": [c.to_dict() for c in candidates],
        "decision": decision.to_dict(),
    }


def create_test_file() -> str:
    """Create a simple test file for demo."""
    test_dir = Path("data/test_files")
    test_dir.mkdir(parents=True, exist_ok=True)
    
    test_file = test_dir / "demo_test.txt"
    test_file.write_text(
        "This is a test file for the SentinelAgent demo.\n"
        "It contains some text that will be analyzed.\n"
        "cmd.exe /c powershell -encodedcommand base64string\n"
        "http://malicious.example.com/payload.exe\n"
    )
    
    return str(test_file)


def main():
    import argparse
    
    parser = argparse.ArgumentParser(description="SentinelAgent Detection Demo")
    parser.add_argument("file_path", nargs="?", help="Path to file to analyze")
    parser.add_argument("--test", action="store_true", help="Use test file")
    parser.add_argument("--profile", default="balanced", help="Detection profile")
    parser.add_argument("--json", action="store_true", help="Output JSON result")
    
    args = parser.parse_args()
    
    if args.test:
        file_path = create_test_file()
        logger.info(f"Created test file: {file_path}")
    elif args.file_path:
        file_path = args.file_path
    else:
        parser.print_help()
        sys.exit(1)
    
    if not Path(file_path).exists():
        logger.error(f"File not found: {file_path}")
        sys.exit(1)
    
    result = run_demo(file_path, profile=args.profile)
    
    if args.json:
        print(json.dumps(result, indent=2, default=str))


if __name__ == "__main__":
    main()
