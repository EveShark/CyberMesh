"""Flow adapters for converting raw telemetry to canonical NetworkFlowFeaturesV1."""

import math
import uuid
import time
from abc import ABC, abstractmethod
from dataclasses import asdict
from typing import Dict, Any, List, Optional

from .contracts import NetworkFlowFeaturesV1, CanonicalEvent, Modality
from ...logging import get_logger

logger = get_logger(__name__)


class FlowAdapter(ABC):
    """
    Abstract base class for flow adapters.
    
    Adapters convert raw flow records from various sources/formats
    into the canonical NetworkFlowFeaturesV1 schema.
    """
    
    @property
    @abstractmethod
    def adapter_id(self) -> str:
        """Unique identifier for this adapter."""
        pass
    
    @property
    @abstractmethod
    def source_format(self) -> str:
        """Description of the source format (e.g., 'CIC-DDoS2019', 'NetFlow v9')."""
        pass
    
    @abstractmethod
    def to_network_flow_features(self, row: Dict[str, Any]) -> NetworkFlowFeaturesV1:
        """
        Convert a raw flow record to NetworkFlowFeaturesV1.
        
        Args:
            row: Raw flow record as dictionary
            
        Returns:
            NetworkFlowFeaturesV1 instance
            
        Raises:
            ValueError: If required fields are missing or invalid
        """
        pass
    
    def to_canonical_event(
        self,
        row: Dict[str, Any],
        tenant_id: str,
        source: Optional[str] = None,
    ) -> CanonicalEvent:
        """
        Convert a raw flow record to a CanonicalEvent.
        
        Args:
            row: Raw flow record
            tenant_id: Tenant identifier
            source: Event source (defaults to adapter_id)
            
        Returns:
            CanonicalEvent with NetworkFlowFeaturesV1
        """
        features = self.to_network_flow_features(row)
        
        return CanonicalEvent(
            id=str(uuid.uuid4()),
            timestamp=time.time(),
            source=source or self.adapter_id,
            tenant_id=tenant_id,
            modality=Modality.NETWORK_FLOW,
            features_version="NetworkFlowFeaturesV1",
            features=features.to_dict(),
            raw_context={"original_row": row},
        )
    
    def to_canonical_events_batch(
        self,
        rows: List[Dict[str, Any]],
        tenant_id: str,
        source: Optional[str] = None,
    ) -> List[CanonicalEvent]:
        """
        Convert a batch of raw flow records to CanonicalEvents.
        
        Args:
            rows: List of raw flow records
            tenant_id: Tenant identifier
            source: Event source
            
        Returns:
            List of CanonicalEvents (skips invalid rows)
        """
        events = []
        for row in rows:
            try:
                event = self.to_canonical_event(row, tenant_id, source)
                events.append(event)
            except (ValueError, KeyError) as e:
                logger.warning(f"Skipping invalid row: {e}")
                continue
        return events
    
    @staticmethod
    def _safe_float(value: Any, default: float = 0.0) -> float:
        """Safely convert value to float, handling NaN/Inf."""
        if value is None:
            return default
        try:
            f = float(value)
            if math.isnan(f) or math.isinf(f):
                return default
            return f
        except (ValueError, TypeError):
            return default
    
    @staticmethod
    def _safe_int(value: Any, default: int = 0) -> int:
        """Safely convert value to int."""
        if value is None:
            return default
        try:
            f = float(value)
            if math.isnan(f) or math.isinf(f):
                return default
            return int(f)
        except (ValueError, TypeError, OverflowError):
            return default


