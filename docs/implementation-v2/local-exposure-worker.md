# CPU local exposure worker installation

The existing `photo-worker run` command can install `semantic.local_exposure`
through `PHOTO_LOCAL_EXPOSURE_ENABLED=true`. Default is `false`; disabled startup
performs no exposure ICC/object-store/database initialization. This switch does
not enable SQL operation admission or import a scientific qualification.

Use a provisioned CPU worker (`PHOTO_WORKER_CLASS=cpu`, backend CPU) and the normal
worker DB/AMQP/mTLS gateway/scratch/capacity configuration. Required additional
configuration:

| Variable | Meaning |
| --- | --- |
| PHOTO_ICC_ROOT | Existing verified ICC bundle directory; exposure requires the bundled work_rec2020_gamma24_v1 identity |
| PHOTO_LOCAL_EXPOSURE_MODEL_LOCK_SHA | Lowercase SHA256 of the qualified deployment model lock |
| PHOTO_LOCAL_EXPOSURE_RUNTIME_LOCK_SHA | Lowercase SHA256 of the qualified measured CPU runtime lock |
| PHOTO_LOCAL_EXPOSURE_DEVICE_PROFILE_SHA | Lowercase SHA256 of the qualified measured device profile |
| PHOTO_LOCAL_EXPOSURE_DATASET_SHA | Lowercase SHA256 of the qualification dataset |
| S3_ENDPOINT | HTTPS endpoint without credentials, path, query or fragment; HTTP allowed only in development |
| S3_BUCKET | Allowed bucket containing exact-version qualification reports |
| S3_ACCESS_KEY / S3_SECRET_KEY | Restricted report-reading object-store credentials |
| S3_REGION | Optional object-store region |

These hashes are trusted deployment inputs supplied by the operator from verified
qualification outputs. Startup format validation does not measure/certify the
machine. Never populate them with task-provided values or arbitrary placeholders.
The task's immutable CPU qualification pin must match all four hashes. Each runtime
check validates the live leased SQL evidence, exact-version report bytes, full
policy and post-read evidence. Source/protection documents and binary arrays are
read through the task gateway with producer-origin checks. The object-store
client has bounded connect/read timeouts and no internal retries.

Assembly and exposure may be explicitly installed in the same CPU process.
`run_worker` still checks that provisioned operation registration is covered by
installed executable operations. No registry, worker provisioning, policy or
production migration is changed by constructing this factory. Deployment requires
actual accepted qualification, proper worker registration and normal SQL admission.

Current verification covers CLI composition, startup validation, concrete factory,
leased SQL report/protection discovery and controlled report-byte proof. Physical
CPU task execution through gateway and downstream adoption is not yet verified.

The scheduler service also requires explicit exposure adoption installation.
`run_adopter(..., exposure_working_icc_sha=WORKING_PROFILE_SHA)` composes the
versioned report/SQL authority only when AdopterConfig's verified working ICC pin
matches and its layer scratch root/budget are configured. The same bounded scratch
resource is used for content replay. Default Adopter service has no exposure installation. To enable it through
`photo-worker v2-adopter`, set `PHOTO_ADOPTER_EXPOSURE_ENABLED=true`,
`PHOTO_ICC_ROOT`, `PHOTO_ADOPTER_LAYER_SCRATCH_ROOT` (existing absolute directory)
and `PHOTO_ADOPTER_LAYER_SCRATCH_BYTES` (positive integer at most 2^53-1). A missing
original working ICC or missing paired scratch configuration rejects startup.
The scratch budget must cover the source/output/mask replay requirements for the
admitted image sizes; a small syntactically valid budget does not authorize replay.
The enablement switch creates no policy or scientific qualification; each completed
task still requires live SQL and exact report-byte/content proof.
