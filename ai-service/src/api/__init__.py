"""
API module for health checks, metrics, and detection stats.

HTTP endpoints for Kubernetes probes, Prometheus scraping, and detection monitoring.
"""

from .server import start_api_server, APIHandler

__all__ = ['start_api_server', 'APIHandler']