class CICDDoS2019Adapter(FlowAdapter):
    """
    Adapter for CIC-DDoS2019 dataset format.
    
    Maps the 79 CIC-DDoS2019 features to NetworkFlowFeaturesV1 schema.
    Handles column name variations and missing values.
    """
    
    # Column name mappings (CIC format -> canonical)
    COLUMN_MAP = {
        # IP/Port
        'src_ip': 'src_ip',
        'dst_ip': 'dst_ip',
        'src_port': 'src_port',
        'dst_port': 'dst_port',
        'protocol': 'protocol',
        
        # Packet counts
        'tot_fwd_pkts': 'tot_fwd_pkts',
        'tot_bwd_pkts': 'tot_bwd_pkts',
        'totlen_fwd_pkts': 'totlen_fwd_pkts',
        'totlen_bwd_pkts': 'totlen_bwd_pkts',
        
        # Flow stats
        'flow_duration': 'flow_duration',
        'flow_byts_s': 'flow_byts_s',
        'flow_pkts_s': 'flow_pkts_s',
        
        # Forward packet lengths
        'fwd_pkt_len_max': 'fwd_pkt_len_max',
        'fwd_pkt_len_min': 'fwd_pkt_len_min',
        'fwd_pkt_len_mean': 'fwd_pkt_len_mean',
        'fwd_pkt_len_std': 'fwd_pkt_len_std',
        
        # Backward packet lengths
        'bwd_pkt_len_max': 'bwd_pkt_len_max',
        'bwd_pkt_len_min': 'bwd_pkt_len_min',
        'bwd_pkt_len_mean': 'bwd_pkt_len_mean',
        'bwd_pkt_len_std': 'bwd_pkt_len_std',
        
        # Overall packet lengths
        'pkt_len_min': 'pkt_len_min',
        'pkt_len_max': 'pkt_len_max',
        'pkt_len_mean': 'pkt_len_mean',
        'pkt_len_std': 'pkt_len_std',
        'pkt_len_var': 'pkt_len_var',
        
        # Inter-arrival times
        'flow_iat_mean': 'flow_iat_mean',
        'flow_iat_std': 'flow_iat_std',
        'flow_iat_max': 'flow_iat_max',
        'flow_iat_min': 'flow_iat_min',
        'fwd_iat_tot': 'fwd_iat_tot',
        'fwd_iat_mean': 'fwd_iat_mean',
        'fwd_iat_std': 'fwd_iat_std',
        'fwd_iat_max': 'fwd_iat_max',
        'fwd_iat_min': 'fwd_iat_min',
        'bwd_iat_tot': 'bwd_iat_tot',
        'bwd_iat_mean': 'bwd_iat_mean',
        'bwd_iat_std': 'bwd_iat_std',
        'bwd_iat_max': 'bwd_iat_max',
        'bwd_iat_min': 'bwd_iat_min',
        
        # TCP flags
        'fin_flag_cnt': 'fin_flag_cnt',
        'syn_flag_cnt': 'syn_flag_cnt',
        'rst_flag_cnt': 'rst_flag_cnt',
        'psh_flag_cnt': 'psh_flag_cnt',
        'ack_flag_cnt': 'ack_flag_cnt',
        'urg_flag_cnt': 'urg_flag_cnt',
        'cwe_flag_count': 'cwe_flag_count',
        'ece_flag_cnt': 'ece_flag_cnt',
        
        # PSH/URG flags per direction
        'fwd_psh_flags': 'fwd_psh_flags',
        'bwd_psh_flags': 'bwd_psh_flags',
        'fwd_urg_flags': 'fwd_urg_flags',
        'bwd_urg_flags': 'bwd_urg_flags',
        
        # Header lengths
        'fwd_header_len': 'fwd_header_len',
        'bwd_header_len': 'bwd_header_len',
        
        # Rates
        'fwd_pkts_s': 'fwd_pkts_s',
        'bwd_pkts_s': 'bwd_pkts_s',
        
        # Size averages
        'down_up_ratio': 'down_up_ratio',
        'pkt_size_avg': 'pkt_size_avg',
        'fwd_seg_size_avg': 'fwd_seg_size_avg',
        'bwd_seg_size_avg': 'bwd_seg_size_avg',
        
        # Bulk stats
        'fwd_byts_b_avg': 'fwd_byts_b_avg',
        'fwd_pkts_b_avg': 'fwd_pkts_b_avg',
        'fwd_blk_rate_avg': 'fwd_blk_rate_avg',
        'bwd_byts_b_avg': 'bwd_byts_b_avg',
        'bwd_pkts_b_avg': 'bwd_pkts_b_avg',
        'bwd_blk_rate_avg': 'bwd_blk_rate_avg',
        
        # Subflow stats
        'subflow_fwd_pkts': 'subflow_fwd_pkts',
        'subflow_fwd_byts': 'subflow_fwd_byts',
        'subflow_bwd_pkts': 'subflow_bwd_pkts',
        'subflow_bwd_byts': 'subflow_bwd_byts',
        
        # Window sizes
        'init_fwd_win_byts': 'init_fwd_win_byts',
        'init_bwd_win_byts': 'init_bwd_win_byts',
        
        # Forward data
        'fwd_act_data_pkts': 'fwd_act_data_pkts',
        'fwd_seg_size_min': 'fwd_seg_size_min',
        
        # Active/Idle times
        'active_mean': 'active_mean',
        'active_std': 'active_std',
        'active_max': 'active_max',
        'active_min': 'active_min',
        'idle_mean': 'idle_mean',
        'idle_std': 'idle_std',
        'idle_max': 'idle_max',
        'idle_min': 'idle_min',
    }
    
    @property
    def adapter_id(self) -> str:
        return "cic_ddos_2019"
    
    @property
    def source_format(self) -> str:
        return "CIC-DDoS2019"
    
    def to_network_flow_features(self, row: Dict[str, Any]) -> NetworkFlowFeaturesV1:
        """Convert CIC-DDoS2019 row to NetworkFlowFeaturesV1."""
        
        # Normalize column names (handle case variations)
        normalized = {k.lower().strip(): v for k, v in row.items()}
        
        # Required fields
        src_ip = str(normalized.get('src_ip', normalized.get('source ip', '0.0.0.0')))
        dst_ip = str(normalized.get('dst_ip', normalized.get('destination ip', '0.0.0.0')))
        src_port = self._safe_int(normalized.get('src_port', normalized.get('source port', 0)))
        dst_port = self._safe_int(normalized.get('dst_port', normalized.get('destination port', 0)))
        protocol = self._safe_int(normalized.get('protocol', 0))
        
        # Packet counts
        tot_fwd_pkts = self._safe_int(normalized.get('tot_fwd_pkts', normalized.get('total fwd packets', 0)))
        tot_bwd_pkts = self._safe_int(normalized.get('tot_bwd_pkts', normalized.get('total backward packets', 0)))
        totlen_fwd_pkts = self._safe_int(normalized.get('totlen_fwd_pkts', normalized.get('total length of fwd packets', 0)))
        totlen_bwd_pkts = self._safe_int(normalized.get('totlen_bwd_pkts', normalized.get('total length of bwd packets', 0)))
        
        # Flow stats
        flow_duration = self._safe_float(normalized.get('flow_duration', normalized.get('flow duration', 0)))
        flow_byts_s = self._safe_float(normalized.get('flow_byts_s', normalized.get('flow bytes/s', 0)))
        flow_pkts_s = self._safe_float(normalized.get('flow_pkts_s', normalized.get('flow packets/s', 0)))
        
        # Build features object
        features = NetworkFlowFeaturesV1(
            src_ip=src_ip,
            dst_ip=dst_ip,
            src_port=src_port,
            dst_port=dst_port,
            protocol=protocol,
            tot_fwd_pkts=tot_fwd_pkts,
            tot_bwd_pkts=tot_bwd_pkts,
            totlen_fwd_pkts=totlen_fwd_pkts,
            totlen_bwd_pkts=totlen_bwd_pkts,
            flow_duration=flow_duration,
            flow_byts_s=flow_byts_s,
            flow_pkts_s=flow_pkts_s,
            
            # Optional fields - extract all available
            fwd_pkts_s=self._safe_float(normalized.get('fwd_pkts_s', normalized.get('fwd packets/s', 0))),
            bwd_pkts_s=self._safe_float(normalized.get('bwd_pkts_s', normalized.get('bwd packets/s', 0))),
            fwd_pkt_len_max=self._safe_float(normalized.get('fwd_pkt_len_max', normalized.get('fwd packet length max', 0))),
            fwd_pkt_len_min=self._safe_float(normalized.get('fwd_pkt_len_min', normalized.get('fwd packet length min', 0))),
            fwd_pkt_len_mean=self._safe_float(normalized.get('fwd_pkt_len_mean', normalized.get('fwd packet length mean', 0))),
            fwd_pkt_len_std=self._safe_float(normalized.get('fwd_pkt_len_std', normalized.get('fwd packet length std', 0))),
            bwd_pkt_len_max=self._safe_float(normalized.get('bwd_pkt_len_max', normalized.get('bwd packet length max', 0))),
            bwd_pkt_len_min=self._safe_float(normalized.get('bwd_pkt_len_min', normalized.get('bwd packet length min', 0))),
            bwd_pkt_len_mean=self._safe_float(normalized.get('bwd_pkt_len_mean', normalized.get('bwd packet length mean', 0))),
            bwd_pkt_len_std=self._safe_float(normalized.get('bwd_pkt_len_std', normalized.get('bwd packet length std', 0))),
            
            # Packet length stats
            pkt_len_min=self._safe_float(normalized.get('pkt_len_min', normalized.get('min packet length', 0))),
            pkt_len_max=self._safe_float(normalized.get('pkt_len_max', normalized.get('max packet length', 0))),
            pkt_len_mean=self._safe_float(normalized.get('pkt_len_mean', normalized.get('packet length mean', 0))),
            pkt_len_std=self._safe_float(normalized.get('pkt_len_std', normalized.get('packet length std', 0))),
            pkt_len_var=self._safe_float(normalized.get('pkt_len_var', normalized.get('packet length variance', 0))),
            
            # IAT stats
            flow_iat_mean=self._safe_float(normalized.get('flow_iat_mean', normalized.get('flow iat mean', 0))),
            flow_iat_std=self._safe_float(normalized.get('flow_iat_std', normalized.get('flow iat std', 0))),
            flow_iat_max=self._safe_float(normalized.get('flow_iat_max', normalized.get('flow iat max', 0))),
            flow_iat_min=self._safe_float(normalized.get('flow_iat_min', normalized.get('flow iat min', 0))),
            fwd_iat_tot=self._safe_float(normalized.get('fwd_iat_tot', normalized.get('fwd iat total', 0))),
            fwd_iat_mean=self._safe_float(normalized.get('fwd_iat_mean', normalized.get('fwd iat mean', 0))),
            fwd_iat_std=self._safe_float(normalized.get('fwd_iat_std', normalized.get('fwd iat std', 0))),
            fwd_iat_max=self._safe_float(normalized.get('fwd_iat_max', normalized.get('fwd iat max', 0))),
            fwd_iat_min=self._safe_float(normalized.get('fwd_iat_min', normalized.get('fwd iat min', 0))),
            bwd_iat_tot=self._safe_float(normalized.get('bwd_iat_tot', normalized.get('bwd iat total', 0))),
            bwd_iat_mean=self._safe_float(normalized.get('bwd_iat_mean', normalized.get('bwd iat mean', 0))),
            bwd_iat_std=self._safe_float(normalized.get('bwd_iat_std', normalized.get('bwd iat std', 0))),
            bwd_iat_max=self._safe_float(normalized.get('bwd_iat_max', normalized.get('bwd iat max', 0))),
            bwd_iat_min=self._safe_float(normalized.get('bwd_iat_min', normalized.get('bwd iat min', 0))),
            
            # TCP flags
            fin_flag_cnt=self._safe_int(normalized.get('fin_flag_cnt', normalized.get('fin flag count', 0))),
            syn_flag_cnt=self._safe_int(normalized.get('syn_flag_cnt', normalized.get('syn flag count', 0))),
            rst_flag_cnt=self._safe_int(normalized.get('rst_flag_cnt', normalized.get('rst flag count', 0))),
            psh_flag_cnt=self._safe_int(normalized.get('psh_flag_cnt', normalized.get('psh flag count', 0))),
            ack_flag_cnt=self._safe_int(normalized.get('ack_flag_cnt', normalized.get('ack flag count', 0))),
            urg_flag_cnt=self._safe_int(normalized.get('urg_flag_cnt', normalized.get('urg flag count', 0))),
            cwe_flag_count=self._safe_int(normalized.get('cwe_flag_count', normalized.get('cwe flag count', 0))),
            ece_flag_cnt=self._safe_int(normalized.get('ece_flag_cnt', normalized.get('ece flag count', 0))),
            
            # Direction-specific flags
            fwd_psh_flags=self._safe_int(normalized.get('fwd_psh_flags', normalized.get('fwd psh flags', 0))),
            bwd_psh_flags=self._safe_int(normalized.get('bwd_psh_flags', normalized.get('bwd psh flags', 0))),
            fwd_urg_flags=self._safe_int(normalized.get('fwd_urg_flags', normalized.get('fwd urg flags', 0))),
            bwd_urg_flags=self._safe_int(normalized.get('bwd_urg_flags', normalized.get('bwd urg flags', 0))),
            
            # Header lengths
            fwd_header_len=self._safe_int(normalized.get('fwd_header_len', normalized.get('fwd header length', 0))),
            bwd_header_len=self._safe_int(normalized.get('bwd_header_len', normalized.get('bwd header length', 0))),
            
            # Ratios and averages
            down_up_ratio=self._safe_float(normalized.get('down_up_ratio', normalized.get('down/up ratio', 0))),
            pkt_size_avg=self._safe_float(normalized.get('pkt_size_avg', normalized.get('average packet size', 0))),
            fwd_seg_size_avg=self._safe_float(normalized.get('fwd_seg_size_avg', normalized.get('avg fwd segment size', 0))),
            bwd_seg_size_avg=self._safe_float(normalized.get('bwd_seg_size_avg', normalized.get('avg bwd segment size', 0))),
            
            # Bulk stats
            fwd_byts_b_avg=self._safe_float(normalized.get('fwd_byts_b_avg', normalized.get('fwd avg bytes/bulk', 0))),
            fwd_pkts_b_avg=self._safe_float(normalized.get('fwd_pkts_b_avg', normalized.get('fwd avg packets/bulk', 0))),
            fwd_blk_rate_avg=self._safe_float(normalized.get('fwd_blk_rate_avg', normalized.get('fwd avg bulk rate', 0))),
            bwd_byts_b_avg=self._safe_float(normalized.get('bwd_byts_b_avg', normalized.get('bwd avg bytes/bulk', 0))),
            bwd_pkts_b_avg=self._safe_float(normalized.get('bwd_pkts_b_avg', normalized.get('bwd avg packets/bulk', 0))),
            bwd_blk_rate_avg=self._safe_float(normalized.get('bwd_blk_rate_avg', normalized.get('bwd avg bulk rate', 0))),
            
            # Subflow stats
            subflow_fwd_pkts=self._safe_int(normalized.get('subflow_fwd_pkts', normalized.get('subflow fwd packets', 0))),
            subflow_fwd_byts=self._safe_int(normalized.get('subflow_fwd_byts', normalized.get('subflow fwd bytes', 0))),
            subflow_bwd_pkts=self._safe_int(normalized.get('subflow_bwd_pkts', normalized.get('subflow bwd packets', 0))),
            subflow_bwd_byts=self._safe_int(normalized.get('subflow_bwd_byts', normalized.get('subflow bwd bytes', 0))),
            
            # Window sizes
            init_fwd_win_byts=self._safe_int(normalized.get('init_fwd_win_byts', normalized.get('init_win_bytes_forward', 0))),
            init_bwd_win_byts=self._safe_int(normalized.get('init_bwd_win_byts', normalized.get('init_win_bytes_backward', 0))),
            
            # Forward data
            fwd_act_data_pkts=self._safe_int(normalized.get('fwd_act_data_pkts', normalized.get('act_data_pkt_fwd', 0))),
            fwd_seg_size_min=self._safe_int(normalized.get('fwd_seg_size_min', normalized.get('min_seg_size_forward', 0))),
            
            # Active/Idle times
            active_mean=self._safe_float(normalized.get('active_mean', normalized.get('active mean', 0))),
            active_std=self._safe_float(normalized.get('active_std', normalized.get('active std', 0))),
            active_max=self._safe_float(normalized.get('active_max', normalized.get('active max', 0))),
            active_min=self._safe_float(normalized.get('active_min', normalized.get('active min', 0))),
            idle_mean=self._safe_float(normalized.get('idle_mean', normalized.get('idle mean', 0))),
            idle_std=self._safe_float(normalized.get('idle_std', normalized.get('idle std', 0))),
            idle_max=self._safe_float(normalized.get('idle_max', normalized.get('idle max', 0))),
            idle_min=self._safe_float(normalized.get('idle_min', normalized.get('idle min', 0))),
            
            # Computed semantic fields
            pps=flow_pkts_s if flow_pkts_s > 0 else None,
            bps=flow_byts_s * 8 if flow_byts_s > 0 else None,
            syn_ack_ratio=self._compute_syn_ack_ratio(normalized),
        )
        
        return features
    
    def _compute_syn_ack_ratio(self, row: Dict[str, Any]) -> Optional[float]:
        """Compute SYN/ACK ratio from flags."""
        syn = self._safe_int(row.get('syn_flag_cnt', row.get('syn flag count', 0)))
        ack = self._safe_int(row.get('ack_flag_cnt', row.get('ack flag count', 0)))
        
        if ack == 0:
            return None
        return syn / ack


