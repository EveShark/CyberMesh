"""
Anomaly lifecycle tracking with Redis persistence.

Tracks anomaly state transitions from detection through commitment,
enabling acceptance rate calibration and false positive analysis.
"""

import time
import uuid
import math
from enum import Enum
from typing import Dict, List, Optional, Any, Tuple, Iterable
from dataclasses import dataclass

from ..utils.errors import ValidationError, StorageError
from ..config.settings import FeedbackConfig
from .storage import RedisStorage
import logging


class AnomalyState(Enum):
    """Anomaly lifecycle states."""
    DETECTED = "DETECTED"      # AI detected, internal only
    PUBLISHED = "PUBLISHED"    # Sent to Kafka ai.anomalies.v1
    ADMITTED = "ADMITTED"      # In mempool, validators accepted
    COMMITTED = "COMMITTED"    # In finalized block
    REJECTED = "REJECTED"      # Validators explicitly rejected
    TIMEOUT = "TIMEOUT"        # No validator response within window
    EXPIRED = "EXPIRED"        # TTL exceeded before commitment


# Valid state transitions
VALID_TRANSITIONS = {
    AnomalyState.DETECTED: [AnomalyState.PUBLISHED],
    AnomalyState.PUBLISHED: [
        AnomalyState.ADMITTED,
        AnomalyState.REJECTED,
        AnomalyState.TIMEOUT,
        AnomalyState.EXPIRED,
        AnomalyState.COMMITTED,
    ],
    AnomalyState.ADMITTED: [
        AnomalyState.COMMITTED,
        AnomalyState.REJECTED,
        AnomalyState.EXPIRED
    ],
    AnomalyState.COMMITTED: [],
    AnomalyState.REJECTED: [],
    AnomalyState.TIMEOUT: [],
    AnomalyState.EXPIRED: []
}


@dataclass
class AnomalyLifecycle:
    """Complete anomaly lifecycle data."""
    anomaly_id: str
    state: AnomalyState
    detected_at: float
    published_at: Optional[float] = None
    admitted_at: Optional[float] = None
    committed_at: Optional[float] = None
    rejected_at: Optional[float] = None
    timeout_at: Optional[float] = None
    expired_at: Optional[float] = None
    anomaly_type: Optional[str] = None
    confidence: Optional[float] = None
    severity: Optional[int] = None
    raw_score: Optional[float] = None
    validator_id: Optional[str] = None
    block_height: Optional[int] = None
    signature: Optional[str] = None


@dataclass
class AcceptanceMetrics:
    """Acceptance rate metrics for a time window."""
    window_name: str
    window_seconds: int
    detected_count: int
    published_count: int
    admitted_count: int
    committed_count: int
    rejected_count: int
    timeout_count: int
    expired_count: int
    acceptance_rate: float
    admission_rate: float
    commitment_rate: float
    rejection_rate: float
    timeout_rate: float
    has_minimum_samples: bool
    calculated_at: float


@dataclass
class LatencyStats:
    """Latency statistics across lifecycle stages."""
    stage: str
    count: int
    mean_ms: float
    median_ms: float
    p95_ms: float
    p99_ms: float
    min_ms: float
    max_ms: float


