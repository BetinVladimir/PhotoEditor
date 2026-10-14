# V27: ordinary semantic create deployment

Status: HTTPServer wiring exists behind explicit operator configuration. Physical deployment decode→Build→authorized project routing→CreateService commit/replay is tested with synthetic qualification/source metadata. A running production HTTP process, real model qualification and native missing-producer initiation are separate acceptance gates.

## Startup

Set `PHOTO_V27_CREATE_CONFIG` to a regular UTF-8 JSON file (maximum 8 MiB), `V27_CREATE_SCHEDULER_DATABASE_URL` to a distinct scheduler connection using actual role `photo_scheduler`, and `PHOTO_ICC_ROOT` to the verified ICC registry directory. The main API pool must use actual role `photo_api`. No startup role escalation or grants are performed.

Absent config leaves the existing unavailable endpoint behavior. Present but invalid/missing dependencies fail startup. `buildSemanticCreate` owns only the newly opened scheduler pool, closes it on installation failure and returns its shutdown callback on success. API pool/object storage stay owned by the main application. Error messages do not expose JSON, credentials or connection strings.

## JSON contract

All named fields are mandatory, including explicit zero operator cost fields. Exact field names/casing are required. Duplicate keys, duplicate projects/check IDs/digests, unknown fields, trailing JSON, null instead of scalar values, unsafe integers and missing fields refuse decoding. Order and insignificant JSON whitespace are immaterial.

Root: `schema_version` = 1; `projects` = 1..4096 project policies. This is an operator allowlist; a project without a policy returns qualification unavailable after authenticated inventory resolution. No default/first policy is selected.

Each project:

- `project_id`: nonzero UUID, unique in this deployment.
- `qualification`: exact SemanticQualificationPin fields `policy_id`, `qualification_id`, `policy_sha`, `report`, `calibration`, `model_lock_sha`, `runtime_lock_sha`, `device_profile_sha`, `dataset_sha`, `issuer_id`, `backend`. Backend is `cuda`. `report` and `calibration` are distinct `{artifact_id, sha256}` refs.
- `expected`: the independent transfer acceptance expectation described below.
- `person`: `{report: {artifact_id, sha256}, expected: ...}` with independent mask.person acceptance expectation.
- `costs`: `{ram_bytes, vram_bytes, scratch_bytes}`; nonnegative safe integers from qualified operator measurements.
- `ram_bytes`, `vram_bytes`, `scratch_bytes`, `output_bytes`, `metadata_bytes`, `reserved_bytes`: positive safe integers. Metadata must not exceed RAM/8. These are deployment ceilings/reservation, not evidence of actual hardware qualification.

Each `expected` contains `operation`, `version`, `backend`, `model_lock_sha`, `runtime_lock_sha`, `device_profile_sha`, `dataset_sha`, `issuer_id`, `required_checks`, `calibration_shas`, `test_fixture_shas`, `evidence_shas`, `license_cleared`. Transfer operation/version/backend are semantic.transfer/1/cuda; person operation/version are mask.person/1, with its independently qualified backend. Issuer must be nonzero and license_cleared true. Each SHA array contains1..256 unique canonical lowercase SHA256 values. Required checks contain1..256 unique nonempty IDs of at most128 bytes. No client-controlled protection refs, tenant identity, ICC override, capability-enabling flag or private key belongs in this file.

## Runtime

`DecodeCreateDeployment` owns all decoded bytes/collections and checks syntactic policy consistency. `CreateDeployment.Build` constructs scoped source/draft/transport publishers from caller-owned dependencies, then `NewProjectCreateService` validates real roles and owns policy maps. The JSON never grants access or qualifies a model by itself.

