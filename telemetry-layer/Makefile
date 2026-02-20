SHELL := /bin/sh
.PHONY: help pcap-e2e pcap-e2e-integration edge-e2e syn-e2e ai-ingest-e2e deepflow-pcap-e2e gate-deepflow-pcap phase3-e2e realistic-step1 realistic-step2 realistic-step3 realistic-step4 realistic-all validate-core validate-full phase5-validate phase5-validate-full

help:
	@echo "Targets:"
	@echo "  pcap-e2e             Run PCAP storage E2E (requires Kafka + Schema Registry + S3/MinIO)"
	@echo "  pcap-e2e-integration Run Go integration test (requires MinIO env)"
	@echo "  edge-e2e             Run edge feature routing E2E (requires Kafka)"
	@echo "  syn-e2e              Run full SYN E2E (flow -> agg -> features)"
	@echo "  ai-ingest-e2e        Run AI ingest check after SYN E2E"
	@echo "  deepflow-pcap-e2e    Run deepflow + AI + PCAP E2E (dry-run path)"
	@echo "  gate-deepflow-pcap   Run deepflow + PCAP gate validation"
	@echo "  phase3-e2e           Alias for deepflow-pcap-e2e (legacy)"
	@echo "  realistic-step1      Realistic Step 1 (flows -> features)"
	@echo "  realistic-step2      Realistic Step 2 (gateway IPFIX UDP)"
	@echo "  realistic-step3      Realistic Step 3 (Suricata deepflow)"
	@echo "  realistic-step4      Realistic Step 4 (PCAP request/result dry-run)"
	@echo "  realistic-all        Realistic Steps 1-4 (full run)"
	@echo "  validate-core        Run unified validation (core required scenarios)"
	@echo "  validate-full        Run full profile including optional dataplane gates"
	@echo "  phase5-validate      Alias for validate-core (legacy)"
	@echo "  phase5-validate-full Alias for validate-full (legacy)"

pcap-e2e:
	@python scripts/e2e_pcap_storage.py

pcap-e2e-integration:
	@cd pcap-service && go test -tags=integration ./...

edge-e2e:
	@python scripts/e2e_edge_feature_routing.py

syn-e2e:
	@python scripts/e2e_full_syn.py

ai-ingest-e2e:
	@python scripts/e2e_ai_ingest.py

deepflow-pcap-e2e:
	@python scripts/e2e_phase3_full.py

gate-deepflow-pcap:
	@python scripts/gates/gate_phase3_deepflow_pcap.py --env env/integration_test.env --timeout-sec 90

phase3-e2e: deepflow-pcap-e2e

realistic-step1:
	@python scripts/e2e_realistic_steps_1_4.py --step 1 --coverage-edge-case --ingest-mode kafka --dataset-set minimal --max-lines 10

realistic-step2:
	@python scripts/e2e_realistic_steps_1_4.py --step 2

realistic-step3:
	@python scripts/e2e_realistic_steps_1_4.py --step 3

realistic-step4:
	@python scripts/e2e_realistic_steps_1_4.py --step 4

realistic-all:
	@python scripts/e2e_realistic_steps_1_4.py --step all --coverage-edge-case --ingest-mode kafka

validate-core:
	@python scripts/phase5_validation_runner.py --profile core --env ../env/integration_test.env

validate-full:
	@python scripts/phase5_validation_runner.py --profile full --env ../env/integration_test.env

phase5-validate: validate-core

phase5-validate-full: validate-full
