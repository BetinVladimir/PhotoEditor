# V30 API metadata source preparation installation

Status: authored; tests/build/process/SQL/object-store checks NOT RUN. API startup now installs the preparer only under explicit opt-in and checks API SQL entrypoints. Parent/child orchestration is authored; full end-to-end qualification is outstanding. No deployment flag has been enabled by this work.

`delivery.MetadataSourceFromEnvironment` returns an owned `PythonMetadataSourcePreparer` only when `PHOTO_DELIVERY_METADATA_SOURCE_ENABLED=true`. Empty or `false` disables it. Other values fail configuration.

For a RAW original, the Go preparer refuses before starting Python unless both the absolute ExifTool path and its SHA-256 are installed. JPEG/PNG/TIFF preparation remains available without those optional RAW settings.

Required settings:

| Setting | Contract |
|---|---|
| PHOTO_DELIVERY_METADATA_SOURCE_PYTHON | Absolute clean Python executable path |
| PHOTO_DELIVERY_METADATA_SOURCE_WORKER_ROOT | Absolute clean directory containing photo_worker |
| PHOTO_DELIVERY_METADATA_SOURCE_TIMEOUT_SECONDS | Optional decimal integer 1–600; default 600 |
| PHOTO_DELIVERY_METADATA_SOURCE_DATABASE_URL | Optional dedicated extraction DSN; takes precedence over DATABASE_URL and is passed to Python as DATABASE_URL |
| DATABASE_URL | Fallback extraction DSN when no dedicated value is configured; connection must permit SET ROLE photo_api and Python requires current_user photo_api |
| S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY | Explicit object-store configuration |
| APP_ENV | Optional environment mode |
| PHOTO_METADATA_SOURCE_SCRATCH_ROOT | Existing absolute resolved scratch directory; Python rejects symlinks |
| PHOTO_METADATA_SOURCE_MAX_BYTES | Integer 1–2147483648 |
| PHOTO_METADATA_SOURCE_RAM_BYTES | At least 268435456 + 4 × MAX_BYTES |
| PHOTO_METADATA_SOURCE_SCRATCH_BYTES | At least MAX_BYTES + 8388608 |
| PHOTO_METADATA_SOURCE_EXIFTOOL | Optional absolute clean path to the qualified RAW ExifTool executable; required for RAW originals |
| PHOTO_METADATA_SOURCE_EXIFTOOL_SHA256 | Optional lowercase SHA-256 of that exact executable; must be configured together with its path |

The subprocess inherits no ambient credentials or Python search path. Its working directory and PYTHONPATH are the explicit worker root. It receives canonical scope, deterministic metadata ID, policy, sorted explicit approval list and timeout on stdin. The source job independently checks live API/session authority, immutable object version/hash, original MIME and size before and after extraction. No source publication is written by the subprocess.

Go limits input to 8192 bytes, publication to 1 MiB and outer response to 6 MiB + 1024 bytes to accommodate JSON string escaping. It verifies the canonical response and recompiles the publication against external scope, metadata identity, policy and approval. A successful process is preparation evidence only; API source reservation/completion still performs current authorization and immutable publication separately.

Resource values are admission floors, not OS-enforced memory reservations. Real resource qualification and deadline/cancellation behavior remain for the later deep-testing phase. This CPU extraction path does not allocate GPU memory.

RAW source preparation accepts only DNG, CR2, NEF, ARW, RAF, ORF and RW2 originals whose physical ExifTool `FileType` matches the authoritative MIME. Before execution and after completion, the worker hashes the configured single-link regular executable (maximum 16 MiB) and compares its file identity with the installed path; symlinked paths and a missing or changed digest fail closed. This pins the entry executable, not ExifTool's Perl modules or native dependencies. An operator must qualify and freeze that dependency set and camera RAW fixtures before enabling RAW metadata extraction. GPS remains opt-in and only the explicit allowlist enters the normalized metadata document.

The RAW command requests only explicit EXIF/GPS tags but performs a complete file scan. An early-stop mode could miss allowlisted fields in some camera RAW layouts. Process time, captured output and original size remain bounded; the camera qualification matrix must confirm each permitted field on the supported RAW formats.

The operator-run matrix is `python -m photo_worker.v2.delivery_metadata_raw_qualification --root /absolute/fixtures --manifest /absolute/matrix.json --exiftool /absolute/exiftool --exiftool-sha256 <64 lowercase hex>`. The canonical manifest has `schema_version:1` and exactly seven `fixtures`, one per permitted MIME. Each entry has a basename `filename`, authoritative `mime`, `original_sha256`, `original_bytes`, and `expected_with_gps` conforming to `MetadataDocument`. At least one fixture must contain real EXIF GPS. The runner reads every SHA-pinned original twice with GPS disabled/enabled, compares the complete normalized document with expected values, and emits canonical local evidence containing the manifest/tool hashes and per-format metadata digests. The output is not a signed capability report or proof of camera provenance; operator review, actual fixture authenticity and physical qualification remain required.

The dedicated DSN allows metadata extraction credentials to be managed independently of the main service connection. An invalid dedicated value fails installation rather than silently falling back. Disabled mode does not read credentials. Neither DSN is included in configuration errors. The Python process still forces role photo_api and verifies current_user; configuring a DSN does not prove connectivity or grants.

## Extraction concurrency

All PythonMetadataSourcePreparer instances in one API process share one extraction permit. Waiting is cancellable and counts toward the configured 1–600 second timeout; a request that expires in the queue cannot launch extraction. The permit is returned after subprocess completion, failure or cancellation, including WaitDelay handling. This bounds extraction process concurrency per API process, not cluster-wide. Multiple API replicas require separate RAM/scratch budgets or a future distributed admission controller. It does not reserve memory in the operating system.

## Retry publication authority

Startup checks all six API function signatures including read_v30_metadata_source_retry, SECURITY DEFINER ownership by photo_fn_owner, API EXECUTE, absence of PUBLIC EXECUTE and no effective EXECUTE for scheduler/worker/gateway/portal roles. The coordinator first resolves current original authority, then reuses stored normalized publication if present. Origin session remains immutable; current session is passed separately for SQL authorization. Inventory may contain different origin sessions after partial retries, but tenant/project/actor and each version binding remain fixed. Pending publication reuse repeats the existing reserve/complete protocol. This path is authored and still requires persisted session-rotation qualification.
