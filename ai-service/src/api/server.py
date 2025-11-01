"""
HTTP API Server for health checks, metrics, and detection stats.

Endpoints:
- GET /health            - Liveness probe (is service alive?)
- GET /ready             - Readiness probe (can service handle traffic?)
- GET /metrics           - Prometheus metrics scraping
- GET /detections/stats  - Detection loop statistics

Security:
- No authentication (health checks should be public)
- No secrets in responses
- Minimal attack surface
"""

import json
import logging
import os
import secrets
import threading
import time
from datetime import datetime
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlparse

from prometheus_client import CONTENT_TYPE_LATEST, generate_latest


API_BEARER_TOKEN = os.getenv("AI_SERVICE_API_TOKEN", "")


class APIHandler(BaseHTTPRequestHandler):
    """
    HTTP handler for health/metrics/detection endpoints.
    
    Injected dependencies:
    - service_manager: ServiceManager instance (set via class attribute)
    """
    
    service_manager = None  # Injected from start_api_server()
    
    def log_message(self, format, *args):
        """
        Suppress default HTTP logging (use our logger).
        
        Override to prevent spam in console. ServiceManager handles logging.
        """
        pass
    
    def do_GET(self):
        """Handle GET requests for health/ready/metrics/detections."""
        parsed = urlparse(self.path)
        path = parsed.path
        query_params = parse_qs(parsed.query)

        if path == '/health':
            self._handle_health()
        elif path == '/ready':
            self._handle_ready()
        elif path == '/metrics':
            self._handle_metrics()
        elif path == '/detections/stats':
            self._handle_detection_stats()
        elif path == '/detections/history':
            if self._require_auth():
                self._handle_detection_history(query_params)
        elif path == '/detections/suspicious-nodes':
            if self._require_auth():
                self._handle_suspicious_nodes(query_params)
        else:
            self.send_error(404, "Not Found")
    
    def _handle_health(self):
        """
        Liveness probe - is service running?
        
        Returns:
            200: Service is alive
            503: Service is unhealthy (error, stopped, etc.)
            500: Exception during health check
        """
        try:
            if self.service_manager is None:
                self._send_json(503, {"status": "unhealthy", "error": "service_manager_not_initialized"})
                return
            
            health = self.service_manager.health_check()
            status = health.get('status', 'unknown')
            
            if status == 'running':
                self._send_json(200, {
                    "status": "healthy",
                    "state": "running",
                    "uptime_seconds": health.get('uptime_seconds', 0)
                })
            else:
                self._send_json(503, {
                    "status": "unhealthy",
                    "state": status,
                    "error": health.get('error')
                })
        except Exception as e:
            self._send_json(500, {"status": "error", "error": str(e)})
    
    def _handle_ready(self):
        """
        Readiness probe - can service handle traffic?
        
        Checks:
        - Service state is RUNNING
        - Producer initialized
        - Consumer initialized
        - Circuit breaker not OPEN
        
        Returns:
            200: Service is ready
            503: Service is not ready
            500: Exception during readiness check
        """
        try:
            if self.service_manager is None:
                self._send_json(503, {"ready": False, "error": "service_manager_not_initialized"})
                return
            
            health = self.service_manager.health_check()
            components = health.get('components', {})
            
            # Service ready if all critical components initialized
            ready = all([
                health.get('status') == 'running',
                components.get('producer', False),
                components.get('consumer', False)
            ])
            
            # Check circuit breaker (if open, not ready)
            circuit_breaker_state = health.get('circuit_breaker', 'unknown')
            if circuit_breaker_state == 'open':
                ready = False
            
            status_code = 200 if ready else 503
            detection_snapshot = health.get('detection_loop') or self.service_manager.get_detection_metrics()
            self._send_json(status_code, {
                "ready": ready,
                "state": health.get('state', 'unknown'),
                "components": components,
                "circuit_breaker": circuit_breaker_state,
                "detection_loop": detection_snapshot
            })
        except Exception as e:
            self._send_json(500, {"ready": False, "error": str(e)})
    
    def _handle_metrics(self):
        """
        Prometheus metrics endpoint.
        
        Returns:
            200: Metrics in Prometheus text format
            500: Exception during metrics collection
        """
        try:
            self.send_response(200)
            self.send_header('Content-Type', CONTENT_TYPE_LATEST)
            self.end_headers()
            self.wfile.write(generate_latest())
        except Exception as e:
            self.send_error(500, f"Metrics error: {e}")
    
    def _handle_detection_stats(self):
        """
        Detection loop statistics endpoint.
        
        Returns detection loop metrics even when the loop is idle or stopped.

        Response includes cached metrics, derived throughput/latency stats,
        publisher and Kafka telemetry to support the System Health dashboard.

        Always returns HTTP 200 when the service manager is available.
        """
        try:
            if self.service_manager is None:
                self._send_json(503, {"error": "service_manager_not_initialized"})
                return
            
            health = self.service_manager.health_check()
            detection_snapshot = health.get('detection_loop') or self.service_manager.get_detection_metrics()
            metrics = detection_snapshot.get('metrics', {}) or {}

            now = time.time()

            def _age(timestamp):
                if timestamp is None:
                    return None
                try:
                    return max(0.0, now - float(timestamp))
                except (TypeError, ValueError):
                    return None

            uptime = health.get('uptime_seconds')
            total_detections = float(metrics.get('detections_total', 0) or 0)
            published = float(metrics.get('detections_published', 0) or 0)
            rate_limited = float(metrics.get('detections_rate_limited', 0) or 0)
            error_count = float(metrics.get('errors', 0) or 0)
            loop_iterations = float(metrics.get('loop_iterations', 0) or 0)

            detections_per_minute = None
            publish_rate_per_minute = None
            error_rate_per_hour = None
            iterations_per_minute = None

            if uptime and uptime > 0:
                minutes = uptime / 60.0
                hours = uptime / 3600.0
                if minutes > 0:
                    detections_per_minute = total_detections / minutes
                    publish_rate_per_minute = published / minutes
                    iterations_per_minute = loop_iterations / minutes
                if hours > 0:
                    error_rate_per_hour = error_count / hours

            derived = {
                "detections_per_minute": detections_per_minute,
                "publish_rate_per_minute": publish_rate_per_minute,
                "iterations_per_minute": iterations_per_minute,
                "error_rate_per_hour": error_rate_per_hour,
                "publish_success_ratio": None,
            }

            if total_detections > 0:
                derived["publish_success_ratio"] = published / total_detections

            cache_age = None
            if detection_snapshot.get('last_updated'):
                cache_age = max(0.0, now - detection_snapshot['last_updated'])

            response = {
                "status": health.get('status', 'unknown'),
                "state": health.get('state', 'unknown'),
                "uptime_seconds": uptime,
                "detection_loop": {
                    "running": detection_snapshot.get('running', False),
                    "last_updated": detection_snapshot.get('last_updated'),
                    "cache_age_seconds": cache_age,
                    "metrics": metrics,
                    "seconds_since_last_detection": _age(metrics.get('last_detection_time')),
                    "seconds_since_last_iteration": _age(metrics.get('last_iteration_time')),
                    "avg_latency_ms": metrics.get('avg_latency_ms'),
                    "last_latency_ms": metrics.get('last_latency_ms'),
                },
                "derived": derived,
                "publisher_metrics": health.get('publisher_metrics', {}),
                "handler_metrics": health.get('handler_metrics', {}),
                "rate_limiter": health.get('rate_limiter', {}),
                "kafka": {
                    "producer": health.get('kafka_producer', {}),
                    "consumer": health.get('kafka_consumer', {}),
                },
                "engines": self.service_manager.get_engine_metrics_summary(),
                "variants": self.service_manager.get_variant_metrics_summary(),
            }

            self._send_json(200, response)
        except Exception as e:
            self._send_json(500, {"error": str(e)})

    def _handle_detection_history(self, query_params):
        try:
            if self.service_manager is None:
                self._send_json(503, {"error": "service_manager_not_initialized"})
                return

            limit = self._parse_limit(query_params.get('limit', ['50'])[0], default=50, max_value=200)
            since_value = query_params.get('since', [None])[0]
            since_ts = self._parse_since(since_value) if since_value else None
            validator_id = query_params.get('validator_id', [None])[0]

            history = self.service_manager.get_detection_history(
                limit=limit,
                since=since_ts,
                validator_id=validator_id,
            )

            self._send_json(200, {
                "detections": history,
                "count": len(history),
                "updated_at": datetime.utcnow().isoformat() + "Z",
            })
        except Exception as exc:
            self._send_json(500, {"error": str(exc)})

    def _handle_suspicious_nodes(self, query_params):
        try:
            if self.service_manager is None:
                self._send_json(503, {"error": "service_manager_not_initialized"})
                return

            limit = self._parse_limit(query_params.get('limit', ['10'])[0], default=10, max_value=50)
            nodes = self.service_manager.get_ai_suspicious_nodes(limit=limit)

            self._send_json(200, {
                "nodes": nodes,
                "updated_at": datetime.utcnow().isoformat() + "Z",
            })
        except Exception as exc:
            self._send_json(500, {"error": str(exc)})

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _require_auth(self) -> bool:
        if not API_BEARER_TOKEN:
            return True

        header = self.headers.get('Authorization', '')
        if not header.startswith('Bearer '):
            self._send_json(401, {"error": "unauthorized"})
            return False

        token = header.split('Bearer ', 1)[1].strip()
        if not token or not secrets.compare_digest(token, API_BEARER_TOKEN):
            self._send_json(401, {"error": "unauthorized"})
            return False

        return True

    def _parse_limit(self, value: str, default: int, max_value: int) -> int:
        try:
            parsed = int(value)
        except (TypeError, ValueError):
            return default

        if parsed <= 0:
            return default
        if parsed > max_value:
            return max_value
        return parsed

    def _parse_since(self, value: str) -> float:
        try:
            return float(value)
        except (TypeError, ValueError):
            pass

        try:
            cleaned = value.rstrip('Z')
            return datetime.fromisoformat(cleaned).timestamp()
        except Exception:
            return None
    
    def _send_json(self, status, data):
        """
        Send JSON response.
        
        Args:
            status: HTTP status code
            data: Dictionary to serialize as JSON
        """
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps(data).encode('utf-8'))


def start_api_server(service_manager, host='0.0.0.0', port=8080):
    """
    Start HTTP API server in background daemon thread.
    
    Serves health checks, metrics, and detection statistics.
    
    Args:
        service_manager: ServiceManager instance
        host: Listen address (default: 0.0.0.0 for all interfaces)
        port: Listen port (default: 8080)
    
    Returns:
        HTTPServer instance (can be shutdown with server.shutdown())
    
    Example:
        from src.api.server import start_api_server
        
        manager = ServiceManager()
        manager.initialize(settings)
        manager.start()
        
        # Start API server
        server = start_api_server(manager, port=8080)
        
        # ... service runs ...
        
        # Graceful shutdown
        server.shutdown()
        manager.stop()
    """
    # Inject service_manager into handler class
    APIHandler.service_manager = service_manager
    
    # Create HTTP server
    server = HTTPServer((host, port), APIHandler)
    
    # Start in daemon thread (dies with main process)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.name = 'APIServer'
    thread.start()
    
    # Log startup (use logging module since we're not in service context)
    logger = logging.getLogger(__name__)
    logger.info(f"API Server started on {host}:{port}")
    
    return server
