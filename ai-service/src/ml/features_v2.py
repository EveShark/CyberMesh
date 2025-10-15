"""
79-Feature Extractor for CIC-DDoS2019 Dataset (Model v2.0.0)

Extracts features matching the trained LGBMClassifier v2.0.0 schema.
NO feature engineering - raw CIC-DDoS2019 columns in exact training order.
"""

import numpy as np
from typing import List, Dict
from ..logging import get_logger


class CICDDoS2019Extractor:
    """
    Extract 79 raw features from CIC-DDoS2019 network flows.
    
    Compatible with LGBMClassifier v2.0.0 trained on 7.65M samples.
    Feature order matches training data exactly.
    """
    
    FEATURE_COUNT = 79
    
    # Feature names in exact training order
    FEATURE_COLUMNS = [
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
    
    def __init__(self):
        self.logger = get_logger(__name__)
        assert len(self.FEATURE_COLUMNS) == self.FEATURE_COUNT
        self.logger.info(f"Initialized CICDDoS2019Extractor: {self.FEATURE_COUNT} features")
    
    def extract_raw(self, flows: List[Dict], window_sec: int = 5) -> np.ndarray:
        """
        Extract 79 raw features from flow records.
        
        Args:
            flows: List of flow dictionaries with CIC-DDoS2019 columns
            window_sec: Ignored (CIC-DDoS2019 data is already pre-aggregated)
        
        Returns:
            Feature matrix (n_flows, 79)
        """
        if not flows:
            raise ValueError("No flows provided for feature extraction")
        
        n_flows = len(flows)
        X = np.zeros((n_flows, self.FEATURE_COUNT), dtype=np.float32)
        
        for i, flow in enumerate(flows):
            for j, col_name in enumerate(self.FEATURE_COLUMNS):
                # Get value, default to 0.0 if missing
                value = flow.get(col_name, 0.0)
                
                # Handle None/NaN
                if value is None or (isinstance(value, float) and np.isnan(value)):
                    value = 0.0
                
                # Handle inf (from division by zero in preprocessing)
                if isinstance(value, float) and np.isinf(value):
                    # Cap at large but finite value
                    value = 1e9 if value > 0 else -1e9
                
                X[i, j] = float(value)
        
        self.logger.debug(f"Extracted {n_flows} x {self.FEATURE_COUNT} raw features")
        return X
    
    def normalize(self, X: np.ndarray) -> np.ndarray:
        """
        No normalization - model trained on raw features.
        
        LightGBM handles raw feature scales internally.
        """
        return X
    
    def extract(self, flows: List[Dict]) -> np.ndarray:
        """Extract raw features (no normalization needed for LightGBM)."""
        return self.extract_raw(flows)
    
    def get_feature_names(self) -> List[str]:
        """Return feature names in order."""
        return self.FEATURE_COLUMNS.copy()
