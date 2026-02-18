"""Flow agent wrapper for network flow heuristics."""

from .telemetry_agent import TelemetryRulesAgent


class FlowAgent(TelemetryRulesAgent):
    """Alias of TelemetryRulesAgent for network flow naming consistency."""

    @property
    def name(self) -> str:
        return "flow_agent"