class EnterpriseFlowAdapter(FlowAdapter):
    """
    Adapter for enterprise/corporate flow data.
    
    This is a placeholder/template for real enterprise flow formats.
    Customize the field mappings for your specific flow data schema.
    """
    
    @property
    def adapter_id(self) -> str:
        return "enterprise_flow"
    
    @property
    def source_format(self) -> str:
        return "Enterprise NetFlow"
    
    def to_network_flow_features(self, row: Dict[str, Any]) -> NetworkFlowFeaturesV1:
        """
        Convert enterprise flow record to NetworkFlowFeaturesV1.
        
        Customize this method for your specific enterprise flow schema.
        """
        # Normalize column names
        normalized = {k.lower().strip(): v for k, v in row.items()}
        
        # Map enterprise fields to canonical (customize these mappings)
        src_ip = str(normalized.get('source_address', normalized.get('src_addr', '0.0.0.0')))
        dst_ip = str(normalized.get('destination_address', normalized.get('dst_addr', '0.0.0.0')))
        src_port = self._safe_int(normalized.get('source_port', normalized.get('l4_src_port', 0)))
        dst_port = self._safe_int(normalized.get('destination_port', normalized.get('l4_dst_port', 0)))
        protocol = self._safe_int(normalized.get('protocol', normalized.get('ip_protocol', 0)))
        
        # Volume metrics
        bytes_in = self._safe_int(normalized.get('bytes_in', normalized.get('in_bytes', 0)))
        bytes_out = self._safe_int(normalized.get('bytes_out', normalized.get('out_bytes', 0)))
        packets_in = self._safe_int(normalized.get('packets_in', normalized.get('in_pkts', 0)))
        packets_out = self._safe_int(normalized.get('packets_out', normalized.get('out_pkts', 0)))
        
        # Duration
        duration_ms = self._safe_float(normalized.get('duration_ms', normalized.get('flow_duration_ms', 0)))
        duration_us = duration_ms * 1000  # Convert to microseconds for CIC compatibility
        
        # Compute derived metrics
        total_bytes = bytes_in + bytes_out
        total_packets = packets_in + packets_out
        flow_byts_s = total_bytes / (duration_ms / 1000) if duration_ms > 0 else 0
        flow_pkts_s = total_packets / (duration_ms / 1000) if duration_ms > 0 else 0
        
        return NetworkFlowFeaturesV1(
            src_ip=src_ip,
            dst_ip=dst_ip,
            src_port=src_port,
            dst_port=dst_port,
            protocol=protocol,
            tot_fwd_pkts=packets_out,
            tot_bwd_pkts=packets_in,
            totlen_fwd_pkts=bytes_out,
            totlen_bwd_pkts=bytes_in,
            flow_duration=duration_us,
            flow_byts_s=flow_byts_s,
            flow_pkts_s=flow_pkts_s,
            
            # Computed semantic fields
            pps=flow_pkts_s if flow_pkts_s > 0 else None,
            bps=flow_byts_s * 8 if flow_byts_s > 0 else None,
        )


