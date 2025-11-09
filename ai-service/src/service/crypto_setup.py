"""
Cryptographic component initialization for AI service.

Handles Signer and NonceManager setup with environment-aware security:
- Production: Strict validation, no auto-generation
- Development: Convenience features, auto-generation allowed

Security principles:
- Never auto-generate keys in production (operator must provide)
- Validate key file permissions (0600 on Unix)
- Persist nonce state to prevent reuse
- Atomic writes for state files
- Fail fast on security violations
"""
import os
import json
import tempfile
from pathlib import Path
from typing import Optional
from ..utils import Signer, NonceManager, generate_keypair
from ..utils.errors import SecurityError, ConfigError
from ..config import Settings


def initialize_signer(settings: Settings, logger) -> Signer:
    """
    Initialize Ed25519 signer with environment-aware key management.
    
    Args:
        settings: Service configuration
        logger: Logger instance
        
    Returns:
        Configured Signer instance
        
    Raises:
        SecurityError: If key missing in production or permissions invalid
        ConfigError: If key file corrupted or invalid format
        
    Security:
        Production:
            - Key file MUST exist (never auto-generate)
            - Key file MUST have 0600 permissions (Unix)
            - Key file MUST be valid Ed25519 format
            
        Development:
            - Auto-generates key if missing (with clear warning)
            - Saves to configured path
            - Logs key location for operator
    """
    key_path = settings.signing_key_path
    key_id = settings.signing_key_id
    domain = settings.domain_separation
    is_production = settings.environment == "production"
    
    logger.info(
        "Initializing signer",
        extra={
            "key_path": str(key_path),
            "key_id": key_id,
            "domain": domain,
            "environment": settings.environment,
        }
    )
    
    # Check if key exists
    if not key_path.exists():
        if is_production:
            # CRITICAL: Never auto-generate keys in production
            raise SecurityError(
                f"Signing key not found in production: {key_path}\n"
                f"Production systems MUST NOT auto-generate keys.\n"
                f"Generate key manually:\n"
                f"  python -c 'from src.utils import generate_keypair; "
                f"generate_keypair(\"{key_path}\")'\n"
                f"Then set proper permissions:\n"
                f"  chmod 600 {key_path}"
            )
        else:
            # Development: Auto-generate for convenience
            logger.warning(
                "Signing key not found - auto-generating (DEVELOPMENT ONLY)",
                extra={"key_path": str(key_path)}
            )
            
            # Ensure parent directory exists
            key_path.parent.mkdir(parents=True, exist_ok=True)
            
            # Generate key
            try:
                pub_bytes, priv_bytes = generate_keypair(str(key_path))
                logger.info(
                    "Signing key generated",
                    extra={
                        "key_path": str(key_path),
                        "pubkey_size": len(pub_bytes),
                        "privkey_size": len(priv_bytes),
                    }
                )
            except Exception as e:
                raise SecurityError(f"Failed to generate signing key: {e}")
    
    # Validate key file permissions (Unix only)
    if os.name != 'nt':  # Not Windows
        stat_info = key_path.stat()
        permissions = stat_info.st_mode & 0o777
        
        if permissions != 0o600:
            if is_production:
                raise SecurityError(
                    f"Signing key has insecure permissions: {oct(permissions)}\n"
                    f"Required: 0600 (owner read/write only)\n"
                    f"Fix with: chmod 600 {key_path}"
                )
            else:
                logger.warning(
                    "Signing key has insecure permissions - fixing",
                    extra={
                        "current": oct(permissions),
                        "required": "0600",
                    }
                )
                key_path.chmod(0o600)
    
    # Load signer
    try:
        signer = Signer(
            private_key_path=str(key_path),
            key_id=key_id,
            domain_separation=domain
        )
        
        logger.info(
            "Signer initialized successfully",
            extra={
                "key_id": key_id,
                "pubkey_size": len(signer.public_key_bytes),
            }
        )
        
        return signer
        
    except Exception as e:
        raise ConfigError(f"Failed to load signing key from {key_path}: {e}")


