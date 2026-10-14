# Semantic review API installation — current implementation

The API bootstrap installs `ApplyService` on `POST /api/v1/tenants/{t}/semantic-intents/{id}/apply`. Current editor authorization, session/CSRF and idempotency remain mandatory. Without preparation configuration, existing committed receipts can replay; new work returns `DEPENDENCY_UNAVAILABLE` (503).

To install rejection preparation, supply both `PHOTO_SEMANTIC_REVIEW_CONFIG_FILE` and `PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256`. The latter is lowercase SHA-256 of the exact file bytes, including whitespace. The file is bounded to 16 KiB, strictly decoded, and captured at startup. Unknown or duplicate fields, trailing JSON, unsupported versions, invalid pins, missing paired settings or changed bytes fail startup without exposing file contents. This file contains no credentials.

Required JSON fields:

| Field | Contract |
| --- | --- |
| schema_version | integer 1 |
| review_model_sha | lowercase 64-hex trusted original review model lock |
| review_dataset_sha | lowercase 64-hex trusted original review dataset lock |
| document_budget_bytes | integer from 2 through 67108864 |

Pins must describe the original qualified review producer. The file is not scientific qualification evidence. Rejection preparation reconstructs original documents using scoped/versioned reads, rechecks sources and targets, publishes the decision, and commits it atomically. Bootstrap uses the existing API pool and object bucket.

Approved local exposure has a separately tested pure constructor, `NewExposureApplyService`, and a database-verified installer, `InstallExposureApplyService`, requiring an explicitly provisioned admission pool, verified original ICC, trusted CPU qualification pins, measured owner costs and execution bounds. CLI bootstrap can install the tenant catalog using the settings below. Migration 00607 grants an explicit existing-function allowlist only to the trusted NOLOGIN/NOINHERIT/NOBYPASSRLS role photo_semantic_admission. API and scheduler receive no private admission grants. The role has tenant/route-scoped RLS on receipts, INSERT on receipt request fields and UPDATE only on response fields. It has no direct runs/tasks/tenant writes or DELETE. Tenant/usage locks are acquired through authenticated lock_semantic_apply_resources. Role membership for a dedicated coordinator login must be provisioned separately; never grant it to API/worker accounts. This role trusts the coordinator to verify actual object bytes; ACLs cannot prove scientific qualification or safe transaction composition. Without approved settings, a new approved request returns 503 before preparation; committed approved receipts can replay.

Remaining integration work: provision dedicated login membership and verify the admission transaction boundary; provision real verified qualification reports/policies and measured deployment costs; validate the approved bootstrap with a dedicated provisioned login; prove mixed batches and real worker/adoption/Apply completion. Current installation does not establish whole V27 or V20–V32 completion.

The installer requires enabled and forced receipt RLS with row_security_active for the admission role. It also requires current_user photo_api on the API pool and verifies current_user photo_semantic_admission, safe role attributes, no outgoing memberships, no destructive/direct data-write privileges beyond receipt columns, immutable request_hash and exactly one executable trusted SECURITY DEFINER function per allowlisted name on the admission pool. It changes no ACLs. This is active-role verification, not an attestation of session_user login privileges: deployment must use a dedicated nonprivileged login which cannot RESET/SET ROLE into owner or another privileged contour.

Approved exposure preparation now reads the 00608 scoped report at its exact object version before publication, validates original JCS/SHA and content against authenticated policy/selected CPU locks, and repeats metadata authorization after IO. Report bytes are bounded to 8 MiB; strict policy metadata to 4 MiB. This validates imported evidence consistency and does not establish the scientific truth of that evidence.

Approved installation configuration is tenant-specific: ExposurePreparationCatalog schema_version 1 contains 1..1000 tenants with tenant_id, review (the review schema above) and execution (qualification, owner_costs, limits and model/runtime/device/dataset locks). Strict JSON is bounded to 4 MiB; duplicate tenants and report/calibration artifacts reused across tenants deny. InstallTenantExposureApplyService owns this catalog and shares verified pools; tenants without configuration can replay receipts but cannot prepare fresh work. CLI loading validates the exact file bytes and invokes this installer.

Approved bootstrap requires all of:

