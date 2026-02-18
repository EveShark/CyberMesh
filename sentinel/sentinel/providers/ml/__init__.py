"""ML-based detection providers."""

from .malware_pe import MalwarePEProvider
from .malware_api import MalwareAPIProvider
from .malware_android import MalwareAndroidProvider
from .anomaly import AnomalyProvider

__all__ = [
    "MalwarePEProvider",
    "MalwareAPIProvider",
    "MalwareAndroidProvider",
    "AnomalyProvider",
]
