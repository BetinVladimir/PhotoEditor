# V24 device multipart Begin reconciliation

This procedure applies only to a device upload whose `BeginDevice` replay returns
`CAPTURE_BEGIN_RECONCILIATION_REQUIRED`. A lost object-store response may have
created a multipart upload even though SQL still says `initializing`. The
`device_begin_token` is a durable fence; neither a timeout nor an empty listing
can clear it automatically.

1. A privileged operator stops or isolates the API process that dispatched the
   original `BeginMultipart` and proves that its transport request has reached a
   terminal outcome. Record the deployment/process identity, stop evidence and
   time. Merely waiting for the HTTP client timeout does not prove this.
2. Read the exact `tenant_id`, `id`, `device_begin_token`, `object_key`, `state`,
   `multipart_id` and `expires_at` from `photo.upload_sessions`. Require the
   canonical `staging/tenants/{tenant}/uploads/{upload}` key, `initializing`,
   non-null token, null multipart ID and unexpired reservation.
3. Observe the exact key through `DiscoverCaptureMultipart` after writer
   quiescence. Inspect errors and duplicate IDs. Record the complete evidence
   in an operator-owned immutable incident record and calculate its SHA-256.
   A foreign prefix member is irrelevant; two exact-key IDs are an incident,
   not a choice between them.
4. If exactly one valid multipart ID exists, call
   `photo.reconcile_device_begin(tenant,upload,claim,'bind',id,true,evidence_sha)`.
   If none exists *and the original writer is proven terminal*, call the same
   function with `'release'` and a SQL NULL ID. It returns false on a changed
   claim, state, key, expiration or malformed evidence. On `bind`, a normal
   deterministic Begin replay returns the existing `uploading` session. On
   `release`, the replay may make one new durable claim and one new external
   Begin. If that attempt is again uncertain, repeat this procedure with its
   new claim and new evidence.
5. The SQL function has no runtime-role EXECUTE grant. It must be called by an
   authorized database operator; the `p_writer_quiesced=true` argument is the
   operator's explicit attestation, not a fact inferred by SQL. The upload row
   retains the latest evidence digest, reconciliation timestamp and count.

The studio-agent journals the server's reconciliation-required response as a
five-minute hold. It preserves the original upload ID and Begin key, does not
count this hold as a transport retry and resumes from the same upload after the
operator binds or releases the fenced writer.

Never abort by key or reset the token while an original Create request may
still finish. The function does not run inside the device/API request path.
The server also refuses a malformed multipart ID and checks that each SQL
claim/attach changed exactly one row. If its successful S3 receipt cannot be
attached, it leaves the claimed upload for operator reconciliation rather than
aborting a multipart that could have been bound concurrently.
Migration 01960 fences existing `initializing` device rows, so those rows also
require this procedure before retry. Physical MinIO and PostgreSQL recovery
drills remain part of the later test phase.