- `PHOTO_SEMANTIC_EXPOSURE_CONFIG_FILE`: regular non-symlink catalog file, not writable by group/others, maximum 4 MiB.
- `PHOTO_SEMANTIC_EXPOSURE_CONFIG_SHA256`: lowercase SHA-256 of the exact catalog bytes.
- `PHOTO_SEMANTIC_ADMISSION_DATABASE_URL`: separate DSN for the dedicated admission login. It is never read from catalog JSON or logged in errors.
- `PHOTO_ICC_ROOT`: directory containing the original `work_rec2020_gamma24_v1.icc`; bounded bytes must match the locked original profile.

Approved settings and rejection-only `PHOTO_SEMANTIC_REVIEW_CONFIG_*` settings are mutually exclusive. Missing pairs, conflicting settings, changed files, invalid catalogs/ICC or unsafe/unavailable roles fail startup. With no approved settings, bootstrap retains the previous replay/rejection path. The admission pool has at most eight connections and a 30-second statement timeout and closes with API shutdown or installation failure.

The dedicated login must be LOGIN, NOINHERIT, NOSUPERUSER, NOBYPASSRLS, NOCREATEROLE, NOCREATEDB and NOREPLICATION. Its only role membership must be photo_semantic_admission; it must own no photo tables and have no direct data-write/destructive column/table grants. Startup queries session_user and ACLs after SET ROLE photo_semantic_admission, then runs the existing active-role/RLS/function guard. Provision the login/membership outside the API account; bootstrap creates neither roles nor grants. Bootstrap with a temporary dedicated nonprivileged login is physically tested against an isolated database, including privileged-login refusal and login cleanup. Committed Apply replay through the dedicated-login bootstrapped service is physically tested. Fresh approved mutation through CLI bootstrap with the dedicated login is physically tested, including sealed child/ready task and durable replay. Fresh native HTTP Apply and replay through the actual handler with bearer/session validation are physically tested in-process; missing auth/CSRF/idempotency is rejected. A live listening-server deployment and downstream processing remain pending.

### Parent review fence for closure UI

`GET /api/v1/tenants/{t}/semantic-intents/{id}/review` requires a current authenticated editor authorized for the intent's project. The server computes the principal from the session. It returns exactly `{run_id,workflow_id,revision,state,plan_sha,remaining,can_close}`: nonnil UUIDs; decimal positive int64 parent revision; JobStatus state; lowercase SHA256 or null; integer proposed-target count 0..5000; boolean closure eligibility. No storage key/version is returned. This endpoint supplements the target listing because target revisions are independent of parent revision.

`plan_sha` comes from the current authenticated, adopted assembly source inventory. Missing or expired source evidence returns null and disables closure. `can_close` requires waiting_review, no cancellation request, an incrementable parent revision, an available plan and zero proposed targets. Queued/applied/failed/cancelled/rejected/unsupported targets are already decided. Completed parent state remains visible while source evidence expires. UI treats this fence as advisory: final close admission reauthorizes and locks all source/target state. A stale or incomplete close returns conflict and requires refreshing the fence; it never infers the parent revision from targets.

Migration00623 installs API-only `semantic_review_status`; its authority delegates to `semantic_review_identity` and its source proof to `semantic_review_plan_sources`. The UI must not offer editing/closure to a viewer merely because target listing is allowed.

### Close/status public errors

Declared SQL review authorization failures return `404 NOT_FOUND`; no existence or project membership detail is disclosed. Review revision/state serialization conflicts return `409 REVISION_CONFLICT`; undecided targets return `409 REVIEW_INCOMPLETE`. Strict request validation returns `422 VALIDATION_FAILED`. Unexpected database privileges, integrity or connection failures remain internal errors; they must not be mislabeled as user validation failures. A UI must retain the original key/body after network or 5xx ambiguity and require a fresh status fence after a definitive conflict. Same-key committed close replay reauthorizes the caller and does not require the close artifact to remain unexpired.

### Explicit decision submission UI

The target screen offers approve/reject only for loaded proposed targets under current editor review authority. Selecting a button toggles that decision; unselected targets receive no decision. A batch contains1..100 distinct targets. Before first submission the UI reads current parent review status and binds its revision/plan SHA, not target row revisions. A request owns its decisions and idempotency key; network/5xx ambiguity disables changes and retries the same batch. Definitive conflict/access/validation errors clear stale choices and require refreshing review status/results. Successful admission reloads targets and parent closure eligibility; closing decisions remains separate from child processing completion.
