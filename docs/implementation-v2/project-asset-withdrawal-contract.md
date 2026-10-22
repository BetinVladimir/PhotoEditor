# Project archive / asset deletion lifecycle

`Project.Archive` archives an existing project; `Asset.Delete` writes a soft
asset tombstone, refusing use by a published collection. Their durable 202 jobs
are **logical withdrawal and cooperative work settlement**, not a receipt for
recursive physical erasure. Migration 02565 supplies their previously absent
maintenance consumer. This distinguishes the API actions from tenant purge.

The maintenance role claims only an active tenant's already archived project or
already deleted asset, with a new 30-second token. Completion binds all five
claim fields, locks sorted affected runs, requests
ordinary cancellation, and waits for terminal runs/tasks. Project cleanup also
expires unfinished uploads and waits for their ordinary cleanup and bulk jobs.
Blocked jobs requeue with a five-second retry time; expired/replaced tokens
cannot settle the job. No task result, attempt fence, reservation or storage
accounting is rewritten. Cancellation does not mean deleting its outputs.

No project/asset row lock is acquired: version application can hold its run
before locking an asset, so the reverse order would introduce a deadlock. The
API provides no unarchive/undelete operation. The exact durable tombstone must
still exist at entry and again after all waits before job settlement; loss of
that predicate rolls the transaction back.

Project matching includes workflow/editor/semantic, upload/ingest, collection,
download/bulk run IDs, style/training, snapshot-member and explicit config project/version
lanes. Asset matching covers its versions in snapshots/config/editor/semantic
reference or target, task inputs and ingest bindings. Every match is tenant
bound. A multi-source run involving the withdrawn resource is cancelled as a
whole; unrelated resources' data are retained.

Unsealed/empty drafts do not go through the sealed success finalizer:
`workflow.Recovery.Recover` invokes `finalize_run_cancellation` (00419), which
accepts cancelling runs after ordinary task drainage without requiring a sealed
graph or any tasks. It closes expansions, seals the cancelled graph and emits
one `run.cancelled` event. Resource cleanup does not directly invent that terminal.

`completed` has cursor `logical_withdrawal_settled` and
`physical_cleanup=reference_and_retention_governed`. The collector still must
honour every version, publication, snapshot, result and lease reference as
required by implementation/09-media-delivery.md. Accepted versions and originals
are not detached to force collection; shared originals and historical evidence
may therefore remain retained. No physical project/asset metadata purge or new
DELETE grants are introduced. The separately approved tenant purge remains the
only complete tenant metadata erasure protocol after confirmed object cleanup.

Startup refuses absent or incorrectly exposed maintenance functions. The actual
maintenance loop mounts this consumer, reports readiness and retries reclaimed
leases. Authored scenarios are not executed evidence or qualification.
