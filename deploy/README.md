# PhotoEditor deployment

`compose.yaml` is the single-node production topology described by the implementation specification. It starts migrations and storage/queue bootstrap as successful one-shot prerequisites, then the API, object gateway, scheduler, publisher, dead-letter and collection consumers, maintenance, CPU/media/archive workers, both frontends and Caddy. `--profile mail` adds SMTP delivery; `--profile cuda` adds one GPU worker. MPS remains a native-host worker and is intentionally not advertised as a Docker capability.

The compose file has no default credentials and no mutable image tags. Copy `env/production.env.example` outside version control, replace every `REPLACE`, and use immutable `name@sha256:digest` images. `PHOTO_SECRETS_DIR` must be an absolute directory whose private keys and JSON key rings are regular files readable only by the service owner. Required layout:

```
auth/keys.json
gateway/ca.crt
gateway/server.crt
gateway/server.key
gateway/worker.crt
gateway/worker.key
gateway/signing.json
mail/keys.json
mail/smtp-password
minio/access
minio/secret
postgres/password
rabbitmq/password
```

The gateway server certificate must contain `object-gateway` in its SAN and be issued by `gateway/ca.crt`. The worker certificate must have client-auth usage. The same certificate may serve several workers on one trusted host, but production hosts should receive separate certificates. Key files and signing/auth key rings must use mode `0600`; CA and public certificates may use `0644`. Key generation and trust distribution are deployment authority actions and are not performed implicitly by containers.

Build arguments are mandatory so a release pipeline must choose reviewed base image digests:

```
docker build -f deploy/images/api/Dockerfile \
  --build-arg GO_BUILD_IMAGE=...@sha256:... \
  --build-arg RUNTIME_IMAGE=...@sha256:... .
docker build -f deploy/images/worker/Dockerfile \
  --build-arg UV_IMAGE=...@sha256:... \
  --build-arg PYTHON_BASE_IMAGE=...@sha256:... .
docker build -f deploy/images/photographer/Dockerfile \
  --build-arg NODE_BUILD_IMAGE=...@sha256:... \
  --build-arg STATIC_IMAGE=...@sha256:... .
docker build -f deploy/images/portal/Dockerfile \
  --build-arg NODE_BUILD_IMAGE=...@sha256:... \
  --build-arg NODE_RUNTIME_IMAGE=...@sha256:... .
```

Validate repository invariants with `make deploy-check`. Validate resolved deployment input before startup with:

```
docker compose --env-file /secure/photo.env -f deploy/compose.yaml config --quiet
docker compose --env-file /secure/photo.env -f deploy/compose.yaml up -d
```

Only Caddy publishes ports. PostgreSQL, RabbitMQ, MinIO, object gateway and service health listeners remain on the internal network. Caddy preserves Host, path and query for the storage endpoint; `/api/*` and `/health/*` route to Go while the remaining application and portal paths route to their frontend containers.

The Compose plan is not release evidence by itself. Before production, build and record image digests, run the capability doctor with the installed model lock, execute backup/restore and target-hardware benchmarks, verify TLS/SMTP from the deployment network, and attach those reports to the release manifest. The repository records those unavailable environment-dependent checks as `blocked_external` until evidence exists.

The API image also contains the one-shot `photo-v2-qualifications` operator command. Provision an enabled row in `photo.v2_qualification_operators` and a matching enabled `photo.v2_qualification_policies` row through DBA change control after checking model licensing, the blind test corpus, issuer and evidence objects. The trusted JSON lock file contains `operation`, `version`, `backend`, `model_lock_sha`, `runtime_lock_sha`, `device_profile_sha`, `dataset_sha`, `issuer_id`, `required_checks`, `calibration_shas`, `test_fixture_shas`, `evidence_shas`, and `license_cleared`; it must be a regular file without group/world write permission. Mount it read-only and run the CLI with the maintenance service's database and S3 environment:

```
docker compose --env-file /secure/photo.env -f deploy/compose.yaml run --rm \
  -v /secure/qualification:/run/qualification:ro \
  --entrypoint /usr/local/bin/photo-v2-qualifications maintenance \
  import --tenant TENANT_UUID --project PROJECT_UUID --operator OPERATOR_UUID \
  --report-artifact REPORT_UUID --sha256 REPORT_SHA256 \
  --locks /run/qualification/locks.json
```

The command returns the immutable qualification ID. After its report is reviewed, a DBA may set `admission_enabled=true` on exactly one enabled policy for the operation/version; the database enforces this uniqueness. Session admission resolves that policy only when the tenant/project has an exact ready `trusted_import` report. The command does not create or enable policies, accept a pass override, or fetch a report from a user-controlled URL.