class AnomalyLifecycleTracker:
    """
    Tracks anomaly lifecycle from detection through commitment.
    
    Uses hybrid Redis model:
    - Hash per anomaly for primary data
    - Sorted sets for time-based queries
    - Atomic counters for metrics
    
    Security:
    - UUID validation
    - Timestamp verification (clock skew tolerance)
    - State transition validation
    - Rate limiting
    - Audit logging for all changes
    """
    
    def __init__(
        self,
        storage: RedisStorage,
        config: FeedbackConfig,
        logger: Optional[logging.Logger] = None
    ):
        self.storage = storage
        self.config = config
        self.logger = logger or logging.getLogger("lifecycle_tracker")
        
        # Rate limiting state
        self._rate_limit_state: Dict[str, List[float]] = {}
    
    def record_detected(
        self,
        anomaly_id: str,
        anomaly_type: str,
        confidence: float,
        severity: int,
        raw_score: Optional[float] = None,
        timestamp: Optional[float] = None
    ) -> bool:
        """
        Record anomaly detection.
        
        Args:
            anomaly_id: UUID v4 anomaly identifier
            anomaly_type: Type of anomaly (ddos, malware, etc.)
            confidence: Detection confidence [0.0, 1.0] (calibrated score)
            severity: Severity level [1, 10]
            raw_score: Raw model output before calibration [0.0, 1.0] (optional)
            timestamp: Detection timestamp (default: now)
        
        Returns:
            True if recorded successfully
        
        Raises:
            ValidationError: Invalid input
            StorageError: Redis failure
        """
        timestamp = timestamp or time.time()
        
        # Validation
        self._validate_uuid(anomaly_id)
        self._validate_timestamp(timestamp)
        self._validate_confidence(confidence)
        self._validate_severity(severity)
        self._check_rate_limit(anomaly_id)
        
        # Check if anomaly already exists
        if self._anomaly_exists(anomaly_id):
            self.logger.warning(
                "Duplicate detected record",
                extra={"anomaly_id": anomaly_id}
            )
            return True  # Idempotent
        
        # Store lifecycle data
        lifecycle_data = {
            "state": AnomalyState.DETECTED.value,
            "detected_at": str(timestamp),
            "anomaly_type": anomaly_type,
            "confidence": str(confidence),
            "severity": str(severity)
        }
        
        # Store raw_score if provided (for calibration training)
        if raw_score is not None:
            self._validate_confidence(raw_score)  # Validate range
            lifecycle_data["raw_score"] = str(raw_score)
        
        key = self._lifecycle_key(anomaly_id)
        
        try:
            # Store hash
            for field, value in lifecycle_data.items():
                self.storage._client.hset(key, field, value)
            
            # Set TTL
            self.storage._client.expire(key, self.config.lifecycle_ttl_seconds)
            
            # Add to timeline sorted set
            self.storage._client.zadd(
                "anomaly:timeline:all",
                {anomaly_id: timestamp}
            )
            self.storage._client.zadd(
                f"anomaly:timeline:{AnomalyState.DETECTED.value}",
                {anomaly_id: timestamp}
            )
            
            # Increment state counter
            hour_key = self._hour_key(timestamp)
            self.storage._client.hincrby(
                f"metrics:state_counts:hourly:{hour_key}",
                AnomalyState.DETECTED.value,
                1
            )
            self.storage._client.expire(
                f"metrics:state_counts:hourly:{hour_key}",
                8 * 24 * 3600  # 8 days
            )
            
            if self.config.audit_all_state_changes:
                self.logger.info(
                    "Anomaly detected",
                    extra={
                        "anomaly_id": anomaly_id,
                        "anomaly_type": anomaly_type,
                        "confidence": confidence,
                        "severity": severity,
                        "timestamp": timestamp
                    }
                )
            
            return True
            
        except Exception as e:
            raise StorageError(f"Failed to record detected state: {e}") from e
    
    def record_published(
        self,
        anomaly_id: str,
        timestamp: Optional[float] = None
    ) -> bool:
        """
        Record anomaly published to Kafka.
        
        Args:
            anomaly_id: UUID v4 anomaly identifier
            timestamp: Publication timestamp (default: now)
        
        Returns:
            True if recorded successfully
        
        Raises:
            ValidationError: Invalid transition or input
            StorageError: Redis failure
        """
        timestamp = timestamp or time.time()
        
        self._validate_uuid(anomaly_id)
        self._validate_timestamp(timestamp)
        self._check_rate_limit(anomaly_id)
        
        return self._transition_state(
            anomaly_id,
            AnomalyState.PUBLISHED,
            timestamp,
            {"published_at": str(timestamp)}
        )

    def record_batch(self, events: Iterable[Dict[str, Any]]) -> int:
        """Persist a batch of detection events using a single Redis pipeline."""

        events_list = list(events)
        if not events_list:
            return 0

        pipeline = self.storage._client.pipeline(transaction=False)
        processed = 0
        now = time.time()

        for event in events_list:
            anomaly_id = event.get("anomaly_id")
            anomaly_type = event.get("anomaly_type") or "unknown"
            confidence = event.get("confidence")
            severity = event.get("severity")
            timestamp = float(event.get("timestamp") or now)
            raw_score = event.get("raw_score")

            try:
                if anomaly_id is None or confidence is None or severity is None:
                    raise ValidationError("Missing required anomaly fields")

                confidence_val = float(confidence)
                severity_val = int(severity)
                raw_score_val = None
                if raw_score is not None:
                    raw_score_val = float(raw_score)

                anomaly_id_str = str(anomaly_id)
                self._validate_uuid(anomaly_id_str)
                self._validate_timestamp(timestamp)
                self._validate_confidence(confidence_val)
                self._validate_severity(severity_val)
                if raw_score_val is not None:
                    self._validate_confidence(raw_score_val)
                self._check_rate_limit(anomaly_id_str)

                if self._anomaly_exists(anomaly_id_str):
                    self.logger.warning(
                        "Duplicate anomaly in tracker batch",
                        extra={"anomaly_id": anomaly_id}
                    )
                    continue

                key = self._lifecycle_key(anomaly_id_str)
                mapping = {
                    "state": AnomalyState.PUBLISHED.value,
                    "detected_at": str(timestamp),
                    "published_at": str(timestamp),
                    "anomaly_type": anomaly_type,
                    "confidence": str(confidence_val),
                    "severity": str(severity_val),
                }
                if raw_score_val is not None:
                    mapping["raw_score"] = str(raw_score_val)

                pipeline.hset(key, mapping=mapping)
                pipeline.expire(key, self.config.lifecycle_ttl_seconds)

                pipeline.zadd("anomaly:timeline:all", {anomaly_id_str: timestamp})
                pipeline.zadd(
                    f"anomaly:timeline:{AnomalyState.DETECTED.value}",
                    {anomaly_id_str: timestamp}
                )
                pipeline.zadd(
                    f"anomaly:timeline:{AnomalyState.PUBLISHED.value}",
                    {anomaly_id_str: timestamp}
                )

                hour_key = self._hour_key(timestamp)
                metrics_key = f"metrics:state_counts:hourly:{hour_key}"
                pipeline.hincrby(
                    metrics_key,
                    AnomalyState.DETECTED.value,
                    1
                )
                pipeline.hincrby(
                    metrics_key,
                    AnomalyState.PUBLISHED.value,
                    1
                )
                pipeline.expire(metrics_key, 8 * 24 * 3600)

                processed += 1

            except ValidationError as err:
                self.logger.warning(
                    "Skipping invalid anomaly in tracker batch",
                    extra={
                        "anomaly_id": anomaly_id,
                        "error": str(err),
                    },
                )
                continue

        if processed == 0:
            return 0

        try:
            pipeline.execute()
        except Exception as exc:
            raise StorageError(f"Failed to execute tracker batch: {exc}") from exc

        if self.config.audit_all_state_changes and processed:
            self.logger.info(
                "Anomaly tracker batch persisted",
                extra={"count": processed}
            )

        return processed
    
    def record_admitted(
        self,
        anomaly_id: str,
        validator_id: str,
        timestamp: Optional[float] = None
    ) -> bool:
        """
        Record anomaly admitted to mempool.
        
        Args:
            anomaly_id: UUID v4 anomaly identifier
            validator_id: Validator node ID
            timestamp: Admission timestamp (default: now)
        
        Returns:
            True if recorded successfully
        
        Raises:
            ValidationError: Invalid transition or input
            StorageError: Redis failure
        """
        timestamp = timestamp or time.time()
        
        self._validate_uuid(anomaly_id)
        self._validate_timestamp(timestamp)
        self._check_rate_limit(anomaly_id)
        
        return self._transition_state(
            anomaly_id,
            AnomalyState.ADMITTED,
            timestamp,
            {
                "admitted_at": str(timestamp),
                "validator_id": validator_id
            }
        )
    
    def record_committed(
        self,
        anomaly_id: str,
        block_height: int,
        signature: str,
        timestamp: Optional[float] = None
    ) -> bool:
        """
        Record anomaly committed to block.
        
        Args:
            anomaly_id: UUID v4 anomaly identifier
            block_height: Block height where committed
            signature: Ed25519 signature from backend
            timestamp: Commitment timestamp (default: now)
        
        Returns:
            True if recorded successfully
        
        Raises:
            ValidationError: Invalid transition or input
            StorageError: Redis failure
        """
        timestamp = timestamp or time.time()
        
        self._validate_uuid(anomaly_id)
        self._validate_timestamp(timestamp)
        self._check_rate_limit(anomaly_id)
        
        if self.config.require_signature_for_backend and not signature:
            raise ValidationError("Backend signature required for COMMITTED state")
        
        return self._transition_state(
            anomaly_id,
            AnomalyState.COMMITTED,
            timestamp,
            {
                "committed_at": str(timestamp),
                "block_height": str(block_height),
                "signature": signature
            }
        )
    
    def record_rejected(
        self,
        anomaly_id: str,
        signature: str,
        timestamp: Optional[float] = None
    ) -> bool:
        """
        Record anomaly rejected by validators.
        
        Args:
            anomaly_id: UUID v4 anomaly identifier
            signature: Ed25519 signature from backend
            timestamp: Rejection timestamp (default: now)
        
        Returns:
            True if recorded successfully
        
        Raises:
            ValidationError: Invalid transition or input
            StorageError: Redis failure
        """
        timestamp = timestamp or time.time()
        
        self._validate_uuid(anomaly_id)
        self._validate_timestamp(timestamp)
        self._check_rate_limit(anomaly_id)
        
        if self.config.require_signature_for_backend and not signature:
            raise ValidationError("Backend signature required for REJECTED state")
        
        return self._transition_state(
            anomaly_id,
            AnomalyState.REJECTED,
            timestamp,
            {
                "rejected_at": str(timestamp),
                "signature": signature
            }
        )
    
    def record_timeout(
        self,
        anomaly_id: str,
        timestamp: Optional[float] = None
    ) -> bool:
        """
        Record anomaly timeout (no validator response).
        
        Args:
            anomaly_id: UUID v4 anomaly identifier
            timestamp: Timeout timestamp (default: now)
        
        Returns:
            True if recorded successfully
        
        Raises:
            ValidationError: Invalid transition or input
            StorageError: Redis failure
        """
        timestamp = timestamp or time.time()
        
        self._validate_uuid(anomaly_id)
        self._validate_timestamp(timestamp)
        self._check_rate_limit(anomaly_id)
        
        return self._transition_state(
            anomaly_id,
            AnomalyState.TIMEOUT,
            timestamp,
            {"timeout_at": str(timestamp)}
        )
    
    def record_expired(
        self,
        anomaly_id: str,
        timestamp: Optional[float] = None
    ) -> bool:
        """
        Record anomaly expired (TTL exceeded).
        
        Args:
            anomaly_id: UUID v4 anomaly identifier
            timestamp: Expiry timestamp (default: now)
        
        Returns:
            True if recorded successfully
        
        Raises:
            ValidationError: Invalid transition or input
            StorageError: Redis failure
        """
        timestamp = timestamp or time.time()
        
        self._validate_uuid(anomaly_id)
        self._validate_timestamp(timestamp)
        self._check_rate_limit(anomaly_id)
        
        return self._transition_state(
            anomaly_id,
            AnomalyState.EXPIRED,
            timestamp,
            {"expired_at": str(timestamp)}
        )
    
    def get_lifecycle(self, anomaly_id: str) -> Optional[AnomalyLifecycle]:
        """
        Get complete lifecycle data for anomaly.
        
        Args:
            anomaly_id: UUID v4 anomaly identifier
        
        Returns:
            AnomalyLifecycle object or None if not found
        """
        self._validate_uuid(anomaly_id)
        
        key = self._lifecycle_key(anomaly_id)
        data = self.storage._client.hgetall(key)
        
        if not data:
            return None
        
        return AnomalyLifecycle(
            anomaly_id=anomaly_id,
            state=AnomalyState(data.get("state")),
            detected_at=float(data.get("detected_at", 0)),
            published_at=float(data.get("published_at", 0)) or None,
            admitted_at=float(data.get("admitted_at", 0)) or None,
            committed_at=float(data.get("committed_at", 0)) or None,
            rejected_at=float(data.get("rejected_at", 0)) or None,
            timeout_at=float(data.get("timeout_at", 0)) or None,
            expired_at=float(data.get("expired_at", 0)) or None,
            anomaly_type=data.get("anomaly_type"),
            confidence=float(data.get("confidence", 0)) or None,
            severity=int(data.get("severity", 0)) or None,
            raw_score=float(data.get("raw_score", 0)) or None,
            validator_id=data.get("validator_id"),
            block_height=int(data.get("block_height", 0)) or None,
            signature=data.get("signature")
        )
    
    def get_acceptance_metrics(
        self,
        window_name: str = "realtime"
    ) -> Optional[AcceptanceMetrics]:
        """
        Calculate acceptance metrics for time window (all anomaly types combined).
        
        Args:
            window_name: Window name (realtime, short, medium, long)
        
        Returns:
            AcceptanceMetrics object or None if insufficient data
        """
        window_seconds = getattr(
            self.config,
            f"metric_window_{window_name}",
            self.config.metric_window_realtime
        )
        min_samples = getattr(
            self.config,
            f"min_samples_{window_name}",
            self.config.min_samples_realtime
        )
        
        now = time.time()
        start_time = now - window_seconds
        
        # Get state counts from hourly buckets
        counts = self._get_state_counts_in_range(start_time, now)
        
        detected = counts.get(AnomalyState.DETECTED.value, 0)
        published = counts.get(AnomalyState.PUBLISHED.value, 0)
        admitted = counts.get(AnomalyState.ADMITTED.value, 0)
        committed = counts.get(AnomalyState.COMMITTED.value, 0)
        rejected = counts.get(AnomalyState.REJECTED.value, 0)
        timeout = counts.get(AnomalyState.TIMEOUT.value, 0)
        expired = counts.get(AnomalyState.EXPIRED.value, 0)
        
        # Check minimum samples
        has_minimum = published >= min_samples
        
        # Calculate rates
        acceptance_rate = committed / published if published > 0 else 0.0
        admission_rate = admitted / published if published > 0 else 0.0
        commitment_rate = committed / admitted if admitted > 0 else 0.0
        rejection_rate = rejected / published if published > 0 else 0.0
        timeout_rate = timeout / published if published > 0 else 0.0
        
        return AcceptanceMetrics(
            window_name=window_name,
            window_seconds=window_seconds,
            detected_count=detected,
            published_count=published,
            admitted_count=admitted,
            committed_count=committed,
            rejected_count=rejected,
            timeout_count=timeout,
            expired_count=expired,
            acceptance_rate=acceptance_rate,
            admission_rate=admission_rate,
            commitment_rate=commitment_rate,
            rejection_rate=rejection_rate,
            timeout_rate=timeout_rate,
            has_minimum_samples=has_minimum,
            calculated_at=now
        )
    
    def get_acceptance_metrics_by_type(
        self,
        anomaly_type: str,
        window_name: str = "realtime"
    ) -> Optional[AcceptanceMetrics]:
        """
        Calculate acceptance metrics for specific anomaly type in time window.
        
        This method queries individual anomaly lifecycles to filter by type.
        More expensive than aggregate metrics but necessary for per-type thresholds.
        
        Args:
            anomaly_type: Anomaly type to filter (ddos, malware, etc.)
            window_name: Window name (realtime, short, medium, long)
        
        Returns:
            AcceptanceMetrics object or None if insufficient data
        """
        window_seconds = getattr(
            self.config,
            f"metric_window_{window_name}",
            self.config.metric_window_realtime
        )
        min_samples = getattr(
            self.config,
            f"min_samples_{window_name}",
            self.config.min_samples_realtime
        )
        
        now = time.time()
        start_time = now - window_seconds
        
        # Get counts by scanning lifecycles (expensive but accurate)
        counts = {
            "DETECTED": 0,
            "PUBLISHED": 0,
            "ADMITTED": 0,
            "COMMITTED": 0,
            "REJECTED": 0,
            "TIMEOUT": 0,
            "EXPIRED": 0
        }
        
        # Scan timeline sorted sets for each state within time window
        for state in AnomalyState:
            timeline_key = f"anomaly:timeline:{state.value}"
            
            # Get anomaly IDs in time window
            anomaly_ids = self.storage._client.zrangebyscore(
                timeline_key,
                start_time,
                now
            )
            
            # Filter by anomaly type
            for aid in anomaly_ids:
                lifecycle = self.get_lifecycle(aid)
                if lifecycle and lifecycle.anomaly_type == anomaly_type:
                    counts[state.value] += 1
        
        detected = counts["DETECTED"]
        published = counts["PUBLISHED"]
        admitted = counts["ADMITTED"]
        committed = counts["COMMITTED"]
        rejected = counts["REJECTED"]
        timeout = counts["TIMEOUT"]
        expired = counts["EXPIRED"]
        
        # Check minimum samples
        has_minimum = published >= min_samples
        
        # Calculate rates
        acceptance_rate = committed / published if published > 0 else 0.0
        admission_rate = admitted / published if published > 0 else 0.0
        commitment_rate = committed / admitted if admitted > 0 else 0.0
        rejection_rate = rejected / published if published > 0 else 0.0
        timeout_rate = timeout / published if published > 0 else 0.0
        
        return AcceptanceMetrics(
            window_name=window_name,
            window_seconds=window_seconds,
            detected_count=detected,
            published_count=published,
            admitted_count=admitted,
            committed_count=committed,
            rejected_count=rejected,
            timeout_count=timeout,
            expired_count=expired,
            acceptance_rate=acceptance_rate,
            admission_rate=admission_rate,
            commitment_rate=commitment_rate,
            rejection_rate=rejection_rate,
            timeout_rate=timeout_rate,
            has_minimum_samples=has_minimum,
            calculated_at=now
        )
    
    def _transition_state(
        self,
        anomaly_id: str,
        new_state: AnomalyState,
        timestamp: float,
        additional_fields: Dict[str, str]
    ) -> bool:
        """Execute state transition with validation."""
        key = self._lifecycle_key(anomaly_id)
        
        # Get current state
        current_state_str = self.storage._client.hget(key, "state")
        if not current_state_str:
            raise ValidationError(f"Anomaly {anomaly_id} not found")
        
        current_state = AnomalyState(current_state_str)
        
        # Validate transition
        if new_state not in VALID_TRANSITIONS[current_state]:
            raise ValidationError(
                f"Invalid transition from {current_state.value} to {new_state.value}"
            )
        
        # Check timestamp monotonicity
        self._validate_monotonic_timestamp(key, timestamp)
        
        try:
            # Update state
            self.storage._client.hset(key, "state", new_state.value)
            
            # Update additional fields
            for field, value in additional_fields.items():
                self.storage._client.hset(key, field, value)
            
            # Update timeline sorted set
            self.storage._client.zadd(
                f"anomaly:timeline:{new_state.value}",
                {anomaly_id: timestamp}
            )
            
            # Increment state counter
            hour_key = self._hour_key(timestamp)
            self.storage._client.hincrby(
                f"metrics:state_counts:hourly:{hour_key}",
                new_state.value,
                1
            )
            self.storage._client.expire(
                f"metrics:state_counts:hourly:{hour_key}",
                8 * 24 * 3600
            )
            
            if self.config.audit_all_state_changes:
                self.logger.info(
                    "State transition",
                    extra={
                        "anomaly_id": anomaly_id,
                        "old_state": current_state.value,
                        "new_state": new_state.value,
                        "timestamp": timestamp,
                        "fields": list(additional_fields.keys())
                    }
                )
            
            return True
            
        except Exception as e:
            raise StorageError(f"Failed to transition state: {e}") from e
    
    def _anomaly_exists(self, anomaly_id: str) -> bool:
        """Check if anomaly exists in Redis."""
        return self.storage._client.exists(self._lifecycle_key(anomaly_id))
    
    def _get_state_counts_in_range(
        self,
        start_time: float,
        end_time: float
    ) -> Dict[str, int]:
        """Get aggregated state counts for time range."""
        start_hour = int(start_time // 3600)
        end_hour = int(end_time // 3600)
        
        counts: Dict[str, int] = {}
        
        for hour in range(start_hour, end_hour + 1):
            hour_key = str(hour)
            hour_counts = self.storage._client.hgetall(
                f"metrics:state_counts:hourly:{hour_key}"
            )
            
            for state, count_str in hour_counts.items():
                count = int(count_str)
                counts[state] = counts.get(state, 0) + count
        
        return counts
    
    def _validate_uuid(self, anomaly_id: str):
        """Validate UUID v4 format."""
        try:
            parsed = uuid.UUID(anomaly_id, version=4)
            if str(parsed) != anomaly_id:
                raise ValidationError(f"Invalid UUID v4: {anomaly_id}")
        except (ValueError, AttributeError) as e:
            raise ValidationError(f"Invalid UUID v4: {anomaly_id}") from e
    
    def _validate_timestamp(self, timestamp: float):
        """Validate timestamp within clock skew tolerance."""
        now = time.time()
        delta = abs(timestamp - now)
        
        if delta > self.config.clock_skew_tolerance_seconds:
            raise ValidationError(
                f"Timestamp {timestamp} outside clock skew tolerance "
                f"(delta: {delta}s, max: {self.config.clock_skew_tolerance_seconds}s)"
            )
    
    def _validate_confidence(self, confidence: float):
        """Validate confidence in [0.0, 1.0]."""
        if not (0.0 <= confidence <= 1.0):
            raise ValidationError(f"Confidence must be in [0.0, 1.0], got {confidence}")
    
    def _validate_severity(self, severity: int):
        """Validate severity in [1, 10]."""
        if not (1 <= severity <= 10):
            raise ValidationError(f"Severity must be in [1, 10], got {severity}")
    
    def _validate_monotonic_timestamp(self, key: str, timestamp: float):
        """Validate timestamp is not older than existing timestamps."""
        existing_timestamps = []
        
        for field in ["detected_at", "published_at", "admitted_at", "committed_at",
                      "rejected_at", "timeout_at", "expired_at"]:
            ts_str = self.storage._client.hget(key, field)
            if ts_str:
                existing_timestamps.append(float(ts_str))
        
        if existing_timestamps and timestamp < max(existing_timestamps):
            raise ValidationError(
                f"Timestamp {timestamp} is older than existing timestamps"
            )
    
    def _check_rate_limit(self, anomaly_id: str):
        """Check and enforce rate limits."""
        now = time.time()
        
        # Clean old entries
        if anomaly_id in self._rate_limit_state:
            self._rate_limit_state[anomaly_id] = [
                ts for ts in self._rate_limit_state[anomaly_id]
                if now - ts < 1.0
            ]
        else:
            self._rate_limit_state[anomaly_id] = []
        
        # Check limit
        if len(self._rate_limit_state[anomaly_id]) >= self.config.rate_limit_per_anomaly:
            raise ValidationError(
                f"Rate limit exceeded for anomaly {anomaly_id}: "
                f"{len(self._rate_limit_state[anomaly_id])} updates in last second"
            )
        
        # Record this request
        self._rate_limit_state[anomaly_id].append(now)
    
    def _lifecycle_key(self, anomaly_id: str) -> str:
        """Get Redis key for anomaly lifecycle."""
        return f"anomaly:lifecycle:{anomaly_id}"
    
    def _hour_key(self, timestamp: float) -> str:
        """Get hourly bucket key from timestamp."""
        return str(int(timestamp // 3600))
