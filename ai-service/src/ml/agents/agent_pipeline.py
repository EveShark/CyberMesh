"""DetectionAgentPipeline - New pipeline using canonical events."""

import time
import uuid
from typing import List, Dict, Optional, Any

from .detection_agent import DetectionAgent
from .ensemble_voter import AgentEnsembleVoter
from .contracts import (
    CanonicalEvent,
    EnsembleDecision,
    DetectionCandidate,
    Modality,
    NetworkFlowFeaturesV1,
    ThreatType,
)
from ...logging import get_logger

logger = get_logger(__name__)


class DetectionAgentPipeline:
    """
    New detection pipeline using canonical events and DetectionAgents.
    
    Consumes CanonicalEvents, routes to appropriate agents by modality,
    and produces EnsembleDecisions.
    """
    
    def __init__(
        self,
        agents: List[DetectionAgent],
        voter: AgentEnsembleVoter,
        config: Dict,
    ):
        """
        Initialize pipeline.
        
        Args:
            agents: List of DetectionAgent instances
            voter: AgentEnsembleVoter for final decision
            config: Pipeline configuration
        """
        self._agents = agents
        self._voter = voter
        self._config = config
        self._profile = config.get('default_profile', 'balanced')
        self._agent_health: Dict[str, str] = {a.agent_id: "healthy" for a in agents}
        self._agent_failure_counts: Dict[str, int] = {a.agent_id: 0 for a in agents}
        self._failure_threshold = config.get('agent_failure_threshold', 5)
        
        logger.info(
            f"Initialized DetectionAgentPipeline with {len(agents)} agents: "
            f"{[a.agent_id for a in agents]}"
        )
    
    def process(
        self,
        event: CanonicalEvent,
        profile: Optional[str] = None,
    ) -> EnsembleDecision:
        """
        Process a single canonical event.
        
        Args:
            event: CanonicalEvent to process
            profile: Override default profile
            
        Returns:
            EnsembleDecision with final verdict
        """
        t0 = time.perf_counter()
        profile = profile or self._profile
        
        # Collect candidates from all compatible agents
        all_candidates: List[DetectionCandidate] = []
        agent_latencies: Dict[str, float] = {}
        
        for agent in self._agents:
            if not agent.can_process(event):
                continue
            
            try:
                agent_t0 = time.perf_counter()
                candidates = agent.analyze(event)
                agent_latency = (time.perf_counter() - agent_t0) * 1000
                agent_latencies[agent.agent_id] = agent_latency
                
                all_candidates.extend(candidates)
                
                logger.debug(
                    f"Agent {agent.agent_id} produced {len(candidates)} candidates "
                    f"in {agent_latency:.1f}ms"
                )
                
            except Exception as e:
                logger.error(f"Agent {agent.agent_id} failed: {e}")
                self._agent_failure_counts[agent.agent_id] = self._agent_failure_counts.get(agent.agent_id, 0) + 1
                if self._agent_failure_counts[agent.agent_id] >= self._failure_threshold:
                    self._agent_health[agent.agent_id] = "degraded"
                continue
        
        # Run ensemble voting
        decision = self._voter.decide(all_candidates, profile=profile)
        
        # Add latency metadata
        total_latency = (time.perf_counter() - t0) * 1000
        decision.metadata['latency_ms'] = total_latency
        decision.metadata['agent_latencies'] = agent_latencies
        decision.metadata['event_id'] = event.id
        
        logger.info(
            f"Pipeline processed event {event.id}: "
            f"{len(all_candidates)} candidates, "
            f"threat={decision.threat_type.value}, "
            f"score={decision.final_score:.3f}, "
            f"publish={decision.should_publish}, "
            f"latency={total_latency:.1f}ms"
        )
        
        return decision
    
    def process_batch(
        self,
        events: List[CanonicalEvent],
        profile: Optional[str] = None,
    ) -> List[EnsembleDecision]:
        """
        Process a batch of canonical events.
        
        Args:
            events: List of CanonicalEvents
            profile: Override default profile
            
        Returns:
            List of EnsembleDecisions
        """
        return [self.process(event, profile) for event in events]
    
    def get_agent_health(self) -> Dict[str, str]:
        """Return health status of all agents."""
        return dict(self._agent_health)
    
    def reset_agent_health(self, agent_id: str) -> None:
        """Reset health status for an agent after recovery."""
        if agent_id in self._agent_health:
            self._agent_health[agent_id] = "healthy"
            self._agent_failure_counts[agent_id] = 0


def build_flow_event(
    flow_dict: Dict[str, Any],
    tenant_id: str,
    source: str = "cybermesh",
) -> CanonicalEvent:
    """
    Build a CanonicalEvent from a raw flow dict.
    
    Args:
        flow_dict: Raw flow data (CIC-DDoS2019 format)
        tenant_id: Tenant identifier
        source: Event source
        
    Returns:
        CanonicalEvent with NetworkFlowFeaturesV1
    """
    return CanonicalEvent(
        id=str(uuid.uuid4()),
        timestamp=time.time(),
        source=source,
        tenant_id=tenant_id,
        modality=Modality.NETWORK_FLOW,
        features_version="NetworkFlowFeaturesV1",
        features=flow_dict,
        raw_context={"original_flow": flow_dict},
    )
