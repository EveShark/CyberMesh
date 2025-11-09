"""PostgreSQL telemetry source for real DDoS data."""

import concurrent.futures
import threading
import time
from typing import Dict, List, Optional, Tuple

import psycopg2
from psycopg2 import extras, pool

from ..logging import get_logger
from .features_flow import FlowFeatureExtractor
from .telemetry import TelemetrySource


class PostgresTelemetrySource(TelemetrySource):
    """Pull real network flows from PostgreSQL with pooling and instrumentation."""

    DEFAULT_METADATA_COLUMNS = ["label", "src_ip", "dst_ip", "flow_id"]

    def __init__(
        self,
        db_config: dict,
        sample_size: int = 100,
        table_name: str = "test_ddos_binary",
        schema: str = "curated",
        *,
        sample_table: Optional[str] = None,
        sample_strategy: str = "tablesample",
        sample_percent: float = 5.0,
        pool_min: int = 1,
        pool_max: int = 4,
        prefetch_enabled: bool = False,
    ):
        self.db_config = db_config
        self.sample_size = sample_size
        self.table_name = table_name
        self.schema = schema
        self.full_table = f"{schema}.{table_name}"
        self._sample_table = sample_table
        self._sample_strategy = sample_strategy.lower().strip()
        self._sample_percent = max(0.1, min(float(sample_percent), 100.0))
        self._prefetch_enabled = bool(prefetch_enabled)
        self.logger = get_logger(__name__)

        if pool_max < pool_min:
            pool_max = pool_min

        try:
            self._pool: pool.SimpleConnectionPool = pool.SimpleConnectionPool(
                int(pool_min), int(pool_max), **self.db_config
            )
        except Exception as err:
            self.logger.error("Failed to create PostgreSQL connection pool", exc_info=True)
            raise

        self._numeric_columns = FlowFeatureExtractor.FEATURE_COLUMNS
        self._metadata_columns = list(self.DEFAULT_METADATA_COLUMNS)
        self._select_clause = self._build_select_clause()
        self._last_timings: Dict[str, float] = {}

        self._executor: Optional[concurrent.futures.ThreadPoolExecutor] = None
        self._prefetch_future: Optional[concurrent.futures.Future] = None
        self._prefetch_lock = threading.Lock()
        if self._prefetch_enabled:
            self._executor = concurrent.futures.ThreadPoolExecutor(max_workers=1, thread_name_prefix="telemetry-prefetch")

        self._test_connection()

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------
    def get_network_flows(self, limit: int = 100) -> List[Dict]:
        """Return recent network flows with optional prefetching."""

        limit = max(1, int(limit))

        if not self._prefetch_enabled:
            flows, timings = self._fetch_flows(limit)
            self._last_timings = timings
            return flows

        with self._prefetch_lock:
            if self._prefetch_future and self._prefetch_future.done():
                try:
                    flows, timings = self._prefetch_future.result()
                except Exception as err:  # pragma: no cover - defensive
                    self.logger.warning("Prefetched telemetry failed; falling back to synchronous load", extra={"error": str(err)})
                    flows, timings = self._fetch_flows(limit)
            else:
                flows, timings = self._fetch_flows(limit)

            self._last_timings = timings
            self._prefetch_future = self._schedule_prefetch(limit)

        return flows

    def get_files(self, limit: int = 50) -> List[Dict]:
        """Not used for DDoS testing."""
        return []

    def has_data(self) -> bool:
        """Check if telemetry table has DDoS samples."""
        if self._pool is None:
            return False
        try:
            conn = self._pool.getconn()
            cur = conn.cursor()
            cur.execute(f"SELECT COUNT(*) FROM {self.full_table} WHERE label = %s", ("ddos",))
            count = cur.fetchone()[0]
            cur.close()
            self._pool.putconn(conn)
            return bool(count and count > 0)
        except Exception:
            return False

    def get_last_timings(self) -> Dict[str, float]:
        """Return timing breakdown for the most recent fetch."""
        return dict(self._last_timings)

    def close(self) -> None:
        """Release pooled resources."""
        if self._executor:
            self._executor.shutdown(wait=False)
            self._executor = None
        if self._pool:
            try:
                self._pool.closeall()
            except Exception:
                pass
            finally:
                self._pool = None

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------
    def _test_connection(self) -> None:
        """Validate connectivity and row availability."""
        try:
            start = time.perf_counter()
            conn = self._pool.getconn()
            cur = conn.cursor()
            cur.execute(f"SELECT COUNT(*) FROM {self.full_table} WHERE label = %s", ("ddos",))
            count = cur.fetchone()[0]
            cur.close()
            self._pool.putconn(conn)
            elapsed = (time.perf_counter() - start) * 1000
            self.logger.info(
                "PostgreSQL connection ok", extra={"rows": count, "latency_ms": round(elapsed, 2)}
            )
        except Exception as err:
            self.logger.error("PostgreSQL connection failed", exc_info=True, extra={"error": str(err)})
            raise

    def _schedule_prefetch(self, limit: int):
        if not self._executor:
            return None
        try:
            return self._executor.submit(self._fetch_flows, limit)
        except Exception as err:  # pragma: no cover - defensive
            self.logger.debug("Failed to schedule telemetry prefetch", exc_info=True, extra={"error": str(err)})
            return None

    def _fetch_flows(self, limit: int) -> Tuple[List[Dict], Dict[str, float]]:
        timings: Dict[str, float] = {}
        conn = None
        cur = None
        if self._pool is None:
            self.logger.debug("Connection pool closed; skipping telemetry fetch")
            return [], timings
        try:
            start_connect = time.perf_counter()
            conn = self._pool.getconn()
            timings["connect_ms"] = (time.perf_counter() - start_connect) * 1000

            cur = conn.cursor(cursor_factory=extras.RealDictCursor)

            query, params = self._compose_query(limit)
            start_query = time.perf_counter()
            cur.execute(query, params)
            timings["query_ms"] = (time.perf_counter() - start_query) * 1000

            start_fetch = time.perf_counter()
            rows = cur.fetchmany(limit)
            timings["fetch_ms"] = (time.perf_counter() - start_fetch) * 1000

            start_convert = time.perf_counter()
            flows = self._normalize_rows(rows)
            timings["convert_ms"] = (time.perf_counter() - start_convert) * 1000
            timings["total_ms"] = sum(
                timings.get(key, 0.0) for key in ("connect_ms", "query_ms", "fetch_ms", "convert_ms")
            )

            return flows, timings
        except Exception as err:
            self.logger.error("Failed to load flows from PostgreSQL", exc_info=True, extra={"error": str(err)})
            return [], timings
        finally:
            if cur:
                try:
                    cur.close()
                except Exception:
                    pass
            if conn:
                try:
                    self._pool.putconn(conn)
                except Exception:
                    pass

    def _compose_query(self, limit: int) -> Tuple[str, Tuple]:
        table = self._sample_table if (self._sample_table and self._sample_strategy == "table") else self.full_table

        clauses = [f"SELECT {self._select_clause}", f"FROM {table}"]
        params: list = []

        if self._sample_strategy == "tablesample":
            clauses.append(f"TABLESAMPLE SYSTEM ({self._sample_percent})")

        clauses.append("WHERE label = %s")
        params.append("ddos")

        if self._sample_strategy == "legacy":
            clauses.append("ORDER BY RANDOM()")

        clauses.append("LIMIT %s")
        params.append(limit)

        query = "\n".join(clauses)
        return query, tuple(params)

    def _build_select_clause(self) -> str:
        select_parts = [
            f"COALESCE({col}, 0)::double precision AS {col}" for col in self._numeric_columns
        ]

        for meta in self._metadata_columns:
            if meta in {"src_ip", "dst_ip", "flow_id"}:
                select_parts.append(f"COALESCE({meta}, '') AS {meta}")
            else:
                select_parts.append(meta)

        return ", ".join(select_parts)

    def _normalize_rows(self, rows) -> List[Dict]:
        flows: List[Dict] = []
        for row in rows:
            # RealDictRow behaves like dict; create copy to detach cursor reference
            flow = dict(row)
            for col in self._numeric_columns:
                value = flow.get(col)
                try:
                    flow[col] = float(value) if value is not None else 0.0
                except (TypeError, ValueError):
                    flow[col] = 0.0
            flows.append(flow)
        return flows

    def __del__(self):  # pragma: no cover - defensive cleanup
        try:
            self.close()
        except Exception:
            pass
    
    def get_files(self, limit: int = 50) -> List[Dict]:
        """Not used for DDoS testing."""
        return []
    
    def has_data(self) -> bool:
        """Check if we can connect to DB."""
        try:
            conn = psycopg2.connect(**self.db_config)
            cur = conn.cursor()
            cur.execute(f"SELECT COUNT(*) FROM {self.full_table} WHERE label='ddos'")
            count = cur.fetchone()[0]
            conn.close()
            return count > 0
        except:
            return False
