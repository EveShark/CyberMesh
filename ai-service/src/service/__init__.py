"""
Service layer for AI service.

Provides service orchestration, lifecycle management, and component initialization.
"""
from .crypto_setup import (
    initialize_signer,
    initialize_nonce_manager,
    shutdown_nonce_manager,
)
from .manager import ServiceManager, ServiceState
from .handlers import MessageHandlers
from .publisher import MessagePublisher

__all__ = [
    "initialize_signer",
    "initialize_nonce_manager",
    "shutdown_nonce_manager",
    "ServiceManager",
    "ServiceState",
    "MessageHandlers",
    "MessagePublisher",
]