The scheduler automatically resumes draft V26 render input publication after restart. It requires migration00430 and at least two scheduler database connections (one pinned advisory-lock session and one for bounded SQL work). A separate source-reading pool uses photo_api, four connections and five-second statement timeouts. Set RENDER_SOURCE_DATABASE_URL when source reads use separate database credentials; when empty, DATABASE_URL is reused with SET ROLE photo_api. That login must already be authorized to assume photo_api. No new role memberships are granted by the runtime. Both pools must target the same project database.

Recovery runs independently every five seconds when idle and participates in scheduler readiness/shutdown. It publishes and registers missing input closure documents; it does not qualify render.compose or activate GPU graphs. Qualification gates continue to apply. Candidate rotation uses an in-memory UUID cursor. Migration00431 persists failed-recovery attempts and next-due timestamps: exponential delay starts at five seconds and caps at five minutes; successful input recovery resets it. Scheduler restart preserves the delay. API callers cannot alter these operational fields.


### Optional measured Render calibration catalog

The API accepts `PHOTO_RENDER_CALIBRATION_FILE` (absolute container path) and `PHOTO_RENDER_CALIBRATION_SHA256` together. Both absent preserves existing startup. Any configured missing, nonregular, oversized, noncanonical, changed or unpinned manifest prevents API startup; no contents or credentials are logged. The catalog is read once from a single bounded file descriptor and retained as an immutable server object. Atomic file replacement applies only after API restart. A matching SHA authenticates operator-configured bytes, not actual GPU measurements. Admit only profiles qualified against the exact installed provider/model/runtime/device/dataset/ICC and tested workload envelope. No generated example measurements are supplied.

Use the optional `compose.render-calibration.yaml` overlay with the base topology. Set `PHOTO_RENDER_CALIBRATION_HOST_FILE` to an absolute existing host file and `PHOTO_RENDER_CALIBRATION_SHA256` to the independently reviewed canonical manifest digest. The overlay mounts only that file read-only with create_host_path=false and fails variable substitution if either setting is missing:

```sh
docker compose --env-file /absolute/operator/production.env -f deploy/compose.yaml -f deploy/compose.render-calibration.yaml config --quiet
```

After validating the merged configuration, deploy through the existing release procedure. The base topology does not install a calibration manifest implicitly. This currently wires the catalog into API startup for calibrated Render preparation; public batch submission and worker dispatch/device qualification still require their separate implementation. Detailed manifest keys and budgeting are specified in docs/implementation-v2/15-execution-contracts.md.


For small isolated Lens/Render object-store integration fixtures on a disk-constrained host, `PHOTO_TEST_MINIO_TMPFS=1` explicitly selects a128MiB tmpfs for the fixture container's /data. Default0 retains the repository-owned bind mount. The real versioned MinIO API, SQL authority and creator/recovery processes remain exercised; tmpfs mode does not prove MinIO container/host restart durability and must not be used for that acceptance criterion. Production topology/storage configuration is unaffected. Never prune unrelated Docker data to make these tests pass.

The API now mounts `POST /api/v1/tenants/{t}/projects/{id}/render-batches` with the V26 submission contract and a4MiB route-specific request cap (up to5000 ordered frames). When the complete Render SQL boundary through migration00442 is installed, startup creates a separate photo_scheduler pool (eight connections,30-second statement timeout) using DATABASE_URL and verifies the API pool remains photo_api. The login must already be allowed to assume both roles; the builder grants no memberships. Incomplete SQL installation leaves the route503. The pinned calibration catalog feeds fresh submissions; absence of the optional catalog blocks fresh work while preserving authorized pending/completed replay. This wiring does not enable a renderer or qualify GPU execution. Existing render worker qualification/enablement and empirical device acceptance are still required.

Render submission now also requires migration00441. New calibrated batches atomically freeze the exact model/runtime/backend/device/dataset tuple and complete calibration-profile SHA on parent and every child. A later qualified device/dataset policy switch does not make the old resource estimate valid. New public202 completion requires this pin; completed legacy tickets retain authorized replay, while unpinned pending shells cannot receive a new202 by assuming a current calibration. The startup builder detects the new binder/guard and leaves admission unavailable on incomplete installations. This change does not itself advertise or verify the physical worker's GPU profile.

Migration00442 repeats live trusted qualification at worker recipe admission. A valid registration/lease alone no longer admits an unpinned render run. Current policy, enabled operation, full frozen tuple and ready verified unexpired qualification report are required before composition. The prior admission core is private; the existing worker SQL entrypoint remains unchanged. API startup now requires this helper (Render SQL through00442). This boundary does not verify physical device telemetry or hold locks throughout GPU execution; device routing, adoption checks and empirical acceptance remain required.


