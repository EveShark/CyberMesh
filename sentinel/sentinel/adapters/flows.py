"""Flow adapters for converting raw flow data to NetworkFlowFeaturesV1."""

from abc import ABC, abstractmethod
from typing import Dict, Any, Optional, List

from ..contracts.schemas import NetworkFlowFeaturesV1
from ..logging import get_logger

logger = get_logger(__name__)


class FlowAdapter(ABC):
    """
    Abstract base class for flow adapters.
    
    Flow adapters convert dataset-specific row formats into the canonical
    NetworkFlowFeaturesV1 schema. Each adapter handles one source format.
    """
    
    @property
    @abstractmethod
    def adapter_id(self) -> str:
        """Unique adapter identifier (e.g., 'cicddos2019_v1')."""
        pass
    
    @property
    @abstractmethod
    def source_schema(self) -> str:
        """Source schema name (e.g., 'CIC-DDoS2019')."""
        pass
    
    @abstractmethod
    def to_network_flow_v1(self, row: Dict[str, Any]) -> NetworkFlowFeaturesV1:
        """
        Convert a raw flow row to NetworkFlowFeaturesV1.
        
        Args:
            row: Dictionary containing flow data in source format
            
        Returns:
            NetworkFlowFeaturesV1 instance with mapped fields
            
        Raises:
            ValueError: If required fields are missing or invalid
        """
        pass
    
    def validate(self, features: NetworkFlowFeaturesV1) -> List[str]:
        """
        Validate a NetworkFlowFeaturesV1 instance.
        
        Args:
            features: Features to validate
            
        Returns:
            List of validation error messages (empty if valid)
        """
        errors = []
        
        # Check required fields
        for field in NetworkFlowFeaturesV1.required_fields():
            value = getattr(features, field, None)
            if value is None or (isinstance(value, str) and not value):
                errors.append(f"Required field '{field}' is missing or empty")
        
        # Check non-negative counts
        count_fields = [
            "tot_fwd_pkts", "tot_bwd_pkts", "totlen_fwd_pkts", "totlen_bwd_pkts",
            "fin_flag_cnt", "syn_flag_cnt", "rst_flag_cnt", "psh_flag_cnt",
            "ack_flag_cnt", "urg_flag_cnt",
        ]
        for field in count_fields:
            value = getattr(features, field, 0)
            if value < 0:
                errors.append(f"Field '{field}' must be non-negative, got {value}")
        
        # Check non-negative duration
        if features.flow_duration < 0:
            errors.append(f"flow_duration must be non-negative, got {features.flow_duration}")
        
        # Check valid protocol
        valid_protocols = {1, 6, 17, 47, 50, 51, 58, 89, 132}  # Common IANA protocols
        if features.protocol not in valid_protocols:
            logger.warning(f"Unusual protocol number: {features.protocol}")
        
        # Check port ranges
        if not (0 <= features.src_port <= 65535):
            errors.append(f"src_port out of range: {features.src_port}")
        if not (0 <= features.dst_port <= 65535):
            errors.append(f"dst_port out of range: {features.dst_port}")
        
        return errors


