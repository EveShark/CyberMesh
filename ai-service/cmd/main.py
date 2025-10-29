#!/usr/bin/env python3
"""
CyberMesh AI Service - Main Entry Point

Military-grade AI anomaly detection service for Byzantine Fault Tolerant network.

Security:
- Environment validation before startup
- Graceful shutdown with state saving
- Signal handling (SIGINT, SIGTERM)
- PID file management
- No secrets in logs or output

Usage:
    python main.py [OPTIONS]

Options:
    --config PATH       Path to .env config file (default: ../.env)
    --log-level LEVEL   Logging level: DEBUG|INFO|WARNING|ERROR (default: INFO)
    --log-file PATH     Log file path (default: stdout only)
    --help              Show this help message
"""
import sys
import os
import signal
import argparse
import time
from pathlib import Path
from typing import Optional

# Don't add src/ to path - use absolute imports to avoid shadowing stdlib modules
# sys.path.insert(0, str(Path(__file__).parent / "src"))

# Add parent directory to path for src package imports (now in cmd/, so go up one level)
sys.path.insert(0, str(Path(__file__).parent.parent))

from src.__version__ import VERSION, SERVICE_NAME
from src.config.loader import load_settings
from src.config.settings import Settings
from src.logging import configure_logging, get_logger
from src.service import ServiceManager
from src.utils.errors import ConfigError, ServiceError


# Exit codes
EXIT_SUCCESS = 0
EXIT_ERROR = 1
EXIT_CONFIG_ERROR = 2
EXIT_SIGNAL = 130  # 128 + SIGINT(2)

# Global service manager for signal handlers
_service_manager: Optional[ServiceManager] = None
_shutdown_requested = False


