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

from http.server import BaseHTTPRequestHandler, HTTPServer
from prometheus_client import generate_latest, CONTENT_TYPE_LATEST
import threading
import json
import logging


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
        if self.path == '/health':
            self._handle_health()
        elif self.path == '/ready':
            self._handle_ready()
        elif self.path == '/metrics':
            self._handle_metrics()
        elif self.path == '/detections/stats':
            self._handle_detection_stats()
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
            self._send_json(status_code, {
                "ready": ready,
                "state": health.get('state', 'unknown'),
                "components": components,
                "circuit_breaker": circuit_breaker_state
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
        
        Returns detection loop metrics including:
        - Total detections
        - Detections published
        - Detections rate limited
        - Last detection timestamp
        - Average latency
        
        Returns:
            200: Detection statistics
            503: Detection loop not initialized
            500: Exception during stats collection
        """
        try:
            if self.service_manager is None:
                self._send_json(503, {"error": "service_manager_not_initialized"})
                return
            
            if not hasattr(self.service_manager, 'detection_loop') or \
               self.service_manager.detection_loop is None:
                self._send_json(503, {
                    "error": "detection_loop_not_initialized",
                    "message": "Detection loop not started"
                })
                return
            
            metrics = self.service_manager.detection_loop.get_metrics()
            
            self._send_json(200, {
                "detection_loop": {
                    "running": self.service_manager.detection_loop.is_healthy(),
                    "metrics": metrics
                }
            })
        except Exception as e:
            self._send_json(500, {"error": str(e)})
    
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
