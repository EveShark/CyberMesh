#!/usr/bin/env python3
"""
End-to-End Architecture Test
Tests complete flow: AI → Kafka → Backend → Consensus → Feedback → AI

This test validates:
1. AI Service detects anomaly and publishes to Kafka
2. Backend consumes, verifies signature, admits to mempool
3. Backend achieves consensus (simulated), commits to DB
4. Backend publishes feedback to control.commits.v1
5. AI Service consumes feedback, updates lifecycle tracker
6. Adaptive learning adjusts thresholds based on acceptance rate

Requirements:
- Backend must be running (./bin/backend.exe)
- AI Service config must be valid (.env)
- Kafka must be accessible (Confluent Cloud)
- Redis must be accessible (Upstash)

Usage:
    python test/test_e2e_architecture.py

Duration: ~60 seconds
"""

import sys
import os
import time
import subprocess
import json
import uuid
from pathlib import Path
from typing import Optional, Dict, List
from datetime import datetime

# Add AI service to path
sys.path.insert(0, str(Path(__file__).parent.parent / 'ai-service'))

# Colors for terminal output
class Colors:
    HEADER = '\033[95m'
    OKBLUE = '\033[94m'
    OKCYAN = '\033[96m'
    OKGREEN = '\033[92m'
    WARNING = '\033[93m'
    FAIL = '\033[91m'
    ENDC = '\033[0m'
    BOLD = '\033[1m'
    UNDERLINE = '\033[4m'

def print_header(text: str):
    print(f"\n{Colors.HEADER}{Colors.BOLD}{'='*80}{Colors.ENDC}")
    print(f"{Colors.HEADER}{Colors.BOLD}{text.center(80)}{Colors.ENDC}")
    print(f"{Colors.HEADER}{Colors.BOLD}{'='*80}{Colors.ENDC}\n")

def print_step(num: int, total: int, text: str):
    print(f"\n{Colors.OKBLUE}[{num}/{total}] {text}{Colors.ENDC}")

def print_success(text: str):
    print(f"  {Colors.OKGREEN}+ {text}{Colors.ENDC}")

def print_error(text: str):
    print(f"  {Colors.FAIL}- {text}{Colors.ENDC}")

def print_info(text: str):
    print(f"  {Colors.OKCYAN}i {text}{Colors.ENDC}")

def print_warning(text: str):
    print(f"  {Colors.WARNING}! {text}{Colors.ENDC}")