Render runtime inventory now includes `render_recomposition_adoption` and `render_adoption_inputs`. Older
RenderRuntimeLock files do not satisfy the complete inventory. Regenerate and
independently qualify the updated build before supplying admission profiles;
local component hashes alone never enable production Render qualification.
The comparison/preparation path is mounted in AdoptionCoordinator with an
operator-configured independent replay context. The standalone process still
needs installed approved complete replay providers; absent replay/scratch
configuration retries without accepting or permanently rejecting a result.
Runtime inventory also pins render_prepare_adoption, adoption_service,
render_replay_reader, render_replay_artifacts and render_profiled_replay. Independent replay receives
only declared input access, and owned NPY materialization uses existing scratch
quotas; no staging or upload interface is exposed by the replay artifacts bridge.
These changes do not alter current image-processing algorithms.


Render adoption replay now requires migration445's scheduler-scoped complete
qualification pin and an identical operator-configured RenderQualificationPin.
External qualification verification receives that full immutable identity.
Device/dataset/calibration mismatch retries without accepting the result.
The runtime inventory also includes render_qualification_pin. This does not
replace physical device verification or empirical qualification.

Render cache publication requires migrations through00447. Startup checks publish_v26_render_cache and guard_v26_render_cache. This enables accepted-result cache metadata publication; actual Render cache lookup/reuse is not yet mounted.

Render candidate lookup requires migrations through00448; startup also requires lookup_v26_render_cache. Rows published before the inventory column was installed are misses. Lookup is scheduler-only and does not authorize worker access or bypass current pinned-byte verification.

Render cache object preparation requires SQL through00449 and startup checks v26_render_cache_artifacts. Read cached objects only via a scheduler autocommit manifest and exact object versions; reserve full unique closure scratch budget before reads. Cache-byte preparation is not yet mounted as automatic task reuse.

Render worker cache authority requires migrations through00450; startup checks v26_render_worker_cache_context and lookup_v26_render_worker_cache. Worker calls are gated by full live lease admission; no direct scheduler cache privileges are granted. Automatic worker reuse remains pending object transport/execution integration.

Render cache gateway transport requires00451. New publications retain immutable original context JCS; older cache rows are gateway misses. Existing task object GET can resolve a current qualified Render cache inventory after the ordinary input path, while retaining mTLS/capability/live-lease/bucket/version controls. Startup also requires v26_render_worker_cache_artifacts. Automatic worker cache orchestration remains pending.

Worker Render cache reuse is now mounted for SQLRenderTaskAdmission after normal runtime/qualification/resource admission. It uses the existing task mTLS object gateway, then normal attempt output uploads, executor commit and independent adoption. Install the complete SQL slice through00451. A missing candidate or insufficient projected scratch continues normal rendering; accepted cache-byte corruption is rejected. Full cross-task SQL/mTLS/S3/GPU acceptance still requires verification.

Render cross-task cache requires00452 and matching worker code. Cache schema2 excludes only task-bound v2_input_manifest; semantic closure entries/count/contracts and all processing/qualification pins remain included. Older schema1 rows are misses for new callers and expire normally. Full task transport is still checked at admission and gateway access.

Versioned Render cache S3 byte checks: run `PHOTO_TEST_MINIO_TMPFS=1 PYTHONPATH=workers:apps/local-editor/src .venv-platform/bin/python -m pytest tests/integration/test_v26_render_cache_objectstore.py -q`. The isolated test verifies old-version reads despite wrong latest versions, deleted-version refusal, real fresh object writes and independent typed recomposition. It uses controlled catalog/gateway adapters, so is not combined production mTLS/SQL commit acceptance. Tmpfs does not test restart durability. After a Docker launch observation timeout, confirm the exact original container is running before using the explicit PHOTO_TEST_EXISTING_MINIO_NAME recovery option for the affected single parameter case; do not create a replacement solely due to timeout.

Render cache lifecycle requires00453: trusted accepted publication can refresh an expired matching content entry without DELETE. Live entries and tenant/key/context identity remain immutable; refresh expiry is at most24 hours and output expiry still bounds it. No additional purge permissions are required.

Render cache repair requires00454. Worker quarantine is bound to the current lease/context and reported result ID; quarantined records are denied by lookup/gateway. Before publication begins, bad cache bytes can fall back to ordinary Render after full admission recheck. A different newly accepted result can replace the quarantined entry; replay of the old result cannot restore it. No metadata DELETE permission is added.

Trusted runtime installation can now call `run_adopter(config, stop, render_replay=qualified_replay, render_max_tile_bytes=tile_budget)`. Replay and tile budget must be paired and require the configured layer scratch root/budget; invalid configuration fails before database/object-store access. This wires the installed dependency to the actual adoption coordinator. The default CLI still needs the approved complete provider installer; supplying a callback does not itself qualify a provider or enable a production model policy.

Trusted worker installation can call `run_worker(config, render_renderer=qualified_renderer)` (or build_worker with the same keyword). This mounts render.compose using the worker's own SQL admission and executor; the renderer backend must match the GPU-class node. Normal database registration and capacity checks still gate consumption. The default CLI still needs the approved complete provider installer. RenderRuntimeLock now seals runtime/service.py and v2/service.py: regenerate the complete inventory and obtain new qualification for the changed runtime SHA before enabling it.

