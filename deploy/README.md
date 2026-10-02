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
