SHELL := /bin/bash
ROOT := $(abspath .)
PYTHON := $(ROOT)/.venv-platform/bin/python
RUN := ./scripts/run-platform.sh
.PHONY: sqlc dev-infra dev-infra-stop setup-platform setup-ml contracts contracts-check v2-check test test-unit test-integration test-fault test-security test-web test-native benchmark test-restore test-visual test-hardware test-legacy test-python test-go test-db deploy-check release-evidence release-check
dev-infra:
	$(RUN) python3 scripts/dev-infra.py up
dev-infra-stop:
	$(RUN) python3 scripts/dev-infra.py down
setup-ml: setup-platform
setup-platform:
	$(RUN) uv sync --project workers --extra dev --extra ml --extra media --python 3.11
sqlc:
	$(RUN) scripts/generate-sqlc.sh
contracts:
	$(RUN) $(PYTHON) scripts/generate-contracts.py
	$(RUN) $(PYTHON) scripts/generate-db.py
	$(RUN) $(PYTHON) scripts/generate-db-constraints.py
	$(RUN) scripts/generate-sqlc.sh
contracts-check:
	$(RUN) $(PYTHON) scripts/generate-contracts.py --check
	$(RUN) $(PYTHON) scripts/generate-db.py --check
	$(RUN) $(PYTHON) scripts/generate-db-constraints.py --check
	$(RUN) scripts/generate-sqlc.sh --check
	$(RUN) $(PYTHON) scripts/check-legacy.py
v2-check:
	$(PYTHON) scripts/build-v2-migration.py --check
	$(PYTHON) scripts/build-v22-schema.py --check
	$(PYTHON) scripts/build-v22-gc-purge.py --check
	$(PYTHON) scripts/build-v22-migration.py --check
	$(PYTHON) scripts/build-v23-schema.py --check
	$(PYTHON) scripts/build-v23-migration.py --check
	$(PYTHON) docs/implementation-v2/check-docs.py
test: contracts-check v2-check test-legacy test-python test-go
test-unit: contracts-check v2-check test-legacy test-python test-go
test-integration: test-db
test-fault:
	$(RUN) $(PYTHON) -m pytest tests/integration/test_task_leases.py tests/integration/test_dead_letters.py tests/integration/test_generation_preview_continuation.py tests/integration/test_media_retry_intake.py -q -p no:cacheprovider
test-security:
	$(RUN) $(PYTHON) -m pytest tests/platform/test_gateway.py tests/platform/test_capabilities_migration.py tests/platform/test_tenant_lifecycle_migration.py tests/platform/test_tenant_cleanup_migration.py -q -p no:cacheprovider
	$(RUN) $(PYTHON) -m pytest tests/integration/test_database.py tests/integration/test_admission.py tests/integration/test_gateway_inputs.py tests/integration/test_gateway_outputs.py tests/integration/test_portal_item_download.py -q -p no:cacheprovider
	$(RUN) go -C backend test ./internal/auth ./internal/gateway ./internal/httpapi ./internal/objectstore ./internal/tenant
test-web:
	cd apps/portal && npm test -- --run && npm run build
	cd apps/photographer && npm test -- --run && npm run typecheck && npx expo export --platform web
test-native:
	cd apps/photographer && npm test -- --run && npm run typecheck
	$(RUN) $(PYTHON) scripts/verification-gate.py --gate native $(if $(NATIVE_EVIDENCE),--evidence $(NATIVE_EVIDENCE),)
benchmark:
	@test -n "$(PROFILE)" || (echo "PROFILE is required" >&2; exit 2)
	$(RUN) $(PYTHON) scripts/verification-gate.py --gate benchmark --profile $(PROFILE) $(if $(BENCHMARK_EVIDENCE),--evidence $(BENCHMARK_EVIDENCE),)
test-restore:
	$(RUN) $(PYTHON) scripts/verification-gate.py --gate restore $(if $(RESTORE_EVIDENCE),--evidence $(RESTORE_EVIDENCE),)
test-visual:
	$(RUN) $(PYTHON) scripts/verification-gate.py --gate visual $(if $(VISUAL_EVIDENCE),--evidence $(VISUAL_EVIDENCE),)
test-hardware:
	$(RUN) $(PYTHON) scripts/verification-gate.py --gate hardware $(if $(HARDWARE_EVIDENCE),--evidence $(HARDWARE_EVIDENCE),)
test-legacy:
	$(RUN) .venv/bin/python -m pytest tests/test_pipeline.py -q -p no:cacheprovider
test-python:
	$(RUN) $(PYTHON) -m pytest tests/platform -q -p no:cacheprovider
	PYTHONPATH=apps/studio-agent $(PYTHON) -m unittest discover -s apps/studio-agent/tests
test-go:
	$(RUN) go -C backend test ./...
test-db:
	$(RUN) $(PYTHON) -m pytest tests/integration -q -p no:cacheprovider
deploy-check:
	$(RUN) $(PYTHON) -m pytest tests/platform/test_deploy_topology.py -q -p no:cacheprovider
	$(RUN) go -C backend test ./cmd/photo-healthcheck
release-evidence:
	$(RUN) $(PYTHON) scripts/release_manifest.py
release-check:
	$(RUN) $(PYTHON) scripts/release_manifest.py --check

.PHONY: build-minio
build-minio:
	$(RUN) python3 scripts/build-minio.py

.PHONY: check-fitting-backends
check-fitting-backends:
	$(RUN) $(PYTHON) scripts/check-fitting-backends.py

.PHONY: test-zip64-large-io
test-zip64-large-io:
	PHOTO_RUN_ZIP64_LARGE_IO=1 PYTHONPATH=$(ROOT)/workers:$(ROOT)/apps/local-editor/src $(RUN) $(PYTHON) -m pytest tests/integration/test_v25_zip64_large_io.py -q -s

.PHONY: test-s3-large-io
test-s3-large-io:
	PHOTO_RUN_S3_LARGE_IO=1 PYTHONPATH=$(ROOT)/workers:$(ROOT)/apps/local-editor/src $(RUN) $(PYTHON) -m pytest tests/integration/test_v25_s3_large_io.py -q -s

.PHONY: test-gateway-large-io
test-gateway-large-io:
	PHOTO_RUN_GATEWAY_LARGE_IO=1 PYTHONPATH=$(ROOT)/workers:$(ROOT)/apps/local-editor/src $(RUN) $(PYTHON) -m pytest tests/integration/test_v25_gateway_large_io.py -q -s

.PHONY: test-gateway-wire-large-io
test-gateway-wire-large-io:
	PHOTO_RUN_GATEWAY_WIRE_LARGE_IO=1 PYTHONPATH=$(ROOT)/workers:$(ROOT)/apps/local-editor/src $(RUN) $(PYTHON) -m pytest tests/integration/test_v25_gateway_wire.py -q -s
