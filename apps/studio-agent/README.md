# PhotoEditor studio agent (V24)

The watch-folder foundation journals each capture in SQLite before network IO. A file becomes `hashed` only after two unchanged observations at least one second apart and a descriptor-based hash through a path that cannot traverse symlinks. `rehash_before_complete` must succeed immediately before the multipart completion call. A transactionally enforced `max_queued_bytes` limit defers discovery of new files while retaining existing sequence numbers and retry backlog. The agent never deletes or edits camera originals.

`Journal.recover` returns interrupted uploads to `hashed`, preserving the upload ID, multipart layout and acknowledged part ETags. `Transfer.process` resumes the server state, sends missing parts, rehashes the source before completion, and retries receipt creation with a constant `(device_id, sequence, action)` idempotency key. A lost completion response is resolved by reading the upload state; a lost receipt response retries the same receipt. The agent marks a capture done only after the receipt matches device, sequence and source SHA. `verified` and `receipt` states remain pending across restarts.

`HTTPClient` implements `CaptureAPI`: challenge signing through an injected Ed25519 signer, capture-token caching, begin/get/parts/complete and receipt HTTP calls. Production origins require HTTPS; development HTTP is limited to explicit loopback origins. Storage PUT origins must be allowlisted, redirects are rejected, and API credentials never accompany storage requests. A 401 invalidates the cached token for the next attempt. Response bodies and multipart part buffers have bounds.

`DeviceKey` uses cryptography Ed25519 and requires an OS keychain backend (macOS, Windows, SecretService or libsecret); plaintext and fallback backends are rejected. `Runner` persists retry due times, caps exponential delay at 300 seconds plus jitter and stops on device authorization failure. Multipart transfer sends at most two parts concurrently and checkpoints a successful sibling even if the other PUT fails. Token acquisition is serialized across part threads. The CLI requires an operator CA bundle in production. QA alerts, native camera adapters and live server/hardware acceptance remain outstanding. Tests inject server behavior and do not replace a live server integration test. OS keychain integration must be verified on each supported platform.

Install this separate package and create a key before registering its public key with an administrator:

```sh
python -m pip install ./apps/studio-agent
photo-studio-agent create-key
photo-studio-agent run --config agent.json
```

The create command prints only `key_id` and the public key. After server registration, config contains `key_id`, `device_id`, `tenant_id`, `project_id`, `session_id`, `collection_id`, `api_origin`, `storage_origins`, `ca_bundle`, `watch_root`, `journal_path` and optional `max_queued_bytes`. `server_origin` overrides the challenge-signature origin when server CAPTURE_ORIGIN differs from API transport origin. Set `development=true` only for explicit loopback HTTP testing. No private key or token belongs in config. `--once` executes one scan/queue cycle; stable-file discovery requires consecutive cycles at least one second apart.

Run the local state-machine tests from the repository root:

```sh
PYTHONPATH=apps/studio-agent python3 -m unittest discover -s apps/studio-agent/tests
```


`CameraAdapter` defines `list_devices`, `connect`, `capture_events`, `read_file`, and `disconnect`. The implemented `WatchFolderAdapter` exposes the configured folder as one device, returns only stable hashed journal entries, and reads through a read-only descriptor with size/mtime checks. Runner uses its event interface and the injected clock; CLI disconnects on shutdown. Native camera adapters and their hardware qualification are not implemented. The adapter test reads an actual local file; it does not prove a complete cloud upload or camera tethering session.

Inspect a running or stopped agent's queue without opening the keychain or contacting the server:

```sh
photo-studio-agent status --journal /path/to/journal.sqlite --device-id DEVICE_UUID
```

The versioned JSON reports counts for each state, queued bytes (including failed captures), due transfer count and oldest pending age in seconds. All counters come from one SQLite query snapshot and are device-scoped. Paths, source hashes, keys and tokens are omitted. The journal must already exist; status does not create an empty replacement. Queue age measures time since discovery and is not the server's QA processing latency.

After resolving a permanent error, explicitly return one failed capture to the queue:

```sh
photo-studio-agent retry --journal /path/to/journal.sqlite --device-id DEVICE_UUID --watch-root /path/to/camera --sequence 42
```

Retry requires an already hashed failed record and rechecks the unchanged source size, mtime and SHA through the symlink-safe reader. A missing or changed source stays failed. Sequence, source SHA, admitted upload and acknowledged parts are preserved; the pending retry delay is reset. This command makes no network requests and does not bypass server authorization or verification. The next running-agent cycle resumes the existing upload and deterministic idempotency keys. A permanently terminated server upload may reject again and requires operator investigation; retry does not allocate a replacement upload under the same receipt identity.

`run` and `retry` exclusively lock the canonical journal path through an OS-owned `.agent.lock` sidecar. A second writer is rejected before opening/recovering the journal. The lock releases on normal shutdown or process exit; the sidecar remains and must not be deleted while an agent may be running. `status` remains available while the agent runs. macOS/Linux use nonblocking `flock`; Windows uses a one-byte nonblocking `msvcrt` lock. Windows platform acceptance remains required.

Receipt acceptance validates the complete server response binding: resource type, canonical receipt UUID, tenant, project, session, device, sequence, source SHA and upload ID. Transfer independently verifies the upload binding before setting the local record to `done`. A mismatched response remains unconfirmed and becomes a protocol error in the runner; it cannot silently finish another transfer.

Live credential interoperability is covered by backend `TestIntegrationCaptureCredentialsHTTP/python_agent_live_challenge`. Set `PHOTO_TEST_STUDIO_PYTHON` to a Python runtime with this package's dependencies and `PHOTO_TEST_DATABASE_URL` to the isolated migrated test database. The fixture starts the real Go router on loopback and runs the actual Python HTTPClient/cryptography signer against it. It proves challenge/token interoperability and authenticated payload validation; it does not yet prove successful storage upload, receipt adoption or QA processing. The optional default runtime is `.runtime/studio-agent-venv/bin/python`; a missing runtime skips only this subtest.

The live credentials fixture additionally creates a successful receipt from synthetic verified upload/ingest metadata, checks replay with the same and different idempotency keys, rejects conflicting sequence content, and asserts one frame binding and one QA queue entry. Actual object-store verification, ingest execution and QA completion are not covered by that metadata fixture.
