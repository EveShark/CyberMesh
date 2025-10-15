"""
FeatureAdapter: derive semantic features from CIC-DDoS2019 79-feature vectors.

Semantics exposed to Rules/Math engines (examples):
- pps: packets per second (from flow_pkts_s)
- syn_ack_ratio: syn_flag_cnt / max(ack_flag_cnt, 1)
- unique_dst_ports: count of unique dst_port in current batch
- port_entropy: Shannon entropy (bits, base-2) of dst_port distribution in batch

All outputs are finite and clamped safely where applicable.
"""

from typing import List, Dict
import numpy as np

from ..logging import get_logger
from .features_flow import FlowFeatureExtractor


class FeatureAdapter:
    """
    Adapter to compute a semantic FeatureView from raw 79 feature vectors.
    """

    def __init__(self):
        self.logger = get_logger(__name__)
        self._col_idx = {name: i for i, name in enumerate(FlowFeatureExtractor.FEATURE_COLUMNS)}

    def derive_semantics(self, flows: List[Dict], X79: np.ndarray) -> List[Dict[str, float]]:
        """
        Build per-flow semantic dicts using both per-row values and batch context.

        Args:
            flows: list of raw flow dicts (must include dst_port)
            X79: numpy array of shape (n, 79)

        Returns:
            List of dicts, each containing semantic keys used by Rules/Math.
        """
        n = X79.shape[0]
        if n == 0:
            return []

        # Batch distribution over destination ports
        try:
            dst_ports = [int(f.get('dst_port', 0)) for f in flows[:n]]
        except Exception:
            dst_ports = [0 for _ in range(n)]

        unique_ports = len(set(dst_ports))
        port_counts = {}
        for p in dst_ports:
            port_counts[p] = port_counts.get(p, 0) + 1

        total = float(n) if n > 0 else 1.0
        probs = np.array([c / total for c in port_counts.values()], dtype=float)
        # Shannon entropy in bits
        with np.errstate(divide='ignore', invalid='ignore'):
            entropy = -np.nansum(probs * (np.log(probs) / np.log(2))) if probs.size else 0.0
        if not np.isfinite(entropy):
            entropy = 0.0

        # Column indexes for required fields
        idx_flow_pkts_s = self._col_idx.get('flow_pkts_s', None)
        idx_syn = self._col_idx.get('syn_flag_cnt', None)
        idx_ack = self._col_idx.get('ack_flag_cnt', None)

        out: List[Dict[str, float]] = []
        for i in range(n):
            # pps from flow_pkts_s; fallback via tot packets / duration
            pps = 0.0
            if idx_flow_pkts_s is not None:
                p = float(X79[i, idx_flow_pkts_s])
                pps = p if np.isfinite(p) else 0.0
            else:
                # Fallback: (tot_fwd_pkts + tot_bwd_pkts) / max(flow_duration, 1e-9)
                fwd_idx = self._col_idx.get('tot_fwd_pkts')
                bwd_idx = self._col_idx.get('tot_bwd_pkts')
                dur_idx = self._col_idx.get('flow_duration')
                if fwd_idx is not None and bwd_idx is not None and dur_idx is not None:
                    num = float(X79[i, fwd_idx] + X79[i, bwd_idx])
                    den = max(float(X79[i, dur_idx]), 1e-9)
                    pps = num / den

            syn_ack_ratio = 0.0
            if idx_syn is not None and idx_ack is not None:
                syn = float(X79[i, idx_syn])
                ack = float(X79[i, idx_ack])
                syn_ack_ratio = syn / max(ack, 1.0)
                if not np.isfinite(syn_ack_ratio):
                    syn_ack_ratio = 0.0

            out.append({
                'pps': float(max(pps, 0.0)),
                'syn_ack_ratio': float(max(syn_ack_ratio, 0.0)),
                'unique_dst_ports': float(unique_ports),
                'port_entropy': float(entropy),
            })

        return out
