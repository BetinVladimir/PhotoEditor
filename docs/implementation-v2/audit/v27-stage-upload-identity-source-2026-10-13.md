# V27 worker stage/upload identity closure — source only

`TaskArtifacts.stage` previously validated a reserved scratch file and then called `Gateway.upload` without a pinned inode for ordinary outputs. A same-directory regular-file swap between those operations could change which bytes the gateway opened while still passing its SHA-of-transmitted-bytes check.

`Reservation.validate_file` now returns the regular, single-link file's `(device,inode)` while holding its scratch lock. `TaskArtifacts.stage` forwards that identity for every upload, so `Gateway.upload`'s existing `O_NOFOLLOW`/`fstat` preflight and post-response identity check apply to all staged outputs. A caller-provided earlier identity still refuses a replaced file before any gateway call. Three fake gateways were updated to assert the pinned identity; an authored platform case replaces a reserved file and requires pre-upload refusal.

Tests, typechecks, formatters, migrations and physical worker runs were NOT RUN under the code-first sequence. A real concurrent file-swap probe at the gateway boundary and native normal/alpha/float32-alpha regression remain required. Pixel algorithms and model selection were not changed.
