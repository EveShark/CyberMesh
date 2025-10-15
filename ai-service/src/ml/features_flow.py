"""
FlowFeatureExtractor: 79-feature extractor for network flow models.

Single source of truth for raw flow features used by ML.
No dataset/version naming in symbols.
"""

import numpy as np
from typing import List, Dict
from ..logging import get_logger


class FlowFeatureExtractor:
    """
    Extract 79 raw features from flow records for network-flow ML models.
    """

    FEATURE_COUNT = 79

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
        self.logger.info(f"Initialized FlowFeatureExtractor: {self.FEATURE_COUNT} features")

    def extract_raw(self, flows: List[Dict]) -> np.ndarray:
        if not flows:
            raise ValueError("No flows provided for feature extraction")

        n_flows = len(flows)
        X = np.zeros((n_flows, self.FEATURE_COUNT), dtype=np.float32)

        # Protocol mapping for test data compatibility
        protocol_map = {'TCP': 6, 'UDP': 17, 'ICMP': 1}

        for i, flow in enumerate(flows):
            for j, col_name in enumerate(self.FEATURE_COLUMNS):
                value = flow.get(col_name, 0.0)
                
                # Handle protocol string -> numeric conversion
                if col_name == 'protocol' and isinstance(value, str):
                    value = protocol_map.get(value, 0)
                
                if value is None:
                    value = 0.0
                if isinstance(value, float) and np.isnan(value):
                    value = 0.0
                if isinstance(value, float) and np.isinf(value):
                    value = 1e9 if value > 0 else -1e9
                X[i, j] = float(value)

        self.logger.debug(f"Extracted {n_flows} x {self.FEATURE_COUNT} raw features")
        return X

    def normalize(self, X: np.ndarray) -> np.ndarray:
        # No normalization; model trained on raw features
        return X

    def extract(self, flows: List[Dict]) -> np.ndarray:
        return self.extract_raw(flows)

    def get_feature_names(self):
        return self.FEATURE_COLUMNS.copy()