class UNSWNB15Adapter(FlowAdapter):
    """
    Adapter for UNSW-NB15 dataset format.
    
    Maps UNSW-NB15 features to NetworkFlowFeaturesV1 schema.
    Dataset: https://research.unsw.edu.au/projects/unsw-nb15-dataset
    """
    
    @property
    def adapter_id(self) -> str:
        return "unsw_nb15"
    
    @property
    def source_format(self) -> str:
        return "UNSW-NB15"
    
    def to_network_flow_features(self, row: Dict[str, Any]) -> NetworkFlowFeaturesV1:
        """Convert UNSW-NB15 row to NetworkFlowFeaturesV1."""
        
        # Normalize column names (handle case/space variations)
        normalized = {k.lower().strip().replace(' ', '_'): v for k, v in row.items()}
        
        # Required fields - UNSW-NB15 uses srcip, dstip, sport, dsport
        src_ip = str(normalized.get('srcip', normalized.get('src_ip', '0.0.0.0')))
        dst_ip = str(normalized.get('dstip', normalized.get('dst_ip', '0.0.0.0')))
        src_port = self._safe_int(normalized.get('sport', normalized.get('src_port', 0)))
        dst_port = self._safe_int(normalized.get('dsport', normalized.get('dst_port', 0)))
        
        # Protocol - UNSW uses text (tcp, udp) or number
        proto = normalized.get('proto', normalized.get('protocol', 0))
        if isinstance(proto, str):
            proto = {'tcp': 6, 'udp': 17, 'icmp': 1}.get(proto.lower(), 0)
        protocol = self._safe_int(proto)
        
        # Packet counts - UNSW uses spkts/dpkts (source/dest packets)
        tot_fwd_pkts = self._safe_int(normalized.get('spkts', normalized.get('src_pkts', 0)))
        tot_bwd_pkts = self._safe_int(normalized.get('dpkts', normalized.get('dst_pkts', 0)))
        
        # Byte counts - UNSW uses sbytes/dbytes
        totlen_fwd_pkts = self._safe_int(normalized.get('sbytes', normalized.get('src_bytes', 0)))
        totlen_bwd_pkts = self._safe_int(normalized.get('dbytes', normalized.get('dst_bytes', 0)))
        
        # Duration - UNSW uses 'dur' in seconds
        duration_s = self._safe_float(normalized.get('dur', normalized.get('duration', 0)))
        flow_duration = duration_s * 1_000_000  # Convert to microseconds
        
        # Compute rates
        total_bytes = totlen_fwd_pkts + totlen_bwd_pkts
        total_packets = tot_fwd_pkts + tot_bwd_pkts
        flow_byts_s = total_bytes / duration_s if duration_s > 0 else 0
        flow_pkts_s = total_packets / duration_s if duration_s > 0 else 0
        
        # UNSW-specific fields
        sttl = self._safe_int(normalized.get('sttl', 0))  # Source TTL
        dttl = self._safe_int(normalized.get('dttl', 0))  # Dest TTL
        sload = self._safe_float(normalized.get('sload', 0))  # Source bits per second
        dload = self._safe_float(normalized.get('dload', 0))  # Dest bits per second
        sloss = self._safe_int(normalized.get('sloss', 0))  # Source packets retransmitted
        dloss = self._safe_int(normalized.get('dloss', 0))  # Dest packets retransmitted
        
        # Inter-packet times - UNSW uses sinpkt/dinpkt (mean time between packets)
        sinpkt = self._safe_float(normalized.get('sinpkt', 0))
        dinpkt = self._safe_float(normalized.get('dinpkt', 0))
        
        # TCP fields
        tcprtt = self._safe_float(normalized.get('tcprtt', 0))
        synack = self._safe_float(normalized.get('synack', 0))
        ackdat = self._safe_float(normalized.get('ackdat', 0))
        
        # Window sizes
        swin = self._safe_int(normalized.get('swin', 0))
        dwin = self._safe_int(normalized.get('dwin', 0))
        
        # Mean packet sizes
        smean = self._safe_float(normalized.get('smean', 0))
        dmean = self._safe_float(normalized.get('dmean', 0))
        
        return NetworkFlowFeaturesV1(
            src_ip=src_ip,
            dst_ip=dst_ip,
            src_port=src_port,
            dst_port=dst_port,
            protocol=protocol,
            tot_fwd_pkts=tot_fwd_pkts,
            tot_bwd_pkts=tot_bwd_pkts,
            totlen_fwd_pkts=totlen_fwd_pkts,
            totlen_bwd_pkts=totlen_bwd_pkts,
            flow_duration=max(0, flow_duration),  # Clamp negative durations
            flow_byts_s=flow_byts_s,
            flow_pkts_s=flow_pkts_s,
            
            # Map UNSW fields to canonical where possible
            fwd_pkt_len_mean=smean,
            bwd_pkt_len_mean=dmean,
            fwd_iat_mean=sinpkt * 1000 if sinpkt > 0 else 0,  # Convert to ms
            bwd_iat_mean=dinpkt * 1000 if dinpkt > 0 else 0,
            init_fwd_win_byts=swin,
            init_bwd_win_byts=dwin,
            
            # Semantic fields
            pps=flow_pkts_s if flow_pkts_s > 0 else None,
            bps=(sload + dload) if (sload + dload) > 0 else flow_byts_s * 8,
        )


