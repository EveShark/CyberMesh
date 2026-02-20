"""Live PostgreSQL telemetry source for curated.live_flows."""

from typing import Dict, List, Tuple

from ..logging import get_logger
from .telemetry_postgres import PostgresTelemetrySource
from .features_flow import FlowFeatureExtractor


def _parse_feature_mask(mask: bytes) -> int:
    if not mask:
        return 0
    try:
        return int(mask.hex(), 16)
    except Exception:
        return 0


def _apply_feature_mask(flow: Dict, feature_names: List[str], mask: bytes) -> Dict:
    bitmask = _parse_feature_mask(mask)
    if bitmask == 0:
        return flow
    for idx, name in enumerate(feature_names):
        if not (bitmask & (1 << idx)):
            flow[name] = None
    return flow


def _has_flow_id(flow: Dict) -> bool:
    flow_id = flow.get("flow_id")
    return isinstance(flow_id, str) and len(flow_id) >= 8


def _meets_coverage(flow: Dict, min_coverage: float) -> bool:
    coverage = flow.get("feature_coverage")
    if coverage is None:
        return True
    try:
        return float(coverage) >= float(min_coverage)
    except Exception:
        return False


class LiveTelemetrySource(PostgresTelemetrySource):
    """Reads live flows without label filtering or random sampling."""

    DEFAULT_METADATA_COLUMNS = ["ts", "tenant_id", "src_ip", "dst_ip", "flow_id", "feature_mask", "label"]

    def __init__(self, *args, feature_mask_enabled: bool = True, min_feature_coverage: float = 0.0, **kwargs):
        self._feature_mask_enabled = bool(feature_mask_enabled)
        self._min_feature_coverage = float(min_feature_coverage)
        self._feature_names = FlowFeatureExtractor.FEATURE_COLUMNS
        super().__init__(*args, **kwargs)
        self.logger = get_logger(__name__)

    def has_data(self) -> bool:
        if self._pool is None:
            return False
        try:
            conn = self._pool.getconn()
            cur = conn.cursor()
            cur.execute(f"SELECT COUNT(*) FROM {self.full_table}")
            count = cur.fetchone()[0]
            cur.close()
            self._pool.putconn(conn)
            return bool(count and count > 0)
        except Exception:
            return False

    def _test_connection(self) -> None:
        try:
            conn = self._pool.getconn()
            cur = conn.cursor()
            cur.execute(f"SELECT COUNT(*) FROM {self.full_table}")
            count = cur.fetchone()[0]
            cur.close()
            self._pool.putconn(conn)
            self.logger.info("PostgreSQL live telemetry connection ok", extra={"rows": count})
        except Exception as err:
            self.logger.error("PostgreSQL live telemetry connection failed", exc_info=True, extra={"error": str(err)})
            raise

    def _compose_query(self, limit: int) -> Tuple[str, Tuple]:
        clauses = [f"SELECT {self._select_clause}", f"FROM {self.full_table}"]
        clauses.append("ORDER BY ts DESC")
        clauses.append("LIMIT %s")
        return "\n".join(clauses), (limit,)

    def _normalize_rows(self, rows) -> List[Dict]:
        flows = super()._normalize_rows(rows)
        if not self._feature_mask_enabled:
            return [flow for flow in flows if _has_flow_id(flow) and _meets_coverage(flow, self._min_feature_coverage)]
        filtered: List[Dict] = []
        for flow in flows:
            if not _has_flow_id(flow) or not _meets_coverage(flow, self._min_feature_coverage):
                continue
            mask = flow.get("feature_mask")
            _apply_feature_mask(flow, self._feature_names, mask)
            filtered.append(flow)
        return filtered