class CICDDoS2019Adapter(FlowAdapter):
    """
    Adapter for CIC-DDoS2019 dataset format.
    
    Maps the 79 columns from CIC-DDoS2019 to NetworkFlowFeaturesV1.
    Column names are expected to match the dataset exactly (lowercase with underscores).
    
    Reference: https://www.unb.ca/cic/datasets/ddos-2019.html
    """
    
    # CIC-DDoS2019 column names (79 columns, matches FlowFeatureExtractor)
    CIC_COLUMNS = [
        'src_port', 'dst_port', 'protocol', 'flow_duration',
        'tot_fwd_pkts', 'tot_bwd_pkts', 'totlen_fwd_pkts', 'totlen_bwd_pkts',
        'fwd_pkt_len_max', 'fwd_pkt_len_min', 'fwd_pkt_len_mean', 'fwd_pkt_len_std',
        'bwd_pkt_len_max', 'bwd_pkt_len_min', 'bwd_pkt_len_mean', 'bwd_pkt_len_std',
        'flow_byts_s', 'flow_pkts_s',
        'flow_iat_mean', 'flow_iat_std', 'flow_iat_max', 'flow_iat_min',
        'fwd_iat_tot', 'fwd_iat_mean', 'fwd_iat_std', 'fwd_iat_max', 'fwd_iat_min',
        'bwd_iat_tot', 'bwd_iat_mean', 'bwd_iat_std', 'bwd_iat_max', 'bwd_iat_min',
        'fwd_psh_flags', 'bwd_psh_flags', 'fwd_urg_flags', 'bwd_urg_flags',
        'fwd_header_len', 'bwd_header_len', 'fwd_pkts_s', 'bwd_pkts_s',
        'pkt_len_min', 'pkt_len_max', 'pkt_len_mean', 'pkt_len_std', 'pkt_len_var',
        'fin_flag_cnt', 'syn_flag_cnt', 'rst_flag_cnt', 'psh_flag_cnt',
        'ack_flag_cnt', 'urg_flag_cnt', 'cwe_flag_count', 'ece_flag_cnt',
        'down_up_ratio', 'pkt_size_avg', 'fwd_seg_size_avg', 'bwd_seg_size_avg',
        'fwd_byts_b_avg', 'fwd_pkts_b_avg', 'fwd_blk_rate_avg',
        'bwd_byts_b_avg', 'bwd_pkts_b_avg', 'bwd_blk_rate_avg',
        'subflow_fwd_pkts', 'subflow_fwd_byts', 'subflow_bwd_pkts', 'subflow_bwd_byts',
        'init_fwd_win_byts', 'init_bwd_win_byts', 'fwd_act_data_pkts', 'fwd_seg_size_min',
        'active_mean', 'active_std', 'active_max', 'active_min',
        'idle_mean', 'idle_std', 'idle_max', 'idle_min'
    ]
    
    # Protocol string to number mapping
    PROTOCOL_MAP = {'TCP': 6, 'UDP': 17, 'ICMP': 1}
    
    @property
    def adapter_id(self) -> str:
        return "cicddos2019_v1"
    
    @property
    def source_schema(self) -> str:
        return "CIC-DDoS2019"
    
    def to_network_flow_v1(self, row: Dict[str, Any]) -> NetworkFlowFeaturesV1:
        """
        Convert a CIC-DDoS2019 row to NetworkFlowFeaturesV1.
        
        Args:
            row: Dictionary with CIC-DDoS2019 column names as keys
            
        Returns:
            NetworkFlowFeaturesV1 instance
            
        Raises:
            ValueError: If required fields are missing
        """
        # Extract IPs if present (not in 79-column feature set but may be in raw data)
        src_ip = row.get('src_ip', row.get('Source IP', '0.0.0.0'))
        dst_ip = row.get('dst_ip', row.get('Destination IP', '0.0.0.0'))
        
        # Handle protocol (may be string or int)
        protocol = row.get('protocol', 0)
        if isinstance(protocol, str):
            protocol = self.PROTOCOL_MAP.get(protocol.upper(), 0)
        
        # Build features with safe extraction
        features = NetworkFlowFeaturesV1(
            # Required identity fields
            src_ip=str(src_ip),
            dst_ip=str(dst_ip),
            src_port=self._safe_int(row.get('src_port', 0)),
            dst_port=self._safe_int(row.get('dst_port', 0)),
            protocol=int(protocol),
            
            # Required volume fields
            tot_fwd_pkts=self._safe_int(row.get('tot_fwd_pkts', 0)),
            tot_bwd_pkts=self._safe_int(row.get('tot_bwd_pkts', 0)),
            totlen_fwd_pkts=self._safe_int(row.get('totlen_fwd_pkts', 0)),
            totlen_bwd_pkts=self._safe_int(row.get('totlen_bwd_pkts', 0)),
            
            # Required timing
            flow_duration=self._safe_float(row.get('flow_duration', 0)),
            
            # Required rates
            flow_byts_s=self._safe_float(row.get('flow_byts_s', 0)),
            flow_pkts_s=self._safe_float(row.get('flow_pkts_s', 0)),
            
            # Optional rates
            fwd_pkts_s=self._safe_float(row.get('fwd_pkts_s', 0)),
            bwd_pkts_s=self._safe_float(row.get('bwd_pkts_s', 0)),
            
            # Packet length stats
            fwd_pkt_len_max=self._safe_float(row.get('fwd_pkt_len_max', 0)),
            fwd_pkt_len_min=self._safe_float(row.get('fwd_pkt_len_min', 0)),
            fwd_pkt_len_mean=self._safe_float(row.get('fwd_pkt_len_mean', 0)),
            fwd_pkt_len_std=self._safe_float(row.get('fwd_pkt_len_std', 0)),
            bwd_pkt_len_max=self._safe_float(row.get('bwd_pkt_len_max', 0)),
            bwd_pkt_len_min=self._safe_float(row.get('bwd_pkt_len_min', 0)),
            bwd_pkt_len_mean=self._safe_float(row.get('bwd_pkt_len_mean', 0)),
            bwd_pkt_len_std=self._safe_float(row.get('bwd_pkt_len_std', 0)),
            pkt_len_min=self._safe_float(row.get('pkt_len_min', 0)),
            pkt_len_max=self._safe_float(row.get('pkt_len_max', 0)),
            pkt_len_mean=self._safe_float(row.get('pkt_len_mean', 0)),
            pkt_len_std=self._safe_float(row.get('pkt_len_std', 0)),
            pkt_len_var=self._safe_float(row.get('pkt_len_var', 0)),
            
            # Inter-arrival time stats
            flow_iat_mean=self._safe_float(row.get('flow_iat_mean', 0)),
            flow_iat_std=self._safe_float(row.get('flow_iat_std', 0)),
            flow_iat_max=self._safe_float(row.get('flow_iat_max', 0)),
            flow_iat_min=self._safe_float(row.get('flow_iat_min', 0)),
            fwd_iat_tot=self._safe_float(row.get('fwd_iat_tot', 0)),
            fwd_iat_mean=self._safe_float(row.get('fwd_iat_mean', 0)),
            fwd_iat_std=self._safe_float(row.get('fwd_iat_std', 0)),
            fwd_iat_max=self._safe_float(row.get('fwd_iat_max', 0)),
            fwd_iat_min=self._safe_float(row.get('fwd_iat_min', 0)),
            bwd_iat_tot=self._safe_float(row.get('bwd_iat_tot', 0)),
            bwd_iat_mean=self._safe_float(row.get('bwd_iat_mean', 0)),
            bwd_iat_std=self._safe_float(row.get('bwd_iat_std', 0)),
            bwd_iat_max=self._safe_float(row.get('bwd_iat_max', 0)),
            bwd_iat_min=self._safe_float(row.get('bwd_iat_min', 0)),
            
            # TCP flags
            fin_flag_cnt=self._safe_int(row.get('fin_flag_cnt', 0)),
            syn_flag_cnt=self._safe_int(row.get('syn_flag_cnt', 0)),
            rst_flag_cnt=self._safe_int(row.get('rst_flag_cnt', 0)),
            psh_flag_cnt=self._safe_int(row.get('psh_flag_cnt', 0)),
            ack_flag_cnt=self._safe_int(row.get('ack_flag_cnt', 0)),
            urg_flag_cnt=self._safe_int(row.get('urg_flag_cnt', 0)),
            cwe_flag_count=self._safe_int(row.get('cwe_flag_count', 0)),
            ece_flag_cnt=self._safe_int(row.get('ece_flag_cnt', 0)),
            fwd_psh_flags=self._safe_int(row.get('fwd_psh_flags', 0)),
            bwd_psh_flags=self._safe_int(row.get('bwd_psh_flags', 0)),
            fwd_urg_flags=self._safe_int(row.get('fwd_urg_flags', 0)),
            bwd_urg_flags=self._safe_int(row.get('bwd_urg_flags', 0)),
            
            # Header
            fwd_header_len=self._safe_int(row.get('fwd_header_len', 0)),
            bwd_header_len=self._safe_int(row.get('bwd_header_len', 0)),
            
            # Derived/ratio
            down_up_ratio=self._safe_float(row.get('down_up_ratio', 0)),
            pkt_size_avg=self._safe_float(row.get('pkt_size_avg', 0)),
            fwd_seg_size_avg=self._safe_float(row.get('fwd_seg_size_avg', 0)),
            bwd_seg_size_avg=self._safe_float(row.get('bwd_seg_size_avg', 0)),
            
            # Bulk stats
            fwd_byts_b_avg=self._safe_float(row.get('fwd_byts_b_avg', 0)),
            fwd_pkts_b_avg=self._safe_float(row.get('fwd_pkts_b_avg', 0)),
            fwd_blk_rate_avg=self._safe_float(row.get('fwd_blk_rate_avg', 0)),
            bwd_byts_b_avg=self._safe_float(row.get('bwd_byts_b_avg', 0)),
            bwd_pkts_b_avg=self._safe_float(row.get('bwd_pkts_b_avg', 0)),
            bwd_blk_rate_avg=self._safe_float(row.get('bwd_blk_rate_avg', 0)),
            
            # Subflow
            subflow_fwd_pkts=self._safe_int(row.get('subflow_fwd_pkts', 0)),
            subflow_fwd_byts=self._safe_int(row.get('subflow_fwd_byts', 0)),
            subflow_bwd_pkts=self._safe_int(row.get('subflow_bwd_pkts', 0)),
            subflow_bwd_byts=self._safe_int(row.get('subflow_bwd_byts', 0)),
            
            # Window
            init_fwd_win_byts=self._safe_int(row.get('init_fwd_win_byts', 0)),
            init_bwd_win_byts=self._safe_int(row.get('init_bwd_win_byts', 0)),
            fwd_act_data_pkts=self._safe_int(row.get('fwd_act_data_pkts', 0)),
            fwd_seg_size_min=self._safe_int(row.get('fwd_seg_size_min', 0)),
            
            # Active/idle
            active_mean=self._safe_float(row.get('active_mean', 0)),
            active_std=self._safe_float(row.get('active_std', 0)),
            active_max=self._safe_float(row.get('active_max', 0)),
            active_min=self._safe_float(row.get('active_min', 0)),
            idle_mean=self._safe_float(row.get('idle_mean', 0)),
            idle_std=self._safe_float(row.get('idle_std', 0)),
            idle_max=self._safe_float(row.get('idle_max', 0)),
            idle_min=self._safe_float(row.get('idle_min', 0)),
        )
        
        # Compute derived semantic fields if not present
        features = self._compute_derived_fields(features)
        
        # Validate
        errors = self.validate(features)
        if errors:
            logger.warning(f"Validation warnings for CIC row: {errors}")
        
        return features
    
    def _safe_float(self, value: Any) -> float:
        """Safely convert value to float, handling NaN/Inf."""
        if value is None:
            return 0.0
        try:
            f = float(value)
            if f != f:  # NaN check
                return 0.0
            if abs(f) == float('inf'):
                return 1e9 if f > 0 else -1e9
            return f
        except (ValueError, TypeError):
            return 0.0
    
    def _safe_int(self, value: Any) -> int:
        """Safely convert value to int."""
        if value is None:
            return 0
        try:
            return int(float(value))
        except (ValueError, TypeError):
            return 0
    
    def _compute_derived_fields(self, features: NetworkFlowFeaturesV1) -> NetworkFlowFeaturesV1:
        """Compute derived semantic fields if not already set."""
        # PPS (packets per second)
        if features.pps is None and features.flow_duration > 0:
            total_pkts = features.tot_fwd_pkts + features.tot_bwd_pkts
            features.pps = total_pkts / (features.flow_duration / 1e6)  # duration in μs
        
        # BPS (bytes per second)
        if features.bps is None and features.flow_duration > 0:
            total_bytes = features.totlen_fwd_pkts + features.totlen_bwd_pkts
            features.bps = total_bytes / (features.flow_duration / 1e6)
        
        # SYN/ACK ratio
        if features.syn_ack_ratio is None and features.ack_flag_cnt > 0:
            features.syn_ack_ratio = features.syn_flag_cnt / features.ack_flag_cnt
        
        return features