class NSLKDDAdapter(FlowAdapter):
    """
    Adapter for NSL-KDD dataset format.
    
    Maps NSL-KDD features to NetworkFlowFeaturesV1 schema.
    Dataset: https://www.unb.ca/cic/datasets/nsl.html
    """
    
    @property
    def adapter_id(self) -> str:
        return "nsl_kdd"
    
    @property
    def source_format(self) -> str:
        return "NSL-KDD"
    
    # NSL-KDD protocol/service/flag mappings
    PROTOCOL_MAP = {
        'tcp': 6,
        'udp': 17,
        'icmp': 1,
    }
    
    SERVICE_TO_PORT = {
        'http': 80, 'https': 443, 'ftp': 21, 'ftp_data': 20,
        'ssh': 22, 'telnet': 23, 'smtp': 25, 'domain': 53,
        'domain_u': 53, 'pop_3': 110, 'imap4': 143,
        'private': 0, 'other': 0, 'eco_i': 0, 'ecr_i': 0,
    }
    
    def to_network_flow_features(self, row: Dict[str, Any]) -> NetworkFlowFeaturesV1:
        """Convert NSL-KDD row to NetworkFlowFeaturesV1."""
        
        # Normalize column names
        normalized = {k.lower().strip(): v for k, v in row.items()}
        
        # NSL-KDD has protocol_type as string
        proto_str = str(normalized.get('protocol_type', normalized.get('protocol', 'tcp'))).lower()
        protocol = self.PROTOCOL_MAP.get(proto_str, 0)
        
        # Service -> approximate port
        service = str(normalized.get('service', '')).lower()
        dst_port = self.SERVICE_TO_PORT.get(service, 0)
        
        # NSL-KDD doesn't have IP addresses, use placeholders
        src_ip = '0.0.0.0'
        dst_ip = '0.0.0.0'
        src_port = 0
        
        # Duration in seconds
        duration_s = self._safe_float(normalized.get('duration', 0))
        flow_duration = duration_s * 1_000_000  # Convert to microseconds
        
        # Byte counts
        src_bytes = self._safe_int(normalized.get('src_bytes', 0))
        dst_bytes = self._safe_int(normalized.get('dst_bytes', 0))
        
        # Compute rates
        total_bytes = src_bytes + dst_bytes
        flow_byts_s = total_bytes / duration_s if duration_s > 0 else 0
        
        # NSL-KDD specific features
        wrong_fragment = self._safe_int(normalized.get('wrong_fragment', 0))
        urgent = self._safe_int(normalized.get('urgent', 0))
        hot = self._safe_int(normalized.get('hot', 0))
        num_failed_logins = self._safe_int(normalized.get('num_failed_logins', 0))
        logged_in = self._safe_int(normalized.get('logged_in', 0))
        num_compromised = self._safe_int(normalized.get('num_compromised', 0))
        
        # Connection-based features
        count = self._safe_int(normalized.get('count', 0))  # connections to same host
        srv_count = self._safe_int(normalized.get('srv_count', 0))  # connections to same service
        serror_rate = self._safe_float(normalized.get('serror_rate', 0))
        srv_serror_rate = self._safe_float(normalized.get('srv_serror_rate', 0))
        rerror_rate = self._safe_float(normalized.get('rerror_rate', 0))
        srv_rerror_rate = self._safe_float(normalized.get('srv_rerror_rate', 0))
        same_srv_rate = self._safe_float(normalized.get('same_srv_rate', 0))
        diff_srv_rate = self._safe_float(normalized.get('diff_srv_rate', 0))
        
        # TCP flag from 'flag' field (SF, S0, REJ, etc.)
        flag = str(normalized.get('flag', '')).upper()
        syn_flag_cnt = 1 if flag.startswith('S') else 0
        rst_flag_cnt = 1 if 'R' in flag else 0
        fin_flag_cnt = 1 if flag == 'SF' else 0
        
        return NetworkFlowFeaturesV1(
            src_ip=src_ip,
            dst_ip=dst_ip,
            src_port=src_port,
            dst_port=dst_port,
            protocol=protocol,
            tot_fwd_pkts=1,  # NSL-KDD is connection-based, not packet-based
            tot_bwd_pkts=1 if dst_bytes > 0 else 0,
            totlen_fwd_pkts=src_bytes,
            totlen_bwd_pkts=dst_bytes,
            flow_duration=max(0, flow_duration),
            flow_byts_s=flow_byts_s,
            flow_pkts_s=1 / duration_s if duration_s > 0 else 0,
            
            # TCP flags derived from flag field
            syn_flag_cnt=syn_flag_cnt,
            rst_flag_cnt=rst_flag_cnt,
            fin_flag_cnt=fin_flag_cnt,
            urg_flag_cnt=urgent,
            
            # Semantic fields
            pps=1 / duration_s if duration_s > 0 else None,
            bps=flow_byts_s * 8 if flow_byts_s > 0 else None,
        )