def initialize_nonce_manager(settings: Settings, logger) -> NonceManager:
    """
    Initialize NonceManager with persistent state.
    
    Args:
        settings: Service configuration
        logger: Logger instance
        
    Returns:
        Configured NonceManager instance
        
    Raises:
        ConfigError: If state file corrupted or invalid
        SecurityError: If state file has insecure permissions (production)
        
    Security:
        - State file has 0600 permissions (Unix)
        - Atomic writes (temp + rename) to prevent corruption
        - Validates state on load (detect tampering/corruption)
        - Fails in production if state corrupted
        - Uses timestamp-based recovery in development
        
    Nonce State Format (JSON):
        {
            "instance_id": 1,
            "last_nonce_counter": 12345,
            "last_updated": "2025-01-03T12:00:00Z",
            "version": 1
        }
    """
    state_path = settings.nonce_state_path
    instance_id = int(settings.node_id)  # Use node_id as instance_id
    is_production = settings.environment == "production"
    
    logger.info(
        "Initializing nonce manager",
        extra={
            "state_path": str(state_path),
            "instance_id": instance_id,
            "environment": settings.environment,
        }
    )
    
    # Create state directory if needed
    state_path.parent.mkdir(parents=True, exist_ok=True)
    
    # Try to load existing state
    saved_state = None
    if state_path.exists():
        saved_state = _load_nonce_state(
            state_path, 
            instance_id, 
            is_production, 
            logger
        )
    
    # Create nonce manager
    initial_timestamp = None
    initial_counter = 0
    if saved_state is not None:
        initial_timestamp, initial_counter = saved_state
    nonce_mgr = NonceManager(
        instance_id=instance_id,
        initial_timestamp_ms=initial_timestamp,
        initial_counter=initial_counter,
    )

    if saved_state is not None:
        logger.info(
            "Nonce manager resumed (timestamp-based, no replay risk)",
            extra={
                "last_saved_timestamp": initial_timestamp,
                "last_saved_counter": initial_counter,
                "instance_id": instance_id,
            }
        )
    else:
        logger.info(
            "Nonce manager started fresh",
            extra={"instance_id": instance_id}
        )
    
    # Save initial state (use current timestamp as marker)
    from ..utils.time import now_ms
    current_ts, current_counter = nonce_mgr.state_snapshot()
    _save_nonce_state(state_path, instance_id, current_ts, current_counter, logger)
    
    # Set up periodic state saving (every 100 nonces)
    # Track nonce count in wrapper
    nonce_count = [0]  # Use list for mutable closure
    original_generate = nonce_mgr.generate
    save_interval = 100
    
    def generate_with_save():
        nonce = original_generate()
        nonce_count[0] += 1
        if nonce_count[0] % save_interval == 0:
            ts, counter = nonce_mgr.state_snapshot()
            _save_nonce_state(state_path, instance_id, ts, counter, logger)
        return nonce
    
    nonce_mgr.generate = generate_with_save
    
    logger.info(
        "Nonce manager initialized",
        extra={
            "instance_id": instance_id,
            "save_interval": save_interval,
        }
    )
    
    return nonce_mgr