`ProjectCreateService.Propose` resolves the original inventory under current tenant/session/project authority, then selects the exact policy. `CreateService` repeats installation preflight, deterministic intent publication, independent unique-root protection selection, physical source/person/protection/qualification preparation, complete input transport registration, and commit-aware parent admission. Already admitted requests reconstruct the saved graph and repeat full authority/closure verification before returning the original receipt. Changed body with the same key, revoked session, foreign tenant, absent policy and ambiguous protection refuse without a successful response. Native preparation requires its dedicated producer orchestration; absence is not turned into a queued parent or child receipt.

Capability and live qualification records remain independent SQL gates. Synthetic tests enabling capability in disposable databases do not authorize enabling it in production.

An empty authorized protection producer candidate set returns `DEPENDENCY_UNAVAILABLE`503. Internally it has a dedicated `errTransferProtectionUnavailable` classification wrapping qualification unavailability, reserved for future producer orchestration. Multiple live roots still conflict; corrupt existing documents, invalid pins and failed qualification retain their own refusal paths. They must not be treated as missing or silently replaced. The client's `reference_mask` never substitutes independent protection. The physical HTTP fixture retires only its unique producer root, verifies the empty-set classification and503, restores its exact expiry, confirms no admission effects and then creates normally.

An empty qualified `mask.person` candidate inventory likewise returns503, but only after independently reading and verifying the original source version/document through the scoped catalog. A nonexistent or invalid source must retain its source error rather than be disguised as a missing person producer. Multiple qualified candidates still conflict; no candidate path can authorize bypassing person coverage. The physical HTTP fixture also retires its sole qualified person root, checks503 while ordinary protection remains available, restores its exact expiry and verifies zero parent admission effects before the successful creation. Neither unavailable classification starts a producer automatically or proves that excluded existing evidence can be regenerated.

## Current HTTP verification scope

The `create-service` physical integration fixture starts a loopback HTTP server using production `httpapi.Server.Routes`, project-routed deployment service, real `photo_auth` pool, signed Ed25519 access token and database-backed session. The first admitted parent is created over HTTP. Missing access token, browser request without CSRF, missing idempotency key, invalid semantic body and foreign tenant are refused before admission; SQL confirms zero queue events/admission receipts before the first successful request. The accepted response contains the original queued parent ticket; exact HTTP replay, changed-body conflict and revoked-session refusal are checked. Direct service replay and independent Python verification of pinned MinIO bytes, SQL closure, quota and exact queue effects follow.

This verifies the HTTP handler and ordinary create path, not executable startup through environment configuration, native missing-producer orchestration, actual model quality or whole-module acceptance. Scoped evidence: [v27-create-http-current.json](audit/v27-create-http-current.json).

The physical `create-service` fixture additionally invokes `TestIntegrationSemanticCreateDeployment` in a separate Go process. It writes the exact operator configuration to a private temporary directory under repository `work`, loads the verified repository ICC profile, and calls the production `buildSemanticCreate` using environment variables and an actual scheduler-role connection. Closing the built service must leave the caller's API pool usable. API-role scheduler credentials and malformed configuration must fail installation. The temporary directory is removed by fixture cleanup. Evidence: [v27-create-startup-current.json](audit/v27-create-startup-current.json). This covers the production builder boundary; it does not yet prove the complete `main` executable startup, HTTP serving and graceful shutdown as one process.

The subsequent executable probe builds the actual `cmd/photo` binary in that private repository directory, generates ephemeral Ed25519/auth keys with0600 file permissions, and starts production `main/run` with an explicit isolated environment. It requires `/health/ready`200, then two authenticated native HTTP requests returning the same original queued parent ticket. It sends SIGTERM and requires a successful process exit; cleanup kills/reaps a failed process and deletes only its owned temporary directory. Independent Python checks afterward still require exact single queue event/outbox/admission receipt, original pinned input bytes and quota increments. This proves executable startup, configured service wiring, existing-parent replay and graceful shutdown, not first-parent creation by the executable (first creation is separately exercised through the production handler), native missing-producer initiation or actual inference quality. Evidence: [v27-create-executable-current.json](audit/v27-create-executable-current.json).
