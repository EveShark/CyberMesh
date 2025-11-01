"""PostgreSQL telemetry source for real DDoS data."""

import psycopg2
import time
from datetime import datetime
from typing import List, Dict
from ..logging import get_logger
from .telemetry import TelemetrySource


class PostgresTelemetrySource(TelemetrySource):
    """Pull real network flows from PostgreSQL."""
    
    def __init__(self, db_config: dict, sample_size: int = 100, table_name: str = 'test_ddos_binary', schema: str = 'curated'):
        self.db_config = db_config
        self.sample_size = sample_size
        self.table_name = table_name
        self.schema = schema
        self.full_table = f"{schema}.{table_name}"
        self.logger = get_logger(__name__)
        self._test_connection()
    
    def _test_connection(self):
        """Test database connection."""
        try:
            conn = psycopg2.connect(**self.db_config)
            cur = conn.cursor()
            cur.execute(f"SELECT COUNT(*) FROM {self.full_table} WHERE label='ddos'")
            count = cur.fetchone()[0]
            conn.close()
            self.logger.info(f"PostgreSQL connection OK: {count:,} DDoS rows in {self.full_table}")
        except Exception as e:
            self.logger.error(f"PostgreSQL connection failed: {e}")
            raise
    
    def get_network_flows(self, limit: int = 100) -> List[Dict]:
        """
        Pull DDoS samples from Postgres.
        
        Returns network flows in AI service format.
        """
        try:
            conn = psycopg2.connect(**self.db_config)
            cur = conn.cursor()
            
            # Pull random DDoS samples with ALL 79 features
            query = f"""
                SELECT 
                    src_port, dst_port, protocol, flow_duration,
                    tot_fwd_pkts, tot_bwd_pkts, totlen_fwd_pkts, totlen_bwd_pkts,
                    fwd_pkt_len_max, fwd_pkt_len_min, fwd_pkt_len_mean, fwd_pkt_len_std,
                    bwd_pkt_len_max, bwd_pkt_len_min, bwd_pkt_len_mean, bwd_pkt_len_std,
                    flow_byts_s, flow_pkts_s,
                    flow_iat_mean, flow_iat_std, flow_iat_max, flow_iat_min,
                    fwd_iat_tot, fwd_iat_mean, fwd_iat_std, fwd_iat_max, fwd_iat_min,
                    bwd_iat_tot, bwd_iat_mean, bwd_iat_std, bwd_iat_max, bwd_iat_min,
                    fwd_psh_flags, bwd_psh_flags, fwd_urg_flags, bwd_urg_flags,
                    fwd_header_len, bwd_header_len, fwd_pkts_s, bwd_pkts_s,
                    pkt_len_min, pkt_len_max, pkt_len_mean, pkt_len_std, pkt_len_var,
                    fin_flag_cnt, syn_flag_cnt, rst_flag_cnt, psh_flag_cnt,
                    ack_flag_cnt, urg_flag_cnt, cwe_flag_count, ece_flag_cnt,
                    down_up_ratio, pkt_size_avg, fwd_seg_size_avg, bwd_seg_size_avg,
                    fwd_byts_b_avg, fwd_pkts_b_avg, fwd_blk_rate_avg,
                    bwd_byts_b_avg, bwd_pkts_b_avg, bwd_blk_rate_avg,
                    subflow_fwd_pkts, subflow_fwd_byts, subflow_bwd_pkts, subflow_bwd_byts,
                    init_fwd_win_byts, init_bwd_win_byts, fwd_act_data_pkts, fwd_seg_size_min,
                    active_mean, active_std, active_max, active_min,
                    idle_mean, idle_std, idle_max, idle_min,
                    label, src_ip, dst_ip, flow_id
                FROM {self.full_table}
                WHERE label = 'ddos'
                ORDER BY RANDOM()
                LIMIT %s
            """
            
            cur.execute(query, (limit,))
            rows = cur.fetchall()
            
            flows = []
            
            # Map all 79 features + metadata
            for row in rows:
                flow = {}
                
                # 79 features in exact order
                col_names = [
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
                
                # Map row values to feature dict
                for i, col_name in enumerate(col_names):
                    value = row[i]
                    # Safe conversion
                    if value is None:
                        flow[col_name] = 0.0
                    else:
                        flow[col_name] = float(value)
                
                # Metadata (after 79 features)
                flow['label'] = row[79]
                flow['src_ip'] = row[80]
                flow['dst_ip'] = row[81]
                flow['flow_id'] = row[82]
                
                flows.append(flow)
            
            conn.close()
            self.logger.info(f"Loaded {len(flows)} DDoS flows from PostgreSQL")
            return flows
            
        except Exception as e:
            self.logger.error(f"Failed to load flows from PostgreSQL: {e}")
            return []
    
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
