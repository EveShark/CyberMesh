"""Feature extraction utilities for agent wrappers."""

from typing import Dict, List, Any
import numpy as np


# Exact 79 CIC-DDoS2019 feature columns in training order
CIC_FEATURE_COLUMNS = [
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


def extract_cic_features(features: Dict[str, Any]) -> np.ndarray:
    """
    Extract exactly 79 CIC-DDoS2019 features in training order.
    
    Args:
        features: Canonical features dict from NetworkFlowFeaturesV1
        
    Returns:
        numpy array of shape (79,) with features in exact CIC order
    """
    vector = []
    for col in CIC_FEATURE_COLUMNS:
        value = features.get(col, 0.0)
        if value is None:
            value = 0.0
        try:
            f = float(value)
            if not np.isfinite(f):
                f = 0.0
        except (ValueError, TypeError):
            f = 0.0
        vector.append(f)
    return np.array(vector, dtype=np.float32)


def extract_semantics(features: Dict[str, Any]) -> Dict[str, float]:
    """
    Extract semantic features for Rules/Math engines.
    
    Args:
        features: Canonical features dict from NetworkFlowFeaturesV1
        
    Returns:
        Dict with semantic keys: pps, syn_ack_ratio, unique_dst_ports, port_entropy
    """
    # PPS from flow_pkts_s or computed pps field
    pps = features.get('pps') or features.get('flow_pkts_s', 0.0)
    if pps is None or not np.isfinite(float(pps)):
        pps = 0.0
    
    # SYN/ACK ratio
    syn_ack_ratio = features.get('syn_ack_ratio')
    if syn_ack_ratio is None:
        syn = float(features.get('syn_flag_cnt', 0))
        ack = float(features.get('ack_flag_cnt', 0))
        syn_ack_ratio = syn / max(ack, 1.0) if np.isfinite(syn) and np.isfinite(ack) else 0.0
    
    # These require batch context - set defaults for single-event processing
    unique_dst_ports = float(features.get('unique_dst_ports_batch', 1.0))
    port_entropy = float(features.get('port_entropy_batch', 0.0))
    
    return {
        'pps': float(max(pps, 0.0)),
        'syn_ack_ratio': float(max(syn_ack_ratio, 0.0)),
        'unique_dst_ports': unique_dst_ports,
        'port_entropy': port_entropy,
    }


def extract_semantics_batch(
    features_list: List[Dict[str, Any]]
) -> List[Dict[str, float]]:
    """
    Extract semantic features for a batch of flows.
    
    Computes batch-level statistics (unique_dst_ports, port_entropy).
    
    Args:
        features_list: List of canonical features dicts
        
    Returns:
        List of semantic dicts, one per flow
    """
    if not features_list:
        return []
    
    # Compute batch-level port statistics
    dst_ports = [int(f.get('dst_port', 0)) for f in features_list]
    unique_ports = len(set(dst_ports))
    
    # Shannon entropy of port distribution
    port_counts: Dict[int, int] = {}
    for p in dst_ports:
        port_counts[p] = port_counts.get(p, 0) + 1
    
    total = float(len(dst_ports))
    probs = np.array([c / total for c in port_counts.values()], dtype=float)
    with np.errstate(divide='ignore', invalid='ignore'):
        entropy = -np.nansum(probs * np.log2(probs)) if probs.size else 0.0
    if not np.isfinite(entropy):
        entropy = 0.0
    
    # Build per-flow semantics with batch context
    result = []
    for features in features_list:
        sem = extract_semantics(features)
        sem['unique_dst_ports'] = float(unique_ports)
        sem['port_entropy'] = float(entropy)
        result.append(sem)
    
    return result