class CICIDS2017Adapter(FlowAdapter):
    """
    Adapter for CICIDS2017 dataset format.
    
    Maps CICIDS2017 features to NetworkFlowFeaturesV1 schema.
    Dataset: https://www.unb.ca/cic/datasets/ids-2017.html
    
    Note: CICIDS2017 has similar schema to CIC-DDoS2019 but with some differences.
    """
    
    @property
    def adapter_id(self) -> str:
        return "cicids_2017"
    
    @property
    def source_format(self) -> str:
        return "CICIDS2017"
    
    def to_network_flow_features(self, row: Dict[str, Any]) -> NetworkFlowFeaturesV1:
        """Convert CICIDS2017 row to NetworkFlowFeaturesV1."""
        
        # Normalize column names - CICIDS2017 uses spaces and title case
        normalized = {}
        for k, v in row.items():
            key = k.lower().strip().replace(' ', '_')
            normalized[key] = v
        
        # CICIDS2017 uses "source_ip", "destination_ip" with spaces
        src_ip = str(normalized.get('source_ip', normalized.get('src_ip', '0.0.0.0')))
        dst_ip = str(normalized.get('destination_ip', normalized.get('dst_ip', '0.0.0.0')))
        src_port = self._safe_int(normalized.get('source_port', normalized.get('src_port', 0)))
        dst_port = self._safe_int(normalized.get('destination_port', normalized.get('dst_port', 0)))
        protocol = self._safe_int(normalized.get('protocol', 0))
        
        # Packet counts - CICIDS2017 naming
        tot_fwd_pkts = self._safe_int(
            normalized.get('total_fwd_packets', 
            normalized.get('tot_fwd_pkts', 0))
        )
        tot_bwd_pkts = self._safe_int(
            normalized.get('total_backward_packets',
            normalized.get('total_bwd_packets',
            normalized.get('tot_bwd_pkts', 0)))
        )
        
        # Byte counts
        totlen_fwd_pkts = self._safe_int(
            normalized.get('total_length_of_fwd_packets',
            normalized.get('totlen_fwd_pkts', 0))
        )
        totlen_bwd_pkts = self._safe_int(
            normalized.get('total_length_of_bwd_packets',
            normalized.get('totlen_bwd_pkts', 0))
        )
        
        # Duration (microseconds in CICIDS2017)
        flow_duration = self._safe_float(normalized.get('flow_duration', 0))
        
        # Rates
        flow_byts_s = self._safe_float(
            normalized.get('flow_bytes/s',
            normalized.get('flow_byts_s', 0))
        )
        flow_pkts_s = self._safe_float(
            normalized.get('flow_packets/s',
            normalized.get('flow_pkts_s', 0))
        )
        
        # Forward packet lengths
        fwd_pkt_len_max = self._safe_float(
            normalized.get('fwd_packet_length_max',
            normalized.get('fwd_pkt_len_max', 0))
        )
        fwd_pkt_len_min = self._safe_float(
            normalized.get('fwd_packet_length_min',
            normalized.get('fwd_pkt_len_min', 0))
        )
        fwd_pkt_len_mean = self._safe_float(
            normalized.get('fwd_packet_length_mean',
            normalized.get('fwd_pkt_len_mean', 0))
        )
        fwd_pkt_len_std = self._safe_float(
            normalized.get('fwd_packet_length_std',
            normalized.get('fwd_pkt_len_std', 0))
        )
        
        # Backward packet lengths
        bwd_pkt_len_max = self._safe_float(
            normalized.get('bwd_packet_length_max',
            normalized.get('bwd_pkt_len_max', 0))
        )
        bwd_pkt_len_min = self._safe_float(
            normalized.get('bwd_packet_length_min',
            normalized.get('bwd_pkt_len_min', 0))
        )
        bwd_pkt_len_mean = self._safe_float(
            normalized.get('bwd_packet_length_mean',
            normalized.get('bwd_pkt_len_mean', 0))
        )
        bwd_pkt_len_std = self._safe_float(
            normalized.get('bwd_packet_length_std',
            normalized.get('bwd_pkt_len_std', 0))
        )
        
        # IAT (Inter-Arrival Time) features
        flow_iat_mean = self._safe_float(
            normalized.get('flow_iat_mean', 0)
        )
        flow_iat_std = self._safe_float(
            normalized.get('flow_iat_std', 0)
        )
        flow_iat_max = self._safe_float(
            normalized.get('flow_iat_max', 0)
        )
        flow_iat_min = self._safe_float(
            normalized.get('flow_iat_min', 0)
        )
        
        # TCP flags - CICIDS2017 uses different naming
        fin_flag_cnt = self._safe_int(
            normalized.get('fin_flag_count',
            normalized.get('fin_flag_cnt', 0))
        )
        syn_flag_cnt = self._safe_int(
            normalized.get('syn_flag_count',
            normalized.get('syn_flag_cnt', 0))
        )
        rst_flag_cnt = self._safe_int(
            normalized.get('rst_flag_count',
            normalized.get('rst_flag_cnt', 0))
        )
        psh_flag_cnt = self._safe_int(
            normalized.get('psh_flag_count',
            normalized.get('psh_flag_cnt', 0))
        )
        ack_flag_cnt = self._safe_int(
            normalized.get('ack_flag_count',
            normalized.get('ack_flag_cnt', 0))
        )
        urg_flag_cnt = self._safe_int(
            normalized.get('urg_flag_count',
            normalized.get('urg_flag_cnt', 0))
        )
        
        # Average packet size
        pkt_size_avg = self._safe_float(
            normalized.get('average_packet_size',
            normalized.get('pkt_size_avg', 0))
        )
        
        # Window sizes
        init_fwd_win_byts = self._safe_int(
            normalized.get('init_win_bytes_forward',
            normalized.get('init_fwd_win_byts', 0))
        )
        init_bwd_win_byts = self._safe_int(
            normalized.get('init_win_bytes_backward',
            normalized.get('init_bwd_win_byts', 0))
        )
        
        return NetworkFlowFeaturesV1(
            src_ip=src_ip,
            dst_ip=dst_ip,
            src_port=src_port,
            dst_port=dst_port,
            protocol=protocol,
            tot_fwd_pkts=tot_fwd_pkts,
            tot_bwd_pkts=tot_bwd_pkts,
            totlen_fwd_pkts=totlen_fwd_pkts,
            totlen_bwd_pkts=totlen_bwd_pkts,
            flow_duration=max(0, flow_duration),
            flow_byts_s=flow_byts_s,
            flow_pkts_s=flow_pkts_s,
            
            # Forward packet stats
            fwd_pkt_len_max=fwd_pkt_len_max,
            fwd_pkt_len_min=fwd_pkt_len_min,
            fwd_pkt_len_mean=fwd_pkt_len_mean,
            fwd_pkt_len_std=fwd_pkt_len_std,
            
            # Backward packet stats
            bwd_pkt_len_max=bwd_pkt_len_max,
            bwd_pkt_len_min=bwd_pkt_len_min,
            bwd_pkt_len_mean=bwd_pkt_len_mean,
            bwd_pkt_len_std=bwd_pkt_len_std,
            
            # IAT stats
            flow_iat_mean=flow_iat_mean,
            flow_iat_std=flow_iat_std,
            flow_iat_max=flow_iat_max,
            flow_iat_min=flow_iat_min,
            
            # TCP flags
            fin_flag_cnt=fin_flag_cnt,
            syn_flag_cnt=syn_flag_cnt,
            rst_flag_cnt=rst_flag_cnt,
            psh_flag_cnt=psh_flag_cnt,
            ack_flag_cnt=ack_flag_cnt,
            urg_flag_cnt=urg_flag_cnt,
            
            # Averages
            pkt_size_avg=pkt_size_avg,
            
            # Window sizes
            init_fwd_win_byts=init_fwd_win_byts,
            init_bwd_win_byts=init_bwd_win_byts,
            
            # Semantic fields
            pps=flow_pkts_s if flow_pkts_s > 0 else None,
            bps=flow_byts_s * 8 if flow_byts_s > 0 else None,
            syn_ack_ratio=syn_flag_cnt / ack_flag_cnt if ack_flag_cnt > 0 else None,
        )


# Adapter registry for dynamic lookup
ADAPTER_REGISTRY: Dict[str, type] = {
    "CICDDoS2019Adapter": CICDDoS2019Adapter,
    "EnterpriseFlowAdapter": EnterpriseFlowAdapter,
    "UNSWNB15Adapter": UNSWNB15Adapter,
    "NSLKDDAdapter": NSLKDDAdapter,
    "CICIDS2017Adapter": CICIDS2017Adapter,
}


def get_adapter(adapter_class: str) -> FlowAdapter:
    """
    Get adapter instance by class name.
    
    Args:
        adapter_class: Name of adapter class
        
    Returns:
        FlowAdapter instance
        
    Raises:
        ValueError: If adapter class not found
    """
    if adapter_class not in ADAPTER_REGISTRY:
        raise ValueError(f"Unknown adapter class: {adapter_class}")
    
    return ADAPTER_REGISTRY[adapter_class]()


def register_adapter(name: str, adapter_class: type) -> None:
    """Register a custom adapter class."""
    ADAPTER_REGISTRY[name] = adapter_class
