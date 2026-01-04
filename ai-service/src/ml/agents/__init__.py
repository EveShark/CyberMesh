"""Detection agent wrappers for existing engines."""

from .contracts import (
    ThreatType,
    Modality,
    DetectionCandidate,
    EnsembleDecision,
    CanonicalEvent,
    NetworkFlowFeaturesV1,
)
from .detection_agent import DetectionAgent
from .rules_engine_agent import RulesEngineAgent
from .math_engine_agent import MathEngineAgent
from .ml_engine_agent import MLEngineAgent
from .ensemble_voter import AgentEnsembleVoter
from .agent_pipeline import DetectionAgentPipeline, build_flow_event
from .feature_extractors import (
    extract_cic_features,
    extract_semantics,
    extract_semantics_batch,
    CIC_FEATURE_COLUMNS,
)
from .sentinel_agent import SentinelFileAgent, build_file_event
from .shadow_mode import (
    ShadowComparison,
    ShadowMetrics,
    ShadowModeCollector,
    ShadowModePipeline,
)
from .rollout_config import (
    RolloutState,
    AgentConfig,
    ProfileConfig,
    RolloutConfig,
    load_rollout_config,
    create_default_config,
)
from .telemetry_config import (
    SourceMode,
    DetectionMode,
    SourceType,
    TelemetrySourceConfig,
    TelemetryRegistry,
    load_telemetry_registry,
    create_default_registry,
)
from .flow_adapters import (
    FlowAdapter,
    CICDDoS2019Adapter,
    EnterpriseFlowAdapter,
    get_adapter,
    register_adapter,
    ADAPTER_REGISTRY,
)
from .flow_ml_agent_v2 import FlowMLEngineAgentV2, create_flow_ml_agent_v2
from .dns_modality import (
    DNSFeaturesV1,
    DNSAdapter,
    DNSAgent,
)

__all__ = [
    "ThreatType",
    "Modality",
    "DetectionCandidate",
    "EnsembleDecision",
    "CanonicalEvent",
    "NetworkFlowFeaturesV1",
    "DetectionAgent",
    "RulesEngineAgent",
    "MathEngineAgent",
    "MLEngineAgent",
    "AgentEnsembleVoter",
    "DetectionAgentPipeline",
    "build_flow_event",
    "extract_cic_features",
    "extract_semantics",
    "extract_semantics_batch",
    "CIC_FEATURE_COLUMNS",
    "SentinelFileAgent",
    "build_file_event",
    "ShadowComparison",
    "ShadowMetrics",
    "ShadowModeCollector",
    "ShadowModePipeline",
    "RolloutState",
    "AgentConfig",
    "ProfileConfig",
    "RolloutConfig",
    "load_rollout_config",
    "create_default_config",
    "SourceMode",
    "DetectionMode",
    "SourceType",
    "TelemetrySourceConfig",
    "TelemetryRegistry",
    "load_telemetry_registry",
    "create_default_registry",
    "FlowAdapter",
    "CICDDoS2019Adapter",
    "EnterpriseFlowAdapter",
    "get_adapter",
    "register_adapter",
    "ADAPTER_REGISTRY",
    "FlowMLEngineAgentV2",
    "create_flow_ml_agent_v2",
    "DNSFeaturesV1",
    "DNSAdapter",
    "DNSAgent",
]
