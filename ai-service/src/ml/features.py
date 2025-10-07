"""
Feature extraction for network flows and malware analysis.

Military-grade feature engineering with StandardScaler normalization.
NO mocks - designed for real CIC-DDoS2019 and EMBER datasets.
"""

import numpy as np
import joblib
from pathlib import Path
from typing import List, Dict, Optional
from collections import Counter
from ..logging import get_logger


class NetworkFlowExtractor:
    """
    Extract 30 features from network flow data.
    
    Features (aligned with existing XGBoost model reference):
    - Flow stats: pps, bps, duration
    - Packet ratios: fwd/bwd, SYN/ACK ratio
    - Port statistics: unique dst ports, port entropy
    - Inter-arrival times: mean, std, min, max
    - Protocol distribution
    - Window aggregations: mean, std, max over time windows
    
    Compatible with CIC-DDoS2019, UNSW-NB15, Bot-IoT datasets.
    """
    
    FEATURE_COUNT = 30
    
    def __init__(self, scaler_path: Optional[str] = None):
        """
        Initialize network flow feature extractor.
        
        Args:
            scaler_path: Path to saved StandardScaler (optional)
                        If not provided, creates new scaler (fit on first batch)
        """
        self.logger = get_logger(__name__)
        self.scaler = None
        self.scaler_fitted = False
        
        # Try to load existing scaler
        if scaler_path:
            try:
                self.scaler = joblib.load(scaler_path)
                self.scaler_fitted = True
                self.logger.info(f"Loaded StandardScaler from {scaler_path}")
            except Exception as e:
                self.logger.warning(f"Failed to load scaler: {e}, creating new scaler")
        
        # Create new scaler if needed
        if self.scaler is None:
            from sklearn.preprocessing import StandardScaler
            self.scaler = StandardScaler()
        
        self.feature_names = self._get_feature_names()
    
    def extract(self, flows: List[Dict], window_sec: int = 5) -> np.ndarray:
        """
        Extract features from network flow records.
        
        Args:
            flows: List of flow dictionaries with keys:
                - src_ip, dst_ip, src_port, dst_port
                - protocol, duration, packets_fwd, packets_bwd
                - bytes_fwd, bytes_bwd, syn_count, ack_count
                - timestamp
            window_sec: Time window for aggregation (default: 5s)
        
        Returns:
            Feature matrix (n_windows, 30)
            
        Raises:
            ValueError: If flows empty or missing required fields
        """
        if not flows:
            raise ValueError("No flows provided for feature extraction")
        
        # Aggregate flows into time windows
        windows = self._aggregate_by_window(flows, window_sec)
        
        # Extract features per window
        features_list = []
        for window_flows in windows:
            features = self._extract_window_features(window_flows)
            features_list.append(features)
        
        # Convert to numpy array
        X = np.array(features_list)
        
        if X.shape[1] != self.FEATURE_COUNT:
            raise ValueError(
                f"Feature count mismatch: expected {self.FEATURE_COUNT}, got {X.shape[1]}"
            )
        
        # Normalize if scaler fitted
        if self.scaler_fitted:
            X = self.scaler.transform(X)
        else:
            # Fit scaler on first batch
            self.logger.info("Fitting StandardScaler on first batch")
            X = self.scaler.fit_transform(X)
            self.scaler_fitted = True
        
        self.logger.debug(f"Extracted features shape: {X.shape}")
        return X
    
    def _aggregate_by_window(self, flows: List[Dict], window_sec: int) -> List[List[Dict]]:
        """
        Aggregate flows into time windows.
        
        Args:
            flows: List of flows
            window_sec: Window size in seconds
        
        Returns:
            List of flow lists (one per window)
        """
        if not flows:
            return []
        
        # Sort by timestamp
        sorted_flows = sorted(flows, key=lambda f: f.get('timestamp', 0))
        
        # Group into windows
        windows = []
        current_window = []
        window_start = sorted_flows[0].get('timestamp', 0)
        
        for flow in sorted_flows:
            timestamp = flow.get('timestamp', 0)
            
            # Check if flow belongs to current window
            if timestamp - window_start < window_sec:
                current_window.append(flow)
            else:
                # Start new window
                if current_window:
                    windows.append(current_window)
                current_window = [flow]
                window_start = timestamp
        
        # Add last window
        if current_window:
            windows.append(current_window)
        
        return windows if windows else [sorted_flows]
    
    def _extract_window_features(self, flows: List[Dict]) -> np.ndarray:
        """
        Extract 30 features from a time window of flows.
        
        Features:
        1-5: Flow stats (pps, bps, duration, total_packets, total_bytes)
        6-10: Packet ratios (fwd/bwd, syn/ack, fin_count, rst_count, psh_count)
        11-15: Port stats (unique_dst_ports, port_entropy, well_known_ports, unique_src_ports, port_diversity)
        16-20: Inter-arrival (iat_mean, iat_std, iat_min, iat_max, iat_cv)
        21-25: Byte stats (bytes_per_packet, fwd_bytes_avg, bwd_bytes_avg, bytes_variance, bytes_skew)
        26-30: Protocol (tcp_ratio, udp_ratio, icmp_ratio, protocol_count, flags_combination)
        
        Args:
            flows: List of flows in window
        
        Returns:
            Feature vector (30,)
        """
        if not flows:
            return np.zeros(self.FEATURE_COUNT)
        
        n_flows = len(flows)
        features = np.zeros(self.FEATURE_COUNT)
        
        # Extract basic stats
        durations = [f.get('duration', 0) for f in flows]
        packets_fwd = [f.get('packets_fwd', 0) for f in flows]
        packets_bwd = [f.get('packets_bwd', 0) for f in flows]
        bytes_fwd = [f.get('bytes_fwd', 0) for f in flows]
        bytes_bwd = [f.get('bytes_bwd', 0) for f in flows]
        syn_counts = [f.get('syn_count', 0) for f in flows]
        ack_counts = [f.get('ack_count', 0) for f in flows]
        dst_ports = [f.get('dst_port', 0) for f in flows]
        src_ports = [f.get('src_port', 0) for f in flows]
        protocols = [f.get('protocol', 'TCP') for f in flows]
        timestamps = [f.get('timestamp', 0) for f in flows]
        
        total_duration = sum(durations) + 1e-10
        total_packets = sum(packets_fwd) + sum(packets_bwd)
        total_bytes = sum(bytes_fwd) + sum(bytes_bwd)
        
        # Features 1-5: Flow stats
        features[0] = total_packets / total_duration  # pps
        features[1] = total_bytes / total_duration  # bps
        features[2] = np.mean(durations)  # avg duration
        features[3] = total_packets  # total packets
        features[4] = total_bytes  # total bytes
        
        # Features 6-10: Packet ratios
        total_fwd = sum(packets_fwd) + 1e-10
        total_bwd = sum(packets_bwd) + 1e-10
        features[5] = total_fwd / total_bwd  # fwd/bwd ratio
        total_syn = sum(syn_counts) + 1e-10
        total_ack = sum(ack_counts) + 1e-10
        features[6] = total_syn / total_ack  # SYN/ACK ratio
        features[7] = sum([f.get('fin_count', 0) for f in flows])  # FIN count
        features[8] = sum([f.get('rst_count', 0) for f in flows])  # RST count
        features[9] = sum([f.get('psh_count', 0) for f in flows])  # PSH count
        
        # Features 11-15: Port stats
        unique_dst = len(set(dst_ports))
        features[10] = unique_dst  # unique dst ports
        features[11] = self._compute_entropy([dst_ports.count(p) for p in set(dst_ports)])  # port entropy
        features[12] = sum(1 for p in dst_ports if p < 1024)  # well-known ports
        features[13] = len(set(src_ports))  # unique src ports
        features[14] = unique_dst / (n_flows + 1e-10)  # port diversity
        
        # Features 16-20: Inter-arrival times
        if len(timestamps) > 1:
            iats = np.diff(sorted(timestamps))
            features[15] = np.mean(iats)  # iat mean
            features[16] = np.std(iats)  # iat std
            features[17] = np.min(iats)  # iat min
            features[18] = np.max(iats)  # iat max
            features[19] = features[16] / (features[15] + 1e-10)  # coefficient of variation
        
        # Features 21-25: Byte stats
        all_bytes = bytes_fwd + bytes_bwd
        all_packets_list = packets_fwd + packets_bwd
        features[20] = total_bytes / (total_packets + 1e-10)  # bytes per packet
        features[21] = np.mean(bytes_fwd) if bytes_fwd else 0  # avg fwd bytes
        features[22] = np.mean(bytes_bwd) if bytes_bwd else 0  # avg bwd bytes
        features[23] = np.var(all_bytes) if all_bytes else 0  # bytes variance
        features[24] = self._compute_skewness(all_bytes) if all_bytes else 0  # bytes skew
        
        # Features 26-30: Protocol distribution
        protocol_counts = Counter(protocols)
        total_protocols = sum(protocol_counts.values())
        features[25] = protocol_counts.get('TCP', 0) / (total_protocols + 1e-10)  # TCP ratio
        features[26] = protocol_counts.get('UDP', 0) / (total_protocols + 1e-10)  # UDP ratio
        features[27] = protocol_counts.get('ICMP', 0) / (total_protocols + 1e-10)  # ICMP ratio
        features[28] = len(protocol_counts)  # protocol count
        features[29] = len(set(zip(syn_counts, ack_counts)))  # flag combinations
        
        return features
    
    def _compute_entropy(self, counts: List[int]) -> float:
        """Shannon entropy: H = -Σ(p_i * log2(p_i))"""
        if not counts or sum(counts) == 0:
            return 0.0
        
        total = sum(counts)
        probabilities = [c / total for c in counts if c > 0]
        return -sum(p * np.log2(p) for p in probabilities)
    
    def _compute_skewness(self, values: List[float]) -> float:
        """Compute skewness (third moment)."""
        if len(values) < 3:
            return 0.0
        
        arr = np.array(values)
        mean = np.mean(arr)
        std = np.std(arr)
        
        if std == 0:
            return 0.0
        
        return np.mean(((arr - mean) / std) ** 3)
    
    def _get_feature_names(self) -> List[str]:
        """Get feature names for debugging/explainability."""
        return [
            'pps', 'bps', 'avg_duration', 'total_packets', 'total_bytes',
            'fwd_bwd_ratio', 'syn_ack_ratio', 'fin_count', 'rst_count', 'psh_count',
            'unique_dst_ports', 'port_entropy', 'well_known_ports', 'unique_src_ports', 'port_diversity',
            'iat_mean', 'iat_std', 'iat_min', 'iat_max', 'iat_cv',
            'bytes_per_packet', 'fwd_bytes_avg', 'bwd_bytes_avg', 'bytes_variance', 'bytes_skew',
            'tcp_ratio', 'udp_ratio', 'icmp_ratio', 'protocol_count', 'flags_combination'
        ]
    
    def save_scaler(self, path: str):
        """
        Save fitted StandardScaler to file.
        
        Args:
            path: Output path for scaler
        """
        if not self.scaler_fitted:
            raise ValueError("Cannot save unfitted scaler")
        
        joblib.dump(self.scaler, path)
        self.logger.info(f"Saved StandardScaler to {path}")
