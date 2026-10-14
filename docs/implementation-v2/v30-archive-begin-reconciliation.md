# V30 archive Begin with unknown storage outcome

`ARCHIVE_BEGIN_RECONCILIATION_REQUIRED` means an open generation writer may have
sent a non-idempotent S3 `CreateMultipartUpload` without receiving its ID. The
normal API must neither start another generation nor close the writer based on
a listing alone. Migration 01962 provides a privileged operator-only SQL
transition for the exact generation. No API, scheduler or maintenance role has
`EXECUTE` on it.

Reservations created before generation keys may instead have a legacy open
Begin writer on `tenants/{tenant}/interchange-uploads/{upload}/archive.zip`.
Migration 01963 provides the separate operator-only
`photo.reconcile_v30_archive_legacy_begin` transition. Apply the same writer
quiescence and exact-key observation requirements. Bind a unique observed ID
with action `bind`; this closes the writer and attaches the legacy key to the
upload. For a terminal original Create and proven empty exact key, use
`release` with SQL NULL multipart ID; a subsequent authorized Begin uses a new
generation key. The release writes an `operator-absent:{sha}` receipt that the
legacy recovery reader explicitly excludes. Do not pass a legacy writer token
to the generation function or choose between ambiguous multipart IDs.

1. Isolate the API process that owned the open writer and establish that its
   original S3 Create request is terminal. Record process/deployment identity,
   transport outcome and stop evidence. Client timeout is not quiescence.
2. Read the `reserved` archive upload and its single `creating` generation and
   `open` begin writer. Capture tenant, upload, generation, reservation and
   writer tokens, canonical generation key and request SHA. The tokens must
   match across all three rows. Stop on conflicting or legacy open writers.
3. After quiescence, use `DiscoverInterchangeGenerationMultipart` to observe
   the exact generation key. Record the observation and independent quiescence
   evidence in an immutable operator incident record; hash it with SHA-256.
   Multiple exact-key IDs, malformed IDs or listing errors require further
   investigation. Empty listing without quiescence never authorizes release.
4. For one observed valid ID, call
   `photo.reconcile_v30_archive_begin_generation(tenant,upload,generation,writer,reservation,'bind',multipart_id,true,evidence_sha)`.
   This atomically closes the writer, records the canonical Create receipt and
   the separate operator evidence digest, binds the generation to its ID and
   moves the reserved upload to `uploading` on the generation key. The same Begin
   idempotency key then replays that upload; no second S3 Create occurs.
5. If the original Create is terminal and the exact key has no multipart,
   call the same function with action `'release'` and SQL NULL multipart ID.
   This closes the writer with operator evidence and marks the generation
   `aborted` with exact-key absence recorded. The next authorized Begin may start a distinct generation
   with a distinct key. A false result means current state or tokens changed;
   reread everything before deciding further action.

The Boolean `writer_quiesced` is an explicit privileged operator attestation,
not SQL's inference from a storage listing. Do not use this transition when
the original Create might still complete. Physical PostgreSQL/MinIO failure
injection and current-build role/ACL checks are required in the later test
phase.