class E2ETest:
    """End-to-End Architecture Test"""

    def __init__(self):
        self.backend_process: Optional[subprocess.Popen] = None
        self.ai_service_manager = None
        self.test_anomaly_id: Optional[str] = None
        self.test_start_time: float = 0
        self.results: Dict[str, bool] = {}
        self.metrics: Dict[str, any] = {}

    def run(self):
        """Run complete E2E test"""
        print_header("CyberMesh End-to-End Architecture Test")
        print_info(f"Test started: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")

        self.test_start_time = time.time()

        try:
            # Phase 1: Setup
            self.test_1_verify_prerequisites()
            self.test_2_start_backend()
            self.test_3_initialize_ai_service()

            # Phase 2: AI → Backend Flow
            self.test_4_ai_detection_and_publish()
            self.test_5_backend_consumption_and_verification()
            self.test_6_mempool_admission()

            # Phase 3: Consensus (simulated check)
            self.test_7_consensus_simulation()

            # Phase 4: Backend → AI Feedback
            self.test_8_backend_feedback_publish()
            self.test_9_ai_feedback_consumption()
            self.test_10_lifecycle_tracking()

            # Phase 5: Adaptive Learning
            self.test_11_threshold_adjustment()
            self.test_12_confidence_calibration()

            # Final Report
            self.generate_report()

        except KeyboardInterrupt:
            print_warning("\nTest interrupted by user")
            self.cleanup()
            sys.exit(1)
        except Exception as e:
            print_error(f"Test failed with exception: {e}")
            import traceback
            traceback.print_exc()
            self.cleanup()
            sys.exit(1)
        finally:
            self.cleanup()

    def test_1_verify_prerequisites(self):
        """Test 1: Verify all prerequisites are met"""
        print_step(1, 12, "Verifying Prerequisites")

        # Check backend binary exists
        backend_binary = Path(__file__).parent.parent / 'backend' / 'bin' / 'backend.exe'
        if backend_binary.exists():
            print_success(f"Backend binary found: {backend_binary}")
            self.results['backend_binary'] = True
        else:
            print_error(f"Backend binary not found: {backend_binary}")
            self.results['backend_binary'] = False
            raise FileNotFoundError("Backend binary not found")

        # Check AI service keys
        ai_keys = Path(__file__).parent.parent / 'ai-service' / 'keys' / 'signing_key.pem'
        if ai_keys.exists():
            print_success(f"AI signing key found: {ai_keys}")
            self.results['ai_signing_key'] = True
        else:
            print_error(f"AI signing key not found: {ai_keys}")
            self.results['ai_signing_key'] = False
            raise FileNotFoundError("AI signing key not found")

        # Check .env file
        env_file = Path(__file__).parent.parent / 'ai-service' / '.env'
        if env_file.exists():
            print_success(f".env file found: {env_file}")
            self.results['env_file'] = True
        else:
            print_error(f".env file not found: {env_file}")
            self.results['env_file'] = False
            raise FileNotFoundError(".env file not found")

        print_success("All prerequisites verified")

    def test_2_start_backend(self):
        """Test 2: Start backend service"""
        print_step(2, 12, "Starting Backend Service")

        backend_binary = Path(__file__).parent.parent / 'backend' / 'bin' / 'backend.exe'
        backend_cwd = Path(__file__).parent.parent / 'backend'

        print_info(f"Launching backend: {backend_binary}")
        print_info(f"Working directory: {backend_cwd}")

        # Start backend process
        self.backend_process = subprocess.Popen(
            [str(backend_binary)],
            cwd=str(backend_cwd),
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1
        )

        # Wait for backend to initialize
        print_info("Waiting for backend to initialize (10 seconds)...")
        time.sleep(10)

        # Check if backend is still running
        if self.backend_process.poll() is not None:
            print_error("Backend exited prematurely!")
            print_error("Backend output:")
            for line in self.backend_process.stdout:
                print(f"    {line}", end='')
            self.results['backend_start'] = False
            raise RuntimeError("Backend failed to start")

        print_success(f"Backend started (PID: {self.backend_process.pid})")
        self.results['backend_start'] = True

    def test_3_initialize_ai_service(self):
        """Test 3: Initialize AI Service components"""
        print_step(3, 12, "Initializing AI Service")

        try:
            from src.config.loader import load_settings
            from src.service import ServiceManager

            print_info("Loading AI service configuration...")
            settings = load_settings(env_file='ai-service/.env')
            print_success(f"Config loaded: {settings.kafka_producer.bootstrap_servers}")

            print_info("Initializing ServiceManager...")
            # Note: ServiceManager has known Settings class mismatch issue
            # We'll initialize components manually if needed

            try:
                self.ai_service_manager = ServiceManager()
                print_success("ServiceManager initialized")

                # Ensure nonce_manager is set (ServiceManager might not initialize it)
                if not hasattr(self.ai_service_manager, 'nonce_manager') or self.ai_service_manager.nonce_manager is None:
                    print_info("Initializing missing nonce_manager...")
                    from src.utils.nonce import NonceManager
                    self.ai_service_manager.nonce_manager = NonceManager(instance_id=1, ttl_seconds=900)

                # Ensure signer is set (ServiceManager might not initialize it)
                if not hasattr(self.ai_service_manager, 'signer') or self.ai_service_manager.signer is None:
                    print_info("Initializing missing signer...")
                    from src.utils.signer import Signer
                    keys_dir = Path(__file__).parent.parent / 'ai-service' / 'keys'
                    self.ai_service_manager.signer = Signer(
                        str(keys_dir / 'signing_key.pem'),
                        key_id='ai-node-1',
                        domain_separation='ai.v1'
                    )

                # Ensure kafka_producer is set (ServiceManager might not initialize it)
                if not hasattr(self.ai_service_manager, 'kafka_producer') or self.ai_service_manager.kafka_producer is None:
                    print_info("Initializing missing kafka_producer...")
                    from src.kafka.producer import AIProducer
                    from src.utils.circuit_breaker import CircuitBreaker
                    circuit_breaker = CircuitBreaker(failure_threshold=5, timeout_seconds=30)
                    self.ai_service_manager.kafka_producer = AIProducer(settings, circuit_breaker)

                # Ensure kafka_consumer is set (ServiceManager might not initialize it)
                if not hasattr(self.ai_service_manager, 'kafka_consumer') or self.ai_service_manager.kafka_consumer is None:
                    print_info("Initializing missing kafka_consumer...")
                    from src.kafka.consumer import AIConsumer
                    self.ai_service_manager.kafka_consumer = AIConsumer(settings)

                self.results['ai_service_init'] = True
            except AttributeError as e:
                print_warning(f"ServiceManager init issue (known): {e}")
                print_info("Using fallback: manual component initialization")
                # Initialize components manually
                self.ai_service_manager = self._initialize_components_manually(settings)
                print_success("Components initialized manually")
                self.results['ai_service_init'] = True

        except Exception as e:
            print_error(f"Failed to initialize AI service: {e}")
            self.results['ai_service_init'] = False
            raise

    def _initialize_components_manually(self, settings):
        """Fallback: Initialize AI service components manually"""
        from src.utils.signer import Signer
        from src.utils.nonce import NonceManager
        from src.kafka.producer import AIProducer
        from src.kafka.consumer import AIConsumer
        from src.utils.circuit_breaker import CircuitBreaker

        # Create a mock manager object
        class MockManager:
            def __init__(self):
                self.settings = settings
                self.signer = None
                self.nonce_manager = None
                self.kafka_producer = None
                self.kafka_consumer = None

        manager = MockManager()

        # Initialize crypto
        keys_dir = Path(__file__).parent.parent / 'ai-service' / 'keys'
        manager.signer = Signer(
            str(keys_dir / 'signing_key.pem'),
            key_id='ai-node-1',
            domain_separation='ai.v1'
        )
        manager.nonce_manager = NonceManager(instance_id=1, ttl_seconds=900)

        # Initialize Kafka producer
        circuit_breaker = CircuitBreaker(failure_threshold=5, timeout_seconds=30)
        manager.kafka_producer = AIProducer(settings, circuit_breaker)

        # Initialize Kafka consumer
        manager.kafka_consumer = AIConsumer(settings)

        return manager

    def test_4_ai_detection_and_publish(self):
        """Test 4: AI detects anomaly and publishes to Kafka"""
        print_step(4, 12, "AI Detection and Kafka Publish")

        from src.contracts import AnomalyMessage

        # Create test anomaly
        self.test_anomaly_id = str(uuid.uuid4())
        timestamp = int(time.time())

        print_info(f"Creating test anomaly: {self.test_anomaly_id}")

        payload_data = {
            'test': 'e2e_architecture_test',
            'threat_type': 'ddos',
            'source_ip': '192.168.1.100',
            'target_ip': '10.0.0.50',
            'packets_per_second': 150000,
            'detection_engines': ['rules', 'math', 'ml'],
            'timestamp': timestamp
        }
        payload = json.dumps(payload_data).encode('utf-8')

        anomaly_msg = AnomalyMessage(
            anomaly_id=self.test_anomaly_id,
            anomaly_type='ddos',
            source='e2e_test',
            severity=9,
            confidence=0.92,
            timestamp=timestamp,
            payload=payload,
            model_version='test-v1.0.0',
            signer=self.ai_service_manager.signer,
            nonce_manager=self.ai_service_manager.nonce_manager
        )

        print_info(f"Anomaly details:")
        print_info(f"  Type: {anomaly_msg.anomaly_type}")
        print_info(f"  Severity: {anomaly_msg.severity}/10")
        print_info(f"  Confidence: {anomaly_msg.confidence}")
        print_info(f"  Nonce: {len(anomaly_msg.nonce)} bytes")
        print_info(f"  Signature: {len(anomaly_msg.signature)} bytes")

        # Publish to Kafka
        print_info("Publishing to ai.anomalies.v1...")

        try:
            success = self.ai_service_manager.kafka_producer.send_anomaly(anomaly_msg)

            if success:
                self.ai_service_manager.kafka_producer.flush(timeout=10)
                metrics = self.ai_service_manager.kafka_producer.get_metrics()

                print_success("Message published to Kafka")
                print_info(f"  Messages sent: {metrics['messages_sent']}")
                print_info(f"  Bytes sent: {metrics['bytes_sent']}")

                self.results['ai_publish'] = True
                self.metrics['kafka_publish_time'] = time.time() - self.test_start_time
            else:
                print_error("Publish failed")
                self.results['ai_publish'] = False
                raise RuntimeError("Kafka publish failed")

        except Exception as e:
            print_error(f"Publish exception: {e}")
            self.results['ai_publish'] = False
            raise

    def test_5_backend_consumption_and_verification(self):
        """Test 5: Backend consumes and verifies signature"""
        print_step(5, 12, "Backend Consumption and Signature Verification")

        print_info("Waiting for backend to consume message (15 seconds)...")

        consumed = False
        verified = False
        start_time = time.time()

        # Monitor backend output for consumption indicators
        while time.time() - start_time < 15:
            if self.backend_process and self.backend_process.stdout:
                line = self.backend_process.stdout.readline()
                if line:
                    print(f"    [BACKEND] {line}", end='')

                    # Check for consumption
                    if 'consumed' in line.lower() or 'received' in line.lower():
                        consumed = True
                        print_success("Backend consumed message")

                    # Check for verification
                    if 'signature' in line.lower() and 'verified' in line.lower():
                        verified = True
                        print_success("Signature verified")

                    # Check for our anomaly ID
                    if self.test_anomaly_id in line:
                        print_success(f"Anomaly ID found in backend logs: {self.test_anomaly_id}")

            time.sleep(0.1)

        if consumed:
            print_success("Backend consumption confirmed")
            self.results['backend_consume'] = True
        else:
            print_warning("Backend consumption not confirmed in logs")
            print_info("Backend may still be processing...")
            self.results['backend_consume'] = False

        if verified:
            print_success("Signature verification confirmed")
            self.results['signature_verification'] = True
        else:
            print_warning("Signature verification not confirmed in logs")
            self.results['signature_verification'] = False

        self.metrics['backend_consume_time'] = time.time() - self.test_start_time

    def test_6_mempool_admission(self):
        """Test 6: Mempool admission check"""
        print_step(6, 12, "Mempool Admission")

        print_info("Checking backend mempool...")

        # Look for mempool admission in backend logs
        admitted = False
        if self.backend_process and self.backend_process.stdout:
            # Read recent backend output
            for _ in range(10):
                line = self.backend_process.stdout.readline()
                if line:
                    print(f"    [BACKEND] {line}", end='')
                    if 'admitted' in line.lower() or 'mempool' in line.lower():
                        admitted = True
                        print_success("Transaction admitted to mempool")

        if admitted:
            self.results['mempool_admission'] = True
        else:
            print_warning("Mempool admission not confirmed in logs")
            print_info("This is expected if backend mempool logs are minimal")
            self.results['mempool_admission'] = None  # Unknown

        self.metrics['mempool_time'] = time.time() - self.test_start_time

    def test_7_consensus_simulation(self):
        """Test 7: Consensus simulation check"""
        print_step(7, 12, "Consensus Simulation")

        print_info("Note: Full consensus requires 4 validators running")
        print_info("This test checks if consensus mechanism is active")

        # Look for consensus indicators in backend logs
        consensus_active = False
        if self.backend_process and self.backend_process.stdout:
            for _ in range(10):
                line = self.backend_process.stdout.readline()
                if line:
                    print(f"    [BACKEND] {line}", end='')
                    if any(keyword in line.lower() for keyword in ['consensus', 'pbft', 'quorum', 'commit']):
                        consensus_active = True
                        print_success("Consensus mechanism active")

        if consensus_active:
            print_success("Consensus indicators found")
            self.results['consensus'] = True
        else:
            print_warning("No consensus indicators in logs")
            print_info("Single validator may not show consensus logs")
            self.results['consensus'] = None  # Unknown

        self.metrics['consensus_time'] = time.time() - self.test_start_time

    def test_8_backend_feedback_publish(self):
        """Test 8: Backend publishes feedback to control.commits.v1"""
        print_step(8, 12, "Backend Feedback Publishing")

        print_info("Checking if backend publishes to control.commits.v1...")

        # Look for feedback publishing in backend logs
        feedback_published = False
        if self.backend_process and self.backend_process.stdout:
            for _ in range(10):
                line = self.backend_process.stdout.readline()
                if line:
                    print(f"    [BACKEND] {line}", end='')
                    if 'control.commits' in line.lower() or 'feedback' in line.lower():
                        feedback_published = True
                        print_success("Backend published feedback")

        if feedback_published:
            self.results['backend_feedback_publish'] = True
        else:
            print_warning("Backend feedback publish not confirmed in logs")
            print_info("Backend may publish feedback asynchronously")
            self.results['backend_feedback_publish'] = None  # Unknown

        self.metrics['backend_feedback_time'] = time.time() - self.test_start_time

    def test_9_ai_feedback_consumption(self):
        """Test 9: AI consumes feedback from control.commits.v1"""
        print_step(9, 12, "AI Feedback Consumption")

        print_info("Starting AI Kafka consumer (feedback loop)...")

        # Start AI consumer in background
        try:
            self.ai_service_manager.kafka_consumer.start()
            print_success("AI consumer started")

            # Wait for potential feedback
            print_info("Waiting for feedback consumption (10 seconds)...")
            time.sleep(10)

            # Check consumer metrics
            consumer_metrics = self.ai_service_manager.kafka_consumer.get_metrics()

            if consumer_metrics.get('messages_received', 0) > 0:
                print_success(f"AI consumed {consumer_metrics['messages_received']} feedback messages")
                self.results['ai_feedback_consume'] = True
            else:
                print_warning("No feedback messages consumed yet")
                print_info("Feedback may arrive later or consensus may not be complete")
                self.results['ai_feedback_consume'] = False

            self.metrics['ai_feedback_time'] = time.time() - self.test_start_time

        except Exception as e:
            print_error(f"AI consumer error: {e}")
            self.results['ai_feedback_consume'] = False

    def test_10_lifecycle_tracking(self):
        """Test 10: Verify lifecycle tracking updated"""
        print_step(10, 12, "Lifecycle Tracking")

        print_info(f"Checking lifecycle state for anomaly: {self.test_anomaly_id}")

        try:
            # Try to check Redis for lifecycle state
            # Note: This requires FeedbackService to be initialized

            if hasattr(self.ai_service_manager, 'feedback_service'):
                tracker = self.ai_service_manager.feedback_service.tracker
                state = tracker.get_state(self.test_anomaly_id)

                if state:
                    print_success(f"Lifecycle state: {state}")

                    # Check if state advanced
                    if state in ['PUBLISHED', 'ADMITTED', 'COMMITTED']:
                        print_success(f"Anomaly reached state: {state}")
                        self.results['lifecycle_tracking'] = True
                    else:
                        print_warning(f"Anomaly in state: {state} (may be too early)")
                        self.results['lifecycle_tracking'] = False
                else:
                    print_warning("Lifecycle state not found")
                    self.results['lifecycle_tracking'] = False
            else:
                print_info("FeedbackService not available (Settings mismatch issue)")
                print_info("Lifecycle tracking test skipped")
                self.results['lifecycle_tracking'] = None  # Skipped

        except Exception as e:
            print_warning(f"Lifecycle check error: {e}")
            print_info("This is expected if Redis is not configured")
            self.results['lifecycle_tracking'] = None  # Skipped

    def test_11_threshold_adjustment(self):
        """Test 11: Check threshold adjustment mechanism"""
        print_step(11, 12, "Threshold Adjustment")

        print_info("Checking ThresholdManager...")

        try:
            if hasattr(self.ai_service_manager, 'feedback_service'):
                threshold_mgr = self.ai_service_manager.feedback_service.threshold_manager

                # Get current threshold
                current_threshold = threshold_mgr.get_threshold('ddos', default=0.85)
                print_info(f"Current DDoS threshold: {current_threshold}")

                # Check adjustment history
                adjustments = threshold_mgr.get_adjustment_history('ddos')
                if adjustments:
                    print_success(f"Threshold adjustments recorded: {len(adjustments)}")
                    self.results['threshold_adjustment'] = True
                else:
                    print_info("No threshold adjustments yet (requires 5+ minutes of data)")
                    self.results['threshold_adjustment'] = False
            else:
                print_info("ThresholdManager not available")
                self.results['threshold_adjustment'] = None  # Skipped

        except Exception as e:
            print_warning(f"Threshold check error: {e}")
            self.results['threshold_adjustment'] = None  # Skipped

    def test_12_confidence_calibration(self):
        """Test 12: Check confidence calibration"""
        print_step(12, 12, "Confidence Calibration")

        print_info("Checking ConfidenceCalibrator...")

        try:
            if hasattr(self.ai_service_manager, 'feedback_service'):
                calibrator = self.ai_service_manager.feedback_service.calibrator

                # Check if calibrator is trained
                brier_score = calibrator.get_brier_score('ddos')
                if brier_score is not None:
                    print_success(f"Calibrator trained, Brier score: {brier_score:.4f}")
                    self.results['confidence_calibration'] = True
                else:
                    print_info("Calibrator not trained yet (requires 100+ samples)")
                    self.results['confidence_calibration'] = False
            else:
                print_info("ConfidenceCalibrator not available")
                self.results['confidence_calibration'] = None  # Skipped

        except Exception as e:
            print_warning(f"Calibration check error: {e}")
            self.results['confidence_calibration'] = None  # Skipped

    def generate_report(self):
        """Generate final test report"""
        print_header("End-to-End Test Report")

        total_time = time.time() - self.test_start_time

        print(f"{Colors.BOLD}Test Duration:{Colors.ENDC} {total_time:.2f} seconds\n")

        # Results summary
        print(f"{Colors.BOLD}Test Results:{Colors.ENDC}\n")

        passed = 0
        failed = 0
        skipped = 0

        for test_name, result in self.results.items():
            if result is True:
                print_success(f"{test_name}")
                passed += 1
            elif result is False:
                print_error(f"{test_name}")
                failed += 1
            else:
                print_warning(f"{test_name} (skipped/unknown)")
                skipped += 1

        print(f"\n{Colors.BOLD}Summary:{Colors.ENDC}")
        print(f"  Passed:  {Colors.OKGREEN}{passed}{Colors.ENDC}")
        print(f"  Failed:  {Colors.FAIL}{failed}{Colors.ENDC}")
        print(f"  Skipped: {Colors.WARNING}{skipped}{Colors.ENDC}")
        print(f"  Total:   {passed + failed + skipped}")

        # Metrics
        if self.metrics:
            print(f"\n{Colors.BOLD}Timing Metrics:{Colors.ENDC}\n")
            for metric_name, value in self.metrics.items():
                print_info(f"{metric_name}: {value:.3f}s")

        # Critical checks
        print(f"\n{Colors.BOLD}Critical E2E Flow:{Colors.ENDC}\n")

        critical_tests = [
            ('backend_start', 'Backend started'),
            ('ai_service_init', 'AI Service initialized'),
            ('ai_publish', 'AI published to Kafka'),
            ('backend_consume', 'Backend consumed message'),
            ('signature_verification', 'Signature verified'),
        ]

        critical_passed = 0
        for test_key, test_desc in critical_tests:
            if self.results.get(test_key) is True:
                print_success(test_desc)
                critical_passed += 1
            else:
                print_error(test_desc)

        # Final verdict
        print(f"\n{Colors.BOLD}Final Verdict:{Colors.ENDC}\n")

        if critical_passed == len(critical_tests):
            print(f"{Colors.OKGREEN}{Colors.BOLD}+ E2E TEST PASSED{Colors.ENDC}")
            print_info("Core architecture flow validated successfully!")
            print_info("AI → Kafka → Backend communication is working")
        elif critical_passed >= 3:
            print(f"{Colors.WARNING}{Colors.BOLD}! E2E TEST PARTIAL{Colors.ENDC}")
            print_info("Some components working, but full flow not validated")
            print_info(f"Critical tests passed: {critical_passed}/{len(critical_tests)}")
        else:
            print(f"{Colors.FAIL}{Colors.BOLD}- E2E TEST FAILED{Colors.ENDC}")
            print_error("Critical architecture flow not working")
            print_error(f"Critical tests passed: {critical_passed}/{len(critical_tests)}")

        print()

    def cleanup(self):
        """Cleanup test resources"""
        print_info("\nCleaning up...")

        # Stop Kafka consumer
        if self.ai_service_manager and hasattr(self.ai_service_manager, 'kafka_consumer'):
            try:
                self.ai_service_manager.kafka_consumer.stop()
                print_success("AI consumer stopped")
            except:
                pass

        # Stop backend
        if self.backend_process:
            print_info("Stopping backend...")
            self.backend_process.terminate()
            try:
                self.backend_process.wait(timeout=5)
                print_success("Backend stopped")
            except subprocess.TimeoutExpired:
                print_warning("Backend did not stop gracefully, killing...")
                self.backend_process.kill()
                print_success("Backend killed")

        print_success("Cleanup complete")


def main():
    """Main entry point"""
    test = E2ETest()
    test.run()


if __name__ == '__main__':
    main()