Full ProfiledTaskRenderer task execution now requires the complete primary-scoped input context. Use execute_render_task/SQLRenderTaskAdmission; do not invoke the renderer with a provider-supplied source_result. Normal rendering rechecks current lease through v26_render_worker_cache_context (SQL through00450, complete cache deployment through00454). Worker and replay select the same source ImageResult from ordered primary parents, preserving source generated coverage and review. Regenerate/requalify RenderRuntimeLock after these worker/replay code changes.

### Optional measured Region calibration catalog

Combine `compose.region-calibration.yaml` with the base topology. Set `PHOTO_REGION_CALIBRATION_HOST_FILE` to an absolute existing canonical Region catalog file, `PHOTO_REGION_CALIBRATION_SHA256` to its independently reviewed digest and `PHOTO_REGION_EVIDENCE_SHA256` to the common approved evidence digest for all bundled profiles. The overlay mounts that file read-only without creating missing host paths.

```sh
docker compose --env-file /absolute/operator/production.env -f deploy/compose.yaml -f deploy/compose.region-calibration.yaml config --quiet
```

The API verifies catalog bytes before startup, requires the complete Region migration boundary and constructs separate API/scheduler pools with request-scoped input and publication dependencies. No catalog configuration leaves the Region queue unavailable. SQL policy/report approval and physical worker runtime/model/ICC/device qualification remain mandatory; supplying the overlay does not enable the operation registry. The worker's independent catalog/native provider installation must use the same approved catalog and device qualification. Default native worker installation and complete GPU acceptance remain outstanding.

### Qualified person runtime

For GPU-class model workers, enable the measured person authority with
`PHOTO_PERSON_RUNTIME_ENABLED=true` and operator-approved
`PHOTO_PERSON_MODEL_LOCK_SHA`, `PHOTO_PERSON_RUNTIME_LOCK_SHA`, and
`PHOTO_PERSON_DEVICE_PROFILE_SHA` (lowercase SHA-256). Existing
`PHOTO_MODEL_ROOT`, `PHOTO_MODEL_LOCK`, and `PHOTO_WORKER_BACKEND` select the
installed model and backend. CPU inference can use the GPU-class model worker
with backend `cpu`; this setting does not change scheduler resource classes.

Startup verifies installed person weights and computes runtime/device hashes
before registering operations. A mismatch fails startup. Each pinned person
execution repeats authority checks before input IO, around inference, before
staging and before returning results. Locks must come from independently
qualified measurements for this build/device; copying hashes from an arbitrary
task or report does not approve a runtime. The current measurement includes
native libraries and device configuration, and queries graphics-driver/NVML versions for CUDA. Actual RTX3060
measurements and acceptance benchmarks still require qualification work. Repeated full
weight/library hashing has an unmeasured performance cost.

When disabled, omit all three locks. Legacy unpinned person operations remain
available; qualification-pinned tasks require this authority and are refused
without it. Enabling this worker authority does not enable the public semantic
proposal API or activate an incomplete semantic graph.

### V28 donor CPU worker (configuration wiring; acceptance pending)

`PHOTO_DONOR_ENABLED=true` installs `portrait.open_eyes` on a CPU worker. Set
`PHOTO_DONOR_RUNTIME_LOCK` to a regular canonical runtime lock file and
`PHOTO_DONOR_RUNTIME_SHA` to its externally provisioned SHA-256. The current
runtime component inventory must match; a lock is not a quality qualification.
Set absolute existing directories `PHOTO_DONOR_MASK_SCRATCH_ROOT` and
`PHOTO_DONOR_MATERIAL_SCRATCH_ROOT`, and positive decimal byte limits
`PHOTO_DONOR_MASK_MAX_BYTES` / `PHOTO_DONOR_MATERIAL_MAX_BYTES`, each within the
worker scratch limit; their sum must also fit that limit. These temporary mappings must be included in host disk
capacity planning. `PHOTO_DONOR_ALPHA_MODE` defaults to `require_float32`;
`normalize_uint16` permits the explicit legacy input alpha normalization policy.