def parse_args():
    """Parse command line arguments."""
    parser = argparse.ArgumentParser(
        description=f"{SERVICE_NAME} v{VERSION}",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python main.py
  python main.py --config /path/to/.env
  python main.py --log-level DEBUG --log-file service.log
        """
    )
    
    # Use project root .env (parent of ai-service directory, now we're in cmd/ so go up 2 levels)
    default_config = Path(__file__).resolve().parent.parent.parent / ".env"

    parser.add_argument(
        "--config",
        type=str,
        default=str(default_config),
        help=f"Path to .env configuration file (default: {default_config})"
    )
    
    parser.add_argument(
        "--log-level",
        type=str,
        choices=["DEBUG", "INFO", "WARNING", "ERROR"],
        default="INFO",
        help="Logging level (default: INFO)"
    )
    
    parser.add_argument(
        "--log-file",
        type=str,
        default=None,
        help="Log file path (default: stdout only)"
    )
    
    parser.add_argument(
        "--version",
        action="version",
        version=f"{SERVICE_NAME} v{VERSION}"
    )
    
    return parser.parse_args()


def setup_signal_handlers():
    """
    Setup signal handlers for graceful shutdown.
    
    Handles:
    - SIGINT (Ctrl+C)
    - SIGTERM (kill command)
    
    Security:
    - Saves cryptographic state before exit
    - Flushes Kafka producer
    - Commits consumer offsets
    - No secrets in shutdown logs
    """
    def signal_handler(signum, frame):
        global _shutdown_requested
        
        if _shutdown_requested:
            print("\n[FORCE] Second signal received, forcing exit...")
            sys.exit(EXIT_SIGNAL)
        
        _shutdown_requested = True
        signal_name = signal.Signals(signum).name
        print(f"\n[SHUTDOWN] {signal_name} received, shutting down gracefully...")
        
        if _service_manager:
            try:
                _service_manager.stop()
                print("[SHUTDOWN] Service stopped successfully")
                sys.exit(EXIT_SUCCESS)
            except Exception as e:
                print(f"[ERROR] Shutdown failed: {e}")
                sys.exit(EXIT_ERROR)
        else:
            sys.exit(EXIT_SIGNAL)
    
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)


def write_pid_file(pid_path: Path):
    """
    Write process ID to file.
    
    Args:
        pid_path: Path to PID file
        
    Security:
    - File permissions: 0644 (readable by all, writable by owner)
    - Atomic write (temp + rename)
    """
    pid = os.getpid()
    
    # Create parent directory if needed
    pid_path.parent.mkdir(parents=True, exist_ok=True)
    
    # Atomic write
    temp_path = pid_path.with_suffix('.tmp')
    temp_path.write_text(str(pid))
    temp_path.rename(pid_path)
    
    # Set permissions (readable by all)
    if hasattr(os, 'chmod'):
        os.chmod(pid_path, 0o644)


def remove_pid_file(pid_path: Path):
    """Remove PID file on clean shutdown."""
    try:
        if pid_path.exists():
            pid_path.unlink()
    except Exception:
        pass  # Best effort


def print_startup_banner(settings: Settings, logger):
    """
    Print startup banner with service info.
    
    Security:
    - No secrets displayed
    - No sensitive paths
    - Environment-aware display
    """
    banner = f"""
{'='*70}
{SERVICE_NAME} v{VERSION}
{'='*70}
Environment:     {settings.environment}
Node ID:         {settings.node_id}
Kafka Bootstrap: {settings.kafka_producer.bootstrap_servers}
Log Level:       {logger.level}
{'='*70}
Starting service...
"""
    print(banner)
    
    logger.info(
        "Service starting",
        extra={
            "version": VERSION,
            "environment": settings.environment,
            "node_id": settings.node_id,
        }
    )


def validate_environment(settings: Settings):
    """
    Validate environment is correctly configured.
    
    Args:
        settings: Loaded settings
        
    Raises:
        ConfigError: If validation fails
        
    Security:
    - Checks critical security settings
    - Validates production requirements
    - Clear error messages (no secrets)
    """
    errors = []
    
    # Check node ID
    if not settings.node_id or settings.node_id == "0":
        errors.append("NODE_ID must be set to a valid node identifier")
    
    # Check Kafka configuration
    if not settings.kafka_producer.bootstrap_servers:
        errors.append("Kafka bootstrap servers not configured")
    
    # Production-specific checks
    if settings.environment == "production":
        # Check signing key exists
        if not settings.signing_key_path.exists():
            errors.append(
                f"Production requires existing signing key at {settings.signing_key_path}. "
                f"Generate with: openssl genpkey -algorithm ED25519 -out {settings.signing_key_path}"
            )
        
        # Check TLS enabled
        if not settings.kafka_producer.security.tls_enabled:
            errors.append("Production requires TLS enabled for Kafka")
    
    if errors:
        raise ConfigError(
            "Environment validation failed:\n" + 
            "\n".join(f"  - {err}" for err in errors)
        )


def main():
    """
    Main entry point.
    
    Returns:
        Exit code (0=success, 1=error, 2=config error, 130=signal)
    """
    global _service_manager
    
    # Health server (initialized later)
    health_server = None
    
    # Parse arguments
    args = parse_args()
    
    # Setup logging (basic console logging first)
    configure_logging(
        level=args.log_level,
        json_format=False,
        log_file=args.log_file
    )
    logger = get_logger("main")
    
    # PID file path (now in cmd/, so go up one level to ai-service root)
    pid_file = Path(__file__).parent.parent / "data" / "service.pid"
    
    try:
        # Load configuration
        logger.info(f"Loading configuration from {args.config}")
        settings = load_settings(args.config)
        
        # Validate environment
        logger.info("Validating environment")
        validate_environment(settings)
        
        # Setup signal handlers
        setup_signal_handlers()
        
        # Write PID file
        write_pid_file(pid_file)
        logger.info(f"PID {os.getpid()} written to {pid_file}")
        
        # Print startup banner
        print_startup_banner(settings, logger)
        
        # Initialize service manager
        logger.info("Initializing service manager")
        _service_manager = ServiceManager()
        _service_manager.initialize(settings)
        
        logger.info("Starting service")
        _service_manager.start()
        
        # Start API server for Kubernetes probes and metrics
        logger.info("Starting API server")
        from src.api import start_api_server
        api_server = start_api_server(_service_manager, host='0.0.0.0', port=8080)
        logger.info("API server listening on :8080 (/health, /ready, /metrics, /detections/stats)")
        
        # Service is now running
        logger.info("Service running - Press Ctrl+C to stop")
        print(f"\n{SERVICE_NAME} is RUNNING")
        print("Press Ctrl+C to stop\n")
        
        # Keep main thread alive
        while not _shutdown_requested:
            time.sleep(1)
            
            # Optional: Print status every 60 seconds
            # (Commented out to reduce noise)
            # if int(time.time()) % 60 == 0:
            #     health = _service_manager.health_check()
            #     logger.info(f"Status check: {health['status']}")
        
        return EXIT_SUCCESS
        
    except ConfigError as e:
        logger.error(f"Configuration error: {e}")
        print(f"\n[ERROR] Configuration error: {e}\n", file=sys.stderr)
        return EXIT_CONFIG_ERROR
        
    except ServiceError as e:
        logger.error(f"Service error: {e}")
        print(f"\n[ERROR] Service error: {e}\n", file=sys.stderr)
        return EXIT_ERROR
        
    except KeyboardInterrupt:
        # Should be caught by signal handler, but just in case
        logger.info("Interrupted by user")
        return EXIT_SIGNAL
        
    except Exception as e:
        logger.error("Unexpected error", exc_info=True)
        print(f"\n[ERROR] Unexpected error: {e}\n", file=sys.stderr)
        return EXIT_ERROR
        
    finally:
        # Cleanup
        if health_server:
            try:
                logger.info("Shutting down health API")
                health_server.shutdown()
            except Exception:
                pass  # Daemon thread, will die with main process
        
        if _service_manager:
            try:
                _service_manager.stop()
            except Exception:
                pass  # Already logged
        
        remove_pid_file(pid_file)
        logger.info("Service stopped")


if __name__ == "__main__":
    sys.exit(main())