def _load_nonce_state(
    state_path: Path,
    instance_id: int,
    is_production: bool,
    logger
) -> Optional[tuple[int, int]]:
    """
    Load nonce state from disk.
    
    Returns:
        Last nonce counter, or None if state invalid/corrupted
    """
    try:
        # Check permissions (Unix only)
        if os.name != 'nt':
            stat_info = state_path.stat()
            permissions = stat_info.st_mode & 0o777
            
            if permissions != 0o600:
                if is_production:
                    raise SecurityError(
                        f"Nonce state file has insecure permissions: {oct(permissions)}\n"
                        f"Required: 0600\n"
                        f"Fix with: chmod 600 {state_path}"
                    )
                else:
                    logger.warning(
                        "Nonce state file has insecure permissions - fixing",
                        extra={"current": oct(permissions)}
                    )
                    state_path.chmod(0o600)
        
        # Load state file
        with open(state_path, 'r') as f:
            state = json.load(f)
        
        # Validate state format
        if not isinstance(state, dict):
            raise ValueError("State file is not a JSON object")
        
        version = state.get("version", 1)

        if state.get("instance_id") != instance_id:
            logger.warning(
                "Nonce state instance_id mismatch - ignoring saved state",
                extra={
                    "saved": state.get("instance_id"),
                    "current": instance_id,
                }
            )
            return None

        if version == 1:
            last_counter = state.get("last_nonce_counter")
            if not isinstance(last_counter, int) or last_counter < 0:
                raise ValueError(f"Invalid last_nonce_counter: {last_counter}")
            last_ts_ms = last_counter
            counter = 0
        else:
            last_ts_ms = state.get("last_timestamp_ms")
            counter = state.get("last_counter", 0)
            if not isinstance(last_ts_ms, int) or last_ts_ms < 0:
                raise ValueError(f"Invalid last_timestamp_ms: {last_ts_ms}")
            if not isinstance(counter, int) or counter < 0:
                raise ValueError(f"Invalid last_counter: {counter}")

        logger.info(
            "Nonce state loaded",
            extra={
                "last_timestamp_ms": last_ts_ms,
                "last_counter": counter,
                "last_updated": state.get("last_updated"),
            }
        )
        
        return last_ts_ms, counter
        
    except json.JSONDecodeError as e:
        if is_production:
            raise ConfigError(
                f"Nonce state file corrupted (invalid JSON): {state_path}\n"
                f"Error: {e}\n"
                f"In production, manual intervention required.\n"
                f"Delete file to start fresh (ONLY if you can guarantee no message replay)."
            )
        else:
            logger.warning(
                "Nonce state file corrupted - starting fresh (DEVELOPMENT ONLY)",
                extra={"error": str(e)}
            )
            return None
    
    except (ValueError, KeyError) as e:
        if is_production:
            raise ConfigError(
                f"Nonce state file invalid: {state_path}\n"
                f"Error: {e}\n"
                f"In production, manual intervention required."
            )
        else:
            logger.warning(
                "Nonce state file invalid - starting fresh (DEVELOPMENT ONLY)",
                extra={"error": str(e)}
            )
            return None
    
    except Exception as e:
        logger.error(
            "Failed to load nonce state",
            exc_info=True,
            extra={"error": str(e)}
        )
        return None


def _save_nonce_state(
    state_path: Path,
    instance_id: int,
    timestamp_ms: int,
    counter: int,
    logger
) -> None:
    """
    Save nonce state to disk atomically.
    
    Uses temp file + atomic rename to prevent corruption.
    """
    from datetime import datetime
    
    state = {
        "version": 2,
        "instance_id": instance_id,
        "last_timestamp_ms": timestamp_ms,
        "last_counter": counter,
        "last_updated": datetime.utcnow().isoformat() + "Z",
    }
    
    try:
        # Write to temp file in same directory (for atomic rename)
        temp_fd, temp_path = tempfile.mkstemp(
            dir=state_path.parent,
            prefix=".nonce_state_",
            suffix=".tmp"
        )
        
        try:
            # Write state
            with os.fdopen(temp_fd, 'w') as f:
                json.dump(state, f, indent=2)
            
            # Set permissions (Unix only)
            if os.name != 'nt':
                os.chmod(temp_path, 0o600)
            
            # Atomic rename
            os.replace(temp_path, state_path)
            
            logger.debug(
                "Nonce state saved",
                extra={"counter": counter}
            )
            
        except Exception:
            # Clean up temp file on error
            try:
                os.unlink(temp_path)
            except:
                pass
            raise
    
    except Exception as e:
        logger.error(
            "Failed to save nonce state",
            exc_info=True,
            extra={"error": str(e)}
        )
        # Don't fail the operation - state saving is best-effort
        # Worst case: nonce counter starts from 0 on next restart
        # This is safe as long as instance_id is unique


def shutdown_nonce_manager(nonce_mgr: NonceManager, state_path: Path, instance_id: int, logger) -> None:
    """
    Gracefully shutdown nonce manager by saving final state.
    
    Call this during service shutdown to ensure state is persisted.
    """
    try:
        current_timestamp, current_counter = nonce_mgr.state_snapshot()

        _save_nonce_state(state_path, instance_id, current_timestamp, current_counter, logger)
        logger.info(
            "Nonce manager shutdown complete",
            extra={
                "final_timestamp": current_timestamp,
                "final_counter": current_counter,
            }
        )
    except Exception as e:
        logger.error(
            "Failed to save final nonce state",
            exc_info=True,
            extra={"error": str(e)}
        )