Object reads use `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, and
optional `S3_REGION`. HTTPS is required outside development. Provision read-only
access to original versions in the configured bucket; output uploads continue
through the task gateway. Database worker credentials need migration 00877 and
its predecessor donor gates. This configuration does not qualify hardware,
activate drafts, or implement result adoption. Current donor SQL migrations and
full processing/adoption need physical acceptance before production enablement.

Donor adopter CLI (`photo-worker v2-adopter`) uses
`PHOTO_ADOPTER_DONOR_ENABLED=true`, the same `PHOTO_DONOR_RUNTIME_LOCK`,
`PHOTO_DONOR_RUNTIME_SHA`, mask/material scratch settings and alpha policy as the
worker, plus `PHOTO_ADOPTER_DONOR_OUTPUT_SCRATCH_ROOT`,
`PHOTO_ADOPTER_DONOR_OUTPUT_MAX_BYTES`, `PHOTO_ADOPTER_DONOR_RAM_MAX_BYTES`, and
`PHOTO_ADOPTER_DONOR_TOTAL_SCRATCH_BYTES`. The sum of three mapping budgets must
fit the total. Pin a regular canonical policy file with
`PHOTO_ADOPTER_DONOR_POLICY` and `PHOTO_ADOPTER_DONOR_POLICY_SHA`. Its CPU operation
and runtime SHA must match the installed runtime; full policy/report/Calibration
qualification is reverified per task. Provision scheduler DB credentials and
read access to exact object versions. Migrations 00878–00880 require physical
acceptance before enablement; successful configuration loading alone does not
prove atomic adoption or image quality.

The donor adopter RAM limit is a required positive integer deployment budget.
The sealed task RAM estimate must fit this budget before image array IO.
Insufficient RAM configuration leaves the candidate pending for retry; malformed
resource evidence is rejected. This admission does not measure resident memory
or replace target-hardware qualification. Bound concurrent adopter processes
so the sum of their RAM limits fits available host memory.


### Donor Compose overlay

After physical migration and qualification acceptance, combine `compose.yaml`
with `compose.donor.yaml` and enable the `v2` profile. Validate structure without
expanding credentials:

```sh
docker compose -f deploy/compose.yaml -f deploy/compose.donor.yaml --profile v2 config --no-interpolate --quiet
```

The overlay requires `PHOTO_DONOR_RUNTIME_HOST_FILE`,
`PHOTO_ADOPTER_DONOR_POLICY_HOST_FILE`, `PHOTO_DONOR_WORKER_SCRATCH_HOST_DIR`
and `PHOTO_DONOR_ADOPTER_SCRATCH_HOST_DIR`. Provision absolute existing paths;
use distinct writable scratch directories owned by the container user. Bind
mounts refuse automatic creation; runtime/policy files are read-only. Mask,
material and adopter output mappings use `/scratch/donor` in their separate
containers and retain unique temporary filenames. Provision free disk for the
combined configured budgets plus the base worker output scratch.

Set the SHA and byte-budget variables documented above. Provide worker
original-version reader credentials through `PHOTO_DONOR_READER_ACCESS_KEY`
and `PHOTO_DONOR_READER_SECRET_KEY`; gateway continues to own output upload.
Adopter uses existing S3 credentials and a scheduler connection provided as
`PHOTO_DONOR_SCHEDULER_DATABASE_URL`. Keep that value in operator secret
configuration. Validate aggregate RAM against all concurrently running services.
Compose validation does not start containers, apply migrations or demonstrate
processing quality. The base Compose file leaves donor disabled.

### V30 delivery creation

Set `V30_DELIVERY_SCHEDULER_DATABASE_URL` on the API service to enable fresh
`POST /api/v1/tenants/{t}/delivery-packages` preparation. This is a separate
credential able to select `photo_scheduler`; the existing API pool remains
`photo_api`. Startup opens at most four scheduler connections, with a 30 second
statement timeout, and checks the expected role and selected SECURITY DEFINER
entrypoints owned by `photo_fn_owner`. Configured but unavailable installation
fails startup. It never grants privileges or applies migrations.

Use the same migrated database as the API, object bucket and V26 freezer. The
required creation/session-recovery migrations through `01301` must be applied
by the deployment migration job. Creation composes the existing V26 frozen
recipe publisher, V30 immutable document publisher and scheduler input closure
registration. Object IO occurs outside SQL transactions. Prepare budgets are
8 GiB RAM, 8 GiB scratch and 8 MiB output; the CPU preparation task requests no
GPU memory. These budgets do not change image processing or GPU test limits.

Without this variable the API remains available for authorized completed
creation replay, but fresh creation returns unavailable. Configure the existing
V30 scheduler expansion and worker/adopter runtimes as well; this setting only
wires the initial creation pipeline and does not enable derivative execution.
The wiring has not yet passed the deferred SQL/storage/runtime test phase.


### V30 interchange import configuration

`V30_INTERCHANGE_SCHEDULER_DATABASE_URL` enables the separate `photo_scheduler` pool for import transport publication/registration. API keeps `photo_api`; startup verifies selected SECURITY DEFINER function ownership/execute privileges and refuses incomplete configured installations. No runtime migrations/grants are performed. Absent configuration preserves authorized completed-ticket replay; fresh preparation returns INTERCHANGE_UNAVAILABLE. Initial limits: CPU only, 2 GiB RAM, 8 GiB scratch, 2 GiB output cap, 1 GiB expanded ZIP, 30,000 files. Archives also must fit scratch together with expanded output. GPU VRAM use is zero. Current implementation accepts project-linked ready photoeditor_bundle archives; new upload, export formats, production parser/adoption and complete review flow remain under implementation. Migrations/tests have not been run in the implementation-only phase.

`PHOTO_INTERCHANGE_ENABLED=true` opts a CPU worker (`resource_class=cpu`, backend `cpu`) into `v2.interchange.parse`. Default is false; strict true/false values only. Worker uses its own `interchange_parse_context` SQL authority before/after IO. Explicit custom ParseInstallation and environment enablement cannot be combined. No GPU is required for ZIP parsing. This registration does not complete import adoption: independent physical result reconstruction, canonical UUID remapping, proposal creation/review and dedicated adoption commit remain outstanding. Do not treat parser registration as end-to-end import readiness.


### Explicit RAW development qualification

`PHOTO_RAW_QUALIFICATION_FILE` optionally points to an absolute operator-provisioned JSON file (regular, no symlink, no group/other write, <=1MiB). Without it, explicit `raw_development` requests fail with QUALIFICATION_UNAVAILABLE; legacy uploads retain historical decoder defaults. It is not a public request field.

File shape: `schema_version:1`, `report_id` (nonzero UUID), `report_sha` (SHA256 of report bytes), `decoder_version` (`rawpy-X.Y.Z/libraw-X.Y.Z`), and `locks` (the complete existing DeploymentLocks object, including operation/version/backend, model/runtime/device/dataset SHA pins, issuer, required checks, calibration/test/evidence SHA arrays and license clearance). Operation is exactly asset.decode/1/cpu. Provision decoder_version only from the installed decoder build covered by runtime_lock_sha. Never derive expected pins from the report being admitted.

Apply01618/01619/01620 before enabling. Import the real camera/ICC/original-preservation qualification report through trusted operator tooling and provision its matching enabled SQL qualification policy. API startup checks access to v25_raw_qualification_object; every owner admission/replay reads the versioned report with current project/session authorization in the admission transaction. Config loading alone does not certify native quality. No fabricated report or default version is installed. Device explicit RAW uses a distinct SQL qualification lookup with current credential epoch and locked open shoot session/collection; user capability selection UI uses the project catalogue. Native camera and full lifecycle acceptance remain required.
Storage bootstrap installs exact bucket CORS origins from `APP_ORIGIN` and `PORTAL_ORIGIN`, permits browser `PUT` (and read-only `GET`/`HEAD`), and exposes `ETag` for resumable multipart uploads. Production origins must be HTTPS; local HTTP requires development loopback. Bootstrap reads the installed CORS policy back and fails if the origin list or `ETag` exposure differs. Run `bootstrap-storage` after changing either origin, before serving archive uploads.

Set `API_PUBLIC_ORIGIN` to the exact externally reachable HTTPS origin of the API. V26 selected-preview capability URLs use this value; `APP_ORIGIN` remains the client application's origin. Production API startup refuses a missing or unsafe `API_PUBLIC_ORIGIN`.
## Локальные предупреждения о ёмкости V32

После установки миграций V32 и задания `PHOTO_V32_SCHEDULER_POLICY_SHA256` scheduler можно запустить с `PHOTO_V32_LOCAL_SCALE_ALERTS=true`. Он каждые 10 секунд читает агрегаты CPU/GPU/signer из существующих бронирований и квалифицированной очереди; предупреждение появляется после непрерывного превышения порога и снимается при восстановлении. Режим только сообщает о давлении на локальный узел и не создаёт облачные машины. Для signer требуется отдельная ёмкость: смешанный signer/general CPU-узел отклоняется как недостоверный источник utilization.


### Dedicated remote RTX worker

`compose.rtx-worker.yaml` starts only the GPU worker on the dedicated RTX server. It connects to existing PostgreSQL, RabbitMQ and HTTPS object gateway endpoints through the deployment's private network. It does not publish ports or start a second database/scheduler. Use `env/rtx-worker.env.example` as input; provision a unique worker UUID, database principal, queue identity and client certificate bound to the same registered node/worker. The gateway certificate SAN must match the configured remote hostname. PostgreSQL uses server verification; place its trusted CA at `postgres-ca.crt` inside the mounted TLS directory. The container explicitly sets `PGSSLROOTCERT=/run/photo/tls/postgres-ca.crt`; this CA may differ from the gateway CA.

The operator installation environment file carries the enabled operation flags, exact runtime/model/qualification pins and any required object-store credentials. Keep real secrets outside the repository. Model weights and TLS files are read-only mounts; missing paths are refused. Fresh named scratch volumes inherit UID/GID 10001 from the worker image. Existing volumes or host-mounted scratch directories must already be writable by UID 10001; startup must not change ownership of an existing installation.

The image must be immutable and qualified for the actual NVIDIA driver, CUDA runtime, PyTorch and required ONNX CUDA providers. The repository's generic worker image currently installs CPU `onnxruntime` through the media extra; it is not sufficient evidence for CUDA ONNX operations. `images/worker/Dockerfile.cuda` installs the frozen shared environment and replaces CPU ONNX with the exact Linux x86_64 CPython 3.11 GPU wheel in `workers/requirements-cuda.txt`, verified by SHA256. It checks dependency consistency and provider installation during the future image build. Supply immutable `UV_IMAGE` and `CUDA_PYTHON_BASE_IMAGE` digests; the latter must provide Python 3.11, glibc >=2.28, CUDA 12 and cuDNN 9. ONNX stable GPU uses CUDA 12 ([official installation documentation](https://onnxruntime.ai/docs/install/)); the Torch CUDA libraries in `uv.lock` do not replace this base contract. Selection and qualification of that base digest, image build and actual CUDA model sessions remain open release gates. No package/provider fallback is allowed. Do not run `uv sync` inside the final image: it would restore the CPU ONNX distribution.

After completing coding, validate the resolved configuration and execute startup, tenant registration, qualification import, lease, actual photo operation, recovery and download on the target server. The decimal 10 GB CUDA allocator/admission limit is enforced by worker startup; real provider peaks and external CUDA allocations still require measurement. Metal continues to run natively with its separate capacity policy.


`provision-rtx-worker.sql` is an operator-only, transactional psql template for the initial tenant registration. Supply every named variable explicitly and create the dedicated nonprivileged LOGIN with `photo_worker` membership separately. The template refuses existing worker/principal identities and node conflicts, pins model and certificate SHA256 values, and creates a disabled node with a decimal 10 GB VRAM budget and a disabled CUDA GPU-class worker. It does not create qualification, resource slots or enable dispatch. Actual doctor/model/operation qualification and activation remain separate installation gates; never run the template using application or worker credentials. Additional tenants require an explicit enrollment workflow rather than rerunning this first-registration template.


Before initial RTX registration, install `configs/capacity-rtx3060.json` with the existing operator command `photo-worker capacity-policy-install` using the operator connection configured by that command. Supply its returned canonical SHA as `capacity_policy_sha` to the registration template. The template requires that exact immutable policy in `photo.capacity_policy_timings`, one concurrent GPU task, decimal 10 GB GPU budget, and a node RAM budget equal to the policy's photo RAM budget. It stores the SHA on the worker registration so startup timing checks and capacity heartbeats use the same policy as the mounted file. A missing or mismatched policy aborts the entire registration transaction.


The Python/CUDA base can be built from `images/worker/Dockerfile.cuda-base`. Supply immutable `UV_IMAGE` and `CUDA_RUNTIME_IMAGE` digests; the NVIDIA runtime input must contain CUDA 12, cuDNN 9 and Ubuntu 24.04 for Linux amd64. The recipe installs managed CPython 3.11.14 using the pinned uv build's interpreter download metadata, makes it readable to the nonroot worker, and checks that CUDA/cuDNN shared libraries load without creating a GPU context. Subsequent builds cannot download a different Python interpreter. Publish and record the resulting base digest as `CUDA_PYTHON_BASE_IMAGE` for `Dockerfile.cuda`. Managed Python behavior follows [uv's Python installation documentation](https://docs.astral.sh/uv/guides/install-python/). Input/output image digests, interpreter archive provenance, host driver compatibility and actual ONNX/Torch interoperability must be recorded during release qualification; no target image has been built or qualified by authoring these recipes.


`activate-rtx-worker.sql` completes **initial** operator activation after reviewing the target-host doctor report and operation qualification. Supply the exact doctor report file bytes and their SHA256, pinned registration identity, and a reviewed operation allowlist shaped as `{"operation.name":["cuda"]}`. The script does not infer operation quality from model smoke results: independent quality/policy gates remain required. It checks the report's backend/model lock and passing locked models for model-backed registry entries, rejects identity changes, existing slots, shared nodes and drained/running installations, and creates one GPU slot. Activation starts with zero available capacity and stale heartbeat fields; only the authenticated worker can refresh readiness. Report integrity here is a trusted operator file pin, not a signature or independent quality certification. Existing deployments and additional tenant enrollment require their own workflow; this template deliberately refuses to overwrite them.

The migrate role now checks embedded bundle filenames, unique versions and Up-section structure before SQL application; legacy absent/comment-only Down sections are accepted and the production command exposes only Goose Up and checks final SECURITY DEFINER ownership, fixed search paths and PUBLIC execution denial afterward under a bounded deadline. A failed final security check leaves already committed forward-only migrations applied and does not authorize destructive rollback; preserve the database and add a reviewed forward-only correction. Fresh/upgrade and role scenarios are authored but remain unexecuted.


Runtime database identity isolation (P0.6.03)

All application pool constructors attest each new physical connection after startup
`SET ROLE` and any existing `AfterConnect` initializer. The selected role must
match the constructor exactly. Both selected role and authenticated `session_user`
must be non-superuser, without BYPASSRLS and without membership in any superuser,
BYPASSRLS role or `photo_fn_owner`. A migration/admin DSN is rejected even if
`SET ROLE photo_api` succeeds. Use separate operator-provisioned nonprivileged LOGIN
credentials with membership only in required service roles; never give runtime
logins `photo_fn_owner` membership. API auxiliary scheduler/admission pools require
those explicit memberships; worker credentials must match their registered worker
principal. `DATABASE_URL`, `AUTH_DATABASE_URL`, and donor/RTX URLs must follow this
rule. Migration/admin credentials are used only by their operator commands.
No runtime LOGIN passwords or additional grants are generated by migrations.
Credential provisioning and actual fresh/upgrade role qualification remain deployment
work, not a claimed completed test.

Owner/DDL privilege separation: connection guards also refuse CREATEROLE and
REPLICATION attributes or memberships; `photo_runtime_owner`, every actual photo
schema/relation owner and every photo SECURITY DEFINER function owner are protected
by catalog ownership checks. Schema CREATE is forbidden for both runtime identities.
The owner checks use transitive membership, independently of INHERIT. Authored
transactional PostgreSQL scenarios cover runtime-owner/function-owner membership,
CREATEROLE, REPLICATION, schema CREATE, schema ownership, table ownership and
SECURITY DEFINER ownership; these scenarios have not been executed.

RTX immutable installation launcher (P0.2.06)

`rtx_installation.py` connects the two existing Docker recipes and the dedicated
Compose deployment through a single operator-reviewed image lock. Copy
`env/rtx-images.lock.example.json` outside the repository. Its null values mean
**unknown**, not an approved image. No NVIDIA, uv, Python/CUDA base or worker
registry digest has been verified locally during coding. Record reviewed
`repository@sha256:<64 lowercase hex>` values before each corresponding phase.
The runtime contract is Linux amd64, CPython **3.11.14**, CUDA12/cuDNN9.
Both CUDA Docker recipes refuse mutable image arguments; the worker refuses an
incorrect interpreter patch and cannot download another managed Python.

The CUDA worker image explicitly selects LCMS from the frozen Pillow wheel rather
than relying on a system library in the NVIDIA base. Its installer checks the
wheel RECORD hash and size, then exercises the production LCMS API before writing
`/opt/photo/native/lcms-manifest.json`. `LCMS2_LIBRARY` points at that verified
binary. Retain the manifest with image evidence; this build check is not a
photographic quality qualification. Existing default ICC discovery and historical
processing policies remain unchanged.

The launcher prints argument arrays by default and runs nothing. After coding
and release approval, the operator may use its explicit `--execute` switch:

```text
python3 deploy/rtx_installation.py base --lock /srv/photoeditor/rtx/images.json --output-tag registry.example/photo/cuda-python:release --metadata-file /srv/photoeditor/rtx/base-build.json
python3 deploy/rtx_installation.py worker --lock /srv/photoeditor/rtx/images.json --output-tag registry.example/photo/worker:release --metadata-file /srv/photoeditor/rtx/worker-build.json
python3 deploy/rtx_installation.py start --lock /srv/photoeditor/rtx/images.json --env-file /srv/photoeditor/rtx/worker.env
```

Build phases publish provenance and Docker metadata; review the resulting registry
manifest digest and record it as `cuda_python_base_image`, then `worker_image`
respectively. A local image ID is not a registry manifest digest. Never substitute
an unreviewed tag. `start` overrides the Compose image with the immutable lock;
operation/model/runtime installation settings remain in the existing mounted
`PHOTO_RTX_INSTALLATION_ENV_FILE` and are independently checked at startup.

Registration order remains capacity-policy-install → provision-rtx-worker.sql
(disabled node/worker) → real target-host doctor and operation qualification →
activate-rtx-worker.sql → authenticated heartbeat/dispatch. Provisioning and
activation now reject replication/privileged memberships and schema CREATE that
would fail the production connection guard, including privileges added between
those phases. These operator scripts never create passwords, qualification or
ready capacity. CUDA remains capped at **10 000 000 000 bytes**; Metal retains its
native availability policy without this artificial cap. Installation, builds,
network access and physical qualification have not been performed here.
### Advanced personal learning installation

Set `V23_TRAINING_SCHEDULER_DATABASE_URL` on the API service only when installing
the qualified personal-learning admission pipeline. Use a dedicated scheduler
credential with the existing `photo_scheduler` role; the API database URL is not
used as a fallback. Empty configuration leaves this optional pipeline unavailable
and preserves baseline style training. Explicit configuration requires the complete
SQL installation and valid dependencies, with startup bounded to15 seconds.
Qualified fit/evaluate workers and operator promotion/safety rules remain required.
