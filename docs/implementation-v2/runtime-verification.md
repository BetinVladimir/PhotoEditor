# Runtime verification evidence

2026-10-01: a new isolated PostgreSQL 18 cluster was initialized under
`.runtime/v24-postgres/data`. It listens only on the repository-local Unix socket
`.runtime/v24-postgres/socket`, port identifier 55439, with TCP disabled. The
runtime and socket directories are accessible only to their filesystem owner.
Database `photo_v24_check` contains synthetic test data, not production data.

The normal `PHOTO_ROLE=migrate` application command applied migrations 00001
through 00299. Its first execution found a syntax error in migration 00298's
CASE expression used as a BETWEEN bound. The expression was parenthesized;
retry then applied 00298 and 00299 and reported database version 299.

With `PHOTO_TEST_DATABASE_URL` set to that database, the following packages
passed with their existing integration tests enabled:

- `backend/internal/artifactjson`
- `backend/internal/v2draft`
- `backend/internal/v2sessions`
- `backend/internal/v2workflow`

This proves creation of the current schema/functions on PostgreSQL 18 and the
covered existing V2 flows. It does not prove all new V24 SQL execution branches,
concurrency/revocation behavior, end-to-end capture, model qualification, target
PostgreSQL-version compatibility, or RTX3060 resource budgets. Those remain
required verification work. Documentation static-check reports retain their
own scope and must not be interpreted as this runtime evidence.

Local test connection (contains no credentials):

```
postgresql:///photo_v24_check?host=/Users/freelancer/Documents/PhotoEditor/.runtime/v24-postgres/socket&port=55439&sslmode=disable
```

The PostgreSQL binaries used are from
`/Applications/Postgres.app/Contents/Versions/18/bin`. The isolated cluster may
be reused for further integration tests; apply new forward migrations before
those tests. Never point the integration environment at an existing user or
production database.

## ShotList runtime verification

Migration 00300 was applied forward to database version 300. The API role has
SELECT but no UPDATE on shoot_sessions, so direct FOR SHARE reads failed with
42501. read_v24_shot_snapshot now locks through an authenticated, tenant-scoped
SECURITY DEFINER boundary with project ACL checks and no API DML grant.

TestIntegrationShotListCASReplayAndRevokedConsent passed against PostgreSQL 18
with the actual photo_api role. It covers absent-list revision, publication,
idempotency replay, stale CAS rejection, replacement retaining two immutable
documents, and refusal after identity consent revocation. This is not evidence
of complete V24 end-to-end readiness.

The generic GetDocument path now shares strict ShotList parsing and the fresh
project/identity-consent gate with GetShotList. The actual API-role integration
test covers generic reads before and after revocation; v2draft tests passed.

Migration 00301 revalidates frozen consent and coverage at graph close, after
publication/transport I/O, while holding the resulting person/grant share locks
until activation commits. It also requires exact staged parameter/context bytes
and existing frozen seeds. Migration application is verified; full graph-close
revocation/concurrency execution remains an outstanding integration gate.

## Technical-quality parity

The shared vectors in
`backend/internal/v2capture/testdata/technical-quality.json` are executed by
`TestIntegrationTechnicalQualityVectors` against the real PostgreSQL function
and by `test_shared_postgresql_quality_vectors` against the typed Python worker.
All ten vectors passed on both implementations: global/face scores, inverted
clipping/glare, context-only checks, unavailable/missing scores, absent global
foundations, duplicate imaging checks, and policy/report coverage mismatch.
Python quality tests: 4 passed. This proves the listed metric vectors only;
it does not qualify imaging models, GPU accuracy, or all possible inputs.

Migration 00302 adds persisted immutable qa_stale and stale warning/event code
at capture report adoption; reference database and public resource contracts
were regenerated. Migration application and static checks are verified. Full
report-adoption execution and exact 30-second boundary integration remain to
be tested; successful migration creation alone does not verify those branches.

Migration 00303 disambiguates adoption session/version variables from SQL
column names and was applied to isolated PostgreSQL (version 303). Full
adoption execution remains outstanding. Photographer OwnerAPI now exposes
ShotList read/CAS replacement, alert pagination/ack, and explicit receipt
confirmation with caller-supplied retry keys. OwnerAPI tests: 19 passed;
photographer TypeScript check passed. This establishes client transport support,
not a completed capture UI or end-to-end capture workflow.

Photographer capture screens added: project navigation to `/capture`, session
list scoped by tenant/project, and `/capture/[id]` with ShotList requirements,
alert cursor pagination, stale indication and revision-bound acknowledgement.
Initial loads cancel on scope/unmount changes. TypeScript check and OwnerAPI
19 tests passed. UI has not been visually rendered or exercised against live
HTTP. Session-list pagination beyond its first 100 entries, ShotList editing,
explicit frame confirmation UI, report details, and live refresh remain open.

Capture session-list cursor pagination beyond 100 entries is implemented with
ID deduplication and scope-generation fencing for late responses. The report
screen provides explicit refresh and fences paginated/acknowledgement responses
on scope change/unmount. These close the earlier pagination/manual-refresh
implementation gaps; visual/live-HTTP verification remains outstanding.

ShotList UI editing now supports add/remove, labels, required-frame counts
1..100 and minimum technical quality 0..1, with a 200-requirement bound. Existing
person bindings are preserved. Save uses the loaded decimal session revision;
unchanged failed saves reuse the same idempotency key, changed edits get a new
key. Responses are scope-fenced. TypeScript passed. Person selection/binding UI,
visual verification and live save/CAS-conflict interaction remain outstanding.

ShotList editor now loads the authoritative session project and pages through
project persons with pinned tenant paths and abortable requests. Active persons
can be selected/deselected per requirement (maximum 100); labels are shown.
Server publication remains responsible for current identity-consent validation;
active-person state alone does not establish consent. Existing IDs are retained.
TypeScript passed. Live person/revocation/save interaction and visual validation
remain outstanding; no full V24 readiness claim is made.

Migrations 00304/00305 persist an immutable nullable alert receipt binding,
validate its project/session/version, and enforce that exact receipt during
confirmation. Historical alerts retain null binding. Reference contracts were
regenerated; v2-check and photographer TypeScript passed. UI offers explicit
frame confirmation with stable retry keys and scope-fenced completion. Migration
creation is verified; full adoption/confirmation and visual/live HTTP testing
remain outstanding. Confirmation alone does not guarantee a frame satisfies
the configured quality threshold or identity requirements.

Capture scheduler startup now checks exact schema-qualified SQL signatures and
EXECUTE privileges through to_regprocedure; any missing/incompatible overload
fails the gate. Real photo_scheduler-role testing exposed an ANY(array) query
against JSONB operation backends; it was corrected to JSONB membership.
TestIntegrationCaptureStartupRequiresQualifiedOperations passed with capture
operations disabled, proving the query executes under scheduler privileges and
rejects unqualified operations. Qualified positive startup and missing-function
fault injection remain outstanding; this is not full scheduler/worker readiness.

The subsequent real-role attempt also exposed denied direct registry SELECT.
Migration 00306 provides narrowly granted SECURITY DEFINER
capture_operations_ready(text); scheduler retains no direct registry SELECT.
The final startup integration run uses this function and exact signatures.

TestIntegrationCaptureStartupExactFunctions passed on real PostgreSQL: a
transaction-local synthetic enabled-operation fixture passes startup under
photo_scheduler; renaming the exact required claim function fails it; restoring
the name and revoking EXECUTE also fails it. All registry/DDL/grant changes are
rolled back. Post-test inspection confirmed disabled qualification, restored
function and restored scheduler EXECUTE. This verifies startup mechanics only,
not imaging/model qualification or actual capture pipeline execution.

Repeated integration execution found a session-test fixture attempting to insert
another globally unique admission policy for session.describe/1. The fixture
now retains an existing policy on conflict and explicitly requires matching
synthetic model/runtime/backend/device/dataset hashes; incompatible state fails
instead of being overwritten. TestIntegrationSessionCreateReadUpdate passed
with count=2. This change affects tests only and does not authorize or qualify
production models.

Frozen coverage/history packet validation now rejects duplicate JSON object
keys; coverage additionally enforces exact nested report and identity keys.
Tests reject conflicting repeated quality/SHA/consent-epoch fields and extra
identity fields. v2input, v2draft and workflow package tests passed without the
integration database environment in this run (DB-gated tests were skipped).
This verifies compiler behavior and existing unit regression coverage only.

Additional compiler regressions cover duplicate history fields, Unicode-escaped
aliases of duplicate JSON keys, trailing objects and valid nested/null values.
Coverage rejects one consent ID reused across two people and malformed frozen
history digests. v2input package tests passed (database environment absent;
this run establishes parser/compiler behavior, not SQL adoption behavior).

Selection documents and consent snapshots now enforce exact nested keys before
canonicalization. Nullable ShotList selection remains explicit; grants require
person_id (including explicit null where supported), and confirmed-face fields
are mandatory. Regression tests reject duplicated nested document SHA and
consent epoch. v2input/v2draft/workflow package tests passed with DB-gated tests
skipped in this run. No new SQL/GPU verification is implied.

Migration 00307 adds a private adoption-time live-evidence assertion: frozen
queued job/session consent epoch, active persons, and exact grant status/epoch
for both current-frame and historical coverage evidence. Share locks remain
held through report/alert adoption; assertion failures roll back generic output
adoption in the same transaction. Schema creation and the synthetic JSON union
query were verified on PostgreSQL; full valid/revoked/concurrent adoption
execution remains outstanding. v2input/v2draft/workflow/v2sessions tests with
DB enabled passed before this migration and are not proof of its new branches.

TestIntegrationCaptureLiveEvidenceGate passed on PostgreSQL 18. A transaction-
local focused fixture proves the adoption assertion accepts a matching active
person/grant, rejects revoked/advanced grant epoch with
V24_IDENTITY_CONSENT_REVOKED, rejects changed session epoch with
V24_CAPTURE_CONTEXT_STALE, and accepts again after savepoint rollback. The whole
fixture rolls back. Graph/receipt/version FKs are deferred and intentionally
uncommitted in this focused test; it is not evidence of valid full workflow,
worker report adoption, historical coverage branch or concurrent revocation.

The live-evidence focused integration fixture now executes both current-grant
and historical-coverage-only branches. Each accepts active matching evidence
and rejects revoked grants, epoch advancement without revocation, and stale
session epoch. Gate execution succeeds as photo_fn_owner; direct calls from
photo_api, photo_worker and photo_scheduler are denied with 42501. Both
subtests passed on PostgreSQL. Deferred/uncommitted graph fixture limitations
still apply; concurrency and full worker adoption remain unverified.

Live-evidence tests now verify real two-connection lock behavior in both
current-frame and historical-coverage branches. Valid synthetic base
identity/session rows are committed solely to make them visible to the second
connection; deferred fake graph/receipt/version references still roll back.
After gate execution as photo_fn_owner, concurrent grant revocation hits
lock_timeout (55P03). After the gate transaction rolls back, the same UPDATE
succeeds for exactly one row; that probe also rolls back. Both subtests passed.
This supersedes the prior description that the entire base fixture rolls back.
It proves grant-lock serialization, not full worker report adoption or its
end-to-end concurrent lifecycle.

Capture UI handles only the explicit CONSENT_REVOKED ShotList-read error with
an authoritative session-revision fetch and a blocked editor. Alerts continue
to load. An explicit 'Start a replacement shot list' action enables a new list;
no forbidden document bytes/person bindings are recovered or silently reused.
Save still uses revision CAS/live consent. Previously loaded unavailable person
bindings are visible and removable. Confirmation text now states that coverage
also depends on quality and required people. TypeScript passed; these UI flows
still need live HTTP and visual interaction verification.

ShotList recovery is now isolated in loadCaptureShots and tested. Only typed
HTTP 409 CONSENT_REVOKED yields an empty, blocked replacement at the freshly
read session revision. Forbidden/dependency/network errors and mismatched
status propagate, and invalid/noncanonical/out-of-range revisions are rejected.
Valid reads do not fetch a replacement revision. Photographer TypeScript and
23 targeted client tests passed. Rendered UI/live HTTP remain unverified.

Worker capture.assess adoption now translates only known SQLSTATE 23514 live-
consent/context invariant messages into AdoptionInvalid rejection codes. This
feeds the existing durable reject_v2_adoption path rather than endless generic
DB retries. Serialization/deadlock/connection errors and unknown SQL details
remain unclassified/retryable; non-capture operations are unaffected. Targeted
classification and capture adoption tests passed. A real full result-rejection
transaction still requires end-to-end verification.

Migration 00308 extends reject_v2_adoption for capture.prepare/capture.assess:
matching durable admission becomes failed with the rejection code and clears
workflow/lease fields in the same transaction as run cancellation/event. The
preparation workflow pointer remains for provenance. Migration application is
verified; actual fenced-result rejection and final cancellation require a full
workflow fixture and are not proven by migration creation.

Focused capture live-evidence integration now includes actual snapshot/run,
workflow, task/attempt, verified output_upload/artifact and canonical task result.
reject_v2_adoption executes as photo_scheduler and is asserted to set run
cancelling/cancel_requested and matching admission failed/CONSENT_REVOKED with
cleared workflow/lease fields. Both current/historical subtests passed. Graph
records/output bytes are synthetic; receipt/version references remain deferred
and rolled back. This proves the rejection function's state transition under
real role privileges, not full camera/worker adoption or final run cancellation.

The focused rejection lifecycle fixture now asserts idempotent repeat rejection
returns false and leaves exactly one v2.adoption.failed event. The actual
finalize_run_cancellation function executes as photo_scheduler and changes the
synthetic capture run to cancelled/graph_sealed; both current/historical
subtests passed. The synthetic run uses an early creation time so the existing
global finalizer selects it first; all graph/result/finalization changes roll
back. This proves scheduler rejection/finalization mechanics for the fixture,
not a full uploaded-frame/adopted-report workflow or hardware qualification.

The focused lifecycle fixture no longer uses fake receipt/version references:
actual asset/version, quota reservation, verified upload session, capture device,
receipt and receipt-assets binding are inserted. The existing enqueue trigger
creates the admission; the fixture stages it for gate testing. SET CONSTRAINTS
ALL IMMEDIATE verifies every FK before execution. Both current/historical tests
passed, superseding earlier fake/deferred-reference limitations. JSON/image
bytes and model/capture policy are still synthetic; this is not real ingestion,
qualified imaging, graph preparation, or full successful report adoption.

The FK-valid focused fixture now tests confirm_capture_frame under photo_api
with a real authenticated session and app tenant context. Exact receipt/alert
confirmation succeeds, direct replay returns the same UUID and leaves one row,
and substituting a different real receipt for the same asset version is rejected
with 23514. Both current/historical subtests passed. The alert/report evidence
is synthetic metadata; HTTP idempotency and genuine adopted QA report/identity
observations still require end-to-end verification.

Focused PostgreSQL tests now execute acknowledge_capture_alert as photo_api:
revision advances to string '2', receipt_id and true qa_stale survive, actor is
recorded, and technical_quality/quality_metric/quality_report_sha are absent
from the public response. A stale revision is rejected with 40001. Both fixture
branches passed, along with confirmation/revocation/rejection/finalization
checks. This verifies SQL acknowledgement behavior, not HTTP replay handling
or actual delayed QA generation.


### V24 SQL report adoption — successful and rejected byte binding (2026-10-01)

`TestIntegrationCaptureLiveEvidenceGate` now exercises `adopt_capture_report` under the real `photo_scheduler` role on PostgreSQL 18, migrations through 00308. Both current-frame consent and historical coverage consent fixtures cover:

- canonical JSON bytes with actual SHA-256 metadata for params, context, and report; declared context task input and verified fenced output upload;
- accepted report yields exactly one alert with the admission receipt, technical quality 0.9, and matching report SHA;
- identical replay returns false without duplicate alert;
- fresh admission yields info / qa_stale=false; admission aged 31 seconds yields warning / qa_stale=true;
- changed context bytes (trailing whitespace, identical decoded JSON) fail with CAPTURE_ADOPTION_BYTES_INVALID; transactional rollback removes the generic adoption marker and inventory links, allowing the valid retry.

Verification: real-database Go tests passed for v2capture, v2draft, v2input, workflow, v2sessions, v2workflow, and artifactjson. Scenario savepoints and graph transaction roll back; committed base identities remain only in the private synthetic test database.

Scope: this is SQL-boundary integration. Minimal synthetic JSON exercises SQL requirements; it does not prove complete typed worker contracts, object-store reads/writes, real camera upload, GPU model quality, full preparation/closure, or rendered UI. V24 and the complete V20–V32 goal remain unfinished.


### V24 client capabilities and production endpoint manifest (2026-10-01)

The contract generator now publishes the existing V2 endpoint specification to `packages/contracts/v2-endpoints.json` without changing the legacy endpoint manifest format. Photographer capability registry includes `/capture` and `/capture/[id]`, with their session, shot-list, alert, person-list, acknowledgement and frame-confirmation dependencies. Client contract checks use both legacy and V2 declarations.

`TestCaptureClientRoutesRegistered` constructs and walks the actual Chi router, checking the eight required handlers against the production V2 endpoint manifest (including loop-registered person listing). This proves route registration, not live HTTP behavior or permission correctness. The V2 manifest still describes planned endpoints as well as implemented endpoints; it is not a completion inventory.

Verification: generator drift check passed; 25 focused client tests and TypeScript check passed; actual Go router-registration test passed. V24 full camera-to-worker-to-browser integration remains outstanding.


### V24 foreground alert refresh (2026-10-01)

Capture session screen now polls alerts every five seconds while active, independently of shot-list loading/editing. Requests are sequential and abort on scope change, mutation, or unmount; foreground return triggers a refresh. New report bursts are scanned through cursor pages until the previously displayed window is reached, preserving the older-history continuation. Alert merge deduplicates IDs, retains loaded history, and accepts only higher canonical decimal revisions, preventing older polling responses from undoing acknowledgement state. Polling does not reload the shot list or clear unsaved edits.

Verification: 29 focused client tests passed (including four merge/revision regressions), and TypeScript passed after pagination integration. This does not prove rendered/native lifecycle behavior, live authenticated HTTP, SSE delivery, or complete V24 end-to-end processing; those remain outstanding. Polling is the implemented delivery mechanism here.


### V24 authenticated HTTP alert and confirmation boundary (2026-10-01)

`TestIntegrationCaptureAlertHTTP` exercises the real HTTP router via httptest, signed native user tokens, `photo_auth` authentication connections, and `photo_api` service transactions against PostgreSQL through migration 00308. Fixture rows satisfy committed foreign keys and remain exclusively in the private synthetic test database.

Verified: alert page exposes receipt_id and qa_stale while omitting private quality fields; unauthenticated confirmation returns 401; missing idempotency key returns 400; successful confirmation returns 200; identical confirmation replay preserves bytes and sets Idempotency-Replayed; changed replay body returns 409; acknowledgement and identical replay succeed; stale acknowledgement with a new key returns 409; foreign-tenant listing returns 404. HTTP test plus actual router registration passed twice consecutively.

Scope: synthetic verified storage metadata and report quality evidence. No actual object storage, network listener, camera transport, rendered browser/native screen, or GPU processing is exercised. Viewer/editor ACL permutations, source receipt substitution through HTTP, and complete worker pipeline remain separate outstanding checks. V24 is not marked complete.


### V24 alert delta paging regressions (2026-10-01)

Extracted `readCaptureAlertDelta` from the session screen and retained its use in the foreground polling path. Four regression cases verify a burst spanning multiple pages until the previously displayed window, one-page initial loading with explicit history continuation, rejection of a repeated cursor, and cancellation after a late response. Cancellation is checked before and after each request. All 33 focused client tests and TypeScript passed.

The plan requires existing SSE plus five-second polling fallback. This change verifies only the polling delta reader; Capture SSE subscription remains missing and must be implemented. Native lifecycle and rendered behavior are still unverified. No V24 completion claim is made.


### V24 server SSE transport (2026-10-01)

Inspection corrected the assumption that an existing SSE transport was available: `/events` supported JSON only. The same route now negotiates `Accept: text/event-stream`, preserving JSON behavior. SSE uses existing authorized durable event queries, decimal cursor IDs, JSON Event envelopes, heartbeat comments and Last-Event-ID resume. Streams end after 20 seconds within the existing 30-second HTTP write timeout; clients must reconnect. Every one-second query repeats authentication and event visibility checks; no DB transaction remains held while idle. Initial cursor/auth errors remain normal HTTP errors before streaming starts.

`TestIntegrationEventSSE` uses a real local HTTP listener and PostgreSQL/API-role authentication, checking event framing, content type, resume without replay, and invalid Last-Event-ID (422). SSE, Capture HTTP and router tests passed together.

Outstanding: authenticated client streaming parser, reconnect/coalescing and Capture subscription; denied-role/visibility and live revocation during an open SSE stream; native and browser runtime qualification. Server transport alone does not fulfill the complete V24 delivery requirement.


### V24 Capture SSE client connection (2026-10-01)

ApiClient now exposes authenticated single-connection SSE reading with Accept, Bearer, native headers and Last-Event-ID; 401 refresh reuses the existing single-flight mechanism. Bounded parser handles fragmented CRLF frames, heartbeat comments, durable cursor/envelope binding, frame size limit and cancellation. Capture reconnects after stream completion/failure, retains the last cursor, refreshes only on its own capture.alert session and keeps five-second foreground polling. No token is placed in a URL. JSON events now sends `after`, matching the server (previously `cursor`).

Parser tests and TypeScript passed; Capture refresh/OwnerAPI tests passed. This is implementation and unit evidence, not a rendered native/browser SSE qualification. Stream revocation/visibility, reconnect/coalescing lifecycle behavior and complete camera/GPU pipeline still require integration verification.


### V24 SSE visibility and active-session revocation (2026-10-01)

Extended real HTTP/PostgreSQL SSE test with an event addressed to another principal: resume yields heartbeat, without delivering that payload. While a connection remains open, the fixture revokes its native auth session and queues a new principal-visible event. The server closes cleanly at reauthentication before delivering any further data; reconnect returns 401. Test passed in ~1 second.

Client Capture stream now owns a per-connection AbortController, cancels streaming when AppState becomes inactive, restarts on foreground return, and prevents concurrent reconnects. Scope/unmount cancellation propagates from its parent controller. Focused client tests and TypeScript passed; native/browser lifecycle behavior itself remains unverified. This test does not establish full project ACL permutations, deletion races, or full V24 completion.


### V24 Capture RU/BG/EN catalogs (2026-10-01)

Capture session/list screens and shot editor now use shared locale catalogs for action text, error messages, severity labels, quality/frame/people labels and accessibility labels. Added a root LocaleProvider and accessible language picker, with persisted device preference. Errors store message keys, so an already visible error changes language too. Delayed preference restoration cannot overwrite a language explicitly chosen during startup. Locale updates do not participate in Capture data-loading effect dependencies and do not intentionally reset draft state.

TypeScript and 15 focused locale/Capture tests passed. This is source/type/catalog evidence; rendered draft preservation across locale change, keyboard commands, profile-locale synchronization, other application screens, session-state translations and complete native accessibility remain outstanding. User-entered names are preserved as entered. V24 is still incomplete.


### V24 camera adapter contract and watch-folder implementation (2026-10-01)

Added CameraAdapter Protocol and immutable CameraDevice descriptor. WatchFolderAdapter implements the plan's list_devices/connect/capture_events/read_file/disconnect interface, using the existing durable scan and no-symlink file opening. read_file checks device, connection and unchanged source metadata before handing out a read-only file descriptor. Runner now obtains discovery events through this adapter using its injected clock; CLI disconnects during shutdown.

All 15 studio-agent tests passed, including actual-file lifecycle, wrong device, source replacement and disconnected read rejection. Existing restart/multipart/dedup tests remain green. Native tethering, OS-keychain platform verification and full cloud watch-folder/hardware acceptance remain outstanding. No camera model is newly declared qualified.


### V24 alert ordering integration correction (2026-10-01)

Inspection found an actual server/client mismatch: ListAlerts was ascending while the Capture delta reader assumes newest-first pages. It could stop at the old first-page overlap and never discover later alerts. Changed ListAlerts to descending created_at/id, strict tuple `<` continuation, and a distinct descending cursor sort identity. Ascending cursors now fail scope validation instead of being silently misinterpreted.

Real HTTP test inserts a newer alert after initial actions, requests limit=1, verifies the newer report appears first, then follows its signed cursor to the older alert with no further continuation. Test passed on PostgreSQL through 00308. The preceding format inspection found watch-folder suffixes already align with server admission; no format support or camera qualification was expanded. Full V24 pipeline remains unfinished.


### V24 SSE refresh coalescing (2026-10-01)

Capture SSE, foreground activation and polling now request a shared CoalescedRefresh queue. Only one delta read runs at a time; a burst arriving during it requests exactly one trailing read rather than being ignored. Scheduling replaces the previous timer and scope cancellation drops pending work. Two deferred-promise tests cover burst coalescing and cancellation; these plus four delta-pagination tests and TypeScript passed. Rendered lifecycle/end-to-end V24 remain unverified.


### V24 project ACL checks on HTTP pagination (2026-10-01)

Capture HTTP fixture now uses a restricted project and a second signed native user with editor tenant membership. Without project ACL, list returns 404. With viewer project ACL, listing succeeds, while confirmation and acknowledgement return 403. Removing that ACL then following the user's previously issued signed cursor returns 404. The real HTTP/API-role/PostgreSQL test passed, proving cursor validity does not replace fresh project authorization. Complete pipeline and rendered UI remain unfinished.


### V24 worker regression and session client contracts (2026-10-01)

All 36 Capture worker tests passed (pixels, ONNX adapter, preparation, proxy, report, quality, adoption/rejection). Existing alert composite index supports backward scanning for descending order; no redundant index migration was introduced. This does not qualify real GPU weights or camera models.

Added typed shooting-session policy/create/update/resource interfaces and OwnerAPI creation/CAS update methods with caller-supplied stable idempotency keys, explicit session and capture calibration refs, nullable capture policy, and decimal revision strings. All 20 OwnerAPI tests and TypeScript passed. UI creation/configuration and calibration selection are still missing; this API foundation alone does not provide a complete launch-shooting flow.


### V24 session admission calibration hardening (2026-10-01)

Inspection found capture_policy calibration was checked only for ready artifact metadata during session creation/update. Replaced that gate with project-scoped resolve_v2_qualification for capture.assess/1, followed by the existing exact pinned-byte canonical calibration verifier matching model_lock_sha and dataset_sha. Missing Capture qualification now yields CAPTURE_UNAVAILABLE (503). Ordinary sessions with capture_policy=null retain their behavior. No model or policy was enabled.

v2sessions package tests and real-database session create/update plus Capture HTTP tests passed. The new qualified Capture calibration branch still requires a dedicated integration fixture; existing passing session fixture uses null capture policy. UI calibration selection/setup remains missing, and V24 remains incomplete.


### V24 Capture calibration admission integration (2026-10-01)

Extended session integration fixture: valid session.describe qualification without capture.assess qualification rejects Capture creation with CAPTURE_UNAVAILABLE and leaves zero session rows. In a separate rolled-back admin transaction, a synthetic capture.assess policy and project qualification permit captureReady under photo_api with exact canonical fixture bytes; modified storage bytes fail CALIBRATION_UNAVAILABLE. Distinct qualification report artifacts satisfy the trusted-report uniqueness invariant. Capture policy/qualification changes never commit. Test passed on PostgreSQL through 00308.

Evidence is scoped to admission and calibrated-byte validation with a fake object reader; it does not qualify a real model, dataset or GPU, nor establish complete Capture session creation with live object storage. UI setup remains outstanding.


### V24 calibrated-byte qualification binding regressions (2026-10-01)

Extended the rolled-back Capture admission fixture with three distinct artifacts whose byte lengths, canonical JSON and SHA metadata all match the fake object reader: wrong model_lock_sha, wrong dataset_sha, and qualified=false. All three are rejected as CALIBRATION_UNAVAILABLE under photo_api despite otherwise verified artifact metadata and valid project qualification. The PostgreSQL integration test passed. Test artifacts and synthetic Capture qualification are rolled back. This proves qualification binding at admission, not real model/dataset qualification or the complete UI/pipeline.


### V24 Capture session close/reopen client flow (2026-10-01)

Capture detail now retains the typed session returned by the initial scoped read and displays its localized state. Open/closed sessions expose close/reopen actions; archived sessions expose neither. Updates use the loaded decimal revision and unchanged name/session policy/capture policy, retaining a stable key for identical retries. Scope generation fences late responses. Successful state changes replace only session state, preserving shot draft edits. RU/BG/EN labels include session state/action/errors; list states are translated too. Required PATCH dependency is included in the client registry and actual server-router check.

TypeScript, 23 OwnerAPI/route/locale tests and Go route-registration check passed. These do not prove the rendered close/reopen flow or Capture-qualified live mutation. Server may reject updates when required qualification/calibration is unavailable; no bypass was added. Initial session creation and calibration preset selection remain missing. V24/full goal remain unfinished.


### V24 tenant metadata purge boundary and denied roles (2026-10-01)

Extended real-database current/historical Capture evidence fixture after run cancellation: a savepoint marks the synthetic tenant deleting and supplies a synthetic running cleanup lease. photo_api, photo_worker and photo_scheduler direct finish_tenant_deletion calls fail 42501; photo_maintenance with a wrong token returns false; the valid token completes purge. Verified zero remaining rows in capture_frame_confirmations, capture_assessment_jobs, capture_alerts, capture_receipt_assets, capture_receipts, capture_devices, shoot_sessions, project_persons, consent_grants, v2_workflows and artifacts. Tenant/job become deleted/completed and cleanup lease fields clear. Both subtests passed.

Purge and tenant status changes roll back before the existing lock-release test; only private test-database synthetic data is involved. This validates metadata deletion/FK ordering and role gates after a simulated storage-cleanup authorization. It does not verify actual object-store cleanup, shot-list purge rows absent from this fixture, GC reachability or production deletion. V24 acceptance is still incomplete.


### V24 explicit GC roots — migration 00309 (2026-10-01)

Found missing direct Capture roots in v2_artifact_has_references. Migration 00309 preserves prior V21/V22/V23 references and adds capture_alerts.report_artifact_id plus calibration refs in shoot_sessions.policy/capture_policy. This prevents report retention from relying solely on workflow links and prevents unadmitted session calibration from appearing unreferenced. Existing shot-list publication uses existing V2 document/ref roots.

Applied only to isolated PostgreSQL (now 309). Current/historical Capture fixtures explicitly assert the helper protects the manually inserted report and session calibration under photo_fn_owner; both passed, including metadata purge regressions. This is SQL reference-helper evidence; actual object deletion/GC claimant behavior and shot-list-specific fixture protection still need separate verification. Complete V24 remains unfinished.


### V24 actual GC claimant and legacy binding repair — migration 00310 (2026-10-01)

Actual claimant regression backdates a referenced QA report to 1900 and an otherwise unreferenced verified artifact to 1901 inside a savepoint. Initial run exposed legacy PL/pgSQL output-column ambiguity (tenant_id/artifact_id) in derivatives/exports/download_intents cleanup. Migration 00310 replaces those unqualified predicates with explicit table aliases, preserving existing GC candidate and detach rules and privileges.

Applied to private PostgreSQL, now 310. Both Capture fixture variants passed: maintenance claimant skips the older referenced report, leases the orphan as deleting, leaves report ready with no GC token, and subsequent purge checks pass. All claims/expiry changes roll back. No actual storage object was removed. This validates SQL selection and lease transition, not physical object cleanup, production concurrency or full V24 acceptance.


### V24 Capture device upload admission integration (2026-10-01)

Added PostgreSQL/photo_api device-upload service fixture: begin creates one multipart upload and reserves 1024 bytes/one file; identical replay reuses upload and storage begin call. Closing shoot session rejects a new begin. Incrementing device credential epoch rejects both an old-epoch begin replay and upload GET; a fresh-epoch principal cannot read the previous epoch's upload (404). Test passed through migration 00310.

Fixture uses synthetic device principal and fake multipart storage, with committed synthetic rows only in the private test database. It does not exercise signed-token HTTP transport, real storage PUT/completion, ingest verification, receipt adoption, or live watch-folder end-to-end. Those remain outstanding; V24/full goal not complete.


### V24 multipart bounds and device completion replay (2026-10-01)

Found part signing did not constrain numbers to the upload's computed PartsTotal. Shared validPartNumbers now rejects zero, out-of-layout numbers, duplicates and oversized lists before signing for both ordinary and device upload services. Device PostgreSQL fixture proves invalid part requests never reach the fake signer, valid part 1 succeeds, CompleteDevice queues one immutable part set, exact replay does not duplicate it, and old-epoch completion replay is rejected after credential rotation.

Full upload package tests passed with PostgreSQL through 310. Completion here queues the durable completion intent; fake storage does not verify actual part bytes or physical multipart completion/ingest. Live watch-folder-to-storage-to-receipt/QA remains unfinished.


### V24 signed device challenge/token HTTP integration (2026-10-01)

Real router/photo_api/PostgreSQL fixture registers an Ed25519 device using owner authorization, obtains a public challenge, signs its canonical payload with configured CaptureOrigin, exchanges it for a capture token, and rejects challenge replay (401). Capture token cannot enter user API; user token cannot enter device receipt API (401). Valid capture token reaches receipt payload validation (422 for intentionally empty body). Owner device revoke invalidates the same token on the next device API call (401). Test passed through migration 310; shared HTTP fixture now explicitly configures CaptureOrigin.

This validates signed credential/authentication boundaries via httptest, not OS keychain signing, live studio-agent interoperability, multipart storage or successful receipt ingestion. Complete V24/full implementation remains unfinished.

### Capture challenge origin and expiry HTTP verification

`TestIntegrationCaptureCredentialsHTTP` additionally verifies with real Ed25519 signatures and the real router against the private PostgreSQL fixture (migrations through 00310): a signature bound to another server origin returns 401; the same challenge remains usable with the correct origin after that failed signature; a correctly signed but expired challenge returns 401. Existing single-use replay and device-revocation checks remain in the same test. Passed with `go test ./internal/httpapi -run '^TestIntegrationCaptureCredentialsHTTP$' -count=1`. Expiry is advanced only on the synthetic fixture row, without waiting or changing production time handling. This is scoped credential validation, not completion of V24 or hardware/agent interoperability qualification.

### Capture draft-preserving manual refresh and shared commands

The Capture screen's manual refresh now reads session state and alert deltas without reinitializing shot-list state, replacing its draft or advancing its shot-list CAS revision. A failed initial load remains retryable via the initial loader; successful initial loads use the independent read path. Manual requests have an AbortController cancelled on scope changes/unmount, with generation checks before state writes. Existing alert polling and SSE remain independent. Added shared typed `commands.ts` IDs/labels/bindings and web `useCommands` lifecycle dispatch: Ctrl/Cmd+S saves, Ctrl/Cmd+Shift+R refreshes. Labels resolve through existing RU/BG/EN catalogs; handlers read current render closures, suppress auto-repeat/composition/already-handled events, and clean up the listener on unmount. Photographer TypeScript check and six focused command/alert-delta tests passed. Native keyboard bindings, full rendered draft-retention/CAS acceptance, and the rest of the planned command table remain unverified/unimplemented; this does not complete V24.

### Capture shot-list conflict comparison

Capture now handles `REVISION_CONFLICT` and `CONSENT_REVOKED` separately from generic save failures. A revision conflict retains the editable local draft and reads the current shared list for a read-only comparison. Saving is blocked while that comparison is pending. Explicit choices either discard the draft in favor of the shared list or retain the draft with the newly read CAS revision for a subsequent explicit save; neither choice writes to the server. A later competing edit remains protected by the existing CAS. A failed comparison read retains the draft and permits a retry; revoked-consent recovery blocks the editor and clears the no-longer-authorized draft through the existing recovery path. Scoped generation checks prevent late reads from applying to another screen scope. Added RU/BG/EN conflict and choice text, and corrected the generic save error's obsolete advice to refresh a revision. Photographer typecheck and six existing shot-list recovery/command tests passed. Rendered concurrent-editor acceptance and full offline draft persistence remain outstanding; these checks do not prove V24 completion.

### Capture persisted device drafts

Added bounded, versioned shot-list draft persistence through AsyncStorage with origin/user/tenant/session isolation. Restoring requires successful current session, shot-list, people and `/me` reads; cached data is not itself authorization and no offline-only initial access is enabled. The original CAS revision is retained; revision drift opens the existing explicit comparison flow. Drafts with now-unavailable person bindings or blocked consent are discarded. Only changed drafts are persisted, while saved/shared-equivalent drafts are removed. Serialized cross-instance storage operations prevent a late write from resurrecting a discarded draft. Parsing rejects malformed/oversized data, invalid bounds/duplicates, revisions above the supported safe-integer range, future timestamps and entries older than seven days. Storage failures display RU/BG/EN guidance to keep the screen open. Photographer typecheck and ten focused storage/recovery/command tests passed. This is restart recovery following a successful authorized load, not full offline cached-proxy access or complete revocation-cache invalidation across the product; rendered restart and concurrent-edit acceptance remain outstanding.

### Capture draft logout cleanup

OwnerAuth now invokes an injected local-data cleanup when credentials are cleared; the app provider wires this to serialized removal of Capture draft keys for its API origin. Other server origins and unrelated preferences are retained. An origin epoch invalidates pre-logout store instances immediately, so queued or later writes from an old screen cannot recreate a draft after cleanup. New stores created after a new authorized load can persist normally. Credential state is cleared synchronously before cleanup awaits. Photographer typecheck and seven focused auth/storage tests passed, including origin isolation, preserved locale preferences and stale writer fencing. Initial access-denial cache deletion and complete cross-product cache invalidation remain outstanding; this is not proof of full consent lifecycle/offline acceptance.

### Capture session access-denial cleanup

Added session-scoped draft invalidation independent of origin-wide logout cleanup. Confirmed 401/403/404 responses from initial Capture reads, manual refresh, alert polling and shot-list save/conflict recovery clear visible session data, stop exposing the draft and enqueue deletion of persisted keys for that origin/tenant/session. Session epochs prevent stale stores from recreating denied-session drafts; other sessions remain writable. Transport failures and server errors preserve drafts for retry. Cleanup failures are surfaced and fenced to the current screen generation. Photographer typecheck and twelve focused storage/auth/recovery tests passed, including denied-session isolation and stale-writer rejection. Cross-tab synchronization, event-driven consent cache invalidation, and rendered access-revocation acceptance remain outstanding; this is scoped Capture behavior, not full lifecycle qualification.

### Studio Agent queue status

Added `Journal.summary` and `photo-studio-agent status --journal ... --device-id ...` for local operator queue visibility. A single SQLite aggregate snapshot returns device-scoped state counts, queued bytes, due transfers and oldest pending age without file paths, source hashes or credentials. CLI validates the device UUID and requires an existing journal, without loading the device key or making network calls. All sixteen Studio Agent tests passed, including actual SQLite fixture isolation, durable retry due times and discovery age. This is queue observability; server QA latency, live agent/cloud interoperability and camera acceptance remain separate outstanding requirements.

### Studio Agent explicit failed-capture recovery

Added `photo-studio-agent retry` backed by `WatchFolder.retry_failed` and transactional `Journal.retry_error`. Only hashed failed entries with unchanged source bytes/metadata through the symlink-safe reader return to `hashed`; missing/changed sources remain failed. Existing sequence, upload binding, multipart checkpoints and retry history are preserved, while stale due delay and last error are cleared. Recovery does not contact the API, allocate a second upload, modify originals or bypass device authorization. All eighteen Studio Agent tests passed; a real-file/SQLite fixture proves multipart recovery uploads only the missing part using the existing completion identity, and a changed-source fixture proves recovery refusal. Capture timestamps remain explicitly unknown (`captured_at=null`) rather than inferred from file mtime. Live agent/server receipt interoperability and hardware qualification remain outstanding.

### Studio Agent single-writer journal ownership

Added `JournalLock` around CLI `run` and operator `retry` before journal open/recovery. The canonical journal path shares one nonblocking OS lock; closing/exiting releases ownership without stale PID recovery or unlink races. `status` remains available concurrently. All twenty agent tests passed on this host, including an actual second Python process rejected while ownership is active and successfully acquiring after release, plus exception cleanup. Windows locking is implemented through msvcrt but not platform-qualified here; native camera and full cloud interoperability acceptance remain outstanding.

### Studio Agent receipt admission binding

Hardened HTTP receipt validation against the actual response fields emitted by migration 00266: canonical receipt identity, resource type and exact tenant/project/session/device/sequence/content SHA/upload binding. Transfer independently requires the receipt upload ID to match the journal before marking done. All twenty-two agent tests passed, including per-field response mismatches and a mismatched-upload receipt that leaves the journal in `receipt` rather than `done`. The protocol test uses injected HTTP responses; it does not substitute for live server/agent interoperability or full cloud acceptance.

### Live Python Studio Agent / Go Capture credentials interoperability

Extended the private-DB credentials HTTP fixture with a real loopback httptest server and a child Python process running Studio Agent HTTPClient with cryptography Ed25519. Go registers the actual device public key; Python signs the real challenge, exchanges it for a real capture JWT, and submits an invalid receipt that reaches authenticated server payload validation (422 VALIDATION_FAILED). No HTTP response/signer mocks are used in this subtest. The synthetic seed is passed only through child stdin, never command arguments or output. Declared cryptography/keyring dependencies were installed into ignored repository-local `.runtime/studio-agent-venv`; cryptography 46.0.7/keyring 25.7.0 were used. `TestIntegrationCaptureCredentialsHTTP`, including the Python subtest, passed against private PostgreSQL through migration 00310. Default runtime points to that local environment; PHOTO_TEST_STUDIO_PYTHON supports an explicitly provisioned runtime. Full successful multipart storage/receipt/QA interoperability remains outstanding, as does OS keychain acceptance.

### Live Python receipt creation, sequence deduplication and QA enqueue

Expanded the live Python/Go/private-PostgreSQL credentials fixture to submit a successful receipt against committed synthetic verified upload/ingest metadata. The actual Python HTTPClient validates the server receipt binding and replays the same idempotency key; a second key with the same device sequence resolves to the same receipt, while changed content SHA on that sequence returns 409. Database assertions prove one receipt-to-version binding and one queued capture assessment despite replays. Test passed through migration 00310, including the real Python challenge signature/token exchange. The fixture supplies synthetic capture policy and verified storage/ingest records; it does not prove production policy qualification, actual multipart bytes/object verification, ingest execution or GPU QA completion. V24 remains incomplete until those and client/hardware acceptance requirements are met.

### Photographer Capture session creation from an existing policy

Added a RU/BG/EN Capture creation form using the existing CreateSession endpoint. The photographer names the session, selects an authorized project collection and an existing session whose grouping/Capture policies are reused. Collections are paginated with repeated-cursor protection; session templates come from the loaded paginated session list. The selected template is read before admission and must belong to the same project and have a Capture policy. A stable request/key is retained across retries with unchanged form selections, so a changed template after a lost response cannot silently alter the retry body. The server remains responsible for qualification/calibration/ACL validation. Scope-generation checks prevent late responses from navigating another scope. Successful creation adds the session and opens its Capture screen. Route capabilities declare the four consumed endpoints. Photographer typecheck and two route contract tests passed. Initial operator policy/calibration provisioning, rendered creation/retry acceptance and fully qualified positive server admission remain outstanding; this reuse flow does not satisfy first-session provisioning by itself or complete V24.

### Calibration normalized-loss invariant

Go session admission and the shared worker Calibration contract now enforce the documented `loss_weights` sum of 1 within 1e-9, in addition to individual bounds. Corrected invalid synthetic session fixtures and added rejection checks for zero, underweight and overweight vectors. The full Go v2sessions package passed against private PostgreSQL through migration 00310; eleven session describe/adapter Python tests passed. A broader run of `test_v2_learning_fit_adapter.py` exposed seven failures before calibration validation because its existing TrainingManifest fixture lacks current frozen-document/descriptor bindings (`base_document`, `approved_document`, `descriptor_shard`, `descriptor_analysis_id`, `descriptor_ordinal`). Those Learning fixture/acceptance failures remain unresolved and must not be treated as green. This change does not create qualified calibration artifacts or claim photographic qualification.

### Learning fit fixture contract recovery

Resolved the seven earlier `test_v2_learning_fit_adapter.py` fixture failures by supplying the current mandatory frozen base/approved document refs and descriptor shard/analysis/ordinal bindings. Fixtures now supply a real typed pre-edit DescriptorShard and derive scene12 feature vectors from its independently fixed fixture measurements. Worker artifact fixtures serve those typed shards without bypassing `validate_training_descriptor`. Added a negative case that tampers with a train row's features while retaining the original descriptor and proves INPUT_INVALID before candidate staging. The combined Learning fit/session describe/session adapter run passed nineteen tests. This supersedes the seven fixture failures recorded above; it is scoped synthetic worker acceptance, not a full photographic/model qualification or V23/V24 completion claim.

### Capture/Session worker regression and creation UI pending state

Capture report tests were deriving semantic Calibration values from structural-only fixtures; they now explicitly supply normalized loss weights when constructing an eligible calibration. Combined Capture, Session and Learning fit adapter tests passed 61 tests. No threshold or unavailable/model behavior was relaxed. The photographer creation form now reports its submitting state to the session list, disabling the form cancellation button while the create request is pending; success clears that state and opens the returned session. TypeScript and focused route/storage verification are recorded separately from rendered/network acceptance. Full V20–V32 implementation remains incomplete.

### Server-authoritative Capture confirmation display

ListAlerts now returns the typed CaptureAlertPage/View projection with nullable confirmation_id, joined only to the exact tenant/project/session/receipt/alert/version immutable confirmation under existing project ACL checks. Photographer initial/manual/poll reads restore confirmation display from that field rather than a transient local Set. The merge handles confirmation appearance at an unchanged alert revision and retains known confirmation through stale reads/ack responses; immediate confirm success merges the returned ID. Public private-quality/evidence fields remain excluded. Real private-PG Capture alert HTTP/ACL integration passed with confirmation projection assertions; photographer typecheck and nine focused merge/delta tests passed. Documentation regenerated deterministically with 2059 static checks, 164 schemas and 54 semantic tests; client manifest regenerated. Rendered restart acceptance, live hardware/GPU qualification and full V24 completion remain outstanding.

### Loaded Capture alert window refresh

The alert reader now continues to the oldest displayed alert rather than stopping at the first overlapping ID, so immutable confirmation changes and acknowledgement revisions on older loaded pages are refreshed. Initial empty reads still fetch only one page, leaving older unloaded history for explicit pagination. Explicit pagination now uses revision-aware merge rather than dropping repeated IDs, retaining immutable confirmation state through overlapping responses. Photographer typecheck and ten focused refresh/merge tests passed, including an older-page confirmation update and cancellation/looping cursor behavior. Refresh request cost scales with the displayed window; large-history load/performance and rendered acceptance remain outstanding.

### Capture acknowledgement retry identity

Photographer acknowledgement now retains an idempotency key per alert/revision across failed replies. A repeat for the same requested CAS revision uses the same key; a changed revision starts a new attempt. Keys reset with screen scope, and already acknowledged alerts do not send another mutation. This avoids a lost successful response being retried under a fresh key with the stale revision. The full photographer suite passed 63 tests; TypeScript passed after the change. Private-PG Capture alert HTTP (including acknowledgement replay/CAS/ACL) and live Python credentials/receipt integration passed together. A rendered dropped-response UI test remains outstanding; these are scoped checks, not full V24 completion.

### Qualified Capture service admission and V2 client envelope decoding

Expanded the private-PG session service fixture to commit synthetic Capture qualification only in the isolated test DB, invoke the real Create/Update service through photo_api, verify successful Capture policy/calibration binding, identical create replay, one resulting Capture session, close success and stale-revision refusal. Only a fixture-owned synthetic global Capture policy is disabled at cleanup. The v2sessions package passed. This is admission logic with synthetic qualification/storage data, not real model qualification.

The positive fixture exposed a client compatibility defect: V2 sessions and project persons are documented Resource envelopes (`id`, `revision`, `document`, `data`), whereas Capture consumers had assumed flat rows. Added typed OwnerAPI decoding for session reads/list/create/update and person lists; updated Capture session list/detail/template creation to use those methods. The decoder checks row/envelope ID, revision and resource type plus expected requested ID. Revision strings retain integer precision. Recovery needing only the envelope revision remains unchanged. Full photographer typecheck and 65 tests passed, including envelope mismatch rejection and created Capture policy extraction. Rendered end-to-end acceptance remains outstanding.

### Rendered Capture creation retry and accessibility

Added jsdom/React Testing Library acceptance using actual React Native Web components and OwnerAPI envelope decoding. The rendered flow selects collection/template, enters the session name, loses the first committed-create response and retries; assertions prove unchanged body/idempotency key, one template fetch, decoded created policy data and balanced pending-state notifications. A second rendered case refuses a template without Capture policy before any create submission. The test exposed missing web radio-state output; collection/template options now set explicit aria-checked plus native accessibilityState.checked. Added test-only dependencies and type declarations in the photographer package; npm cache for the follow-up install is repository-local. Full photographer suite passed 67 tests and TypeScript passed. Transport responses in these DOM tests are injected; actual Go/PostgreSQL admission is covered separately, and native/browser visual acceptance plus real model qualification remain outstanding.

### Rendered Capture draft preservation, CAS resolution and restart

Added DOM acceptance using actual CaptureSession, LocaleProvider, React Native Web and OwnerAPI, with injected API transport/storage. The first scenario edits a shot label, switches RU/EN without reloading or losing edits, manually refreshes while preserving the draft, receives REVISION_CONFLICT, compares the readonly shared list, verifies save disabled until explicit choice, keeps the draft without a write, then saves against the newly chosen CAS revision with a new request key. The second scenario waits for actual draft-store persistence, unmounts/remounts after a server revision change and verifies draft restoration with comparison/save blocked and no automatic PUT. Full photographer suite passed 69 tests and TypeScript passed. These prove rendered client transitions with mocked transport/AsyncStorage; actual native storage crash behavior, browser visual layout, cross-tab lifecycle and qualified cloud/GPU acceptance remain outstanding.

### V25 LensProfile and normative tiled map foundation

Started V25 with the exact documented LensProfile worker contract and a tiled inverse-map algorithm following section Lens of 16-imaging-algorithms.md: radial/tangential distortion, strength interpolation, per-channel CA scales and vignette gain with .25 denominator floor. Maps contain input pixel centers as float32; metadata matching requires exact camera/lens and focal/aperture within 1e-6, with no profile interpolation. Two actual NumPy tests passed for identity centers, strength-zero maps, tile-invariance and mismatched metadata refusal. This is an unregistered algorithm foundation; it does not yet perform pixel/alpha resampling, Jacobian/inverse-point validation, WorkingImage dense-warp publication, trusted profile qualification, API admission, GC/purge integration or camera qualification. V24 acceptance gaps remain tracked and neither package is complete.

### V25 lens geometry validity and source correspondence

Added a shared float64 lens point map used by tile generation, positive-Jacobian validation on a 33x33 grid including canvas corners, and bounded Newton inversion for forward source correspondence (central difference 1e-3px, residual tolerance 1e-4px, at most twenty updates, singular determinant threshold 1e-10). Folded maps are refused before the first tile; forward correspondence returns unmeasurable for a folded/unresolved/outside point. Three NumPy tests passed, covering nonlinear round-trip, outside-domain refusal, folded-map rejection, zero-strength identity and tile invariance. This establishes geometry primitives only; actual RGB/alpha/mask resampling, dense-warp artifact publication and runtime/profile qualification remain outstanding.

### V25 — bounded linear lens resampling (2026-10-01)

Implemented `resample_lens_tiles` and `LensImageTile` in
`workers/photo_worker/algorithms/lens.py`. Float32 linear RGB permits negative
and HDR values; alpha must be finite in [0,1]. Bilinear sampling gathers and
interpolates premultiplied neighbors before unpremultiplying. Intermediate
allocations remain tile-sized, including input validation, and inputs may be
disk-backed ndarrays. Chromatic channels use their respective maps; output
opacity is the minimum of their sampled opacities, conservatively suppressing
transparent fringes. Any channel outside pixel-center coverage produces zero
output alpha and RGB and sets the tile's `uncovered` flag. Vignette gain does
not clamp HDR. Strength zero copies source pixels exactly, including hidden
RGB, and bypasses geometric rejection. Input buffers are never mutated.

Verification: `PYTHONPATH=workers:src .venv-platform/bin/python -m pytest
 tests/unit/test_v25_lens_map.py -q`: **6 passed**. Includes map/forward geometry,
metadata binding, hidden-color exclusion, HDR preservation, exact zero-strength
bypass, tile invariance and uncovered canvas. This is algorithm-level evidence;
V25 operation registration, artifact-backed qualification, transform/mask
propagation, scheduling and end-to-end acceptance remain outstanding. No
photographic quality qualification or entire-package completion is claimed.

### V25 — reserved disk-backed lens preparation (2026-10-01)

Added `operations/optical/prepare.py`: `prepare_lens` produces attempt-owned
float32 NPY RGB, alpha and green inverse-field files under a single scratch
reservation (24 bytes/pixel plus bounded header allowance). Pixel intermediates
stay tiled; all output mappings close before the caller stages files. The
reservation remains alive while the context yields and removes outputs on exit,
including cancellation and exceptions. Geometry generation uses the same green
map as correction, and the output carries an uncovered-area flag. Artifact
publication and transform-chain construction are intentionally still pending.

Verified actual files through the worker's strict `read_array` and
`read_inverse_field` readers, exact zero-strength pixels/coordinates, cleanup on
normal exit and cancellation, and quota refusal without leaked files. Additional
algorithm evidence covers channel-specific CA coverage and unclipped vignette
HDR. Scoped command: `PYTHONPATH=workers:src .venv-platform/bin/python -m pytest
 tests/unit/test_v25_lens_map.py tests/unit/test_v25_lens_prepare.py -q`:
**9 passed**. This does not qualify full V25 execution or photographic quality.

### V25 — green-geometry probability mask sampling (2026-10-01)

Added `resample_lens_mask_tiles`: float32 finite probability masks use the same
half-pixel green inverse map as RGB geometry, zero outside coverage, without
chromatic-channel displacement or vignette gain. Validation and working arrays
are tiled. Added strict `OpticalLensParams` matching the generated parameter
schema, including `VersionBinding` and bounded focal/aperture/strength.

Inspection found an integration blocker: existing `algorithms/mask_transform.py`
consumes dense fields as zero-based sample indices, while normative lens fields
contain pixel centers (x+.5,y+.5). Lens is not registered prematurely: a complete
consumer compatibility solution must precede artifact publication and pipeline
admission. Current lens-specific mask sampler is tested independently; it does
not establish compatibility of all existing dense-field consumers.

Scoped verification: lens map/preparation suite **10 passed**, including exact
identity masks, tile-invariant nonlinear masks, zero uncovered mask samples,
rejection of NaN probability input, and independence from CA/vignette. Full V25
and V20–V32 completion remain unproven and incomplete.

### V25 — dense-field convention compatibility (2026-10-01)

Resolved the previously found mask-consumer coordinate mismatch with an explicit
optional `DenseWarpTransform.pixel_centers=half_pixel` marker. Absence preserves
legacy integer-index field semantics and serialization, including chain hashes.
Explicit null and unsupported marker values are rejected. The Python JSON
Schema exposes an optional string constant, not a nullable field. New lens
transforms must carry the marker; normative imaging documentation records it.

The shared mask consumer subtracts .5 for marked fields and enforces source
pixel-center coverage (zero outside). Legacy body fields and transforms are
unchanged. Go review parsing accepts marked dense transforms, rejects invalid
conventions, and still verifies canonical hashes. Regenerated baseline schemas
and passed the contract drift check.

Evidence: Python scoped mask/body/lens suite **22 passed**, including legacy
hash/serialization round-trip, old body adapter behavior, half-pixel identity
mask, and actual nonlinear lens-field sampling through the shared consumer
matching the lens-specific sampler. `go test ./internal/review` passed, including
new marker validation. This resolves this consumer's raw-coordinate mismatch;
full operation publication, source-correspondence integration and photographic
acceptance remain outstanding, so V25 is not complete.

### V25 — lens output geometry and result contracts (2026-10-01)

Added worker-side `ImageResult`/`ImageReport` contracts matching the v2 schemas
and `optical/result.py:lens_working_image` for immutable staged float32 linear
artifacts. Nonzero correction preserves source identity and dimensions, creates
a stable lens canvas identity from source canvas/chain and field ref, appends a
partial dense warp with `pixel_centers=half_pixel`, and retains output alpha as
coverage. It does not invent a projective transform or crop. Zero strength
retains the original canvas and transform chain. Encoded or non-float32 images
are rejected at this construction boundary, pending explicit working-space
conversion in the eventual adapter.

Scoped lens suite **11 passed**, including WorkingImage JSON round-trip,
explicit inverse-field binding, stable canvas identity and identity-chain
preservation. These are construction/algorithm tests; artifact publication,
qualified profile admission, metadata binding and complete worker registration
are still pending. No full operation readiness is claimed.

### V25 — exact changed-mask output (2026-10-01)

Lens preparation now reserves and writes a fourth float32 NPY artifact,
`changed_mask`. Each output tile compares RGB and alpha bit representations
against its source tile; changed pixels are exactly 1, all others exactly 0.
This includes alpha-only and signed-zero changes, without approximate float
thresholds. The combined reservation is now 28 bytes/pixel plus 16384 bytes
header allowance. All four files share the same cancellation/cleanup lifecycle.

Scoped lens suite **12 passed**, including actual changed-mask files compared
to independent bitwise RGB/alpha differences and zero masks for exact bypass.
Full worker publication/admission remains pending. The zero-strength operation
adapter must return the original WorkingImage ref, as required by the normative
contract; preparation itself only produces exact copied arrays.

### V25 — staged lens result closure (2026-10-01)

Implemented `stage_lens_result`: validates VersionBinding source ref and canonical
canvas hash before staging, stages pixels/alpha/warp field and marked WorkingImage
for nonzero correction, plus changed-mask array/MaskRef, ImageReport and ImageResult.
No generated pixels or invented homography are reported. Uncovered correction
requires review. Exact zero strength reuses the original WorkingImage ref and
stages only zero changed-mask and report/document artifacts. This helper does
not adopt or register operations; caller must supply verified preparation output
and perform profile qualification and metadata checks beforehand.

Scoped lens suite **13 passed**, using real prepared NPY files and a staging test
double to inspect roles/contracts, zero-strength ref reuse, marked geometry,
review status and binding refusal before any staged write. Real object-store
publication, output adoption and scheduler integration remain unverified.

### V25 — executable/normative optical contract parity (2026-10-01)

Added schema-backed round-trip tests for OpticalLensParams, LensProfile,
ImageResult and ImageReport. Representative worker documents validate against
normative v2 Draft202012 schemas, and both boundaries reject unknown fields.
Combined optical/lens suite **17 passed**.

Inspection of `v2/adoption.py` identified a remaining integration conflict:
role policies require staged working_image/pixels even for normative zero-strength
reuse of an input WorkingImage. `stage_lens_result` correctly reuses the input,
but is not yet registered. Before registration, adoption policy/compiler must
support the exact bypass without allowing missing outputs for ordinary nonzero
execution. This finding prevents a false readiness claim; publication remains
incomplete.

### V25 — ordinary lens closure passes adoption validation (2026-10-01)

Added typed output bindings for ImageResult, WorkingImage pixels/alpha/dense
field, and MaskRef pixels/dense field. Adoption loads portable baseline image
schemas under Baseline-prefixed internal definition names. This avoids a real
collision: v2 Rect is normalized x0/y0/x1/y1 while baseline Rect is pixel x/y/
width/height. Existing v2 schema definitions are not overwritten.

Staging tests now hash actual NPY/JSON bytes and feed the full nonzero lens
closure through production `prepare_v2_adoption`, verifying inventory reachability
and required roles. The input source ref is retained in the inventory. Scoped
lens-result plus existing adoption suite **9 passed**. No live DB/object-store
adoption was run. Zero-strength role-policy conflict remains unresolved and
lens is not yet registered; the passing result proves ordinary nonzero closure
preparation only.

### V25 — evidence-backed zero-strength eligibility (2026-10-01)

Added `v2/lens_bypass.py:verify_lens_identity`. It consumes a params ref supplied
by authoritative task metadata, verifies declared-input evidence and exact JSON
bytes SHA, requires strength zero and original WorkingImage ref/canvas binding,
and validates pass report plus changed-mask geometry. It computes the canonical
float32 zero NPY SHA in bounded 64KiB chunks with cancellation checks and rejects
any changed mask whose SHA/size/media/role differs. No worker-provided boolean
can assert identity. This helper deliberately does not relax adoption policies
until wired to authoritative task params in the coordinator.

Scoped verifier test passed with actual np.save zero/nonzero bytes; nonzero
strength, nonzero mask and tampered params bytes were refused. This is verifier
coverage, not live DB/object-store identity adoption or operation readiness.

### V25 — identity guard connected to adoption (2026-10-01)

`prepare_v2_adoption` now accepts an authoritative params ref for optical.lens.
Only after immutable params strength zero and the full identity verifier pass
may staged working_image/pixels requirements be omitted. Missing params retains
normal required-role enforcement; nonzero/tampered changed masks are rejected.
Coordinator retrieves the ref through new scheduler-only SECURITY DEFINER
`v25_lens_adoption_params` (migration311), constrained by valid adoption context,
operation/version, ready params and exact task-input SHA. Migration311 has been
written but has not yet been applied or tested on PostgreSQL.

Input refs may occur through both typed WorkingImage and generic VersionBinding
source paths; adoption permits this alias only for authoritative input evidence,
while retaining rejection of contradictory concrete types and output types.
Scoped identity/result/adoption suite **11 passed**, including identity refusal
without params, valid verified bypass, and nonzero-mask rejection. Live
coordinator/SQL execution and full worker operation remain pending.

### V25 — migration311 verified on isolated PostgreSQL (2026-10-01)

Applied forward-only migration311 to the private synthetic `photo_v24_check`
database (previous version310). Live privilege test verifies SECURITY DEFINER,
photo_fn_owner ownership, pinned search_path, scheduler execute access, photo_api
execute denial, and no params for a nonexistent context. Test command using
PHOTO_TEST_DATABASE_URL: `tests/platform/test_v25_lens_adoption_migration.py`:
**1 passed**. No production or object-store state was touched. Positive lens
workflow/context and full coordinator adoption are still pending.

Also bounded lens identity NPY hash verification to 200 million pixels, matching
algorithm admission rather than allowing an enormous safe-integer Canvas to
cause an unbounded hash loop. Combined scoped optical/adoption/mask/body suite
ran 38 test cases; its final process completion is checked separately before
claiming a passing result.

Completion of the combined process was observed with exit code0:
**38 passed in 22.27s**. This scoped success does not prove V25 completion.

### V25 — adversarial identity-result verification (2026-10-01)

Expanded identity verification tests with correctly rehashed output documents,
not merely corrupt SHA evidence. The verifier rejects review status, unexpected
reason/operation, skipped work, nonzero clipping/outside-mask error, substituted
input image, and mask canvas mismatch. A valid but oversized Canvas is rejected
before zero-data hashing; cancellation is observed. JSON reads now enforce the
8MiB bound directly in the verifier as well as in its evidence-reader contract.

Scoped identity suite **11 passed** after test-fixture UUID serialization was
corrected. These tests strengthen the no-op exception, but do not replace the
still-pending positive live coordinator/workflow and complete lens adapter.

### V25 — unclipped optical working-transfer boundary (2026-10-01)

Added optical-local gamma24/linear conversion functions, retaining source
primaries/ICC and signed HDR values rather than changing existing color helpers.
Signed power is specified in normative imaging math, invertible for negative
channels and unclipped above1. Linear input is copied exactly; invalid dtype,
nonfinite input and float32 overflow are rejected. Helpers are tile-compatible,
but adapter integration and disk-backed encoded input/output conversion remain
pending. Zero strength must bypass conversion entirely.

Scoped color tests **2 passed**, covering signed-zero preservation, HDR and
negative round-trip, no source mutation, linear copies and overflow refusal.
No gamut conversion or photographic qualification is claimed.

### V25 — encoded working-image lens preparation (2026-10-01)

`prepare_working_lens` now handles source gamma24 primaries: tile conversion to a
reserved disk-backed linear RGB input, lens correction in linear space, then
in-place tile re-encoding into output RGB. It recomputes changed-mask from the
final source-encoded RGB and alpha bits. Source primaries/ICC are retained;
no gamut conversion or HDR clamp is introduced. Exact strength zero bypasses
both transfer functions. Linear sources use the existing preparation path.
The additional gamma input reservation is 12 bytes/pixel plus bounded header
allowance, alongside the existing output reservation.

Output construction/staging permits validated float32 gamma24 WorkingImage as
well as linear, because the prepared pixels now retain source encoding. Scoped
prepare/result/color suite **8 passed**, including actual disk files compared
to an independent linear correction plus re-encoding, exact zero-strength
bits/mask and removal of both input/output temporaries. Lens profile admission,
metadata binding, nullable/uint16 source-alpha normalization and complete worker
registration remain outstanding; no full operation completion is claimed.

### V25 — source alpha preparation wired into lens math (2026-10-01)

Added `normalized_alpha`: missing alpha becomes reserved disk-backed opaque
float32, uint16 becomes exact float32 division by65535 in tiles, and existing
float32 opacity is bounded/validated without mutation. Scratch ownership, quota
and cancellation cleanup follow the same attempt lifecycle.
`prepare_source_lens` binds RGB shape and alpha presence to source WorkingImage,
then connects normalization to gamma/linear lens preparation. Nonzero output
alpha is the v2 float32 opacity format; compatibility of other legacy consumers
with that format still needs explicit adapter/pipeline validation.

Scoped alpha/preparation suite **7 passed**, including real legacy gamma24 +
uint16 source alpha, HDR visible color, hidden transparent RGB suppression,
missing-alpha mismatch refusal, cancellation/quota cleanup and exact conversion.
Qualification/metadata admission and complete worker registration remain pending.

### V25 — identity consumer accepts v2 opacity (2026-10-01)

Added strict `read_alpha_array` for exactly legacy uint16 or finite unit-interval
float32 NPY. It retains exact-shape/header/EOF validation and does not normalize
or allocate full image copies. Only layout mismatch selects the alternative
encoding; corrupt file/value errors remain failures. Identity comparison now
uses this reader, normalizing uint16 only within each compared tile so equivalent
legacy/v2 opacities are not falsely reported as changed. Missing alpha is opaque.

Consumer plus existing identity adapter suite **4 passed**: both alpha formats,
invalid float64/out-of-range refusal, mixed-format equality and alpha changes,
and previous adapter behavior. Learning and other legacy alpha readers still
need explicit compatibility work before admitting lens outputs across pipelines.

### V25 — learning consumers accept float32 opacity (2026-10-01)

Holdout source now uses strict read_alpha_array. Holdout workspace preserves
float32 opacity and divides only uint16 by65535. Person-stat input validation
and bounded statistics/coverage accept finite [0,1] float32 alpha while retaining
legacy uint16 visibility semantics (alpha>0). Validation occurs in bounded
blocks, without full-frame conversion. Learning source/workspace/person suite
**9 passed**; additional statistics regression process is tracked separately.

The float32 workspace fixture stages a fresh immutable alpha/image reference;
no existing artifact bytes are changed in place. Legacy consumers outside these
learning/identity paths still require review before full lens pipeline admission.

Additional alpha-consumer/workspace/statistics regression process completed:
**16 passed in 26.43s**, including legacy statistics and float32 visibility
coverage equivalence plus NaN refusal. Full V25 readiness remains unproven.

### V25 — Capture proxy consumers retain float32 alpha (2026-10-01)

Capture preparation and assessment now map alpha through strict read_alpha_array.
Proxy pixel preparation accepts/validates both legacy uint16 and [0,1] float32;
resized/copy proxy opacity retains its dtype. Publication selects matching NPY
dtype and reservation byte count rather than casting v2 opacity to uint16.
Model preview visibility accepts float32 with finite/range checks. Proxy binding
also validates prepared opacity shape/dtype/range before upload.

Capture proxy/preparation adapter suite **11 passed**, including float32 alpha
copy/preview visibility and NaN refusal alongside legacy preparation behavior.
No new Capture model qualification or full lens-to-Capture workflow is claimed;
remaining export/render consumers still require audit before lens registration.

### V25 — export/preview accept opacity and HDR inputs (2026-10-01)

Export encoding and media derivative readers now accept strict uint16/float32
alpha and normalize only uint16. Finite HDR RGB is admitted, with explicit [0,1]
clamp immediately before presentation uint16 quantization/ICC conversion. This
prevents integer overflow/wrap for same-size output while retaining HDR values
in source artifacts. Working lens and processing math remain unclipped.

Actual ICC-managed JPEG/PNG/media suite **14 passed**, including PNG transparency
with both alpha formats and RGB below0/above1 producing black/white rather than
wrapped values. Previous media-only suite also completed **6 passed**. Full lens
qualification, operation registration and end-to-end workflow remain outstanding.

### V25 — lens worker execution adapter foundation (2026-10-01)

Added optical adapter execution connecting params/source/profile contracts,
canonical canvas binding, authoritative camera/lens/focal/aperture admission,
strict NPY source mapping, source-alpha/color preparation and staged result.
It returns the single ImageResult root for v2 adoption. CPU renderer v2.2 is
required; without an injected authoritative admission provider it fails
LENS_PROFILE_UNAVAILABLE before reading inputs. There is no permissive default
or runtime registration yet.

LensAdmission specifies required qualification/metadata bindings, but its real
DB/artifact-backed implementation is outstanding. Scoped fail-closed adapter
test **1 passed**; positive execution and publication are still unverified.
Existing algorithm and staging tests do not establish full adapter readiness.

### V25 — positive lens adapter execution evidence (2026-10-01)

Adapter tests now execute actual gamma24 NPY source files through strict readers,
opacity preparation, linear correction/re-encoding and staged result construction
for strength0 and1. They verify single root result, original WorkingImage reuse
for zero strength, source color preservation, half-pixel dense geometry, uncovered
review and scratch cleanup. Wrong authoritative camera metadata is refused before
any stage. Admission and object publication are test doubles, so this is not
actual profile qualification or object-store workflow acceptance.

Added exact staged byte accounting (NPY sizes plus canonical JSON bytes) to lens
result construction and OperationResult telemetry. Adapter/result suite
**5 passed**. Real admission provider and runtime/scheduler registration remain
outstanding; overall V20–V32 goal remains active and incomplete.

### V25 — transparent export/preview resizing (2026-10-01)

Added presentation RGBA resize: translucent geometry resizes linear premultiplied
RGB with alpha, then unpremultiplies above1e-6 and encodes back. Working gamma24
and decoded sRGB use their explicit transfer functions. Presentation input/output
is clipped to display range; source artifacts remain untouched. Missing/opaque
alpha retains previous resize behavior, and same-size output retains exact input
values. Export and both media derivative source paths call this helper.

Export/media/transparency suite **17 passed**; after reducing helper temporaries
to float32 and clipping before display transfer, transparency suite **3 passed**.
Tests verify hidden transparent color exclusion for gamma24/sRGB, legacy opaque
resize parity and unchanged geometry identity. This presentation helper still
uses frame-sized arrays as the existing export path does; disk/tile memory
admission for very large exports remains required and is not claimed complete.

### V25 — presentation frame-buffer RAM admission (2026-10-01)

Added a conservative source+target RAM estimate for current presentation buffers,
with larger allowance for translucent transfer/premultiplication and codec/ICC
buffers. Export and working-source media preview compare it to execution-profile
ram_bytes before source mapping/processing; malformed budgets are rejected.
Legacy profiles without estimates retain a bounded 512MiB compatibility budget.
Oversized requests fail RAM_ADMISSION_EXCEEDED rather than allocating unchecked.

Memory/export suite **9 passed**; existing media suite **6 passed**. Tests cover
exact budget boundary, alpha overhead, malformed boolean budgets and pixel cap.
This is admission for the current frame-buffer implementation, not a substitute
for planned disk/tile large-image export support. Large jobs need appropriate
planner reservations or the pending bounded codec path; encoded-master preview
predecode admission is also still outstanding.

### V25 — encoded-master RAM check before decode (2026-10-01)

Media master preview now inspects validated lazy libvips header dimensions/bands
and runs presentation RAM admission before write_to_memory pixel decoding.
The production execution path supplies the execution-profile budget and target
preview dimensions. Failed admission preserves RAM_ADMISSION_EXCEEDED rather
than collapsing it into an input error.

Media/memory suite **8 passed**. Actual WebP decoding verifies the admission
callback receives oriented dimensions/alpha; a header test double proves a
rejected request never calls pixel decode. This closes the previously recorded
predecode admission gap, but does not implement bounded tiled master decoding or
large export support; those remain outstanding.

### V25 — dense source correspondence primitive (2026-10-01)

Added `algorithms/dense_correspondence.py:dense_source_point`. It bilinearly
samples an immutable float32 dense field at output pixel-center coordinates,
converts historical integer-index stored fields to source centers, and preserves
marked half-pixel fields directly. Invalid layout/nonfinite sampled field values
are rejected; points outside declared valid region or source coverage return
unmeasurable, without clamping or guessed homography.

Scoped correspondence test **1 passed**, covering both conventions at corners
and fractional positions, out-of-canvas/uncovered source and NaN field refusal.
The primitive is not yet wired into comparison UI/metrics consumers, so this
does not establish full source-correspondence integration or V25 readiness.

### V25 — full transform-chain source correspondence (2026-10-01)

Added `chain_source_point`: validates chain continuity/hash and traverses crop,
resize, matrix and dense steps backwards. Public coordinates remain physical
pixel centers; matrix conversion respects existing integer-index convention,
resize border replication matches the shared mask consumer, and dense sampling
uses the explicit stored-field convention. Unsupported/malformed transforms
are not replaced with identity, uncovered points return unmeasurable, and each
step checks cancellation. Caller supplies SHA-verified field mappings.

Scoped chain/dense suite **2 passed**, including composed crop+resize+dense,
crop+resize+translation, boundary replication and uncovered matrix coordinates.
The comparison UI/metrics still need to call this helper with authoritative
field resolution; this is algorithm-level chain support, not complete V25.

### V25 — artifact-backed correspondence runtime scope (2026-10-01)

Added `runtime/correspondence.py:open_source_correspondence`, connecting the chain
algorithm to TaskArtifacts input and strict read_inverse_field mappings. Repeated
field refs are opened once, conflicting shapes are rejected, target canvas is
validated, cancellation is checked and the resolver cannot be used after scope
exit. An output point outside the image canvas is unmeasurable even for an empty
chain. Production TaskArtifacts remains responsible for declared-ref/SHA checks.

Runtime/chain suite **3 passed**, using actual NPY fields with a test input loader,
repeated refs, fractional coordinates and scope cleanup/use-after-close refusal.
Comparison UI/metrics integration, authoritative lens admission and complete
operation registration remain outstanding.

### V25 — pinned profile admission implementation (2026-10-01)

Added `PinnedLensAdmission` using operator-provided immutable pins for exact
qualification artifact, coefficient-document SHA (qualification ref excluded),
runtime lock and approved dataset SHA. Qualification is materialized through
TaskArtifacts declared-input/SHA verification; duplicate/absent pins refuse
admission. Trusted metadata resolver must return exact asset-version/source-bound
camera/lens/focal/aperture evidence; profile fields are never used as substitute
EXIF. Existing exact metadata matcher applies afterward.

Admission/adapter suite **4 passed**, covering exact match, changed coefficients,
wrong source version and ambiguous pins. Registry loading, actual installed
runtime verification, authoritative EXIF resolver and compiler input closure
wiring remain outstanding. Test pins/evidence do not qualify any real lens or
establish production operation readiness.

### V25 — installed runtime version verification (2026-10-01)

PinnedLensAdmission now requires an OpticalRuntimeLock and verifies its canonical
SHA plus actual CPython patch version, machine architecture, and installed
numpy/pydantic/rfc8785 distribution versions before accepting qualification pins.
Wrong hash or installed version fails admission. Runtime verification is no
longer just comparison of two caller-supplied SHA strings.

Scoped admission/runtime suite **2 passed** against the installed test interpreter.
This verifies version inventory only: exact dependency binary hashes, operation
code hashes, approved runtime registry and GPU/device qualification remain
outstanding. It must not be used as proof of full reproducible runtime or lens
production readiness; operation registration remains disabled.

### V25 — critical runtime byte hashes (2026-10-01)

OpticalRuntimeLock now requires named component SHA256 pins. Verification hashes
the installed Python executable, NumPy core binary, pydantic core binary,
canonical JSON implementation and lens algorithm/prepare/color/adapter/result
source files in bounded 1MiB reads, and rejects byte mismatch even when all
package versions match. Component paths are resolved by code, not supplied as
arbitrary paths in a qualification document.

Admission/runtime suite **3 passed**, including actual installed-file hashes and
an intentionally wrong algorithm hash with unchanged package versions. These
pins cover critical components only; full transitive dependencies/shared native
libraries, immutable deployment verification and approved qualification registry
remain outstanding. No installed environment is declared qualified by these
tests, and lens registration remains disabled.

### V25 — immutable operator registry loader (2026-10-01)

Added LensRegistry/LensRegistryPin and `load_lens_registry`: reads a regular
single-link non-symlink file under1MiB, checks operator-configured raw-byte SHA,
rejects duplicate JSON keys/unknown schema fields and inconsistent/duplicate
profile pins, and verifies the installed runtime before returning admission pins.
An empty registry is valid and admits no lens; no test profile is auto-approved.

Registry/admission suite **4 passed**, including actual file/runtime loading,
wrong digest, changed bytes and symlink refusal. CLI/service configuration,
trusted EXIF resolver and task/compiler closure wiring remain outstanding.
This loader does not supply photographic qualification or mark V25 complete.

### V25 — worker lens registry configuration (2026-10-02)

WorkerConfig now accepts paired PHOTO_LENS_REGISTRY and
PHOTO_LENS_REGISTRY_SHA256 on CPU workers only. Startup rejects missing pairs,
symlinked/noncanonical paths, invalid registry bytes and unqualified runtime
inventory; validated LensRegistry is retained on configuration. Existing worker
classes/configurations remain unchanged when the options are absent.

Worker service suite **10 passed**, including actual registry loading and wrong
SHA/media-worker/pair refusal alongside previous service registration behavior.
Loading this configuration does not yet advertise optical.lens: authoritative
EXIF metadata resolution and compiler-declared input closure remain required
before runtime registration. Empty registry enables no profiles.

### V25 — exact original-file EXIF extraction (2026-10-02)

Added optical EXIF header extraction for camera Model, LensModel, physical
FocalLength and FNumber. Rational/decimal numbers are bounded and validated;
missing/invalid tags are unavailable rather than inferred from a profile or
35mm-equivalent focal length. Header loading uses lazy libvips and returns
asset-version/source-bound evidence. Caller must supply the materialized,
SHA-verified original, not an editor-provided metadata document.

Scoped EXIF suite **2 passed**, including an actual generated JPEG with nested
ExifIFD lens/focal/aperture tags read by libvips, plus invalid rationals and
35mm-only refusal. RAW format coverage, original-file binding lookup/compiler
closure and production admission resolver wiring remain outstanding.

### V25 — declared original artifact EXIF resolver (2026-10-02)

Added OriginalLensBinding and ArtifactLensMetadataResolver: exact VersionBinding
must match a single compiler-authorized original ref/suffix, then TaskArtifacts
materializes that declared original before EXIF inspection. Different canvas,
source or asset version and ambiguous bindings are unavailable before input IO.
The original file mapping closes on return/error.

Resolver/EXIF suite **3 passed** with actual JPEG header bytes and input test
double, exact binding refusal and input lifetime checks. Catalog inspection
confirms assets.original_artifact_id exists, but the authoritative catalog/task
binding query and compiler serialization are not yet implemented; this resolver
must not be populated from editor-controlled metadata. No RAW qualification or
full lens operation readiness is claimed.

### Capture float32 alpha output budget (2026-10-02)

Aligned scheduler closure, backend prepared artifact resolver and forward-only
migration 00312 output policy with 2048 x 2048 float32 alpha: 16,785,408 bytes
including the existing 8192-byte NPY allowance. Historical uint16 remains valid.
Adoption tests: **4 passed**, covering no alpha, legacy budget, float32 maximum
and rejection one byte beyond the maximum. Backend v2input suite passed.
Migration 00312 has not yet been applied to the private integration database;
no production migration or end-to-end capture qualification is claimed.

### V25 — compiler original catalog provenance (2026-10-02)

Migration 00313 adds API-only v25_lens_original_source with editor-session
authorization, active tenant, project-scoped undeleted verified asset, exact
asset version master ID/content SHA and ready verified unexpired versioned
WorkingImage/original checks. Arbitrary same-tenant source refs cannot match.
SQLLensOriginalSource checks canonical canvas binding and original content SHA
against the supplied hash-verified WorkingImage; callers must verify the image
bytes before invoking it. Compiler wiring and original suffix selection remain
pending. Go v2input suite passed including canvas/content substitution tests.

Private synthetic PostgreSQL migrated 311 → 313 transactionally. Verified
photo_api has EXECUTE and photo_scheduler does not; capture.prepare alpha
policy is 16,785,408 bytes. This supersedes the preceding note that migration
312 was unapplied. Positive catalog fixture, revoked authorization and full
lens task closure integration are still unverified. No production migration
or V25 completion is claimed.

### V25 — verified source loader and original binding compiler (2026-10-02)

SQLLensOriginalSource.Load now owns immutable WorkingImage byte verification
(SHA, exact length, schema, float32 working profile, canonical canvas hash and
non-null object version), with the metadata-only resolver private. Its source
object descriptor must still come from the caller's authorized artifact scope.
Tests prove changed bytes and unversioned objects never reach the catalog query.
CompileLensOriginalBinding serializes the exact worker VersionBinding/original/
suffix contract and checks original descriptor/ref consistency. JPEG, PNG, TIFF
and explicit supported RAW MIME mappings are allowed; ambiguous octet-stream
is refused. RAW container/header qualification remains pending.

Go v2input suite passed, including positive verified source load and container
allowlist/SHA mismatch tests. These are object reader/catalog doubles; the
positive real catalog fixture and complete task-input closure/compiler dispatch
are not yet integrated. No lens operation production readiness is claimed.

### V25 — lens input closure and typed binding discovery (2026-10-02)

CompileLensClosure pins params/source/pixels/optional alpha/inherited geometry,
profile/qualification, original photo and hash-verified OriginalLensBinding.
Rejects mismatched binding, nonfinite/out-of-range scalars and inconsistent
binding document SHA; replacing the original changes the closure digest.
Worker V2InputIndex and TaskArtifacts now expose bounded typed ref discovery
from the verified disk-backed input closure, refusing ambiguity above the limit.
ArtifactLensMetadataResolver.from_declared_inputs consumes the single declared
OriginalLensBinding document rather than runtime editor metadata. Dispatch and
production operation registration are still pending.

Go v2input suite passed; input closure suite **8 passed**. Found and corrected
existing worker contract SHA drift against current schemas.json while retaining
the previous pin for already-created jobs. No full lens qualification claimed.

### V25 — CPU worker lens operation installation (2026-10-02)

CPU worker build now installs optical.lens/1 only when its explicitly configured
validated LensRegistry has operator pins. Empty/unconfigured registries install
no lens operation. The operation owns TaskArtifacts lifecycle, discovers the
single declared OriginalLensBinding, builds PinnedLensAdmission with registry
runtime hash and pins, then invokes the tiled lens adapter. Output-role allowlist
matches result staging exactly: document/working_image/pixels/alpha/warp_field/
mask_pixels/mask/report. No pins or photographic qualification were fabricated.

Operation/adapter/EXIF resolver checks **7 passed**; worker config/registry/runtime
admission checks **14 passed**; latest operation/declared binding/input closure
checks **14 passed**, including prior schema-pin compatibility. Factory wiring
tests use artifact/admission doubles; real gateway + approved photographic
registry task execution remains unverified. Backend catalog/dispatch activation
and resource admission still pending; this does not expose a production API.

### V25 — lens resource admission (2026-10-02)

Added worker lens_resources/admit_lens and backend CompileLensProfile with matching
conservative estimates: RAM 60*N+512MiB, scratch 60*N+64MiB, output 28*N+32MiB
(or 4*N+32MiB for exact identity). Backend scratch also covers original EXIF
materialization peak originalBytes+64MiB. N is bounded to 200M pixels. Resource
checks reject missing/malformed budgets, mismatched pixel count/nonzero VRAM and
RAM/scratch/output underbudget before EXIF admission or array downloads.
Runtime component pins now include lens_resources.py. Actual peak-RSS and
photographic performance qualification remain pending; these estimates are
conservative static admission, not measured guarantees.

Go v2input suite passed; resource/adapter/operation/registry **10 passed**,
including one-byte-underbudget checks and original scratch peak. Source children
and original byte sizes must still be resolved by the production compiler;
operation catalog and dispatch commit remain pending.

### V25 — lens operation catalog / Capture alpha estimates (2026-10-02)

Migration 00314 registers CPU optical.lens/1 (600s, CPU backend) and all ten
output-role policies exactly from normative operations.json, including JSON
and NPY MIME restrictions, byte caps and counts. v2_enabled remains false while
compiler dispatch/admission and photographic qualification are incomplete.
Private synthetic PostgreSQL advanced 313→314; exact policy comparison and
registry attributes verified. Reusable real-DB catalog integration **1 passed**.

CapturePrepareProfile now budgets 16 bytes/pixel RGB+alpha for source and proxy
when alpha exists, covering float32 optical outputs as well as legacy uint16.
Previously it reserved 14 bytes/pixel despite float32 proxy support. Go v2input
suite passed including exact scratch/output and underbudget regression checks.
No production dispatch activation or package completion is claimed.

### V25 — lens graph-task preparation (2026-10-02)

PrepareLensTask now combines deterministic UUIDv5 optical.lens:ordinal identity,
resource profile, exact dependency inventory and published chunk/manifest
transport into a GraphTask compatible with existing atomic graph registration.
It returns params JCS/ref, resource profile and input digest for shell staging.
Frozen refs must originate from authorized source/profile/original resolution;
this helper does not create run/task shells or activate dispatch.

Go v2input suite passed. Tests cover complete eight-entry transport, exact
manifest/direct-input consistency, retry-stable publication hashes, wrong
publisher SHA refusal and underbudget rejection before transport writes.
Memory publisher doubles are used; scoped production document staging, shell
commit and full photographic lens execution remain pending.

### V25 — task source dependencies derived from verified bytes (2026-10-02)

PrepareLensTask now receives exact WorkingImage JSON instead of independently
supplied dimensions/pixels/alpha/geometry. It verifies source-ref SHA, bounded
length and WorkingImage schema/profile/dtype, canvas hash and original-content
SHA before deriving every child ref and canvas dimensions. Dense-warp field
refs come from the parsed validated transform chain. Trusted catalog resolution
must still prove this source belongs to the selected version/project.

Go v2input suite passed. Regression cases reject modified JSON, wrong canvas
and original-content substitution before publishing any closure transport.
Run/task shell creation, scoped params/original document staging, atomic graph
activation and end-to-end photographic execution remain pending.

### V25 — two-phase shell/transport preparation (2026-10-02)

Split lens task preparation into CompileLensTask (no IO, frozen profile/closure
and DraftTask), then PublishLensTask (transport publication after blocked shell
commit). Existing scoped publisher requires the shell/task scope before writes;
the prior all-in-one helper alone could not satisfy that ordering. DraftTask and
published result copy JSON slices so caller mutation cannot alter frozen profile
state. PrepareLensTask remains a convenience for already-created shells.

Go v2input suite passed with equivalent two-phase/convenience transport, draft
identity and defensive profile-copy checks. This provides shell construction
inputs; actual lens-specific scoped params/original staging, authenticated shell
transaction orchestration and dispatch activation are still pending.

### V25 — profile-derived qualification closure (2026-10-02)

CompileLensTask now verifies exact LensProfile JSON SHA against params.profile,
strict canonical schema/required fields, camera/lens length, focal/aperture,
center and coefficient/CA ranges. Qualification dependency is extracted from
that verified profile instead of an independently supplied ref. Params focal
and aperture must match profile within 1e-6. No operator qualification is
created or accepted from a client status flag; worker registry admission remains.

Go v2input suite passed with altered profile bytes rejected before publication;
contract checks cover missing center, null coefficients, wrong lengths, excess
tangential/CA values and unknown fields. Full authenticated staging/shell
orchestration and photographic qualification remain incomplete.

### V25 — actual catalog original provenance / revocation (2026-10-02)

Added real PostgreSQL test for v25_lens_original_source under photo_api with
synthetic active tenant, owner session, project, verified original and exact
WorkingImage master version. Positive original ID/SHA resolution is verified.
Wrong project, different same-SHA WorkingImage artifact, wrong source SHA,
expired/unverified original and revoked session are rejected with no rows.
The test uses existing migrated authorization/session/catalog functions rather
than query doubles. Integration **1 passed**. This covers original SQL admission;
scoped lens document publication and atomic workflow orchestration remain pending.

### V25 — worker original lineage cross-check and runtime pins (2026-10-02)

Declared-input metadata resolver now reads the declared source WorkingImage and
checks canonical canvas binding, float32 dtype and original SHA equality against
Canvas.source_sha256 before constructing EXIF resolution. A compiler binding
document alone cannot substitute a different original for that source. Exact
source byte verification remains TaskArtifacts.contract responsibility.

Optical runtime component pins now include admission, metadata-source resolver
and EXIF implementation in addition to lens math/preparation/resource code.
Matching package versions cannot hide changed bytes in those components.
Config/registry/admission/metadata/operation suite **22 passed**, including
source-lineage substitution and independent component SHA mismatch tests.
Lens-specific SQL document scope/staging and transactional workflow launch
remain pending; ordinary task_params scope cannot be reused for this purpose.

### V25 — frozen document scope foundation (2026-10-02)

Migration 00315 adds internal fn-owner-only v25_lens_document_scope. It binds
params/original binding SHA to exact JCS and JSON stored under immutable run
config v25_lens.nodes; checks task owner identity, active tenant, unsealed
uncancelled draft V2 run, v2.2 workflow, live reserved run quota and live editor
authority using the run actor's pinned auth session. No API/scheduler EXECUTE
is granted; fenced reserve/complete entry points are not yet installed.
Private PostgreSQL advanced 314→315, function privilege denial verified.
Positive scope and failure/revocation fixtures remain pending.

FrozenLensTask.DocumentPlan emits the exact params/original JSON, SHA and JCS
for future immutable config staging. Go v2input suite passed; emitted bytes
hash to their refs. This is scope preparation, not full publication or dispatch
activation. Production scoped staging/orchestration remains incomplete.

### V25 — fenced lens document publication entry points (2026-10-02)

Migration 00316 permits only explicit lens params/original-binding purposes and
adds scheduler-only reserve/complete functions using v25_lens_document_scope.
Lifecycle preserves quota locking/accounting, immutable SHA/length/bucket,
resumption token rotation, replay checks and completion object-version fences.
Go artifactjson publisher routes these purposes to dedicated functions; scope
keys remain bounded and unrelated purpose aliases are rejected.
Private PostgreSQL advanced 315→316. Catalog/scope integration **2 passed**:
scheduler reservations outside frozen scope are forbidden without changing
reserved quota; API EXECUTE is denied. Go artifactjson/v2input suites passed.
Positive reserve/upload/complete/replay/token-staleness and revocation between
reservation/completion fixtures remain pending. Full launch is not ready.

### V25 — positive SQL publication lifecycle (2026-10-02)

Added parametrized isolated PostgreSQL tests for both lens document purposes.
Actual owner/session/project/V2 draft/workflow/quota config drives created →
resumed with token rotation → completed → replayed. Old token/null object
version/different replay version are refused. Revoking the author's session
between reserve and complete refuses both completion and new reservation;
restored synthetic session permits completion. Reserved-to-storage accounting
and exactly persisted artifact SHA/length/version/role are verified.
Integration **2 passed**. Fixtures exercise SQL byte fences with small JSON;
they do not qualify LensProfile content or exercise object-store upload.

The integration db fixture creates isolated fully migrated databases on its
configured test PostgreSQL service (127.0.0.1:55432); PHOTO_TEST_DATABASE_URL
is not consumed by that fixture. Prior direct psycopg migration checks used
the separate private socket database at 55439. Both are synthetic. The previous
no-scope quota check now explicitly creates tenant_usage so it proves unchanged
accounting against an existing row. Full launch remains incomplete.

### V25 — exact frozen document size and unique node scope (2026-10-02)

Migration 00317 adds internal v25_lens_document_bytes and makes both reserve
and complete require exact octet length of frozen params/original JCS and a
single matching task owner in config nodes. This prevents wrong-size reservations
from occupying the immutable document scope and duplicate node ambiguity.
The existing authorization, quota and completion token checks remain intact.

Isolated PostgreSQL publication suite **4 passed** for both purposes, including
wrong-size refusal and duplicate task nodes with no document/quota mutation,
plus positive replay/token/revocation lifecycle. Private socket database
advanced 316→317. No object-store integration, lens workflow activation or
package completion is claimed.

### V25 — scoped document publication helper and ref rebinding (2026-10-02)

PublishLensDocuments validates frozen params/original binding JCS and hashes
before any write, publishes under exact run/task scope using dedicated purposes,
and verifies returned refs/SHA. Publication order is original binding then params.
Final CompileLensTask uses the returned refs rather than provisional artifact
IDs. Tests prove stable task ID/content, published refs in the final closure,
correct scopes/purposes and invalid-plan refusal before publication.
Go v2input/artifactjson suites passed. Publisher test double is used; real
object-store publication still unverified. Authorized draft run/config must
exist before publication; final task shell staging follows publication. This
refines the earlier shell-order note: provisional DraftTask is planning data,
not sufficient to persist a task referencing unpublished params. Full SQL
orchestration/dispatch activation remains pending.

### V25 — blocked lens task staging (2026-10-02)

Migration 00318 adds scheduler-only stage_v25_lens_task. It checks active tenant,
authorized unsealed frozen document scope, UUIDv5 workflow/stage ordinal task ID,
exact frozen CPU/v2.2 profile and both ready versioned verified lens documents
with exact sizes. It creates a blocked task or accepts only an exact untouched
blocked replay. It does not seal the graph or enable dispatch. LensDocumentPlan
now includes the frozen execution profile; SQLLensTaskStage binds the compiler
result to scheduler staging.

Private PostgreSQL advanced 317→318 with scheduler/API execute privileges
verified. Go v2input suite and isolated catalog migration tests passed. Added
no-run scope rejection check. Positive blocked task/replay/CAS and complete
transport/activation integration remain pending. No full launch claimed.

### V25 — positive blocked task stage/replay and revocation (2026-10-02)

Extended actual PostgreSQL lens document lifecycle fixtures with UUIDv5 run/
workflow/task identity and frozen CPU resource profile. Staging without the
second ready document is refused. After both complete, exact task is inserted
blocked/attempt0; identical replay succeeds, altered digest and wrong task ID
fail. Revoking author session refuses another stage while preserving one task.
SQL lifecycle suite **4 passed** (includes duplicate-scope cases). These SQL
byte fixtures do not exercise full OpticalLensParams content or image quality.

Go v2input suite passed with SQLLensTaskStage wrapper success, wrong workflow
refused before DB query, and rejected SQL stage mapped to ErrExpired. Exact
transport registration, graph activation, object-store upload and photographic
qualification remain pending.

### V25 — lens task input registration and activation gate (2026-10-02)

Actual PostgreSQL lifecycle tests now publish canonical closure chunk/manifest
via existing input-document reserve/complete, register two scoped document
entries and seal exact task inputs. v2_input_closure_ready is true with four
rows (two entries plus transport). Graph seal is refused while optical.lens
v2_enabled is false; run remains draft/unsealed. Integration **4 passed**.
These two-entry fixtures verify registrar composition, not the full production
lens source/profile/NPY dependency closure, which is covered separately by Go
compiler tests and still needs end-to-end execution.

Admission.RegisterLensInputs adds a DB-only transactional registration phase
for the prepared lens task, validating run-derived workflow/task identity and
transport consistency without enabling dispatch. Go v2input suite passed.
Production launch orchestration, qualification gating and actual object-store
transport/image execution remain pending.

### V25 — preparation coordinator / post-seal retry (2026-10-02)

LensPreparationCoordinator.Resume joins document publication, returned-ref
recompilation, blocked task staging, scoped transport publication and transactional
input registration for an existing authorized frozen draft. Checks run-derived
workflow before writes and stops at first error/cancellation. It does not
create the run or enable/seal graph dispatch. Go v2input suite passed: success
phases, stage refusal before transport/registration, wrong workflow before writes.
Dependencies in these coordinator tests are doubles.

Composition exposed a real replay gap: seal_v2_task_inputs adds transport keys
to execution_profile. Migration 00319 permits exact staged replay after verified
input-closure readiness while comparing the unchanged base profile without only
those four transport keys. Isolated SQL lifecycle **4 passed**, including stage
replay after input seal and later session revocation refusal. Private socket
database advanced 318→319. Production launch wiring, object-store execution and
photographic qualification still pending.

### V25 — exact sealed input registration retry (2026-10-02)

Coordinator composition exposed another real retry gap: generic input registrar
refuses already-sealed tasks. Migration 00320 adds scheduler-only exact retry
acknowledgement for blocked lens tasks with live frozen document authority,
ready closure, exact params/manifest/chunks/count/entry digest/direct inputs.
Admission.RegisterLensInputs returns successfully only for matching sealed
transport, otherwise delegates to generic registration which refuses unsafe
sealed edits. No closure is rewritten or graph activated.

Go v2input suite passed; isolated SQL lifecycle **4 passed** with exact replay,
changed digest and direct-root list refusal. Added revoked-session retry refusal.
Private socket database advanced 319→320. Full object-store execution and
production run/admission/activation integration remain pending.

### V25 — combined optical regression and coordinator retry (2026-10-02)

Combined all current test_v25 unit suites with worker service/input-closure
checks: **79 passed**. This spans algorithms, alpha/color, preparation/staging,
EXIF/admission/runtime pins, resource checks and input allowlist configuration;
it does not imply complete V25 requirements or production image qualification.

Coordinator tests now use immutable scoped publication semantics rather than
new random refs on every call. Repeating Resume preserves params ref, input
digest and manifest ref, and invokes registration recovery; subsequent stage
refusal still prevents transport/registration. Go v2input suite passed. Publisher
and stage/registrar remain doubles in this coordinator test; separate PostgreSQL
fixtures prove SQL replay behavior. Full real object-store/coordinator/run
creation/activation integration and photographic qualification remain pending.

### V25 — immutable draft run configuration compiler (2026-10-02)

CompileLensDraftConfig builds canonical config with exact project/snapshot/
auth-session and lens node document/profile plans. Checks every UUIDv5 task
against run-derived workflow, forbids duplicates, bounds count/document size,
sorts plans by task UUID and sums output reservation within JS-safe integer
range. Returns exact config bytes/SHA and output reserve for future authorized
run creation. It creates no database rows or qualification.

Go v2input suite passed including byte-hash consistency, duplicate task and
foreign-run refusal. SQL authorized run creation, snapshot/provenance freeze,
idempotency/quota admission and production coordinator wiring remain pending.

### V25 — frozen source snapshot preflight (2026-10-02)

Migration 00321 adds API read-only v25_lens_snapshot_ready: live editor authority,
actor-owned frozen V2 snapshot, 1..5000 items, exact count/contiguous ordinals/
unique asset versions, project-scoped undeleted verified assets, exact version
master IDs/SHA and ready verified unexpired versioned WorkingImage/originals.
Run admission must repeat it under transaction locks; this read alone creates
no run and proves no photographic qualification.

Real PostgreSQL source integration **1 passed**, including positive frozen
selection, other-project refusal, expired/unverified original and revoked session.
Snapshot fixture follows actual immutability rules: collect with expiry then
freeze expires_at=NULL. Private socket database advanced 320→321. Authorized
run creation/idempotency/quota and production launch wiring remain pending.

### V25 draft admission (2026-10-02, scoped progress)

Migration 00322 introduces scheduler-only begin_v25_lens_draft. It locks tenant,
usage and frozen snapshot, repeats source/session preflight, checks run-derived
workflow UUID, freezes the document plan and reserves output quota once. Exact
live draft replay succeeds; changed reservation size and revoked session fail.
The reservation must equal the sum of CPU v2.2 node output estimates.
SQLLensDraftStore validates canonical compiled config bytes/hash before calling
this function. Go internal/v2input passed; initial isolated PostgreSQL admission
integration passed (1 test). The private socket DB is still at migration 321.
This proves admission invariants on synthetic fixtures, not photographic lens
qualification or end-to-end product readiness. Production request orchestration,
activation checks and remaining V25–V32 work remain pending.

Follow-up verification: complete V25 SQL catalog/original/document suite
**7 passed** (isolated fully migrated PostgreSQL databases), including wrong
workflow and quota exhaustion refusal. Go internal/v2input passed after adding
canonical config substitution and SQL refusal propagation checks. Lens resource
and operation tests **6 passed**. No operation enablement or production deployment.
Private synthetic socket database advanced 321→322 after the integration suite.

### V25 whole-draft preparation and exact selection (2026-10-02)

LensDraftCoordinator.Prepare derives run/workflow from tenant, actor and stable
key, validates contiguous task ordinals and distinct asset versions, compiles
all tasks before admission, freezes the configuration and then resumes each
scoped publication/staging/registration sequence. Profile/source JSON are copied
before IO. It leaves blocked tasks in draft; it does not activate the operation.
Go checks cover stable retry, invalid batch with no writes, refused admission,
stage failure stopping registration, and successful retry after that failure.

Migration 00323 repeats an exact snapshot-plan check inside draft admission:
UUIDv5 task per selected ordinal; exact version and WorkingImage ID/SHA; matching
params/original binding; exact catalog original ID/SHA. It rejects replacement
versions, same-SHA replacement WorkingImage IDs, wrong original IDs/SHA and wrong
task IDs on new runs, rather than relying only on replay config hash conflicts.
Isolated PostgreSQL original/admission integration passed (1 test) after those
new-run substitutions were added. Private synthetic socket DB advanced 322→323.
These are scope/admission checks, not complete RAW/lens/export qualification.
Production launch wiring, graph activation and full V25–V32 remain unfinished.
Follow-up: complete V25 SQL suite **7 passed** with migration 00323, including
API denial for draft creation and scheduler denial for the internal plan guard.
Go internal/v2input and internal/artifactjson passed. Existing generic graph
sealing still only has general registry/closure and consent guards; lens-specific
fresh source/session/plan revalidation at graph activation is the next unresolved
integration step. Lens remains disabled in the operation registry.

### V25 graph seal authority (2026-10-02)

Migration 00324 adds internal v25_lens_activation_ready and integrates it into
seal_v2_graph for any lens-configured run or run containing optical.lens tasks.
It repeats exact snapshot/plan and live editor checks, holds existing session,
user, membership, project/ACL, snapshot, version/asset/source/original rows with
share locks, and matches all task IDs, node keys, params SHA and frozen execution
profiles (excluding the four transport seal fields). General consent and core
registry/input-closure/quota gates remain in the existing seal path. The operation
registry is not enabled by this migration.

SQLLensDraftStore.Seal validates prepared identities and transport descriptors,
then invokes the atomic SQL seal without registering inputs a second time.
Go internal/v2input passed: correct prepared boundary, foreign workflow refusal
before SQL, and propagation of a refused seal. PostgreSQL V25 plus V22 scene suite
**8 passed**. Final activation-guard integration **1 passed**, covering positive
synthetic authority guard, stale/unverified original, revoked session, wrong node
key and changed execution profile. Synthetic task has no input closure and cannot
activate; these tests do not prove qualified lens processing end to end. Private
synthetic socket DB advanced 323→324. Production compiler/request wiring, qualified
positive lens execution/adoption and full RAW/export/V25–V32 acceptance remain open.
Follow-up general V2 regression: the original scaffold test initially failed
before execution because its database fixture skipped V21–V23 foundation while
still applying later dependent migrations (missing model_qualifications).
The fixture now explicitly stops scaffold modules at migration 196. Shared graph
registration/seal assertions also run in new test_v2_graph_seal_runtime.py on all
production migrations, independently of scaffolds. Both tests **2 passed**,
including successful normal V2 graph seal and replay, queued event/outbox
uniqueness, frozen input mutation refusal and denied worker/scheduler privileges.

### V25 nonzero lens adoption boundary (2026-10-02)

prepare_v2_adoption now requires authoritative params for every optical.lens
result. Zero-strength eligibility retains its existing evidence-backed identity
path. Nonzero results are checked by verify_lens_correction: exact source canvas
binding, float32 source, deterministic corrected WorkingImage (source color/ICC,
canvas/source identity, prior chain, partial half-pixel DenseWarp), corresponding
changed mask and source provenance report; generated mask and invented registration
are rejected. Adoption streams canonical little-endian C-order float32 NPY output
bytes and verifies exact dimensions/size/SHA, finite RGB and field values, alpha
within [0,1] and binary changed mask. HDR/negative RGB remains unclipped.

PostgresObjectEvidenceReader.iter_bytes uses the exact catalog version, bounded
64KiB blocks, exact-length checks and close/release on consumer failure. Missing,
empty and 'null' object versions are refused before IO. It performs no object IO
inside the eventual adoption transaction; SQL still rechecks attempt/fence/state.

The staged real-array unit fixture now supplies hash-valid source/params bytes
and verifies full nonzero closure with fragmented binary reads. Negative checks
cover valid-SHA substituted provenance/ICC, nonfinite/unit/binary violations,
wrong same-size shapes, SHA mismatch, truncation, oversized blocks, and unpinned
object versions. These tests are not live object-store + PostgreSQL adoption or
photographic qualification. Production launch wiring and V25–V32 acceptance remain
unfinished; no lens operation enablement or existing imaging algorithm change.
Final worker verification: all test_v25 unit tests plus existing V2 adoption and
adopter-service tests **85 passed** after all changes above.

### V25 adoption commit authority fence (2026-10-02)

Migration 00325 wraps the existing V21/V22/V23 adopt_v2_plan chain and removes
scheduler execute permission from the renamed internal core. Optical lens commits
lock tenant→run→task and repeat v25_lens_activation_ready, including live session,
selected catalog rows and frozen task plan, before any adoption links/markers.
General operation adoption still follows the unchanged prior core/barrier chain.
A live synthetic lens scope reaches the existing inventory-validation error;
after session revoke the same call returns false before invoking that core.
Scheduler cannot bypass the new fence via the renamed internal function.
Private synthetic PostgreSQL advanced 324→325. Initial source/admission and normal
V2 seal tests **2 passed**; strengthened authority/bypass test **1 passed**.

Dedicated photoeditor-dev-minio container and its previous pinned image were
absent. Added deploy/images/minio-integration/Dockerfile, an HTTP-only local test
image using the existing source-pinned static MinIO binary plus license/manifest.
Binary SHA was verified against .runtime/minio/source-manifest.json before an
offline network=none build. Built image:
sha256:d0010f7f0b7f7ca6b15ab5d6a61c4859c0720e80aba02a7f05a639a7c4757591.
No container/data was started, no production image lock changed and no downloads
were performed. Positive live PostgreSQL + versioned object-store lens adoption
and production compiler/request wiring remain pending; no operation enablement.
Final migration325 regression: lens original/document/catalog plus fully migrated
normal V2 graph seal suite **8 passed**.

### V25 real versioned binary IO boundary (2026-10-02)

Added tests/integration/test_v25_lens_objectstore.py. The fixture resolves the
local integration image, verifies development/purpose labels, creates a fresh
repository-owned data/0600 credential directory, publishes only a loopback random
port, runs with memory/CPU limits and stops/removes its own container/data afterward.
A new bucket has versioning enabled. No existing bucket/container/data is used.

Real PostgreSQL records for a synthetic succeeded V2 lens task, attempt/fence,
verified produced artifacts, ready output uploads and a document result root
feed PostgresObjectEvidenceReader under photo_scheduler. Real MinIO stores NPY
bytes with negative/HDR values. After overwriting the same key, validation still
reads the original pinned version. Wrong tenant and fence are refused by SQL
before object IO. PostgreSQL prevents changing artifacts.object_version. Another
synthetic verified output pointing to malformed NPY bytes is rejected by the
stream validator. The full integration test passed after final fence checks.

The fixture's root is a synthetic JSON document and it does not invoke full
ImageResult preparation or AdoptionCoordinator.adopt; this proves the real
catalog/object IO boundary, not end-to-end lens execution/adoption or photographic
qualification. That complete typed-result commit and production compiler/request
wiring remain pending. Registry enablement remains unchanged.

### V25 full typed lens adoption IO and terminal rejection (2026-10-02)

Added test_v25_lens_typed_adoption.py. Synthetic HDR/negative float32 source and
explicit lens coefficients go through prepare_working_lens and stage_lens_result.
All typed documents and NPY files are uploaded to actual versioned MinIO; real
PostgreSQL catalog, attempt/fence, declared inputs, ready output uploads and
canonical task result feed separate scheduler read/write connections through
AdoptionCoordinator. Strength0 and strength1 commit the exact closure once and
produce one adoption event/outbox message. Identity source legitimately has two
purpose paths (WorkingImage and provenance), so distinct artifact count and link
count are checked separately. Review/uncovered dense output is adopted without
inventing a homography or clipping source HDR. The test bypasses compiler,
dispatch/gateway and photographic qualification using explicit synthetic metadata;
it proves typed preparation/staging/adoption IO, not a qualified product release.

Migration 00326 distinguishes an unadopted, exact current lens result whose scope
has become stale from stale-fence/cancelled/already-adopted calls. Only that current
result raises V25_LENS_SCOPE_STALE; AdoptionCoordinator maps that exact SQLSTATE/
message to LENS_SCOPE_STALE. The normal Adopter rejects and finalizes via the
existing rejection path instead of endlessly retrying a permanent scope failure.
Already-adopted replays after later revoke remain false/no-op without rejection.
A real MinIO read callback revokes the session after output pixels evidence was
resolved and before SQL commit: Adopter.run_once rejects once, creates no artifact
links/adoption event, emits the explicit failure code and does not reject again.

Typed live integration **3 passed** after terminal rejection changes. Existing
V2 adoption/adopter and binary validation checks **25 passed**. Private synthetic
PostgreSQL advanced 325→326. Lens registry remains disabled; production compiler,
request/dispatch/gateway wiring, approved lens registry/photographic qualification,
RAW/export acceptance and remaining V25–V32 requirements are still open.
Final SQL/live-IO regression: all lens integration tests plus fully migrated
normal V2 graph seal **12 passed**. All integration containers, credentials and
scratch/data directories created by these tests were cleaned by their fixtures.

### V25 archive verification cancellation (2026-10-02)

archive.build replaces non-cancellable ZipFile.testzip with _verify_archive.
Each stored entry and embedded manifest is read in bounded blocks through the
existing check_active/size/SHA copy boundary; ZipExtFile still verifies CRC.
Names/order, entry sizes and stored compression are checked before publishing.
A real 3MiB force-ZIP64 test loses its fence during the second verification block
and propagates cancellation; normal verification succeeds and wrong SHA fails.
Archive platform tests **5 passed**. This is not the required actual >4GiB ZIP64
IO qualification, which remains pending alongside RAW/export V25 acceptance.

### V25 actual ZIP64 >4GiB disk IO (2026-10-02)

Added opt-in test_v25_zip64_large_io.py (PHOTO_RUN_ZIP64_LARGE_IO=1). It executes
archive.build with an actual streamed 4,296,015,889-byte stored member, bounded
1MiB source blocks, repository-owned ScratchSpace and no full-size memory/source
file. The generated ZIP is 4,296,016,470 bytes; actual allocated disk blocks exceed
4GiB, fsync completes, ZIP64 end record/locator and member version/size are checked.
The production verification path reads CRC/SHA/manifest, and staging independently
rehashes the actual archive. Manifest sizes/SHA agree, 12,299 activity checks run,
and test-created archive/scratch files are removed. Initial large-IO run **1 passed**
in 18.80s with 48.12GiB initially free. This is real disk IO, not sparse metadata
or an artificially lowered ZIP64 threshold; gateway/S3 >4GiB upload remains a
separate unproven integration requirement.

_copy_entry also enforces bytes-only blocks no larger than the requested bounded
read size. Malformed/oversized stream tests and existing archive tests **6 passed**.
A final >4GiB rerun is performed after this stream-bound refinement. Full V25
RAW/export/camera/device qualification and the wider V20–V32 objective remain open.
Final-code large-IO rerun **1 passed** in 16.24s: member 4,296,015,889 bytes,
archive 4,296,016,472 bytes, 12,299 activity checks. ZIP size varies slightly with
fresh UUID values in the compressed manifest. Reproduce with make test-zip64-large-io;
the test requires 8GiB free and cleans only its newly generated repository files.

### V25 actual immutable S3 >4GiB IO (2026-10-02)

Added opt-in Go TestIntegrationLargeImmutableIO and pytest runner using the
isolated source-pinned MinIO fixture. Production Store.PutImmutable uploads
4,296,015,889 bytes with 8MiB single-thread multipart, expected full SHA256,
create-only semantics, catalog-style exact immutable version and stat checks.
Production Store.Open reads the full pinned object and verifies byte count/SHA
at EOF. The input repeats a bounded 1MiB deterministic buffer rather than
allocating a logical 4GiB body; both client and MinIO data belong to test scope.

Real upload/read **1 passed**: Go IO phase 60.44s, full pytest 69.13s. SHA256:
11aa0b2cf81403c31231d8653ab5a0d3b65700da2e4b743411f1bac60bf9f49b.
All newly created MinIO object versions/bucket/container/data were cleaned.
Reproduce with make test-s3-large-io (8GiB free required, explicit opt-in).
This proves the production S3 store path with a large synthetic binary. It does
not prove HTTP gateway task authorization/lease rotation/quota/complete-output
handling for a >4GiB ZIP artifact; that integration and wider V25–V32 remain open.
Objectstore Go regression passed with loopback listener access. The first sandboxed
run could not bind httptest's local port; this was an environment restriction,
not a test assertion failure. Re-run with bounded local-listener permission passed.

### V25 large gateway handler/service → S3 boundary (2026-10-02)

Added opt-in TestIntegrationLargeOutputHandler and pytest runner (make
test-gateway-large-io). Production HTTP.Handler, AuthorizeRequest, uploadReader,
UploadService and Store.PutImmutable handle a 4,296,015,889-byte streamed output
against real versioned MinIO. The signed capability binds the task/worker/attempt/
fence/scope; an injected verified TLS peer and synthetic task resolver/output
repository isolate the handler path from DB provisioning. Content-Length remains
int64, 64KiB reader bounds apply, live authorization is rechecked 9 times and
repository Begin/Complete each run once. HTTP result is 201; input is exhausted.
The created attempt object is purged and the isolated test container/data cleaned.

Actual handler/service upload **1 passed**: Go phase 37.06s, pytest 41.98s. The
binary is synthetic (declared application/zip); it is not the prior generated
ZIP64 file. This proves the gateway handler/S3 size and streaming boundary, not
wire mTLS, real SQL output quota/role policies, full ZIP pipeline or TLS/capability
rotation during transfer. Those gates and the broader V25–V32 requirements remain
open. No production operation enablement or imaging-quality change.
Gateway Go regression tests passed with local httptest listener permission after
the large-handler integration addition. The opt-in command requires 8GiB free
repository storage and leaves no test-owned credentials/data/container afterward.

### V25 upload fence and SQL large-output accounting (2026-10-02)

Gateway reader tests now simulate the next authorization interval: a changed
fence returns ErrCapability before another body read; initial 1MiB SDK requests
are restricted to 64KiB. Cancelled context consumes no bytes and records the
cause for HTTP error propagation. Gateway Go regression passed.

Added test_v25_large_output_quota.py on real, fully migrated PostgreSQL. A live
claimed gateway task reserves 4,296,015,889 bytes, rejects another output exceeding
the remaining per-task budget, rejects stale-fence completion and changed sizes,
commits exact bigint artifact metadata, and replays Begin/Complete without an
extra upload row or quota transfer. Tenant usage is exactly (reserved=size,
storage=0) before commit and (0,size) afterward. The initial test **1 passed**.
This test uses synthetic version/hash metadata and does not perform object IO;
it proves actual SQL accounting separately from the previous real S3/handler IO.
Combined wire mTLS + SQLOutputs + actual >4GiB object remains unproven. Full V25
RAW/export qualification and wider V20–V32 implementation are still unfinished.
Final gateway SQL regression: existing output reserve/commit/replay/cleanup tests
plus >4GiB bigint accounting **5 passed** on isolated fully migrated databases.

### V25 real wire mTLS / SQL / versioned S3, including >4GiB (2026-10-02)

Added wire_integration_test.go and test_v25_gateway_wire.py. A generated isolated
CA signs actual server/client certificates; the TLS 1.3 HTTP connection requires
and verifies the client certificate. Its leaf hash is bound to a real claimed
worker registration and signed output capability. Production Authorizer and
SQLOutputs run as photo_gateway against an isolated fully migrated PostgreSQL;
production UploadService/ObjectStore write to fresh versioned MinIO.

Both 27-byte and 4,296,015,889-byte streamed uploads return 201. Idempotent replay
returns 200 with the same ArtifactRef; a changed fence returns 403. Tenant SQL
usage is exactly reserved=0/storage=body size after completion. The catalog's
pinned object version is read in bounded 1MiB blocks and independently hashed;
size and SHA256 match. The large transfer keeps its real lease alive via normal
heartbeat_task calls. Test-owned object versions, container and data are cleaned.
Opt-in combined run: **2 passed in 70.89s**. Gateway Go regression also passed.
Use make test-gateway-wire-large-io to reproduce the opt-in case.

This closes the combined wire/SQL/object-store large-output transport evidence
gap reported above. The large body is deterministic synthetic binary declared
application/json under an isolated asset.verify policy; it is not a generated
ZIP64 archive or an actual lens/export result. End-to-end archive generation,
production deployment/certificate rotation, photographic qualification, RAW
coverage and the remaining V20–V32 work still require implementation/evidence.
No production operation was enabled and no processing-quality logic changed.

### V25 authorized immutable lens selection source (2026-10-02)

Migration 327 adds v25_lens_selection_sources, executable only by photo_api.
It returns ordered source/original versioned object descriptors for the entire
frozen snapshot after v25_lens_snapshot_ready checks live editor authority and
complete selection provenance. It writes no state and does not bypass the later
locked draft/seal/adoption checks. Worker/gateway/scheduler EXECUTE is denied.

SQLLensSelectionSource.Load consumes and closes catalog rows before object IO,
rejects missing/count/order/duplicate-version/bucket/opaque-version defects, then
reads each WorkingImage through bounded 1MiB JSON verification. Exact bytes/SHA,
float32 working color profile, canvas and original hash bind the resulting
LensSelectedSource, immutable SourceJSON and metadata-source suffix. A failure
returns no partial selection. Qualified profile selection remains a separate
required admission step; this loader does not enable optical.lens or claim a
finished public API.

All v2input Go tests passed. Real PostgreSQL original/snapshot test passed with
exact returned source/original rows, project mismatch, expiration/session revoke
and denied-role assertions. Migration 327 is applied to the private verification
DB as well as freshly migrated test databases. Imaging algorithms unchanged.
Final V25 catalog/original/document-publication SQL regression: **7 passed in 14.81s**.

### V25 selected-source compiler and admission boundary (2026-10-02)

CompileLensSelection now maps one exact profile choice to each selected asset
version, builds canonical params/original hashes and deterministic provisional
UUIDv5 document refs, derives focal/aperture from the verified profile document,
and validates every task's source/profile content and resource estimate before
any draft write. Duplicate/missing/foreign assignments, substituted profile SHA
and insufficient resources reject the whole batch. Compiled source/profile
bytes are copied to prevent caller mutation. Scoped publication replaces the
provisional IDs before final task staging.

LensAdmissionCoordinator joins the authorized selection loader, this compiler,
replayable draft/document/input preparation and qualification-gated SQL sealing.
A queued JobTicket is returned only after the sealer accepts; rejected activation
returns no success ticket. Tests cover deterministic retries, profile mapping
independent of request order, defensive copies, bad profile/resource preflight
before draft writes, full coordinator phase sequence and seal rejection. All
v2input Go regression tests passed. Coordinator tests use injected stores; this
is not proof of production end-to-end dispatch. Public API wiring, authorized
profile/qualification catalog resolution and actual hardware/photo qualification
remain open. No production optical.lens enablement or imaging algorithm change.

### V25 backend operator qualification pin admission (2026-10-02)

VerifyQualifiedLensProfile mirrors the worker registry identity: exact profile
byte SHA, canonical coefficient hash with qualification excluded, exact report
ref, runtime SHA and valid dataset SHA. Pins are deployment configuration held
by LensAdmissionCoordinator, not part of LensAdmissionRequest. Missing/duplicate
matches or runtime/report/profile substitutions fail before catalog reads or
draft writes. The coordinator now requires this operator pin boundary before
selected-source compilation; it does not synthesize qualification evidence.

Go v2input regression covers matching pins through joined admission, absent and
duplicate pins, another report and another runtime. The helper validates trusted
pin identities, not photographic report semantics; actual operator provisioning,
verified profile/report object catalog, public API wiring and hardware/photo
qualification remain required. No production optical.lens activation occurred.

### V25 verified profile/report object source (2026-10-02)

Migration 328 adds API-only v25_lens_profile_object for exact tenant artifact/SHA
under live project editor authority. It requires ready, verified, unexpired JSON
and a non-null pinned version. Lookup itself grants no qualification. Other
service roles have no EXECUTE. SQLLensProfileSource loads <=1MiB profile bytes,
verifies exact length/hash and operator pins, then resolves and streams the
pinned <=8MiB qualification report in 64KiB blocks with SHA/length/context/close
checks. Truncated, oversized, wrong-hash and unversioned reports are rejected.
No report semantics or photographic qualification are synthesized.

LensAdmissionCoordinator now requires a profile loader and replaces caller
ProfileJSON with catalog-loaded bytes before pin checking and compilation.
Joined-admission test provides forged caller JSON and still uses the trusted
loader document; no recipe-supplied bytes establish profile authority. All
v2input Go tests passed. Real PostgreSQL exact lookup/SHA mismatch/denied roles/
revoked session test **1 passed in 2.32s**. Migration 328 also applied to private
verification DB. Production construction/API wiring, operator profile catalog
provisioning and real qualification remain incomplete; optical.lens stays gated.

### V25 operator registry loader and production dependency factory (2026-10-02)

LoadLensRegistry matches the worker registry envelope and pin identity. It reads
only a bounded regular single-link file with O_NOFOLLOW/O_NONBLOCK and checks an
independently configured raw SHA, schema/unknown fields, runtime document hash,
per-pin runtime/dataset/ref digests and duplicate report+coefficient identities.
Synthetic registry tests cover correct identity, raw SHA substitution, symlink,
hardlink, duplicate pin, runtime mismatch and unknown envelope fields. Backend
pins the runtime document; actual installed-runtime verification stays at worker.

LensRuntime.Coordinator wires SQL selection/profile sources on photo_api and
publication/task/input/seal mutations on photo_scheduler. Distinct pool instances
and current_user role checks are required before constructing dependencies;
operator pin slices are copied. All v2input Go regression tests passed. Factory
role checks still need real PostgreSQL integration evidence. No new public lens
endpoint was invented: the designed preview/workflow route integration remains
to implement, along with startup registry settings and actual qualifications.

### V25 real factory role isolation and runtime registry schema (2026-10-02)

Actual isolated PostgreSQL factory test **1 passed in 4.91s**: correct API and
scheduler pools construct the production coordinator; selection/profile sources
retain API credentials and sealer retains scheduler credentials. Shared pools,
swapped roles and gateway credentials are rejected. Registry pin mutation after
construction does not affect either coordinator or profile-source pin copies.
This test performs no S3 IO or task activation.

Backend registry loading now validates the worker OpticalRuntimeLock document
schema as well as its canonical hash: CPython version/machine, exact numpy/
pydantic/rfc8785 package set and the exact 13 runtime component digests. Unknown,
missing and incompatible lock structures cannot reach coordinator construction
through LoadLensRegistry. Synthetic structurally valid locks prove parsing only;
worker remains responsible for installed version/component checks. Go v2input
regression passed. Preview's existing legacy modules need an EditingRecipeV3
adapter for lens before public route wiring can be claimed complete.

### V25 Python worker / Go backend registry parity (2026-10-02)

Cross-language integration builds OpticalRuntimeLock from the actual installed
Python versions/component bytes, writes one canonical registry/profile in a
repo-local temporary directory, verifies the registry with the production worker
loader, then reads it with the Go loader. Runtime and coefficient/report pins
match across languages, including Cyrillic/Greek camera/lens strings and floating
coefficients. **1 passed in 4.01s**. Go v2input regression passed; Python registry/
admission regression **7 passed in 0.41s**. Temporary fixtures are removed.
Synthetic report/dataset pins exercise format/hash parity only, not photographic
qualification or operation activation.

Physical migration mapping records V25 313–328 and V24 alpha-policy migration
312; package statuses remain planned pending their full acceptance evidence.
EditingRecipeV3 carries optical operations through ordered LayerStack refs;
preview integration must preserve other edits, masks and blending rather than
substitute a standalone lens run for the recipe. That implementation is still
open, along with the broader V25 qualification gates and V26–V32 scope.
Documentation check currently stops at BASELINE_DRIFT for workers/photo_worker/contracts/images.py; the source-baseline lock needs explicit review of implemented transform contract changes before documentation consistency can be claimed.

### V25 reviewed source baseline and stream lifecycle (2026-10-02)

Reviewed the two source-baseline drifts explicitly: DenseWarpTransform's optional
half_pixel marker preserves legacy omitted-field serialization/hash and rejects
explicit null; TaskArtifacts adds bounded typed contract discovery and declared
streaming without relaxing input authority. Gateway ArtifactStream checks lease
before/after each bounded read and closes its response/connection. Archive
consumers independently hash the body; stream header identity alone is not body
hash evidence. Review notes accompany the two updated baseline SHA entries.

Existing transform/gateway/typed-closure/lens-operation regression **28 passed in
6.20s**. Added actual TLS TaskArtifacts.stream assertions: declared input full
read/EOF/close, undeclared input refusal, consumer exception and closed partial
stream. Gateway regression with these assertions **7 passed in 5.31s**. These
checks preserve processing quality/legacy transform identity and introduce no
operation enablement. Documentation generator verification is recorded below.
Documentation check passed: 2059 static checks, 54 semantic scenarios, 164 schemas/51 endpoints/25 operations/40 tables/114 foreign keys and deterministic regeneration. This is documentation consistency evidence only, not full implementation or hardware qualification.

### Recipe LayerStack strictness and geometry prefix (2026-10-02)

The existing v2draft.ValidateLayerStack now rejects explicit null enabled/opacity,
duplicate layer IDs and duplicate JSON keys. Canonical parsing checks field
uniqueness before raw-message interpretation. Active optical.lens/perspective/
panorama/focus_stack layers must precede active photometric layers; disabled and
zero-opacity layers remain preserved and do not change the active prefix.
This implements the geometry-prefix invariant in 06-clients-integrations before
renderer graph construction. Existing exact canvas/mask/ref checks remain.

Tests cover valid lens→photometric order, each geometry operation after a
photometric layer, disabled/zero geometry, null scalars, duplicate layer identity
and repeated JSON enabled key. v2draft/v2learning/v2input Go regression passed.
This is validation and recipe admission correctness, not a finished LayerStack
renderer or public lens preview integration. No pixel-processing code changed.

### Ordered layer and reusable mask plan compiler (2026-10-02)

Added backend/internal/v2render.Compile over the existing source-bound
LayerStack validator. It retains recipe order, layer IDs, enabled/opacity/blend,
operation/version/params refs and mask expressions. Canonical expression hashes
deduplicate mask nodes; dependencies are emitted before parents. ActiveMasks
selects only the reachable subgraph for enabled nonzero-opacity layers without
raster IO for inactive layers, and rejects missing/cyclic/non-topological graphs.
The compiler performs no artifact publication, qualification or dispatch; the
scoped resolver must prove referenced artifact authority before calling it.

Tests cover shared subtract/union/invert/ref expressions across layers, exact
metadata/order preservation, deterministic plan hashing, owned document bytes,
empty/invalid whole stacks, inactive mask pruning, zero opacity and malformed
DAGs. v2render/v2draft/v2learning/v2input Go regression passed. Runtime mask
raster evaluation, layer compositing and recipe→task graph integration remain
unfinished; no lens preview/product completion is claimed from this compiler.

### Worker bounded probability-mask DAG evaluator (2026-10-02)

Added algorithms/layer_masks.py: compile_mask validates exact MaskExpr keys,
source canvas, strict wire ArtifactRef, depth16/node256 limits and emits reusable
canonical-hash nodes after dependencies. evaluate_mask_tile computes union=max,
intersect=min, subtract=max(0,a-b), invert=1-a in float32; leaf probabilities must
be finite in [0,1], exact shape/dtype. Borrowed source arrays are never modified.
Explicit max_tile_bytes admission bounds retained node buffers plus conservative
temporaries before resolver IO. Cancellation is checked per node and after IO.
mask_tiles emits owned <=1024-square tiles and partial canvas edges; no full
canvas allocation, maximum200MP. Resolver must supply scoped verified masks in
the target geometry; the evaluator does not invent artifact authority.

Regression **14 passed** for mask expressions plus existing dense-transform/
correspondence behavior. Tests cover probability semantics, repeated leaf reuse,
unchanged source buffers, wrong canvas/depth/probability, budget refusal before IO,
cancellation before IO and a257-wide partial tile edge. The initial strict-UUID
Python dict validation failure was corrected by validating canonical wire JSON,
retaining the existing strict contract instead of relaxing UUID validation.
Runtime artifact-backed mask resolution, layer compositing and public recipe
render integration are still required; no full product completion is claimed.

### Artifact-backed mask inputs for layer expressions (2026-10-02)

operations/layers/mask_inputs.resolve_mask_inputs uses existing TaskArtifacts
contract/input boundaries for declared MaskRef documents and their NPY refs.
Before exposing a tile callback it requires exact target canvas and transform
chain identity, materializes each unique leaf once, and validates complete
float32 probability NPY shape/layout/EOF/finite/unit bounds through read_array.
Geometry mismatch returns UNSUPPORTED_TRANSFORM and requires an explicit graph
mask.transform; equal dimensions alone do not establish correspondence.

ExitStack owns attempt materializations and read-only mappings, including setup
failure and consumer exceptions. The callback refuses undeclared refs, invalid
tile bounds and use after context close. Expression evaluator outputs are owned
and remain valid after mappings close. Unit tests cover shared leaf materialization,
actual NPY mapping, bad probability cleanup, canvas substitution before array IO,
consumer failure cleanup and closed callback refusal. Artifact provider is
injected in these tests; actual gateway/SQL-backed full layer rendering remains
unverified. Pixel compositing and recipe graph integration are still open.
Mask-expression/source, existing dense transform/correspondence and NPY regression: **30 passed in 1.02s**.

### Bounded linear adjustment-layer composition kernel (2026-10-02)

Added algorithms/layer_compose.compose_layer_tile for normal/multiply/screen in
the same working linear primaries. Mask opacity is applied once; any positive
protected probability forbids changes. Straight float32 alpha interpolates as
an adjustment, preserving unchanged alpha without source-over accumulation.
HDR/negative RGB remain unclamped; nonfinite/float32 overflow is rejected.
Explicit >=256bytes/pixel tile workspace admission, <=1024-square inputs and
cancellation bound the kernel. Inputs are never mutated; output/changed mask
are owned. Zero effect preserves implicit opaque alpha. Exact source/proposal
endpoints and unchanged linear/alpha bits avoid color round-trip changes,
including signed zero. Legacy uint16 alpha is explicitly refused pending the
adapter's source-ref preservation/declared conversion policy.

Independent expected blend values, protected/zero masks, opacity0 identity,
HDR signed-gamma blend, overflow, alpha interpolation/no accumulation, exact
normal/full and identity bit preservation, signed-zero alpha, memory refusal and
cancellation all pass. Combined layer/mask/input/lens-result/optical-color suite
**23 passed in 0.34s**. Initial multiply expected-value test was corrected from
.2 to .325 (source .5, proposal .3, opacity .5); implementation matched the
specified interpolation. Atomic scratch output adapter, recipe graph compilation
and public preview wiring remain required; no whole renderer completion claimed.

### Atomic scratch layer composition preparation (2026-10-02)

operations/layers/prepare.prepare_layer composes <=200MP source/proposal arrays
in256/512/1024 tiles through the admitted composition kernel, including partial
edges and callback-resolved mask/protected tiles. Scratch reservation accounts
for exact RGB/changed/optional-alpha NPY payloads plus header allowance. Float32
input/alpha shape/type, tile workspace and operation settings are checked before
output allocation. ExitStack flushes/closes mappings before yielding paths and
validates every attempt-owned file for later fenced staging.

LayerFiles.any_changed records actual RGB/alpha bit changes. The future adapter
must reuse original refs when false; this preparer performs no object upload or
publication. Optional output alpha is float32 and preserves exact source alpha
outside effect. Input canvas/transform/color provenance remains the adapter's
responsibility, not merely the preparer's equal array shape check.

Tests cover257-wide partial edges, exact protected/zero-mask RGB, preserved float
alpha, strict NPY readback, absent alpha/zero effect, normal context cleanup,
failure after first completed tile and disk quota before output allocation.
Combined layer/mask tests **22 passed in 0.37s**. Staging/adoption, full recipe
render graph and public preview integration remain incomplete; no product-ready
claim is based on scratch preparation alone.
Broader layer/lens scratch preparation and strict NPY regression: **39 passed in 1.08s**.

### Composed layer result closure and independent ref reuse (2026-10-02)

LayerFiles now records RGB and alpha changes independently. stage_layer_result
validates source binding and coherent change flags before any staged write,
stages only changed pixel/alpha payloads, and reuses unchanged source refs. With
no changes it reuses the original WorkingImage document. New image documents
retain source canvas/transform chain/color; changed MaskRef, deterministic
ImageReport and ImageResult close the declared output graph. No homography,
photographic identity or inference-generated pixel claim is manufactured.

The helper uses the fixed internal operation label layer.compose, not a caller
operation name. Its task params, registry/output policy, graph compiler, typed
adoption checks and inherited generated-mask/lineage handling still need complete
integration before this helper can become a dispatchable production operation.
Unit staging provider is injected, not actual gateway/S3 proof.

Tests cover no change/RGB only/alpha only/both, independent original ref reuse,
exact geometry/color, changed mask values and wrong canvas binding before any
staging. Initial layer preparation/composition/result suite **17 passed in
0.39s**. Wider mask/input/lens-result regression recorded below. No existing
operation was enabled and no full recipe-render readiness is claimed.
Combined layer result/preparation/composition/mask inputs and lens-result regression: **28 passed in 1.08s**.

### Frozen composition params and parent result lineage contract (2026-10-02)

Added strict LayerComposeParams with exact VersionBinding, required nullable
source ImageResult ref, proposal ImageResult ref, source-bound MaskExpr, required
nullable protected MaskRef, unit opacity and the three implemented blends.
Mask validation reuses bounded strict expression compilation; UUID refs parse
through strict wire JSON. Proposal is a result-document ref, retaining access to
report/generated-mask lineage rather than treating unbound pixels as sufficient.
Unknown fields, wrong canvas, unsupported blend, boolean opacity, missing lineage
field and null mask are rejected. Contract roundtrip/rejection tests **7 passed**.

Combined layer contract/result/preparation/composition/mask suite passed (below).
No registry activation or machine operation catalog entry is added yet: worker
parent provenance, source_result=null relational proof, inherited generated-mask
coverage and typed adoption must be implemented before registering this internal
operation. The deterministic-parent staging helper must not be used to discard
generated-mask lineage. Documentation records those explicit remaining duties;
this contract is not evidence of finished recipe rendering.
Staging now requires a proposal ImageResult and checks any source parent against the source WorkingImage ref. Generated parent masks explicitly fail before writes with LAYER_GENERATED_LINEAGE_PENDING until propagation is implemented; tests verify no staged output on that refusal. Final combined suite: **33 passed in 0.29s**.

### Layer generated dependency coverage kernel (2026-10-02)

Added algorithms/layer_lineage.generated_coverage with bounded <=1024-square
float32 binary input masks, explicit64bytes/pixel workspace admission and
cancellation. It uses the same clipped mask/opacity/strict protected effect.
Normal E=1 removes source dependency; normal E<1 retains it. Multiply/screen
conservatively retain source coverage. Proposal coverage is inherited wherever
E>0, even for bit-identical pixels. Output is owned binary union; both missing
parents return None. Nonfinite/nonbinary/wrong-shape masks and insufficient
memory are rejected. This records dependency coverage, not authenticity.

Coverage tests exercise source replacement/partial mix/protection, multiply/
screen source dependency, opacity0, absent masks, binary enforcement, workspace
refusal and cancellation. Coverage+composition+result regression **19 passed in
0.23s**. This kernel is not yet wired into scratch files/staging/adoption; the
existing staging rejection for generated parent lineage remains until those
phases verify actual parent masks. No production registry activation occurred.

### Layer generated coverage in attempt scratch (2026-10-02)

`operations/layers/prepare.py` now accepts optional source/proposal generated-coverage tile readers and writes a binary Float32 NPY alongside the composed output. Reservation includes the additional 4 bytes per canvas pixel; mappings close before staging and reservation cleanup removes the generated file on completion or any failure. Coverage computation runs even when pixel values are identical, retaining generated dependencies independently of RGB change flags. Normal full replacement drops source coverage; multiply/screen retain it; protected pixels retain source dependencies and block proposal dependencies.

Verification: 27 unit cases passed across preparation, lineage, composition and result staging, including partial edge tiles, strict protection, identical proposals, invalid binary coverage in the second tile and cleanup. This is scratch preparation evidence only. Parent artifact resolution, coverage-to-parent validation, generated-mask staging, operation registration, task admission/adoption and full renderer remain integration work. Existing staging rejection for generated parents remains in force.

### Layer parent and binary lineage resolution (2026-10-02)

`operations/layers/parents.py` resolves the declared source WorkingImage and parent ImageResults, verifies the source parent points to the exact source ref, and requires matching canvas, full transform chain, Float32 dtype and color metadata for the proposal. Every generated MaskRef descriptor is checked against the source geometry before NPY reads. Shared coverage descriptors are materialized once, completely validated as binary Float32 in bounded blocks, and exposed through bounds-checked readers only while the context is open. Input reservations and mappings close on validation/consumer failure; post-close reads fail before accessing closed mappings.

Verification: 47 layer unit cases passed, including four parent-resolution cases exercising shared coverage, source substitution, color mismatch before array IO, nonbinary coverage, consumer failure, reader lifetime and absent generated lineage. These tests use an injected artifact provider and actual local NPY files. They do not prove gateway authorization, source-original admission or end-to-end layer execution. SQL still must prove that null source_result identifies the permitted original snapshot source. Generated-parent staging remains guarded until the resolver, preparation and publication are joined with explicit parent coverage proof.

### Joined layer preparation and generated-mask staging (2026-10-02)

`operations/layers/inputs.py` now joins parent resolution, MaskExpr evaluation, optional protected-mask resolution and exact Float32 RGB/alpha mappings to tiled preparation. Shared pixel/alpha inputs are cached by exact artifact identity and layout. Source/proposal generated-mask refs are carried into LayerFiles; staging refuses mismatched or missing coverage bindings before any writes. Generated coverage is staged as NPY plus a geometry-preserving MaskRef and included in ImageResult, independently of WorkingImage pixel changes. Empty coverage remains an explicit binary mask when generated parents existed.

Verification: 51 layer unit cases passed. Four joined-path cases use actual NPY files with injected artifact loading/staging, exercising full normal replacement, multiply/screen inheritance, unchanged RGB with changed lineage, output closure, mismatched parent rejection before writes and complete context cleanup. The earlier unconditional generated-parent staging guard is replaced by exact coverage binding checks. This internal path is not yet registered for production dispatch: authoritative admission, resource profile, gateway output policy, task graph/adoption SQL and full recipe rendering remain required. Alpha remains Float32-only here; legacy uint16 requires the specified explicit conversion path. No photographic authenticity or identity verification is inferred from dependency coverage.

### Layer resource preflight (2026-10-02)

`operations/layers/resources.py` computes conservative CPU estimates from canvas size, distinct MaskExpr/protection refs and deduplicated DAG node count. N=canvas pixels, T=actual maximum tile pixels, R=distinct expression/protection refs, D=DAG nodes: mapped allowance=(68+4R)N; RAM adds max(256,4(D+3))T plus 256 MiB; scratch adds 64 MiB; output allowance=24N+32 MiB; VRAM=0. Full mapped pages are counted rather than relying on eviction. Estimates cover source/proposal RGB/alpha, both parent coverage masks, probability/protection arrays and prepared outputs. Multiple distinct mask descriptors sharing a payload are conservatively counted separately.

The joined input helper accepts an execution profile and performs resource admission after source/proposal descriptor validation but before lineage or pixel NPY downloads. Pixel count and CPU VRAM must match; RAM/scratch/output estimates must meet requirements; tile temporary budget is checked independently. Production dispatch must provide this profile; unprofiled use remains internal helper testing only. No operation registry activation is implied.

Verification: 64 layer unit cases passed including budget changes for a 200MP canvas and 256-node DAG, insufficient RAM/scratch/output, mismatched pixel/VRAM values and refusal before any array input opens. Joined prepare/stage cases also pass with a valid admitted profile (17 resource/joined cases rechecked). Scheduler-side equivalent estimation and host-cap enforcement remain integration work.

### Parent report gates and internal layer worker adapter (2026-10-02)

Layer parent resolution now reads declared ImageReports before any coverage-array input: fail/unavailable parents reject composition, review parents remain review. Staging repeats the gate before writes and emits review status/reason LAYER_PARENT_REVIEW plus review_required instead of silently replacing review with pass. No parent identity/registration evidence is fabricated; the full parent result closure remains necessary for graph/adopter validation.

`operations/layers/adapter.py` joins typed params, mandatory injected relational admission, CPU renderer v2.2 validation, resource preflight, verified inputs, tiled preparation and result staging. Null admission rejects without artifact reads. CPU tile budget is derived from the compiled DAG, and task cancellation checks surround admission and publication. No runtime operation registration was added: a production relational admission implementation, registry/output policy, graph/adoption validation and deployment wiring remain required.

Verification: 74 layer unit cases passed. Report tests cover pass/review across unchanged, RGB-only, alpha-only and combined outputs, and fail/unavailable refusal before array IO. Adapter tests cover missing admission, incompatible renderer/backend and propagation of authoritative admission rejection before source reads. Successful worker execution against real gateway/SQL remains unproven; joined local prepare/stage evidence does not substitute for that integration test.

### Successful layer adapter and Go resource estimates (2026-10-02)

The complete internal layer adapter path now has actual local NPY verification for source/proposal composition, generated output closure, progress completion, pass/review propagation and input/scratch cleanup. Rejected parent reports perform no array reads or writes; cancellation during staging propagates the task error and cleans prepared files. Artifact provider and relational admission are injected in these tests: this does not establish live gateway or SQL admission correctness.

`backend/internal/v2render/resources.go` now provides the same conservative single-composition resource formulas as the CPU worker, using int64 counts and validating dimensions before multiplication to reject overflow. It is available for graph compiler/scheduler integration; host-cap scheduling is not yet wired. Go tests validate fixed worker-parity values, 200MP/256-node budget deltas and invalid/overflow inputs.

Verification: 79 Python layer cases passed; Go v2render tests passed. Production layer registry activation remains pending relational admission, output policy, immutable graph adoption and end-to-end integration. Successful injected admission is not a substitute for authoritative tenant/version/snapshot proof.

### Per-layer compiled mask resource budget (2026-10-02)

`Plan.BudgetLayer` now selects an exact active layer ID, validates its reachable topological mask DAG, counts distinct leaf artifact ID/SHA pairs plus optional protection and applies worker-equivalent resource formulas. Unrelated active-layer masks do not inflate this composition's estimates. Inactive/missing/duplicate layer selections and malformed/cyclic/non-topological node shapes fail; invalid protection refs fail. Returned leaf refs are copied, preventing callers from mutating the source plan through budget results.

Verification: Go v2render, v2draft and v2input package tests passed. Tests cover an actual compiled subtract/union/invert expression with shared leaves and protection deduplication, inactive layer refusal, unrelated-mask exclusion, independent output ownership, additional protection cost, malformed DAGs and invalid refs. This compiler method supplies one task's estimate; it does not yet enqueue or admit tasks, implement host concurrency accounting, or establish live SQL/gateway layer admission. Production integration remains required.

### Frozen Go layer-composition wire contract (2026-10-02)

`v2render.CompileComposition` now validates one active same-geometry layer, current VersionBinding, parent/protection refs and bound MaskExpr through the existing strict layer compiler. It returns canonical LayerComposeParams JSON and its per-layer budget. Nullable source_result/protected_mask keys are always present. Mask JSON and nullable refs are copied; inactive layers, wrong mask canvas and invalid parents fail. This is a pure compile step: byte resolution and authoritative SQL proof of null original lineage are still required before task creation.

Verification: Go v2render/v2draft/v2input passed. With PHOTO_LAYER_PYTHON pointing to the actual repository worker environment, a cross-language test feeds generated Go JSON directly into Python LayerComposeParams.model_validate_json for null and populated parent/protection cases; canonical SHA256 matches after Python serialization. This establishes wire/canonical compatibility, not gateway scope or SQL admission. Production graph creation/registration/adoption remains unfinished.

### Composition input roots (2026-10-02)

`CompositionInputRoots` enumerates the typed direct document dependencies of frozen layer params: Params, source WorkingImage, source/proposal ImageResults and expression/protection MaskRefs. It verifies canonical params bytes and their artifact digest, recompiles the wire MaskExpr instead of trusting a mutable cached budget, sorts UUIDs, deduplicates exact entries and rejects one artifact ID assigned conflicting SHA/contracts. This is the root inventory for scoped recursive resolution; it is deliberately not published as a complete InputClosure. Pixel/alpha arrays, generated masks, reports, transform fields and evidence children must still be resolved and declared.

Verification: Go v2render tests passed for sorted/deduplicated parent/mask roots, wrong params digest and conflicting/inconsistent dependencies. Recursive typed closure expansion and authoritative SQL registration remain the next integration work; no production layer activation is claimed.

### Typed ImageResult traversal in scoped closure resolver (2026-10-02)

The existing v2draft scoped closure traversal now recognizes ImageResult and validates its exact schema_version/working_image/changed_mask/report/generated_mask wire shape before exposing children. WorkingImage, ImageReport and MaskRef children retain their parent ref and typed contract; generated_mask is required-nullable. This avoids losing image/mask type checks through generic Artifact recursion when parent ImageResults are supplied as closure roots. Existing catalog authorization, immutable JSON hash checks, bounded traversal and per-occurrence parent scope checks remain in force.

Verification: v2draft and v2render tests passed. New cases cover typed children and parent refs, present/null generated coverage, omitted nullable fields, extra fields, null required refs, invalid schema/ref and duplicate JSON keys. No live composition closure registration was performed. Exposing composition roots through an authorized resolver entry point, expanding report evidence with contract validation, SQL scope support and complete task admission still require implementation; this change alone is not a complete task InputClosure.

### Strict ImageReport traversal (2026-10-02)

Scoped closure traversal now validates ImageReport's exact eleven fields, schema/status, operation/reason lengths, skipped count, finite metric ranges, required nullable evidence refs and bounded source/registration arrays. Source VersionBindings require exact keys and valid version/source/canvas digests; their WorkingImage dependencies carry the declared canvas hash. Identity evidence is typed IdentityReport. Transform/registration evidence retains Artifact typing because the current ImageReport contract does not declare one universal registration document type; no invented evidence schema is assumed.

Verification: v2draft/v2render Go tests passed for source canvas/parent typing, nullable/populated evidence, missing/extra fields, invalid status/length/count/range/null values, invalid sources and evidence refs. The resolver enforces structure and traverses refs, not photographic qualification or parent review acceptance (the worker handles review/rejection gates). IdentityReport's own strict validation, authorized composition resolver entry point, SQL scope support and full task registration remain unfinished.

### Strict nested IdentityReport dependencies (2026-10-02)

Scoped closure traversal now validates IdentityReport's exact schema/status/face metrics/masks/calibration shape. Face arrays are bounded to 100 entries, every face uses an exact UUID/similarity/landmark_drift object, nullable metrics are explicit, similarity is within [-1,1] and drift within [0,1]. The outside-mask error must be finite within [0,1000000]. Required refs are strict; generated_mask remains required-nullable. Changed/generated masks are typed MaskRef dependencies and calibration is typed Calibration, with parent occurrence scope retained.

Verification: Go v2draft/v2render passed. Tests cover not_measurable/nullable metrics, valid finite metrics, typed evidence refs, missing/extra fields, invalid statuses, null arrays/required values, invalid calibration, out-of-range metrics, null face IDs and oversized arrays. This validates document transport and nested dependencies; it does not qualify an identity model or establish an authenticity assertion. End-to-end report traversal with live SQL scope and composition task closure registration remains required.

### Recursive image-result evidence traversal verification (2026-10-02)

A new scoped traversal test constructs a hash-verified JSON graph containing ImageResult, a real repository WorkingImage fixture, shared MaskRef, ImageReport, IdentityReport and calibration plus pixel/mask binary catalog descriptors. The real ClosureResolver walks all eight dependency types and retains occurrence-level catalog lookups when WorkingImage/mask refs recur. Denying the nested Calibration lookup rejects the entire resolution. The mask fixture includes the required nullable object_id; its initial omission was corrected rather than relaxing parsing.

Verification: Go v2draft/v2render/v2input passed. JSON bytes and hashes are real; object/catalog access is injected, binary payloads are metadata-only in this test. This proves recursive discovery and propagation of catalog rejection, not live SQL provenance or NPY content verification. Calibration bytes are synthetic discovery evidence and do not constitute qualification. Composition admission, operation registration and real gateway/SQL execution remain unfinished.

### Repeated dependency schema/canvas validation fix (2026-10-02)

Inspection found that the existing seen-artifact shortcut returned after catalog authorization, skipping context-specific canvas/protection checks and allowing an earlier generic Artifact encounter to bypass later typed validation. The resolver now tracks successful contract validation separately. Repeated WorkingImage/MaskRef documents are revalidated for each occurrence; generic-to-typed promotion performs the typed schema check and child traversal. Already-expanded matching typed dependencies retain bounded deduplication. Every occurrence checks immutable object SHA/version/size and typed media expectations before any shortcut.

Verification: Go v2draft/v2render/v2input passed; repeated-source wrong-canvas and generic malformed-document promoted to WorkingImage now reject. Existing recursive evidence tests still prove nested scope rejection. This is a resolver correctness fix, not proof of production composition registration or completion of the V20–V32 plan.

### Scoped task input inventory entry point (2026-10-02)

`ClosureResolver.ResolveTaskInputs` accepts bounded typed compiler roots (Params/WorkingImage/ImageResult/MaskRef), requires the exact source WorkingImage root and validates the VersionBinding. It expands dependencies through the existing catalog/object resolver, records every authorized occurrence, maps pixel/alpha/warp/mask binaries to NpyArray, promotes generic references to validated specific contracts, and returns UUID-sorted unique entries. Conflicting identities/contracts reject. Source traversal is performed first so source-dependent checks remain initialized even when compiler roots arrive in UUID order.

Verification: v2draft/v2render/v2input tests passed. The recursive evidence fixture now also uses this public entry point and checks all eight unique dependencies, types and sort order; missing source and generic roots reject. Catalog/objects are injected: real SQL scope, graph admission/revalidation, transport publication and production layer registration are still unfinished. The returned inventory is preparation evidence, not task authorization.

### Frozen composition inventory and InputClosure transport (2026-10-02)

`FreezeCompositionInputs` validates exact typed root coverage against the compiled params, rejects duplicate artifact IDs and invalid/conflicting input identities, freezes a sorted copy of the complete scoped resolver inventory and supplies a repeatable cancellation-aware CursorFactory. The input digest binds task ID, layer.compose/version1 and all entries. Params is the sole direct transport handle; the full manifest carries all image/mask/report dependencies, avoiding the direct-ref count limit for larger DAGs. Completeness of descendants still relies on the scoped resolver; this function does not authorize inputs supplied by an arbitrary caller.

Verification: v2render/v2draft/v2input passed. Tests mutate the caller slice, reopen cursors twice, check task-sensitive digest, cancellation, missing/erased root types and duplicates. The actual BuildInputClosure generator consumes the frozen cursor and publishes one chunk plus manifest through an injected SHA-verifying publisher; count and canonical entries SHA match. Production immutable publisher purposes, SQL graph shell/admission and registry activation remain unfinished.

### Composition preparation coordinator (2026-10-02)

`PrepareComposition` now joins strict layer/params compilation, params digest validation, typed root construction, scoped resolver expansion, frozen inventory and InputClosure chunk/manifest publication. It carries tenant/actor/session/task identity to resolution and returns a GraphTask with the repeatable cursor, params direct ref and input digest for subsequent atomic graph admission. Resolver refusal or params mismatch prevents transport writes. Publication happens outside a database admission transaction; this helper does not create task shells, prove original lineage, enqueue or seal a graph.

Verification: v2render/v2draft/v2input passed. Coordinator tests verify identity/binding propagation, complete phase execution, GraphTask transport handles and rejection before publication/resolver IO. The fake resolver returns only fixture roots for phase verification; recursive completeness is tested separately through ClosureResolver, and production must inject that scoped implementation. Durable params/transport publication purposes, SQL graph shell and layer-specific authoritative admission/adoption remain unfinished.

### Composition transport/input identity separation (2026-10-02)

Preparation now verifies that published InputClosure chunk/manifest artifact IDs do not collide with any frozen input dependency, including refs available only through the manifest. The existing generic admission validator checks direct refs, so this full-inventory check closes the gap introduced by using only Params as the direct handle. Verification reopens the frozen cursor without object IO and remains cancellation-aware.

Verification: v2render/v2draft/v2input tests passed. An injected publisher returning a transport chunk with the proposal ImageResult ID is rejected before a PreparedComposition can be returned. Live publisher reservation guarantees, SQL layer-specific admission and production dispatch remain unimplemented; this collision test does not establish those gates.

### Parent report version binding and SQL scope boundary (2026-10-02)

Current SQL v2_draft_artifact_scope explicitly excludes ImageResult/ImageReport/IdentityReport/Calibration typed dependencies and limits WorkingImage access to accepted masters. It cannot currently serve production composition parent results. No permissive alias to generic Artifact scope was introduced; a dedicated admitted-result provenance path and migration remain required.

Worker parent review validation now additionally requires each parent report to include the composition's exact asset_version_id among its declared sources, both during resolution and before staging. This catches foreign-version reports without demanding that a geometry operation's original source ref equal its output WorkingImage. This JSON consistency check complements, and does not replace, authoritative same-version SQL lineage admission.

Verification: 82 Python layer cases passed, including foreign version reports on both parents and adapter refusal before any array reads or writes. All existing composition/review/cleanup cases remain green. Production SQL scope extension, graph shell/admission, publisher reservation and registry activation remain unfinished.

### Live SQL composition scope boundary (2026-10-02)

The real PostgreSQL V21 quota-backed draft upload integration test now explicitly probes ImageResult, ImageReport, IdentityReport and Calibration contracts under photo_api with an authorized tenant/project/version/session and a ready verified project upload. All four typed probes reject while the legitimate LayerStack probe succeeds. This establishes that project upload readiness cannot impersonate an admitted composition parent.

Verification: targeted live PostgreSQL integration test passed (1.97s), preserving quota/fence/replay checks. Inspection confirms that task canonical_result_id plus v2.task.adopted markers and v2_artifact_links record adoption, but version-specific scope must additionally bind the producer's frozen source selection; workflow/project membership alone is insufficient. No migration broadening access was applied. Dedicated producer/version provenance and its positive/negative live tests remain necessary before production composition activation.

### Adopted lens result source proof, migration 329 (2026-10-02)

Forward-only migration 00329 adds API-only read-only v25_adopted_lens_binding. Under current tenant/editor/session authorization, it proves the producer task/version1, exact canonical attempt/fence/result, root document ready/versioned metadata, adopted event/inbox marker, workflow artifact link and frozen lens node's source/version/canvas against the snapshot and accepted project asset version. The source must be declared as WorkingImage input. It returns result/root/source/canvas identities; it grants no generic artifact access and does not modify v2_draft_artifact_scope. This proof currently supports optical.lens producers only; other producer kinds and nested output scope remain future integration.

Verification: actual PostgreSQL + isolated versioned MinIO typed lens tests passed (3 cases, 10.91s). Strength0 and strength1 adopted roots return the exact expected binding; wrong version/project and later revoked session return no row; concurrent revocation adoption rejection remains valid. The fixture source type is assigned on INSERT because task inputs are immutable; no immutability guard was relaxed. Migration 329 is tracked under V25. No photographic qualification is claimed for synthetic frames, and production composition activation remains unfinished.

### Go adopted-lens source loader (2026-10-02)

`v2render.SQLAdoptedLensSource.Load` calls migration329's binding proof inside tenant.WithTenant with active editor authorization and exact actor/session/project/version/producer identity. It validates the expected root ref, canonical result ID, source ref and canvas digest before returning the binding; absent proof is ErrExpired and malformed/mismatched proof is ErrInvalid. It does not expose object bytes or broaden generic catalog access. Callers still must resolve and SHA-verify result/child documents and integrate layer-specific admission.

Verification: Go v2render/v2draft/v2input tests passed. New unit cases cover mismatched root/version, missing result ID, malformed source/canvas and unconfigured database. The SQL function's positive/negative live tests were established separately; the Go transaction wrapper itself has not yet been run against that live adopted fixture. Production catalog/graph wiring remains unfinished.

### Live Go adopted-result transaction verification (2026-10-02)

The typed lens PostgreSQL/MinIO integration fixture now invokes the actual Go SQLAdoptedLensSource through a photo_api pool before and after session revocation. The loader additionally requires current_user=photo_api inside its tenant transaction, preventing accidental privileged-pool use. Positive proof matches exact canonical result/root/original VersionBinding; wrong version/project/root and revoked sessions produce no usable binding.

Verification: 3 real typed lens integration cases passed, including the cross-language Go transaction probe. No connection credentials are printed; subprocess input uses isolated synthetic fixture state. This verifies the loader, not parent child-object resolution or production composition registration. The initial SQL-only proof and injected unit evidence are now complemented by actual Go tenant authorization/transaction execution.

### Adopted lens immutable root object, migration330 (2026-10-02)

Forward-only migration330 adds API-only v25_adopted_lens_object. It derives access exclusively from the adopted producer/version proof and exact expected root ID/SHA, returning only the root's immutable JSON object metadata (bounded to 1 MiB). It grants neither arbitrary workflow artifacts nor nested child access. SQLAdoptedLensSource.LoadObject first verifies the root binding and then rechecks authoritative access in a separate tenant transaction; it validates deployment bucket, nonempty key/version, exact JSON media and bounded size. Object IO remains outside DB transactions, with byte/SHA verification still required by the consumer.

Verification: 3 live PostgreSQL/versioned MinIO integration cases passed (13.20s), with the actual Go loader checking key/version/size/SHA against the published root and rejecting a wrong bucket. Go v2render/v2draft/v2input also passed. Migration330 is tracked under V25. Root-byte loading/strict ImageResult parsing, nested admitted-child scope, composition graph admission and production dispatch remain unfinished.

### Verified adopted root bytes and typed ImageResult parsing (2026-10-02)

`LoadAdoptedLensResult` joins authoritative object lookup with post-transaction immutable object reading, exact length/SHA256 and canonical JSON verification, then strict ImageResult parsing. Reads are bounded to the admitted length plus one byte (maximum1MiB), use 64KiB blocks with cancellation checks and close the reader on all read/parse failure paths. SQL binding is checked again before IO. Child refs are returned as typed values without granting child access. The exported v2draft.ParseImageResult reuses the exact required-nullable wire validator.

Verification: v2render/v2draft/v2input passed. Six object-reader cases cover valid bytes, truncation, trailing bytes, hash mismatch, omitted generated_mask and noncanonical bytes; reader cleanup is asserted for all. These byte-read tests inject the object reader/source. Actual SQL metadata lookup was verified separately against PostgreSQL/MinIO; the combined root-byte helper has not yet been exercised with the real objectstore reader. Nested scope and complete production composition remain unfinished.

### Real Go SQL + objectstore adopted-root read (2026-10-02)

The existing live typed lens fixture now passes isolated MinIO configuration to the Go integration test and executes LoadAdoptedLensResult with the actual production objectstore.Store. Fixture uploads include the SHA256 object metadata required by Store.Check. Before the positive read, the test writes a different newer version at the root key; the helper must still read the exact SQL-pinned older VersionID and obtain the expected typed ImageResult. The original missing metadata failure was fixed in fixture publication, not by weakening integrity verification.

Verification: 3 PostgreSQL/versioned MinIO cases passed (12.45s). The actual Go path performs tenant authorization, producer/version/root proof, object metadata checks, versioned GET, EOF digest validation, independent exact-length/SHA/JCS checks and strict typed parsing. Isolated credentials remain environment-only and are not printed. This proves adopted-root reading for lens; it does not grant nested input scope, establish model qualification or complete production composition registration.

### Exact adopted lens child occurrence scope, migration331 (2026-10-02)

Migration331 adds API-only v25_adopted_lens_child_object. It reuses the exact adopted root/version proof, then requires the child's task-qualified adoption inventory path hash, workflow/project link, exact artifact SHA, producer attempt/fence, ready/versioned state and explicit contract-to-output-role/media mapping. It does not authorize another task's output via workflow membership and does not grant arbitrary project inputs. Reused original inputs still require their separate original-source scope; this function authorizes staged adopted output occurrences only.

Verification: 3 PostgreSQL/versioned MinIO typed lens cases passed (12.86s). Positive checks cover report, changed MaskRef, its nested mask NPY and nonzero lens RGB NPY. Wrong inventory paths and role/contract substitutions reject. Existing exact root reading and authority/revocation checks remain green. Migration331 is tracked under V25. Go child loader, adopted-result catalog adapter, reused original scope and complete composition task admission remain unfinished.

### Go admitted child lookup and real versioned reads (2026-10-02)

SQLAdoptedLensSource.LoadChildObject wraps migration331 in a photo_api tenant transaction and requires exact root/child refs, producer/version/project/actor/session, valid task inventory path and a narrow contract allowlist. Deployment bucket, immutable metadata, media type and bounded document/NPY size are checked after lookup. NPY caps cover the 200MP RGB/alpha/mask/XY field layouts plus header allowance; actual shape/dtype/value checks still belong to the consuming array reader. Reused original inputs are intentionally outside this staged-output scope.

Verification: Go v2render passed, and 3 live PostgreSQL/MinIO cases passed (13.49s). The actual Go path resolves report and changed-mask objects, reads their pinned versions through production objectstore.Store to EOF and verifies exact lengths/SHA; wrong inventory paths and contract substitutions reject. Unit cases cover path grammar and contract/media/size allowlist. This is output-occurrence access evidence; catalog integration, typed child JSON geometry validation, reused original scope and complete composition admission remain unfinished.

### Closure binary media compatibility (2026-10-02)

The typed dependency closure now accepts both application/x-npy (staged adopted lens arrays) and application/octet-stream (existing original arrays) for WorkingPixels, WorkingAlpha, MaskPixels and DenseWarpField. It preserves the catalog's exact object metadata rather than rewriting media types. JSON and unrelated binary media remain rejected for those contracts, including repeated occurrences before the validation cache shortcut. This resolves the media mismatch between migration331's admitted output objects and the existing closure reader.

Verification: Go v2draft/v2render/v2input passed. The full typed ImageResult dependency fixture checks both allowed media and rejects application/json, image/png and empty media for pixels. This is metadata compatibility evidence, not NPY payload/shape validation or a complete adopted-result catalog integration. Original-source scope, exact adoption occurrence mapping and composition admission remain unfinished.

### Exact schema array paths for adopted dense fields, migration332 (2026-10-02)

The actual adoption schema walker represents array indices by a literal `*` segment. Migration331 and the Go child loader rejected this segment, preventing access to an adopted dense inverse field. Forward-only migration332 replaces only the function's path grammar: segments must be identifiers or the literal `*`; empty segments, trailing slashes and embedded wildcards reject. The task-qualified SHA256 purpose equality remains exact, with no SQL wildcard matching. The Go loader uses the same grammar. Migration332 is tracked under V25; previously applied migration331 remains unchanged.

Verification: 3 real PostgreSQL/versioned MinIO cases passed (13.20s), and Go v2render/v2draft/v2input passed. Nonzero lens output's dense field resolves through its actual first inventory occurrence `$/changed_mask/transform_chain/steps/*/inverse_field`; substituting numeric index `0` rejects. The live Go loader reads the field's pinned NPY object to EOF through production Store with integrity checks. Path unit cases cover literal array segments and malformed paths. Synthetic IO evidence does not prove photographic qualification, NPY geometry consumption or complete composition admission. Catalog integration still remains unfinished.

### Closure-edge to adoption inventory path translation (2026-10-02)

`adoptionChildPath` converts a closure child path relative to its verified parent into the corresponding adoption inventory path. It keeps identifier segments and replaces numeric array indices with literal `*`, including nested arrays. Exact parent boundaries, malformed indices, leading-zero indices, invalid characters and oversized paths reject. This helper performs syntax conversion only; it does not prove parent provenance or grant artifact access. Shared references still need their exact first-visit inventory occurrence, which is not necessarily the current closure traversal occurrence.

Verification: Go v2render/v2draft/v2input passed and 3 real PostgreSQL/versioned MinIO scenarios passed (13.99s). The actual Go integration derives the dense field path from `TaskInputs[3].changed_mask.transform_chain.steps[0].inverse_field`, then uses that derived path for authoritative SQL lookup and pinned-object reading. Unit cases exercise array nesting and ambiguous or malformed edges. Full catalog construction, shared-reference first-visit indexing, original-input scope and composition admission remain unfinished.

### Adopted lens catalog and shared-reference first-visit index (2026-10-02)

`BuildAdoptedLensCatalog` now verifies the adopted root and builds a bounded typed dependency index in adoption schema traversal order. JSON children are authorized through exact task-qualified SQL paths, read from pinned versions, closed, length/SHA/JCS checked and parsed before exposing their direct references. Binary children require the narrow contract/media/size metadata checks; indexing does not read or validate NPY payloads. Depth32, 512 entries, cycles and conflicting ID/SHA/contracts reject. Exact original WorkingImage inputs terminate the adoption walk, matching the worker inventory semantics.

`DocumentChildren` exposes validated direct references for ImageResult, ImageReport, WorkingImage and MaskRef. Root property ordering follows adoption, while report and nested document array order is preserved. The immutable index records the first occurrence of shared output refs. `AdoptedLensCatalog.Lookup` repeats current SQL authorization for every output access and checks the original actor/session/tenant. Unknown refs use the existing original project/version catalog scope; no new generic workflow access is granted. Production construction fixes this original catalog to the same SQL pool, bucket, project and version.

Verification: Go v2render/v2draft/v2input passed and 3 real PostgreSQL/versioned MinIO cases passed (16.94s). Both zero-strength original-image reuse and nonzero outputs build the actual Go catalog. An independent changed-mask root resolves; the shared inverse field encountered through WorkingImage resolves via its first changed-mask inventory path. Contract and identity substitutions reject. Root/session revocation probes remain green. Unit checks cover root schema ordering and malformed supported documents. Complete ClosureResolver geometry propagation, actual original descendant lookup, NPY shape/value consumption, composition admission and production dispatch remain unfinished; this is catalog construction/access evidence, not complete render admission.

### Real original-source scope and unchanged-geometry closure (2026-10-02)

The typed lens fixture now records its original master/pixel provenance against a distinct synthetic source task in a separate run. This satisfies the existing original-array scope's exact producer/attempt/fence checks. An initial placement of this source task inside the lens run correctly failed V25_LENS_SCOPE_STALE because that run must match its frozen graph; the fixture was corrected without weakening admission.

Verification: 3 PostgreSQL/versioned MinIO cases passed (18.29s), and Go v2render/v2draft/v2input passed. The actual AdoptedLensCatalog resolves the original WorkingImage through existing SQL scope, parses its pinned bytes and authorizes/reads the original pixel NPY to EOF through production Store. Substituting the report as the pixel's parent rejects. For zero-strength lens, actual ClosureResolver.ResolveTaskInputs expands the real source plus adopted ImageResult into the complete deduplicated typed metadata inventory. This covers unchanged geometry; nonzero lens changes canvas/chain and still requires explicit geometry propagation in the resolver. NPY shape/value checks, complete composition admission and production dispatch remain unfinished.

### Adopted result geometry agreement and full changed-geometry closure (2026-10-02)

The adopted lens catalog now hashes parsed WorkingImage/MaskRef canvas and complete transform chains, and rejects disagreement between the result image and its masks before exposing the catalog. Zero-strength reuse obtains its expected geometry from the scoped, pinned, SHA/JCS-verified original WorkingImage and checks the original VersionBinding canvas hash. The optional v2draft.GeometryCatalog contract supplies this verified immutable geometry after each occurrence's ordinary SQL authorization; existing catalogs keep their original geometry checks.

ClosureResolver verifies adopted image/mask canvas and complete chain hashes against that catalog metadata, rejects conflicting explicit image canvas bindings and still requires SourceSHA256 to match the original source established by the first source root. Repeated image/mask occurrences retain these checks. The geometry metadata does not replace SQL authority, artifact integrity, NPY shape validation or the composition worker's geometry compatibility checks.

Verification: Go v2render/v2draft/v2input passed, and 3 real PostgreSQL/versioned MinIO cases passed (15.77s). Actual ResolveTaskInputs now expands both zero-strength and nonzero dense-warp lens results, retaining the original source and all deduplicated typed dependencies. Injected geometry cases reject mask canvas/chain mismatch and malformed geometry hashes; image geometry mismatch rejects as well. Composition task admission, NPY payload qualification and production renderer dispatch remain unfinished. This evidence proves scoped typed metadata closure, not complete composition execution.

### Composition parent preflight before transport publication (2026-10-02)

`VerifyCompositionParents` resolves only scoped pinned JSON documents, checks exact length/SHA/JCS with reader cleanup, verifies the source canvas binding and actual width/height, and requires Float32 plus identical proposal/source canvas, color and full transform chain. A supplied source ImageResult must refer to the exact source WorkingImage. No NPY payload is opened by this preflight. A null source result remains subject to eventual authoritative original-source admission; parent compatibility alone is not that admission.

`PrepareVerifiedComposition` validates compilation and frozen params before object IO, performs this parent preflight, then connects the actual ClosureResolver to existing immutable inventory/transport preparation. It still returns an unregistered GraphTask; it does not create shells, admit lineage or enqueue.

Verification: Go v2render/v2draft/v2input passed. Unit checks prove scope denial cannot open objects/publish transport, and malformed params or cancellation cannot reach the catalog. Three real PostgreSQL/versioned MinIO cases passed (17.04s) with parent verification: zero-strength same geometry passes, nonzero lens geometry relative to the original source rejects as a photometric composition, and incorrect dimensions reject. The complete PrepareVerifiedComposition transport path has not yet been exercised with a real registered params artifact/publisher. Relational composition admission, effective sources after geometry stages and production execution remain unfinished.

### Machine LayerComposeParams and compiler/schema/worker wire parity (2026-10-02)

The documentation generator now emits strict LayerComposeParams with VersionBinding, required-nullable source_result/protected_mask, typed proposal ref, recursive MaskExpr, opacity [0,1] and the three supported blends. This adds a machine contract without registering or activating layer.compose. The algorithm document now accurately records implemented runtime generated-coverage helpers while retaining the incomplete admission/adoption/production gates. JSON Schema checks structural wire shape; mask canvas agreement and DAG depth/node limits remain semantic runtime checks.

Verification: the full documentation checker passed deterministically (2067 static checks, 54 semantic scenarios, 165 schemas, 25 operations). All 97 layer unit tests passed, including 22 contract tests with nullable/nonnullable parents, all blends, recursive mask expressions and malformed-wire rejection by both schema and worker. Actual Go-generated JSON passes the generated JSON Schema and Python worker parser with identical canonical SHA256 for both parent/protection variants; Go v2render/v2draft/v2input passed with this cross-language check enabled. No SQL composition registration or production activation is claimed.

### Frozen composition draft identity and computed CPU profile (2026-10-02)

`CompileCompositionDraft` now produces the exact blocked DraftTask plus owned frozen inventory before any transport publication. It requires UUIDv5(workflow, layer.compose:ordinal) to match the requested task, validates compiled/frozen params and complete root coverage, and binds the shell's input digest to task/operation/version/entries. Execution profile is computed from the actual compiled mask DAG and dimensions: CPU, renderer v2.2, conservative RAM/scratch/output and zero VRAM. The caller cannot supply an alternative resource estimate through this API. No shell write, SQL admission or enqueue is performed by this pure compiler function.

Verification: Go v2render/v2draft/v2input passed with the real worker Python cross-language test enabled. Draft tests cover stable task identity, params/digest binding, computed profile, invalid ordinal/task rejection and frozen inventory ownership after caller mutation. Both nullable and populated parent/protection variants generate draft profiles accepted by Python admit_layer, while params retain schema/worker/canonical SHA parity. The draft test uses a root-only injected inventory to test compiler contracts; complete scoped closure was verified separately in the lens integration scenarios. Composition-specific relational shell creation, transport publication after shell commit, admission and production dispatch remain unfinished.

### Digest-verified composition transport publication (2026-10-02)

`PublishCompositionInputs` now separates transport publication from blocked draft compilation. It reads the frozen cursor once, copies and bounds its inventory, requires strictly sorted unique valid refs and a declared Params direct handle, then recomputes the exact task/operation/version/entries digest before any object write. Transport uses that owned snapshot and the returned GraphTask owns its retry cursor/direct refs. Generated chunk/manifest IDs cannot collide with any input dependency. Existing PrepareComposition now delegates to this shared path rather than reopening caller state after publication.

Verification: Go v2render/v2draft/v2input passed with compiler/schema/worker/profile parity enabled. Tests verify one snapshot read, independent graph retry cursors, protection against later caller direct-ref changes, and task/digest/direct-handle/null-cursor rejection before publisher calls. Existing preparation/transport collision tests remain green. These publisher tests inject transport IO; actual composition shell persistence and task-scoped publication authorization have not yet been exercised. SQL admission/registration, activation and production dispatch remain unfinished.

### SQL frozen composition wire checks, migration333 (2026-10-02)

Forward-only migration333 adds pure immutable v25_layer_ref_shape and v25_layer_params_shape. They reject unknown/missing fields, malformed or nil canonical UUID refs, invalid SHA values, omitted nullable lineage/protection fields, invalid opacity/blend and inconsistent mask leaf canvas hashes. The iterative mask walk enforces root depth0 with maximum depth16 and at most256 visited occurrences, matching worker compile_mask rather than counting only deduplicated nodes. Invalid numeric conversions return false. Functions grant execution to photo_scheduler, revoke PUBLIC, perform no table IO and do not authorize a graph, task or artifact. Migration333 is tracked under V25.

Verification: 22 real PostgreSQL cases passed (56.37s). Valid parent variants/blends, depth16 and 255-occurrence trees pass; depth17, 511-occurrence repeated trees, missing nullable fields, malformed masks/refs/base and invalid opacity reject. Worker canonical parsing agrees on these cases; nil version UUID rejection is an additional admission-domain invariant, not claimed Python UUID parsing behavior. SQL NULL rejects; photo_api has no function execution grant. Go v2render/v2draft/v2input with cross-language wire/profile checks remains green. These validators still need to be invoked by composition-specific relational admission; shell creation, registration, activation and production execution remain unfinished.

### SQL composition resource/profile agreement, migration334 (2026-10-02)

Forward-only migration334 adds immutable v25_layer_resources and v25_layer_profile_shape. After the independent frozen params check, resource calculation deduplicates semantic JSON mask nodes and exact mask/protection refs, uses the fixed512px tile profile and the same conservative CPU formula as Go/Python. Bounds are checked before multiplication or division; invalid dimensions/params return NULL. Profile validation requires exact CPU/v2.2/resource_estimate agreement, including zero VRAM, with no extra fields. These pure functions grant scheduler execution only and neither access tenant metadata nor authorize task/graph registration. Migration334 is tracked under V25.

Verification: two real PostgreSQL tests passed (4.53s), comparing 15 resource cases against the actual worker formula: leaf/shared/protected graphs, a129-node/65-ref DAG, edge tile dimensions and the200MP limit. Every resource-field alteration and backend/renderer/extra-field substitution rejects; zero/negative/oversized/int64-limit dimensions and inconsistent canvas reject without overflow. Shared protection refs do not inflate the budget. Go v2render/v2draft/v2input with wire/profile parity remains green. Composition relational admission still needs to invoke these checks; shell creation, task-scoped publication, registration and activation remain unfinished.

### Exact snapshot original scope for null composition lineage, migration335 (2026-10-02)

Forward-only migration335 adds scheduler-only v25_layer_original_scope. It requires independently valid LayerComposeParams, an explicit null source_result, active editor/session/project authority and a complete frozen source snapshot through the existing snapshot preflight. The targeted VersionBinding must match the exact snapshot item and accepted asset version's master ID/SHA in the same undeleted verified project asset. A different accepted version in that project is insufficient. Source/original ready/version/expiry checks are inherited from snapshot preflight. This read-only predicate does not prove source canvas from object bytes, authorize proposal/mask refs, create a shell or register a task; atomic admission must repeat it under its locks alongside those remaining checks. Migration335 is tracked under V25.

Verification: scope plus resource-profile PostgreSQL tests passed (3 tests,7.15s). Positive original binding passes; tenant/project/snapshot/actor/session substitutions, parent-present lineage, version/source/SHA substitutions, missing source_result, rejected version, expired source and revoked session reject. An existing accepted version2 outside the selected snapshot also rejects. Its fixture initially reused default version_no1 and was corrected to2 without changing constraints. photo_api has no execution grant. Go v2render/v2draft/v2input and deterministic documentation checks remain green. Composition-specific atomic shell/admission/registration and production execution are still unfinished.

### Connected original composition SQL preflight and Go role isolation, migration336 (2026-10-02)

Migration336 adds scheduler-only v25_layer_original_preflight, combining exact original snapshot/source authority with independent frozen params/resource-profile validation in one read-only predicate. SQLOriginalCompositionPreflight.Check validates bounded canonical params/profile and dimensions before entering a transaction, requires current_user=photo_scheduler and calls this combined predicate. Rejection maps to stale preflight; use of an API/privileged pool rejects before the restricted function call. This does not create a shell, reserve quota, bind params to a published artifact or grant proposal/mask scope. Atomic admission must repeat provenance/profile checks under its locks and bind verified document refs. Migration336 is tracked under V25.

Verification: scope/resource PostgreSQL tests passed (3 tests,7.21s), with the combined predicate repeated for all identity/source/staleness cases and additionally rejecting altered RAM, renderer and dimensions. The actual Go SQL call passed its isolated PostgreSQL role test (1 test,4.38s): scheduler reaches the predicate and rejects a nonexistent authorized selection; API rejects due to pool role. Positive provenance/profile was tested directly in SQL, not yet through this Go wrapper. Go v2render/v2draft/v2input with cross-language wire/profile checks and deterministic documentation checks passed. Frozen params artifact scope, composition shell creation, atomic registration/activation and production execution remain unfinished.

### Frozen composition params artifact binding, migration337 (2026-10-02)

Migration337 adds scheduler-only v25_layer_original_params_scope. Exact params text length and SHA must match a completed ready v2_draft_upload and ready verified JSON artifact for the same tenant/project/source version. Upload/artifact bucket, bytes and pinned object version must agree; expired artifacts or retired uploads reject. The original source/profile preflight is repeated inside this predicate. Invalid JSON returns false. SQL binds the supplied bytes; Go CheckParams additionally requires canonical JSON through Check and exact artifact SHA before entering the params scope transaction, with the scheduler role rechecked. This is not yet a task-specific registration or proposal/mask authorization.

Verification: real params scope plus Go preflight regression tests passed (2 tests,6.74s). Positive ready params binding passes; wrong artifact ID/SHA, trailing bytes, retired upload, different source-version upload and version/bucket/length metadata mismatches reject. The initial SQL variable named bytes conflicted with table columns and was renamed wire_bytes before successful verification. Go v2render/v2draft/v2input with cross-language wire/profile checks and deterministic documentation checks passed. Positive SQL params binding uses synthetic verified metadata, not actual object-store publication; CheckParams' positive combined Go/SQL path still requires integration evidence. Composition blocked shell creation, quota reservation, task-scoped publication, atomic registration/activation and production execution remain unfinished.

### Positive Go/SQL frozen params binding and live revocation (2026-10-02)

A reusable committed synthetic metadata fixture now supplies the actual Go CheckParams integration test with an accepted original snapshot, active editor session, canonical params/profile and completed version-scoped params upload. Isolated connection credentials remain subprocess environment-only. The same fixture can support subsequent composition shell/admission integration without treating metadata seeding as object-store publication.

Verification: real PostgreSQL positive/revoked Go scenario passed (1 test,5.19s). CheckParams reaches both SQL transactions and succeeds for the exact params artifact. Foreign artifact ID rejects through authoritative SQL; altered SHA rejects before scope lookup; noncanonical params with their recomputed SHA and noncanonical profile reject through Go canonical validation. After session revocation, the same params reference returns stale preflight. Go v2render/v2draft/v2input also passed with compiler/schema/worker/profile parity enabled. This closes the positive Go/SQL metadata-binding evidence gap; actual params object publication, composition blocked shell/quota persistence, proposal/mask scope, atomic registration/activation and production execution remain unfinished.

### Real composition params publication and replay (2026-10-02)

PublishCompositionParams compiles canonical layer parameters and publishes them through the existing immutable v2draft.Publisher upload lifecycle. The document UUID is derived from the composition task UUID and layer.compose.params:1, so retries retain the same identity. The returned artifact must have a nonzero ID and exact compiled SHA. The returned preparation owns its nullable references and mask bytes. This helper does not authorize parent lineage or admit a task.

Verification: isolated PostgreSQL and versioned MinIO integration passed (1 test,6.18s). The real photo_api publisher stores params, completes the upload, and returns the same artifact on retry. The photo_scheduler CheckParams accepts its source/version/snapshot/profile binding. The API catalog authorizes the Params reference and the production object store reads the pinned version back to exact canonical bytes. Upload and artifact object versions agree; storage quota increases exactly once, with no remaining reservation. Unit tests cover publication identity/bytes, owned references, invalid receipts, propagated publisher failures and cancellation before IO. Go v2render/v2draft/v2input passed with cross-language schema/profile parity. Parent image metadata in this test is synthetic; this closes actual params publication evidence only. Composition shell admission, authoritative proposal/mask lineage, atomic registration/activation and production execution remain unfinished.

### Frozen multi-task composition document plan (2026-10-02)

CompileCompositionDraftConfig now produces a bounded canonical v25_composition plan for a run: project/snapshot/session identity, ordered node plans with task UUID, ordinal, published params reference and exact params JSON, complete input digest, dimensions and independently computed execution profile. Task UUIDs must match the run-derived workflow and layer.compose ordinal. The selection must have contiguous unique ordinals and one tenant/actor/session. All tasks are compiled through CompileCompositionDraft, including params SHA and frozen inventory validation. Output quota is summed from computed estimates with the canonical safe-integer bound. The result contains task shells and privately owned retryable frozen inventories; it performs no publication, SQL authorization, shell creation or dispatch.

Verification: Go v2render/v2input/v2draft passed with cross-language checks enabled. Tests establish deterministic plan/hash under selection reorder, output estimate sum, params/profile/input digest bindings, ownership after caller mutation, and rejection of changed tenant/actor/session/task identities, duplicate/gapped ordinals, bad params SHA, missing inventory, cancelled context, nil selection identities, empty selection and more than 5000 nodes. The serialized plan is limited to 1 MiB. SQL persistence and independent relational validation of this new composition config remain to be implemented; no registry or worker activation is claimed.

### Independent SQL composition plan validation, migration338 (2026-10-02)

CompositionDocumentPlan now retains canonical params bytes as the params_jcs JSON string. PostgreSQL can compare the exact published SHA and later reuse the params artifact scope without reserializing jsonb. Migration338 adds scheduler-only v25_layer_plan_shape, a pure immutable validator with no data access or authority grant. It requires exact config/node keys, nonzero lowercase UUID selection identifiers, a bounded ordered nonempty node array, contiguous integer ordinals, workflow-derived layer.compose task UUIDs, typed artifact refs/input digests, exact params text SHA, bounded integer dimensions and the independent resource profile guard. Summed computed output bytes must equal the proposed reservation. Canonical serialization remains the Go compiler's responsibility; SQL independently checks the supplied bytes and parsed shape, without asserting that a SHA alone proves parent authority.

Verification: isolated SQL shape and actual Go publication/plan parity scenarios passed (2 tests,8.85s). The real Go compiler's plan for an actual published params artifact is accepted by the scheduler SQL guard. The synthetic SQL tests reject changed task identity/ordinal, missing/extra node keys, malformed dimensions/types/digest/params text, modified resource profile, duplicate ordinal, altered output sum, malformed config and wrong workflow. Go v2render/v2input/v2draft passed with cross-language checks. Migration338 is tracked under V25 and was applied only in the isolated integration database. No composition run/task persistence, proposal/mask authority, atomic activation or production execution has been completed by this pure guard.

### Task-bound params and original composition plan scope, migrations339–340 (2026-10-02)

Migration339 adds scheduler-only v25_layer_original_task_params_scope. It repeats the existing original params scope and additionally requires the completed upload's document_id to equal UUIDv5(task, layer.compose.params:1), matching PublishCompositionParams. CheckTaskParams validates cancellation/nil task before IO and repeats SQL authority in its final scheduler transaction; a prior metadata success is not cached authorization.

Migration340 adds scheduler-only v25_layer_original_plan_scope. After pure plan validation it requires matching project/snapshot/session, repeats task-bound params authority for every node and requires every selected snapshot version to occur in the plan. Multiple layer nodes per version are permitted; per-node source scope rejects versions outside the snapshot. This is limited to original-source plans with explicit null source_result; adopted source lineage and proposal/mask authority remain separate unfinished work. CheckDraft verifies bounded canonical config/body/hash and each exact params_jcs string before deriving the workflow from the run and entering the scheduler transaction. It does not persist tasks or reserve quota.

Verification: actual PostgreSQL/MinIO Go publication scenario passed (1 test,5.40s). Published params are accepted for their exact task; a different task rejects. The compiled one-version plan passes live SQL scope; a foreign run UUID or changed output reservation rejects. Go regression tests passed and show changed body/hash/config, out-of-range output, noncanonical nested params, incomplete actor identity and cancellation reject before any database query. Full multi-version coverage rejection and concurrent atomic admission still require separate integration evidence. Migrations339–340 are tracked under V25 and applied only in isolated test databases. Composition persistence, proposal/mask lineage, graph activation and production execution remain unfinished.

### Persisted original composition run and output reservation, migration341 (2026-10-02)

Migration341 adds scheduler-only begin_v25_original_composition_draft. Tenant and usage rows are locked for update, snapshot for share, then live snapshot and complete original composition plan scope are repeated. The workflow UUID must derive from the run. Exact config text SHA, JSON config, actor/snapshot/policy and a live quota reservation must match for a replay. A new draft run, preview workflow, one-hour output reservation and usage increase are persisted in the same transaction; no task or graph activation is created. Storage quota and the canonical safe integer bound are enforced before insertion. SQLOriginalCompositionDraftStore.Begin checks the bounded canonical compiled plan through CheckDraft, validates policy SHA, requires the scheduler role and invokes this locked entry point.

Verification: actual Go/PostgreSQL/MinIO scenario passed including two concurrent first calls followed by sequential replays (1 test,4.75s). Admin inspection confirms one reserved output amount of 33,555,032 bytes for the synthetic 5x5 composition, draft/unsealed run and zero tasks. Params storage is charged once in addition to that reservation. A changed policy SHA rejects. Go v2render/v2input/v2draft with cross-language parity passed. Migration341 is tracked under V25 and applied only in the isolated integration database. This proves original-plan run/quota persistence, not proposal/mask authority, task staging, input registration, graph activation, production execution or photographic quality.

### Blocked original composition task staging, migration342 (2026-10-02)

Migration342 stages a blocked layer.compose task against the saved original composition plan. The scheduler-only entry point checks active tenant, locks the draft/unsealed/uncancelled run, requires a live reserved output quota, locks the snapshot and repeats full original plan scope. Task UUID, ordinal, params ID/SHA, input digest and profile must exactly match its saved node. Concurrent/sequential replays require the same blocked task, attempt zero and matching metadata. Transport-enriched profiles may replay only after the existing input-closure readiness predicate succeeds. SQLOriginalCompositionTaskStage additionally requires a run-derived workflow, task-derived node key, cpu operation/version, valid refs/digests and canonical bounded profile before enforcing the scheduler role in a transaction. No operation registry activation was added.

Verification: actual PostgreSQL/MinIO scenario passed (1 test,5.82s), including two concurrent first staging calls and a later replay. Inspection confirms exactly one blocked layer.compose task, attempt zero, empty inputs and unchanged single output reservation. A changed input digest rejects through SQL. Go unit tests cover malformed shell identity/operation/profile/digest and cancellation before IO. This closes blocked task persistence only; relational proposal/mask authority, complete input transport registration, graph activation and execution remain unfinished. Migration342 is tracked under V25 and applied only in isolated test databases.

### Real composition input transport publication (2026-10-02)

CompositionTransportPublisher derives the layer.compose task from run/workflow/ordinal and connects frozen composition inputs to v2input.ScopedPublisher and the existing artifactjson SQLRepository/Publisher. Chunk and manifest reservations commit independently before object IO; replay retains each artifact/version. This phase operates on an already staged blocked task and performs no relational input registration or graph activation.

Verification: actual PostgreSQL/MinIO composition scenario passed (1 test,6.54s). Two transport publications return the same chunk/manifest references. Admin inspection confirms exactly one ready chunk and manifest with matching upload/artifact object versions. Both pinned versions are read from MinIO with exact length/SHA, and manifest entry count and entries digest agree with canonical chunk entries. Transport storage is charged once, while the single output reservation remains unchanged. Go v2render/v2input/v2draft with cross-language checks passed. The fixture contains synthetic parent metadata and only root inventory, so this is transport evidence, not complete parent closure or scope evidence. Current generic transport scope does not independently repeat the composition session/plan guard; that must be added before production admission/activation. Proposal/mask provenance, complete closure registration and execution remain unfinished.

### Live composition authority for transport reservation/completion, migration343 (2026-10-02)

Migration343 strengthens the existing v2_input_document_scope used by both reserve_v2_input_document and complete_v2_input_document. A composition task or composition-config run now requires the exact blocked/unattempted draft state, live output reservation, complete original plan/session/upload scope and task metadata matching its saved node. Only matching renderer/resource profile, or transport-enriched profile with proven existing closure, can pass. The original predicate remains for other V2 operations; mixed operation/composition config cannot bypass the stronger branch. No registry activation was added.

Verification: real Go/PostgreSQL/MinIO scenario passed (1 test,6.80s). Existing positive publication/replays still pass. After live session revocation, replay of the exact already-ready manifest returns stale through the production Go publisher. A chunk reserved before revocation cannot complete after revocation; no artifact is inserted and its two-byte pending reservation remains, without storage charge. The completion rejection uses synthetic unpublished object metadata, proving the SQL authorization guard rather than S3 upload behavior. Existing V2 input-document and input-registration regression scenarios passed (2 tests,3.70s). Go v2render/v2input/artifactjson/v2draft passed with cross-language checks. Migration343 is tracked under V25 and applied only in isolated databases. Proposal/mask provenance, complete composition closure registration, activation and execution remain unfinished.

### Exact adopted lens proposal provenance, migration344 (2026-10-02)

Migration344 adds scheduler-only v25_layer_lens_proposal_scope. The proposal must be the exact canonical root of a succeeded optical.lens task, matching its current attempt/fence, verified pinned root artifact and immutable producer metadata. Workflow/project, accepted selected asset version and exact original source/canvas binding must agree. A registered WorkingImage input, adopted artifact link and adoption inbox/event are required. Live editor authorization is repeated. The predicate does not depend on caller app.tenant_id; its scheduler-only ACL and explicit authorization protect tenant scope. CheckLensProposal validates typed references, identity and canvas hash before enforcing the scheduler role and invoking this proof. It covers lens proposals only; other producer operations require their own proven typed lineage before activation.

Verification: actual typed lens adoption and Go scope scenarios passed (3 tests,18.30s), using synthetic HDR 5x5 frames and real PostgreSQL/MinIO. Scheduler SQL succeeds with an empty tenant GUC; changed tenant/project/version/actor/session/proposal ID/SHA/source ID/SHA/canvas rejects. The actual Go wrapper accepts the adopted root, rejects a foreign root, returns stale after revocation and rejects an API pool before the scheduler predicate. Existing pinned-root/catalog/closure and geometry preflight checks still pass. Go v2render/v2input/v2draft passed with cross-language checks. Migration344 is tracked under V25 and applied only in isolated databases. This proves proposal metadata provenance, not photometric geometry, mask ownership, complete composition input admission or production qualification. The predicate is not yet wired into atomic composition registration/activation.

### Adopted lens mask occurrence scope, migration345 (2026-10-02)

Migration345 adds scheduler-only v25_layer_lens_mask_scope. It repeats the exact proposal/source/version/canvas and live editor proof, then requires the mask document to be a ready verified pinned JSON mask produced by the same task/current attempt/fence. Its adopted artifact link must identify the exact changed_mask or generated_mask occurrence through the established task/path purpose hash. Workflow-wide membership, an arbitrary root, binary artifact or another producer is insufficient. CheckLensMask validates the exact mask reference, repeats CheckLensProposal and rechecks SQL authority in its last scheduler transaction. Pinned JSON geometry and binary mask values remain mandatory separate verification; this predicate does not grant scope to user masks or other operations.

Verification: actual Go/PostgreSQL/MinIO typed lens adoption scenarios passed (3 tests,20.75s). The real wrapper accepts the adopted changed mask, rejects an ImageResult root used as a mask and returns stale after session revocation. Existing root/child/pinned-version/geometry checks and Go v2render/v2input/v2draft cross-language regression checks passed. Migration345 is tracked under V25 and applied only in isolated test databases. Generated-mask positive cases, other mask producers, user masks and integration into atomic composition registration remain unfinished; no activation or quality qualification is claimed.

### Version-scoped user mask roots, migration346 (2026-10-02)

Migration346 adds scheduler-only v25_layer_original_user_mask_scope. The original snapshot/source/session proof first validates the bounded composition mask expression. A recursive traversal then requires the exact mask ref to occur in the expression or protected_mask. Only a completed ready upload for the same tenant/project/asset version and exact SHA can pass, with verified ready JSON document artifact, matching bucket/bytes/pinned version and no expiration. This is metadata authority only; a generic JSON upload must still parse as MaskRef and its declared binary closure must pass geometry/probability verification. CheckUserMask checks bounded canonical params, identities, typed ref and cancellation before requiring the scheduler role in its SQL transaction.

Verification: isolated PostgreSQL synthetic metadata scenario passed (1 test,2.66s). Exact direct/nested/shared mask roots and protected-only usage pass. Foreign ID/SHA, an uploaded mask absent from the params, upload version/bucket/length/state mismatches and revoked session reject. API has no execute privilege. Go v2render/v2input/v2draft passed with cross-language checks and pre-IO rejection tests for invalid/noncanonical/oversized params, invalid ref and cancellation. Positive Go/SQL wrapper integration and actual user MaskRef upload bytes still need evidence; no typed user mask endpoint or full composition registration is claimed. Migration346 is tracked under V25 and applied only in isolated databases. Adopted-source user mask scope and complete binary lineage/admission remain unfinished.

### Typed user MaskRef document publication helper (2026-10-02)

ValidateUserCompositionMask checks bounded canonical MaskRef and source WorkingImage documents, source SHA against the version binding, exact canvas hash, float32 source, all eight mandatory mask fields, non-null boolean protected and numeric confidence, and equality of canvas/full transform chain. Source bytes must already come from a scoped pinned read. PublishUserCompositionMask invokes the existing immutable upload publisher only after this validation, with a stable caller-supplied document ID and owned publication bytes; the returned artifact ref must have the exact document SHA. It does not validate binary array probabilities or independently authorize binary dependencies.

Verification: Go v2render/v2input/v2draft passed. Tests use Python-generated valid WorkingImage fixtures and establish exact publication identity/bytes, typed mask acceptance, rejection before IO of missing fields, null protected/confidence, malformed/noncanonical mask, wrong source SHA/canvas and cancellation, and rejection of an invalid publisher receipt. The publisher in these tests is injected; real user-mask API/S3 publication and CheckUserMask positive Go/SQL integration remain to be verified. This helper is not a complete mask upload endpoint, task admission or operation activation.

### Real typed user mask publication and Go/SQL scope (2026-10-02)

The live typed lens fixture now exercises PublishUserCompositionMask through the actual API v2draft.Publisher, PostgreSQL upload lifecycle and versioned MinIO. It reads a scoped pinned original WorkingImage and an adopted changed-mask document. For the zero-strength same-geometry result, this typed MaskRef is republished as a version-scoped user document; composing params reference the exact new artifact. For nonzero lens geometry, validation rejects the mask before publication. This scenario reuses already-adopted binary mask data; it does not implement new user binary uploads.

Verification: actual PostgreSQL/MinIO scenarios passed (3 tests,19.27s). The real publisher returns the same user-mask artifact on retry. CheckUserMask succeeds through the actual Go scheduler/SQL path; an unrelated ImageResult ref rejects. The API catalog authorizes the new user MaskRef, and the production store reads its pinned version back to exact bytes. Other adopted root/child/geometry/revocation checks still pass. Go v2render/v2input/v2draft with cross-language checks passed. This closes real typed document publication and positive Go/SQL scope evidence, while a user-mask endpoint, new binary upload authority/probability validation, complete composition registration and execution remain unfinished.

Additional quota inspection passed (3 tests,18.92s): zero-strength publication leaves exactly one ready user upload and charges only its document length once, with no remaining upload reservation. Nonzero-geometry cases create no user uploads.

### Bounded streaming user mask NPY verification (2026-10-02)

VerifyUserMaskNPY validates a pinned reader without allocating an entire mask. It accepts NPY v1/v2/v3 minor zero with bounded header (64 KiB), exactly the scalar <f4 descriptor, C-order and exact height/width shape. A restricted dictionary parser handles key order and quote variations without Python evaluation; extra/duplicate keys, object/structured dtype and expressions reject. Dimension limits are checked before multiplication (200 MP). File length must equal header plus four bytes per pixel; a 64 KiB data buffer checks every probability is finite and in [0,1]. EOF and exact SHA are required. Truncation/data violations are invalid; transport failures and cancellation propagate. The caller owns and closes the reader on success or early failure.

Verification: Go v2render/v2input/v2draft with cross-language checks passed. Unit cases cover valid NPY versions, reordered header keys, multi-buffer payload, wrong dtype/order/shape/duplicate keys/unknown keys/object/expression, NaN/Inf/out-of-range probabilities, truncation/trailing bytes, SHA mismatch, cancellation and propagated object-read errors. The live typed adoption test now invokes this verifier on the exact scoped/pinned mask array read from MinIO before user-document publication. Its first run failed during Docker container startup/cleanup timeouts (three setup errors,127.81s), before application/NPY checks. Docker later responded with no remaining temporary containers; the integration run was retried. New binary upload scope, endpoint, persistence and complete composition admission still remain unfinished.

The MinIO retry also failed before application checks (three Docker startup/cleanup setup errors,140.60s). A subsequent Docker inventory again confirmed no remaining temporary containers. Real object-store evidence for this new NPY verifier remains unproven. NumPy itself generated v1/v2/v3 payload fixtures locally; these were added as deterministic Go compatibility tests, independent of Docker.

Targeted Go NPY tests including the actual NumPy writer v1/v2/v3 fixtures passed (go test ./internal/v2render -run ^TestUserMaskNPY -count=1). This establishes NumPy-format compatibility without claiming the unavailable live MinIO scenario.

### Scoped composition mask document and binary preflight (2026-10-02)

VerifyCompositionMask now combines scoped pinned source/MaskRef JSON reads with the typed geometry validator and streaming probability-array verification. The catalog must authorize every document and the exact MaskPixels ref with its MaskRef parent. Document byte counts, SHA, canonical form and object versions are checked; source canvas/full chain must match. Binary media/size/SHA/version are checked before opening, then the entire NPY is verified and closed even on invalid values. Parent JSON reads were factored into the shared private readCompositionJSON helper, retaining scoped canonical/SHA/version validation. Nil readers reject. This is byte preflight, not a cached access token or replacement for atomic SQL admission.

Verification: Go v2render/v2input/v2draft with cross-language checks passed. A complete injected 400x300 scoped fixture verifies source/MaskRef/probability bytes, exact binary parent contract, and all three reader closures. Denial at the source performs no object IO; denial at the binary closes both prior JSON readers and never opens the prohibited array. NaN probability rejects and closes the binary reader; cancellation before lookup performs no IO. The actual MinIO fixture is wired to invoke the combined verifier before user-document publication, but its new live result is not claimed after the previously documented Docker failures. New binary upload persistence/endpoint, full composition registration/activation and production execution remain unfinished.

### Shared version-scoped binary mask upload lifecycle, migration347 (2026-10-02)

Migration347 extends v2_draft_uploads with upload_contract (Document by default, or MaskPixels), nullable mask_width/mask_height and payload constraints. JSON records retain the existing 1 MiB limit and null dimensions. MaskPixels require positive bounded dimensions, at most 200 MP and byte length compatible with float32 NPY plus bounded header. Existing JSON reserve/complete functions reject binary records, preserving distinct upload types. New API-only reserve_v25_user_mask_pixels/complete_v25_user_mask_pixels reuse exact tenant/editor/version authorization, latest accepted asset version, storage quota, stable document identity, fencing token and the existing key/cleanup lifecycle. Binary completion stores a verified application/x-npy mask_pixels artifact and charges storage once. Completion repeats accepted/latest undeleted asset authority; ready replay checks exact artifact media/role/bucket/length/version. No new tenant table or purge allowlist extension is needed.

Verification: isolated PostgreSQL binary lifecycle and user-mask scope regressions passed (3 tests,7.05s). Reserve/resume rotates the token without changing artifact ID; stale token, changed dimensions and cross-type JSON reserve/complete reject. Completion/replay produce one 228-byte storage charge with no remaining reservation. Ready metadata has exact NPY media/role/size/version. Revoked session rejects reserve and completion. Zero/negative/null/overflow and >200 MP dimensions reject before authority or quota writes. The object version is synthetic, so this does not prove a valid NPY PUT; backend streaming validation, publisher wiring, actual S3 binary upload, complete mask root linkage and production admission remain unfinished. Migration347 is tracked under V25 and applied only in isolated integration databases.

### Go binary mask publisher and isolated PostgreSQL integration (2026-10-02)

UserMaskPixelsPublisher now verifies the full seekable NPY before quota reservation, rewinds the caller-owned reader, reserves through migration347, streams application/x-npy to the existing immutable object-store interface and completes with the reservation fencing token. Exact key, SHA, length, media and non-null version are required in the immutable receipt. Uncertain PUT or an existing object invokes exact immutable recovery. Ready replay returns the existing artifact after payload validation without another PUT. Nullable reservation UUIDs are scanned safely for rejection outcomes. The store must independently verify length/SHA during PUT to detect source changes between validation and upload.

Verification: actual Go/API-role/PostgreSQL integration passed (1 test,4.42s), with an injected immutable transport rather than S3. Three completed binary uploads charge their total bytes once, with zero remaining reservation. Ready replay performs no additional PUT; an uncertain PUT recovers the same receipt. Invalid probabilities never reach the object store. A receipt with version null rejects without completion; retry resumes the reservation with a new fence and completes successfully. Documentation checks passed (2067 static checks,54 semantic tests; deterministic regeneration). Real S3 binary publication, HTTP endpoint wiring, source-bound mask root linkage, complete atomic composition registration and operation activation remain unfinished.

### Real binary mask PUT and pinned probability reads (2026-10-02)

The Go publisher now has a real PostgreSQL/API-role/versioned-MinIO integration case, complementing the injected transport failure cases. Production objectstore.Store performs immutable NPY PUT. Ready replay preserves the artifact identity. RecoverImmutable independently validates exact key/SHA/length/media and provides the pinned object version; Open plus VerifyUserMaskNPY validates every probability and closes the reader. Python separately fetches the catalog-pinned version, verifies SHA/size, loads the actual bytes using NumPy and checks all six exact float32 values. Database inspection establishes one ready binary upload, one storage charge and zero remaining reservation.

Verification: injected and real binary publisher cases passed together (2 tests,9.18s). Docker availability recovered without restarting shared services. The previously wired typed lens adoption integration then passed (3 tests,19.62s), including scoped source/MaskRef reads, exact-parent binary lookup, pinned NPY verification before user MaskRef publication, wrong geometry rejection and existing relational adoption/revocation checks. This closes the formerly unverified MinIO evidence for the streaming NPY verifier and composition mask preflight. Go v2render/v2draft/objectstore with cross-language checks passed. It does not establish new user binary-to-MaskRef relational linkage, HTTP endpoint wiring, complete atomic task registration, worker admission or operation activation; those remain required.

### New user binary scope and verified MaskRef publication (2026-10-02)

Migration348 adds API-only SECURITY DEFINER read_v25_user_mask_pixels with tenant GUC, live editor/session authority, latest accepted undeleted verified asset version, exact project/version/upload contract and dimensions. Only completed ready MaskPixels uploads with matching artifact SHA/bytes/bucket/version, canonical V2DraftKey, verified NPY media/role and unexpired metadata return a pinned object. No project-wide task producer fallback is used. SQLUserMaskPixelsCatalog invokes that function in tenant.WithTenant and enforces the configured bucket. This is pre-publication authority for newly uploaded user arrays; adopted-mask lookup remains a separate exact-parent path.

PublishVerifiedUserCompositionMask now owns request bytes, reads the scoped pinned WorkingImage, validates typed mask/source geometry and SHA, proves exact binary version/dimensions through UserMaskPixelsCatalog, opens the pinned NPY, verifies every probability, closes both readers and only then publishes the typed document. It does not turn preflight into a lasting task permission; atomic admission must repeat live scope.

Verification: actual Go/API-role/PostgreSQL/MinIO binary cases passed (2 tests,8.46s), including positive scoped binary lookup and rejection of swapped/changed/zero dimensions and a foreign asset version. Migration348 applied only to isolated integration databases and is tracked under V25. Go v2render/v2draft/objectstore with cross-language checks passed. Injected full-source/new-array publication tests prove successful publication closes both readers; binary scope denial performs no binary IO or document publication; NaN probability closes the binary and rejects before document publication. Documentation checks passed (2067 static,54 semantic; deterministic regeneration). Complete real S3 new-array-to-MaskRef publication, durable relational root linkage, revocation-specific new scope tests, HTTP wiring, atomic task registration and worker admission remain unfinished.

### User mask read revocation and role boundaries (2026-10-02)

The real binary publisher integration now probes migration348 directly under photo_api before and after committed session revocation. An active session and exact tenant GUC return the ready object; an absent tenant GUC returns no rows. After revocation the same pinned ready upload returns no rows. A separate actual Go/API-role SQLUserMaskPixelsCatalog invocation also rejects the revoked session. photo_scheduler cannot execute this API-only function (InsufficientPrivilege). The stored object is retained; revocation changes read authority rather than deleting bytes.

Verification: injected and live PostgreSQL/MinIO mask cases passed (2 tests,9.56s), including the separate revoked Go invocation and direct SQL authority/role assertions. This supplies the previously missing revocation evidence for the new binary scope. It does not prove durable MaskRef-to-binary linkage, HTTP integration or composition execution; those remain outstanding.

### Exact original WorkingImage version in the API catalog (2026-10-02)

Inspection before durable user-mask linkage found that the legacy v2_draft_artifact_scope WorkingImage branch establishes accepted project provenance but does not constrain v.id to its p_version argument. SQLArtifactCatalog now adds an explicit WorkingImage predicate requiring the requested asset version, exact master artifact and content hash, accepted review state, matching project and undeleted verified asset. This protects scoped source reads used by PublishVerifiedUserCompositionMask; project membership alone cannot substitute another version's original. Other contract branches are unchanged. The legacy SQL function itself remains a broader project predicate, so callers outside SQLArtifactCatalog must continue using their own exact version binding proof; this change is not claimed as a global SQL rewrite.

Verification: actual API-role Go catalog integration accepts the fixture's exact source version and rejects the same source with a foreign version UUID (binary mask integration 2 tests,9.24s). Existing live typed lens adoption/source/mask/revocation scenarios passed (3 tests,16.52s). Go v2draft/v2render with cross-language checks passed. Documentation checks passed (2067 static,54 semantic; deterministic regeneration). Durable new user MaskRef-to-binary binding and full API/task integration remain outstanding.

### Full real new-array to typed MaskRef publication (2026-10-02)

The original-version metadata fixture now optionally accepts a real immutable source object and canvas hash; its default synthetic behavior remains unchanged. A new integration publishes a canonical Python WorkingImage fixture into versioned MinIO with the mandatory SHA metadata and records its exact object version in PostgreSQL. Its pixel dependency remains synthetic: the test qualifies source document geometry and mask handling, not photographic image processing.

The actual Go path publishes a new 400x300 float32 probability NPY through UserMaskPixelsPublisher, then invokes PublishVerifiedUserCompositionMask with the real API SQL source catalog, migration348 binary catalog, production object store and v2draft.Publisher. It reads/verifies the pinned source and entire new probability array before publishing MaskRef. Retry returns the same typed document; an independent Python pinned fetch verifies its binary ref against the uploaded artifact and exact source canvas/full transform chain. Database inspection shows exactly two new uploads (MaskPixels and Document), storage charged once for their sum and zero remaining reservation. The initial fixture omitted required source SHA metadata and was correctly rejected by object integrity validation; the fixture was corrected without weakening the production check.

Verification: all three injected/live binary/full-new-mask cases passed (3 tests,21.99s). Default original-scope and user-mask-scope fixture regressions passed (2 tests,6.32s). Go v2render/v2draft with cross-language checks passed. The immutable MaskRef bytes now provide verified binary-reference evidence in a real publication scenario, while durable relational dependency binding, API endpoint/startup wiring, atomic task registration and worker admission still remain unfinished. No operation was activated and no photographic-quality qualification is claimed.

### Durable new-user MaskRef binding, migration349 (2026-10-02)

Migration349 adds nullable user_mask_binding to the existing v2_draft_uploads row, restricted to bounded Document metadata. A trigger prevents replacing or clearing a non-null binding. API-only bind_v25_user_mask locks the ready root upload, hashes the exact supplied document text and matches artifact SHA/length/bucket/version/media/role, repeats live editor/session authorization and exact accepted source version/master SHA, and invokes migration348 for the exact completed binary upload and dimensions. The binding records source ref, canvas hash and the full typed mask JSON, whose artifact ref identifies the binary. Replays require identical binding and still repeat live authority. No new tenant table or deletion allowlist is introduced.

PublishVerifiedUserCompositionMask now requires UserMaskPublicationCatalog and returns success only after BindUserMask succeeds following pinned byte verification and document publication. Failure after publication leaves an immutable ready document without a binding; a retry revalidates and can bind it. SQL does not itself read object bytes or establish photographic quality: full typed geometry/canonical checks and NPY validation remain mandatory in the Go path. The binding is not yet consumed by task admission or general GC dependency traversal.

Verification: actual injected/live binary and full user mask integration passed after migration349 (3 tests,39.88s). Added immutable-binding/raw substitution/canvas conflict/revoked replay checks then passed in the real new-mask scenario (1 test,10.10s): persisted source/canvas/mask match, clearing the binding raises CheckViolation, unchanged bind replays true, changed canvas/raw rejects, and revoked session rejects despite an existing binding. Go v2render/v2draft with cross-language checks passed; unit tests require one binding for each successful publication and no binding after binary denial/invalid probabilities. Documentation checks passed (2067 static,54 semantic; deterministic regeneration). Migration349 is tracked under V25 and applied only in isolated test databases. API endpoint/startup wiring, admission consumption of bindings, cleanup reachability, full atomic composition registration and worker execution remain outstanding.

### Bound mask dependency GC reachability, migration350 (2026-10-02)

Migration350 extends v2_artifact_has_references without removing prior reference sources. A ready or retired upload with user_mask_binding protects its source and binary artifact IDs only while its root artifact is ready and its root has an existing direct reference (including a ready upload, documents, document refs, artifact links or task inputs). Root reachability uses the previous direct predicate rather than recursive function calls, preventing cycles or permanent retention caused merely by a stale binding row. Tenant filters apply to root/dependency reference checks. No new tenant table or purge allowlist change is introduced.

Verification: the real new-mask PostgreSQL/MinIO scenario passed (1 test,12.89s). After binary upload retirement, the ready bound root still protects the array; after root upload retirement with no other reference, the array is no longer referenced by this binding. Existing publication, binding immutability, replay/conflict and revocation checks also passed in this run. Documentation checks passed (2067 static,54 semantic; deterministic regeneration). The migration is tracked under V25 and applied only in isolated test databases. Persistent document/task root reference cases and a real GC claim/delete cycle have not yet been verified; full task admission, API wiring and execution remain outstanding.

### Persistent document mask reachability and production GC claim (2026-10-02)

The real new-mask integration now creates a synthetic persisted LayerStack document row and exact v2_document_refs edge to the published MaskRef. This metadata fixture tests reachability, not the public recipe-save implementation. With both binary and mask uploads retired, the persisted edge keeps the binary referenced; a foreign tenant does not see that reachability. Removing the edge makes the binary unreferenced.

The test then expires the actual binary artifact and invokes the production claim_artifact_gc function under photo_maintenance: no ticket is issued while the document edge exists; after removing the edge, it issues a ticket for exactly this binary and a non-null fencing token. The successful claim runs in a force_rollback transaction, preserving bytes/catalog for the remaining binding/revocation checks. Actual object deletion and GC completion are not claimed.

Verification: persisted-root reachability passed (1 test,32.52s), then the extended production claim scenario passed (1 test,16.87s). Existing real publication, pinned reads, quota, immutable binding and revoked replay checks also passed. Documentation checks passed (2067 static,54 semantic; deterministic regeneration). Task-root references, complete production delete/finish cycle, admission consumption of mask bindings and API wiring remain outstanding.

### Physical pinned binary mask GC through production Go cleaner (2026-10-02)

The live new-user-mask scenario now retires the unreferenced root/binary uploads and expires only the binary artifact after all binding/revocation assertions. It creates a newer test version at the same object key, then invokes actual Go maintenance.ArtifactCleaner with SQLArtifactGC under photo_maintenance and production objectstore.Store against isolated MinIO. CleanOne claims, RemoveConfirmed deletes the catalog-pinned version and Finish marks the artifact deleted. A second CleanOne performs no mutation. Python independently verifies the old pinned version is absent, the newer version remains and the MaskRef document object remains. All objects are isolated test data; no repository application data is deleted.

Verification: full publication/binding/document reachability/claim/physical deletion scenario passed (1 test,16.98s). Go v2render/maintenance with cross-language checks passed. This establishes the real binary mask GC delete/finish cycle and version-specific deletion. Storage quota reclamation is not asserted in this scenario. Task-root references, admission consumption of bindings, public API wiring and complete composition execution remain outstanding.

### Scheduler consumption of new-user mask bindings, migration351 (2026-10-02)

Migration351 adds scheduler-only v25_layer_original_user_mask_pixels_scope. It repeats original snapshot/actor/session/version authority and exact mask occurrence through migration346, then matches the immutable root binding's source and canvas hash to composition params, its binary ref and dimensions to caller input, and the same-version completed binary upload to ready verified pinned NPY metadata. Exact key, SHA, length, bucket/version, role/media and expiration are checked. SQLOriginalCompositionPreflight.CheckUserMaskPixels checks canonical bounded params, identities/refs/dimension limits and current_user photo_scheduler before invoking the predicate. This is a live preflight; complete atomic graph registration still needs to invoke it inside its locks.

Verification: actual API publication followed by actual Go/scheduler/PostgreSQL binding consumption passed in the full new-mask scenario (1 test,9.63s). Exact bound input accepts; swapped dimensions and a foreign binary ID reject. Direct scheduler checks accept before committed session revocation and reject afterward. Existing pinned S3 reads, quota, immutable binding, persistent document GC reachability, production GC claim/delete/finish and version-specific retention checks also pass in this scenario. Initial setup caught and corrected a reserved SQL alias; a subsequent fixture incorrectly omitted required MaskExpr canvas_hash and was corrected without weakening validation. Go v2render/v2draft with cross-language checks passed. Migration351 is tracked under V25 and applied only in isolated integration databases. This is not complete task admission/activation: atomic registration, worker use, public API wiring and other operation families remain outstanding.

### Complete new-user mask set preflight, migration352 (2026-10-02)

Migration352 adds scheduler-only v25_layer_original_user_mask_set_scope. Original params/snapshot authority is validated before recursive traversal; existing shape bounds constrain depth/occurrences. Distinct ref leaves from the full MaskExpr and protected_mask form the required set. Each must have a durable upload binding and pass migration351 with exact common source-canvas dimensions; missing/unbound/foreign masks reject the entire set. Shared refs are deduplicated. SQLOriginalCompositionPreflight.CheckUserMaskSet validates canonical bounded params and scheduler role before invoking the proof. This method is for entirely new-user mask sets; adopted producer masks require distinct lineage routing and are not silently accepted.

Verification: the actual new-mask Go/scheduler/PostgreSQL/MinIO scenario passed with complete-set positive and unverified protected-mask rejection (1 test,28.57s). A subsequent run covering the newly added nested invert/union with repeated refs and the same protected mask also passed (1 test,9.70s). Existing binary/document publication, binding, revocation and real version-specific GC assertions pass in the same fixture. Go v2render with cross-language checks passed; documentation checks passed (2067 static,54 semantic; deterministic regeneration). Migration352 is tracked under V25 and applied only in isolated integration databases. Atomic task registration must consume this proof under its locks; this turn does not complete runnable/sealed graph registration, worker admission or public API wiring.

### Atomic user-masked composition shell staging, migration353 (2026-10-02)

Migration353 adds scheduler-only stage_v25_user_masked_composition_task. It locks the active tenant and draft run, locates the exact planned node and checks the complete mask set from that node's frozen params before invoking the existing exact-plan shell staging function in the same transaction. SQLOriginalCompositionTaskStage.StageUserMasked validates derived workflow/task IDs, ordinal/node/op/version/profile/digest and scheduler role before calling it. Success persists a blocked attempt-zero task; it never seals a graph, queues work or substitutes for proposal/closure/worker qualification.

The real new-mask fixture now publishes stable task params, compiles the original composition config, reserves the exact output budget and invokes StageUserMasked twice through Go. The additional params publication and output reservation are independently counted. Its synthetic proposal and incomplete input inventory are explicitly not qualified as executable inputs. Initial test output reservation exceeded the old 10 MB fixture quota; the isolated tenant's JSON storage quota was raised to 100 MB. An erroneous test quota column was corrected to the repository's actual JSON quotas field.

Verification: real Go/API+scheduler/PostgreSQL/MinIO publication-to-staging scenario passed (1 test,6.56s). Additional actual scheduler replay/cancel/revocation assertions passed (1 test,6.53s): exactly one blocked attempt-zero task exists, unchanged replay accepts, a task cancel flag rejects, and committed session revocation rejects staging replay. Existing mask binding, set proofs, GC reachability and pinned physical deletion assertions continue to pass. Go v2render/v2draft with cross-language checks and documentation checks passed. Migration353 is tracked under V25 and applied only in isolated test databases. Runnable graph registration, complete proposal/mask closure admission, worker qualification/activation and public API/startup wiring remain unfinished.

### All-mask pinned byte checks before composition transport (2026-10-02)

VerifyCompositionMasks derives distinct expression/protected MaskRef roots from the hash-checked compiled params using CompositionInputRoots. Each receives scoped pinned source/MaskRef reads, full geometry comparison and streaming probability-array verification through VerifyCompositionMask. Duplicate expression leaves and a protected reference to the same artifact are verified once. PrepareVerifiedComposition now invokes this set check after parent verification and before closure expansion/transport publication, so malformed or unreadable explicit masks cannot pass merely on catalog metadata.

Verification: Go v2render/v2draft with cross-language checks passed. A nested union/invert expression with shared protected mask opens/closes exactly one source, one mask and one NPY; valid probabilities pass and NaN rejects with the same reader closure guarantees. The real new-user-mask API+scheduler/PostgreSQL/MinIO scenario invokes the new complete mask byte check before composing/staging and passed (1 test,5.65s), retaining existing binding/revocation/GC evidence. Documentation checks passed (2067 static,54 semantic; deterministic regeneration). This fixture still has a synthetic proposal and is not proof of complete PrepareVerifiedComposition success or runnable worker execution. Complete adopted-proposal/closure transport integration, atomic graph admission, qualification/activation and API startup wiring remain outstanding.

### New user mask with a real adopted lens proposal (2026-10-02)

The live typed lens integration's same-geometry zero-strength case now publishes a fresh 5x5 float32 probability array through the production user binary publisher, creates a geometry-matched MaskRef with that exact binary ref, and invokes PublishVerifiedUserCompositionMask using actual API catalogs/MinIO and durable binding. Composition params reference this new mask and the actual adopted ImageResult root. The production scheduler complete user-mask set proof accepts; VerifyCompositionParents reads the real source/proposal documents and VerifyCompositionMasks reads the new pinned typed mask and all probabilities through the adopted catalog's scoped user-upload fallback.

Verification: all three actual typed lens adoption/geometry/revocation cases passed (3 tests,17.72s). Independent quota inspection confirms three user uploads in the same-geometry case (the prior adopted-array user document plus the new binary and bound document), exact summed storage charge and zero remaining reservation; changed-geometry cases create no user uploads. Go v2render/v2draft with cross-language checks and documentation checks passed. An initial duplicate test variable declaration was caught at compilation and removed; existing parent geometry tests already covered the original scenario. The new path establishes real adopted proposal plus new-user-mask document/byte preflight, while full closure transport publication, atomic runnable registration, worker execution/qualification and public API startup wiring remain outstanding. Lens fixture pixels are synthetic HDR data, so this is not photographic-quality qualification.

### Complete real adopted-proposal composition closure and transport (2026-10-02)

Extending the adopted lens fixture to full closure exposed a production defect: generic Params traversal downgraded typed source/proposal/mask references to Artifact, which the strict adopted catalog correctly rejected. The new compositionParamRefs decoder preserves WorkingImage/ImageResult/MaskRef contracts for the bounded eight-field layer.compose wire, validates base/ref/nullable fields, opacity/blend, common mask canvas hash and depth/visit limits, and returns typed nested/protected roots. ClosureResolver uses this decoder for composition Params and retains generic traversal for unrelated Params.

The same-geometry adopted lens fixture now publishes actual stable composition params referencing the new bound user mask and real adopted proposal, expands their entire scoped closure, compiles the plan from that inventory, reserves output quota and stages the blocked task through migration353. PrepareVerifiedComposition performs parent/all-mask pinned byte validation, repeats closure expansion and publishes actual chunk/manifest documents through ScopedPublisher and production artifactjson.Publisher/SQLRepository/MinIO. Count/digest match the saved task plan. Retry returns the same manifest. Independent SQL inspection accounts for four user uploads, backend transport bytes and the one output reservation. Its isolated tenant quota is 100 MB to accommodate the conservative output estimate.

Verification: all three actual typed lens adoption/geometry/revocation cases passed (3 tests,29.67s). Go v2draft/v2render with cross-language checks passed. Decoder tests establish preserved root contracts, rejection of foreign mask canvas and >16 depth, and generic fallback for unrelated params. An initial test used the wrong Built manifest field name and was corrected; the subsequent actual closure rejection led to the production typed-decoder fix without weakening scope. This establishes full real proposal/user-mask closure and immutable transport preparation, not registered/sealed/runnable inputs or worker execution. Atomic registration must still validate all producer lineage and the declared inventory under its locks; qualification/activation, public API startup wiring and the broader V20–V32 scope remain incomplete. Fixture HDR pixels are synthetic, not photographic-quality evidence.

### Atomic adopted lens proposal plus user-mask shell staging, migration354 (2026-10-02)

Migration354 adds scheduler-only stage_v25_lens_user_masked_composition_task. Under the active tenant/draft run locks it reads the exact frozen node, proves the complete user-mask set and repeats migration344 adopted lens proposal lineage for the exact version/source/canvas/actor/session, then invokes the existing exact-plan shell staging function within the same transaction. SQLOriginalCompositionTaskStage.StageLensUserMasked retains derived task/workflow identity and bounded profile/digest/role checks. The real adopted-proposal closure/transport fixture now uses this stricter staging route. Other operation proposal families require their separate lineage proofs; no arbitrary project fallback is introduced.

Verification: all three actual lens adoption/geometry/revocation cases passed through the stricter path (3 tests,63.00s). The newly extended same-geometry case then passed (1 test,21.63s): exactly one composition shell exists; direct scheduler replay accepts, expiration of the adopted proposal rejects, restoring its expiry allows the remaining fixture checks, and committed session revocation rejects replay. Existing actual source/new-mask params publication, complete scoped closure, PrepareVerifiedComposition manifest publication/replay and independent quota assertions pass. Go v2render/v2draft with cross-language checks and documentation checks passed. Migration354 is tracked under V25 and applied only in isolated test databases. Input batch registration/sealing and runnable graph activation, worker admission/qualification, public API startup wiring and the broader planned features remain incomplete; staging still leaves attempt-zero blocked tasks.

### Atomic composition task input registration and replay (2026-10-02)

CompositionInputAdmission.RegisterLensUserInputs now snapshots a bounded compiler-owned cursor (at most 512 entries), verifies exact task/workflow identity, sole direct params ref, entry count and compositionInputDigest against the saved draft, and copies transport refs. In one DB transaction it checks scheduler role, repeats migration354 proposal/user-mask authority under the run lock, registers the frozen rows using existing RegisterInTransaction and seals the task inputs. A ready replay compares manifest, entries SHA/count and all chunk refs rather than rewriting rows. This does not seal the runnable graph or qualify/activate the operation. The inventory must come from the existing scoped compiler resolver; this method does not independently reconstruct every transitive parent document inside SQL.

Migration355 provides scheduler-only v25_composition_input_state for a bounded read of the draft blocked task's sealed status/transport metadata, repeating actor/session editor authority. It uses the private readiness helper as function owner and exposes no direct table grants. Integration initially caught optional cursor Close handling, a missing query parameter and forbidden direct access to the private readiness function/tasks table. These were corrected using optional io.Closer and the narrow SECURITY DEFINER state function; existing role isolation was preserved.

Verification: actual adopted lens/new-user-mask scoped closure, transport, input registration and identical replay passed (1 test,8.93s). The extended full lens set then passed (3 tests,53.36s), including foreign-manifest and wrong-count rejection, independent input row count (entries plus chunks plus manifest), exactly one blocked attempt-zero task and a draft unsealed run. Existing expiration/revocation staging checks and quota assertions pass. Go v2render/v2draft/v2input with cross-language checks and documentation checks passed. Migration355 is tracked under V25 and applied only in isolated test databases. Runnable graph admission, worker operation registration/qualification, public API/startup wiring, broader operation family lineage and the remaining V20–V32 requirements remain unfinished.

### Attempt-fenced worker admission for original/lens/user-mask composition

Migration 356 introduces `photo.admit_v25_lens_user_composition`, executable only by
`photo_worker` through a fixed-search-path `SECURITY DEFINER` boundary. No table
grants, operation activation, or qualification records are added. The function
checks tenant/run/task identity, enabled registration bound to `session_user`,
attempt/fence/worker, unexpired lease and deadline, matching resource-slot fence
and `worker_authorized`. It requires a sealed v2.2 graph, complete registered
input transport, frozen task parameters/profile/digest, pinned ready input
metadata, live editor/session, original snapshot source, all durable user-mask
bindings, and adopted lens proposal provenance. Other parent families fail closed.

`LensUserLayerAdmission` binds the parsed parameters' RFC8785 bytes and SHA to the
leased command before calling the bounded `TaskDatabase` query. Local lease
checks bracket admission. A database denial stops processing before source
reads with `LAYER_SCOPE_STALE`; changed parameter bytes yield `INPUT_INVALID`.
Database errors propagate to the existing executor failure boundary. This check
is an admission-time proof; heartbeat, input materialization and output commit
remain responsible for subsequent lease loss and byte integrity.

Verification performed:

- Layer unit suite: **105 passed**, including exact lease/canonical-byte forwarding,
  denial, changed hash/operation/backend/parent, lost lease and query fence binding.
- Isolated PostgreSQL with all migrations through 356: **1 passed**; function role
  allowlist, security/search-path properties, missing-lease and wrong-SHA denial.
- Existing actual PostgreSQL/MinIO adopted-lens zero-strength composition fixture:
  **1 passed, 2 deselected** (13.95 seconds), including typed publication, complete
  closure transport and atomic blocked-task input registration/replay.
- Documentation checker: 2067 static checks and 54 semantic tests passed before
  this evidence append; deterministic generation verified.

Limits: there is no positive real running `layer.compose` lease integration yet.
The new admission implementation is intentionally not installed in the public
worker operation registry; runtime qualification and graph activation remain
pending. The MinIO fixture above exercises staging, not a runnable composition
worker. Physical migration 356 was verified in the isolated integration database,
not rolled out to the separate persistent development database. These checks do
not establish photographic quality or RTX3060 performance, nor completion of
V20–V32.

### Positive relational composition admission with published inputs

The zero-strength adopted-lens PostgreSQL/MinIO fixture now additionally creates
an explicitly synthetic worker registration, resource node/slot and running
attempt inside a `force_rollback` transaction. All input documents, binary masks,
transport manifests/chunks and adopted proposal are the fixture's actually
published objects. The new worker SQL admission accepts this valid relational
state and rejects each substituted tenant/run/task/attempt/fence/worker/parameter
ID/SHA/bytes/digest, expired task lease, mismatched slot fence, expired slot lease,
unrelated SQL principal, disabled registration, and revoked editor session.
Restoring the registration permits admission again. The entire simulated running
state rolls back; the authoritative composition task remains blocked at attempt
zero with an unsealed draft run. No production qualification or dispatch is inferred.

This supersedes the prior missing-positive-relational-proof limitation for
migration 356. An actual claimed worker process executing/staging/committing
composition is still unverified. Migration 356 now also explicitly requires an
unexpired CPU resource-slot lease. Initial testing exposed a test savepoint's
`SET LOCAL ROLE` surviving its release; explicit `RESET ROLE` fixes the fixture
without widening any application grant. After that fix, the extended positive
fixture passed once in 8.32 seconds before the slot-lease checks were added.

Final verification after the slot lease/principal checks: **2 passed, 2 deselected**
for the dedicated migration/grants test and extended zero-strength lens fixture
(9.72 seconds). Documentation regeneration remains deterministic; 2067 static
checks and 54 semantic tests passed after this section was added.

### CPU worker composition handler installation

`layer_operations(config, database)` now owns the per-attempt `TaskArtifacts`
context, restricts output roles to document/working_image/pixels/alpha/mask_pixels/
mask/report and invokes the composition adapter with `LensUserLayerAdmission`.
`build_worker` installs this handler for CPU workers with the same `TaskDatabase`
instance used by their executor. It is not installed for GPU/media/archive.
Registration and qualification checks remain mandatory; no database operation
activation or qualification is added. The implemented admission still supports
only original-source/adopted-lens/user-mask composition.

Verification: **106 layer unit tests passed** including factory failure cleanup
and real admission injection; **10 worker service tests passed**; documentation
checker passed (2067 static, 54 semantic). The initially specified service test
path did not exist, so no tests ran in that invocation; the actual platform test
file was then located and executed successfully. Previous statements that the
handler is not installed in worker code are superseded by this section. Public
registry activation, build qualification and actual claimed-task composition
execution/commit remain outstanding. No image algorithm was changed.

### Composition CPU runtime lock verification

`operations/layers/runtime_lock.py` adds a strict `LayerRuntimeLock` and
`verify_installed_runtime(lock, expected_sha)`. The verifier checks an externally
supplied canonical lock digest, exact CPython version/architecture, numpy,
pydantic and rfc8785 distribution versions, and streaming hashes of the Python
executable, numerical/parser binaries, composition algorithms/adapters/contracts,
resource/input/output handling and explicit shared color/session/input-closure
components. Missing or extra components/packages are rejected. It does not create
an approved lock from the running machine, qualify performance/image quality or
write any registry record. This verifier is not yet required by worker startup
or linked to a task execution-profile pin; that connection remains outstanding.

Verification: six actual-runtime lock tests pass, including changed external pin,
package version, architecture, component bytes and missing component. Combined
layer/worker-service suite: **122 passed** before adding the explicit shared
component hashes; the six runtime-lock tests passed again after that extension.
Documentation checker passed (2067 static, 54 semantic). This work adds build
identity machinery; it does not establish successful claimed-task execution.

### Mandatory pinned runtime for composition handler

Worker configuration now accepts paired `PHOTO_LAYER_RUNTIME_LOCK` and
`PHOTO_LAYER_RUNTIME_SHA256` settings exclusively for CPU workers. The lock file
is bounded to 64 KiB, rejects duplicate keys, and must resolve to the requested
canonical path. Installed runtime verification is required before handler
installation. Without explicit pins, `layer_operations` installs no handler;
partial or mismatched pins fail closed. Each invocation requires its frozen
execution profile's `layer_runtime_sha256` to equal the verified lock digest
before creating `TaskArtifacts`. This supersedes automatic unpinned installation
introduced earlier. No approved lock or qualification is synthesized. Compiler
profile pin emission, relational qualification, graph activation and actual queue
execution remain pending; existing blocked composition staging remains intact.

### Frozen compiler runtime pin and profile admission

`CompilePinnedCompositionDraftConfig` validates a lowercase SHA256 supplied by
its operator-qualified caller, compiles the existing bounded composition plan,
adds `layer_runtime_sha256` to every immutable task/node profile and recomputes
canonical config bytes and hash. It does not mutate prior tasks or synthesize a
qualification. Migration 357 preserves the previous exact resource predicate
as a private helper and allows one additional strictly validated runtime pin;
all original resource/backend/renderer checks still apply. Legacy unpinned
staging remains accepted, while the pinned worker refuses unpinned execution.

Go v2render tests passed, including pin presence in both profile copies, changed
plan hash and invalid pin rejection. Isolated PostgreSQL with all migrations
through 357: **2 tests passed** (4.48 seconds), including valid pinned profile,
uppercase pin rejection and insufficient RAM rejection. Documentation checks
passed (2067 static, 54 semantic, deterministic generation). Production caller
wiring, an authoritative qualification record tying this hash to measured quality
and capacity, runnable activation and queue execution/commit remain unfinished.

### Single-read runtime lock loading

`load_layer_runtime_lock` now opens a bounded regular lock file once with
`O_NOFOLLOW|O_NONBLOCK`, checks its descriptor metadata (single link, 1..65536
bytes), parses and validates the same bytes, then verifies the operator's
canonical digest and installed runtime. Worker configuration uses this loader
instead of separate reads for model validation and duplicate-key detection.
Symlinks, hardlinks, directories, oversized files and duplicate JSON keys are
rejected. A actual pinned runtime file loads successfully. No approval or
qualification record is inferred from successful loading. Inspection also
confirmed generic adoption currently has no layer-specific lineage verifier;
that verifier remains required before registry activation.

### Composition output metadata validator

`validate_layer_output_metadata` checks exact source canvas hash, source-parent
WorkingImage binding, unchanged canvas/color/dtype/transform chain, mandatory
inherited generated-mask presence, output mask metadata and exact composition
report semantics. It rejects clearing inherited generated lineage or downgrading
parent review to pass. The component is included in the layer runtime lock.
Tests use actual composed/staged fixture results and deliberate lineage/report
mutations. This validator is not yet wired into the production adopter and does
not prove binary mask coverage or pixel equality. Those checks and authoritative
adoption scope remain outstanding before operation activation.

### Bounded generated-coverage output proof

`validate_generated_coverage` verifies already hash-scoped mapped float32 arrays
in 512×512 tiles, bounded to 200 million pixels. It recomputes source/proposal
dependency coverage with the canonical lineage algorithm, including full normal
replacement, protected pixels and proposal contribution even without visible
RGB change. It requires the output mask exactly when inherited lineage exists,
rejects nonbinary parent masks and nonfinite/out-of-range probabilities, and
compares every output tile exactly. Cancellation checks occur before/between
tiles and within coverage evaluation. Artifact hash/provenance/canvas proof and
mapped-file ownership belong to the caller; this helper publishes nothing and
is not yet connected to production adoption.

Tests cover a 513×513 image crossing all four tile regions, protected source
coverage, generated contribution in the last tile, a deliberately omitted pixel,
missing output, fractional parent lineage, invalid probabilities and cancellation.

### Evidence-scoped composition metadata adoption

`verify_layer_metadata` reads task-authoritative params as input evidence and
ImageResult as output evidence, resolves source/parents and applies the metadata
validator. Documents use existing byte-length/SHA checks before contract parsing.
`prepare_v2_adoption` now requires a params reference for layer.compose and invokes
this proof before generic closure inventory construction. The adoption service
has not yet gained its narrow SQL params lookup; it therefore fails closed for
composition instead of accepting an unverified generic result. Binary coverage
verification, relational adoption and runnable activation remain pending.

Tests cover rejecting output-scoped parameter evidence, staged metadata wiring
and review downgrade. Parent fixture hashes are synthetic, so the latter test
explicitly isolates document loading; it does not prove real object byte integrity.

### Generation-bound composition adoption parameter lookup

Migration 358 adds scheduler-only `v25_layer_adoption_params` with explicit
attempt/fence/canonical-result arguments. It requires a succeeded uncancelled
composition task, matching canonical task result generation, sealed v2.2 run,
existing adoption context, sealed input transport and verified ready pinned JSON
parameter artifact registered as Params. No new table grants or worker/API
execute rights are added. AdoptionCoordinator now reads this reference before
composition evidence validation and rejects an absent/stale lookup.

Verification: all migrations through 358 apply in the isolated database;
**3 PostgreSQL tests passed** (6.22 seconds), including scheduler-only rights and
missing-generation denial. **24 adoption regression tests passed** (composition
metadata, generic V2 and lens); documentation checks passed. Positive successful
composition result lookup/adoption is not yet demonstrated. Binary coverage
materialization/checks and relational composition-specific commit validation
remain necessary before runnable registry activation; no operation was enabled.

### Streaming composition output array checks in adoption

`verify_layer_output_arrays` now follows metadata proof in `prepare_v2_adoption`.
Output changed/generated masks require version-scoped output evidence and exact
canonical float32 NPY shape, byte count/SHA, finite binary values. Newly staged
RGB/alpha arrays require matching roles and finite float32 values (alpha in unit
interval); inherited arrays remain subject to scoped input closure validation.
The implementation uses the existing bounded streaming verifier and closes its
iterator on success/failure; cancellation now runs between stream blocks. No
full-size array is allocated. Tests exercise seven-byte stream boundaries,
accepted zero/one and rejected fractional/NaN masks with iterator closure.
Twenty-eight adoption regressions passed before adding per-block cancellation.
Exact generated coverage against parent inputs and RGB/changed-mask consistency
are still pending; operation activation remains disabled. The metadata wiring
tests use synthetic fixture hashes and are not photographic qualification.

### Bit-exact composition pixel proof

`validate_composed_pixels` compares mapped RGB, changed mask and optional alpha
against canonical `compose_layer_tile` results in bounded 512×512 tiles, up to
200 million pixels. RGB/alpha use uint32 bit comparisons so signed zero and
protected pixels cannot pass after an unintended transfer round-trip. Inputs
must have exact float32 shape; mask/protection probabilities are finite and
bounded rather than silently clamped into accepted evidence. Output alpha
presence follows existing staging semantics (original alpha or an actual alpha
change). Cancellation is checked between/within tiles. No full-size array is
allocated or artifact published.

Thirteen focused output pixel/coverage tests passed: HDR full replacement,
protected signed zero, last-tile mutation, false changed mask, unnecessary alpha,
alpha-only changes and coverage rejection. Adoption materialization still needs
to supply scoped mapped arrays to this proof; it is not yet called by production
adoption. A canonical pixel proof is not photographic/model qualification.

### Budgeted evidence array materialization

`ArrayEvidenceSession` creates an isolated temporary directory under the caller's
scratch root, reserves declared bytes against an explicit aggregate budget before
object IO, and streams only exact evidence-scoped NPYs (maximum 64 KiB blocks).
It verifies actual length and SHA before read-only float32 mapping through the
existing bounded shape/value validator. Repeated identical requests reuse the
mapping without further IO. ExitStack closes mappings before removing files and
the directory. Stream iterators close on success, truncation, wrong SHA and shape
failure; budget failure performs no stream read. A failed materialization still
consumes the session budget. No full-size byte buffer or image allocation occurs.

Five dedicated tests passed for actual NumPy bytes, read-only mapping/cache,
pre-IO budget denial, tampered/truncated stream and wrong shape, with directory
cleanup. This component still needs adoption coordinator scratch configuration
and composition-proof orchestration; successful mapping alone is not successful
adoption. Existing operation activation remains disabled.

### Composition proof orchestration and adoption hook

`verify_layer_composition` now joins scoped parent/image/mask descriptors,
`ArrayEvidenceSession` mappings, bounded mask-DAG evaluation, exact generated
coverage and bit-exact RGB/alpha/changed-mask proofs. The effective mask is an
attempt-owned disk memmap reserved against the same explicit scratch budget;
no full-image mask is allocated in RAM. Parent canvas/color/chain must match.
All mappings and derived files close/remove on failure. `prepare_v2_adoption`
requires this proof for layer.compose after streaming output validation;
AdoptionCoordinator accepts explicit `layer_scratch_root`/`layer_scratch_bytes`
and forwards them. Missing configuration fails closed with
`LAYER_ADOPTION_SCRATCH_REQUIRED`; no ambient temp directory is selected.

The orchestration test uses actual algorithm/staging fixture data, mocked
metadata/mapping IO because those parent hashes are synthetic, and rejects a
mutated output pixel. Dedicated ArrayEvidenceSession tests independently prove
hash-scoped disk IO/cleanup. This is not yet a real PostgreSQL/MinIO positive
composition adoption integration. Production scratch configuration and
composition-specific relational commit validation remain pending before
activation. The resource budget includes derived effective-mask bytes.

### Real-byte complete composition proof

A new fixture builds actual canonical JSON and NumPy NPY artifacts with exact
SHA references, HDR proposal values, mask DAG and inherited generated lineage.
`verify_layer_composition` runs without mocking document validation, disk
materialization, hash/shape checks or output algorithms. It accepts a correct
result, rejects a corrupt stream, rejects insufficient scratch, and rejects
incorrect output RGB even after every affected JSON/NPY SHA is recomputed
correctly. All opened iterators close and the scratch directory is empty on all
paths. This replaces the earlier synthetic-hash limitation for the pure
composition proof; object evidence authority is still an in-memory test reader.
It does not establish PostgreSQL task provenance, MinIO version pinning, atomic
adoption or actual worker execution. Those integration gates remain pending.

### Actual versioned MinIO composition proof

`test_v25_layer_objectstore` publishes the real-byte composition fixture's entire
JSON/NPY closure to an isolated versioned MinIO bucket. It uses production
PostgresObjectEvidenceReader object-read/stream methods with explicit fixture
catalog locations (the SQL lookup is bypassed only in this test). Full
`verify_layer_composition` runs through actual object reads, SHA checks, disk
mappings, DAG evaluation, inherited coverage and pixel/alpha/changed-mask proof.
Publishing corrupt newer versions of the ImageResult and output pixel key does
not replace catalog-pinned versions; the proof still succeeds and scratch is
empty. **1 integration test passed** (1.30 seconds). Documentation checks passed.

This proves MinIO version pinning and complete binary composition preparation,
not PostgreSQL scoped evidence authority, successful worker dispatch/commit,
registry qualification or atomic adoption. Catalog authority and composition
commit fences remain separate outstanding integration work. No public operation
was activated and no image-quality claim is inferred from synthetic HDR data.

### Composition relational adoption authority wrapper

Migration 359 wraps the existing adoption chain with composition-specific checks
under tenant → run → task locks. It rechecks succeeded attempt/fence/canonical
result, ready generation-bound Params, sealed input transport, frozen parameter
bytes/SHA/input digest/profile and explicit runtime pin. The implemented original
source + adopted lens + user-mask family also repeats snapshot/live editor and
session authority, durable complete mask-set bindings and adopted proposal proof
before existing artifact/link/inbox/event publication. Other parent families
remain unsupported. The prior core is private to fn_owner; scheduler/API/worker
cannot bypass the wrapper. Existing noncomposition operations retain their
previous path and exception behavior. No runtime qualification is invented.

Initial isolated PostgreSQL/MinIO regression: **4 passed, 2 deselected**, including
actual adopted-lens zero-strength composition staging. The final wrapper narrows
malformed-plan exception handling to composition only; a subsequent dedicated
migration/grants suite verifies the final SQL. Successful atomic composition
adoption and concurrent authority-change behavior are still unproven. This
wrapper does not activate the registry or replace the mandatory Python binary
proof. Persistent development DB rollout is separate from isolated verification.

### Production adopter scratch configuration and retry classification

`AdopterConfig.from_env` accepts paired `PHOTO_ADOPTER_LAYER_SCRATCH_ROOT` and
`PHOTO_ADOPTER_LAYER_SCRATCH_BYTES`. The root must be an existing absolute
canonical directory; the budget is a bounded positive integer. `run_adopter`
passes them through Adopter → AdoptionCoordinator → composition proof. No
ambient scratch directory or implicit unbounded allowance is used.

Missing composition scratch and exhausted scratch budget are now retryable
operational conditions in the adopter loop, not invalid-result rejection.
Composition proof preserves `AdoptionInvalid` resource codes instead of wrapping
all such failures as semantic corruption. Tests verify paired/bounded config,
retry without rejection and preserved budget-exceeded code in the real-byte proof.
This does not assert that production deployment settings have been provisioned
or that positive PostgreSQL composition adoption has succeeded.

### Machine and SQL composition catalog registration

Inspection found `layer.compose` absent from the generated operation catalog,
so generic adoption returned UNKNOWN_OPERATION despite the new proofs. The
source generator now registers the existing LayerComposeParams/ImageResult
contract as a V25 CPU operation. ImageResult output policies permit inherited
working_image/pixels refs for compositions without visible pixel changes; the
mandatory composition proof still establishes exact output semantics. Machine
catalog/index regeneration now yields 26 operations and 77 entry points without
changing the 165-schema set. Migration 360 adds the CPU SQL operation and output
policies with `v2_enabled=false`; no runnable qualification/activation is implied.

An actual-byte composition test now runs complete `prepare_v2_adoption`, including
proofs and generic closure/inventory digest construction. **156 layer/adoption
unit tests passed**. Isolated migration/grants suite: **4 passed**; dedicated
catalog test verifies registered CPU routing and disabled v2 activation.
Documentation: **2076 static checks, 54 semantic tests passed**, deterministic
regeneration. Successful PostgreSQL atomic composition adoption remains pending;
this evidence establishes preparation and disabled catalog registration only.

### Positive generation-bound SQL Params read

The actual adopted-lens/Go composition fixture now also creates synthetic
terminal metadata inside a nested force_rollback transaction: task attempt,
verified document/upload metadata, canonical task result and succeeded state.
The scheduler-only parameter lookup returns the existing actually published
composition Params for the exact task/attempt/fence/result. Substituted
identities/generations return no rows; an expired Params artifact also returns
no rows. Nested rollback restores the running test lease before its session
revocation check, and outer rollback preserves blocked draft staging.

Initial positive lookup test passed after the fixture cleared lease_until when
entering succeeded state, as required by tasks_domain_3. This is relational
parameter-read evidence only: the synthetic output is not valid composition
JSON and does not establish byte-verified terminal output or atomic adoption.
A valid complete PostgreSQL/MinIO composition result remains the next integration
gate. No runnable registry activation or qualification was added.

### Positive SQL/MinIO complete composition adoption

The zero-strength adopted-lens fixture now compiles its composition using
CompilePinnedCompositionDraftConfig with an explicit synthetic test pin before
immutable run creation. Test terminal registration publishes actual zero changed
NPY, MaskRef, composition report and ImageResult to versioned MinIO; source RGB
and WorkingImage refs are retained. Artifact/upload/result metadata is tied to
the composition attempt/fence. Production PostgresObjectEvidenceReader performs
SQL scope lookup and pinned IO, and complete prepare_v2_adoption validates actual
bytes/DAG/coverage/pixels then creates inventory/digest. Public SQL adopt_v2_plan
returns true; replay returns false without re-publication. An additional run
asserts one inbox marker, one adoption event and exactly inventory-length links.

An initial immutable-config mutation was rejected by V2_RUN_CONFIG_IMMUTABLE;
the fixture was corrected to use the pinned compiler instead of weakening the
constraint. The complete adoption fixture then passed (8.06 seconds). The prior
synthetic-output limitation for this integration is superseded: output bytes
are real, but worker dispatch/gateway upload attestation is still bypassed by
fixture metadata registration. The test runs inside rollback transactions to
preserve original blocked staging; it does not prove transaction-free object
preparation in the deployed coordinator, concurrent revoke handling, registry
qualification or queue execution. The synthetic runtime pin is not an approved
build. Public v2 activation remains false.

### Adoption rejects authority changes without partial publication

The actual SQL/MinIO complete composition fixture now prepares inventory and
binary proof first, then tests six separate authority changes before commit:
revoked editor session, cancelled run, cancelled task, incremented task fencing
token, expired Params artifact, and expired adopted proposal artifact. Each
public adopt_v2_plan call returns false. Independent SQL assertions show zero
composition links, zero matching inbox marker and zero adoption events after
each rejected call. Each mutation rolls back before the next case; the same
prepared result then adopts successfully and its replay remains idempotent.
The zero-strength integration case passed (8.82 seconds).

These are sequential post-preparation authority changes in isolated rollback
transactions, not a concurrent-thread race test. They verify the wrapper's
negative branches and no partial publication, not deployed worker qualification,
queue dispatch or gateway attestation. All output bytes/proof and SQL evidence
remain real; only registration/terminal state and runtime qualification pin are
fixture-provisioned. Public v2 operation activation stays disabled.

### Authority row locks through adoption completion

Migration 361 extends the composition-only adoption branch with FOR SHARE locks
on user, session, tenant membership, project and existing actor project-ACL rows
before repeating authority predicates. Locks remain through caller transaction
completion, preventing status/role/session/project changes between validation
and link/inbox/event publication. The established tenant → run → task order is
preserved before these authority locks. No table grants were expanded; positive
SQL/MinIO composition adoption passed under the existing fn_owner privileges.

An independent PostgreSQL connection now attempts session revoke, user status,
membership role and project-name updates while the successful adoption transaction
is still open. Each receives LockNotAvailable under a 150ms lock timeout rather
than changing authority before commit. Writers use force_rollback transactions;
the test does not leave changed authority. The full fixture passed (8.59 seconds).
The owner fixture has no actor ACL row, so concurrent ACL deletion is not yet
independently exercised. This is a real two-connection lock-holding test, not a
proof of every possible deadlock/race ordering. Worker qualification, queue and
gateway execution remain outstanding. Documentation checker passed with the
new migration (2076 static, 54 semantic).

### Runtime pin revalidation before each composition task

The CPU layer operation factory now retains a deep copy of the operator runtime
lock and re-runs installed-runtime verification before every task's TaskArtifacts
creation, in addition to initial installation and frozen profile SHA matching.
An external mutation of the original configuration object cannot change the
factory's retained pin. Changed package/binary/source bytes fail before artifact
IO. The added test simulates post-startup runtime drift, verifies both checks use
the original copied component pin, and proves no artifact session is created.

This enforces build identity, not approved image-quality/resource qualification.
A trusted qualification dataset/report and actual queue/gateway worker execution
are still outstanding; no approved report was fabricated or operation enabled.

### Explicit legacy alpha normalization primitive

`layers.alpha.layer_alpha` adds an explicit mode boundary for legacy uint16
straight opacity: require_float32 rejects uint16; normalize_uint16 uses the
existing bounded optical normalization with exactly float32(value)/65535.
Missing alpha stays absent without allocating an opaque image; float32 input is
validated and borrowed. Returned LayerAlpha marks converted representation as
requiring a new artifact reference, so a caller cannot label normalized bytes
with a preserved legacy reference. Disk mappings/reservations close on exit.
The new component and shared optical alpha helper are included in runtime pins.

Four focused tests pass for endpoint/intermediate conversion, unchanged input,
scratch cleanup, rejected implicit conversion, absent/borrowed alpha and nonfinite
float32. This helper is intentionally not automatically enabled in composition:
compiler-declared conversion/provenance and adoption support must be completed
before legacy alpha flows through the production composition adapter. The current
adapter still requires float32; existing quality pipelines are unchanged.

### Frozen composition alpha policy (migration 362)

CompileCompositionDraftConfigWithAlpha records require_float32 or normalize_uint16
in every task and plan node execution profile, then recalculates canonical plan
bytes and SHA256. Unknown policies are rejected; the two supported declarations
produce different plan hashes. Migration 362 wraps the existing runtime/resource
profile validator, allowing only these explicit policy values while preserving
all previous runtime pin and resource checks. Profiles without the field retain
the existing require_float32 behavior.

The production adapter rejects normalize_uint16 with CONFIG_INVALID before
artifact IO. Declaration does not authorize executable conversion: resource
accounting, new output alpha reference publication, preserved input provenance
and adoption support must be implemented before that mode can execute. No
operation activation or quality qualification is claimed.

Verification: Go v2render tests passed; isolated PostgreSQL pinned-profile test
passed (1 test, 4 deselected), including both policy declarations and rejected
unknown mode. Documentation validation passed (2076 static and 54 semantic
checks, deterministic regeneration). Migration 362 was tested in an isolated
database only; it was not applied to a running deployment.

### Explicit alpha conversion resource accounting (migration 363)

The compiler and Python admission now reserve two concurrently live normalized
parent alpha mappings for normalize_uint16: an additional 8*N+8192 bytes in both
RAM and scratch, where N is the canvas pixel count. This matches two float32
maps and their bounded file overhead. The existing output budget already
includes float32 output alpha and remains unchanged. Missing/float32 alpha may
use less memory, but the frozen declaration budgets the worst case without
inspecting or inferring mutable inputs.

Forward-only migration 363 requires the exact expanded frozen estimate before
delegating to the existing runtime/profile validator. Old-sized normalization
profiles and estimates short by one byte are rejected. Strict/default profiles
retain their prior estimates. This supersedes migration 362's initial acceptance
of normalization declarations with the baseline estimate.

Verification: 156 layer unit tests passed; isolated PostgreSQL profile test
passed for both declarations, strict baseline, insufficient RAM/scratch and
invalid mode. Go v2render tests and documentation validation passed. This is
resource accounting only: production normalize_uint16 remains blocked until
input normalization, representation-aware publication and adoption proof are
integrated. Migration 363 was not rolled out to any running deployment.

### Representation-aware alpha preparation and publication

prepare_layer_inputs now honors the explicit resource-admitted normalization
profile when reading parent alpha. Legacy NPY inputs use the existing strict
uint16/float32 layout validator and bounded layer_alpha normalization; strict
profiles still reject uint16 without implicit conversion. Both parent maps stay
owned until composition and staging finish, then close and release reservations.

LayerFiles.alpha_converted records source representation conversion separately
from semantic alpha_changed/any_changed. Conversion forces publication of a new
float32 alpha artifact and WorkingImage even when RGB, opacity and changed_mask
are unchanged. RGB retains its original reference independently; the report's
VersionBinding retains the original source WorkingImage reference. Proposal-only
normalization does not by itself require replacement of source alpha.

Verification covers real uint16 NPY inputs (0 and 32768), exact float32 division
by 65535, new alpha references, zero semantic mask for normal unchanged content,
unchanged original input bytes, strict-mode rejection, balanced input lifetimes
and cleaned scratch files. Result tests cover independent RGB/alpha changes,
conversion and inherited parent review. All 168 layer unit tests passed;
documentation checks passed (2076 static, 54 semantic).

This preparation/publication path is tested directly. The production adapter
continues to reject normalization profiles until adoption validates the same
conversion and newly published alpha bytes. No queue activation or photographic
qualification is claimed.

### Budgeted legacy alpha evidence for adoption

ArrayEvidenceSession now supports explicit immutable-input legacy alpha evidence
and an alpha normalization helper. SHA256, exact stream size, NPY layout and
scope checks remain mandatory before mapping. normalize_uint16 reserves an
additional 4*N+4096 bytes before allocating its float32 disk map, converts in
512-pixel tiles by float32(value)/65535, checks cancellation, and returns a
read-only cached mapping. Float32 inputs are borrowed; absent alpha allocates
nothing. Output evidence cannot opt into legacy decoding; output float32
validation remains strict.

Five new tests cover exact uint16 conversion, cache/budget accounting, rejected
strict mode, insufficient normalization scratch, forbidden output legacy mode,
invalid policy, closed streams and removed files. All 173 layer unit tests pass.
This helper is not yet invoked by the composition adoption coordinator: binding
the policy to authoritative frozen task metadata and verifying replacement
output references remain required before production normalization is enabled.

### Bit-exact conversion proof and replacement references

verify_layer_composition accepts an explicit alpha_mode and uses the budgeted
input-alpha evidence helper for both parents. Canonical composition then checks
float32 output alpha and changed_mask against the normalized parents, tile by
tile. When source input alpha is uint16, proof additionally requires a new
output-scoped alpha artifact with role alpha and an output-scoped WorkingImage;
retaining the old alpha/source reference is rejected. Defaults remain strict.

Five real-byte cases cover valid exact conversion (0, 1, 32768, 65535), strict
mode rejection, retained legacy reference, output uint16 and wrong float32
values with internally consistent SHA references. All 178 layer unit tests
passed after updating the orchestration test's evidence-session double for the
new alpha method; documentation checks passed (2076 static, 54 semantic).

This closes the binary proof helper, not the policy-authority boundary. The
coordinator still uses the strict default; it must obtain alpha_mode from a
narrow generation-bound SQL lookup of frozen task metadata before passing it
to preparation. Production normalization remains blocked pending that wiring.

### Frozen SQL alpha policy wired into the adoption coordinator (migration 364)

The new scheduler-only v25_layer_adoption_alpha_policy lookup first repeats the
exact tenant/task/attempt/fence/result Params eligibility gate. It requires one
frozen composition node, equality of frozen and task profiles (excluding the
four input transport fields), and the existing SQL runtime/resource validator.
Malformed, stale or mismatched state returns NULL. Missing policy retains the
strict require_float32 default. No direct scheduler SELECT grants were added.

AdoptionCoordinator now passes only this SQL-returned mode into generic adoption
preparation and the composition byte proof. A missing/unknown policy rejects
before preparation or commit. Four coordinator cases verify exact generation
arguments, both accepted policies, and absence of preparation/commit on stale
or invalid policy. Eleven adoption platform tests pass; the preceding combined
layer/platform run passed 185 tests before these four cases were added.

Isolated PostgreSQL checks confirm scheduler-only execution and stale lookup
returns NULL. The real SQL/MinIO zero-strength composition fixture returned the
strict frozen policy and completed existing byte proof/SQL adoption (1 passed,
2 deselected, 10.31s). Documentation validation passed. Migration 364 has not
been applied to a running deployment. A positive SQL/MinIO normalization-mode
fixture and production adapter qualification still remain; the adapter's
normalization execution guard is unchanged for now.

### Worker execution and generic adoption of normalized alpha

The CPU adapter now accepts both known explicit alpha modes; unknown modes still
fail before artifact IO. Execution retains relational admission, runtime pin and
resource checks. The operation catalog remains v2_enabled=false and no approved
qualification report was created. This removes the temporary adapter-only guard
superseded by resource, publication and adoption policy/proof implementations.

Adapter tests exercise actual uint16 inputs under both inherited pass/review
reports, preserved source bytes, new alpha reference publication, and cancellation
cleanup for normalized and strict paths. A new canonical-byte test executes the
real adapter, stages its generated JSON/NPY closure, and passes that closure to
generic adoption with normalize_uint16 and full byte proof. Relational admission
is a test double in this case; it is not an actual queue/gateway or SQL-normalized
run. The test checks no temporary files or adoption directories remain, allowing
ScratchSpace's intentionally retained empty parent directories.

All 193 layer/adoption platform tests passed after correcting the new test's
cleanup expectation for those empty parent directories. Documentation validation
passed (2076 static, 54 semantic). A normalization-mode SQL/MinIO run and runtime
quality qualification remain required before operation activation.

### Real object-version verification of legacy alpha conversion

The MinIO composition proof test now covers both strict and legacy alpha inputs
using actual versioned JSON/NPY objects. In the conversion case it verifies
exact normalization and new alpha output bytes, rejects the strict policy, then
writes corrupt latest versions of source alpha, output alpha, RGB and root JSON.
The pinned original versions still pass. Replacing a catalog location's version
pin with the corrupt latest version rejects rather than falling back to latest
or accepting the original metadata hash. Scratch cleanup is verified for success
and rejection. Both cases passed (2 tests, 3.24s); documentation checks passed.

This test uses production object reads with fixture-supplied catalog locations;
it does not exercise SQL catalog lookup, task claiming or gateway attestation.
The existing SQL/MinIO composition authority fixture was additionally extended
to assert the alpha-policy lookup rejects substituted tenant/task/attempt/fence/
result and expired Params, alongside the existing Params lookup negatives.
The extended SQL/MinIO zero-strength authority fixture passed (1 test, 2
deselected, 21.90s), confirming those generation/expiry policy rejections.

### Bounded photographer editor history checkpoint

The existing photographer EditorStore now caps local history at 200 complete
snapshots as required by the professional editor client specification. On a new
command it discards the redo branch, retains the latest 200 entries and sets the
cursor to the final entry. The oldest retained complete snapshot becomes the
checkpoint, preserving parameters, mask references and selection without
replaying older commands or launching any model. Previous EditorState objects
remain unchanged; pending previews are still invalidated on commands.

A boundary/branch test applies 250 commands, verifies checkpoint 51 and all 200
retained snapshots, undoes to the checkpoint, redoes to 250, branches after undo,
and verifies discarded redo and stale preview rejection. Four editor-store
tests and photographer TypeScript typecheck passed. This closes the bounded
local history behavior; offline draft persistence and full v2 client flows are
separate requirements and are not claimed complete here.
The full photographer suite also passed: 70 tests in 16 files; TypeScript
verification passed again after the new test was included.

### Save selected editor mask history with source/revision checks

prepareEditorDraftSave now builds the save payload from the selected history
snapshot's mask references and current recipe, preserving the server CAS revision.
It rejects changed source SHA, asset version or revision before a request and
deep-copies mutable payloads. The editor route uses this helper instead of the
latest server draft's mask list, so undo followed by save no longer silently
reinstates masks from a newer checkpoint.

After successful mask persistence the route also appends the persisted mask
snapshot to the existing EditorStore and advances its draft revision. This
prevents a subsequent recipe save from dropping newly saved masks. Source
changes initialize a new source-bound store. Tests cover selected masks after
undo, unchanged CAS authority, independent payload copies and rejected identity/
revision mismatch. This does not implement offline persistence or conflict
comparison UI; those remain separate unfinished client requirements.
Verification: all 71 photographer tests in 16 files and TypeScript typecheck passed.

### Preserve newer local recipe edits across asynchronous save responses

acceptEditorDraftSave binds a response to the submitted source/version and base
revision. If local requestRevision advanced meanwhile, it preserves current
history and recipe, advances the saved server revision, and invalidates pending
previews. A clean response appends the persisted snapshot. Duplicate/stale base
responses and changed sources reject before state replacement.

The editor route now records valid recipe edits in its existing history; even
temporarily invalid JSON advances the local request revision and invalidates
previews. Save responses cannot overwrite a newer local text edit. If newer
edits exist, saveCurrent updates its baseline then stops dependent preview/mask/
generation actions rather than treating an older saved recipe as current. The
save UI states that the submitted revision was saved and newer edits remain
unsaved. Source/revision conflict comparison and offline persistence remain open.

The new test covers clean response, preserved newer snapshot, advanced baseline,
immutable history and stale/source rejection. TypeScript passed. The first full
suite had one Capture creation DOM retry failure; isolated rerun of that file
and editor store passed (8 tests). This is recorded rather than treating the
initial full run as passing.
The full rerun reproduced the Capture retry failure. Its test immediately
clicked retry after the error text appeared, without waiting for the submission
button to leave its disabled state. The test now waits for aria-disabled to
clear before retrying, matching the available user action. Full suite then
passed: 72 tests in 16 files. This change does not weaken request body/key or
callback assertions. No production Capture behavior was changed.

### Explicit editor CAS conflict comparison and resolution

Recipe save 409/412 responses now load the latest server draft and expose a
comparison of local/server recipe and mask references. No server mutation is
performed by conflict resolution. Keeping local edits advances their baseline
to the fetched server revision for the next explicit CAS save; accepting the
server snapshot adds it to existing undo history. Both choices reject changed
source/version and older revisions. Unresolved conflicts block saveCurrent and
its dependent actions. New comparison strings are translated in RU/BG/EN.

The initial draft loader now clears stale conflict/store state and ignores its
response after unmount/source dependency change. This prevents an old load from
installing the prior source's editor state. Pure state tests cover both explicit
choices, correct next-save revision/masks, undo recovery and source/revision
rejection; locale key parity remains tested. This does not yet add offline
persistence, conflict handling for the independent mask-save path, or rendered
DOM acceptance of the full editor route; these remain unverified requirements.
Verification: 73 tests in 16 photographer files and TypeScript typecheck passed.

### Shared CAS conflict handling for asynchronous mask persistence

Recipe and generated-mask saves now share saveDraftWithConflict for 409/412
lookup and comparison. Mask persistence captures its editor source/revision,
checks them before waiting and again before committing generated masks, then
uses acceptEditorDraftSave instead of unconditionally replacing JSON/history.
A changed source/revision rejects; newer local request edits retain their local
snapshot and open an explicit comparison with the successfully persisted server
mask snapshot. Neither accepting nor keeping that conflict immediately writes
a new server draft. Undo can recover the local snapshot after choosing server.

The new state test covers a generated-mask save arriving after local recipe and
mask edits, preserved local masks, and both subsequent explicit choices. The
existing full suite and typecheck passed after route changes (73 tests); final
verification including the new state case is recorded below. Full rendered
editor route acceptance and offline persistence remain unfinished.
Final verification passed: 74 photographer tests in 16 files and TypeScript typecheck.

### Scoped offline editor draft cache foundation

EditorDraftCache stores a validated base server draft and local complete snapshot
under origin/user/tenant/asset-version keys. Reads reject incompatible versions,
invalid source SHA/revision, malformed recipe modules, more than 512 mask refs,
invalid timestamps, data older than seven days, and envelopes exceeding one
million characters. Returned envelopes omit unneeded server fields. Writes
validate before persistence; storage mutations are serialized.

AuthProvider logout cleanup now clears editor caches as well as capture drafts.
Origin epochs invalidate old store instances before cleanup queues, preventing
late writes from resurrecting logged-out drafts. Reads also recheck the epoch
after asynchronous storage access. Other origins remain unaffected.

Three tests cover scope isolation, malformed/stale/cross-version rejection and
logout versus late-write behavior including a fresh post-login store. Full
photographer suite passed 77 tests in 17 files; after tightening revision type
and post-read epoch checks, the three cache tests and TypeScript passed again.
This is the persistence foundation only: editor route autosave/restoration,
proxy cache and reconnect comparison are not yet integrated or claimed ready.

### Editor route persists and restores source-bound local snapshots

The editor route now obtains the authenticated user from /me, creates its scoped
EditorDraftCache, and reads cached state after loading the authorized server
draft. Matching source/version/revision restores the local complete snapshot.
A changed server revision restores local edits against their original base and
opens explicit comparison with the fetched server draft; no write is automatic.
A changed source/version refuses restoration and reports the source change.
Local cache/storage failure does not prevent authorized network draft editing.

Valid recipe changes, undo/redo, accepted recipe/mask saves and conflict choices
persist their source/revision-bound snapshot. Invalid in-progress JSON retains
only the last valid cached snapshot. Storage errors are surfaced in RU/BG/EN.
A restore-mode test covers unchanged base, changed revision, SHA and version.

This implements recovery after an authorized network draft load, not a cold
start without network: cached principal discovery and proxy byte caching are
still missing. Full editor DOM/browser acceptance is also outstanding. No
complete offline product readiness is claimed.
Verification: all 78 photographer tests in 17 files and TypeScript typecheck passed.

### Rendered editor conflict comparison acceptance

The editor route uses a dedicated EditorDraftConflict component for the existing
local/server comparison. Its rendered web tests verify visible local recipe and
mask snapshot, server recipe/masks, alert text, no automatic resolution callback,
and exact explicit local/server button actions. Both choices are disabled while
busy; clicks in that state do not resolve anything. Buttons now explicitly expose
the button accessibility role on all React Native surfaces.

This verifies the comparison component's DOM behavior alongside the existing
state/CAS tests. It does not substitute for full editor route network/cache race
acceptance, native device validation, proxy offline storage or cold offline boot.
Verification: 80 tests across 18 photographer files and TypeScript typecheck passed.

### Rendered full editor route: CAS retry and late-response recovery

New web DOM tests mount the actual editor route with controlled API/storage
adapters (only imaging subviews are replaced). The first case edits the recipe,
receives a 409, verifies comparison appears without another save, explicitly
keeps local edits and checks the next save sends the fetched server revision
with those local parameters. The second case edits again while save is pending,
then resolves the older request; the visible recipe remains newer and the local
cache records that newer snapshot against the successfully saved base revision.
The save action now has an explicit button accessibility role.

These are route-level asynchronous UI tests, not live API/backend or native
hardware acceptance. Offline cold boot, proxy cache, full mask UI races and
remaining V20–V32 product requirements are still unfinished.
Verification: 82 tests across 19 photographer files and TypeScript typecheck passed.

### Rendered restart recovery and source preview lifecycle

Two additional full editor route DOM cases load a persisted local snapshot.
Changed server revision restores local recipe against its cached base, opens
comparison without writing, and an explicit keep-local choice makes the next
save use server revision 9 with those edits. A cached different source SHA is
refused: the route displays the authorized server recipe, reports the source
change, and neither opens a merge nor writes a draft.

The media preview effect now clears source/candidate previews and selected mask/
protected-output state when API/asset/version changes. It ignores late responses
and errors after cleanup, preventing a previous source preview from replacing
new source state. This effect's late-response branch is not directly exercised
by the new restart cases; dedicated media lifecycle acceptance remains open.
Cold offline startup and persistent preview bytes remain unfinished.
Verification: 84 photographer tests across 19 files and TypeScript typecheck passed.

### V26 region preview geometry foundation

CompileRegionGeometry calculates an integer pixel ROI on the already-oriented
canonical canvas from normalized Rect coordinates. It rejects nonfinite/outside/
empty rectangles, invalid edge limits and canvases above 200 million pixels.
Floor start/ceil end conservatively cover the selected region; output dimensions
preserve aspect within integer rounding, never upscale and stay at or below
max_edge (64..1024). No image bytes, master download or database access occurs.

Tests cover landscape/portrait oriented canvases, tiny subpixel ROI, no-upscale
small images and invalid rectangles/dimensions. Go editor package tests passed.
This is a pure geometry foundation: the specified region-preview HTTP endpoint,
source/recipe admission, worker rendering/color handling, output ticket and
client loupe are not yet implemented or claimed available.

### RegionPreview strict public request decoder

RegionPreviewRequest and RegionRecipeRef mirror the specified version/rect/
immutable-recipe/max_edge contract. DecodeRegionPreviewRequest bounds request
bytes to 16 KiB, applies existing canonical JSON validation, requires every
field (including zero-valued rectangle starts), rejects unknown/nested extra
fields, nulls, duplicate/trailing JSON, nil UUIDs and non-lowercase SHA256, and
reuses geometry checks for nonempty normalized ROI and edge limits.

Go editor tests passed for valid exact contract and malformed/null/missing/
duplicate/extra/oversized requests. This validates shape only; authorization,
recipe/source relation, object readiness and runtime rendering are not inferred
from JSON. No new HTTP endpoint or successful preview job is claimed yet.

### Region immutable recipe/source evidence compilation

The region calculation/request code now lives in internal/v2region so it can
reuse the existing strict FrozenRecipe decoder without introducing editor ↔
v2draft/v2learning import cycles. CompileRegionEvidence checks raw document SHA
against supplied immutable recipe/source refs, parses WorkingImage and FrozenRecipe
using existing validators, and requires exact recipe asset-version/source ref/
canvas hash before calculating ROI geometry. It preserves validated color,
transform and runtime lock metadata for the future plan compiler.

Tests cover valid canonical byte evidence, foreign asset version, changed recipe
bytes and substituted source artifact ID. This is immutable contract evidence,
not SQL authorization or ready-object attestation: scoped access, pinned versions,
closure sealing and HTTP/worker execution remain unfinished.
Verification: Go tests passed for internal/v2region and internal/editor after extraction.

### Bounded version-pinned RegionPreview document loading

LoadRegionEvidence reads recipe/source through the repository Object reader
interface with exact object versions, nonempty keys, JSON media type, bounded
2..1MiB declared length and matching reference SHA metadata. Reads are bounded
to declared length plus one byte, verify exact EOF size and SHA256, close streams
on success/failure, and check cancellation before IO, between reads and after
compilation. It then invokes the shared immutable recipe/source compilation.

Tests verify exact version metadata reaches Open, valid bytes pass, an extra
byte rejects, streams close, unversioned metadata rejects before IO and cancelled
requests do not open objects. Go v2region tests passed. Caller-supplied metadata
still requires independent SQL authority/readiness proof; this loader does not
establish access or create tasks. SQL lookup, HTTP route and worker remain open.

### Authorized region original-source SQL lookup (migration 365)

v26_region_source binds tenant/asset/exact asset-version and actor/session through
the existing v2_editor_authorized gate. It returns only the accepted version's
master ref with matching content hash, undeleted asset, ready verified JSON
artifact, bounded document bytes, concrete object version and unexpired metadata.
Only photo_api and photo_scheduler can call it; worker/public execution is not
granted. No direct table grants were added. This source gate is distinct from
FrozenRecipe project authority, which still requires its own SQL proof.

An isolated PostgreSQL test passed for grants and unavailable scope under both
allowed roles. Documentation validation passed (2076 static, 54 semantic). The
existing real SQL/MinIO composition fixture was extended to call the new lookup
under photo_api, compare exact master ID/SHA, and reject substituted tenant,
asset, version, actor and session. Migration 365 is not deployed to a running
service; HTTP scheduling/recipe authority/worker rendering remain unfinished.
The positive source/identity-substitution SQL/MinIO fixture passed: 1 test,
2 deselected, 10.94s.

### Go adapter for authorized RegionPreview source metadata

ReadRegionSource invokes only v26_region_source with the exact tenant/asset/
version/actor/session tuple. It checks cancellation and nonnil identity fields
before querying, rejects missing rows, validates returned project/ref/SHA and
exact configured bucket, JSON media, bounded bytes and concrete object version,
then supplies the reference SHA to the pinned object reader metadata. The adapter
accepts the standard pgx QueryRow interface so a caller may use its chosen DB
connection/transaction without adding table reads.

Go tests verify scope argument preservation, accepted pinned metadata, foreign
bucket rejection and no query for invalid identity. v2region tests passed. The
SQL lookup itself was verified in the preceding isolated database fixture; this
adapter test uses a controlled row. FrozenRecipe authority and actual service/
HTTP job creation still remain unfinished.

### Trusted published FrozenRecipe lookup foundation (migration 366)

v26_region_recipe first requires the authorized exact original-source lookup,
then resolves a ready FrozenRecipe from the current trusted V23 backend-document
publisher in a sealed, uncancelled learning workflow belonging to that source's
project. Requested ID/SHA, backend-document state/version and ready artifact
state/version/media/size/verification/expiry must agree. Only API/scheduler can
call it; no raw table access was granted. Immutable recipe source binding still
must be checked by LoadRegionEvidence after byte verification.

Both RegionPreview lookup integration tests passed against an isolated database
for narrow role grants and absent/unauthorized scope. Documentation checks passed
(2076 static, 54 semantic). A populated positive recipe fixture has not yet been
verified. This covers the existing V23 FrozenRecipe publication path only; V26
editor recipe freeze/publication must be implemented and admitted explicitly
before arbitrary editor recipes can be used. No complete recipe-authority or
HTTP preview availability is claimed, and migration 366 is not deployed.

### RegionPreview preparation service joins both metadata gates and byte proof

v2region.Service.Prepare validates the request before querying, reads source and
recipe only through the narrow SQL lookup functions, requires identical project
and exact requested recipe ref, and runs pinned bounded object loading plus
FrozenRecipe/source/canvas checks. It returns verified metadata/evidence for the
future job compiler without creating a run or implying task admission. Shared
metadata decoding enforces configured bucket and bounded ready-object shape.

A service test joins controlled metadata rows with actual canonical recipe/source
bytes, proves both objects are read/closed and expected geometry is produced,
and rejects a recipe from another project before object IO. Go v2region tests
passed. SQL metadata tests and object-reader tests remain separately scoped;
a populated SQL recipe positive and commit-time authority recheck still need
integration. No HTTP endpoint/task execution is claimed ready.

### Populated RegionPreview recipe metadata fixture

The region SQL tests now seed an accepted source, sealed learning workflow and
ready backend FrozenRecipe document metadata. Under photo_api, lookup returns the
exact project/recipe ID/SHA and rejects substitutions of all seven scope/ref
arguments. Additional cases cover recipe expiry, missing verification, run
cancellation and session revocation. Artifact version mutation is expected to
raise the existing immutable-field constraint; guards are not disabled.

This fixture inserts trusted publication metadata directly and hashes a recipe
JSON fixture. It does not exercise the V23 publisher or MinIO bytes, and its
source canvas hash is fixture metadata. Thus it proves the SQL relational gate,
not complete recipe publication or byte/SQL service integration. The first run
confirmed positive/substitution/expiry checks but failed when the test attempted
an immutable version mutation; the test now checks that rejection explicitly.
Corrected isolated PostgreSQL run passed all three region lookup tests (42.31s).

### V26 RegionPreview frozen preparation plan

Service.Prepare now returns defensive canonical plan bytes and their SHA256.
The plan binds tenant/project/asset, the complete validated request, exact source
and recipe artifact IDs/SHA/object keys/versions/bucket/lengths, decoded frozen
recipe runtime locks, computed integer geometry and explicit
full_canvas_then_crop_then_downscale render order. Actor/session are deliberately
excluded from the content digest; live authority must be checked at admission.
JSON access returns a copy so consumers cannot mutate the stored plan bytes.

Tests prove repeatability, byte-copy isolation and digest changes for tenant,
ROI, output edge, source version and recipe version changes. All backend
internal Go tests passed. Documentation verification passed 2076 static checks
and 54 semantic tests with deterministic regeneration.

This is preparation evidence only. It is not a registered worker operation,
complete nested artifact closure, transactionally admitted job, HTTP endpoint,
render implementation or output adoption. Those remain required before the
RegionPreview feature can be considered implemented end to end. No GPU quality
qualification or migration rollout was performed in this increment.

### V26 RegionPreview explicit input requirements

Frozen preparation plans now include a UUID-sorted input inventory derived from
validated source and FrozenRecipe bytes: WorkingImage, FrozenRecipe, StyleV2,
RGB NPY, optional alpha NPY and each inherited dense-warp inverse field. Duplicate
identical references are collapsed; a UUID with conflicting digest or contract
is rejected. The inventory is part of the canonical plan digest, and the public
accessor returns a copy. Arbitrary nested JSON is not recursively scanned to
invent worker permissions.

Tests construct a byte-bound source with alpha and a valid dense-warp chain,
then verify both binary inputs and the style root are retained. They also cover
ambiguous UUID contracts and defensive-copy isolation. All backend internal Go
tests passed; after aligning the style contract name with the existing StyleV2
transport, the region package passed again.

This is a required-input declaration, not an admitted InputClosure. Base-style
nested references still need the scoped resolver; binary objects still need
catalog authorization, exact version pins, and transactional admission checks.
No RegionPreview worker operation or public job endpoint is active yet.

### V26 scoped source/style resolver integration

ClosureResolver.ResolveRegionInputs now traverses the byte-verified source and
base StyleV2 through the existing contract/parent-dependent catalog, including
style child artifacts and inherited image binaries. It shares inventory
normalization with ResolveTaskInputs. FrozenRegionPlan.ResolveInputs supplies
scope/binding from its canonical bytes, merges the separately authorized recipe
root and requires exact coverage of its declared source/style/binary inputs.
It rejects empty plans, missing required refs and conflicting inventory entries.

Tests verify source/style traversal with canonical JSON and catalog metadata,
parent provenance on a nested LUT, missing LUT rejection, altered style-byte
rejection, frozen tenant propagation and required-input coverage. All backend
internal Go tests passed. This uses controlled catalog/object readers, not a
live PostgreSQL/MinIO admission test. Style child content is reached through the
existing generic Artifact catalog gate; this increment does not add full StyleV2
schema validation, persistent closure publication, version snapshot recording,
SQL graph admission, an operation registry entry or the region endpoint.

### V26 effective style bounds during region resolution

ResolveRegionInputs now reads the scoped, version-pinned StyleV2 JSON and uses
existing DecodeStyleBaseline plus CheckStyleBounds before expanding the worker
inventory. FrozenRegionPlan passes effective_delta from its immutable recipe
bytes to that check. Unsupported schema/renderer versions, invalid baseline
scalar ranges and out-of-range effective exposure/gains/saturation/smoothing
are rejected without silently clamping. This preserves the existing correction
formula and rendering bounds.

The resolver test uses a hash-bound style scalar fixture and verifies an EV
correction that would exceed the gain range is rejected. Region and draft
package tests passed, followed by all backend internal Go tests. Full StyleV2
module schema validation is not provided by DecodeStyleBaseline; the style
fixture tests scalar validation and dependency traversal, not every color LUT,
curve, beauty, geometry or generation invariant. Complete contract validation
and real region job publication/admission/rendering remain required.

### V26 immutable Params publication and retryable input transport

FrozenRegionPlan.PublishParams now adapts the existing draft-upload publisher:
identity is required, tenant/version are taken from frozen bytes, document UUID
is deterministic for the region task, and the returned digest must match the
plan. Changed content cannot overwrite a task's initial document through the
publisher's existing conflict semantics. No new SQL grants were introduced.

FreezeInputs validates Params content identity and required inventory coverage,
rejects duplicate/conflicting UUIDs, snapshots sorted entries, and exposes fresh
cursors for retryable v2input.BuildInputClosure publication. The task ID, plan
SHA and region.preview operation/version are bound into the input digest. Only
the Params reference is a direct transport handle.

Tests invoke the actual closure builder twice with fresh cursors after mutating
the caller's input slice; each publishes one chunk and manifest with five input
entries. Cases cover task-dependent digests, mismatched Params SHA, duplicates,
missing required refs, cancellation, stable Params document ID, frozen scope
and publisher digest substitution. All backend internal Go tests passed for the
transport changes; after the publication adapter was added, the region package
passed again. Publishers in these tests are controlled implementations. No live
Params publication, catalog version snapshot, region SQL graph admission,
registered worker operation, rendering or public endpoint was exercised here.

### V26 live Params publication through photo_api and versioned MinIO

A Go integration driver now invokes FrozenRegionPlan.PublishParams with the
real v2draft.Publisher under photo_api against an isolated migrated PostgreSQL
database and versioned MinIO. The driver proves publication replay returns the
same artifact reference and a changed max_edge/geometry for the same task fails
specifically with REVISION_CONFLICT. The Python harness reads the actual pinned
object version, checks byte length/SHA, frozen tenant, original edge/render order
and four input declarations. It verifies exactly one ready upload, identical
artifact/upload version pins and one storage charge with no leftover reserved
bytes. The isolated test passed (4.97s); after strengthening the conflict error
assertion, it passed again (5.74s). The region Go package also passed with the
integration test correctly skipped without its required isolated fixtures.

This proves real Params upload and replay semantics only. Source/recipe refs and
object pins inside the prepared plan are synthetic test evidence, not objects
published/read through RegionPreview.Prepare. Full live source/recipe preparation,
input closure publication, graph admission, worker rendering and endpoint/client
integration remain incomplete. No production migration rollout or GPU benchmark
was performed.

### V26 live source/recipe preparation followed by Params publication

The integration driver now optionally calls the real Service.Prepare under
photo_api instead of constructing a plan from synthetic object pins. A new
isolated fixture stores canonical WorkingImage and FrozenRecipe JSON in versioned
MinIO, seeds accepted source and trusted backend-document publication metadata
in PostgreSQL, then replaces the latest versions with corrupt JSON. Preparation
successfully reads the exact older SQL-pinned versions, validates byte hashes,
source/version/canvas binding and returns the expected project. A foreign source
version is rejected. The resulting Params document is read back from MinIO and
its source/recipe version pins and 1024x512 geometry are checked.

The initial run failed because direct fixture uploads omitted SHA256 user
metadata required by objectstore.Open. The fixture now supplies that metadata;
production integrity verification was preserved. Corrected run passed both live
region tests (13.95s), including existing replay/content-conflict verification.

The actual WorkingImage/recipe JSON and Params publication are exercised. The
fixture seeds trusted recipe publication metadata directly, so it does not
exercise the V23 recipe publisher or a new V26 editor freezer. RGB and base-style
refs are declared synthetic dependencies and are not decoded/resolved in this
fixture. Graph admission, closure publication, pixel rendering, adoption and
endpoint/client behavior still require implementation and end-to-end evidence.

### V26 independent worker RegionPreview plan contract

Added strict Python RegionPreviewParams matching the canonical Go plan shape.
Worker validation requires nonzero scope IDs, sorted unique typed inputs, exact
source/recipe/version bindings, FrozenRecipe content digest, matching object
buckets, real version pins and the full-canvas render order. validate_source
checks original canonical JSON bytes (not reserialized model defaults), source
SHA/canvas binding, independently recomputed floor/ceil crop geometry and output
scaling, plus required pixels/alpha/inverse-field refs before binary reads.

Thirteen platform tests passed, covering geometry/no-upscale, invalid request
bounds, unversioned objects, substituted recipe, wrong bucket/render order,
duplicate/unsorted inputs, zero tenant, source/pixel substitution and ambiguous
or noncanonical JSON. Initial unit failure came from a fixture alias: changing
its inventory also changed its source pixels; the fixture now copies that ref.
The two real PostgreSQL/MinIO publication tests now parse the actual Go plan
through the Python contract and validate pinned source bytes. They passed after
the raw-byte validation update (7.93s; earlier contract run 8.23s).

This is independent transport/input validation, not an operation registration
or renderer. Pixel loading, resource admission, full recipe rendering, output
encoding/adoption, job admission and public endpoint/client integration remain
incomplete. Existing quality operators were not modified in this increment.

### V26 post-render region pixel extraction

Added extract_rendered_region for a fully rendered canonical canvas. It validates
source plan bytes and geometry, requires the full source dimensions and float32
unit RGB/alpha, scans the entire canvas in 512px tiles with cancellation checks,
then crops and uses the existing export area/premultiplied resize. Returned
arrays own storage and remain bounded by the frozen 1024px edge. The shared RGBA
resizer now additionally supports linear working encoding; existing gamma24 and
sRGB branches were preserved.

Region contract/extraction and existing export/alpha resize tests passed together
(32 tests). After adding an outside-ROI invalid-pixel case, extraction plus
existing export/alpha resize tests passed (20 tests). Coverage includes exact
resize equivalence, detached output even without resizing, partial canvas and
invalid dtype/range rejection, cancellation, and linear alpha visible-color
preservation. No image quality operators were replaced.

This is downstream extraction, not a full recipe renderer or operation adapter.
Caller must run the complete frozen recipe through the qualified pipeline before
invocation. Source reading, full-canvas scratch, alpha resize temporaries and
render resource admission still need integration; output size alone does not
bound all peak RAM. ICC conversion/encoding, output adoption, registry, graph
admission and public endpoint/client remain incomplete.

### V26 ICC-managed region presentation and encoded output

Added prepare_region_presentation/write_region_presentation after full-canvas
extraction. The installed profiles.lock.json byte SHA must match the frozen ICC
lock; profiles are re-read and verified per presentation and lock bytes are
rechecked after resolution. Source ICC SHA must match WorkingImage. Conversion
uses the existing RGB16 LittleCMS path into verified sRGB, preserving separate
alpha. The cache key binds the full plan, target ICC SHA, JPEG/WebP selection,
quality 88, resize policy and stripped metadata policy.

Region encoding uses the existing derivative writer with an opt-in ICC-only
metadata mode. Initial encoded-output verification exposed libvips-generated
EXIF (186 bytes) despite array-only inputs. The opt-in mode now saves keep=icc;
previous writer callers keep their existing defaults and quality settings.
Outputs are checked for dimensions, RGB/RGBA mode, exact embedded ICC hash,
absence of EXIF/XMP, decodability, suffix and cancellation.

Forty-eight region/ICC/media/export/alpha tests passed. Real LittleCMS conversion
matches the existing path exactly; JPEG and alpha WebP retain the expected ICC
and dimensions while metadata is removed. Tests ran with libvips 8.18.6. The
future qualified region runtime must support libvips keep=icc and verify LCMS
binaries; deployment-image compatibility/qualification was not proven here.

These are presentation helpers, not an admitted worker operation. They consume
already completely rendered RGB and do not implement or prove full recipe
rendering. Resource/scratch admission, operation registry, complete renderer,
output manifest/adoption, region jobs and endpoint/client integration remain.

### V26 bound region derivative manifest and encoded-byte verification

Added RegionDerivativeManifest, build_region_manifest and verify_region_encoded.
The strict manifest binds Params SHA, source/recipe refs, asset version, rect,
integer geometry, derivative ref/length, target ICC, cache key, JPEG/WebP alpha
policy, quality and stripped metadata. Cache-key calculation is shared with
presentation. Validation requires independent expected target ICC/alpha and
exact agreement with frozen Params. Encoded-byte proof checks bounded declared
length/SHA, actual format/dimensions/mode, embedded ICC and absent EXIF/XMP before
image verification. Reusing a source/recipe UUID as derivative is forbidden.

Fifty-one region/ICC/media/export/alpha tests passed. Real JPEG and alpha WebP
outputs round-trip the strict manifest and pass byte checks. Added rejection
cases cover changed bytes, untrusted target ICC, altered geometry and actual
JPEG containing automatically generated EXIF. This does not prove that the
pixels were rendered by the complete recipe; source/render quality evidence
and catalog/version/attempt scope must be supplied by the adoption coordinator.

This verification is not yet registered with the V2 adoption dispatch or SQL
output policies. Output staging, complete renderer, resource admission, worker
operation, job graph admission and endpoint/client integration remain required.

### V26 region derivative and manifest staging

Added stage_region_result using the existing attempt artifact interface and real
ScratchSpace reservations. It validates frozen source/geometry, presentation
shape/dtype/range, alpha presence, ICC bytes and cache binding before encoding or
upload. JPEG/WebP files are written and checked in reserved scratch, uploaded as
derivative, independently verified against the returned reference, then paired
with a strict derivative_manifest whose upload SHA is checked. Output byte
accounting includes both actual file bytes and canonical manifest bytes.

Fifty-six region/ICC/media/export tests passed. Staging tests use real scratch
files, encoders and manifest verification with a controlled upload interface.
They cover opaque JPEG, alpha WebP, byte accounting/cleanup, rejection before
upload for invalid pixels, upload digest substitution and cancellation after
first upload without emitting a manifest or successful result. Aborted staged
artifacts still need the existing attempt cleanup lifecycle; this helper does
not claim remote rollback.

HTTP gateway/MinIO output uploads and SQL attempt/output-policy fences were not
exercised in these tests. Region operation registration, qualified complete
recipe rendering, scratch/RAM graph admission, adoption dispatch, public job
endpoint and client integration remain required before feature completion.

### V26 ICC runtime-pin compatibility correction

Inspection of the existing FrozenRecipe producer and pinned holdout provider
showed that ICCLockSHA is a runtime-supplied pin: current editor approval uses the
source ICC SHA, and PinnedColorHoldoutProvider uses the working-profile SHA. The
region presentation helper's earlier assumption that it always equals the byte
hash of profiles.lock.json would reject existing valid recipes. Removed that
assumption. prepare_region_presentation now requires runtime_icc_lock_sha from
the qualified caller and compares it with the frozen recipe. Source profile
identity and installed profile bytes remain independently verified, and registry
manifest changes during resolution are still rejected.

Presentation fixtures now use the existing working-profile pin convention. A
regression test explicitly proves that this valid pin differs from the manifest
file SHA, succeeds with the runtime pin and rejects substitution of the manifest
file SHA. Fifty-seven region/ICC/media/export tests passed. This supersedes the
previous runtime-verification statement that ICCLockSHA must be profiles.lock
byte SHA. No existing recipe or database metadata was migrated or reinterpreted.

The complete renderer/provider and its runtime qualification are still pending;
this API requires the caller to supply a verified runtime pin, not copy the
untrusted task's expected value to bypass compatibility checking. Job admission,
operation registration, adoption integration and client endpoint remain pending.

### V26 worker orchestration with mandatory renderer/admission

Added region adapter execute/region_operations. It requires explicit relational
admission and a complete qualified renderer provider. Profile/backend/renderer/
model/ICC pins are checked before Params IO; installed runtime verification is
called per task. Admission and resource checks precede source/style reads. The
adapter checks the separately loaded FrozenRecipe against Params, reads bounded
source bytes with short-read/cancellation handling, validates source/geometry,
uses existing full StyleV2 parsing/materialize_frozen_style, obtains a full-canvas
render from the provider context, then prepares sRGB presentation and stages both
outputs. Full-canvas resources are released before output staging.

Sixty region/ICC/media/export tests passed. Adapter tests exercise real
presentation/encoding/scratch/staging with controlled admission, artifact IO and
a constant full-frame renderer. They prove sequencing, denial before source IO,
missing-provider failure and runtime profile rejection before Params IO. They do
not qualify the renderer or prove image effects: the test provider is explicitly
synthetic and cannot serve as production quality evidence.

The factory is not added to the default worker registry. Concrete relational
RegionAdmission, qualified complete renderer, compiled resource/runtime profile,
SQL operation/output policies and graph admission remain required. The adapter
must not be activated with a renderer that silently substitutes the color-only
holdout path for effect styles. Public endpoint/client and adoption dispatch also
remain incomplete; no claim of full V26 completion is made.

### V26 complete-renderer module coverage gate

Existing Apply effect adapters require typed landmarks, masks, resolved modules
and model evidence beyond the color-only holdout path. Region adapter now
requires a renderer supported_modules frozenset and compares it with every stage
requested by the fully materialized StyleV2: color/strength, adaptive/local,
beauty/defects/geometry, enabled extension stages, masking/restoration/background
and region adjustments. Zero strength does not waive module coverage. Missing
coverage rejects before provider.render and output staging.

A regression test supplies a valid beauty StyleV2 with detector model declarations
to a color-only provider and asserts the specific
REGION_COMPLETE_RENDERER_REQUIRED cause, no render invocation and no outputs.
Sixty-one region/ICC/media/export tests passed. This declaration gate prevents
silent omission but does not prove a provider correctly implements its claimed
modules. A qualified concrete complete renderer, evidence-input planning,
resource admission, relational admission and operation activation remain pending.
Legacy src/photoeditor/render.py and Apply quality algorithms were inspected
without changes; no incompatible legacy config-to-StyleV2 mapping was invented.

### V26 operation catalog and normative worker wire schemas

Forward-only migration367 registers region.preview@1 as GPU/CUDA with 1800s
limit and two bounded output policies: document JSON<=8388608 (one manifest),
derivative JPEG/WebP<=16777216 (one encoded image). v2_enabled remains false.
The worker factory enforces CUDA for this registered operation; private CPU
adapter fixtures remain orchestration tests only.

The normative generator now includes RegionGeometry, RegionObjectPin,
RegionInput, RegionPreviewParams and RegionDerivativeManifest, plus region.preview
role policies. RegionEncodedDerivative is an explicitly documented binary
contract with frozen dimensions/ICC/alpha/metadata bounds. Generator outputs,
operation table and entrypoint index were regenerated. Manifest staging uses
standard V2 envelope role document; this supersedes earlier derivative_manifest
role references in region staging evidence. Existing media operations retain
that original role.

Documentation verification passed 2113 static checks and 54 semantic tests,
170 schemas/51 endpoints/27 operations/78 entrypoints, with deterministic
regeneration. Isolated PostgreSQL migration/catalog test passed (2.56s), proving
CUDA route, disabled activation and exact output policies. Sixty-one region/ICC/
media/export platform tests passed after role alignment. No production rollout,
qualification or runnable region activation was performed. Complete renderer,
relational admission, graph creation, output adoption and endpoint/client still
remain required.

### V26 generic adoption dispatch and encoded evidence reader

prepare_v2_adoption now dispatches region.preview to verify_region_result and
binds RegionDerivativeManifest.derivative to the registered derivative role.
The proof requires exact input Params and FrozenRecipe, byte-bound source JSON,
output scope/role/media/length, and separately supplied trusted target ICC SHA.
Encoded bytes are read through the reader's pinned iter_bytes API with bounded
blocks/length, cancellation and iterator cleanup, then verified against the
manifest and geometry. Generic closure preparation still rejects unreachable
or role-policy-invalid staged artifacts. Missing trusted ICC/Params fails closed.

Seventy-four combined region, generic adoption, ICC/media/export tests passed.
Region adoption tests use actual encoded JPEG bytes and canonical Params/source/
recipe/manifest with controlled evidence rows. They prove generic dispatch,
required Params/ICC, extra bytes and wrong output role rejection. No SQL commit,
MinIO evidence-reader integration or full recipe pixel provenance is established
by these unit fixtures. AdoptionCoordinator still needs a trusted runtime ICC
provider plus RegionPreview relational generation/admission checks before this
operation can execute and commit; registration remains disabled.

### V26 coordinator trusted ICC and generation-bound Params lookup

Forward-only migration368 adds scheduler-only v26_region_adoption_params. It
requires a succeeded uncancelled region.preview@1 task with exact attempt/fence/
canonical result, sealed live V2 run/workflow, ready verified version-pinned JSON
Params, unexpired artifact, complete input closure, current adoption context and
an exact typed Params input row. It grants no API/worker execution or table reads.

AdoptionCoordinator now accepts optional trusted region_target_icc_sha deployment
configuration, validates its lowercase SHA256 shape and requires it for region
results. It reads Params with exact tenant/task/attempt/fence/result arguments
before calling generic adoption proof and forwards the trusted ICC independently
from output JSON. Missing pin or stale Params reject before write transactions.

Sixteen region/generic adoption tests passed. Coordinator fixtures assert exact
SQL arguments and forwarded pins, missing-pin rejection before Params lookup,
and stale-generation rejection before proof. They stop preparation explicitly,
so no synthetic successful SQL commit is claimed. Two isolated PostgreSQL region
catalog tests passed (4.40s), covering migration application, denied API/worker
roles and random-generation lookup rejection. Documentation verification passed
2113 static/54 semantic checks with deterministic regeneration.

A populated region result-generation SQL fixture, region-specific commit locking
and live source/recipe/session rechecks remain required. The default catalog is
still disabled and no qualified complete renderer or region graph is activated.
Runtime ICC configuration must be derived from verified installed profiles;
passing task/output claims as configuration would not establish that trust.

### V26 locked commit-scope helper

Forward-only migration369 adds scheduler-only v26_region_locked_scope. Given
frozen Params and actor/session, it locks user/session/membership/project/ACL,
asset/version, sorted source/recipe artifact rows and recipe backend-document/run
rows. It then repeats live source/recipe authority gates and requires exact
project/ref/SHA/bucket/key/version/length agreement. Frozen recipe source and
request recipe/version relationships are checked. Caller must already hold the
normal tenant -> run -> task locks and retain this transaction through commit.
This is scope proof only, not complete Params schema validation or adoption.

The first populated test failed because FOR SHARE of v2_workflows requires
UPDATE privilege absent from photo_fn_owner. Workflow rows have the existing
v2_workflows_immutable trigger; removed that unnecessary lock, preserving the
recipe run/document and other mutable authority locks without issuing grants.
Corrected isolated PostgreSQL suite passed all six region source/catalog tests
(12.92s). Added cases prove positive metadata scope, substituted source/recipe
object-version rejection and revoked-session rejection. API/worker cannot call
the locking helper. Documentation passed 2113 static and 54 semantic checks.

These fixtures seed publication metadata and use partial Params relationships
for SQL scope checks; they do not exercise immutable full-plan admission or
concurrent independent-connection lock races. A region-specific adoption wrapper
must bind this helper to exact task generation and frozen run config before
publication. Full renderer, graph admission, activation and endpoint/client still
remain incomplete. No production migration rollout occurred.

### V26 immutable-node commit wrapper (migration 370)

Forward-only migration370 wraps adopt_v2_plan and revokes scheduler access to
its renamed predecessor. Region commit requires enabled catalog, exact task
generation, sealed run, a unique frozen v26_region node, matching input digest,
Params artifact and raw JSON SHA, execution profile, recipe runtime pins and
runtime fingerprint. It repeats locked source/recipe/session authority and base
StyleV2 scope before delegating to the existing adoption chain. Non-region
operations retain delegation to the predecessor.

Isolated PostgreSQL source/catalog suite passed seven tests (16.88s), including
wrapper role grants, predecessor bypass denial and unknown-tenant rejection.
Go v2region/v2draft/v2input suites passed. Documentation regeneration passed
2113 static and 54 semantic checks. The initial sandboxed SQL invocation could
not connect to localhost; the authorized isolated invocation above passed.

These checks do not prove successful region commit, populated immutable node
validation, non-region populated delegation or concurrency. Frozen node compiler,
style authority locking, snapshot membership, worker admission/resource guards,
complete renderer and endpoint/client remain required before activation. Catalog
remains disabled. No production migration rollout occurred.

### V26 frozen node compiler

FrozenRegionPlan.CompileNode now builds the immutable node consumed by the
migration370 commit wrapper. It hashes the exact Params bytes, freezes the
resolved input inventory through FreezeInputs, preserves the canonical runtime
profile and binds task ID, Params reference and input digest. Renderer/backend
and ICC/model pins must match the recipe; runtime SHA must be lowercase hex.
Input-transport decoration is rejected in the undecorated profile. ConfigJSON
creates the single-node v26_region member with an explicit nonzero auth session.
JSON/direct-ref getters return defensive copies.

Go v2region/v2draft/v2input suites passed. New tests cover exact node/config
bindings, preserved resource pins, defensive copies, missing session, profile
pin substitution, all four transport keys, duplicate profile keys and modified
Params bytes. Runtime qualification and resource limits remain the responsibility
of a trusted server profile provider; this compiler does not accept HTTP inputs
directly and does not itself prove renderer qualification or SQL authority.

Run creation/sealing is not yet connected to this compiler. Populated SQL commit
fixtures, style authority locks, snapshot membership, worker admission, complete
renderer and endpoint/client remain required before region activation.

### V26 preparation orchestration

FrozenRegionPlan.PrepareNode now performs runtime-profile preflight before
external providers, resolves scoped descendants, validates the resolved node
before upload, publishes immutable Params and compiles the node with the actual
returned artifact reference. Profile bytes are frozen before resolver callbacks.
Missing identity/dependencies, cancelled contexts, invalid runtime and incomplete
input inventories stop preparation without publishing Params.

Go v2region/v2draft/v2input suites passed; the focused preparation test passed
20 repetitions with randomized artifact IDs. Its missing-input case explicitly
removes WorkingImage rather than relying on random UUID sort order. These are
controlled-provider tests, not transactional admission or renderer qualification.
Job creation/sealing and successful SQL adoption remain to be connected and
verified. Region stays disabled.

### V26 base-style commit authority (migration 371)

Forward-only migration371 replaces v26_region_locked_scope in place. It locks
matching StyleV2 style/version rows in deterministic order and includes the
exported style artifact in sorted artifact locks. It repeats the existing
v2_draft_artifact_scope after acquiring locks, requiring the exact accepted
renderer/schema version and unarchived project-scoped style. Existing function
owner privileges suffice; no new table grants were issued.

The first SQL run exposed a PL/pgSQL record/alias collision, corrected by using
distinct style aliases. The second rejected the fixture's missing app.tenant_id;
the fixture now sets the same transaction-local tenant context required by the
existing scope gate. No production authorization check was weakened.
Corrected isolated PostgreSQL suite passed seven tests (17.06s), including
positive style scope and archived-style rejection. Documentation passed 2113
static and 54 semantic checks with deterministic regeneration.

The fixture seeds metadata and checks sequential authority changes. Independent
connection races, successful populated region adoption and nested style-child
authority remain unproven. Full renderer, transactional admission, endpoint and
client remain incomplete; operation stays disabled. No production rollout.

### V26 independent-connection style authority races

Extracted a shared region metadata fixture and committed it in an isolated test
database so separate reader/writer connections see the same authority rows.
After scheduler scope proof, writer updates of style.archived_at,
style_versions.export_artifact_id and the style artifact expiry each raise
LockNotAvailable under a 150ms lock timeout. After reader transaction release,
archive succeeds. In the reverse ordering an uncommitted archive blocks the
scope reader, and after archive commit the next scope proof returns false.
These checks exercise actual PostgreSQL row locks in both directions without
replacing database functions or disabling integrity guards.

The region source/catalog suite passed eight tests (19.64s). After adding the
reverse ordering, the focused concurrency test passed (2.87s). Documentation
passed 2113 static and 54 semantic checks. This proves base-style scope locks,
not end-to-end task adoption, nested style descendants, runtime qualification
or renderer quality. Transactional graph admission and full rendering remain
incomplete; region remains disabled.

### V26 adapter to draft-shell admission

FrozenRegionNode.DraftShell now produces the existing V2 DraftShell and frozen
TaskSource for the single region.preview node. Run/workflow/task IDs derive
from tenant, actor and idempotency key; a node compiled for another run is
rejected. Canonical config includes project, input snapshot and the immutable
v26_region member with session. Params, input digest and undecorated execution
profile bind the task shell; workflow is preview and resource class GPU.
Reservation must be positive and within interoperable integer bounds.

Go v2region/v2input tests passed, including stable identities, exact shell
scope/Params and canonical config hash, changed-key rejection and defensive
copies. This adapter does not create a snapshot or prove snapshot membership,
transactional scope or runtime qualification. The SQL admission store still
needs region-specific live checks before this shell can be used by an endpoint.
No default route activation or production rollout occurred.

### V26 frozen snapshot commit authority (migration 372)

Migration372 adds v26_region_snapshot_scope and invokes it from the region
commit wrapper. Snapshot must be tenant-scoped V2, owned by the run actor,
frozen (expires_at NULL), declare exactly one item and contain exactly one row
matching Params source version/artifact/SHA. Snapshot and item rows are locked.
API/scheduler may call this helper; worker/public cannot. No new table grants.

Initial fixture insertion failed the existing V22_FROZEN_ITEMS_IMMUTABLE guard.
The fixture now builds a draft snapshot with expiry and freezes it only after
inserting the item. The helper rejects draft snapshots: locking current rows
alone would not prevent insertion of another item into mutable snapshots.
Existing guards remain enabled and reject additions to the frozen snapshot.

Isolated region PostgreSQL suite passed nine tests (26.19s), including missing
item, mutable snapshot, positive exact source, wrong principal/snapshot/version/
artifact and frozen-item insertion rejection. Documentation passed 2113 static
and 54 semantic checks with deterministic regeneration. This does not prove
successful populated task adoption or snapshot creation through the endpoint.
Admission integration, complete rendering and endpoint/client remain incomplete;
region stays disabled. No production migration rollout occurred.

### V26 transactional draft authority entry point (migration 373)

Migration373 adds API-only v26_region_admission_scope. It checks transaction
tenant context and enabled catalog, locks the active tenant, then verifies
frozen snapshot and locked source/recipe/style/session scope. FrozenRegionNode.
BeginInTransaction derives the shell, calls this gate and delegates idempotency
binding/draft creation to BeginAdmissionInTransaction within the caller's TX.
There is no object IO inside this entry point; caller owns rollback and commit.

Go v2region/v2input suites passed. Isolated PostgreSQL region suite passed nine
tests (25.51s). Authority tests prove API-only grants, disabled-catalog rejection,
positive scope after an explicitly rolled-back fixture-only catalog toggle and
revoked-session rejection. That toggle is not renderer qualification or actual
activation. Documentation passed 2113 static and 54 semantic checks.

Successful draft creation through the new Go entry point, second-phase sealing,
immutable replay handling, installed runtime/resource qualification, full
renderer and endpoint/client remain unverified or incomplete. Generic admission
paths are not replaced by this entry point. No production rollout occurred;
default region catalog remains disabled.

### V26 second-phase graph scope (migration 374)

Migration374 adds API-only v26_region_seal_scope. It locks active tenant/run/task
in order, binds run actor/session and preview workflow, requires exactly one
stored node equal to the supplied frozen node, and verifies task operation,
Params bytes/ref/SHA, input digest, undecorated execution profile and project.
It then repeats the admission scope helper for frozen snapshot and live authority.
FrozenRegionNode.SealInTransaction uses this gate before the existing atomic
input registration and graph sealing protocol. Caller retains TX and rollback
responsibility; the closure must already be published outside the transaction.

Go v2region/v2input suites passed. Controlled transaction tests prove denial
does not register inputs and cancelled requests perform no SQL. Isolated
PostgreSQL region suite passed ten tests (26.34s), including API-only seal-gate
permissions and unknown-run rejection. Documentation passed 2113 static and
54 semantic checks. These checks do not prove successful populated sealing or
adoption. Complete runtime qualification, full renderer, endpoint/client and
successful two-phase live integration remain outstanding. Default catalog stays
disabled; no production rollout occurred.

### V26 populated seal authority and runtime pins (migration 375)

Added a populated SQL preview workflow/run/task fixture with immutable config,
Params byte hash and a frozen single-source snapshot. The second-phase scope
helper succeeds only after a fixture-only rolled-back catalog toggle. Tests
reject changed task ID, input digest, Params bytes, profile, actor/session and
live session revocation, style archive, run cancellation and task cancellation.
These are seeded metadata relationships, not a fully schema-valid rendered job.

Migration375 strengthens the same helper with GPU resource class, CUDA backend,
renderer v2.2, lowercase runtime/ICC/model digests and ICC/model equality with
FrozenRecipe. This closes a difference between commit checks and pre-seal checks;
it does not qualify installed models or resources. Isolated PostgreSQL suite
passed eleven tests (26.76s), including populated positive scope and rejection
cases. Documentation passed 2113 static and 54 semantic checks.

Successful creation/sealing through Go entry points with actual published input
closure, renderer execution and adoption remain unproven. Region remains
disabled; no production migration rollout occurred.

### V26 generation-scoped worker admission (migration 376)

Added worker-only admit_v26_region_preview, TaskDatabase transport method and
SQLRegionAdmission adapter. SQL binds session_user registration, exact task
attempt/fence/worker, active run, unexpired lease/deadline, authorized GPU slot,
ready input closure, exact Params JSON/ref/SHA/digest and immutable region node.
It repeats frozen snapshot/source/recipe/style/session authority via seal scope
and rechecks lease/deadline after potentially waiting for locks. Worker tenant
context is set locally only after registration and generation proof.

Python admission rejects wrong operation/resource/backend/tenant and Params
byte digest before DB. Nine admission/adapter tests passed (1.16s); initial test
fixtures used strict Python validation with UUID strings and were corrected to
use actual JSON decoding. Six final registry SQL tests passed (13.04s), proving
role grants and unknown-generation rejection. Earlier seven registry/populated
seal tests passed before the final lease-after-lock recheck. Documentation passed
2113 static and 54 semantic checks.

Positive populated running-worker admission, slot expiration/cancellation races,
database transport integration, full renderer qualification and endpoint/client
remain unproven or incomplete. Adapter is not installed in the default operation
registry; catalog remains disabled. No production migration rollout occurred.

### V26 worker registration authority stability (migration 377)

Migration377 keeps worker registration stable after tenant/run/task locks and
through GPU-slot, snapshot and source/style authority checks. It locks the exact
registration row and repeats enabled/session_user checks after acquisition;
otherwise concurrent revocation during a later authority wait could invalidate
the initial registration read. Existing table privileges suffice.

Isolated registry/populated seal PostgreSQL suites passed seven tests (15.14s).
Python transport tests additionally verify all ten SQL arguments preserve exact
tenant/run/task/attempt/fence/worker/Params/digest and accept only literal boolean
true from SQL (missing/false/integer replies reject). These are controlled
transport tests, not a real running-worker admission or registration race proof.
Positive generation/slot integration and complete renderer remain outstanding;
default region operation remains disabled. No production rollout occurred.

### V26 saved runtime rejection and Params readiness (migration 378)

Expanded populated seal fixture to persist invalid runtime profiles at initial
run creation. Candidate node, stored config and task profile agree; SQL still
rejects CPU backend/resource class, wrong renderer, mismatched ICC/model pins,
uppercase runtime SHA and missing runtime SHA. Thus these cases exercise runtime
guards directly rather than only candidate-vs-config comparison.

Migration378 repeats Params readiness in the seal scope inherited by worker
admission: ready state, verification, JSON media, a real pinned object version
and nonexpired retention are required alongside exact raw JSON SHA. Added
post-draft Params expiry and verification removal rejection cases.
Eight populated SQL cases passed (17.42s); the earlier runtime-only version also
passed eight cases (17.35s). Documentation passed 2113 static and 54 semantic
checks. Installed model/runtime qualification, running worker with real closure,
complete rendering and end-to-end admission/adoption remain incomplete. Catalog
stays disabled; no production migration rollout occurred.

### V26 full-canvas source mappings

Added prepare_region_source context for a future complete renderer. It validates
byte-bound WorkingImage before IO, maps exact full-canvas float32 RGB with the
existing strict NPY reader and preserves finite scene values above display range.
Inherited geometry is provenance of already materialized pixels; it is not
reapplied. Optional alpha uses explicit require_float32/normalize_uint16 policy
and existing normalization kernel; the latter requires full 4N+4096 scratch
budget before materialization. Context owns all maps and input reservations.

Ten source-input/adapter tests passed (2.68s): full-canvas/HDR preservation,
map/file release, changed source bytes, invalid policy/budget, shape mismatch
and nonfinite input rejection. Alpha-specific source integration and conversion
budget behavior are not covered by these new tests. The helper is not yet bound
to a complete renderer and does not resolve style descendants or qualify GPU
resources. End-to-end renderer/admission/adoption remain incomplete; region
stays disabled. No production rollout occurred.

### V26 alpha source integration checks

Added transparent source fixtures bound through canonical WorkingImage and
FrozenRecipe hashes and sorted input declarations. Real NPY mappings and
ScratchSpace prove uint16 normalization exactly as float32/65535 and unchanged
float32 alpha. Both normal exit and a renderer-body exception close RGB/alpha
maps and input handles and return the entire scratch reservation budget.
An insufficient 4N+4096 conversion budget rejects before input IO; strict mode
rejects legacy uint16 without implicit conversion.

Initial tests used incorrect ScratchSpace constructor/cleanup/property names;
corrected to its existing API and verify budget reuse through reserve(). Final
source/adapter/pixel suite passed 25 tests (1.27s). Documentation passed 2113
static and 54 semantic checks. Cancellation during alpha conversion itself,
real object-store inputs and complete recipe rendering remain unproven here.
The source helper is still not connected to a qualified complete renderer;
region remains disabled and no production rollout occurred.

### V26 cancellation inside source alpha conversion

Source preparation now bounds normalization budget and RAM/scratch estimates
to interoperable nonnegative integers (at most 2^53-1). Controlled cancellation
after actual ScratchSpace reservation but before the first normalization tile
proves conversion never yields, both input contexts close and the full scratch
budget becomes reusable. Tests reject boolean, fractional, oversized, negative
and missing RAM budget values. Source/resource/adapter suite passed 32 tests
(3.05s); documentation passed 2113 static and 54 semantic checks. This does not
qualify GPU/model resource usage or complete rendering. Full renderer, running
worker integration and public endpoint/client remain outstanding.

### V26 two-phase admission orchestration

FrozenRegionNode.Admit now joins transactional BeginInTransaction, external
BuildInputClosure publication and transactional SealInTransaction. Both DB
phases establish tenant-local context and repeat live authority. Created/pending
requests publish the frozen inventory; replay skips publication. Conflict/expired
outcomes propagate domain errors, returned run identity must match the frozen
shell, and the final ticket must match run/workflow and queued state. Params and
the source snapshot must already be published/frozen. Failed sealing leaves a
blocked resumable draft; object IO never occurs inside DB transactions.

Go v2region/v2input/v2draft suites passed. Documentation passed 2113 static and
54 semantic checks. This new orchestration method has not yet been exercised
with a successful live SQL/object-store job. Full renderer, qualified runtime,
endpoint/client and end-to-end adoption remain incomplete; region remains
disabled and no production rollout occurred.

### V26 live Go admission disabled-operation boundary

Extended actual PostgreSQL/MinIO Params publication fixtures with stable
run/workflow/task identities and FrozenRegionNode.Admit through a photo_api pool.
Default disabled catalog rejects at the real transactional authority gate before
the controlled closure publisher is called. Independent database assertions
confirm no region task, v26_region run or admission idempotency key is written.
Existing actual version-pinned Params publication, replay/content-conflict and
live source/recipe pinned-read cases remain covered.

Two live integration cases passed (8.66s); Go v2region/v2input and 2113 static/
54 semantic documentation checks passed. Fixtures still use synthetic pixel/style
references and a missing requested preview snapshot; disabled-catalog rejection
is proven, not positive admission, renderer qualification or successful replay
of a queued job. Complete rendering and end-to-end positive admission/adoption
remain outstanding; no production rollout occurred.

### V26 source snapshot descriptor

Added FrozenRegionPlan.SourceSnapshot with stable UUIDv5(run, region.snapshot),
canonical descriptor SHA and exact source version/artifact/canvas binding. This
is an internal descriptor for SQL snapshot persistence, not a worker contract.
JSON is defensively copied and exact plan bytes are verified before compilation.
Go tests prove stable retries, source binding, cross-run identity/hash separation,
defensive copies, missing run and changed-plan rejection. v2region/v2input suites
passed. SQL creation/freezing of this descriptor is still required before positive
admission. Complete renderer and end-to-end positive execution remain incomplete.

### V26 source snapshot persistence (migration 379)

Added API-only freeze_v26_region_snapshot and Go FreezeSnapshotInTransaction.
SQL verifies raw descriptor SHA, stable UUIDv5(run, region.snapshot), exact
recipe source binding and live locked source/recipe/style/session authority.
It creates an expiring draft snapshot/item before setting expiry NULL, preserving
existing immutable guards. Retries require matching digest, principal, exact
source scope and stored payload; a run already created without this snapshot
cannot retroactively create it. Go derives run from tenant/actor/idempotency key.

Isolated SQL test passed (2.50s): positive freeze, replay without duplicate item,
frozen state and wrong bytes/session/target rejection. Go v2region/v2input suites
and 2113 static/54 semantic documentation checks passed. This fixture seeds
metadata; successful Go freeze plus positive two-phase job admission on actual
objects remains unverified. Endpoint orchestration, full renderer and adoption
remain incomplete; no production migration rollout occurred.

### V26 queue service composition

Added QueueService.Queue joining authorized source/recipe preparation, a
server-owned RegionProfileProvider, scoped input resolution, immutable Params
publication, transactional snapshot freeze and two-phase node admission.
Run/workflow/task identities derive from tenant/actor/key. Public request SHA
includes tenant/asset/request; runtime profile, policy SHA and reservation come
from the trusted provider. The provider must qualify installed runtime and
budget the complete recipe before returning; source-only estimates cannot
substitute for that requirement.

Go v2region/v2input/v2draft suites and 2113 static/54 semantic documentation
checks passed. This new service is not yet covered by positive live integration,
not installed as a public route and has no qualified complete renderer/profile
provider. Default catalog remains disabled. Positive enqueue/execution/adoption
and endpoint/client remain outstanding; no production rollout occurred.

### V26 HTTP queue contract

Registered POST /api/v1/tenants/{t}/assets/{id}/region-previews using the existing
authenticated mutation boundary. Handler validates asset/key, caps JSON at16KiB,
preserves raw JSON for the strict RegionPreview decoder and invokes an injected
RegionQueue. It maps admission conflict/expiry/invalid errors and accepts only
nonzero run/workflow IDs with queued state before returning202. Missing queue
dependency returns503; no default qualified renderer/provider is wired.

Go httpapi/v2region tests passed, including actual router registration and
unauthenticated401 rejection. Documentation passed2113 static/54 semantic checks.
Initial build had an unused import, removed without changing behavior. Authenticated
positive HTTP integration, queue injection in production, complete runtime and
client remain outstanding; catalog stays disabled and no rollout occurred.

### V26 authenticated HTTP boundary integration

Extended shared HTTP fixture with an optional RegionQueue injection while
preserving existing callers. Real PostgreSQL auth bootstrap/session/signing and
native access token exercise the actual router. Controlled queue receives exact
authenticated user/session, tenant/asset and version; handler returns exact202
ticket. Duplicate keys, missing rect fields, oversized max_edge and injected
runtime field reject before queue. Admission conflict maps409 and nonqueued
queue ticket rejects503.

Live HTTP boundary integration passed (4.64s); Go httpapi/v2region suites and
2113 static/54 semantic documentation checks passed. Queue is controlled here;
these checks do not prove actual source authority, positive SQL enqueue, object
publication or complete rendering. Production queue/provider installation,
complete runtime and client still remain outstanding; catalog stays disabled.

### V26 exact frozen bytes across preparation

Unified frozen plan decoding for input resolution, Params publication and node
compilation. All three now verify SHA of exact stored bytes and require canonical
JSON before decoding. Hashing a reserialized typed model could hide changed
whitespace or discarded fields while sending different bytes to publication.
Regression test proves an appended byte cannot reach resolver/publisher.
Go v2region/v2draft/v2input tests passed. This closes preparation integrity only;
complete renderer and end-to-end admission/adoption remain incomplete.

### V26 source resource gate

Added region_source_resources and admit_region_source for source preparation
only. Estimates account for full RGB/alpha mappings, bounded NPY headers,
normalization scratch, row validation temporaries and tile working memory.
prepare_region_source optionally repeats this gate against a server execution
profile before materialization. Pixel count must agree exactly; RAM/scratch
estimates below the source requirement reject. Complete renderer must add its
own operator/model/geometry/presentation costs; this is not a complete GPU
resource estimate or a qualified hardware benchmark.

Source/resource tests passed 22 cases (0.64s), including alpha normalization
cost differences, invalid dimensions/policy/types, undersized estimates and
resource rejection before any input IO. Documentation passed 2113 static and
54 semantic checks. This optional source gate is not yet wired to a complete
renderer or public endpoint. Region remains disabled; no production rollout.

### V26 client request and selection isolation

Implemented `OwnerAPI.regionPreview`, strict request snapshot validation and exact queued JobTicket validation. The caller supplies the idempotency key and AbortSignal. `RegionPreviewSession` binds selection state to origin/user/tenant/asset/revision, aborts superseded requests, independently rejects late success/failure from obsolete generations, clears tickets on scope change/logout, and returns defensive ticket copies. Aborting the HTTP request does not claim cancellation of an admitted server job.

Verification: photographer `npm run typecheck` passed; targeted `region-preview.test.ts` and `region-preview-session.test.ts`: 10 tests passed. Tests include transports ignoring AbortSignal, all five scope changes, logout during failed request, mutable caller data, malformed ROI/request and malformed tickets.

This is client infrastructure only. Loupe UI, polling/rendered derivative display and integration with editor revision lifecycle are not wired. RegionPreview remains disabled until full renderer/runtime qualification and positive end-to-end admission/render/adoption verification. This evidence does not establish completion of V26 or the overall V20–V32 plan.

### V26 editor revision binding and rendered-scale calculation

Added `editorRegionScope` using the existing EditorState's versionId/sourceSha/draftRevision/requestRevision. Local edits and undo/redo generations invalidate the RegionPreviewSession binding even when the persisted draft CAS revision is unchanged. This helper does not introduce a parallel history store.

Added client `regionGeometry` with already-oriented final canvas dimensions, 200M pixel bound, outward floor/ceil crop rounding and bounded no-upscale output matching the server calculation. `regionRenderedScale` reports separate output/source ratios for both axes and marks sourcePixels only for an unscaled derivative; dimensions larger than 1024 or upscaled output are rejected.

Verification: TypeScript typecheck passed; three RegionPreview client suites: 14 tests passed, including crop rounding, extreme thin canvas, reduced scale, invalid geometry and late ticket invalidation after an unsaved editor command. No browser/native Loupe rendering, actual manifest loading or GPU execution was exercised. Helpers still require wiring into the editor UI and authorized run-output flow.

### V26 cancellable authorized output polling

OwnerAPI.run/runOutputs now forward optional AbortSignal without changing existing callers. Added waitRegionOutputs: up to 90 run metadata reads, abortable 1s delays, exact run identity, completed-only output retrieval, terminal/review states rejected. A completed result requires one document JSON reference (<=8MiB) and one JPEG/WebP derivative reference (<=16MiB), distinct artifact IDs, lower-case SHA256, integral bounded sizes and unexpired URLs. This reads catalog metadata only and does not download source/master bytes.

RegionPreviewSession.loadCurrent places polling and subsequent manifest IO under the existing selection generation and abort controller, suppressing obsolete success/error even for transports that ignore cancellation. It does not cancel admitted server tasks.

Verification: TypeScript typecheck and five targeted client suites passed (43 tests including OwnerAPI existing coverage). Output tests exercise bounded polling, failed/cancelled/review runs, wrong run identity, ambiguous/oversized/expired output references and cancellation during ignored transport abort; session test rejects late derivative metadata after revision change.

Remaining: validate actual downloaded manifest and derivative bytes against the requested immutable source/recipe/rect/ICC, wire Loupe UI, qualify complete renderer and prove positive full backend execution. Catalog checks alone do not authorize display or prove render quality. Region operation remains disabled.

### V26 canonical RegionDerivativeManifest client verifier

Added verifyRegionManifest for actual bounded canonical UTF-8 document bytes: catalog size and SHA256, strict field sets, canonical serialization equality (rejecting duplicate keys, BOM and noncanonical JSON), version/source/recipe/rect/complete-canvas geometry, expected Params SHA256 and target ICC, alpha/media, quality88/stripped metadata, cache key matching the worker formula, and exact distinct bounded catalog derivative reference. Returns copied geometry/reference rather than untrusted manifest objects.

Verification: TypeScript typecheck passed; five RegionPreview suites passed, 26 tests. Manifest mutation tests recompute valid document hashes yet reject substituted binding evidence, dimensions, profiles, output identity/SHA and extras. Tests include noncanonical trailing bytes, duplicate keys and BOM.

The caller must obtain trusted complete canvas dimensions, immutable source/recipe, admitted Params SHA and target ICC before invoking this verifier; they cannot be inferred from the untrusted manifest. Fetching bounded document bytes, verifying actual derivative encoding/bytes, wiring UI and positive full-runtime qualification remain outstanding. Tests use controlled canonical document fixtures, not a real GPU rendered output. Operation stays disabled.

### V26 bounded signed artifact download and composed loader

readRegionArtifact validates reference kind, declared size (JSON8MiB/image16MiB), digest and expiry before fetching, accepts HTTP(S) links without embedded credentials, omits credentials/referrer, rejects redirects and non-200 responses, checks Content-Length when present and streams into one declared-size allocation with incremental SHA256. Overflow/truncation/digest mismatch fail; aborted or failed streams cancel and release the reader. Stream-unavailable environments fail explicitly rather than buffering an unbounded response. No API authorization headers are forwarded to signed object URLs.

loadRegionPreview snapshots trusted binding evidence, waits for authorized catalog outputs, downloads and verifies the manifest, and only then downloads SHA/size-verified derivative bytes. It does not download masters. EncodedBytes are not yet approved for display: image decoding, dimensions/ICC/metadata checks and lifecycle-safe UI integration remain necessary.

Verification: TypeScript typecheck and three download/loader/manifest suites passed (8 tests). Tests exercise overflow, truncation, wrong hashes, stale links, stream cancellation and the composed loader refusing any image request after an invalid manifest. Fetch transports are controlled Responses; native stream support and live browser/object-store CORS are not proven. Full RegionPreview rendering/qualification and V20–V32 implementation remain incomplete.

### V26 actual worker-to-client contract compatibility

Added a Python-driven cross-language test that uses existing RegionPreview presentation/ICC conversion and actual JPEG/WebP encoding, builds and verifies RegionDerivativeManifest through worker contracts, and supplies canonical document bytes plus encoded bytes to TypeScript verifyRegionManifest/loadRegionPreview. Both opaque JPEG and alpha WebP cases pass. Client tests also reject an altered expected ICC and a one-byte corruption of each actual encoded image. Temporary test cases are created and removed under repository .runtime; no static hand-copied worker schema fixture substitutes for worker serialization.

Verification: test_region_preview_client_contract.py passed (1 Python driver invoking 2 TypeScript cases); photographer TypeScript typecheck passed. The TypeScript cases explicitly skip when not invoked with PHOTO_REGION_CLIENT_CASES; ordinary vitest alone is not evidence for this cross-language check.

This exercises real ICC presentation/encoding from controlled rendered arrays and controlled HTTP Responses. It does not exercise complete recipe rendering, GPU qualification, live signed-link/CORS access, native stream support or browser/native Loupe decoding/display. Full project implementation remains incomplete.

### V26 owned browser derivative display resource

prepareRegionDisplay snapshots verified metadata, rechecks encoded byte count/SHA256 before decode, copies the bounded derivative bytes into an owned Blob, invokes createImageBitmap with browser default color conversion and verifies decoded width/height against manifest geometry. Decoded bitmaps close on success, dimension mismatch and late abort. A URL is created only after these checks; dispose/abort revokes it exactly once and removes the listener. Returned scale reports actual output/source ratios.

Verification: photographer TypeScript typecheck and four display-resource tests passed. Controlled decoders prove no URL exposure on dimension mismatch or late cancelled decode, no decoding after byte corruption, bitmap closure and idempotent URL revocation. These tests do not prove real browser color management, embedded ICC/metadata parsing, native decoder support, DOM rendering or full Loupe integration. createImageBitmap absence explicitly fails. The worker/adoption path still owns encoded ICC validation; client byte identity does not substitute for GPU/render qualification.

### V26 localized Loupe presentation component

Added RegionLoupe with injected admitted/verified display loader, localized RU/BG/EN loading/failure/image labels and explicit per-axis rendered scale. Scope/loader changes hide previous images synchronously; effects abort previous work, dispose owned resources and dispose late results even when loaders ignore cancellation. Deferred IO does not begin after immediate unmount. A source-pixel-detail label describes unscaled derivative detail, not CSS/device zoom.

Verification: TypeScript typecheck passed after replacing unsupported React Native accessibilityRole=status with accessibilityLiveRegion=polite; Loupe DOM/locales/display suites passed (9 tests). DOM tests use react-native-web and controlled display loaders; they prove scope hiding, late cleanup, localized errors, unmount cleanup and no deferred IO after unmount.

This component is not mounted in the editor route yet. It requires trusted admitted recipe/Params/source/canvas/ICC evidence and capability-qualified backend, which are not fully wired. Real browser/native decode/color management and full GPU renderer remain unverified; the RegionPreview operation remains disabled. No completion claim for V26 or V20–V32.

### V26 early disabled-operation admission gate

Forward-only migration380 adds API-only v26_region_available(tenant): current transaction tenant context, active tenant and enabled region.preview@1 catalog entry. QueueService checks cancellation/identity before work, then this availability function before source/recipe object IO, profile compilation, Params publication and snapshot freezing. ErrRegionUnavailable maps to HTTP503 DEPENDENCY_UNAVAILABLE. Begin/Seal retain their live catalog and authority checks; early availability is advisory and grants no permission to run.

Verification: Go v2region/httpapi package tests passed; isolated PostgreSQL region registry suite passed (8 tests), including migration role privileges, tenant-context mismatch, rolled-back enabled catalog fixture and real Go QueueService refusing before external providers. Cancelled QueueService exits before SQL. Documentation checker passed with migration380 assigned to V26.

Migration380 was verified in isolated test databases only. RegionPreview remains disabled and production wiring/runtime qualification/positive full admission-render-adoption proof remain incomplete. This change avoids unnecessary preparation artifacts while unavailable; it does not complete V26 or the V20–V32 goal.

### V26 authenticated HTTP failure contract coverage

Extended the real-router/PostgreSQL-auth integration test to assert status and public error code for ErrConflict (409 IDEMPOTENCY_CONFLICT), ErrExpired (410 SNAPSHOT_EXPIRED), ErrRegionInvalid (422 VALIDATION_FAILED), and ErrRegionUnavailable (503 DEPENDENCY_UNAVAILABLE). Added a separate authenticated-boundary test proving missing authentication and missing/oversized idempotency keys reject before Queue invocation. The Python driver now selects both RegionHTTP tests rather than only the original exact name.

Verification: test_v26_region_http_go.py passed (1 Python driver running both Go integration tests). Queue is controlled in this boundary test; this does not prove real queue creation, worker execution or qualification. Early availability migration380 remains tested separately; operation remains disabled and full V20–V32 implementation is still in progress.

### V26 adaptive prototype local-stage coverage

Fixed required_region_modules: adaptive prototypes with enabled local color now require the local stage even when the fixed fallback's local module is disabled. Zero strength does not waive stage coverage. This closes a path where a renderer declaring only color/strength/adaptive could pass the complete-renderer gate despite a selectable prototype requiring person/background local correction.

Verification: RegionPreview adapter suite passed (5 tests). A strictly validated adaptive StyleV2 with disabled fixed local, enabled prototype local and strength0 requires local; execution with a renderer lacking local rejects before render or staging. Existing orchestration/authority/beauty-rejection tests still pass. This gate fix does not implement the missing full renderer or qualify CUDA execution.

### V26 renderer runtime digest binding

QualifiedRegionRenderer now declares runtime_sha256. Region adapter requires a lower-case SHA256 and exact equality with execution_profile.region_runtime_sha256 before runtime verification or any Params IO, alongside existing backend/renderer/model/ICC bindings. CUDA operation registration requires a valid renderer runtime digest before verify_runtime. The provider still must verify actual installed pinned bytes; declaring a digest alone is not runtime qualification.

Verification: RegionPreview adapter suite passed (10 tests). Missing, changed, upper-case and malformed task runtime pins reject with CONFIG_INVALID before runtime verification/input IO; a CUDA provider without a valid digest cannot register. Existing orchestration, authority denial, module coverage and adaptive-local tests pass. Full runtime provider/rendering and target RTX3060 qualification remain incomplete; operation stays disabled.

### V26 verified full-source shape available to profile provider

FrozenRegionPlan retains private value-owned canvas/alpha evidence from the validated WorkingImage. SourceShape checks exact plan bytes/canonical hash, canvas hash against FrozenRecipe.Source.CanvasHash and compiled ROI geometry before returning full width/height and alpha presence. Queue calls this check before profile compilation. Profile providers can now budget the entire canvas rather than incorrectly deriving source size from the ROI; operator/model/style and presentation costs remain their responsibility.

Verification: Go v2region/httpapi tests passed. SourceShape tests prove defensive values, rejection of changed canvas, altered plan bytes and plans lacking retained verified source evidence. Worker Params wire contract and operation activation remain unchanged. No actual full profile provider/GPU renderer qualification is claimed.

### V26 server full-source resource lower bound

Added RegionSourceShape.ResourceBudget and Queue policy validation before PrepareNode/input resolution/Params publication. Requires an integral resource_estimate with exact full-source pixel_count and RAM/scratch at least the source mapping lower bound. The bound includes NPY headers, mapped RGB/alpha, conservative full float32 legacy alpha normalization, validation rows/tile temporaries and fixed overhead, matching the worker source estimator's normalize_uint16 policy. Qualified policy must add complete style/model/operator/presentation costs; satisfying this bound does not qualify a runtime.

Verification: Go source-budget tests cover exact opaque budget, alpha normalization delta, invalid canvases, missing estimates, boolean values and underbudget fields. No production provider is installed or operation enabled. Profile providers lacking resource_estimate are now rejected by Queue rather than admitted with an unspecified budget.

### V26 server/worker source budget parity

Added Python-driven Go parity verification using the actual worker region_source_resources estimator with normalize_uint16 policy. Sixteen opaque/alpha vectors cover 1px, standard frames, 512-tile boundaries, 24MP, thin 200M-pixel canvases in both directions and a full 200M-pixel frame. Go ResourceBudget matches pixel_count/RAM/scratch exactly. This test checks arithmetic without allocating those images.

Verification: source-resource cross-language driver and worker resource suite passed (15 tests). Go parity test explicitly skips without PHOTO_REGION_SOURCE_BUDGETS; ordinary go test alone does not run the worker parity vectors. This proves source component agreement, not complete operator/model budgets or hardware feasibility. V26 full runtime/provider and overall V20–V32 implementation remain incomplete.

### V26 independent worker source-budget gate and typed failures

After bounded source JSON verification, region execute independently calls admit_region_source with the validated full canvas/alpha and conservative normalize_uint16 policy, before style loading, binary source mapping or rendering. Renderer.admit_resources still runs earlier for full pipeline costs; an empty/no-op implementation cannot waive the independent source minimum. An explicit OperationFailure pass-through and regression tests preserve CONFIG_INVALID/RAM/SCRATCH/VRAM admission codes. OperationFailure derives from RuntimeError, so the prior ValueError handler did not itself rewrite these codes; initial budget-test failures came from an incorrectly bound source length.

Verification: adapter, SQL-admission and source-resource suites passed (38 tests). Tests use a no-op provider resource check to reject wrong pixel count, low RAM/scratch and boolean estimate before style/render/staging, and preserve an explicit VRAM failure before recipe/source IO. Resource fixtures were corrected to bind actual source JSON length before exercising the budget gate. Full qualified provider/renderer and GPU execution remain incomplete.

### V26 frozen adaptive-style preservation regression

Added a strictly validated adaptive StyleV2 regression through materialize_frozen_style. Frozen EV/saturation edits modify their declared global fields while adaptation/prototype LUT/local configuration, dependencies and other non-global/non-beauty recipe fields remain identical. The base style remains unchanged, and mutating the returned prototype local settings does not mutate the source style. Required module coverage continues to include adaptive and local.

Verification: frozen adaptive-style and RegionPreview adapter suites passed (16 tests). This checks contract materialization and object ownership; it does not prove selected adaptive LUT pixel rendering, full recipe effects, model quality or GPU execution. Full renderer/profile provider and overall V20–V32 implementation remain incomplete.

### V26 cancellation/lease fences between preparation stages

Region execute now rechecks activity after runtime verification, recipe/style retrieval, before renderer entry and after renderer exit before staging. A slow provider returning after cancellation or lease loss cannot initiate the next expensive stage solely because activity was checked before the provider call.

Verification: RegionPreview adapter suite passed (21 tests). Six new scenarios inject TaskCancelled/LeaseLost during runtime verification, recipe loading and style loading and assert exact stopped reads, no renderer entry and no staged outputs. These are controlled provider-boundary tests, not live lease concurrency or CUDA cancellation proof. The earlier resource-error diagnosis is corrected above: OperationFailure is RuntimeError; initial test failures were caused by incorrect fixture source length.

### V26 renderer ownership and post-Params cancellation fence

Added activity check immediately after Params loading and before SQL admission, independent of provider-specific cancellation handling. Cancellation/lease-loss tests now cover this boundary and require zero admission calls. Added renderer context tests for stop at entry and stop during release: finally closes the controlled renderer context, no artifacts are staged and all attempt scratch capacity remains reusable.

Verification: adapter plus SQL-admission suites passed (36 tests). Four renderer context scenarios cover TaskCancelled/LeaseLost at entry/release; two post-Params cases reject before admission. Tests use controlled renderers and do not prove CUDA context release, live worker lease races or full runtime qualification. Region operation remains disabled; full V20–V32 implementation remains in progress.

### V26 full-source shape/budget on pinned object preparation

Extended actual PostgreSQL+versioned-MinIO preparation/publication integration to verify SourceShape against the byte-validated WorkingImage's full canvas and alpha presence, accept its calculated minimum source budget and reject scratch budget one byte below that minimum. The live source/recipe case now runs opaque and alpha-reference variants while latest object versions are deliberately corrupt, preserving proof that shape comes from the SQL-pinned versions.

Verification: test_v26_region_publish_go.py passed (3 tests). Source and recipe documents and Params use real pinned MinIO versions; RGB/alpha array refs and StyleV2 descendants remain synthetic in this preparation fixture. No binary array fetch, complete renderer, successful queued admission or GPU quality is proven. RegionPreview remains disabled and the full project goal remains incomplete.

### V26 actual pinned RGB/alpha arrays through TaskArtifacts

Added MinIO integration using real NPY RGB and uint16/float32 alpha objects, controlled pinned catalog locations and actual TaskArtifacts.input/materialize_gateway/read_array/prepare_region_source. Latest object versions are deliberately corrupted after storing valid versions. The pinned old arrays remain usable; float32 RGB values1.5 are preserved before rendering, uint16 opacity converts exactly /65535 and float32 opacity stays unchanged. All mappings close and the entire admitted source scratch budget is reusable after exit.

Verification: test_v26_region_source_arrays.py passed (2 alpha dtype cases) after correcting strict UUID parsing in the fixture. Transfer uses actual MinIO reads and materialization SHA/size checks. Authenticated HTTP gateway, SQL closure authority, full recipe/rendering and CUDA qualification remain outside this test; controlled gateway locations do not prove those paths. Overall project implementation remains incomplete.

### V26 failed RGB/alpha materialization cleanup on actual object reads

Extended the MinIO array integration with contradictory transport evidence: declared original artifact SHA/size but selected object version containing a one-byte mutation, separately for RGB and alpha. Actual TaskArtifacts/materialize_gateway rejects ARTIFACT_HASH_MISMATCH before exposing arrays to rendering. Both uint16 and float32 alpha cases verify every response stream closes and the entire attempt scratch budget remains reusable after each failure, including failure after RGB was already mapped.

Verification: test_v26_region_source_arrays.py passed (2 parametrized cases with four corruption paths). This is a controlled gateway-location mismatch test, not a mutation of an immutable object version or proof of SQL scope/closure authorization. Full runtime/GPU rendering and overall V20–V32 implementation remain incomplete.

### V26 artifact/profile compatibility with shared worker runtime

Queue now validates its qualified profile against existing TaskArtifacts requirements before PrepareNode: admitted RAM >=3GiB for strict StyleV2 parsing, and positive integral output_bytes within the safe integer bound. This complements the full-source component budget; it does not estimate model/operator/presentation costs or guarantee enough output capacity for every image. The existing worker parser budget is reused, not replaced with a RegionPreview-specific assumption.

Verification: Go v2region/httpapi tests passed. New tests cover exact 3GiB threshold, missing output budget, zero/boolean/unsafe output values and malformed RAM. Full production provider/renderer remains unimplemented and the operation disabled.

### V26 source-bound batch parameter command preparation

Added applyBatchCommand using existing EditorState/applyEditorCommand history, not a parallel store. Frozen selection is bounded to5000 unique version IDs and carries source SHA/server draft revision. Each matching source receives an independent JSON parameter edit preserving its other parameters/masks/history; missing states, changed source/revision and missing parameter paths produce separate conflicts. Unsafe JSON values and prototype paths reject before preparation. affectedCount supports previewing prepared changes.

Verification: photographer TypeScript typecheck and batch/editor-store suites passed (11 tests), covering per-source override preservation, immutable input state, request-generation advancement and conflict separation. This prepares local overrides only; gallery selection UI, review/confirmation and per-source server CAS persistence are not wired. Full V26 and V20–V32 implementation remain incomplete.

### V26 per-source batch CAS persistence coordinator

Added saveBatchCommand using existing prepareEditorDraftSave/acceptEditorDraftSave and injected OwnerAPI-compatible save callback. It snapshots requests/asset IDs before awaits, checks current source/server/local generation before each sequential write, reports each conflict/failure independently and preserves edits made while a save is pending. Cancellation stops new writes but a completed in-flight commit is acknowledged. Missing/changed current state after successful commit reports saved_refresh_required rather than claiming no write happened.

Verification: Final TypeScript typecheck, batch preparation/save suites (7 tests) and documentation checker passed after the asset-ID snapshot edit. Tests cover independent revisions, 409 conflict, late local edits, cancellation after commit and changed selection before write. This is an executable coordinator, not yet mounted gallery UI or real HTTP/native persistence evidence; UI must bind tenant/user/origin lifetime and expose per-source conflicts. Full V26/V20–V32 remains incomplete.

### V26 synchronized multi-frame comparison state

Added compareFrames for2–6 unique source-bound authorized previews with the same ICC SHA, copied descriptors, one normalized center/zoom viewport and per-version local accept/reject decisions. compareFrameTransform maps that shared normalized viewport to different display boxes; selection cycles without modifying source frames. Zoom is bounded1..16 and invalid viewport/selection input rejects.

Verification: photographer TypeScript typecheck and three comparison tests passed, covering descriptor ownership, ICC mismatch/cardinality rejection, synchronized transforms and isolated decisions. This is comparison state logic only: synchronized multi-image UI, keyboard bindings, real canonical preview byte validation and server review persistence remain unwired. A matching metadata ICC field does not independently prove color-managed bytes. V26/V20–V32 implementation remains incomplete.

### V26 shared comparison keyboard commands

Added compare.previous/next/accept/reject to the common commands table with RU/BG/EN labels and Ctrl/Meta+Left/Right/Enter/Backspace bindings. Per-command allowInText policy preserves Capture save/refresh behavior while comparison shortcuts ignore actual input/textarea/select/contenteditable/textbox targets. Existing repeat, IME, Alt and prevented-event guards remain. useCommands updates handlers without accumulating document listeners and removes its listener on unmount.

Verification: TypeScript typecheck plus command/locales/DOM-hook suites passed (5 tests). Actual DOM targets test nested contenteditable/input suppression, Capture Save in input, latest handler after rerender and listener cleanup. Comparison gallery mounting, native actions and server decision persistence remain unwired; V26/V20–V32 still incomplete.

### V26 controlled multi-frame comparison UI

Added CompareFramesView with shared viewport transforms across2–6 frame panels, selectable frames, per-frame local decisions, bounded pan/zoom buttons and common previous/next/accept/reject keyboard commands. All controls/status labels are RU/BG/EN. The controlled parent owns selection/session lifetime; component does not alter originals or persist review decisions.

Verification: TypeScript typecheck and comparison UI/state/commands/locales suites passed (8 tests). DOM harness proves buttons/keyboard share latest selection/decisions and viewport state, editing input suppresses comparison shortcuts, source preview URIs remain unchanged. Real browser layout alignment/color management, native gesture behavior, gallery route mounting and server decision persistence remain unverified/unwired. V26 and overall V20–V32 remain incomplete.

### V26 collection screen lifetime fences (2026-10-02)

The collection route mounts a fresh scope for each tenant/collection pair and resets loaded metadata and preview links when its API client changes. Its load controller also fences moderation acknowledgements, contact-sheet snapshot completion and navigation after contact-sheet submission. A synchronous mutation lock prevents concurrent review/contact-sheet actions before React updates button state. Completed server mutations are not rolled back on navigation; late acknowledgements are suppressed locally. Remaining bounded thumbnail tasks stop starting requests once the scope is aborted.

Verification: photographer TypeScript check and three DOM regression tests in `src/collection-scope.dom.test.tsx` passed: old preview suppression, no contact-sheet submission after a late snapshot, and no navigation after a late committed contact-sheet response. These checks use controlled API responses; they do not prove live HTTP cancellation, native navigation or account revocation. The comparison component remains unmounted: current media-link responses lack the ICC evidence required by its comparison contract. Full V26 and V20–V32 completion is not established.

### V26 authorized comparison preview metadata (2026-10-02)

Owner media-links responses now add asset_version_id, artifact_sha256, icc_sha256 and profile_hash. The asset version is the authorized request version; artifact SHA comes from the selected pinned ready derivative; ICC is the configured sRGB digest and the profile hash is the exact existing media-generation policy hash used to resolve that derivative. These fields attest catalog/profile selection, not independent inspection of encoded pixels. Client MediaLink fields remain optional for older servers; comparePreview refuses missing evidence.

comparePreview accepts only preview1600/preview2560, validates version/source and artifact/ICC SHA, finite unexpired expiry, positive bounded dimensions, JPEG/WebP and safe HTTP(S) URL, and recomputes the canonical server profile policy hash. It returns the comparison descriptor only after these checks. Tests: Go media package passed; two client tests passed; photographer TypeScript passed. Live PostgreSQL signing, encoded ICC verification, gallery mounting and full comparison workflow remain outstanding.

### V26 gallery comparison mounted (2026-10-02)

CollectionDetail now mounts GalleryComparison in its list header. The tenant/collection-keyed parent owns its lifetime. Users select 2–6 collection items; only preview1600 links are requested, paired with authorized editor drafts to check exact version/source binding. comparePreview validates the server catalog/policy evidence and compareFrames requires a shared ICC digest. Draft and media-link APIs accept AbortSignal. Selection/API/item changes abort the load, immediately hide stale loaded state, and ignore transport responses delivered after cancellation. The earliest signed-link expiry removes the comparison. Decisions remain explicitly labelled local; collection moderation persistence is not yet connected.

Verification: photographer TypeScript passed; nine targeted tests across gallery/collection scope/comparison UI/metadata passed, followed by four gallery tests including explicit expiry regression. Documentation checks passed. Gallery tests use controlled API responses and RN-web DOM; native device behavior, live HTTP/SQL signing, actual pixel ICC verification and remote decision persistence remain unproven/incomplete. Source/master artifacts are not requested by this UI. Earlier entries stating comparison was unmounted describe the prior state. Full V26 and V20–V32 completion remains unproven.

### V26 comparison Apply decision persistence (2026-10-02)

Gallery comparison exposes an explicit Save decisions action for Apply collections. CollectionScope shares its synchronous mutation lock across individual moderation, contact sheets and comparison persistence. saveCompareDecisions freezes 1–6 decisions and validates unique pending-review versions in the exact collection, eligible Apply origin and safe integer revision before writes. Existing moderation calls are sequential CAS requests; exact returned item/version/decision bindings are required before advancing the known revision. Cancellation stops new writes but acknowledges a confirmed in-flight commit. Any conflict, invalid response or transport uncertainty stops the remaining batch; no rollback or automatic retry is claimed. An incomplete outcome invalidates the parent collection state to prevent further writes with stale revision and asks for reload. Route lifetime fences suppress updates after navigation.

Verification: TypeScript and 11 targeted tests passed (save coordinator, gallery UI, collection lifetime). Coordinator tests cover sequential revisions, uncertain failure stop, cancellation after commit and preflight rejection. UI persistence uses the existing HTTP API but live HTTP/SQL batch behavior and native acceptance remain unverified. General collection culling persistence beyond Apply moderation remains outstanding. Earlier local-only descriptions record the earlier state; unsaved comparison decisions remain local until explicit save. Full V26/V20–V32 completion remains unproven.

### V26 collection reload and 5000-frame metadata pagination (2026-10-02)

The collection route exposes Reload collection, disabled while loading or mutating. Reload aborts the prior load lifetime, resets previews/items and remounts comparison selection. Individual Apply moderation now uses the same frozen-decision CAS coordinator as comparison persistence. Thumbnail requests receive the load AbortSignal.

loadCollectionPages reads up to 5000 metadata rows in pages of at most 100, validates exact collection membership, unique item IDs and the same collection revision on every page, rejects repeated cursors/empty advancing pages/overflow, and checks cancellation before and after each read. Only the first 24 thumbnails are loaded by the existing preview queue. The comparison selector is now a horizontal FlatList with initial/batch 10 and window size 5, rather than eagerly rendering every frame selector. No final independent revision read or live DB snapshot is claimed; subsequent mutations remain CAS protected.

Verification: TypeScript and 11 targeted tests passed. Tests cover 5000 rows across 50 pages, changed revisions/duplicates/repeated cursors, cancellation, and reload after uncertain moderation without automatic mutation retry. Tests use controlled API responses; real 5000-frame rendering performance, native behavior and live concurrent SQL mutations remain unverified. Full V26/V20–V32 completion remains unproven.

### V26 Scene Board immutable model and presentation (2026-10-02)

buildSceneBoard consumes an already verified ScenePlan with exact snapshot ID/digest/policy pins and a frozen 1–5000 frame binding inventory. It rejects unknown/missing plan fields, invalid UUID/SHA/confidence, duplicate versions/groups/ordinals, unsorted group membership, missing frames and references outside their groups. It returns defensive copies in stable frozen ordinal order. Outliers are explicitly null: the current ScenePlan schema has no outlier assessment, and no heuristic is presented as server evidence.

SceneBoardView provides virtualized groups/frame selectors, reference/confidence labels and an unavailable outlier label in RU/BG/EN. Reference callbacks explicitly request a new analysis; the supplied immutable plan is never modified. Verification: three model tests (including 5000 bindings) and TypeScript passed. The view is not yet mounted in the required sessions route; authenticated plan/inventory loading, callback admission and live/native view verification remain outstanding. No completed Scene Board or V26/V20–V32 acceptance is claimed.

### V26 authenticated Scene Board API foundation (2026-10-02)

GET /session-analyses/{id}/scene-board now delegates to v2sessions.GetSceneBoard. The read transaction authorizes current tenant/project viewer access and requires a completed/waiting-review session workflow, matching immutable snapshot/policy, verified ready version-pinned plan and matching workflow artifact link. Snapshot members must be 1–5000 contiguous ordinals with unique versions and valid source SHA. The transaction releases before bounded exact-canonical/SHA object reads. Existing decodeScenePlan checks the full frozen group partition. Responses include analysis/snapshot/policy/plan SHA, actual plan JSON and metadata-only frame bindings; no-store is set. No source/master bytes are returned.

SceneBoardDocument schema and endpoint were added to normative generation. Go v2sessions/httpapi tests passed, including unavailable dependencies fail-closed; documentation checks passed with 171 schemas/52 endpoints/79 entry points. Current evidence does not prove execution of the new SQL under PostgreSQL roles or live storage IO. Client route/API integration and concurrent revocation tests remain outstanding. Full V26/V20–V32 completion remains unproven.

### V26 Scene Board client route and authenticated projection validation (2026-10-02)

SceneBoardDocument now includes session_id from the authorized analysis row. OwnerAPI.sceneBoard requests the authenticated API with AbortSignal and readSceneBoardDocument validates exact response keys, route session/analysis identities, frame limits, canonical plan SHA (8 MiB cap), then builds the source-bound board. /sessions/[id]/scenes mounts SceneBoardView for the analysis query parameter, keyed by tenant/session/analysis. Refresh/API changes suppress old loaded state immediately; aborted/late responses cannot populate the new scope. RU/BG/EN reload/error UI and LocalePicker are included. Reference controls stay disabled until new-analysis admission is connected.

Verification: seven targeted client tests passed across immutable model, document digest/identity validation and actual route DOM stale-response handling; TypeScript, Go v2sessions and documentation checks passed. No live SQL/storage route, native-device qualification or complete session navigation/reference workflow is claimed. Full V26/V20–V32 completion remains unproven.

### V26 Scene Board live PostgreSQL API-role verification (2026-10-02)

The existing isolated V22 Apply integration fixture now establishes the immutable scene-plan workflow link and invokes GetSceneBoard through a connection running as photo_api. It verifies exact analysis/session/snapshot/plan SHA and canonical bytes, one contiguous frame/version/source binding, then rejects modified plan bytes, missing analysis and an unregistered authenticated session. Existing Apply-new and sealed-replay checks run in the same driver without changing their admission invariants. No production migrations or catalog changes were applied.

Verification: tests/integration/test_v22_apply_replay_go.py passed against a freshly migrated isolated PostgreSQL database (7.56 s); Go v2sessions/httpapi and documentation checks passed. The integration store is a controlled in-memory ObjectReader, so this proves SQL syntax/API grants/tenant authorization and actual canonical/hash validation, not MinIO version-pin transport, concurrent revocation during object reads, native rendering or full reference-change admission. Full V26/V20–V32 completion remains unproven.

### V26 reference-change client admission wiring (2026-10-02)

SceneBoardView reference actions now load the current authorized shoot session and collection, create a selection snapshot, then submit a new analysis through OwnerAPI.analyzeSession. sceneReferenceRequest checks the chosen version belongs to the selected immutable group and session remains open; all other displayed group references are preserved as previous-analysis choices. Policy is deep-copied, revision is pinned and canonical body is capped at 1 MiB. The old plan is not modified.

The route synchronously locks submission and retains the exact prepared request/key after an admission error; Retry submits the same body/key, and fresh reference actions/reload remain disabled while this attempt is unresolved. Lifetime cancellation prevents subsequent preparation requests and old-scope navigation. A confirmed ticket opens the existing run route. Attempt persistence is currently in memory only; process/navigation recovery is not proved. Terminal errors do not automatically create a replacement request.

Verification: TypeScript and six targeted tests passed across request preparation, route stale-read fencing and authenticated projection validation. Tests establish preserved references, policy defensive copy and ineligible choice/session rejection. The actual reference mutation route, live snapshot/admission/worker execution and duplicate-ticket retry still need integration coverage. The primary session page/navigation and outlier assessment remain incomplete; full V26/V20–V32 completion remains unproven.

### V26 reference route retry and revoked-access regression checks (2026-10-02)

The mounted scenes route now distinguishes reference failure, 503 unavailable, terminal selection/revision rejection and revoked access in RU/BG/EN. Transport/503 failures retain the exact prepared body/key. Explicit 409/410/412/422 rejections release the pending attempt and instruct reload; no automatic replacement request occurs. 401/403/404 clear the displayed board and pending mutation.

Verification: six route DOM tests passed, including same-body/key retry with only one snapshot, suppression of a late admission ticket after navigation, immediate board clearing after API 403 and terminal conflict releasing reload. TypeScript and request preparation tests passed. These tests execute the actual route with controlled OwnerAPI responses; they do not prove live HTTP idempotency or GPU analysis execution. Attempt recovery across process restart remains incomplete. Full V26/V20–V32 completion remains unproven.

### V26 session analysis discovery API (2026-10-02)

GET /shoot-sessions/{id}/analyses exposes ready analysis Resource envelopes, with current tenant/project viewer authorization and no-store. ListAnalyses uses the existing signed cursor codec scoped to tenant/principal/session and deterministic created_at/id order, limit 1–100. Ready plans require verification, an actual object version and nonexpired artifact. The list is metadata only; Scene Board performs its stronger workflow/partition checks when opened. This supplies the missing server dependency for selecting analyses from the primary session page.

Verification: Go v2sessions/httpapi and documentation checks passed (53 endpoints/80 entry points). The existing isolated V22 integration driver passed in 5.30 s; through photo_api it verifies one returned exact analysis envelope, no spurious next cursor, and denial for an unregistered authenticated session, while preserving existing Apply-new/replay checks. Multi-page/live cursor integration and the client primary session page still remain outstanding. Full V26/V20–V32 completion remains unproven.

### V26 primary session page and analysis discovery client (2026-10-02)

/sessions/[id] now reads the authorized session and first analysis page, renders a virtualized analysis list and opens the selected analysis at /sessions/[id]/scenes without manual analysis identifiers. A keyed tenant/session scope and AbortController fence reads; reload resets metadata; explicit More analyses loads bounded pages with duplicate/repeated-cursor checks and a current 5000-record client cap. Revoked access during pagination clears loaded data. UI labels are RU/BG/EN.

OwnerAPI.sessionAnalyses uses readSessionAnalyses, which validates immutable Resource revision=1, matching row/envelope IDs, session membership, ready-plan artifact binding/SHA, timestamp and page size/cursor. It deliberately does not require data.revision on immutable session_analyses rows. Verification: TypeScript and eight targeted tests passed across decoder and existing scene reference route. Primary session page DOM/native navigation and pagination concurrency still require tests; first-analysis launch and policy editing remain incomplete. Full V26/V20–V32 completion remains unproven.

### V26 initial analysis launch from session page (2026-10-02)

The primary session page now starts analysis for an open authorized session. It reads the current collection, freezes a selection snapshot, and initialSceneAnalysisRequest copies the loaded session policy/revision with an empty manual-reference set. A synchronous lock prevents overlapping launches/pagination. The exact prepared body/key is kept for explicit retry after transport/503 errors; terminal 409/410/412/422 releases the attempt for reload, and access denial clears loaded metadata. Scope cancellation prevents subsequent requests or navigation from a late ticket. Confirmed tickets open the existing run screen. Retry recovery remains in-memory, not durable across process/navigation.

Verification: TypeScript and five targeted tests passed across actual primary-route DOM and immutable analysis-list decoding. Route tests prove one snapshot and exact body/key across retry, no late old-session navigation, and correct session/tenant/analysis parameters when opening Scene Board. These use controlled API responses; live initial-analysis admission/worker results and native navigation remain outstanding, as does policy editing. Full V26/V20–V32 completion remains unproven.

### V26 session grouping policy editor (2026-10-02)

The session page mounts SessionPolicyEditor keyed by current server revision. Users edit integer time gap 30–7200 and neighbors 1–64. editSessionPolicy deep-copies and preserves calibration, camera offsets and manual-pick protection. Unsaved changes disable analysis/reload; explicit revert restores the loaded baseline. Saving uses updateShootSession CAS with the existing session name/state/capture policy and a frozen body/key. Transport failure retains that attempt for explicit retry; terminal validation/revision/access errors release it. The parent serializes policy saves with analysis/pagination, acknowledges only its live scope, and installs the returned session revision on success. Revoked access clears metadata.

Verification: TypeScript and six targeted tests passed across policy bounds/defensive copy and actual session-route DOM. The save test verifies unsaved-edit analysis blocking, CAS revision, preserved protection, and identical body/key across retry. Camera-offset/calibration editing, durable draft recovery, explicit side-by-side policy conflict resolution, live API CAS and native interaction remain outstanding. Full V26/V20–V32 completion remains unproven.

### V26 explicit grouping-policy conflict resolution (2026-10-02)

On 409/412, SessionPolicyEditor now reads the current authorized session and shows local/server time-gap and neighbor values side by side. No automatic write follows. The explicit resolve action prepares a new body/key using the freshly read revision and preserves its current name/state/capture policy/calibration/camera offsets, while applying only the two local grouping controls. Another conflict re-reads current state. Transport uncertainty after resolution retains the exact newly prepared attempt. Scope cancellation ignores late conflict reads, and access denial clears the parent metadata.

Verification: TypeScript and seven targeted tests passed. The actual session-route DOM test asserts no second write before explicit resolution, revision 5 on the new write, a distinct key from the rejected request, preservation of current server camera offsets/name, and local grouping values. This proves controlled-client behavior; live policy HTTP CAS/native interaction, camera-offset/calibration editing and durable draft/attempt recovery remain outstanding. Full V26/V20–V32 completion remains unproven.

### V26 complete client regression and LayerStack editing foundation (2026-10-02)

The full photographer test run passed 175 tests in 45 suites; two external-worker fixture cases in one suite remained skipped. This is client test evidence, not V26 acceptance.

editor-layers adds the exact existing LayerStack/Layer/MaskExpr wire types and defensive-copy validator: exact keys/source binding, 32 layers, unique IDs, finite opacity, documented blends, UTF-8 operation/version lengths, exact mask canvas, depth <=16 from root depth 0, shared mask-node limit 256, and active geometry prefix. patchEditorLayer edits only enabled/opacity/blend; moveEditorLayer revalidates the resulting stack. Server authorization/qualified operation policy remains mandatory. OwnerAPI.saveV2Draft now optionally submits a validated inline stack instead of always forcing layers:null, while copying the recipe before transport. Default callers remain compatible.

Verification: TypeScript passed; four layer tests passed, including geometry reordering rejection, canvas mismatch, defensive copy, and exact 256-node acceptance/257-node rejection. Layer UI, integration with existing editor history, live draft CAS/publication and full renderer/loupe qualification remain incomplete. Full V26/V20–V32 completion remains unproven.

### V26 LayerStack in the shared editor history/cache (2026-10-02)

EditorSnapshot now optionally carries a LayerStack. applyEditorCommand deep-copies and retains it across ordinary parameter/mask commands; an explicit null clears it. setEditorLayerStack/patchLayerCommand/moveLayerCommand require matching version/source SHA and validate the exact trusted V2 binding (including canvas). They reuse existing undo/redo, request generations, preview invalidation and 200-entry history, without a separate store. EditorDraftCache now validates/preserves cached stacks and rejects version/source SHA mismatch; a subsequently loaded server V2 binding remains required before layer commands/save.

Verification: full photographer run passed 181 tests with two external-fixture skips; TypeScript passed. Three targeted layer-command/cache tests passed afterward: shared undo/redo and preview cancellation, binding rejection/undoable removal, and cached stack preservation/source rejection. Layer UI, V2 draft baseline/CAS coordination and actual renderer/loupe integration remain outstanding. Legacy draft saving still does not persist V2 layers; this foundation does not claim otherwise. Full V26/V20–V32 completion remains unproven.

### V26 controlled layer panel (2026-10-02)

EditorLayerPanel provides enabled/opacity/blend/reorder controls with RU/BG/EN labels. It consumes the existing EditorState plus trusted V2 binding, routes edits through patchLayerCommand/moveLayerCommand and returns the full shared history state. Invalid canvas/source/geometry order changes show an error and leave history unchanged. It does not create parameter/mask artifacts or substitute legacy render behavior.

Verification: TypeScript and four targeted tests passed across actual RN-web panel DOM and shared layer commands/cache. The panel test verifies opacity undo, rejected geometry move without state mutation and layer disable. The component is not yet mounted in the editor route: loading the LayerStack artifact referenced by a recipe requires an authorized artifact-to-document resolution contract; the existing document endpoint addresses document IDs. V2 CAS/history acknowledgement and full rendering remain incomplete. Full V26/V20–V32 completion remains unproven.

### V26 authorized asset-bound LayerStack reference read

GET /assets/{id}/v2-layers/{artifact}?sha256 resolves the exact LayerStack artifact ref in the authorized current asset project, rather than interpreting an artifact ID as a document ID. GetLayerStack obtains the current verified WorkingImage binding, requires editor access/current V2 draft stream, resolves a ready/verified/version-pinned nonexpired LayerStack document with matching SHA in that project, uses GetDocument for exact immutable bytes/fresh ACL, then ValidateLayerStack for current source/canvas. The HTTP response is no-store. OwnerAPI.v2Layers snapshots the expected ref/binding and readLayerDocument independently verifies canonical SHA after structural validation.

Normative endpoint/API behavior documentation updated. Verification: Go v2draft/httpapi and TypeScript passed; five layer tests passed including pinned-SHA/tampered-document rejection; documentation checks passed with 54 endpoints/81 entry points. The new SQL/role path and object storage transport still require live integration; no production migration was applied. Editor-route mounting/V2 save coordination remain outstanding. Full V26/V20–V32 completion remains unproven.

### V26 LayerStack live API-role read and pinned client load

The existing approval-read fixture now creates a genuine LayerStack document/artifact in the authorized draft project. GetLayerStack is invoked via photo_api against full isolated migrations, returns exact canonical bytes, and rejects changed expected SHA, corrupted bytes and unregistered auth session. Existing approval/descriptor checks remain in the same test. Verification: tests/integration/test_v26_layer_read_go.py passed (5.63 s). Storage is a controlled ObjectReader; MinIO version transport and cross-project revocation races remain unproven.

loadEditorV2Layers snapshots the editor version/source pins, reads draft and binding with AbortSignal, validates immutable draft stream identity/revision/current document ID, retrieves that exact recipe, verifies its canonical SHA and complete source equality, then loads the referenced LayerStack or validated empty stack for null. It returns defensive copies without modifying editor state or requesting master pixels. Three tests passed for exact pin loading, changed recipe digest rejection before dependencies and cancellation before recipe read; TypeScript passed.

Actual editor-route panel mounting and V2 save acknowledgement/conflict handling remain outstanding. Full V26/V20–V32 completion remains unproven.

### V26 layer panel mounted in the editor route

The editor route now exposes Load V2 layers and mounts EditorLayerPanel after loadEditorV2Layers validates current source/recipe/stack pins. Layer IO has a synchronous lock, AbortController and scope generation over origin/tenant/asset/version/API. Old-scope responses are ignored; a changed EditorState object while reading rejects installation. Existing local/cached layer changes (including explicit null removal) are preserved and revalidated against the new trusted binding. No source master pixels are downloaded. Panel changes use the shared editor store and cached snapshot; layer-only undo/redo now forces a panel refresh even when legacy params are unchanged. The interface explicitly labels layer changes local until V2 save.

Verification: TypeScript and seven targeted tests passed across loader, layer commands/cache and panel DOM. The actual full editor-route load lifecycle still needs DOM/native regression coverage. V2 save acknowledgement/conflict handling, layer creation from qualified operation results and full composition preview/loupe remain incomplete; existing legacy save/render does not persist/render these layer edits. Full V26/V20–V32 completion remains unproven.

### V26 — frozen V2 layer save coordinator

Added `apps/photographer/src/editor-v2-save.ts`: captures a cloned request, separate V2 CAS revision and stable caller key; validates source binding and the combined 1 MiB request limit. Verified acknowledgements require the same draft stream and source, a strictly larger bigint revision, exact canonical recipe digest, unchanged recipe fields and the published layer-stack digest. An acknowledged mutation whose document cannot be verified raises `V2LayerSavedRefreshRequired`; callers must refresh rather than initiate another write. The coordinator never replaces local editor state or its legacy draft revision. OwnerAPI.saveV2Draft now accepts an explicit idempotency key and AbortSignal.

Validation: three targeted Vitest tests passed (frozen request, bigint revision and unchanged legacy revision; committed-but-unreadable acknowledgement; changed source rejection); photographer TypeScript check passed. This coordinator is not yet connected to the editor Save UI. Retry lifecycle, route scope fencing for saves, native UI and live backend composition remain to be implemented/verified. V26 and the overall V20–V32 plan remain incomplete.

### V26 — editor route V2 layer save action

The editor route now mounts Save V2 layers and explicit retry actions. An unresolved transport outcome retains the same frozen request/key and blocks reloading or layer-panel edits until retry. A verified response advances only the V2 baseline, preserving local history and legacy revision. Source/API scope changes abort and fence replies; terminal access/CAS errors require a fresh load. A committed-but-unverified response clears the write attempt and requires document refresh. Added RU/BG/EN messages explicitly distinguishing V2 layer persistence from the current legacy preview, which does not render these layers.

TypeScript and seven targeted loader/save/localization tests passed. Route save DOM lifecycle and live backend layer-save/render remain unverified; no full renderer qualification or V26 completion is claimed.

### V26 — mounted editor save lifecycle verification

Extended the actual editor route DOM suite with five V2-layer scenarios: lost-response retry preserves the request body and key and blocks reload; newer legacy recipe edits survive the V2 acknowledgement without changing legacy revision; switching photos aborts the request and ignores an acknowledgement even when transport ignores cancellation; an acknowledged write followed by document-read failure requires refresh and offers no new write/retry action; CAS rejection clears the stale baseline and requires reload. All nine editor route tests passed, including the existing four legacy conflict/cache cases; TypeScript passed. This is browser DOM verification with controlled API responses, not live backend, native-device or rendering qualification. Overall V26/V20–V32 completion remains unproven.

### V26 — protection binding preserves persisted layers

OwnerAPI.bindProtectionToV2Draft now reads and verifies an existing pinned layer document and submits its inline body when adding protection, preserving the stack instead of rejecting layered drafts. A failed stack digest/source validation prevents the write. Added two controlled API tests for preservation and corrupt-stack rejection; all 22 OwnerAPI tests passed. The editor serializes this operation with layer mutations, blocks it while a save outcome is unresolved, invalidates its V2 baseline after success and fences late status updates after scope changes. TypeScript passed. Live protection/layer composition and full renderer remain unverified.

Region loupe mounting remains pending: the client verifier requires authoritative admitted-plan parameters (canvas, alpha, params SHA and ICC SHA), while the current submit response contains only a job ticket. Do not derive this proof from the untrusted output manifest or bypass the existing verifier. An authorized descriptor read must precede mounting the verified result path.

### V26 — regional preview descriptor foundations

Added server RegionDescriptor projection from verified FrozenRegionPlan evidence. It binds asset/request/source, full source canvas, Params SHA, server-supplied output ICC SHA and alpha without disclosing object locations, input inventory or frozen recipe. Corrupt plan bytes, invalid ICC and canvas hash mismatch fail. This projection does not prove admission or access; an authorized admitted-job reader must do that before exposing it.

Added strict client readRegionDescriptor parser: exact public fields, requested asset/source/request equality, bounded canvas geometry, valid lowercase digests and defensive copies. Two Go descriptor tests passed as part of the complete internal/v2region package; two targeted client tests and TypeScript passed. The HTTP reader, persisted-node/object evidence reconstruction, normative endpoint contract and editor loupe mounting remain pending. No live render or V26 completion claimed.

### V26 — persisted-node descriptor reconstruction

Added RestoreRegionDescriptor for the future authorized admitted-job reader. It checks bounded canonical node JSON, exact node fields, task/tenant/asset/project binding, Params SHA, and exact equality of source/recipe object locators, versions, sizes and refs with freshly authorized metadata. It reads only pinned bounded JSON documents, verifies their actual SHA and source binding, rebuilds the original plan and requires byte-for-byte equality before projecting public evidence. Corrupt object bytes and cancellation fail; scope, version and malformed-node rejection occurs before object IO.

The complete Go internal/v2region suite passed with the new reconstruction test (positive round-trip and scope/version/canonical/body/cancellation failures). Object IO is controlled in memory. SQL live-access/sealed-admission proof, HTTP endpoint and editor mounting still require implementation; this helper by itself grants no authorization. Overall V26/V20–V32 remains incomplete.

### V26 — descriptor SQL admission boundary and service reader

Added forward-only migration 00381 with read-only SECURITY DEFINER photo.v26_region_descriptor_node, executable only by photo_api. It requires matching tenant context, current editor authorization, a sealed noncancelled V2 preview run, matching task/node/runtime profile and ready input closure, plus ready verified version-pinned nonexpired Params whose digest matches the exact stored plan. It returns internal node evidence only to the server service. Added Service.ReadDescriptor to resolve freshly authorized source/recipe metadata and call the independently verified reconstruction path. No HTTP route or production migration rollout yet.

Isolated PostgreSQL applied all migrations: eight existing seal-scope cases passed; the new boundary test passed for missing/foreign run and actor scope and API-only execution grants. Go internal/v2region passed. These SQL cases do not prove a successful admitted descriptor read; populated positive-read, closure corruption and access-revocation coverage still need implementation before claiming the endpoint ready. Renderer remains disabled/unqualified; V26 and V20–V32 remain incomplete.

### V26 — populated descriptor SQL success and revocation

Extended descriptor SQL integration coverage with a populated sealed-run/node/task/input-closure metadata fixture. The authorized read returns the exact project/task/node; foreign actor/session/asset, revoked session, cancelled run, unverified/expired Params and a missing closure chunk fail. Strengthened migration 00381 to require every registered input artifact to exist with matching digest, ready/verified state, actual object version and no expired lifetime. Added unverified chunk and expired manifest rejection. Both descriptor SQL tests passed after all migrations in isolated PostgreSQL (5.30s). This is SQL metadata authority coverage; fixture documents are not renderer outputs or live objectstore byte verification. Combined live SQL-to-objectstore ReadDescriptor and HTTP/contract/client wiring remain pending; no production rollout or V26 completion claimed.

### V26 — descriptor HTTP route and scoped reader

Added GET /api/v1/tenants/{t}/assets/{id}/region-previews/{run}/descriptor. It requires tenant editor authentication, configured object reader and server sRGB output-policy SHA. Missing or invalid admitted evidence returns 404; successful reads expose only RegionDescriptor. Added OwnerAPI.regionDescriptor with frozen request/source and strict verification. ReadDescriptorScoped performs DB-only tenant transactions before and after bounded object IO, repeats live access and input metadata checks, and requires identical evidence; object IO never runs inside a retryable SQL callback.

Go v2region/httpapi packages and photographer TypeScript passed. Route registration, unauthenticated rejection and private/no-store headers passed in targeted HTTP tests. Normative schema/endpoint regeneration, populated HTTP-to-objectstore integration, access revocation during IO and editor loupe mounting remain pending. These package tests do not prove a successful live HTTP descriptor read or qualified rendering. Overall V26/V20–V32 remains incomplete.

### V26 — normative descriptor API and captured client evidence

Added RegionPreviewDescriptor to spec_v22.py and GetRegionPreviewDescriptor (editor GET) to the normative endpoint catalog; regenerated schemas/OpenAPI/function indexes. Documented current authorization, no-store behavior, sealed/input evidence, DB-only transactions around pinned object reads, final live recheck, error behavior, private-data omissions, canvas constraints and the distinction between saved V2 recipe and unsaved edits. Documentation check passed deterministically: 172 schemas, 55 endpoints, 82 entrypoints, 2138 static checks and 54 semantic tests. These are documentation gates, not runtime qualification.

Added an OwnerAPI contract test which mutates caller request/source while descriptor transport is pending; the parser still checks the captured originals and forwards AbortSignal. OwnerAPI/descriptor suites: 25 tests passed; photographer TypeScript passed. Positive HTTP-to-versioned-objectstore integration, revocation during object IO, final-canvas compilation for geometric operators and editor loupe mounting remain pending. No V26 or overall V20–V32 completion claimed.

### V26 — verified loupe loader and explicit retry UI

Added createRegionLoupeLoader: captures request/source/key once, retries unknown admission with the same key, retains the validated ticket after acknowledgement, rereads authorized descriptor on output retries and composes existing output polling, manifest/byte checks and decoder. Descriptor failure or aborted admission prevents output download/decoding. Its input recipe must be an authorized FrozenRecipe, not an EditingRecipeV3 draft; the missing general draft-freeze path must not be bypassed.

RegionLoupe now offers localized explicit retry after failure, invoking the same loader/attempt and cancelling the prior lifecycle; verified result disposal remains scope-owned. Three orchestration tests and the loupe/localization DOM suites passed (nine targeted tests total); TypeScript passed. Orchestration tests use controlled downstream stages; they are not full pixel/HTTP/MinIO evidence. Editor mounting, general V2 draft freezing, final geometry qualification and populated HTTP objectstore integration remain pending. V26/V20–V32 remains incomplete.

### V26 — complete authored render recipe freeze foundation

Added FrozenRenderRecipe and BuildFrozenRenderRecipe, separate from the existing scalar V23 FrozenRecipe. The compiler retains exact canonical EditingRecipeV3 bytes and SHA (including layer/protection fields), resolved scalar evidence and pinned learning provenance. It checks referenced LayerStack bytes/source, canonical authored input, expected learning pins and combined 1 MiB output cap. It uses existing ordered correction resolution and style bounds; scalar correction is applied once, not again from retained provenance fields. It does not prove ACL, protection-mask bytes, operation qualification or closure by itself.

Go internal/v2draft passed with a new preservation/digest/canonical/extra-learning-dependency test. Added both normative schemas and execution semantics; documentation regeneration/check passed (174 schemas, 55 endpoints, 82 entrypoints). Artifact publication, independent worker decoder/composition, source/recipe admission expansion, full geometry qualification and editor rendering remain pending. This foundation must not be confused with a working full-layer preview; V26/V20–V32 remains incomplete.

### V26 — independent worker full-recipe decoder

Added strict worker EditingRecipeV3/FrozenRenderLearning/FrozenRenderRecipe models and decode_frozen_render_recipe. Validation requires canonical bounded duplicate-free bytes, exact fields, authored recipe SHA, source/style equality with scalar evidence, nonzero authored refs and complete learning provenance pinned to the authored version. Exact canonical reserialization rejects explicit-null optional protection and other lossy representation changes. No scalar-only fallback or implicit rendering is installed.

Nine worker tests passed, including composition preservation, digest/source/style/extra/null-protection/learning failures, canonical/duplicate-key failures and positive/invalid learning provenance. The actual Go BuildFrozenRenderRecipe output was piped into the Python decoder using PHOTO_WORKER_PYTHON; targeted Go cross-language test passed and required canonical output bytes to remain identical. Cross-language coverage runs only when that environment variable is supplied. General publication/admission, complete nested closure checks and worker composition dispatch remain pending. No live full-layer preview or V26/V20–V32 completion claimed.

### V26 — independent worker LayerStack validation

Added strict RenderLayer/RenderLayerStack models and decode_render_layer_stack for source-bound full-recipe composition. Checks include exact canonical bounded bytes and artifact SHA, source binding, nonzero unique layer/params/mask IDs, max 32 layers, UTF-8 operation/version byte caps, qualified structural mask expressions on the pinned canvas, max 256 mask-node occurrences across the entire stack and active geometry-prefix ordering. Existing mask DAG compilation remains authoritative for individual expression depth/shape validation. Disabled/zero-opacity geometry is not treated as an active transform.

Sixteen targeted Python tests passed across full recipe and layer suites, including layer preservation/digest substitution, duplicate IDs, geometry order, canvas mismatch, zero params, unknown fields and aggregate mask budget. Full nested artifact closure loading, operation/model qualification, protection pixel proof and composition dispatch are still pending. This parser is not a rendering implementation or complete V26 proof; V20–V32 remains active.

### V26 — bounded full-render metadata input loader

Added read_render_document over TaskArtifacts.stream (declared closure membership), with cancellation before/during/after IO, a 1 MiB streaming cap, independent actual byte SHA and owned stream closure. Added load_frozen_render_inputs for strict root recipe, WorkingImage canvas binding, verified LayerStack and protected MaskRef geometry/transform lineage. An unprotected mask in the protection slot fails. Caller must admit resources/runtime first; this metadata loader does not verify mask pixel arrays, layer Params, style/model qualification or the full operator closure yet.

Eighteen prior targeted tests passed; three loader tests passed after adding a populated root/source/layer/protection metadata case and explicit unprotected-mask rejection (nineteen total targeted cases across the three files). Controlled streams/contracts were used; TaskArtifacts/Gateway live IO and composition execution remain pending. This is incremental input loading, not a complete renderer or V26/V20–V32 completion.

### V26 — typed qualified layer Params loader

Added RenderLayerOperator/LoadedRenderLayer and load_render_layer_params. A runtime-owned provider supplies strict Params models, pinned runtime SHA, runtime verification and source-specific binding validators. All referenced operator/version pairs are preflighted before Params IO; unsupported disabled layers are not silently dropped. Params read through declared TaskArtifacts streams, actual SHA and canonical/strict model checks, with an aggregate document-byte cap and cancellation fences. Shared documents are read once per typed operator; each returned layer/Params is deep copied so later mutation cannot alter another layer's input.

Twenty-two targeted worker cases passed across Params, metadata, LayerStack and full recipe suites. Params tests prove runtime checks precede IO, shared-document deduplication without mutation aliasing, unsupported disabled-layer rejection, source mismatch and aggregate cap. Installed operator registration/qualification, learning/style evidence, nested mask/NPY checks and full composition dispatch remain pending. Controlled Params handlers in these tests are not real renderer qualification. V26 and V20–V32 remain incomplete.

### V26 — pinned learning contribution for full render recipe

Added load_render_learning_prediction. It reads declared bounded CandidateProfile and DescriptorShard bytes, verifies actual digest and canonical duplicate-free JSON, resolves exactly one matching ordinal/source binding and rejects duplicate descriptor ordinals. It recomputes scene12_v1 prediction with Go's ordered scalar accumulation and the existing per-coordinate model-output bounds. Missing learning makes no artifact reads. This returns only the learned contribution; it must be combined once with session/overrides/authored delta and checked against the frozen scalar result before execution. SQL approval/training provenance and analysis/shard ownership remain admission responsibilities.

Twenty-eight targeted worker tests passed, including six learning cases for positive prediction/clamp, changed binding/ordinal/duplicate/actual bytes and absent learning. No approved-model SQL-to-worker end-to-end proof, full scalar reconciliation, style/operator closure completion or composition execution yet. V26/V20–V32 remains active and incomplete.

### V26 — independent ordered scalar reconciliation

Added load_render_session_delta to read a declared canonical CorrectionPlan, require ordinal-complete unique source inventory and exact current binding, and reject nonzero manually locked/no-change items. Added verify_render_scalar_evidence: resolves session and learning contributions, requires caller-resolved override refs in authored order, combines session→learning→overrides→parameter delta with final CorrectionVector bounds and requires exact equality with frozen effective_delta. No second scalar application or implicit clamp is permitted. OverrideV1 is a full module document; typed source/style resolution and full module merge remain separate responsibilities and are not replaced by this scalar check.

Thirty-three targeted worker cases passed before adding the final override case; all six scalar tests then passed. Coverage includes positive authored/session sums, frozen mismatch, locked/nonordinal/wrong-source plan rejection, ordered override evidence and final bounds. Full OverrideV1 materialization/resolution, approved provenance, composition dispatch and live rendering remain pending. V26/V20–V32 remains incomplete.

### V26 — full OverrideV1 metadata and ordered module replacement

Added load_render_overrides: bounded declared-input documents, actual SHA/canonical duplicate-free JSON, full strict OverrideV1 validation, original source-canvas SHA (not WorkingImage JSON SHA) and inline mask canvas/transform lineage checks. Documents retain candidates, masks, polygons and landmarks for dedicated stages. Repeated documents are read once with owned copies per authored occurrence and an aggregate byte budget.

Added merge_render_override_modules using existing style.resolve semantics: each non-null module replaces the preceding module in authored order; validate complete StyleV2 after each merge, preserve unrelated fields and original inputs. It does not execute mask/landmark/candidate effects or turn arbitrary modules into scalar vectors. Thirty-seven targeted worker tests passed, including three full-override cases for ordered replacement, preserved unrelated fields and candidate metadata, deduplicated IO without aliasing, wrong source/budget rejection and complete global module preservation.

Full scalar-to-module ordering integration, mask pixel/provenance validation, geometric override execution, installed operator qualification and complete render dispatch remain pending. No V26/V20–V32 completion or full preview qualification claimed.

### V26 — shared layer/protection mask pixel mappings

Added resolve_mask_input_plans while preserving resolve_mask_inputs as the existing single-plan wrapper. Multiple source-bound plans share MaskRef/NPY mappings, retain existing complete float32 probability validation and canvas/transform proofs, and enforce bounded plan/ref inventory. Added resolve_render_masks for active authored layer masks and optional protection: checks unique-input byte estimate before IO, shares arrays, returns bounded tile evaluators and closes mappings/lifetimes on exit. Geometry changes still require explicit transformed inputs; coordinate correspondence is never inferred.

Fourteen targeted tests passed across the new full-render mask suite and existing layer mask/input suites. New cases prove two layer expressions read/map a shared mask once, post-close callbacks fail, and inadequate input budget rejects before contract/file reads. Existing mask/array/consumer-failure cleanup remains green. Protective-mask integration, geometry transformation stages, full operator registration and complete render dispatch remain pending. V26/V20–V32 remains incomplete.

### V26 — sequential same-canvas adjustment pixel phase

Added compose_render_adjustments using the existing validated prepare_layer/compose_layer_tile algorithms. Qualified proposal factories receive read-only borrowed current RGB/alpha; active layers execute in authored order with opacity/blend/protection. Each output is mapped under an owned reservation; the preceding intermediate is released after replacement, with cleanup on proposal failure or consumer exit. The helper returns scoped pixels only, not staged artifacts or inherited report/generation lineage. Active optical geometry is preflight-rejected pending its explicit transform phase.

Extended shared-mask tests to cover protection and layers sharing one verified array and cleanup after consumer failure. The adjustment tests prove ordered accumulation, exact protected HDR/negative-zero bits, unchanged source input, read-only results and operator-failure cleanup, plus geometry rejection before proposal IO. Existing layer preparation/blend/mask/input tests were included and passed. Runtime operator registration, geometry stages, provenance aggregation/publication and full-draft render admission remain pending; this same-canvas pixel phase is not the complete renderer or V26/V20–V32 completion.

### V26 — generated coverage and parent review evidence

AdjustmentPixels and sequential adjustment preparation now carry generated dependency coverage through existing generated_coverage logic. RGB equality does not erase generated provenance; full normal replacement by nongenerated proposal removes the prior generated dependency where unprotected. Generated arrays are owned read-only mappings with the same intermediate cleanup lifecycle. A targeted case verifies both behaviors with unchanged RGB.

Added collect_render_parent_reports to retain deduplicated pinned parent report roots, require exact original source binding, reject conflicting pins and fail/unavailable statuses, and inherit every review gate. Parent ImageResult identity and nested report provenance must still be proved by the caller. Seven report/adjustment tests passed; the expanded adjustment and existing preparation/blend suites also passed (21 cases). Publication retaining these report roots, final changed-mask aggregation, geometry execution and full renderer admission remain pending. V26/V20–V32 remains incomplete.

### V26 — final same-canvas changed-mask evidence

Added prepare_render_changes to compare original and final float32 RGB/alpha in bounded tiles, retaining bit-exact RGB differences including signed zero. Missing alpha is interpreted as opaque for semantic comparison. The result is an owned float32 changed-mask with changed-pixel count and RGB/alpha flags; it does not union transient intermediate changes. Any positive protected coverage rejects a changed final pixel. Finite RGB, alpha range, shape, canvas size and tile-memory limits are checked; scratch mappings and reservations close on errors, consumer failure and cancellation.

Nineteen targeted changed-mask, adjustment, shared-mask and existing layer-preparation tests passed. Five changed-mask cases cover RGB bits/alpha, equivalent opaque alpha, tiny positive protection, invalid input/budget, consumer failure and cancellation cleanup. This helper is not yet connected to staged ImageResult publication. Geometry comparison, qualified operator dispatch, full-render admission and the remaining V20–V32 plan remain incomplete.

### V26 — composed pixels and final evidence in one owned scope

Added compose_render_pixels connecting sequential adjustment composition to final changed-mask preparation. It compares the admitted original source against the final result, including upstream base-style changes, and holds both output pixels and changed-mask reservation alive for caller staging. A final protection check runs even with zero layers. Caller must establish original/base canvas, color and lineage equivalence; base-style execution, geometry and publication are not implemented by this helper.

Twenty-one targeted composition, changed-mask, adjustment, shared-mask and existing preparation tests passed. New integration cases show a layer restoring original pixels yields an empty changed-mask while generated dependency remains present, base-style-only differences are counted, protected base-style differences fail, and intermediate/final scratch files are removed. Qualified renderer integration, full artifact publication and V20–V32 completion remain pending.

### V26 — original/base pixel preflight before composition IO

compose_render_pixels now validates both admitted original and base arrays before proposal execution or scratch allocation: matching float32 RGB shape, bounded canvas, alpha/generated float32 dimensions and probability range, finite RGB, tile size and conservative tile-memory budget. Validation scans bounded tiles with cancellation checks and accepts finite HDR/negative RGB. This closes the no-active-layer path that previously bypassed generated-coverage validation. Canvas/color/lineage contract admission remains an upstream requirement.

Twenty-seven targeted tests passed. Six added cases reject mismatched shapes, nonfinite/out-of-range generated coverage, wrong alpha dimensions, nonfinite RGB and insufficient budget before any proposal invocation or output file. Complete renderer dispatch, geometry and publication remain pending; V20–V32 remains active.

### V26 — declared source pixel materialization

Added load_render_source_pixels to map declared WorkingImage float32 RGB and alpha through TaskArtifacts.input and existing strict array validators. It admits bounded input bytes before IO and owns mappings through cancellation/consumer exit. Legacy uint16 alpha normalization is explicitly selected and uses existing layer_alpha; its representation-change flag is returned for publication. uint16 RGB still requires an explicit color-conversion stage. Actual pin/SHA proof remains TaskArtifacts responsibility, not a property of controlled test doubles.

Two source cases passed for unchanged float32 alpha and explicit uint16 normalization, HDR RGB/read-only mappings, ownership cleanup and insufficient budget rejection before IO. The combined source/composition suite passed (29 cases). Full source-to-qualified-renderer dispatch, color conversion admission, result publication and the remaining V20–V32 scope remain incomplete.

### V26 — owned same-canvas preparation integration

Added prepare_render_pixels joining declared source RGB/alpha materialization, shared layer/protection masks, caller-qualified base-style proposal, sequential layer composition and final changed-mask in one scope. It verifies loaded layer inventory equals authored inventory before IO and rejects active geometry pending its explicit stage. Input, mask, base proposal and final output lifetimes remain open through caller staging; alpha conversion representation evidence is retained.

Thirty targeted preparation/source/composition/change/mask/layer tests passed before the final geometry preflight guard; the complete suite was rerun afterward. The new integration case uses actual NPY RGB/masks and composition algorithms, verifies ordered two-layer pixels and changed count, shared mask mapping, owner close order and missing-layer rejection before input IO. Provider qualification, real base-style execution, geometry, staged publication and full V20–V32 completion remain pending.

### V26 — aggregate render foundation verification and failure ownership

Ran the complete currently implemented frozen-render foundation together: authored recipe, bounded inputs, layer stack/typed Params, learning/session scalar evidence, full overrides, parent reports, source arrays, mask sharing, pixel composition and final changes. All 74 tests passed. This is combined unit/controlled integration evidence, not production qualification, SQL admission-to-render end-to-end proof or RTX3060 quality/performance evidence.

Added preparation failure cases using actual NPY inputs: base provider failure closes source/mask inputs, consumer failure releases final and intermediate files, and active geometry rejects before any materialization. Full installed renderer, geometry execution, source lineage/report-root publication, editor preview wiring and remaining V20–V32 requirements are still outstanding.

### V26 — source generated lineage retained in preparation

Source pixel loading accepts an optional caller-admitted ImageResult and exact source document ref, rejects mismatched parent identity before IO, includes generated-mask bytes in admission, validates matching canvas/transform and generated selector, and maps complete binary float32 coverage through declared inputs. prepare_render_pixels forwards this lineage; base providers receive original generated coverage and must preserve dependencies correctly. Parent report/provenance admission is still caller responsibility.

Ten source/preparation/adjustment cases passed, including a new actual-NPY generated parent case proving retained read-only coverage, fractional-coverage rejection, cleanup and mismatched parent rejection before materialization. Full publication and qualified base-provider lineage behavior remain pending. V20–V32 remains incomplete.

### V26 — source quality gate integrated before pixel IO

prepare_render_pixels now verifies optional source ImageResult identity and evaluates its pinned ImageReport before any source-array materialization. Failed/unavailable reports reject preparation; exact original binding is required. PreparedRender retains source report refs and inherited review_required for the future publication stage. Base/proposal reports still require provider evidence integration; retained source evidence alone is not a complete composed quality report.

Fourteen preparation/source/report tests passed. New cases prove fail/unavailable source reports cause no pixel IO and a reviewed source remains reviewed with its pinned root available through preparation. Full report-root publication and complete V20–V32 scope remain pending.

### V26 — owned final array files for fenced staging

Added prepare_render_output_files: bounded tiled copies of scoped final arrays into a dedicated scratch reservation, flush/close mappings before yield, and validate files for staging. RGB is materialized only for final RGB changes; alpha only for semantic changes or explicit representation conversion. Generated coverage is preserved independently of RGB equality. Final changed-mask retains its existing separate owner, so caller must retain preparation scope through staging.

Twenty-one files/preparation/source/composition cases passed. Three file cases cover exact HDR/signed-zero RGB, unchanged-reference reuse, alpha representation conversion, generated coverage and cleanup after consumer/staging failure. This materializes files but does not stage/adopt ImageResult or publish inherited report roots. Full renderer and remaining V20–V32 scope remain incomplete.

### V26 — fenced image and mask artifact staging

Added stage_render_image using existing TaskArtifacts.stage/stage_contract interfaces and the actual owning reservations for final arrays and changed-mask. Retains source canvas/color/transform, reuses original WorkingImage ref when RGB/alpha are unchanged, stages generated coverage independently, and returns pinned refs/output inventory/exact byte accounting. It deliberately requires the caller to retain quality/provider provenance and construct/adopt the complete result root; no fabricated pass report is produced.

Five staging/file tests passed after correcting the test reservation to fit its existing scratch quota. Two controlled staging cases use actual NPY bytes and canonical contracts to verify geometry preservation, unchanged source reuse, changed/generated descriptors and byte inventory. Live gateway/object-store adoption, composed report publication and full renderer remain pending; V20–V32 remains incomplete.

### V26 — prepared pixels through final materialization and staging

Added controlled end-to-end same-canvas preparation→owned final files→artifact staging evidence using actual NPY arrays, existing pixel algorithms and canonical contracts. The case checks resulting WorkingImage, changed/generated MaskRef documents, exact staged byte inventory and complete scratch cleanup. Thirteen preparation/staging/file cases passed. The controlled gateway does not establish object-store fencing/adoption or runtime/provider qualification.

Architecture inspection confirms ImageReport currently has no typed parent-report references in Python optical contracts or Go imageReportRefs/DocumentChildren. Publishing retained parent roots requires a coordinated contract/spec/closure extension (or another explicitly supported typed result root); merely carrying them in an unreferenced outputs list is insufficient. Complete ImageResult publication remains pending for this reason, alongside provider reports, geometry and remaining V20–V32 requirements.

### V26 — typed parent quality-report roots

Extended ImageReport with optional ordered parent_reports (0..33). Legacy serialization omits the field and retains old document bytes. Python rejects explicit null/duplicate/zero pins; Go validates pins, rejects duplicate/self references and exposes typed ImageReport children to existing closure traversal. Normative generated schema updated; 2150 static and 54 semantic documentation checks passed deterministically using the project virtualenv. System Python lacked jsonschema; no installation was attempted.

Go ImageReport tests passed; Python report/preparation tests passed, followed by legacy layer-result and explicit optional-field tests. Complete composed-report construction, recursive quality-status proof, live closure adoption and full V20–V32 implementation remain pending.

### V26 — bounded ancestor quality DAG validation

collect_render_parent_reports now iteratively validates every typed parent_reports ancestor, rejects cycles/conflicting pins, bounds unique documents (1024) and path depth (32), and inherits review gates from all ancestors. It retains only ordered direct pinned roots for publication and reads shared ancestors once. Cached subtree heights enforce depth budgets even when a shared subtree is reached via a longer path. Declared artifact membership/actual bytes remain TaskArtifacts responsibilities; ImageResult-to-report identity is caller admission.

Fourteen report/preparation tests passed, including nested review, nested fail, cycle and node/depth-budget rejection. Complete composed-report construction/adoption, qualified renderer and remaining V20–V32 work remain pending.

### V26 — complete typed ImageResult root staging

Added stage_render_result to stage caller-measured ImageReport plus ImageResult over staged image/mask refs. It revalidates provider report wire, exact source binding and ordered direct parent pins; rejects fail/unavailable and prevents inherited review becoming pass before any staging. Result returns exact newly produced output refs/byte accounting. Quality metrics are supplied by qualified providers, not manufactured by this helper; source/base/layer parent inventory and graph adoption still require caller proofs.

Thirteen result/report/image-stage cases passed. Five result cases cover retained roots/review, report linkage and exact accounting, and dropped parent/pass-over-review/fail/substituted-binding rejection before IO. Installed provider/report aggregation, live gateway closure adoption and remaining V20–V32 work remain incomplete.

### V26 — Go typed ancestor report closure verification

Extended the existing scoped ImageResult closure fixture with an actual SHA-verified parent ImageReport document. The test verifies the nested report is traversed as ImageReport, included in dependency inventory and subjected to occurrence catalog lookup alongside its WorkingImage evidence. The complete Go v2draft package passed. This uses the controlled catalog/object reader; live SQL scope and object-store adoption of parent_reports remain unverified and require integration evidence before production enablement.

Existing Go closure traversal has active-path cycle rejection and a 32-depth/512-occurrence bound. Worker report traversal uses its own explicit unique-node/path-depth limits; no claim of full renderer or V20–V32 completion is made.

### V26 — full controlled pixel-to-ImageResult root integration

Extended the actual-NPY preparation/materialization/staging integration case through caller-supplied fixture quality metrics and stage_render_result. It verifies ImageResult→ImageReport/WorkingImage links, exact binding and full produced output/byte inventory while all temporary arrays remain owned and close afterward. Nineteen preparation/result/report tests passed; Go draft/render packages were also checked.

SQL review confirms generic v2_draft_artifact_scope deliberately does not authorize ImageReport/ImageResult. AdoptedLensCatalog uses exact adopted task/output occurrence proof for such children. New parent links do not justify broadening the generic allowlist: full composition must register/adopt exact output occurrences and expose ancestor report pins through equivalent qualified catalog authority. This integration remains controlled, not a live SQL/object-store full-render proof. Full provider execution, composed admission and remaining V20–V32 scope are still incomplete.

### V26 — existing layer.compose inherits ancestor quality gates

Existing layer parent_review now uses the bounded ancestor-report validator and exact VersionBinding, closing its previous asset-version-ID-only acceptance. It maps source/status failures to existing layer error codes. Layer runtime component lock now includes v2/render_reports.py so qualification proves the new transitive implementation bytes; old lock documents require regeneration and qualification, not bypass. Layer output report root persistence still needs coordinated output/adoption integration; this change establishes quality rejection/inheritance during execution.

Forty-nine layer result/parents/adapter/render-report tests passed. The added existing-layer case proves nested review inheritance and substituted ancestor canvas rejection. Full render dispatch, parent-root publication by layer.compose, qualified runtime rollout and remaining V20–V32 scope remain pending.

### V26 — layer.compose publishes and adopts exact parent-report roots

Layer result staging now writes ordered deduplicated source/proposal report refs into ImageReport.parent_reports, using the same validated evidence as review inheritance. Metadata adoption independently requires exact parent refs/order/SHA rather than accepting a correct status with dropped provenance. Updated the actual-byte adoption fixture to contain the required proposal-report root; the older fixture correctly failed the strengthened gate until its provenance was made complete.

Seventy-two layer result/adapter/parents/adoption/actual-byte/output-pixel/coverage cases passed. Existing legacy reports remain parseable; fresh adoption now requires retained parent roots. Runtime byte identity changes require qualification before deployment. Live SQL/object-store proof for nested retained roots and complete full-render provider orchestration remain pending; V20–V32 remains incomplete.

### V26 — generic adoption typed parent-report binding

Generic output traversal now binds ImageReport.parent_reports[*] to ImageReport/report/application-json. Declared input ancestors retain exact task-input authorization without becoming newly produced outputs; operation-specific layer verification separately reads their actual bytes and validates the ancestor quality DAG. Newly staged ancestors still face existing output role/count policies; the binding does not relax publication limits.

Generic actual-byte layer-adoption test now explicitly verifies the proposal report root is present in the final inventory. Layer adoption, render-result and report tests passed. Live SQL/object-store full-render admission/adoption and provider orchestration remain pending; V20–V32 remains incomplete.

### V26 — rehashed provenance substitution rejected by actual-byte adoption

Added actual-byte adversarial layer-adoption cases that omit, empty or replace output ImageReport.parent_reports and recompute both report and ImageResult SHA correctly. All three are rejected by metadata proof before pixel validation; valid hashes alone cannot remove caller-proved quality lineage. Input/output stream ownership and scratch cleanup remain checked.

Sixty-one actual-byte/generic adoption, adapter, result and ancestor-report tests passed. These establish the new retention gate with controlled evidence readers, not live tenant SQL/object-store authority or full renderer qualification. Remaining V20–V32 scope remains active.

### V26 — actual Python/Go ImageReport wire compatibility

Added TestImageReportWorkerWire invoking the actual configured worker Python runtime. Both legacy and parent_reports-extended Go canonical documents round-trip byte-for-byte through strict Python ImageReport, then Go DocumentChildren verifies exact typed parent extraction. Executed with PHOTO_WORKER_PYTHON pointing to the project virtualenv; ImageReport tests passed. Without the explicit runtime variable this cross-language test skips rather than implying coverage.

This proves wire/hash compatibility for these fixtures, not runtime qualification, live SQL adoption, full renderer or V20–V32 completion.

### V26 — measured quality aggregation with retained parent roots

Added build_render_quality: strictly revalidates caller-measured final report, exact source binding and allowed pass/review status; validates full ancestor DAG and assembles direct parent pins. Preserves clipping/error/identity/transform evidence and measured reason, promotes pass to review when required by any parent, and fills RENDER_PARENT_REVIEW only if no reason exists. Rejects contradictory provider parent inventory instead of overwriting it. Caller must prove inventory completeness and supply qualified actual measurements.

Sixteen quality/result/report cases passed. Four quality cases verify metric/reason retention, inherited review, input immutability and fail/parent inventory/source substitution rejection. Actual provider execution/measurement, full admission/adoption and V20–V32 completion remain pending.

### V26 — quality-gated prepared result staging orchestration

Added publish_prepared_render joining final measured-quality/ancestor validation, required retained source report pins, owned output materialization, image/mask staging and ImageResult root staging. Invalid quality or dropped source roots reject before artifact writes. This stages attempt artifacts only; caller retains preparation scope, proves complete source/base/layer provider inventory and performs qualified graph adoption separately.

Nineteen publication/preparation/quality/result tests passed. The actual-NPY preparation fixture now exercises the combined publication function and verifies complete root/output/byte inventory; additional cases reject lost source provenance or measured fail before materialization. Full provider dispatcher, geometry, live SQL/object-store adoption and remaining V20–V32 scope remain incomplete.

### Worker aggregate regression — 407 unit cases

Ran all tests/unit after report retention/adoption integration. Initial run found four stale learning.evaluate adapter fixtures missing pinned DescriptorShard evidence and two Go-cache sandbox write failures. Updated the fixtures to provide the existing descriptor_shard(item) matching actual training features; negative recipe-binding cases now reach their intended gate without bypassing descriptor verification. Repeated under granted repository write permission: all 407 unit cases passed, including Go training wire tests.

This is unit/controlled integration regression evidence. It does not prove PostgreSQL/object-store full-render integration, qualified models, RTX3060 performance or complete V20–V32 implementation. Those requirements remain outstanding.

### Aggregate Go regression and real MinIO parent-report version proof

All backend internal Go packages passed after report graph changes. Actual MinIO layer-composition proof passed for float32 and explicit legacy-alpha normalization. Extended it to corrupt newer versions of retained parent reports as well as root/pixels: original pinned report versions remain valid, replacement version pins fail, and scratch cleanup holds. Both expanded MinIO cases passed.

MinIO uses actual isolated versioned objects and PostgresObjectEvidenceReader with controlled exact catalog locations; live SQL authorization is intentionally outside this test. This does not qualify full renderer providers or complete V20–V32.

### SQL composition boundary regression and cached report path depth

Executed layer worker-admission, plan-shape and Params-shape suites against isolated PostgreSQL with all repository migrations: 28 cases passed. This verifies existing narrow worker grants, missing/altered lease rejection and validated plan/Params boundary; it is not new full-render SQL authority or composed parent-report output adoption proof.

Added an ancestor DAG case where a shared subtree is first visited along a short path and later through a longer path. Cached heights correctly reject depth 3 and accept depth 4; all eight report cases passed. Complete qualified full-render orchestration and remaining V20–V32 scope remain outstanding.

### V26 — conservative full composition resource estimate

Added render_resources for simultaneous original/base/intermediate/publication mappings, unique shared masks, bounded mask/composition tiles and metadata allowance. Requires an explicit four-field qualified provider peak and adds RAM/scratch/VRAM/output costs rather than assuming model costs are zero. Bounds canvas/counts/safe integers and rejects aggregate overflow. This is same-canvas accounting, not GPU/model qualification or installed runtime admission; integration into full task planner/admission remains required.

Resource case passed for 24 MP, exact provider additions, shared-mask increment, missing provider inventory and overflow rejection. Full geometry/provider profiles and V20–V32 completion remain pending.

### V26 — resource profile gate before render materialization

Added admit_render to validate sealed estimate fields/safe integer ranges/exact pixel count and enforce independent RAM/scratch/VRAM/output lower bounds. prepare_render_pixels accepts an execution profile and explicit qualified provider peak, compiles active masks/protection to count unique refs/nodes, and applies this gate before reports or array IO. Existing lower-level fixture use without a profile remains caller-admitted; full installed task dispatcher must require the profile and prove qualification rather than treating omission as production authorization.

Seventeen preparation/resource tests passed: exact admission, each resource one byte short, invalid profiles and preparation rejection without any artifacts implementation/materialization. Full planner/profile registration, qualified provider execution and remaining V20–V32 work remain pending.

### V26 — Go same-canvas full-render resource estimator

Added RenderResources/RenderProviderPeak matching the worker formula for mapped inputs/intermediates/publication, shared masks, tile buffers, explicit alpha normalization and qualified provider costs. Rejects canvas/count/mode/negative/safe-integer overflow; returns existing resource estimate wire fields. This is available to the future full-render planner and does not install/admit a provider by itself.

Go resource tests passed for 24 MP, exact provider additions and overflow/canvas rejection. Cross-language formula execution, planner/sealed profile integration and complete V20–V32 implementation remain pending.

### V26 — actual Go/Python resource formula parity

Executed TestRenderResourcesActualWorkerParity against project Python: 18 combinations of 1 pixel/24 MP/wide near-200 MP, tiles256/512/1024, strict/normalized alpha, maximum mask counts and explicit provider peaks. All estimate fields equal Go exactly. The test skips without explicit PHOTO_WORKER_PYTHON, so default Go tests do not claim this runtime evidence.

This is arithmetic/contract compatibility, not measured host capacity, GPU benchmark, provider qualification or full planner integration. Remaining V20–V32 scope remains active.

### V26 — strict server FrozenRenderRecipe decoder

Added DecodeFrozenRenderRecipe as the prerequisite for typed full-render dependency traversal. It requires canonical exact Go wire, bounded size, schema/renderer/scalar bounds and lock digests, exact authored source/style, recipe digest and learning presence/version/model/descriptor/analysis/ordinal consistency. Unknown/dropped/default-changing typed fields fail reserialization equality. Caller still revalidates resolved scalar/style bounds and actual admitted dependency bytes.

Frozen render tests passed, including generated compiler output acceptance and unknown-field rejection. Typed closure integration is the next pending step; no full renderer or V20–V32 completion claimed.

### V26 — typed FrozenRenderRecipe dependency extraction

Connected strict full-recipe decoder to DocumentChildren and closure traversal. Direct dependencies retain exact parent pins/path: WorkingImage with canvas proof, StyleV2, optional CorrectionPlan/LayerStack/protection MaskRef, ordered Overrides and learning CandidateProfile/DescriptorShard. Mirrored scalar source/style refs are validated by decoder and not redundantly emitted. Generic untyped traversal is disabled for this root.

All Go v2draft tests passed; compiler-output case verifies source/style/layers/protection dependency typing, exact refs, parent pin and canvas hash. Current SQL catalogs still need qualified full-recipe/model/descriptor authority; typed extraction does not grant that authorization or enable rendering. Learning nested contract validation, live closure integration and V20–V32 completion remain pending.

### V26 — learning pins and frozen-recipe substitution proof

Added compiler→decoder→DocumentChildren coverage for a learned full render recipe with ordinal4999. It retains exact CandidateProfile and DescriptorShard pins as typed children alongside source/style. Eight recomputed canonical tamper cases reject changed learning version, out-of-range ordinal, nil model UUID, missing learning, scalar style/source substitution, recipe digest replacement and invalid effective delta.

Frozen render Go tests passed. This proves immutable document semantics/direct dependency inventory, not SQL model approval, descriptor-analysis ownership, independent scalar recomputation or installed provider qualification. Full live closure/renderer integration and V20–V32 remain incomplete.

### V26 — strict nested learning dependency documents

Go DocumentChildren/closure now validate CandidateProfile through existing strict dimension/value decoder. DescriptorShard validates exact header, digest, bounded1..128 frame inventory, nonzero source bindings, unique ordinals and every complete frame through DecodeFrameFeatures; exposes typed source WorkingImage/canvas and FaceObservations pins under exact parent. Generic traversal is disabled for these documents.

All v2draft tests passed, including descriptor typed refs and duplicate ordinal rejection. FaceObservations content proof, analysis/ordinal ownership, SQL-approved learning authority and live full-render closure integration remain pending; V20–V32 remains incomplete.

### V26 — pre-edit face evidence validation in learning closure

Extracted ValidatePreEditFaces from the existing strict source-bound observation validator while preserving ResolveFaceID's selected-face requirement. The shared validator checks complete detector evidence, unique nonzero face/observation IDs, boxes/landmarks/embeddings/quality and rejects assigned person IDs pending separate identity proof. Go full-recipe closure now validates FaceObservations and exposes exact WorkingImage binding/canvas dependency.

Both observation and draft packages passed, including empty detector document source dependency and malformed face rejection. This validates pre-edit detector documents, not person association, consent, analysis ownership or model qualification. Full SQL/provider/renderer integration and V20–V32 remain incomplete.

### V26 — descriptor-to-face exact occurrence binding

Descriptor face dependencies now carry the expected complete frame VersionBinding. Closure traversal compares the referenced FaceObservations binding against that exact occurrence and rechecks repeated face documents instead of trusting earlier validation under another frame. Current catalog/byte authorization remains required separately.

Draft/render package tests passed. Added exact binding expectation and changed asset-version rejection cases. Live qualified learning/source catalog integration and full V20–V32 scope remain pending.

### V26 — full recipe bound to exact task source before child IO

Closure traversal now requires FrozenRenderRecipe.Scalars.Source to equal the complete task VersionBinding before exploring any nested dependencies. This prevents a valid immutable recipe from a different version being admitted merely because its internal source/style pins are self-consistent. Catalog authorization and independent scalar evidence remain separate gates.

All v2draft tests passed. A actual canonical/hash object fixture verifies a different task asset-version is rejected after root lookup with no child catalog IO. Full qualified catalogs/renderer/adoption and remaining V20–V32 scope remain pending.

### Learning model numeric null rejection

DecodeCandidateProfile now explicitly validates every means/scales/coefficient/intercept element as a finite JSON number, rejecting null (including whitespace) before Go fixed-array decoding can silently substitute zero. Existing value/dimension/feature-order/builtin constraints remain in force.

Both v2draft and v2learning packages passed. Four null mutations of otherwise valid canonical model documents are rejected. Full approved-model/source authority and V20–V32 remain incomplete.

### Learning descriptor numeric null rejection

DecodeFrameFeatures explicitly validates numeric ordinal/metrics and every scene_embedding/luma_quantile/nonnull neutral_rgb coordinate before Go decoding, sharing the finite-number helper with CandidateProfile. Nullable neutral_rgb remains supported; null array elements and null required metrics no longer silently become zero.

Both draft and learning packages passed. Seven canonical null mutations are rejected while existing descriptor/model/prediction tests remain green. Full qualified renderer integration and V20–V32 scope remain incomplete.

### V26 — bounded full-recipe closure occurrence budget

A 128-frame descriptor shard can exceed the legacy512 occurrence limit once every source/face/source-pixels occurrence is checked. Full roots containing FrozenRenderRecipe now use a bounded4096 occurrence limit with existing depth32/cycle/pin/authorization checks; legacy draft roots retain512. Generic nested JSON per-document limits are unchanged. This does not grant full-recipe catalog authority.

All draft tests passed, including a new513 repeated-root rejection after exactly512 catalog authorizations for legacy scope. A complete128-frame qualified full-root integration still needs evidence; resource/SQL admission and remaining V20–V32 work remain pending.

### V26 — full 128-frame recipe document closure

Added controlled actual-canonical-document closure test with compiled FrozenRenderRecipe, CandidateProfile, 128 complete frame descriptors and 128 exact empty FaceObservations, source WorkingImage and real style fixture. Traversal succeeds beyond512 occurrences under the full-root cap, visits every face dependency and performs catalog lookup for every recorded occurrence. Source pixel metadata is controlled; binary bytes are not inspected by this document test.

The initial test assumption (>512 occurrences) failed: actual traversal has390 occurrences because repeated source bytes avoid redundant descendant traversal. Removed the unjustified4096 cap extension and retained512 for all roots; updated the test to require390 and128 faces. This proves typed document traversal/budget, not live SQL approval/source authority, binary NPY/model qualification or complete renderer execution. Remaining V20–V32 scope remains active.

Correction: the earlier proposed4096 full-root cap was removed after actual128-frame evidence showed390 occurrences. Legacy and full roots both retain512; larger distinct-image inventories require separate evidence and explicit admitted bounds.

### V26 — distinct 128-frame closure and verified occurrence limit

Extended the actual-canonical-document full-recipe fixture to two inventories: a shared WorkingImage (390 authorized occurrences) and 128 distinct WorkingImage/pixel pins (517 occurrences). The distinct inventory failed under the previous512 limit. FrozenRenderRecipe roots now admit at most4096 occurrences; legacy roots retain512. Existing per-occurrence catalog authorization, depth32, immutable pins and cycle checks are preserved. A repeated full-root adversarial case rejects exactly at4096 authorizations before another catalog lookup; legacy513-root rejection remains covered.

This supersedes the preceding correction:512 is sufficient only for the shared-source fixture, not the distinct-image fixture. Targeted tests passed for both exact inventories and both budget boundaries. Pixel metadata is controlled, and binary NPY contents are not inspected by this document-closure test. The full recipe remains subject to separate SQL contract/model authority, qualified renderer installation and task resource admission. V20–V32 implementation remains incomplete.

Verification for this increment: all backend internal Go packages passed; both exact occurrence fixtures and boundary rejection tests passed after final assertions. Selected full-render Python recipe, learning, scalar, input, report, preparation and resource tests:49 passed. Documentation validation:2150 static checks,54 semantic tests and deterministic regeneration passed. These checks do not include production SQL publication of FrozenRenderRecipe, actual RTX3060 qualification or complete application execution.

### V26 — pinned complete style preparation

Added render_style.prepare_render_style to join actual pinned StyleV2 loading, production example-profile rejection, complete ordered OverrideV1 loading/module merge and independent scalar evidence reconciliation. PreparedRenderStyle retains base and merged styles, complete override decisions/landmarks and an owned effective delta; it does not collapse module replacements into scalar additions or apply corrections twice.

Four new tests passed: ordered complete module/decision preservation and owned delta; example rejection before override IO; scalar substitution rejection; actual pinned style-byte substitution rejection. This is metadata preparation, not an installed qualified pixel renderer. SQL approval, operator/model qualification, dedicated override decision stages and full V20–V32 completion remain pending.

Aggregate verification after style preparation:104 full-render foundation tests passed (recipe/inputs/learning/scalars/overrides/layers/masks/composition/resources/files/staging/reports/publication/style preparation). Documentation validation passed2150 static and54 semantic checks with deterministic regeneration. This aggregate covers lower-level fixtures, not installed production dispatcher, live full-recipe SQL authority or RTX3060 qualification.

### V26 — authored style wired into same-canvas pixel preparation

Added prepare_authored_render with required resource profile/provider peaks and explicit base runtime verification before document IO. Extracted admit_render_inputs for shared mask inventory/resource admission. The joined path resolves every authored typed layer, complete style/overrides and scalar evidence, then supplies PreparedRenderStyle explicitly to the base pixel provider and retains all pixel owners through consumer scope. Lower-level prepare_render_pixels keeps existing caller-admitted use.

Tests exercise actual pinned style/override documents, source NPY mapping, controlled base pixel provider and final changed-mask calculation; complete style/scalar/decision evidence reaches the provider, arrays close after scope. Separate resource/runtime/scalar failures reject before pixel materialization, with resource/runtime failure before document reads. The first fixture estimate omitted a protected-mask ref and correctly failed RAM admission; the no-protection fixture now removes that ref and revalidates the canonical frozen recipe. These are controlled provider tests, not qualification or dispatcher installation. Full V20–V32 implementation remains active.

Verification after authored integration:108 full-render foundation tests passed, including the revalidated canonical no-protection fixture. Documentation validation passed2150 static and54 semantic checks with deterministic regeneration. No production SQL admission or RTX3060 qualification is claimed.

### V26 — authored execution through measured result staging

Added execute_authored_render and typed MeasuredAuthoredRender to retain complete style/pixel preparation through mandatory final measurement and quality-gated ImageResult staging. Measurement receives exact PreparedAuthoredRender; invalid provider/evidence is rejected rather than manufacturing a passing report. Existing publication validates source/parent report retention and exact output accounting.

Four tests passed: actual pinned style/override documents and source NPY through controlled same-canvas provider, measurement-before-staging, pass/review root status, fail rejection before writes, complete output inventory/byte accounting and owner cleanup. Missing measurement provider rejects before preparation. Controlled report metrics are not model qualification. Dispatcher/SQL adoption, qualified production operators and remaining V20–V32 remain incomplete.

Verification after measured execution integration:112 full-render foundation tests passed. Documentation validation passed2150 static and54 semantic checks with deterministic regeneration. Production dispatcher/SQL and RTX3060 qualification were not exercised.

### V26 — independent full-render same-canvas metadata proof

Added render_adoption.verify_render_metadata using task-authoritative FrozenRenderRecipe ref, exact source binding and explicit provider parent-root inventory. It validates actual pinned recipe/document bytes, immutable input/output scope, source canvas, preserved image geometry/color/dtype, exact changed/generated mask metadata, parent DAG quality and output report operation/source/root order/review inheritance. Recipe bytes are rehashed before strict decoding. This helper does not authorize contracts, adopt results or prove pixel composition/metrics.

Three actual canonical-document tests passed: full source/recipe/output/parent metadata proof, task-source substitution rejection and dropped parent inventory rejection. Existing layer fixture supplies controlled binary evidence; this metadata test does not stream NPY arrays. Full binary recomposition proof, SQL wiring, dispatcher installation and V20–V32 completion remain pending.

Aggregate verification after metadata proof:115 full-render foundation tests passed. Production SQL adoption and binary recomposition were not exercised by this increment.

### V26 — streamed full-render output array proof

Added verify_render_output_arrays after authoritative metadata validation: <=200M pixel bound, source-exact input reuse, bounded staged NPY streams with canonical float32 headers, exact role/shape/size/SHA, finite RGB, unit alpha and binary changed/generated masks. Stream iterators close on failures. Existing lens array validator is reused without weakening its checks.

Eight adoption tests passed, including actual three-array output streams, corrupt bytes and rehashed fractional/NaN/out-of-range changed masks. This proves binary representation and immutable integrity, not exact composition/change-mask equality, measured ML quality, SQL adoption or production dispatcher. Remaining V20–V32 work stays active.

### V26 — independent exact final changed-mask and protection proof

Added verify_render_changes using actual immutable source/output arrays under ArrayEvidenceSession scratch quotas. It checks metadata and canonical staged arrays, compares original/final RGB and alpha bitwise by bounded tiles, treats absent alpha as opaque, supports explicit legacy input normalization and verifies changed-mask equality. Positive pinned protection coverage forbids any change; source canvas/transform/protected flag are independently checked.

Eleven adoption tests passed, including exact final coverage with cleanup, rehashed binary zero mask over changed RGB rejection and a real positive0.001 protection mask rejection. Scratch mappings and streams close after failure. This is change/protection proof, not full style/layer recomposition, generated provenance, SQL wiring, provider qualification or V20–V32 completion.

### V26 — prevent retained legacy alpha in float32 render output

Exact-change proof now rejects reuse of a uint16 source alpha pin in the float32 output WorkingImage even when normalize_uint16 is explicitly allowed for source comparison. The source normalization policy cannot silently legitimize legacy output representation. Float32 source alpha reuse remains supported.

Thirteen adoption tests passed, including actual uint16 source alpha with valid normalized staged output and rejection of rehashed output metadata retaining the legacy alpha pin. Array mappings/streams close after both paths. Full production dispatcher, SQL adoption, recomposition and qualification remain incomplete.

### V26 — pinned full composition build identity

Added RenderRuntimeLock for all current render orchestration/adoption modules plus existing layer/runtime binary inventory and style/override/learning dependencies. Verification revalidates lock inventory, canonical operator-pinned digest, Python patch/architecture, installed package versions and actual streamed component file hashes. Providers/model weights retain separate qualification requirements.

Six tests passed against actual local installed hashes/package versions, wrong pinned digest, substituted component hash, mismatched package/machine and incomplete inventory rejection. No external qualification approval or GPU performance is inferred from a self-created fixture lock. Production loading/dispatcher installation, SQL adoption and remaining V20–V32 remain incomplete.

### V26 — bounded canonical runtime lock file loading

Added load_render_runtime_lock: single descriptor, no symlink following, regular-file/single-link/64KiB bound, canonical duplicate-free JSON and full installed build verification against externally supplied pin. File/parse failures return the existing runtime mismatch code.

Fourteen runtime tests passed, including actual canonical local lock loading and symlink, hardlink, oversize, noncanonical JSON, missing file, wrong digest and duplicate-key rejection. This proves lock loading/build identity only; provider qualification, startup/dispatcher wiring and full V20–V32 completion remain pending.

### V26 — composition lock wired into authored execution

Added execute_pinned_authored_render requiring immutable composition lock path and externally supplied SHA. The gate runs after resource admission and before provider verification/any input document or pixel reads, then preserves complete preparation→measurement→staging ownership flow. Provider/model qualification remains independent and mandatory.

Nineteen combined execution/runtime tests passed. Pass/review/fail actual-document/NPY fixtures now use a real canonical lock with actual installed component verification; wrong digest rejects before provider callback and all input IO. These remain controlled-provider fixtures, not external qualification approval, dispatcher installation, SQL adoption or V20–V32 completion.

### V26 — enforce complete base provider module inventory

Moved existing complete RegionPreview module requirements into shared render_modules.required_render_modules and retained the region adapter's public alias. Full authored preparation now requires an explicit frozenset supported_modules and verifies coverage against the fully merged style before source pixel IO. Adaptive prototype local support and all requested extensions remain required even at zero strength. Composition runtime lock pins the shared helper.

Twenty-five focused authored/execution/runtime tests passed, including missing color/strength coverage and untyped inventory rejection before pixels. This closes a provider-contract omission, not actual module implementation/qualification. Full SQL/dispatcher integration and V20–V32 completion remain pending.

### V26 — materialize effective style before complete-provider coverage

Found and corrected a coverage gap: positive frozen retouch_delta may enable beauty after module merge. PreparedRenderStyle now retains a distinct effective style produced by existing materialize_frozen_style after independent scalar reconciliation. Provider coverage checks effective rather than merged; providers render effective without reapplying delta. Base/merged provenance and complete overrides remain retained. Runtime lock includes the existing materialization implementation.

Sixteen focused style/authored/execution tests passed, including exact exposure materialization while merged gains remain unchanged, and positive retouch enabling beauty with declared model metadata causing an incomplete color-only provider to reject before pixels. Model metadata in this fixture is controlled and does not prove model qualification. Full provider execution, SQL/dispatcher wiring and V20–V32 remain incomplete.

### V26 — owned existing color-stage base provider

Added prepare_color_base to execute the existing render_materialized_style math on PreparedRenderStyle.effective into a separate scratch-owned float32 mapping. Scalar edits are not applied again; source alpha/generated coverage are retained. It explicitly rejects all requested modules outside color/strength/adaptive/local/region_adjustments and requires upstream runtime/model/feature/mask qualification. Composition lock includes color provider/math/resolved-style dependencies.

Two tests passed: exact existing global/strength math on the effective exposure style with owned readonly output and cleanup, and beauty rejection without silently rendering a partial pipeline. This is a color stage building block, not a complete base provider or qualified GPU renderer. Full effect stages/dispatcher/SQL and V20–V32 remain incomplete.

### V26 — actual color math through pinned authored publication

Added a joined execution fixture using actual immutable style/override documents, source NPY, installed composition-lock verification and prepare_color_base (existing color math), rather than a constant proposal provider. It checks measured pixels against effective global/strength math before staging, rereads published RGB and changed-mask NPY bytes, retains review and exact output byte accounting, and verifies cleanup.

Six execution tests passed. Measurement/runtime approval callbacks remain controlled; provider peaks/real RTX3060 qualification, complete effect pipeline and SQL adoption are not proven by this small fixture. Remaining V20–V32 scope remains active.

### V26 — color provider resource peak in authored admission

Added color_provider_peak for owned output RGB, full-frame provider masks, fixed/adaptive LUT conversion/blend and bounded tile temporaries plus conservative overhead. Its four resource fields supplement shared composition estimates; no GPU/model qualification is inferred. The actual color execution fixture now supplies this nonzero provider peak and matching shared execution estimate instead of zero provider cost.

Nine focused color/execution tests passed, including24MP owned scratch sizing, resident/tile RAM overhead and over-limit rejection. Runtime calibration/RTX3060 benchmark remains required before production approval; complete effect providers, SQL/dispatcher and V20–V32 remain unfinished.

### V26 — color stage preserves protected source pixels

Color base rendering now accepts a caller-verified tiled protection reader and copies original RGB bits wherever protection is positive, retaining alpha/generated coverage. An authored protected_mask requires the reader; wrong dtype/shape/nonfinite/out-of-range protection fails explicitly. Final independent change/protection proof remains mandatory. Caller proves reader pin/canvas/transform authority; this callback alone does not grant provenance.

Seven color tests passed, including positive0.001 exact source-bit preservation, unprotected color changes, invalid/missing tile data and output owner cleanup. Full masking/provider assembly, quality metrics, dispatcher/SQL and V20–V32 remain unfinished.

### V26 — verified shared masks passed to authored base providers

Full authored preparation now passes the existing scoped RenderMaskTiles to its base provider alongside complete effective style and original pixels. The lower-level preparation retains its two-argument callback by default for existing callers. The color execution fixture uses the shared verified protection callback only when protection exists, preventing redundant mask IO.

Initial aggregate run failed because the fixture passed an absent-protection reader returningNone to color's strict protection validator; pytest attempted to repr a closed memmap during failure reporting and crashed. Corrected the fixture to distinguish absent protection. Subsequent full-render foundation tests passed with short traceback reporting. Full production masking/provider assembly, dispatcher/SQL and V20–V32 remain incomplete.

### V26 — safe diagnostics after pixel owner cleanup

AdjustmentPixels excludes RGB/alpha/generated arrays from generated dataclass repr. This prevents error/pytest diagnostics from reading closed memmap pages after scoped cleanup, addressing the actual earlier diagnostic crash. A real color output mmap is closed before repr in the new regression test; diagnostic repr stays safe. Pixel processing/ownership semantics are unchanged. Full-render foundation aggregate:152 passed. Complete providers/dispatcher/SQL and V20–V32 remain unfinished.

### V26 — existing multi-face beauty stage bridge

Added prepare_beauty_stage around existing portrait.prepare.prepare_beauty, preserving working Rec2020 gamma24 color/geometry/ROI/texture guards and face evidence. Scoped mapped pixels retain alpha/generated coverage; absent faces and skipped-face evidence require review rather than fabricated pass. Caller supplies independently verified effective params/landmarks/masks and qualifies model/runtime dependencies. Composition lock pins the bridge and existing beauty source helpers; external native provider qualification remains separate.

Two bridge tests passed: existing no-face path retains source pixels/review and cleans owners; incompatible working color rejects. Actual face/model execution, full effect ordering/strength integration, complete providers/dispatcher/SQL and V20–V32 remain incomplete.

### V26 — actual multi-face beauty bridge equivalence and failure cleanup

Added platform tests exercising two actual synthetic face/skin ROI fixtures through existing prepare_beauty and the new scoped beauty bridge. The bridge preserves exact output pixels and complete FaceEvidence, retains alpha/generated owners and reports no review only when all faces applied. Consumer failure and cancellation after face patch creation release every ROI/output mapping and scratch file.

Eight focused existing/new beauty tests passed. This proves implementation equivalence and scoped cleanup on synthetic landmarks/skin evidence, not detector/model qualification, production effect ordering or V20–V32 completion.

### V26 — shared one-time beauty strength semantics

Extracted effective_beauty_params from existing resolve_effective_params: both existing effect resolution and render_beauty_params now scale smoothing/tone_evenness once while preserving texture_keep. Authored helper derives params from materialized effective style; prepare_beauty_stage consumes those effective params without further scaling. Composition lock includes shared helper bytes.

Thirteen focused beauty bridge/platform/adapter tests passed, including owned authored strength derivation and existing multi-face pixel/evidence behavior. Complete pipeline ordering/provider assembly, runtime qualification and SQL/dispatcher integration remain unfinished.

### V26 — ordered color/beauty base-stage assembly

Added prepare_portrait_base to run existing color then beauty in one nested ownership scope with exactly-once beauty strength derivation, complete face evidence and retained review. It rejects all authored effects beyond supported color/local/adaptive/regions/beauty. Color phase uses a separate owned style with beauty delegated explicitly to the following stage, preserving full authored effective style provenance. A needs_review style retains review even when all faces apply. Composition runtime lock includes assembly bytes.

Actual two-face test compares assembled pixels/evidence against separately executed existing stages and checks cleanup; the initial example fixture correctly failed production style gate, then was changed to explicit needs_review fixture metadata. Eighteen assembly/face/runtime tests passed. Synthetic model/face metadata is not qualification. Protection across later beauty, full source/mask authority, effect resource summation, remaining effects and SQL/dispatcher integration still require completion; V20–V32 stays active.

### V26 — final source protection after color/beauty assembly

Added preserve_render_protection: owned readonly RGB/optional alpha mapping, bounded validated protection tiles, exact original bits restored wherever coverage is positive, opaque source-alpha semantics, finite/unit validation and no borrowed-input mutation. Portrait assembly invokes it after beauty using shared verified masks. Generated coverage is retained for separate provenance proof; final adoption change/protection verification still applies.

Two focused protection/portrait tests passed, including signed-zero source RGB restoration and opaque alpha restoration with0.001 coverage. An initial test used float64 expected0.4 for float32 alpha and was corrected to exact float32 expected bytes. Extra scratch/RAM must be included in complete provider peaks before production. Full providers/resources/SQL/dispatcher and V20–V32 remain incomplete.

### V26 — planned portrait provider resource peak

Added portrait_provider_peak using actual source-bound FaceInput ROI plans: owned color/beauty output, global face protection, retained face delta/weight files, resident mask pages, filter ROI working peak and optional final source-restoration RGB/alpha. It rejects duplicate/too many/source-mismatched faces, unsupported modules, unexpected disabled-module faces and invalid ROI budgets. Costs supplement shared composition admission; runtime calibration/qualification remains external.

Sixteen focused portrait/runtime tests passed, including actual two-face ROI sizing and exact added protection scratch16*pixels+16384. Full installed provider integration must pass this peak before IO; conservative estimates do not prove RTX3060 performance or V20–V32 completion.

### V26 — mandatory portrait provider resource admission

prepare_portrait_base now requires execution_profile and verifies actual planned portrait provider peak plus shared composition costs before color/beauty/protection scratch allocation. This supplements upstream complete task admission rather than permitting provider peak omission. Unsupported effects and face/source checks remain mandatory.

Three portrait tests passed: actual two-face assembled pixel/evidence equality under matching admitted estimate, ROI/protection resource sizing, and missing budget rejection with empty scratch directory. Provider/model qualification, remaining effects and SQL/dispatcher integration remain unfinished.

### V26 — assembled portrait protection regression

Extended real two-face portrait fixture to protected/unprotected variants with matching shared-mask and provider resource estimates. The positive0.001 protection variant executes color/beauty then final restoration and verifies every final RGB bit equals original source; unprotected variant retains existing color/beauty equivalence and face evidence. Synthetic mask/face/model metadata remains caller-controlled, not live declared-artifact or qualification proof.

Four portrait tests passed, including ROI resource costs and pre-allocation admission rejection. This strengthens assembled protection evidence; full production authority/remaining effects/SQL/dispatcher and V20–V32 remain unfinished.

### V26 — review-bound defect repair stage bridge

Added prepare_defect_stage around existing accepted_candidates and prepare_repair. It preserves exact source/review/candidate selection guards, existing working color/ROI/layout/value checks, measured repair evidence, alpha/generated owners and skipped/no-selection review. Caller must prove actual current image pixel pin and review ACL; stale proposals must not be rebound to different post-beauty pixels. Composition runtime inventory includes bridge/shared repair helpers.

Existing/new real label repair tests verify only accepted label pixels change, applied_pixels evidence and cleanup. This is a scoped stage, not complete portrait integration, review authorization, native qualification or SQL adoption. V20–V32 remains active.

### V26 — stale defect review and bridge cleanup regressions

Added real review-bound repair bridge tests for changed label-map review pin rejection before allocation and consumer failure after output mapping. Existing accepted_candidates revision/source authority gates and scratch cleanup remain preserved. Native model/runtime approval and actual current pixel-to-image pin verification are still caller requirements; the bridge must not rebind proposals to changed post-beauty content.

Focused defect review/repair/adapter tests passed. Full provider integration, resources, SQL/dispatcher and V20–V32 remain incomplete.

### V26 — shared effective defect intensity

Extracted effective_defect_params for existing resolve_effective_params and new render_defect_params. Both derive an owned validated repair strength once from the materialized style, preserving review policy, diameter limits and permanent-mark constraint. The repair stage consumes the resulting strength without further multiplication.

Focused effective-parameter/repair/beauty adapter tests passed, including requested0.7 × authored0.4 with untouched original params/policy. This does not authorize review or bind proposals to newly edited pixels; complete provider/DAG/SQL integration remains unfinished.

### V26 — admitted review-bound repair provider resource peak

Added defect_provider_peak sized from exact accepted review candidates and existing repair radius/halo support windows, full-frame RGB/labels/protection residency and owned RGB scratch. Invalid ROI budgets and support windows over budget fail explicitly. prepare_defect_stage requires execution_profile and admits provider plus shared composition minimum before prepare_repair allocates output; review binding remains checked first.

Focused repair/runtime tests passed, including accepted ROI sizing, over-budget rejection and unchanged accepted-label/cleanup behavior under admitted profiles. Conservative native working estimates still require calibration; actual current pixel pin, review ACL, full provider/DAG/SQL and V20–V32 remain unfinished.

### V26 — broad existing worker regression and current schema pin

Expanded verification to all unit/platform tests after shared beauty/defect effective-parameter changes. The first sandbox run found a real stale worker schema digest after optional ImageReport parent_reports generation, plus loopback/cache/runtime-file permission errors (1134 passed,4 failed,7 errors,2 skipped). Updated SCHEMAS_SHA256 from actual deterministic schemas.json bytes; retained the immediately preceding digest as an explicit compatibility constant in the existing bounded input-closure allowlist. Unknown digests still reject; this change grants no operation/provider qualification.

Rerun with required local loopback/file permissions:1146 passed,1 skipped in39.37s. Separate top-level full-render recipe/component tests:107 passed. Existing beauty/repair/effective-parameter behavior remains green. Renderer build/source-byte locks affected by input_closure changes require regeneration and external qualification before rollout. SQL adoption, installed complete providers/dispatcher and V20–V32 remain incomplete.

### V26 — repair current RGB bound to approved immutable image

prepare_defect_stage now opens the exact approved image.pixels through TaskArtifacts input and compares current RGB bitwise in bounded tiles before repair output allocation. Different in-memory post-color/beauty pixels cannot reuse an old proposal/review. Resource admission and accepted-candidate guards precede mapping. Caller still proves image/proposal/review document authority and native qualification.

Bridge fixtures now publish actual matching RGB/WorkingImage/proposal pins and recompute candidate IDs atomically through strict model validation. Initial in-place fixture reassignment correctly failed candidate identity validation. Focused tests verify changed current RGB rejection and empty repair scratch. Full intermediate publication/DAG/provider/SQL and V20–V32 remain incomplete.

### V26 — repair label pixels bound to immutable reviewed label map

Repair bridge now maps the exact proposal.label_map through TaskArtifacts and compares the provided uint16 labels tilewise before repair allocation, in addition to current RGB pin proof. Changed label pixels cannot reuse approved metadata. Provider resource peak now conservatively accounts owned output plus pinned RGB/label mappings (32*pixels RAM base,26*pixels+24576 scratch) and ROI temporaries. Fixtures publish actual labels and atomically recompute proposal/candidate/review pins.

Focused bridge/runtime tests passed, including changed label pixels rejection before repair scratch. Production TaskArtifacts verifies actual download hashes; fixture artifacts are controlled real NPY files. Protection-mask provenance, complete provider/DAG/SQL and V20–V32 remain unfinished.

### V26 — repair protection bound to declared MaskRef bytes

prepare_defect_stage requires protection_ref, resolves protected MaskRef with exact source canvas/transform and maps its immutable float32 unit array through TaskArtifacts. Supplied protection tiles are compared bitwise before repair allocation, alongside RGB/label pin proofs. Provider costs include verification protection mapping (36*pixels RAM base,30*pixels+32768 scratch). Task-authoritative ACL and actual gateway hashes remain production reader duties.

Focused bridge/runtime tests passed; changed protection pixels reject with empty repair scratch. Fixture publishes real canonical MaskRef/NPY metadata. Full intermediate image/review authority, pipeline assembly, SQL/dispatcher and V20–V32 remain unfinished.

### V26 — repair documents bound to declared immutable inputs

Repair bridge requires review_ref and independently reloads WorkingImage, DefectCandidates and DefectReview through task-scoped contract reader, comparing complete canonical contents with supplied objects before mapping/repair. Existing exact accepted-candidate guards remain first. Production TaskArtifacts enforces declared refs/hash; backend must still prove saved review ACL and accepted task authority.

Focused repair/runtime tests passed, including valid but changed in-memory review selection rejection with empty scratch. This closes document substitution, not intermediate image publication, provider/DAG assembly, native qualification or SQL adoption. V20–V32 remains active.

### V26 — repair admission before all declared artifact IO

Moved resource admission ahead of WorkingImage/DefectCandidates/DefectReview reloads. A missing resource profile rejects with CONFIG_INVALID without any contract/input IO or scratch output. Initial focused defect/runtime regression:24passed.

### V26 — repair alpha bound to immutable approved source

The repair bridge now verifies float32 source alpha bitwise in bounded tiles and refuses removed, added, changed or differently typed transparency before output allocation. Opaque sources retain absent alpha. Resource peak includes the additional alpha mapping. Updated controlled bridge fixtures to publish the exact source representation before creating proposal/review; existing legacy repair fixtures remain unchanged. Signed-zero alpha preservation and mismatch cleanup are covered alongside the original RGB/label/protection proofs. Production dispatcher, native qualification and independent generated provenance remain outstanding.

Verification for the two preceding changes:30focused tests passed (defect bridge, pinned render runtime and existing repair preparation); complete selected render suite tests/platform/test_v26_render*.py plus tests/test_v26_render*.py:117passed. Documentation checker:2150static checks and54semantic tests passed with deterministic regeneration. These results do not qualify RTX3060 or assert full-system completion.

### V26 — typed base effect evidence retained through measured publication

prepare_render_pixels now accepts exact RenderBeautyStage/RenderDefectStage/RenderPortraitBase alongside plain AdjustmentPixels and retains typed evidence in PreparedRender. Measurement receives these records with pixel owners live. Publication cannot drop their review flags or skipped outcomes even when the measured report says pass; it preserves metrics, existing reasons and parent inventory and stages review instead. Invalid evidence fails explicitly; zero_strength and disabled portrait beauty retain their existing semantics. Composition runtime inventory now pins render_effects. Controlled full pinned authored publication tests cover pass/review/fail with retained review evidence; standalone tests cover skipped-stage false flags and malformed records. Initial combined render/preparation verification:134passed. This does not install a full qualified provider or assert native/RTX qualification.

Final verification of typed base-evidence propagation:174tests passed across all selected top-level, platform and unit V26 render suites. Documentation validation passed2150static checks and54semantic tests, with deterministic regeneration. PostgreSQL integration and hardware/model qualification were not claimed by these checks.

### V26 — ordered multiple-effect evidence through final publication

Added bounded RenderEffectChain for1..32 typed stage outcomes and final scoped AdjustmentPixels. Nested/empty/untyped chains reject. All records validate even after a preceding review outcome; a later malformed record cannot hide behind short-circuit aggregation. Full pinned authored attempt-staging regression now exercises retained multiple-effect evidence across measured pass/review/fail. Publication regression verifies measured reason, clipping/outside-mask/skipped metrics, parent roots and original measured report bytes remain intact while a skipped beauty stage promotes status to review. Initial focused tests:18passed. This carrier does not approve intermediate image/review rebindings or qualify a provider.

Final multiple-effect verification:176tests passed across selected top-level/platform/unit V26 render suites. Documentation checks passed2150static checks and54semantic tests with deterministic regeneration. Full-system integration and hardware qualification remain outside this verification.

### V26 — finite and coherent retained effect measurements

Retained FaceEvidence/RepairEvidence now enforce bounded inventories, unique identities, real bool/integer fields, finite bounded clipping/repair metrics, finite nonnegative texture ratios and coherent applied-beauty/skip reasons. Malformed provider dataclasses fail before composition/publication instead of passing through a typed wrapper. Focused regressions include NaN/infinity, negative counts, bool counts, out-of-range labels/clipping and invalid candidate hashes. RegionLoupe route integration remains pending full FrozenRenderRecipe API support; its scalar FrozenRecipe endpoint cannot represent all V26 authored edits.

Verification:187selected render tests passed; after moving duplicate-face identity comparison behind field validation,17effect-evidence tests passed. Documentation validation passed2150static checks and54semantic tests with deterministic regeneration. No full-system or hardware qualification claim is implied.

### V26 — server full-render immutable source evidence compiler

Added v2render.CompileRenderEvidence retaining the complete FrozenRenderRecipe rather than scalar projection. It verifies exact admitted version/source/canvas binding, canonical source bytes and both pinned digests with bounded bodies. Returned entries explicitly name FrozenRenderRecipe and WorkingImage; no child gains authority from parsing. Actual BuildFrozenRenderRecipe fixture preserves LayerStack, protected mask and authored parameter delta through compilation; changed version/source/canvas, invalid roots and changed bytes reject. Render/region/draft Go package checks passed. SQL scope/closure, scalar recomputation and full qualified dispatcher remain outstanding; scalar-only RegionPreview is unchanged.

Final source-evidence verification:go test ./internal/... passed (including unchanged region/draft services); documentation validation passed2150static checks and54semantic tests with deterministic regeneration. Integration/hardware qualification and completion of all V20–V32 requirements are not asserted.

### V26 — verified full-render roots for scoped dependency traversal

Added RenderInputRoots with immutable source/recipe revalidation and deterministic UUID ordering. Only FrozenRenderRecipe and WorkingImage become roots; child links retain typed parent/occurrence authority through the existing closure traversal rather than becoming root grants. Conflicting artifact IDs across root roles and changed recipe bytes reject. Actual full authored recipe fixture checks exact root contract/ref inventory. SQL full-root authority, closure integration and dispatcher remain required subsequent work.

### V26 — scoped full-render dependency resolution boundary

Added ResolveRenderInputs with authenticated scope/context checks, verified root compilation and delegation to the existing scoped resolver interface. It re-enumerates direct children from actual validated recipe/source bytes and rejects omitted roots/children, duplicate IDs, invalid refs, oversized inventories and canceled requests. Sorted output is an owned copy for stable downstream preparation. Controlled resolver fixture using actual BuildFrozenRenderRecipe/source documents proves complete direct coverage, missing-child and duplicate rejection, no IO on pre-cancellation and no backing-slice mutation. Recursive/SQL authority remains resolver responsibility; full SQL registration/dispatch is still pending.

### V26 — full-render task roots connected to typed closure resolver

ResolveTaskInputs now accepts the exact full-render root pair, routes it through existing occurrence/source-bound traversal and preserves the4096 full-render budget. Mixed/extra/repeated full roots reject before catalog IO; legacy modes remain intact. Existing actual128-frame shared/distinct document fixtures now exercise this public task-input interface, checking exact source/root/NPY contracts and that distinct occurrence traversal exceeds512 without truncation while the deduplicated inventory retains128 WorkingImage,128 NpyArray and128 FaceObservations. SQL catalog publication/allowlist support is not implied by controlled catalog fixtures.

### V26 — typed full-render immutable publication entry point

Added Publisher.PublishFrozenRenderRecipe with strict canonical wire/version validation and cancellation before upload dependency access. Valid payloads reuse the existing scoped immutable draft upload/quota/lease/recovery protocol. Actual compiled full recipe fixture checks wrong version, changed wire and malformed documents fail with VALIDATION_FAILED before dependencies; valid wire reaches the required dependency gate and cancellation remains terminal. Dedicated full-render SQL catalog registration/provenance and real object/database integration are still required; no generic draft artifact gains root authority from this method alone.

### V26 — compilation connected to typed immutable publication

Added CompileAndPublishFrozenRenderRecipe, deriving exact uploaded full-recipe bytes from EditingRecipeV3, resolved compiler inputs and verified layer body. Invalid/missing referenced layer bytes fail before upload dependencies; valid actual authored fixture reaches the required publisher dependency gate. This closes compiler-to-uploader orchestration, while SQL full-root registration/authority remains pending. The current SQL v2_draft_artifact_scope does not grant FrozenRenderRecipe merely because a generic draft upload is ready.

### V26 — broad regression after full-root resolver and typed publisher integration

All backend internal package tests passed with the current full-render source/root compiler, task resolver profile and compiler-to-publisher entry point. Worker unit/platform suite:1153passed,1skipped in46.86s, executed with local loopback/temp-file access required by existing fixtures. Explicit Go-to-Python full FrozenRenderRecipe test passed using the project worker venv (TestFrozenRenderRecipePreservesCompositionAndChecksLayerBytes, count=1), confirming actual compiled canonical bytes are accepted by the Python full-recipe decoder. These checks establish compatibility within their fixture scope, not full SQL recipe publication/registration, full provider dispatch, hardware/model qualification or completion of V20–V32.

### V26 — full-render descendant denial and root collision

The public task-input resolver now rejects a full recipe/source artifact-ID collision before any catalog lookup. Actual128-frame shared/distinct fixtures additionally revoke the descriptor shard through a scoped catalog wrapper: resolver traversal fails with no returned task-input inventory. This verifies authorization is still applied inside the full-root traversal and is not inferred from authorized roots. Controlled catalog denial does not establish production SQL full-root provenance, which remains pending.

### V26 — SQL full-recipe source registration foundation

Migration00382 adds a complete nullable source-provenance tuple to existing v2_draft_uploads and a narrowly granted SECURITY DEFINER registration function. It verifies tenant/editor/session, ready upload/object metadata, exact raw digest and accepted exact source/version/project. It does not grant FrozenRenderRecipe catalog access or assert full semantic/model qualification. All migrations applied in isolated PostgreSQL; schema/function privilege and unscoped-denial test passed. Metadata fixtures additionally exercise pending-vs-ready upload, matching replay, changed bytes and revoked session; those fixtures prove provenance registration, not full recipe semantic compilation.

### V26 — Go immutable publisher connected to SQL source registration

Added RegisterFrozenRenderRecipe with bounded owned bytes, strict complete-wire decoding, exact uploaded SHA/version and authenticated scope checks before SQL. Typed publication now uploads the owned canonical payload, rechecks current source/editor/session via register_v26_render_recipe and returns a ref only after registration; ready-upload retries use existing stable document replay. Actual full recipe fixture tests valid dependency gating, changed pin and foreign version rejection. Full-root catalog lookup, real Go/object/database end-to-end and graph dispatch remain pending; existing isolated PostgreSQL registration tests separately prove source-provenance semantics.

### V26 — registered full-recipe root SQL scope connected to catalog

Migration00383 introduces dedicated v26_render_recipe_scope with current tenant/editor/session, exact project/version/root pin, ready registered upload and live accepted source checks. SQLArtifactCatalog selects this predicate for parentless FrozenRenderRecipe roots without broadening the legacy contract allowlist. Shared metadata integration fixture now checks registered root access, foreign-version denial and revoked-session denial; complete wire/child authority remains independent. Go draft/render package checks passed. Production full descriptor/model scope and qualified dispatch remain pending.

### V26 — registered root scope negative PostgreSQL verification

Extended isolated PostgreSQL full-recipe registration/scope test: a ready but unregistered generic upload is denied, while matching registration enables the dedicated root predicate. Foreign tenant/project/version/SHA/user/session are denied. Temporarily expired recipe and source artifacts each disable access, with savepoint rollback restoring the original valid fixture; revoked session remains denied. Both integration tests passed with all383migrations applied in isolated databases. This verifies metadata source-provenance/root access restrictions, not complete semantic/model admission or qualified pixel dispatch.

### V26 — frozen learning occurrence metadata and selected descriptor source

Full recipe model/descriptor child occurrences now carry owned frozen learning metadata and recipe binding for subsequent SQL authority checks. Actual compiled learning fixture verifies version/analysis/ordinal retention. Descriptor traversal now rejects absent/duplicate selected ordinal or an ordinal bound to a different source even when the desired source occurs elsewhere; the check follows full descriptor structure validation. Existing128-frame shared/distinct full traversal remains covered. SQL model/descriptor approval authority is still required independently.

### V26 — exact authored document pin persisted for model-use authority

Migration00384 adds render_authored_sha to full-recipe registration. The authenticated raw-digest/source registration runs first through a private helper; the wrapper fills or verifies the recipe_sha256 metadata atomically, including ready-upload replay recovery. Existing PostgreSQL fixture now verifies the authored pin survives matching replay. This enables subsequent exact document/model_use_links joins; no model catalog approval or operation admission is granted here.

### V26 — dedicated model-use SQL predicate and catalog selection

Migration00385 joins registered full root to the exact authored document SHA, its model_use_links, active eligible learning version and exact immutable model/training documents with live consents. Go catalog validates carried learning/source metadata and uses the dedicated predicate for CandidateProfile. Go draft/render tests passed; isolated PostgreSQL applies the migration and checks an unlinked/unapproved model is denied without changing registered-root access. Positive eligible-model, consent/activation revocation and full Go/object/SQL traversal remain pending, so this is not complete model admission qualification.

### V26 — positive learning scope integration audit exposed stale training fixture

Attempted reuse of test_v23_training_freeze happy-path adoption to qualify positive full-render model scope. Current TrainingManifest rejects its legacy RenderCase fixtures missing base_document, approved_document and descriptor shard/analysis/ordinal. A provisional field-only update reached SQL freeze_v23_training_snapshot but was denied, confirming metadata substitution does not satisfy current provenance gates. Provisional changes/hooks were removed; the existing training fixture is preserved and the gates were not weakened. Added a rollback-scoped render_model_scope_fixture helper for eventual adopted-version positive/authoring-SHA/consent tests, but it has not executed successfully. Positive model scope remains unverified; next work must build/update the training fixture with actual authoring documents and analysis-bound descriptors, then run the adopted-version scenario.

### V26 — positive adopted model scope and real frozen-upload SQL defects fixed

Updated stale SQL training fixture to current RenderCase metadata, saved exact recovery, deterministic per-event FrozenRecipe documents and zero-profile baseline. Reserve/complete80frozen documents exposed a production AmbiguousColumn purpose-variable defect, fixed forward-only in00386. Updated training/adoption scenario passed before model-scope hook. Positive model hook then exposed the actual adopted training artifact role v23_training_manifest versus document-only gate;00387 and Go LearningProfileResolver now accept both actual and legacy roles for the exact approved version training pin. Full training SQL scenario including adopted-version model scope, wrong model/version/authoring SHA and consent-revoke denial passed. Fixture authority is SQL metadata/protocol evidence, not descriptor pixel/model qualification or full object-byte compilation proof.

### V26 — exact analysis descriptor SQL scope foundation

Migration00388 ties full-root DescriptorShard access to selected source/ordinal snapshot and scene membership, exact session analysis/model lock, live sealed session run/plan and exact canonical describe output generation. Go passes owned frozen model lock with existing learning/source occurrence metadata and rejects missing/mismatched descriptor context before SQL. Isolated registration fixture includes unknown-analysis/descriptor denial. Positive adopted-analysis and full Go/object/catalog integration remain pending; this does not enable qualified render dispatch.

### V26: positive descriptor SQL authority on materialized V22 analysis

Extended the real `materialize_v22_scene_plan` integration scenario with a nested, rolled-back descriptor authority fixture. The fixture publishes ready output-upload metadata and a canonical `session.describe` task result, reserves/completes/registers a full-render root through the SQL API, and confirms `v26_render_descriptor_scope` access for the selected source ordinal and model lock. Wrong source version, descriptor ID/SHA, analysis, ordinal, model lock, actor/session, changed fencing token, blocked task, expired descriptor, and cancelled run deny access; rollback restores access.

Validation: `.venv-platform/bin/python -m pytest tests/integration/test_v22_scene_materialization.py tests/integration/test_v26_render_registration.py -q --tb=short`: **3 passed in 7.03s**, isolated PostgreSQL database applying all 388 migrations. This proves relational authority over a materialized analysis and canonical task-result metadata. Fixture bodies are intentionally SQL metadata headers; it does not establish complete FrozenRenderRecipe semantic validation, descriptor pixel validation, end-to-end object publication, descendant closure scope, or GPU qualification. Those integration steps remain outstanding.

### V26: preserve descriptor frame bindings during closure traversal

Descriptor `WorkingImage` children now carry their frame's exact `SessionVersionBinding`, matching `FaceObservations` children. Face documents propagate the same binding back to their source, and nested artifacts inherit a copied binding during traversal. The 128-frame shared/distinct closure fixtures assert source binding and pixel binding retention. This is prerequisite context for per-frame SQL authority; it does not by itself grant access to additional source versions or authorize binary descendants.

Repeated `DescriptorShard` occurrences now rerun selected-ordinal validation before the usual inventory deduplication. A regression case references the same valid shard twice with different frozen ordinals; the absent second ordinal must reject the entire closure instead of returning partially approved inputs.

Validation: `GOCACHE=/private/tmp/photoeditor-go-cache go -C backend test ./internal/v2draft ./internal/v2render`: **passed**, draft 1.192s; render cached. Remaining: connect per-frame analysis authority to the SQL catalog for source/face/binary descendants, then verify actual full task publication and execution.

### V26: relational authority for each descriptor frame source

Added forward-only migration `00389_v26_render_frame_source_scope.sql`. `photo.v26_render_frame_source_scope` first repeats current full-root/descriptor authority, then requires exact frame ordinal/version/source ID/SHA in the analysis snapshot, accepted version whose master matches that source, verified live WorkingImage, project ownership, and matching scene membership. The API role can execute the narrow SECURITY DEFINER function; public access is revoked. The trusted resolver still must validate canonical descriptor bytes and frame binding/canvas before using this function; SQL metadata alone cannot prove an artifact ref occurs inside JSON.

Validation: `.venv-platform/bin/python -m pytest tests/integration/test_v22_scene_materialization.py -q --tb=short`: **1 passed in 3.28s**, all **389** migrations on an isolated PostgreSQL database. Positive authority is checked over the actual materialized analysis fixture; wrong/missing/out-of-range ordinal, version, source, digest, actor/session and nonaccepted source deny access. This fixture has one frame; distinct multi-frame source authority, SQLArtifactCatalog wiring, faces and binary-descendant authority remain outstanding.

### V26: wire frame-source authority into SQLArtifactCatalog

Added backend-only `RenderFrameAuthority`, derived after canonical descriptor validation, carrying the registered full root, selected asset version, descriptor pin, frozen learning analysis/ordinal, model lock and individual frame ordinal. Both source and face child occurrences receive independent context copies; nested descendants inherit context. SQLArtifactCatalog uses `v26_render_frame_source_scope` for descriptor-context WorkingImage occurrences and applies accepted-master checks to the individual frame version, preserving the selected root version as a separate SQL argument. Generic artifacts do not acquire this authority automatically.

The 128-frame shared/distinct controlled-catalog fixtures assert context retention, root/descriptor/selected-version/model-lock pins, source binding and nested pixel binding. The repeated-descriptor regression now additionally proves its first occurrence is valid before the second absent ordinal is rejected. Targeted `go -C backend test ./internal/v2draft ./internal/v2render` passed. Real Go SQLArtifactCatalog multi-frame integration remains required; faces and binary dependencies still require dedicated SQL authority wiring before full closure/task execution can be enabled.

### V26: descriptor face-child SQL authority

Migration `00390_v26_render_frame_faces_scope.sql` authorizes ready FaceObservations only after exact frame-source authority, and requires the same descriptor producer task/attempt/fence plus a matching ready `faces` output upload (SHA, bytes, immutable object version). This matches the actual session.describe worker: faces are staged children and only DescriptorShard is returned as root. SQLArtifactCatalog now invokes this function for FaceObservations with derived RenderFrameAuthority and exact descriptor parent. JSON face binding remains independently validated by the resolver.

Verification: materialized-analysis integration **1 passed in 2.82s**, all **390 migrations** on isolated PostgreSQL; valid face child passes, wrong face ID/SHA/frame/actor and expired child deny. Targeted Go draft/render suites passed after wiring. This confirms SQL metadata scope and controlled resolver behavior; full Go/database/object multi-frame closure, binary descendant scope, and qualified full render dispatch remain outstanding.

### V26: frame pixel/alpha descendant SQL scope

Added forward-only migration `00391_v26_render_frame_array_scope.sql` and SQLArtifactCatalog wiring for descriptor-context WorkingPixels/WorkingAlpha. The trusted resolver must first parse the canonical WorkingImage and derive the exact child ref; catalog additionally requires exact source parent, per-frame analysis source authority, matching nonnull source producer task/attempt/fence, live verified immutable NPY bytes and a compatible output role (`working_rgb` or `pixels` for RGB, `alpha` for alpha). Generic Artifact refs do not use this grant. The producer role pair covers existing decode and adopted lens outputs.

Verification: materialized-analysis SQL fixture **1 passed in 3.36s**, all **391 migrations** on isolated PostgreSQL; correct RGB metadata passes, wrong ID/SHA, alpha/generic contract substitution, absent frame and changed fence deny access. Go draft/render suites **passed** (1.693s / 0.974s). The fixture source/array provenance is controlled relational metadata, not an actual decode execution or actual NPY bytes. Real Go SQLArtifactCatalog + object-store multi-frame closure, alpha positive producer execution, publication and qualified full render dispatch remain outstanding.

### V26: actual Go SQLArtifactCatalog calls against committed PostgreSQL metadata

Added `TestIntegrationRenderCatalogFrameAuthority` and Python orchestration `test_v26_go_catalog.py`. The isolated database fixture materializes the V22 analysis and commits the ready registered root, describe descriptor/face uploads, exact source and RGB producer metadata with task_attempts satisfying deferred task-result foreign keys. Go opens a real pgx pool as photo_api, authenticates the actor/session/epoch through tenant.WithTenant, and performs SQLArtifactCatalog.Lookup for full root, frame WorkingImage, FaceObservations and WorkingPixels. Correct ready immutable metadata is returned; wrong SHA and absent frame ordinal deny access. Existing fixture default remains rolled back; persistence is enabled only by this isolated integration scenario.

Initial focused run: **1 passed in 5.64s** with all **391 migrations**. Unlike direct SQL-function tests, this executes the real generated Go SQL and argument binding. This remains single-frame relational metadata: canonical full recipe/descriptor/image object bytes, object-store reads, alpha, distinct-frame closure and complete render dispatch are not yet covered.

### V26: alpha catalog authority and session revocation

The committed SQL catalog fixture now includes an immutable alpha NPY artifact with source producer pins. Actual Go `SQLArtifactCatalog.Lookup` validates WorkingAlpha alongside root/source/faces/RGB, rejects wrong SHA and absent frame ordinal, then repeats all five lookups after revoking the authenticated session through the admin connection; every lookup must fail. This verifies current authorization is repeated rather than cached after initial success.

Validation: `pytest tests/integration/test_v26_go_catalog.py tests/integration/test_v22_scene_materialization.py -q --tb=short`: **2 passed in 7.88s**, isolated PostgreSQL with 391 migrations. Alpha bytes remain relational fixture metadata; actual NPY contents/object-store traversal and distinct-frame end-to-end closure remain to implement and verify.

### V26: repeat descendant authorization for shared frame sources

Fixed a traversal gap: a repeated WorkingImage occurrence revalidated its canvas and source authority but returned before authorizing pixel/alpha descendants in the later frame context. Repeated typed documents with derived RenderFrameAuthority now traverse their child occurrences again; final task inventory still deduplicates immutable refs. The existing cycle guard and 4096 full-render occurrence cap remain active. Shared and distinct 128-frame fixtures now both traverse 646 authorized occurrences.

Added a denial catalog regression that refuses WorkingPixels only at ordinal 127. Both shared and distinct full-root resolution must fail without returning partial task inputs; earlier authorization of the same RGB artifact cannot bypass the later frame boundary. Targeted Go draft/render suites passed. Controlled-catalog evidence does not substitute for real distinct-frame SQL/object-store integration, still outstanding.

### V26: normalize binary direct-child transport requirements

Fixed ResolveRenderInputs contract mismatch: DocumentChildren produces provenance contracts WorkingPixels/WorkingAlpha/MaskPixels/DenseWarpField, whereas actual ClosureResolver transport inventory uses NpyArray for these bytes. Required direct-child entries now normalize to NpyArray before exact inventory comparison. The controlled render-input test now supplies actual transport naming and verifies substituting WorkingPixels back is rejected.

Verification: all backend internal package tests passed before this fix; targeted draft/render suites passed after it. This removes a valid-closure rejection on the full-render preparation path; actual SQL/object full closure and task publication remain outstanding.

### V26: actual ClosureResolver feeds full-render preparation

Added TestResolveRenderInputsWithActualDocumentClosure. The test compiles a real canonical FrozenRenderRecipe, uses the Python WorkingImage contract fixture and the existing canonical style example, and calls ResolveRenderInputs with the actual v2draft.ClosureResolver instead of a preassembled inventory. ClosureResolver reads/verifies JSON bytes and emits binary transport NpyArray; preparation now accepts that exact inventory. Corrupting the descendant style bytes rejects the entire result with no partial inputs.

Validation: Go render/draft suites **passed**, render 0.606s. The catalog/object reader are controlled in-memory implementations; pixel bytes are metadata only and not read by closure construction. This verifies the real compiler/resolver/preparation boundary, not SQL provenance, S3 publication, GPU rendering or multi-frame full-system execution.

### V26: publish full-render InputClosure transport

Added PrepareRenderInputClosure: after authenticated full-root/source evidence and complete scoped resolution, it owns the sorted transport inventory, publishes actual InputClosure manifest/chunks through v2input.BuildInputClosure, rejects transport/semantic artifact collisions, and returns a GraphTask whose sole direct handle is FrozenRenderRecipe. Retry cursors independently read the owned inventory. Caller must commit its task shell before object I/O; this helper does not register/seal/enqueue tasks or qualify providers.

Actual ClosureResolver integration fixture now publishes transport through the controlled digest-checking publisher, verifies direct root and repeatable cursor inventory, and proves corrupt style descendants cause no publication calls. Go render/draft suites passed (render 0.590s). Real immutable object publication with SQL task-shell registration and full operation dispatcher remains outstanding.

### V26: scoped full-render transport publication adapter

Added RenderTransportPublisher.Prepare, wiring complete ResolveRenderInputs/PrepareRenderInputClosure to existing v2input.ScopedPublisher and backend DocumentPublisher reservation lifecycle with explicit tenant/run/task identity. It requires nonnil document publisher and nonzero tenant/run and preserves cancellation before delegation. Task-shell commit must precede object I/O; reservation validates actual task/run ownership. This provides the durable-publication adapter needed by full render orchestration without inventing or enabling an unqualified operation/provider.

Go render/input suites passed after wiring. Actual task-shell orchestration, full-render operation/profile admission, real scoped publication integration, and worker dispatch remain outstanding.

### V26: scoped transport identity and manifest retry

Extended the actual ClosureResolver full-render preparation fixture to exercise RenderTransportPublisher through real v2input.ScopedPublisher. A controlled DocumentPublisher records scope, derives stable IDs by owner/purpose, and rejects changed retry bytes. The first manifest publication fails after chunk publication; the error propagates. Retrying the same staged task reproduces identical chunk/manifest bytes and returns the same task with FrozenRenderRecipe direct handle. Every document scope must retain tenant/run/task owner; exactly one chunk and one manifest are retained.

Go render/input suites passed (render 0.538s). This verifies scoped adapter and retry protocol with a controlled backend-document publisher, not actual SQL reservation/object-store lifecycle. Full operation/profile registration, durable publisher integration and qualified worker dispatch remain outstanding.

### V26: derive full-render mask resource counts from plan

Added Plan.BudgetRender and RenderPlanBudget. Active layers contribute their reachable mask graph node counts separately, matching worker admit_render_inputs; mask refs are deduplicated by UUID/SHA, conflicting pins reject, and protection contributes its own node even when its ref is shared. Inactive layers do not consume mask counts. Qualified provider peaks remain mandatory caller evidence rather than inferred model costs. The result delegates bounded RAM/scratch/output/VRAM arithmetic to RenderResources.

Targeted render suite passed. Regression checks two active layers sharing one invert graph plus shared protection: one unique ref and five mask nodes; disabling a layer reduces nodes, and malformed active graph rejects. Execution-profile/task registration wiring and actual GPU qualification remain outstanding.

### V26: resource budget compiled from pinned render documents

Added CompileRenderBudget. It revalidates FrozenRenderRecipe/WorkingImage bytes against the admitted binding, derives actual canvas dimensions, extracts authored LayerStack/protection refs, rejects unexpected layer bodies, requires exact authored layer SHA, compiles the layer graph against the same binding, and derives counts with Plan.BudgetRender. Caller cannot substitute a separate width/height or cached Plan into this boundary. Qualified provider peaks, artifact authorization and profile admission remain separate gates.

Render/draft suites passed. Actual full recipe fixture confirms 2000x1000 source pixels, protection-only mask accounting and supplied VRAM peak; missing, substituted and noncanonical appended layer bytes reject before budgeting. Profile sealing, task registration and actual qualified GPU rendering remain outstanding.

### V26: canonical full-render execution profile compilation

Added CompileRenderExecutionProfile. It accepts only cpu/cuda/mps and a lowercase SHA256 runtime pin, verifies full recipe/source/layer bytes via CompileRenderEvidence and CompileRenderBudget, and serializes canonical backend/renderer/runtime/model/ICC/tile/alpha/resource fields. Model and ICC pins come from frozen scalar evidence, never separate caller overrides. Runtime SHA and provider peaks must be supplied by the qualified registry; profile serialization does not establish qualification or task authority.

Render suite passed. Tests compare profile resource_estimate to the exact verified document budget and frozen model pin, and reject unsupported backend. Worker profile validation, registry selection, SQL admission and full operation dispatch remain outstanding.

### V26: worker validates admitted full-render profile pins

Added validate_render_execution_profile, requiring typed FrozenRenderRecipe plus exact configured backend, renderer v2.2, approved runtime SHA, frozen model/ICC locks, tile size and alpha mode. Boolean tile values reject. Added execute_profiled_authored_render, invoking this validation and cancellation before the existing pinned-runtime/resource-admitted/measured execution path. Provider qualification remains independent; no operation dispatcher is enabled by this helper. Changed render_resources/render_execute component bytes must be re-pinned and externally qualified before deployment.

Tests cover every pin substitution and entrypoint rejection with unavailable providers/runtime file, proving mismatched profile is rejected before those dependencies. Profile/resources/runtime focused suite: 31 passed in 1.87s; expanded execute suite passed. Registry selection, SQL profile admission and actual dispatcher integration remain outstanding.

### V26: actual Go→Python profile admission and profiled execution

Full-render evidence test now sends the exact Go-compiled canonical FrozenRenderRecipe/profile bytes to the configured real Python worker. Python decodes the frozen recipe, validates configured backend/runtime/model/ICC/tile/alpha pins, and runs admit_render for the verified 2000x1000 protection-only budget with the same provider peak. Command with PHOTO_WORKER_PYTHON executed successfully (Go test 1.013s).

Existing full execution pass/review/fail and inherited-base-review scenarios now run through both pinned and profiled entrypoints. They preserve actual staged output inventory, measured metrics, inherited review, failed measurement rejection and pixel-owner cleanup. Execute/profile focused suite passed. Controlled providers and temporary local pixel files do not prove real GPU qualification or dispatcher/SQL adoption.

### V26: worker task adapter for complete frozen recipe

Added render_task.execute_render_task and mandatory RenderTaskAdmission/QualifiedTaskRenderer protocols. Task params are the FrozenRenderRecipe root; adapter verifies renderer runtime, reads strict pinned canonical recipe, validates profile locks, invokes authoritative admission, loads exact source/layers/protection metadata, rechecks unchanged recipe and actual per-layer resource admission, then invokes complete renderer. Only typed StagedRenderResult is accepted; OperationResult exposes its ImageResult document root plus actual bytes/review evidence. No Operation registry/service installation is enabled yet.

Runtime component inventory now includes render_task; changed build must be externally re-pinned/qualified. Controlled adapter tests prove runtime→admission→metadata→execute order and that insufficient RAM prevents renderer execution. Existing execute/runtime suites remain green: **32 passed in 1.67s**. Metadata loader and renderer/admission are controlled in adapter tests; actual SQL admission, complete qualified provider implementation, registration/dispatcher and final adoption remain outstanding.

### V26: bind task adapter to installed renderer model/ICC pins

QualifiedTaskRenderer now exposes model_lock_sha and icc_lock_sha. Before runtime verification or artifact reads, execute_render_task requires configured backend cpu/cuda/mps, lowercase SHA256 runtime/model/ICC pins and exact profile equality with the installed renderer pins. Subsequent frozen-recipe validation ties those same profile pins to the recipe. A callback that verifies installed models can no longer implicitly substitute a different model/ICC configuration.

Focused task/runtime/execute tests passed; new cases reject model substitution, ICC substitution, missing runtime SHA and unsupported backend before calling renderer.verify_runtime or artifacts. Full SQL admission/registration/provider qualification and operation service wiring remain outstanding.

### V26: concrete profiled renderer adapter

Added ProfiledTaskRenderer, connecting the task adapter to actual execute_profiled_authored_render. Installed runtime bytes and external qualification are checked by verify_runtime; provider loader supplies operators/scalar overrides/base/proposal/measurement callbacks and remaining preparation options. Task execution profile, backend, runtime pin/path, provider peak, tile size, alpha mode and cancellation are taken from authoritative configured renderer/task fields and cannot be overridden by provider options. Provider peak mapping is copied on construction.

Task/runtime/execute suites passed, including attempts to override profile/runtime/peak/tile. This adapter wires the existing full executor but does not create real model providers, fabricate qualification or install a dispatcher operation. New SQL graph registration/admission, complete qualified providers and production service integration remain outstanding.

### V26: full renderer execution and operation factory

Existing measured pass/review/fail and inherited review scenarios now additionally execute through concrete ProfiledTaskRenderer with actual pinned runtime loading and authored preparation/staging. Controlled model/measurement callbacks remain explicit. The result preserves exact output inventory, measurement rejection before staging, review propagation, byte accounting and pixel owner cleanup.

Added render_task_operations factory for render.compose version 1, matching the existing full-render ImageReport/adoption operation identity. It returns no operation without dependencies, rejects partial configuration, verifies renderer runtime before installation, selects cpu/gpu class by configured backend, enforces leased command operation/version/class, and creates TaskArtifacts with explicit output roles before invoking the task adapter. This factory is not yet installed by runtime service and does not authorize SQL graph/task admission.

Expanded task/execute/runtime suite passed. Complete SQL registration/admission, runtime service configuration, actual qualified model providers and final fenced adoption remain outstanding.

### V26: compile full render task shell draft

Added RenderDraftRequest and CompileRenderDraft for render.compose version1. The pure compiler derives deterministic workflow/ordinal task ID and node key, revalidates complete recipe/source direct-ref coverage against a caller-supplied scoped inventory, owns sorted transport entries, compiles pinned execution profile from exact source/layer bytes, and derives task/operation/version-bound input digest. FrozenRenderRecipe is task params; cpu backend uses cpu class, cuda/mps use gpu. Retry cursor factory preserves the owned inventory. Scoped recursive authority and provider qualification remain caller/SQL duties.

Actual ClosureResolver preparation test now compiles the draft and verifies deterministic task, params, class, digest and cursor coverage; incomplete inventory rejects. Go render/input suites passed. SQL task-shell registration, manifest admission, runtime installation and actual complete provider execution remain outstanding.

### V26: draft retry and task-bound inventory digest

Extended actual closure→render draft tests: same workflow/ordinal/input evidence reproduces identical task ID, digest and profile bytes; different ordinal derives a different task ID and digest. Duplicate inventory entries and missing workflow reject. Targeted Go render/input suites passed. These pure compiler checks do not establish SQL task-shell registration, which remains outstanding.

### V26: blocked full-recipe SQL task staging scaffold

Added forward-only migration00392 stage_v26_render_recipe_task, scheduler-only SECURITY DEFINER staging. It locks active tenant/draft unsealed run, requires preview v2.2 workflow and live run quota, exact frozen v26_render node params/body SHA/digest/profile, accepted snapshot source, current registered root/editor session, accepted StyleV2 and conditional exact learning/model/descriptor scope. It inserts only blocked render.compose version1 tasks, with cpu/gpu class matching backend, and supports matching blocked retries. This stage does not enable graph activation or worker admission, and complete profile/resource/layer/closure qualification must still be checked before sealing.

Isolated PostgreSQL applied all392 migrations; registration tests passed with scheduler execute/worker denial and unknown-scope stage denial. Positive full staging, exact deterministic plan validation, comprehensive profile/schema checks, layer authority, graph sealing/registration, admission and provider execution remain outstanding; this migration is a scaffold, not proof that full-render scheduling is complete.

### V26: positive blocked task SQL staging

Added test_v26_render_task_stage.py over accepted source snapshot, approved StyleV2, actual draft-upload reserve/complete/full-root registration functions, draft preview workflow and live quota reservation. Scheduler staging creates render.compose/gpu/blocked/attempt0 and accepts exact replay; changed inventory digest and revoked editor session deny staging.

Validation: **1 passed in2.19s** on isolated PostgreSQL applying all392 migrations. The source/style/full-root bodies are SQL metadata fixtures, not complete canonical renderer qualification. The current staging scaffold permits a partial resource profile and metadata root headers; deterministic task/plan/schema/layer/resource validation must be strengthened before graph activation. No runnable task is enabled by this positive test.

### V26: strict resource profile shape before SQL staging

Forward-only migration00393 replaces staging with strict tile/alpha/resource validation. Resource estimate requires exactly pixel_count/ram_bytes/scratch_bytes/output_bytes/vram_bytes as integer JSON numbers within safe integer bounds; pixels are1..200M, RAM/scratch/output positive. Tile must be numeric256/512/1024 and alpha require_float32/normalize_uint16. Partial profiles, boolean tile, invalid tile and alpha reject. Positive fixture now supplies complete resource shape.

Isolated SQL stage/registration tests passed with all393 migrations. This validates profile representation, not independently recomputed minimums, deterministic task/plan identity or complete authored wire/layer scope; those remain required before runnable activation.

### V26: deterministic task identity enforced by SQL staging

Forward-only migration00394 computes UUIDv5 task identity from actual workflow UUID and render.compose:ordinal, using the same namespace SHA1/version/variant bits as Go TaskID. Staging rejects arbitrary task IDs even when frozen node fields agree with them. It also requires bounded nonempty nodes array and ordinal membership.

SQL fixture now uses actual UUIDv5 identity and adds a separate run plan with an arbitrary matching node/task ID; that plan must not create a task. Stage/registration tests passed with all394 migrations. Full deterministic workflow/run identity, complete plan/wire/layer/resource minima validation and runnable activation remain outstanding.

### V26: source-bound minimum resource floor in SQL staging

Migration00395 requires bounded frozen source_jcs whose exact SHA equals the recipe source pin, extracts integer width/height from those bytes, bounds pixels to200M, and checks pixel_count plus minimum RAM/scratch/output accounting against the shared RenderResources baseline (zero masks/provider peak). Alpha normalization adds its mapped cost. This floor cannot replace complete per-layer/provider budgeting or WorkingImage schema validation, but rejects impossible base budgets before staging.

Stage fixture now uses actual canonical Python WorkingImage test bytes and matching accepted source metadata, with complete render_resources budget. Separate plans containing RAM=1 reject without task creation, even when staging arguments exactly match the frozen profile. SQL stage tests passed on all395 migrations. Full authored/schema/layer/provider minima, aggregate run quota, workflow/run identity and graph activation remain outstanding.

### V26: aggregate frozen run output quota before task staging

Migration00396 sums every frozen v26_render node's positive integer output_bytes using numeric arithmetic and rejects malformed/unsafe values or aggregate output above the live run quota reservation. This check occurs before any task insertion, preventing individually affordable nodes from staging against an underfunded multi-node plan. The per-task pinned-source resource floor remains required.

Integration matrix covers deterministic/arbitrary task identity, valid/underfunded RAM, and sufficient/insufficient aggregate quota. The insufficient-quota plan includes a second node consuming the full reserve; staging ordinal0 must reject without creating its task. Tests passed on all396 migrations. This covers output reservation, not complete manifest/backend-document upload overhead, per-layer/provider minimums, frozen-plan completeness or activation.

### V26: compile immutable render run plan from draft evidence

Added CompileRenderRunPlan/CompiledRenderRun/RenderRunNode. It derives workflow from run UUID, compiles1..5000 deterministic render drafts, rejects mixed tenant/actor identity, owns exact recipe/source/layer strings and profile bytes in canonical v26_render config, computes config SHA and safe aggregate output reservation, and retains independent inventory cursor factories. SQL authority, upload overhead quota, begin-run transaction, plan shape bounds and graph activation remain separate implementation steps.

Actual ClosureResolver integration fixture compiles a two-node plan, verifies distinct task IDs, exact document evidence/config pin/output sum, and rejects mixed tenant requests. Render/input suites passed. This is pure compiler evidence, not proof of SQL creation or complete system execution.

### V26: deterministic workflow identity in SQL staging

Migration00397 requires actual workflow UUIDv5(run,'workflow'), matching Go WorkflowID, before deriving the node task identity. Integration fixture now creates the workflow and task from its explicit run UUID. Wrong-workflow plans remain internally consistent (task UUIDv5 from that wrong workflow and matching node IDs) but must reject without creating a task.

All16 combinations of workflow/task identity, source-budget and aggregate-quota validity passed in33.78s on isolated PostgreSQL with397 migrations. Full plan wire/schema/layer/provider authority, begin-run transaction, transport registration and activation remain outstanding.

### V26: bind compiled render plan to input snapshot

CompileRenderRunPlan now requires a nonzero input snapshot UUID and pins it in canonical config alongside project_id. Migration00398 rejects missing/different config snapshot pins and project mismatches before blocked task staging, and requires the referenced tenant snapshot to exist. Existing workflow creation already rejects mismatched project scope; its immutable run-config protection remains intact.

Go tests verify exact snapshot retention and reject a missing compiler snapshot. SQL negative scenarios create independent immutable runs with an absent or substituted snapshot pin and require rejection with no task insertion; the corrected focused scenario passed. Backend internal suites and42 render task/execution/profile worker tests passed. Complete begin-run, closure registration, graph activation and qualified GPU provider installation remain outstanding; this change does not demonstrate end-to-end production rendering.

Final snapshot-binding verification:18 SQL stage/registration tests passed in39.72s using all398 migrations;2150 static documentation checks and54 semantic documentation tests passed. No production database migration or RTX3060 qualification was performed.

### V26: atomic full-render draft creation and scheduler store

Migration00399 adds scheduler-only begin_v26_render_draft. It repeats live actor/project/snapshot authority under tenant/usage/snapshot locks, verifies deterministic workflow and frozen session/project/snapshot pins, caps config to1MiB, checks aggregate output against the reservation and tenant storage quota, persists run/workflow/reservation, and invokes stage_v26_render_recipe_task for every node. Any rejected node raises a caught subtransaction exception, rolling back the entire new run, all tasks and quota/usage changes. Replay requires unchanged live draft/config/policy/reservation and rechecks every staged node; no graph activation occurs.

SQLRenderDraftStore.Begin validates canonical config/hash and identity, enforces scheduler DB role, and calls the atomic function inside the existing DB-only retry transaction. CompileRenderRunPlan now rejects configs beyond the same1MiB persistence bound. The Go store is compiled/tested but not yet mounted in an API or exercised through real pgx by the SQL fixture. Complete wire/layer/provider qualification, input closure registration, upload overhead accounting and production graph activation remain outstanding.

Focused SQL begin scenario passed with399 migrations: a rejected second node leaves no run/tasks or reserved-byte change; successful draft creation and exact retry reserve once. Expanded two-node success and the complete stage/registration matrix are being verified separately.

Final begin-draft verification:18 SQL stage/registration tests passed in41.81s on399 migrations, including two-node success, exact replay and rollback after a rejected second node. All backend internal suites,2150 static documentation checks and54 semantic documentation tests passed. Scheduler-only ACL checks were added and verified separately. Production API mounting, closure registration and activation remain pending.

### V26: transport publication authority for render tasks

Migration00400 adds a render-specific branch to the existing private v2_input_document_scope predicate used by input document publication. Full-render transport requires an unattempted blocked task in an unsealed draft, pinned project/snapshot/session, exact frozen params/digest/profile (or registered closure profile extension), a live run reservation, and current registered render-recipe/source/editor authority. Runs carrying v26_render cannot bypass this branch using a different operation. Prior V25 composition checks remain unchanged.

The SQL fixture exercises positive manifest scope and denial after session revocation using the administrative fixture role because the predicate remains private to SECURITY DEFINER callers; production ACLs were not expanded. This is a publication authority predicate, not input closure registration, complete model/layer revalidation or graph activation.

### V26: scheduler input registration adapter

Added RenderInputAdmission.RegisterRenderInputs: it materializes a bounded4096-entry inventory before the DB retry callback, verifies deterministic workflow/task IDs and the render.compose input digest, repeats SQL task staging authority, compares registered manifest/chunks/count/digest for replay, and delegates exact batch registration and task-input sealing to the existing RegisterInTransaction mechanism. Object I/O remains outside the transaction; graph activation remains separate. Migration00401 exposes a narrow scheduler-only render input state view gated by current transport scope and editor authorization. Generic input batch registration accepts bounded named semantic contracts, including FrozenRenderRecipe; no contract allowlist was broadened.

Go render suites passed. SQL fixture on401 migrations verifies an unsealed state row and denial after session revocation. Actual Go/SQL full-closure registration, transport upload overhead, full layer/model authority at admission and graph activation are still unverified/incomplete. These helpers are not yet mounted into a production API.

### V26: validate render registration inventory before replay or DB access

RenderInputAdmission now rejects mismatched operation/version/node key/resource class, invalid refs, duplicate/unsorted artifact UUIDs, transport-contract entries in semantic inventory, missing or incorrectly typed/pinned FrozenRenderRecipe root, and absence of WorkingImage before entering the transaction. These guards apply even when a caller recomputes a matching input digest; previously replay comparison alone could bypass semantic inventory checks. Full source pin/recursive typed authority remains the responsibility of scoped compilation and must be covered by end-to-end admission verification.

Nine negative cases use matching recalculated digests except the dedicated wrong-digest case and an uninitialized pool to prove rejection precedes database access. Render and input Go suites passed. No graph activation, API mounting or end-to-end full closure SQL registration is claimed.

### V26: SQL semantic input sealing guard

Migration00402 wraps existing seal_v2_task_inputs, preserving its generic manifest/chunk/row validation. The pre-V26 implementation remains private: scheduler EXECUTE is revoked on the renamed function. Render tasks/plans additionally require current transport authority,2..4096 entries, one exact direct recipe root, exactly one FrozenRenderRecipe input at the pinned params ref, and a WorkingImage input at the exact frozen source ref. The run/task are locked before checks. Missing/mistyped source or root raises V26_RENDER_INPUT_ROOT_INVALID.

SQL tests exercise real input batch insertion. Missing rows and a source incorrectly labeled StyleV2 fail the new semantic guard; correctly pinned root/source progress to the preserved V2_INPUT_MANIFEST_UNPUBLISHED rejection when transport documents are absent. This does not prove successful complete closure sealing or graph activation; those remain pending along with API mounting and full recursive authority validation.

### V26: successful relational transport sealing and current contract lock

The render SQL fixture now reserves/completes real task-owned input chunk/manifest metadata through existing publication functions, registers sorted recipe/source/style rows, seals task inputs, reads exact manifest/chunks/count/digest through scheduler input state and successfully repeats task staging with the transport profile extension. Session revocation still denies staging and state access. This fixture has three semantic metadata entries and a registered recipe header, not the entire production recursive object closure; no actual object-store upload or full-schema Go compiler is exercised here.

The successful path exposed a real integration mismatch: generic sealing emitted341984... while current worker input indexing no longer accepts that hash. Migration00403 pins new render.compose profiles to the current schema hash6c52d2... and extends SQL closure readiness to accept it, retaining prior behavior for other operations. Existing private sealing core ACLs remain private. Successful render sealing on403 migrations passed; comparison to the worker constant and existing lens publication/sealing regression are checked separately. Full recursive Go/SQL admission, API mounting, qualified GPU runtime and graph activation remain pending.

### V26: structural render readiness on every check

Migration00404 extends v2_input_closure_ready with render-specific structural invariants: current contract hash,2..4096 semantic entries, exactly manifest plus one direct recipe root, exact frozen params pin, one FrozenRenderRecipe semantic entry, and the exact WorkingImage source from frozen recipe evidence. This predicate deliberately remains structural; live session/model/layer authority belongs to separate admission/transport checks. Existing other-operation behavior is preserved.

Relational integration scenarios replace a sealed root/source input contract inside rollback-only administrative savepoints while preserving row count, UUID and SHA. Readiness must become false and become true again after rollback. This catches a structural mismatch that generic row counts and direct-ref membership previously missed. Full recursive object-byte registration, qualified worker execution and graph activation remain pending.

### V26: connect scoped render compilation, draft creation and input registration

Added RenderDraftCoordinator.Prepare and explicit store/transport/registrar interfaces. It owns recipe/source/layer byte evidence before calling dependencies, resolves every scoped inventory, compiles the complete immutable run plan, commits the draft through Store.Begin, then publishes and registers each task closure in order. Publication stays outside retry transactions; a failure stops subsequent phases and preserves the existing draft for deterministic retry. Completion here means registered draft inputs, not runnable activation or provider qualification.

Actual ClosureResolver fixture exercises two-node coordinator success using real typed compilation/BuildInputClosure and controlled persistence/publication dependencies. Begin/publish/register failure scenarios verify no later phase runs. Go render suites passed. Concrete SQLStore/RenderInputAdmission/RenderTransportPublisher satisfy the interfaces but complete wiring into API, durable recovery and a real object-store/SQL integration scenario remain pending.

### V26: coordinator cancellation boundaries and stable retry evidence

RenderDraftCoordinator now rejects invalid run/project/snapshot/policy pins before dependency I/O and checks cancellation before persistence, between publication and registration, and after final registration. Controlled callbacks that return successfully after cancelling cannot cause the next phase to start. The returned compiled plan remains available after a post-begin failure so callers can recover the same immutable draft.

Actual typed closure fixture cancels during begin/publish/register and verifies exact phase stop; a registration failure followed by retry on the same run/project/snapshot preserves config SHA, task UUID and input digest. Render/input Go suites passed. This proves coordinator behavior with controlled persistence, not durable crash recovery against SQL/object storage; that integration remains pending.

### V26: repeat frozen render authority before generic graph activation

Migration00405 adds private v26_render_activation_ready and calls it from seal_v2_graph for full-render plans/tasks before the existing consent and core graph gates. Under run/task locks it requires an unsealed live draft, exact frozen task count, every planned task already blocked/unattempted, complete structural closure readiness and successful restaging against live registered recipe/source/style/model authority. Missing planned tasks cannot be inserted by this predicate; task existence is checked before restaging. It neither registers providers nor bypasses core operation-registry gates.

SQL fixture verifies rejection before input sealing, positive render authority after sealing, denial for an extra task and mistyped root/source, and denial after session revocation. Public graph sealing must also reject the revoked session. Render scenario and four existing lens SQL scenarios passed on405 migrations. Positive runnable activation and real worker qualification remain unverified; API wiring, durable object-store recovery, full recursive closure admission and additional V20–V32 scope remain outstanding.

### V26: render operation catalog and consistent routing class

Migration00406 registers render.compose version1 on gpu.render_compose with backend choices cpu/cuda/mps and seven exact output-role policies matching task staging: document,working_image,pixels,alpha,mask_pixels,mask,report. v2_enabled remains false until genuine qualification. Registry identity is operation/version with one resource class, so SQL staging, Go draft/admission and worker operation installation now consistently use gpu scheduling class; cpu is a backend choice in that profile, as with existing GPU operations, not a separate operation registry record. The runtime component change requires fresh runtime pin/qualification before installation.

Go render tests and12 worker task tests passed. SQL fixture verifies catalog/routing/policy count and proves public graph sealing still rejects an otherwise structurally admitted draft while the operation is disabled, leaving the run unsealed. No production qualification or successful runnable activation is claimed. API mounting and full SQL/object-store pipeline remain pending.

### V26: regression verification after operation routing integration

All backend internal suites and56 render task/execution/profile/runtime tests passed after catalog/routing changes. Added explicit cpu/cuda/mps cases in typed Go draft compilation and worker operation factory, confirming every backend retains the gpu queue class required by the operation/version registry; updated focused worker task suite14 tests passed. Documentation verifier passed2150 static and54 semantic checks. A pre-permission pytest cache write warning did not affect test results.

The full SQL stage/registration matrix is checked with406 migrations separately. These checks do not establish qualified runnable rendering: worker SQL attempt/fence admission, concrete provider installation, full object-store integration, durable recovery and API activation remain incomplete.

SQL regression result:18 stage/registration scenarios passed in41.55s on isolated databases applying all406 migrations. No production database changes were applied.

### V26: attempt-fenced SQL render admission foundation

Migration00407 adds worker-only admit_v26_render_recipe. It requires exact params bytes/SHA, running task operation/version/gpu class, matching attempt/fence/worker/digest, live lease/deadline and resource slot, registered DB principal and worker authorization, sealed noncancelled run, structural full-render closure, exact frozen profile/root bytes and all input artifact metadata ready/live. It repeats registered recipe/editor/source, accepted StyleV2 and optional learning model/descriptor authority. No table grants were added.

This is an incomplete admission foundation: positive live-attempt execution is not tested; authored layer/protection requests remain denied until dedicated SQL binding proof exists, and session correction/complete recursive child authority must still be completed. The function is not installed as a production worker adapter. Migration/ACL tests verify worker EXECUTE, API denial and false for absent scope; registration suite passed on407 migrations. This does not complete V26 or enable qualified runtime execution.

### V26: worker SQL admission adapter with exact root bytes

Added TaskDatabase.admit_render_recipe using existing DB retry/connect semantics and exact lease tenant/run/task/attempt/fence/worker/params/digest arguments. SQLRenderTaskAdmission validates operation/version/gpu class, params ref, bounded exact raw SHA and typed recipe identity, then requires the SQL predicate to return true. execute_render_task now retains canonical root bytes from read_render_document and passes those exact bytes to admission instead of reserializing the decoded recipe. Cancellation checks surround the SQL boundary. Runtime task component changed and requires repinning/qualification before deployment.

37 task/execution tests passed, including adapter success/SQL denial with exact byte transmission and corrupt bytes rejected before DB callback. These use controlled DB callbacks; live fenced SQL admission, layers/protection authorization and production operation installation remain incomplete.

### V26: pin DB admission transport and verify generation argument mapping

Render runtime component_paths now includes runtime/database.py, so qualification/build identity includes the SQL admission transport alongside task-boundary code. Any change to this shared adapter invalidates the renderer component pin and requires a fresh qualified runtime lock before deployment.

Database-adapter tests verify exact tenant/run/task/attempt7/fence19/worker/params/raw/digest mapping and require a literal boolean true SQL result; absent,false and integer1 responses reject. Runtime inventory test asserts transport inclusion.35 task/runtime tests passed. These tests use controlled query callbacks and do not prove positive live SQL leasing or production qualification. Remaining layers/protection and recursive admission scope is unchanged.

### V26: positive relational running-attempt admission

Added render_worker_admission_fixture to the successfully sealed relational render scenario. It provisions a test gpu node/worker/slot, marks the run/task running with live attempt1/fence1 and leases inside a rollback-only transaction, and calls the actual worker-only admit_v26_render_recipe as photo_worker. The positive case passed. Mutations of all ten admission arguments reject, as do slot fence mismatch, expired task/slot leases, unrelated worker DB principal and revoked editor session. Provisioning and running-state changes are discarded after the test.

This verifies actual PostgreSQL authority/fencing logic on407 migrations for a three-entry recipe/source/style metadata fixture. It does not qualify a GPU build, activate the public catalog, execute pixels or prove full recursive typed object closure. Authored layer/protection authorization and production runtime installation remain outstanding.

### V26: actual Python DB adapter through PostgreSQL admission

The live relational render admission fixture now invokes TaskDatabase.admit_render_recipe through its actual _query/SQL path. Only _connect is redirected to the existing rollback-only fixture transaction so unpublished synthetic provisioning is visible; the SQL function and parameter transport are not mocked. Worker role is scoped to each test connection context and restored afterward.

Adapter admits the current attempt/fence and rejects changed attempt/fence, mismatched resource-slot generation, expired leases, unrelated DB principal and revoked editor session in the actual PostgreSQL scenario. The focused test passed on407 migrations. This still does not test a separately authenticated production worker connection, complete typed recipe object bytes or qualified execution; those remain outstanding.

### V26: exact style/correction/override input authority at worker admission

Migration00408 adds private v26_render_direct_inputs_ready and invokes it in fenced admission. Base style, optional session corrections and up to32 override refs must have exact semantic input rows and pass project/version/editor artifact scope. Tenant context is established from the already-fenced task before scoped predicates, so a fresh worker connection need not inherit API tenant context. Authored layers/protection remain pending dedicated proofs.

Live SQL adapter fixture clears app.tenant_id at each connection boundary, admits the valid three-entry recipe/source/style fixture, rejects a StyleV2 input relabeled CorrectionPlan while preserving UUID/SHA/count, and admits again after rollback. Positive correction/override scopes and full recursive closure remain unverified. Focused SQL scenario passed on408 migrations.

### V26: positive correction/override relational scope

The live admission fixture now separately exercises v26_render_direct_inputs_ready with ready API draft-upload metadata and matching CorrectionPlan/Override task-input rows. It verifies positive project/version/editor scope, rejection of each substituted SHA, malformed overrides object and revoked session. The fixture and these additions are rollback-only, and the immutable running recipe/config are never rewritten. Focused SQL scenario passed on408 migrations.

The added upload bodies are minimal metadata fixtures, not typed CorrectionPlan/Override documents. This proves relational direct-input authority only; end-to-end typed correction/override loading, complete closure hashing, actual measured rendering and authored layer/protection admission remain outstanding.

### V26: frozen LayerStack root binding during staging

Migration00409 adds private v26_render_layer_stack_scope and invokes it during SQL render staging. A pinned stack requires bounded layer_jcs bytes with exact SHA, LayerStack header/source equality to frozen source binding, layers array up to32, project/version/editor artifact scope, and (when task supplied) exact typed semantic input row. Null stack rejects stray layer evidence. This validates the document root/header, not individual layer methods, mask expressions or parent result lineage.

Relational fixture publishes a typed empty LayerStack root through draft-upload metadata, registers its typed input and verifies positive source/project binding plus rejection of altered bytes. Focused SQL stage/admission scenario passed on409 migrations. Full authored layer/protection worker admission remains intentionally pending recursive child/parent/resource proofs; the existing worker denial has not been removed. This is not complete layer rendering or API integration evidence.

### V26: strict LayerStack root header JSON types

Migration00410 tightens layer-stack root scope to require an exact artifact ref object, exact source-binding fields and numeric schema_version. It preserves prior bounded byte/SHA/source/layer-array/provenance checks. This removes acceptance of string schema_version that differed from the typed Go LayerStack contract.

SQL fixture publishes malformed layer header metadata with genuinely matching bytes/SHA and project/version upload scope, then verifies rejection of string version, extra top-level field and object instead of layers array. These failures cannot be explained by a mismatched SHA or absent upload. Focused integration passed on410 migrations. Individual layer/mask/parent SQL authority and full worker admission remain pending.

### V26: admit pinned empty LayerStack in fenced worker SQL

Migration00411 replaces blanket rejection of every layer_stack ref with the exact layer-root scope and typed input check for empty stacks. Empty stacks are normal authored editor documents after all layers are removed and add no unresolved descendants. Nonempty authored stacks and protection still require unfinished recursive relational proofs and remain denied; their implementation scope is not removed.

New relational scenario uploads an actual empty LayerStack bound to the source, pins its exact bytes/ref in recipe/run evidence, registers four semantic inputs, seals transport, then positively admits a synthetic running worker through TaskDatabase. Relabeling the LayerStack input while preserving UUID/SHA/count causes admission denial. Empty-stack and base fixture SQL scenarios passed on411 migrations. No complete typed FrozenRenderRecipe object-store path, production qualification or full authored layer execution is claimed.

### V26: align override count with recipe contract and admission regression

Inspection found a real mismatch: EditingRecipeV3 permits128 override refs, while the newly added SQL direct-input predicate imposed32. Migration00412 aligns the SQL maximum to128 without changing exact row/provenance checks. Boundary tests exercise128 authorized refs and rejection of129 at the predicate level (repeated refs reuse existing metadata; this is not an end-to-end128-document render). The focused empty-stack/admission scenario passed on412 migrations.

The broader stage/registration/lens regression run passed23 tests in53.79s;2150 static and54 semantic documentation checks passed. The regression run started before the limit change; the focused412 test verifies the final boundary adjustment. Nonempty authored stack/protection, full typed closure/object-store integration, real qualified providers, API mounting and durable recovery remain outstanding.

### V26: preserve compiled evidence after uncertain draft commit

RenderDraftCoordinator now returns the compiled immutable plan when Store.Begin errors instead of dropping it. A database connection loss can occur after commit, so callers need the exact config/task/digest evidence to reconcile or repeat the same request; returning it does not assert that a run was persisted. Publication still stops after the failed begin boundary.

Typed closure tests cover failures at begin and registration, exact plan return, no later phase after error and stable config/task/digest across retry. Go render/input suites passed. Durable persistent recovery/API reconciliation remain unfinished and this change does not simulate an actual connection loss after PostgreSQL commit.

### V26: scoped persisted draft recovery read

Migration00413 adds scheduler-only v26_render_draft_state exposing workflow/project/snapshot, immutable config/hash/policy and live reservation bytes for an active editor/session's unsealed noncancelled draft. No table grants are added; expired reservations or mismatched actor/session yield no row. SQLRenderDraftStore.Load uses DB-only transactions, requires scheduler role, validates deterministic workflow/nonzero identities/safe reservation and canonical config SHA before returning RecoveredRenderDraft. The result is evidence for reconciliation, not reconstructed providers/inventories or activation.

SQL scenario reads exact newly begun two-node draft evidence/hash/policy/reservation, denies different actor/session and checks recovery ACLs. Go render suites passed. SQL recovery predicate is integration-tested; actual Go Load against pgx, automatic restart reconstruction and independently committed crash-recovery/object-store scenarios remain pending. Nonempty layers/protection and complete production runtime/API remain outstanding.

### V26: validate typed frozen evidence on recovery

Added DecodeRecoveredRenderPlan/RecoveredRenderNode and invoked validation from SQLRenderDraftStore.Load. Recovery requires deterministic workflow/task IDs, pinned config/project/snapshot/session, valid digests, exact full FrozenRenderRecipe and WorkingImage evidence, layer stack binding, renderer/runtime/model/ICC profile pins and resource estimates at least the computed authored budget with zero provider peak. Aggregate outputs must fit the live reservation; safe integer bounds are retained. Returned nodes own profile bytes.

Actual typed closure fixture recovers a two-node compiled plan and rejects a substituted snapshot or reduced output reservation. Go render suite passed. This does not qualify a runtime/provider or prove live dependency scope; fresh scoped inventory resolution, matching saved digests, actual pgx recovery and durable restart/object-store reconciliation remain pending.

### V26: resume saved render input publication and registration

RenderDraftCoordinator.RecoverInputs connects validated persisted nodes to transport/registrar phases without recreating a run or changing runtime/profile choices. It requires the frozen session, freshly resolves every scoped inventory and matches each saved render digest before any publication, reconstructs deterministic gpu-class task evidence with copied saved profiles, then publishes/registers nodes with cancellation checks. Transport and registrar still repeat live SQL scope; no graph activation is performed.

Actual typed closure fixture recovers two nodes through real BuildInputClosure with controlled persistence, and rejects foreign sessions, incomplete inventories and changed inventory digests before publication. Go render tests passed. Real committed SQL/object-store restart, durable recovery queue/API orchestration and qualified execution remain pending.

### V26: pin protection metadata in immutable render plans and recovery

RenderDraftRequest now retains ProtectionRaw and compiled run nodes preserve exact protection_jcs. ValidateRenderProtectionEvidence requires the authored protection ref SHA to match bounded canonical MaskRef bytes, protected=true, the frozen source canvas hash and the actual WorkingImage transform chain. Unreferenced bytes are rejected. CompileRenderRunPlan and DecodeRecoveredRenderPlan enforce these checks, and the coordinator copies protection bytes before dependency calls.

Dedicated tests cover valid typed protection metadata, missing/changed bytes, a matching-SHA unprotected mask, a matching-SHA foreign canvas, foreign source transform and stray bytes for null protection. All backend internal Go suites passed, including v2render. These are metadata tests, not validation of binary mask pixels or tenant provenance. SQL worker admission still denies protection pending scoped relational proofs; production activation and full V26 completion are not established.

### V26: scoped published user protection at SQL staging and admission

Migration00414 adds private v26_render_protection_scope. It binds bounded exact SHA MaskRef/source bytes to protected=true, identical source canvas/transform, a ready immutable user_mask_binding for the same tenant/project/asset version/source/canvas, current editor/session authority and live published MaskPixels metadata with exact dimensions/bucket. A task requires exact typed MaskRef and normalized NpyArray input rows. API, scheduler and worker receive no direct EXECUTE grant on the helper.

Migration00415 invokes this predicate during staging and fenced worker admission and increases the staging memory/scratch minimum for the protection mask. Published user protection can now pass admission; masks without this verified publication lineage remain denied pending producer lineage implementation. Nonempty LayerStack admission remains unfinished, and render.compose stays disabled in the operation catalog.

The live relational protection scenario passed through real SQL upload/bind, stage, input transport registration/seal and TaskDatabase admission with controlled transaction persistence and synthetic object versions. Predicate tests passed for missing binding, wrong typed input, corrupt bytes/source pins, wrong asset version/canvas binding, expired binary, revoked session, and genuinely uploaded matching-SHA unprotected/foreign-canvas masks. This is not complete full-wire recipe/object-store/GPU execution evidence. The earlier staging regression passed17 scenarios on415 migrations; documentation checks passed2150 static and54 semantic checks. Production wiring, qualified providers and actual object-byte end-to-end execution remain outstanding.

Final protection/empty-stack/registration ACL regression passed5 tests in11.25s on415 migrations after fixture corrections. All new SQL predicates and their staging/worker calls were exercised; no production database was migrated during these tests.

### V26: canonical protection from the existing multi-output rasterizer

Migration00416 preserves the user-upload predicate as private v26_render_user_protection_scope and adds private v26_render_raster_protection_scope behind the existing staging/admission entry point. The raster branch accepts the existing non-V2 mask.rasterize/1 multi-output result format. It requires the exact current accepted asset-version master, a live undeleted verified asset in the project, editor/session authority, exact source ref among producer inputs, succeeded noncancelled producer, canonical result membership of BOTH mask_protected_ref and mask_protected, matching result/task/artifact attempt/fence, exact output roles/media and live verified immutable artifact versions. Mask/source bytes, protection flag, canvas and transform checks are retained. Consumer tasks require typed MaskRef/NpyArray rows. No direct helper grants are added.

The distinction from V2 is intentional: the current rasterizer produces multiple outputs, while V2 result guards require one document root. A V2 producer branch needs exact child-link/root proof and is not silently admitted by this legacy-format predicate. Broader segmentation/defect/generative producer lineage remains unfinished; this does not change the final scope.

Relational tests cover positive canonical raster metadata, wrong attempts/fences/role, expired binary, wrong operation/nonterminal producer, missing source input, nonexistent source version, deleted asset, incomplete canonical outputs, revoked session and private ACLs. A separate scenario carries this protection through actual SQL stage/input registration/seal and fenced TaskDatabase admission. Metadata and object versions are controlled synthetic fixtures; no real raster invocation, object-store byte closure or GPU qualification is claimed. Final raster/user-protection/empty-stack/registration regression passed7 tests in22.21s on416 migrations. Documentation checks passed2150 static and54 semantic checks.

### V26: typed protection closure through compilation and recovery

The actual ClosureResolver Go fixture now runs both base and protected compositions. The protected variant uses a typed canonical MaskRef bound to the real WorkingImage fixture and a declared pinned binary object, authors an actual full frozen recipe, then exercises resolution, deterministic task compilation, coordinator phase/retry/cancellation boundaries, transport closure and two-node persisted-plan decoding/input recovery. Recovery explicitly asserts byte-for-byte ProtectionJCS preservation. The absent optional authored protected_mask field is omitted, matching the typed recipe contract rather than serializing null.

Go v2render tests passed. Catalog/object persistence remains controlled; binary arrays are metadata fixtures and are not read by this closure test. Actual SQL plus full typed recipe/object-store recovery and qualified render execution remain outstanding.

### V26: bounded run compilation and 5000-frame batch partitioning

CompileRenderRunPlan now enforces the same1MiB config limit as SQLRenderDraftStore.Begin and persisted recovery, preventing compilation of a plan that persistence cannot accept. CompileRenderBatchPlans/CompiledRenderBatchPart preserve the full ordered selection of up to5000 frames by partitioning canonical node evidence into bounded runs. Child run IDs derive deterministically from batch UUID and part index; each part exposes its first selection ordinal. Tenant/actor consistency, all typed document/profile/inventory checks and aggregate safe output bounds remain enforced. Final parts are independently compiled with their actual task IDs/digests.

The actual typed Go closure fixture compiles a5000-frame selection into multiple bounded plans, proves contiguous coverage of all5000 frames and deterministic child run identities, and decodes each persisted-plan representation successfully. A single oversized run is rejected. Protected recovery also rejects removal of ProtectionJCS even after the enclosing config SHA is recomputed. Go v2render suite passed in18.480s.

This is pure compilation, not persisted batch orchestration or5000-frame execution. Durable batch manifest, per-part begin/publication reconciliation, aggregate progress/cancellation and API integration remain necessary; no large batch production completion is claimed.

### V26: durable compact batch manifest and scoped reconciliation reads

Migration00417 persists a compact parent using existing tenant-scoped runs(kind=bulk, unsealed draft), avoiding a new tenant table. begin_v26_render_batch validates exact manifest fields, frozen snapshot/editor/session authority, schema/policy SHA, deterministic child run IDs, contiguous ranges, safe output totals and exact snapshot item-count coverage. Advisory locking serializes initial creation/replay. Exact replay accepts; changed/cancelled parent evidence rejects. This reserves no child output quota and creates no child tasks.

Scheduler-only v26_render_batch_manifest/v26_render_batch_parts expose scoped immutable identity and child/task counts. A child only matches if tenant/actor/project/snapshot/session/config SHA agree. Missing children are explicitly reported missing with zero task/completion counts. No aggregate succeeded state is inferred from an empty batch. Existing tenant purge of runs covers this parent without new table grants or purge allowlists.

BuildRenderBatchManifest validates actual decoded child evidence, task IDs, ordered ranges and session before canonical compact serialization; policy SHA is retained. SQLRenderDraftStore.BeginBatch and LoadBatch connect scheduler-only transactions. DecodeRenderBatchManifest authenticates canonical SHA, exact typed field inventory, session, contiguous5000-node bounds and deterministic IDs. Tests reject rehashed malformed fields/ranges/IDs/digests and foreign sessions, while accepting PostgreSQL-style whitespace with canonical digest.

The Go typed5000-node fixture builds the compact manifest after compiling/recovering all parts; these5000 nodes repeat the same source metadata fixture, not5000 independent image executions. Go v2render passed in21.169s. SQL manifest/replay/gap/count/ACL/revocation and registration regressions passed3 tests in7.15s on417 migrations. Documentation checks passed2150 static/54 semantic checks. Actual Go pgx BeginBatch/LoadBatch integration is not yet exercised.

Durable orchestration remains incomplete: child plans not yet staged cannot be reconstructed from hashes alone. Persisting their complete prepared evidence before crash-safe dispatch, per-part start/input recovery, progress finalization/cancellation propagation, API/UI wiring and qualified execution remain outstanding. This parent manifest is not complete batch processing.

### V26: atomically stage every full child plan before external publication

Migration00418 adds scheduler-only begin_v26_render_batch_part. Each full child config must match its parent ordinal/config SHA/frame count; tenant/actor/session/cancel state and parent policy/output estimate are pinned. The existing begin_v26_render_draft then validates all source/style/learning/protection authority, reserves quota and creates blocked tasks. Child workflow/task identities remain deterministic.

SQLRenderDraftStore.BeginBatch now calls parent creation and every child staging function inside ONE DB-only transaction, retaining owned config strings before retry. Any rejection/error/cancellation returns an error from the transaction, rolling back earlier parent/child/quota changes. No object-store calls occur there. Upon successful commit, all full child configs exist for recovery; BeginBatch no longer leaves planned child evidence only in hashes. Connection loss after commit still requires reconciliation rather than assuming failure.

Scheduler-only v26_render_batch_part_state and SQLRenderDraftStore.LoadBatchPart recover an exact parent-bound child in one scoped query. Cancelled parents, wrong ordinals/session, missing children, stale reservation or mismatched config/policy/output/count yield no recovery row. Go additionally checks deterministic child ID and full typed recovered evidence. Publication/activation remain separate.

SQL fixture creates an actual parent, stages/replays its blocked child, rejects altered raw config/foreign session, reads exact persisted config/hash/quota and reports one draft task with zero completions. User/raster protection plus manifest regression passed3 tests in7.12s on418 migrations. Focused Go manifest/protection suites passed. SQL functions are integration-tested; actual Go pgx BeginBatch multi-child rollback/commit-loss and LoadBatchPart execution are still untested. This does not establish complete batch orchestration, cancellation propagation, runnable production execution or API/UI integration.

### V26: prepare and resume batch input publication from persisted evidence

RenderBatchCoordinator/RenderBatchStore connect full scoped resolution and batch compilation to atomic BeginBatch, then reload persisted parent/child evidence for publication. Prepare owns all request document bytes, validates common tenant/actor/session and policy before resolver calls, retains compiled parts on uncertain begin/publication error, and checks cancellation at each boundary. No graph activation occurs.

RecoverInputs verifies parent manifest SHA/identity/cancel flag and every child config/project/snapshot/policy/reservation/count against the manifest. It freshly resolves every node's scoped inventory and verifies saved input digests for ALL parts before the first publication. RenderDraftCoordinator's existing recovery phases were factored into shared prepareRecoveredInputs/publishRecoveredInputs; publication still rechecks current SQL transport/registration authority through its dependencies.

The actual typed closure fixture covers two persisted child parts, both base/protected compositions, real BuildInputClosure publication with controlled persistence, failed last-part read, mismatched late child SHA, late inventory drift and cancelled parent—all negative cases stop before publication. Primary preparation tests prove retained immutable plans on begin failure, stable retry SHA and no publication after cancellation at the commit boundary. Go v2render passed in21.279s. This does not exercise actual pgx batch atomic commit, object-store crash recovery, durable dispatch after process restart, cancellation propagation during publication or qualified worker execution. Batch API/UI and full nonempty authored layer execution remain unfinished.

### V26: batch cancellation through existing API and task drain

Migration00419 adds cancel_v26_render_batch for API/scheduler callers with exact creator/session/project authority. It sets cancellation/cancelling on the parent and matching nonterminal child runs, increments revisions once and writes one audit record. Replays are idempotent; workers receive no direct EXECUTE permission. No task lease/fence/slot is changed here: existing idle cancellation, worker heartbeat and recovery paths own attempt/resource settlement. Revision exhaustion raises/catches a subtransaction error so prior child flag changes roll back atomically.

The existing finalize_run_cancellation now skips a batch parent until its matching child runs become terminal, with the guard repeated after acquiring the run lock. It continues draining/finalizing children through the ordinary scheduler. Parent creation and cancellation lock the tenant before parent/child rows; batch creation now takes the exclusive tenant lock initially to avoid a later shared-lock upgrade while cancellation waits on the parent. Concurrent lock contention stress remains untested.

run.Service.Cancel detects V26 batch metadata before generic parent locking and calls the scoped batch function, preserving the existing public run cancellation route. SQLRenderDraftStore.CancelBatch exposes the scheduler path. Other run kinds retain their original cancellation/audit behavior.

SQL tests verify wrong-session denial, API-role propagation/replay, no part staging/recovery after cancellation, parent finalization blocked before idle drain, task fence increment through cancel_idle_task, child then parent cancellation, single audit and rollback when parent revision is exhausted. Actual Go run.Service.Cancel executes against an isolated committed419-migration database under photo_api, testing ordinary and batch cancellation/replay/audit. Its batch rows are controlled metadata, not full renderer execution. API Go plus SQL lifecycle regression passed2 tests in5.80s; the final revision-exhaustion SQL case passed separately. Go run/v2render suites and2150 static/54 semantic documentation checks passed.

Running GPU attempt cancellation, lease/reaper races, full pgx BeginBatch crash recovery, aggregate success/progress finalization, batch creation/UI and full authored layer execution remain outstanding. This does not establish full project completion.

### V26: aggregate batch counts in existing run Get/List/cancel responses

Migration00420 adds tenant-context-bound read-only v26_render_batch_counts for API/scheduler roles. Total is the immutable planned frame count, including missing children. Terminal counts use only exact tenant/creator/project/snapshot/session/config-SHA children and planned render.compose/1 task IDs with matching params/input digest. Duplicate task occurrences are deduplicated. Pending reviews are counted over matching child runs. Existing API failed counts include cancelled tasks; a finalized cancelled parent counts remaining never-started frames as cancelled so cancellation completion reaches100% without fabricating success.

run.Service Get/List/cancel responses now apply these counts for V26 bulk parents. Other bulk operations retain normal counters when the projection returns no row. Progress calculation is shared with normal runs. List closes its result cursor before issuing projection queries on the transaction, avoiding a busy pgx connection. The SQL projection materializes intermediate scopes and disables JIT locally; no global DB setting changes.

Actual Go API service integration checks parent planned count/progress via cancel, Get and List despite no child task in that controlled metadata fixture. SQL lifecycle tests check blocked, skipped and cancelled planned-frame counts and cross-tenant denial. Final focused integration run passed2 tests in39.55s on420 migrations; duration breakdown showed schema setup dominated(22.37s/8.21s), while Go API call was3.10s and SQL lifecycle call1.76s. These timings are not a5000-image production benchmark. Go run suites and2150 static/54 semantic documentation checks passed.

Parent success/running status aggregation and finalization, restart dispatch, full pgx batch atomicity/crash recovery, batch creation/UI and qualified full authored rendering remain unfinished. Counters do not establish execution readiness or full project completion.

### V26: aggregate display stage without inventing terminal parent state

Migration00421 adds tenant-context-bound v26_render_batch_stage and run.Service applies it to the existing stage field for batch Get/List/cancel responses. Matching child identity uses tenant, creator, project, snapshot, session and config SHA. Missing/unsealed children report preparing; sealed child phases report queued/running/waiting_review. All-terminal children report finalizing, while parent cancellation/terminal status remains authoritative. The persisted status is not overwritten by a read projection, and finalizing does not mean success or qualified output.

Controlled SQL metadata tests cover preparing, queued, running, waiting_review, failed-child finalizing and cancelled parent through the existing lifecycle. Actual Go API integration asserts cancelling stage alongside planned counts. Focused integration passed2 tests in7.89s on421 migrations; Go run tests passed. Finalizer, aggregate durable transitions/event emission, crash-safe dispatch and full renderer/API creation readiness remain outstanding.

### V26: durable batch terminal finalization

Migration00422 adds scheduler-only finalize_v26_render_batch. It locks the active tenant, parent and matching children before checking all declared parts and planned task identities. Pending reviews and unfinished tasks block completion. Succeeded tasks require an exact current-attempt/fence canonical result with one ready, verified immutable root and matching output SHA; the private readiness helper is not directly granted to service roles. Parent completion seals the graph, increments revision and emits one terminal run event. Failed-only batches fail, mixed success/error batches complete_with_errors, and otherwise completed/skipped tasks complete the parent. Cancellation remains handled by the existing cancellation path. A trigger rejects reopening a terminal child after its batch has finalized.

The workflow scheduler runs this finalizer in a dedicated expansion loop, includes it in startup schema checks and readiness, and waits for loop shutdown. An ambiguous PL/pgSQL task alias discovered by integration testing was corrected.

Controlled SQL metadata tests verify failed finalization, replay without duplicate events, rejection of reopening, a terminal child with an unfinished task blocking finalization, and completion of a skipped-only batch. Focused lifecycle test passed1 in11.58s; separate batch manifest, actual Go cancellation service, user-mask and raster-mask SQL tests passed4 in19.32s. Go workflow/run/v2render suites passed;2150 static and54 semantic documentation checks passed. These fixtures do not execute GPU rendering. Positive canonical successful-output finalization, mixed-output aggregation, concurrency/retry races and finalizer fairness under corrupted metadata remain unverified. Retry API error mapping for the terminal-child guard remains outstanding.

Durable intermediate parent transitions, mounted batch creation and recovery dispatch, actual pgx batch transaction/crash recovery, full authored layer/mask execution and model qualification remain unfinished. This is incremental implementation, not completion of V26 or the full project.

### V26: terminal batch child retry conflict and scoped attempt read

run.Service.Retry now translates only SQLSTATE23514 with the exact V26_BATCH_ALREADY_TERMINAL guard message into the existing RUN_NOT_RETRYABLE409 domain error. Other database errors propagate unchanged. The error leaves tenant.WithTenant to roll back earlier task resets; no retry audit is committed.

Actual Go API-role integration exposed a preexisting missing SELECT privilege on task_attempts used by Retry. Forward-only migration00423 grants SELECT only on tenant_id, task_id, attempt and error, retaining existing forced tenant RLS. It does not grant profile access or mutation privileges.

The committed isolated423-migration test exercises actual Service.Retry with a retryable failed attempt: a finalized parent rejects the child retry, run state/revision and failed task remain unchanged, and no audit appears. With a controlled active-parent fixture, retry succeeds, advances child revision, readies the task and writes one audit. A separate API transaction with a foreign tenant context sees zero matching attempts; column privilege assertions verify error readable and profile unreadable. Final integration passed1 in4.55s; Go run/workflow suites passed. The fixture uses controlled metadata and does not execute a renderer or exercise finalizer/retry concurrency. Full V26 and full project implementation remain incomplete.

### V26: skip incomplete planned-task metadata during finalizer candidate selection

Migration00424 replaces finalize_v26_render_batch without changing its grants. Before choosing the oldest candidate it checks declared frame count against the child node array and left-joins each planned node to its exact tenant/run task ID, operation/version, params artifact and input digest. Missing or mismatched planned tasks and nonterminal tasks exclude that candidate. JSON array expansion uses a guarded empty-array fallback for malformed shapes. Existing locked checks still revalidate identity and canonical successful output before committing terminal state.

Previously an older terminal child with a missing planned task could be selected repeatedly, fail locked validation and prevent a later valid batch from being selected. A controlled SQL fixture now creates two fully staged one-part parents, removes the oldest child task, marks both children failed and verifies that the second parent finalizes exactly once while the first remains draft at its original revision. This tests metadata corruption deliberately; it does not provide an API for modifying frozen plans.

Full render staging matrix, user/raster protection variants and actual Go API cancellation/retry passed20 tests in52.12s against424 migrations. Documentation checks passed2150 static/54 semantic. Concurrent row-lock contention, inactive-tenant candidate fairness, positive canonical successful output and mixed-output finalization remain unverified. Real pgx BeginBatch atomic multi-part failure/unknown-commit/restart recovery remains outstanding; this turn resolved a finalizer starvation issue found while preparing that integration. Full V26 and full project completion are not established.

### V26: active-tenant finalizer selection and canonical-success SQL evidence

Migration00425 filters inactive tenants before choosing the oldest candidate. It retains the active-tenant row lock and all existing locked identity/task/result checks, so the preliminary filter does not replace lifecycle revalidation. A ready batch belonging to a deleting tenant no longer prevents a later ready batch belonging to an active tenant from being selected.

New rollback-only controlled metadata tests create failed batches in two tenants, mark the older tenant deleting without starting deletion or purging anything, and verify that the active batch finalizes while the inactive batch keeps its draft revision and emits no event. Restoring active status in the fixture makes the older batch eligible again.

A separate canonical-success test creates V2 workflow/profile, task attempt, ready document artifact, matching ready output upload and canonical task result metadata, then finalizes a succeeded planned task exactly once. Expired root, mismatched produced fence and advanced live task fence block finalization. Existing immutable-content triggers reject object-version/SHA/result-fence mutation; tests assert these denials rather than bypassing the guards. The final parent is completed, sealed, revision2 with one completed event. The fixture does not publish object bytes or run a renderer, so this establishes relational finalization only.

Both new integration tests passed2 in43.47s on425 migrations; Go workflow/run suites passed. Full GPU execution, mixed-output aggregate finalization, concurrent retry/finalizer contention, real pgx multi-part BeginBatch atomicity/unknown-commit/recovery, public batch creation and durable intermediate parent transitions remain outstanding. This does not establish V26 or full project completion.

### V26: mixed successful/failed frame aggregate verification

The finalizer SQL fixture now supports two declared planned tasks within one child. One task has a matching V2 workflow, current attempt/fence, ready verified document/upload and canonical result; the other remains failed. The V2 fail-fast constraint prohibits completed_with_errors on the child, so the child remains failed while the parent finalizer produces completed_with_errors. Tests preserve this constraint rather than weakening it.

The test asserts parent revision2, graph sealed, exactly one run.completed_with_errors event and aggregate counts(total2,succeeded1,failed1,skipped0,review0). Expired root and mismatched producer/live task fence still prevent parent settlement, including the mixed case. The single-success completed case and cross-tenant scheduling case remain covered. Final focused integration passed3 in14.60s.

These are controlled relational metadata fixtures, not full batch creation: the two-task case does not prove snapshot selection/creation admission or object-byte qualification. Actual multi-part BeginBatch transactions, restart recovery, public batch creation, intermediate durable parent transitions and renderer execution remain outstanding. Full project completion is unproven.

### V26: durable intermediate parent states and existing API filters

Migration00426 adds a private aggregate-state helper and scheduler-only sync_v26_render_batch_state. It derives draft/queued/running/waiting_review from exact scoped child identity, excludes inactive tenants and cancellation/terminal parents, locks the active tenant then parent, rechecks the derived state, increments revision and writes one principal run.<state> event on a change. Preparing maps to draft; finalizing maps to running until the terminal finalizer proves completion. A cancelling child alone does not request parent cancellation. Phase updates do not seal the parent graph or invent terminal success.

RenderBatchFinalizer.ProcessOne first attempts terminal finalization; if no batch finalized, it synchronizes one durable intermediate phase. Workflow startup schema readiness also requires the sync function. This reuses the existing expansion-loop lifecycle rather than adding another independent writer.

Controlled SQL lifecycle tests cover queued/running/waiting_review and failed-child finalizing-to-running, idempotent sync, revision and single-event emission while the parent stays unsealed. Combined SQL finalizer/lifecycle tests passed4 in15.49s. Actual Go API integration commits a scheduler-role sync for an active child and verifies that Service.List filtered by running includes the parent with running stage and revision3; cancellation/retry regressions remain covered. This integration passed1 in5.59s. Go workflow suite and2150 static/54 semantic documentation checks passed.

The intermediate state is an eventually synchronized snapshot of child phases; concurrent child phase changes may require the next loop pass. Full phase-transition contention/stress and frontend event handling are not yet verified. Full pgx multi-part BeginBatch atomicity/unknown-commit/recovery, public batch creation/dispatch and qualified renderer execution remain incomplete. Existing draft-only creation admission still rejects restarting BeginBatch after activation; recovery must use the scoped persisted recovery path. Full project completion is not established.

### V26: mounted run-screen event invalidation and recovery polling

The existing photographer run route now mounts watchRun, connects the authenticated tenant SSE stream and reloads run catalog state when an event payload identifies the current run. All phase/terminal event types can invalidate the catalog; payload status is never adopted as authoritative state. Unrelated run events advance the stream cursor without causing a reload. Reconnect uses Last-Event-ID through the existing API client; EVENTS_EXPIRED410 resets the cursor and polling refreshes the current catalog state. Two-second polling remains available during disconnects, including retry after a formerly terminal state.

Loads are serialized/coalesced. Responses for a different run, invalid/unsafe revision or older revision cannot replace the current state. Mutation responses pass through the same revision guard, so an older in-flight GET cannot undo a newer cancel/retry response. Cleanup aborts the stream and GETs and removes polling/reconnect timers. The route clears prior run state on navigation, avoids accepting mutation responses into a different watcher and retains mutation busy state during background refreshes.

Focused watcher tests verify matching/unrelated events, authoritative GET instead of payload state, older-response rejection, transient load recovery, reconnect cursor preservation,410 cursor reset and absence of polling after abort. Two tests passed; photographer TypeScript typecheck passed. These are watcher tests, not a rendered browser/native route or live SSE transport test. Full UI integration and batch creation/gallery remain incomplete, as do real pgx multi-part BeginBatch atomicity/unknown-commit/recovery and qualified renderer execution. Full project completion is not established.

### V26: mounted run route DOM lifecycle verification and access revocation

Added rendered RunScreen tests using the actual route, actual watchRun and React Native Web under jsdom; the authenticated transport and Expo routing are controlled adapters. Tests exercise initial failed run, Retry POST with expected revision, an older pending GET resolving after the newer retry response, subsequent completed event refresh, navigation to another tenant/run while cancellation is pending, and unmount stream abortion. The old cancellation response does not replace the new scope.

A gap found during this review was that403 load errors left previously displayed metadata and mutation controls visible. watchRun now passes the load error to its failure callback. The route clears run metadata and aborts monitoring on401/403/404 from a catalog load or cancellation/retry response; transient errors retain recovery polling. Mutation lifecycle guards still prevent late responses from affecting another watcher. Rendered tests cover revoked access both during event-driven loading and cancellation, checking metadata/control removal and stream abortion.

Combined route/watcher tests passed6; TypeScript typecheck passed. These tests render the route but do not run a real browser/native app, authenticated HTTP service or live network SSE. Batch creation/gallery, pgx multi-part BeginBatch crash recovery and qualified full rendering remain unfinished. Full project completion is not established.

### V26: bounded run-monitor polling and stream reconnect pressure

watchRun now polls a known terminal run every10 seconds instead of every2 seconds. Event invalidation remains immediate and a newly accepted active state returns to2-second polling. Polling therefore still detects retry from another client when streaming is unavailable. Consecutive stream failures use2/4/8/16/30-second capped reconnect delays, with received events resetting the delay; abort cancels both the poll interval and pending reconnect delay. Revision guarding and serialized/coalesced loads remain unchanged.

New fake-clock tests verify no terminal polling before10 seconds, detection of a retry and return to active cadence, six reconnect attempts across60 simulated seconds of continuous stream failure, and no additional attempts after abort. Combined watcher/rendered route tests passed8; TypeScript typecheck passed after implementation. These timings describe client scheduling, not server capacity or a production benchmark. Live native/browser SSE and backend deployment capacity remain unverified. Full V26 and full project implementation remain incomplete.

### V26: stream access-denial propagation

watchRun now forwards stream401/403/404 failures through the same access-denial callback as catalog failures. It locally blocks further polling/reconnect requests and acceptance of outstanding GET responses after denial, even when a callback does not itself abort. The mounted route aborts its controller, removes displayed run metadata and controls and shows its load error.410 still resets the resume cursor, while transient stream failures retain backoff/polling recovery.

A watcher test denies the stream while the initial GET is pending, then resolves that GET and advances60 simulated seconds: no metadata is accepted and neither polling nor reconnect issues another request. A rendered-route test denies an established stream after the run is visible and verifies immediate removal and controller abortion without another catalog request. Combined watcher/route tests passed10; TypeScript typecheck passed after implementation. This does not verify live native/network SSE or establish full V26/project completion.

### V26: bounded scheduler passes without phase-update starvation

RenderBatchFinalizer.ProcessOne now attempts both terminal finalization and intermediate state synchronization on each healthy pass, returning whether either operation changed state. Previously a successful finalization returned immediately, so a continuously replenished terminal queue could indefinitely defer intermediate phase updates. Each SQL operation remains independently bounded to one parent and retains its existing transactional locks/grants. Context cancellation prevents starting the next operation.

A finalizer database error still stops the pass and propagates. If finalization committed but synchronization fails, the adapter preserves changed=true together with the error; the expansion loop marks readiness failed and backs off without pretending to roll back the already committed finalization.

New adapter tests cover all four combinations of terminal/intermediate progress, fixed invocation order, early finalizer failure, later sync failure preserving committed progress and pre-cancelled context issuing no query. The workflow suite passed. These tests control pgx rows; existing SQL/API integrations independently cover both functions, but a live combined adapter stress test remains outstanding. This change addresses starvation under a healthy continuing terminal queue, not all row-lock/error fairness cases. Full V26 and project completion remain unproven.

### V26: durable-state function grants and rollback guards

New integration tests exercise migration00426 access and transaction boundaries against all current migrations. Only photo_scheduler has direct EXECUTE on sync_v26_render_batch_state; API/worker/gateway roles do not. The aggregate desired-state helper is private even to scheduler. Actual API/worker calls are rejected with insufficient privilege, not merely checked through ACL metadata.

Controlled parent fixtures verify that completed/completed_with_errors/failed/cancelled/cancelling and a draft with cancel_requested remain unchanged with no phase event. Separate revision-limit and tenant-event-cursor-limit cases assert EXHAUSTED errors and rollback preserving parent status/revision with no event. These tests do not alter the existing forward-only migration or bypass its guards.

All9 new integration cases passed in43.89s;2150 static/54 semantic documentation checks passed. They establish scoped SQL invariants, not live scheduler contention, object-store execution or full product completion. Real pgx multi-part batch creation/recovery, public batch submission/gallery and qualified render providers remain outstanding.

### V26: actual pgx scheduler combined-pass integration

Added an isolated committed-database integration invoking the actual Go RenderBatchFinalizer through a pgx pool configured as photo_scheduler. Two controlled batches belong to different active tenants: one has a terminal failed child and another has a sealed queued child with blocked task metadata. One ProcessOne call finalizes the first parent and synchronizes the second parent to queued, both at revision2 with exactly one matching run event. A second pass is idle and emits no new transition. No SQL calls are mocked in this integration.

The Python wrapper creates a fresh database with all426 migrations, commits fixture metadata before starting Go and passes connection settings through environment without logging them. The database is removed by the existing integration fixture. The test passed1 in6.41s. Adapter tests also cover empty pgx row results without skipping synchronization or losing already committed finalizer progress; the workflow suite passed.

This verifies the real scheduler-role adapter and both SQL functions together, but the fixtures do not invoke BeginBatch, publish object bytes or execute a renderer. Continuous queue/concurrent row-lock stress, full pgx BeginBatch atomicity/crash recovery and mounted batch submission remain outstanding. Full V26/project completion is unproven.

### V26: deterministic phase-sync/cancellation transaction ordering

Added committed isolated-database tests using separate scheduler-role connections for sync_v26_render_batch_state and cancel_v26_render_batch. In cancellation-first order the uncommitted cancellation holds the tenant lock, phase sync returns false through SKIP LOCKED, and committed parent cancellation remains authoritative at revision2. In sync-first order the phase update holds the tenant lock; a concurrent cancellation call is confirmed waiting through pg_stat_activity wait_event_type=Lock before sync commits. Cancellation then completes and leaves the parent cancelling/cancel_requested at revision3.

After both orders, another phase sync and terminal-success finalizer return false and preserve cancellation state/revision. Only the sync-first fixture has one run.running event. The test bounds lock observation and statement duration and does not rely solely on sleep to claim contention. Both cases passed2 in7.35s against426 migrations.

These fixtures establish the two targeted transaction orderings using controlled failed-child metadata, not a full scheduler load benchmark, active GPU cancellation or complete crash recovery. Full pgx multi-part BeginBatch atomicity/recovery, public batch creation/gallery and qualified renderer execution remain outstanding. Full project completion is unproven.

### V26: child-part counters pinned to declared tasks

Migration00427 replaces v26_render_batch_parts with the same signature/grants and existing parent/session/tenant scope. Task counts now require a declared child node with the exact task ID, params artifact ID and input digest, in addition to render.compose/1. The guarded node-array expansion does not trust undeclared tasks. This closes a discrepancy with the aggregate parent counters: an extra render task in the child no longer inflates part progress.

A controlled SQL test inserts an undeclared failed task and verifies only the single planned task contributes. Separate rollback cases change the planned task input digest or params artifact to another valid same-tenant artifact and verify no task count contribution. The shared failed-batch fixture was corrected to include its input_snapshot_id manifest pin; the stricter part scope had exposed that missing test metadata. Combined part/finalizer tests passed4 in13.73s; the final params mismatch case passed separately1 in2.65s on427 migrations.

The parts reader remains scheduler-only; public client access has not been added or claimed. This verifies counters, not real multi-part creation, rendering or gallery readiness. Full pgx BeginBatch atomicity/recovery, public batch submission and qualified render execution remain outstanding. Full V26/project completion is unproven.

### V26: actual pgx LoadBatch across durable lifecycle phases

Added isolated committed-database integration for SQLRenderDraftStore.LoadBatch using a real pgx pool as photo_scheduler. It loads and strictly decodes the canonical manifest from the database, then verifies unchanged manifest SHA and declared part evidence after queued/running/waiting_review/failed parent transitions. A cancelling parent returns cancel_requested=true so the coordinator can reject input publication through its existing guard.

Foreign session, tenant and actor all return ErrExpired. Revoking the actual bound session also prevents recovery. These checks execute the real scoped SQL reader and canonical Go decoder, without mocked query rows. The integration passed1 in5.01s against427 migrations.

The fixture is controlled persisted manifest/failed-child metadata and does not invoke BeginBatch or decode a full persisted child FrozenRenderRecipe. Real pgx LoadBatchPart with fully typed evidence, multi-part BeginBatch atomicity/unknown-commit, complete coordinator crash/restart publication and public batch creation remain outstanding. This verifies parent manifest recovery, not full V26/project completion.

The complete Go v2render suite also passed after adding this integration (21.815s); the environment-gated live test remains proven by the separate Python-orchestrated run above.

### V26: active parent lifecycle required for part recovery

Migration00428 adds an explicit live parent-run join to v26_render_batch_part_state, requiring matching manifest SHA, no cancel flag and draft/queued/running/waiting_review status. Terminal/cancelling parent states cannot return a publication-eligible child even if child metadata appears draft eligible. Existing tenant/editor/session, exact child pins, typed draft scope and reservation checks remain unchanged; grants/signature are preserved. The manifest reader remains available for lifecycle inspection and cancellation evidence.

The SQL staging fixture first demonstrates a recoverable, fully staged child, then checks that queued/running/waiting_review parent metadata still permits recovery and completed/completed_with_errors/failed/cancelled/cancelling metadata excludes it. These are rollback-only parent-state transitions; no immutable child evidence is rewritten. Combined SQL lifecycle and actual Go LoadBatch regression passed2 in12.01s against428 migrations.

The SQL child fixture uses its existing bounded recipe header rather than a fully typed FrozenRenderRecipe, so this does not establish actual Go LoadBatchPart positive decoding. The reader is not itself a publication transaction lock; downstream admission still must revalidate live state. Real pgx multi-part BeginBatch atomicity/unknown-commit/recovery, mounted batch submission and qualified renderer execution remain outstanding. Full V26/project completion is unproven.

### V26: combined batch lifecycle regression on428 migrations

Ran the current batch integration set together after part-counter/recovery changes: manifest admission/read scope, declared-task counters, scheduler/private ACLs, terminal/cancel preservation, revision/cursor exhaustion rollback, deterministic sync/cancel contention, actual pgx combined scheduler pass, actual pgx manifest recovery and canonical-success/mixed-output finalization. All18 tests passed in65.11s against fresh isolated428-migration databases. Go workflow/run/v2render suites and2150 static/54 semantic documentation checks also passed.

This regression verifies compatibility of the targeted persisted lifecycle components and the corrected shared fixture. It does not include full typed pgx child recovery, actual BeginBatch multi-part atomicity/unknown-commit, complete object-store crash/restart publication, mounted batch submission/gallery, full layer/mask execution or GPU provider qualification. Passing this set is not proof of complete V26 or allV20–V32 requirements. Those missing end-to-end paths remain implementation work.

### V26: full typed recipe with actual pgx BeginBatch and LoadBatchPart

Added a two-phase isolated integration building a complete FrozenRenderRecipe through Go BuildFrozenRenderRecipe from the actual WorkingImage Python fixture and style example bytes. The actual closure resolver reads these document bodies through controlled catalog/object adapters; binary Npy metadata is controlled and no binary pixels are read. CompileRenderBatchPlans constructs the immutable single-part plan and resource profile. Temporary fixture/recipe files are created only under repository work and removed by the wrapper.

The database side seeds controlled ready immutable source/style/document upload metadata and invokes the real API-role register_v26_render_recipe function with the complete generated recipe. The second Go phase uses a real photo_scheduler pgx pool: BeginBatch persists the parent/child/reservation/tasks, exact replay succeeds, LoadBatchPart returns the persisted child and DecodeRecoveredRenderPlan validates full typed evidence. Config SHA and reserved bytes match the compiled plan. Actual CancelBatch then makes the previously recoverable part return ErrExpired while LoadBatch retains cancellation evidence.

Final integration passed1 in7.91s against428 migrations. This closes the positive actual Go single-part creation/recovery gap and causally verifies cancellation exclusion. It does not prove actual object-store upload bytes, live binary catalog authority, runtime provider qualification or renderer execution. Multi-part atomic rollback, unknown-commit/crash restart and mounted public batch submission remain outstanding. Full V26/project completion is unproven.

The full Go v2render suite passed after this integration (26.287s).

### V26: actual BeginBatch rollback after parent admission

Extended the full-typed pgx integration with a controlled expired registered recipe before creation. A separate scheduler transaction explicitly proves that the exact parent manifest is admitted despite the recipe expiry, then rolls back that probe. Actual SQLRenderDraftStore.BeginBatch subsequently returns ErrExpired when child admission checks the recipe. Database assertions find neither parent/child runs nor their quota reservations and show tenant_usage.reserved_bytes unchanged.

Restoring the fixture artifact expiry permits creation with the same batch ID, followed by successful replay, typed part recovery and cancellation exclusion from the previous integration. The final integration passed1 in4.60s against428 migrations. This causally verifies the real single-part failure boundary after successful parent admission; it is not a multi-part late-child rollback, lost COMMIT response or process-crash test. Controlled object/binary metadata limitations remain unchanged. Those multi-part/crash scenarios and full public batch/render integration remain outstanding. Full V26/project completion is unproven.

### V26: real late-part rollback and two-part recovery

The full-typed pgx fixture now creates a second distinct asset/version in the same project using the same controlled source bytes, builds/registers a second complete FrozenRenderRecipe, and freezes a two-item snapshot only after inserting both items. An initial test attempt hit V22_FROZEN_ITEMS_IMMUTABLE; the fixture was corrected to follow the normal mutable-before-freeze lifecycle without disabling guards. Both versions are distinct and pass the existing snapshot authority checks; they are not two independently processed photo files.

Two bounded child plans are compiled explicitly with CompileRenderRunPlan at deterministic render.batch:0/1 IDs and contiguous first ordinals, then validated by BuildRenderBatchManifest. This targets store transaction semantics, not the automatic size-based partitioner's boundary selection. A scheduler probe admits the parent and first child and confirms one staged task before rolling back. With only the second recipe expired, actual SQLRenderDraftStore.BeginBatch fails. Parent/children, tasks and their reservations are absent and tenant reserved bytes match the prior value.

After restoring the second recipe expiry, the same two-part batch creates successfully. Exact replay leaves two tasks, and both LoadBatchPart calls return full typed recoverable plans. Final integration passed1 in4.64s against428 migrations. This closes the actual late-child atomic rollback/retry and positive multi-part store recovery gap under controlled immutable object metadata. Lost COMMIT responses, process-crash/restart publication, actual object upload qualification, public batch submission and GPU rendering remain outstanding. Full V26/project completion is unproven.

### V26: typed two-part recovery from a separate Go process

The typed batch integration now ends its creator Go test process after committing/replaying the two-part batch and writes only expected identity/hash/reservation evidence to a temporary repository-local checkpoint. The Python wrapper then launches a distinct Go test process that has no compiled plans or previous pgx pool in memory. A new photo_scheduler pool executes actual LoadBatch and LoadBatchPart for both ordinals and DecodeRecoveredRenderPlan validates the full persisted documents.

The fresh process checks manifest SHA, cancellation=false, part count, run/project/snapshot IDs, child config SHA and reserved output bytes against the checkpoint. It reads full child configs from PostgreSQL rather than using checkpoint configs. The complete create/rollback/replay/cancel/cold-read integration passed1 in6.11s against428 migrations.

This proves persisted store evidence survives normal creator-process exit and supports typed recovery in another process. It is not an abrupt process crash or a dropped COMMIT response; neither object input publication nor graph activation is resumed here. Unknown-commit injection, full coordinator/object-store restart recovery, public submission/gallery and qualified renderer execution remain outstanding. Full V26/project completion is unproven.

### V26: abrupt creator-process termination after committed batch creation

The typed batch integration now runs both normal-exit and forced-termination variants. After successful real pgx creation/replay/recovery assertions and checkpoint writing, the crash variant kills only its own Go test process through os.Process.Kill. The Python wrapper requires a nonzero result and the actual go-test signal:killed diagnostic, plus the completed checkpoint; an arbitrary assertion/compiler failure cannot count as the intended crash. Existing service processes and production databases are not targeted.

A new Go process then loads the committed manifest and both full typed child plans from the isolated database and verifies all existing identity/hash/reservation checks. Both variants passed2 in12.11s against428 migrations. Temporary files remain repository-local and the isolated database is dropped by the existing fixture.

This proves recovery after abrupt creator termination at a known post-COMMIT point, before normal deferred pool cleanup. It does not inject an unknown COMMIT outcome, crash during input-object publication or worker execution, or resume graph activation. Those paths and public batch submission/gallery and GPU qualification remain outstanding. Full V26/project completion is unproven.

### V26: confirmed server COMMIT with lost client response

Added a test-only net.Conn adapter on the isolated local PostgreSQL connection. It recognizes the frontend COMMIT query, reads framed backend protocol messages and closes the connection only after receiving the exact CommandComplete COMMIT tag, before returning that tag to pgx. Authentication/query payloads are never logged or persisted. The fixture disables TLS only for this controlled local test connection so framing can be observed; production connection configuration is unchanged. Reads are serialized and injection occurrence is checked atomically.

Actual SQLRenderDraftStore.BeginBatch receives an error while the adapter proves the server confirmed COMMIT. Independent admin queries verify the parent and both child runs persisted. Retrying the same immutable plans through a normal scheduler pool succeeds with exactly two tasks, and the separate-process typed recovery checks still pass. Both normal-exit and abrupt-termination variants include this unknown-client-outcome case. Final integration passed2 in11.79s against428 migrations.

This establishes idempotent store reconciliation after a confirmed server commit whose response pgx did not receive, in addition to late-part rollback and post-commit process recovery. It does not test a crash during object publication, coordinator recovery dispatch, graph activation, GPU execution or public submission. Controlled source/binary metadata limitations remain unchanged; full V26/project completion is unproven.

### V26: cold coordinator recovery validates all parts before publication

Extended TestIntegrationRenderBatchTypedRestart to invoke actual RenderBatchCoordinator.RecoverInputs with SQLRenderDraftStore and a new scheduler pool in the separate restart process. Full frozen recipes and WorkingImage documents are reconstructed from the persisted SQL child configurations; the style fixture and immutable binary metadata remain controlled. Removing the second recipe from the controlled catalog causes recovery to fail with zero publication/registration calls, despite a valid first part. Restoring it permits publish/register for both parts in order.

A controlled registration failure stops before the next part. A subsequent recovery repeats both parts successfully. Actual SQL cancellation then causes coordinator recovery to return ErrExpired without any publication call. Both normal creator exit and forced creator-process kill scenarios passed (2 tests in 13.28s, all 428 migrations).

Publication and registration use renderCoordinatorProbe: it builds a real input closure through PrepareRenderInputClosure but uses controlled document publication and records registration without SQL writes. This verifies cold SQL recovery and coordinator phase boundaries; it does not establish real object upload, SQL input registration, activation, HTTP submission or GPU execution. Full V26/project completion remains unproven.

### V26: cold recovery through durable SQL publication and actual input admission

Replaced the final positive recovery boundary in the typed restart integration with RenderTransportPublisher, artifactjson.Publisher using SQLRepository on a real scheduler pool, and RenderInputAdmission. The external immutable object adapter validates each actual canonical document payload length and SHA, returns a controlled v1 object version and counts writes; it does not connect to S3. Binary NPY metadata is now explicitly seeded as a verified ready artifact in the isolated database; the payload is still not uploaded or decoded.

The first real attempt exposed a production integration defect: reserve_v2_input_document relied on v26_render_recipe_scope, which requires transaction-local app.tenant_id, but standalone SQLRepository publication calls did not set it. Migration 00429 establishes that context inside the scheduler-only reserve/complete functions. Existing owner/task/run/root/session/quota/fence checks and function ACLs are preserved. This is not a session-global setting. A foreign tenant paired with the actual child run/task is rejected before any object write.

Both recovered child inventories now register through actual RegisterInTransaction and seal in SQL. Publication produces exactly four object writes (one chunk and one manifest per part); exact recovery replay performs no additional writes. Narrow input-state assertions explicitly establish their required tenant context within a read transaction. Actual cancellation continues to prevent recovery. Ordinary exit and forced creator kill variants both passed: 2 tests in17.79s against429 migrations. Existing render staging integration passed19 in60.78s. Go v2render and artifactjson suites passed (v2render22.208s); deterministic documentation verification passed2150 static and54 semantic checks.

This closes the controlled-object/real-SQL transport and input registration recovery gap. It does not prove actual S3 versioned uploads, runnable activation/provider qualification, HTTP batch creation, mounted gallery dispatch or GPU image quality/performance. Full V26/project completion remains unproven.

### V26: live versioned transport recovery after creator crash and upload interruption

Added a third typed batch integration using the existing isolated repository-owned MinIO fixture. It uses the actual objectstore client for transport document uploads/recovery and actual artifactjson SQLRepository, RenderTransportPublisher and RenderInputAdmission after a forced creator Go-process kill. Credentials remain in the fixture's private environment/configuration; tests do not print them. The temporary MinIO container/data are cleaned up by the existing fixture.

The typed recovery object wrapper now injects a one-shot io.ErrUnexpectedEOF after the first successful immutable PUT but before Publisher can call SQL Complete. An independent database read confirms one pending backend document and no ready artifact for it. Coordinator retry resumes the durable reservation, attempts the same immutable key, and recovers its already stored version. Across both parts the test requires exactly four successful PUTs and exactly one RecoverImmutable call. Further recovery replay adds no PUTs.

For the live variant, Python independently reads all four exact recorded MinIO versions and verifies payload sizes, SHA256, schema version, and equality of backend-document/artifact version pins. Listing every object version in the isolated bucket confirms exactly four versions matching those recorded references; the interrupted PUT has not created a replacement version on retry. Tenant denial and cancellation checks remain included. Final three-mode integration passed3 in33.47s; full Go v2render suite passed33.329s against the current code. SQL fixture uses429 migrations.

This is real S3-compatible versioned IO for transport chunks/manifests. Source, recipe, style and NPY catalog data still use controlled metadata/body fixtures; no actual GPU render or real source binary verification is established. The upload/completion interruption is an injected error after successful PUT, not a second OS process kill between PUT and Complete. Public batch factory/dispatch, graph activation/qualification and full V26–V32 completion remain outstanding.

### V26: per-request SQL catalog assembly for multi-version batches

Added SQLRenderInputResolver, implementing CompositionInputResolver. For every ResolveTaskInputs call it constructs a fresh SQLArtifactCatalog using the supplied binding.AssetVersionID and fixed project/bucket/API-pool configuration, then delegates to the existing ClosureResolver. This permits coordinator reuse across batch parts without reusing a catalog scoped to the first photograph. API connections retain existing tenant.WithTenant/RLS/editor authorization; scheduler transport writes remain separate. Cancellation and missing dependencies/identity/version return before catalog access.

Full Go v2render suite passed21.112s and documentation verification passed2150 static/54 semantic checks. This verifies compatibility/compilation, not a positive multi-version SQL catalog integration. Existing typed batch recovery still resolves source inventories through controlled catalogs. Scheduler candidate claiming, mounted coordinator wiring, positive authoritative source closure and activation remain outstanding; the new resolver is not yet mounted in the service. Full implementation remains incomplete.

### V26: positive multi-version SQL closure recovery and structural graph readiness

The typed cold restart integration now replaces its final controlled catalog with actual SQLRenderInputResolver on a photo_api pool. It loads the current actor auth epoch from the database, resolves all source inventories through tenant.WithTenant/editor authorization and SQLArtifactCatalog, and retains the real scheduler publication/admission path. Each of the two distinct asset versions gets its own catalog; both share one controlled WorkingImage source document.

The isolated Python fixture adds an ingest run and asset.decode task plus matching producer/attempt/fence metadata for the ready source and working_rgb NPY artifact. This is explicit synthetic provenance, not a decoded photograph or canonical worker result. Controlled JSON bodies are mapped to the exact SQL object coordinates returned by authority lookup; transport documents still have a separate live MinIO variant. No SQL artifact scope or guard is disabled. Fixture setup initially failed mandatory tasks inputs/execution_profile guards; explicit required fields corrected it.

Expiring only the second root causes actual SQL scoped recovery to reject the batch before any object PUT. Restoring expiry permits recovery, including the existing injected upload-before-completion failure, durable retry and exact-version replay. Both full recovered child drafts now pass actual private v26_render_activation_ready checks, repeating frozen task-set/closure authority. This proves structural readiness only: render.compose remains unqualified/disabled in the operation catalog and no runnable graph/GPU execution is claimed.

Final three variants passed3 in18.76s on429 migrations; full Go v2render passed22.582s before the last integration-only readiness assertion. Scheduler candidate claiming/service wiring, source binary qualification, public batch submission and complete V26–V32 implementation remain outstanding.

### V26: role-checked SQL render batch coordinator factory

Added SQLRenderBatchFactory.ForProject. It assembles SQLRenderDraftStore for atomic preparation/recovery, SQLRenderInputResolver using the API pool and per-request asset catalog, RenderTransportPublisher using artifactjson.SQLRepository/Publisher, and RenderInputAdmission using the scheduler pool. Separate source/document buckets and reader/writer adapters support a read-only source location and independently writable transport location. Service retains ownership of pools and clients. The factory validates dependencies/cancellation/project and checks current_user=photo_api/photo_scheduler on the respective distinct pools; it does not authorize a project by itself or activate/qualify operations.

The typed cold restart test now obtains its final coordinator from the production factory instead of manually assembling dependencies. Swapping API/scheduler pools is rejected with ErrInvalid against actual PostgreSQL connections. Full frozen two-part recovery, SQL source authority, late expired-root denial, upload/completion interruption, replay, structural readiness, cancellation and live MinIO exact-version checks all remain included. Three variants passed3 in22.52s on429 migrations; full Go v2render suite passed23.087s. Documentation verifier passed2150 static/54 semantic checks.

This completes dependency assembly with positive integration coverage. It is not mounted in the long-running scheduler yet; candidate claims/recovery leases, service dispatch and public batch creation still need implementation. Source binary/GPU/provider qualification and the remaining V26–V32 scope are incomplete.

### V26: bounded recovery job with cross-session serialization

Added SQLRenderBatchRecovery.Recover around the production factory/coordinator. It uses a pinned scheduler connection and a tenant/batch-namespaced session advisory lock, returning done=false without publication when another session owns that batch. Object IO occurs without an open transaction. Work is bounded by an explicit timeout (positive, at most30 minutes); the scheduler pool needs at least2 connections because the lock session remains pinned while coordinator SQL uses another connection.

Cleanup uses an independent5-second context so caller cancellation cannot leave a locked session in the pool. Unknown acquisition/release outcomes hijack and close the connection rather than returning it for reuse; cleanup errors are preserved. OS connection loss releases the PostgreSQL session lock. Project/actor/session and current SQL authority are still revalidated through the coordinator; the lock does not authorize work or activate rendering. Serialization applies to callers using this recovery job.

Typed restart integrations now use this production job for the upload/completion interruption and successful recovery. An independent database connection holds the same lock: contending job returns false with zero PUTs. After injected upload failure, that independent connection successfully reacquires/unlocks the key, proving the failed job released its lock; retry then resumes SQL/MinIO recovery. Existing three controlled/live/crash variants passed3 in18.15s; full Go v2render passed23.099s on the current implementation.

This provides the bounded per-candidate recovery operation, not automatic candidate selection or a mounted scheduler loop. Durable candidate retry/fairness, service wiring and public submission remain unfinished, along with activation/GPU qualification and the broader project scope.

### V26: scheduler candidate discovery and bounded ProcessOne recovery

Migration00430 adds scheduler-only next_v26_render_recovery(after UUID), returning one active draft bulk parent whose matching draft child has an unsealed blocked render.compose task. Tenant/project/session and stored parent/child IDs are joined without unsafe JSON-to-UUID casts; malformed non-array parts yield no entries. Returned creator identity includes current user auth_epoch. Discovery is advisory only: scoped coordinator checks all current project/session/root/task authority before publication.

SQLRenderBatchRecovery now implements ProcessOne for the existing ExpansionJob interface. It serializes calls with a mutex, uses a bounded candidate read, advances its UUID cursor before recovery (including on failure/lock contention), and resets at end-of-scan. This avoids repeatedly selecting the first failed candidate within one live process. Cursor/retry order is in-memory and resets after service restart; no durable retry/backoff metadata is claimed. Recovery remains protected by the existing cross-session advisory lock and bounded IO operation.

The typed restart integrations call actual ProcessOne to discover and recover their persisted batch through the production factory, and call it again to confirm sealed inputs leave the candidate set. API role execution of the discovery function raises InsufficientPrivilege. All three controlled/live/crash variants passed3 in17.76s on430 migrations. Full Go v2render passed22.149s; documentation passed2150 static/54 semantic checks.

Automatic per-pass processing is implemented but not yet mounted in workflow service startup/readiness/shutdown. A separate API pool needs configuration there; durable retry fairness, public submission, activation/qualification and complete project scope remain outstanding.

### V26: mounted workflow recovery lifecycle and separate source API pool

RunScheduler now checks next_v26_render_recovery capability and starts SQLRenderBatchRecovery through its existing independent RunExpansionLoop. The new loop has readiness state and explicit cancellation/join on shutdown. The source API pool closes only after the loop has stopped; the shared scheduler pool/object client remain service-owned. NewRenderRecoveryProcessor validates the scheduler role and minimum2 connections, creates a separate4-connection photo_api pool with5-second statement/3-second lock timeouts, and bounds each recovery to2 minutes.

RENDER_SOURCE_DATABASE_URL optionally supplies separate source credentials. With no override, existing DATABASE_URL is reused under photo_api; the login must already have that role membership. Compose exposes the override and deploy/README documents it. No grants or qualifications are changed. Startup configuration/connection errors are sanitized rather than exposing DSNs.

The live typed integration additionally runs TestIntegrationRenderRecoveryRuntimeLoop in the workflow package using actual PostgreSQL and MinIO configuration after its batches are sealed/cancelled. It constructs the production processor, observes an idle healthy ProcessOne/RunExpansionLoop, cancels and waits for termination, and checks readiness=false/failure/materialized counters. This is real constructor/idle-loop lifecycle verification, not a full RunScheduler HTTP readiness test. Typed recovery still positively verifies candidate processing separately. Three variants including that runtime test passed3 in21.98s on430 migrations. workflow/v2render Go suites and2150 static/54 semantic documentation checks passed.

Automatic input recovery is mounted. Full service startup/load/HTTP lifecycle, durable retry fairness, public batch creation, render activation/provider/GPU qualification and remaining project functionality remain incomplete.

### V26: live source JSON closure with pinned old WorkingImage version

The live typed recovery variant now uses the actual objectstore client for source JSON reads as well as transport writes. WorkingImage, StyleV2 and first FrozenRenderRecipe are uploaded before their fixture ready records are inserted; the second FrozenRenderRecipe uses actual Go PutImmutable before its ready upload/registered-root metadata is created. The production SQLRenderBatchFactory receives the actual reader and matching live source bucket. Controlled modes retain the prior reader.

An initial attempt to replace bucket/version metadata on already ready fixture artifacts was rejected by reject_content_update. The fixture was corrected to upload-before-registration without disabling guards or editing immutable fields. Python-uploaded documents include the SHA256 user metadata required by objectstore.Stat/Open; its omission correctly caused ErrIntegrity in the next attempt and was fixed at upload time. Those failures show real objectstore/catalog boundaries now participate.

After registering the source's valid immutable version, the fixture deliberately writes a later `{}` version at the same key with its own checksum metadata. SQL continues to pin the original version. Actual source closure resolution/recovery succeeds through SQLRenderInputResolver and Store.Open, demonstrating that it reads the exact old version rather than the latest replacement. The isolated bucket version inventory includes the four original source documents, one intentional replacement and four transport documents; replay adds no extra versions. Existing candidate/lock/failure/cancellation/runtime-loop tests remain included.

Final three variants passed3 in19.98s on430 migrations; full Go v2render/workflow suites passed (v2render22.017s). NPY bytes and producer task evidence remain synthetic metadata, not verified decoded pixels or a canonical worker render result. Root ready records are still seeded fixture registration, not end-to-end HTTP document uploads. Full RunScheduler active recovery, public submission, GPU/provider qualification and project completion remain unproven.

### V26: real pinned NPY source through worker artifact materialization

The live typed fixture now uploads an actual300x400x3 little-endian float32 NPY before creating its artifact metadata. WorkingImage carries its real SHA256; the binary artifact has real size/key/version/checksum metadata and the existing explicit fixture producer lineage. RGB channels include-0.125,0.25,2.0. A later corrupt NPY version is deliberately uploaded at the same key while SQL pins the first version. Both recovered child inventories contain the actual NpyArray reference/SHA (two registered rows).

After SQL/coordinator/live-object recovery, Python invokes actual TaskArtifacts materialization and load_render_source_pixels using a controlled lease/gateway adapter that streams the SQL-pinned MinIO version. Actual worker byte verification, NPY dtype/shape validation, memory mapping and scratch accounting run on the real payload. Exact pixel comparison preserves negative/highlight values without clipping; absent alpha remains absent and does not require a new reference. The map and MinIO stream close, and all scratch capacity is reusable afterwards. The first run exposed the fixture's missing private scratch root directory; the fixture now explicitly creates it mode0700 without changing worker path protections.

Three integration variants passed3 in26.92s on430 migrations. Existing render source unit checks passed3 in0.48s. The isolated version inventory now includes five original source objects, two intentional replacements and four transport documents; recovery replay creates no additional versions.

This proves the worker source-loading path on real declared NPY bytes under controlled gateway/lease admission. Producer metadata is still synthetic; no canonical asset.decode worker result, HTTP/mTLS gateway admission, activated render task or GPU pipeline execution is established. Full service active recovery/qualification/public submission and complete V20–V32 implementation remain unproven.

### V26: active production recovery loop over a fresh live batch

Added TestIntegrationStageRenderRuntimeBatch. After the prior direct recovery/cancellation test, it reads immutable frozen child configurations from the isolated database, validates them, preserves backend/runtime/tile/alpha choices, resolves current source inventories through the actual SQL/MinIO factory, compiles a fresh two-frame batch and commits it through SQLRenderDraftStore.BeginBatch. It does not publish inputs. Only tenant/batch/task IDs are checkpointed for the subsequent separate workflow process.

TestIntegrationRenderRecoveryRuntimeLoop now checks those new tasks have zero registered input rows, constructs NewRenderRecoveryProcessor with real API/scheduler pools and actual objectstore client, and runs the actual RunExpansionLoop until one batch is materialized. The loop must discover/publish/register it autonomously. It then cancels, joins and verifies readiness=false, zero failures and exactly one materialization. Independent SQL checks find both task input closures ready and both tasks still blocked at attempt0. This proves active recovery without bypassing renderer qualification/activation.

The Python wrapper independently reads/hash-checks all eight ready transport documents across the direct and runtime batches, confirms the declared NPY is registered for all four tasks, and verifies the isolated MinIO version inventory has no unexpected replacement versions. Existing actual worker NPY materialization and old-version JSON/NPY checks remain included. Three integration variants passed3 in21.96s on430 migrations. Full workflow/v2render suites passed (workflow0.854s, v2render21.635s); documentation passed2150 static/54 semantic checks.

This tests the exact production processor/loop constructor with active work, but not the complete RunScheduler process, all its parallel queues, or HTTP readiness endpoints. Source producer lineage is still fixture evidence, not canonical decoding. Durable retry fairness, public submission, full-service load verification, activated GPU rendering/model qualification and complete project implementation remain outstanding.

### V26: persistent bounded retry backoff and private scheduling fields

Migration00431 adds runs.v26_input_recovery_after and nonnegative v26_input_recovery_failures. Scheduler-only record_v26_render_recovery locks the draft bulk parent, rechecks cancellation/schema, and schedules incomplete failed inputs after5,10,20,40,80,160,300 seconds (capped thereafter). Counter increment saturates safely. Success resets both fields; stale failure arriving after all inputs are ready also clears rather than delaying complete work. Success recorded while inputs remain incomplete is rejected. Candidate discovery filters future due times. No new tenant table/artifact/DELETE grant is introduced.

A trigger protects explicit scheduling changes on INSERT/UPDATE from non-photo_fn_owner callers, because existing API grants cover runs columns. API can still create ordinary runs with default0/NULL fields. ProcessOne records success/failure outcomes after recovery, preserves errors and skips scheduling writes on caller cancellation or advisory-lock contention. Startup now requires the recording function. In-memory UUID rotation remains separate from persistent failure timing.

The active runtime integration expires a recipe, observes failed ProcessOne, checks one failure/future due time, closes/recreates the API runtime pool, and confirms the new instance cannot immediately retry. It restores recipe authority and, using a DBA fixture SET LOCAL ROLE photo_fn_owner, advances only the due timestamp with guards enabled. The active loop then succeeds and resets failure count/due time. API direct scheduling UPDATE and function execution both raise InsufficientPrivilege. Final sequential three-variant integration passed3 in24.99s on431 migrations.

Parallel verification was resource constrained: one prior10-second runtime test timed out and its MinIO cleanup initially preserved the directory. Other verification completed (three variants170.31s; Go v2render90.319s/workflow1.372s). After confirming its container absent, only that fixture directory was removed. The expanded runtime test now allows30 seconds; the final sequential run passed without cleanup errors. No benchmark/production throughput claim follows from these timings.

Durable recovery backoff is implemented. Complete RunScheduler HTTP/load verification, per-tenant scheduling quotas, public batch creation, canonical decode provenance, render activation/GPU/provider qualification and full V20–V32 completion remain outstanding.

### V26: backoff boundary/ACL matrix and late failure after sealed inputs

Added test_v26_render_recovery_backoff.py with17 real SQL checks on431 migrations. Controlled incomplete child/task metadata tests the5/10/20/40/80/160/300-second delays, saturation at2147483647 failures, unchanged manifest hash/user revision, due-candidate exclusion, rejection of false success while inputs are incomplete, foreign-tenant exclusion, and unchanged scheduling for terminal/cancelled parents. API insertion of either a nondefault counter or future due timestamp is rejected by V26_RECOVERY_SCHEDULE_PRIVATE; function EXECUTE is available only to photo_scheduler among API/worker/gateway/scheduler roles. No guards are disabled; explicit DBA fixture counter setup uses photo_fn_owner.

All17 checks passed in46.86s. These controlled scheduling rows do not represent a full qualified render plan or executed photographs; they verify operational SQL behavior only.

The full typed two-part recovery integration additionally records a deliberately late failure after actual SQL input sealing. Both failure count and due timestamp remain cleared, preventing stale reports from deferring already recovered work. Three controlled/live/crash variants passed3 in21.34s with the existing real source/NPY/transport/runtime-loop coverage. Full service HTTP/load behavior, public submission, canonical decode provenance and GPU/provider qualification remain unproven; complete project scope is not achieved.

### V26: isolate recorded scoped input rejection from scheduler readiness

SQLRenderBatchRecovery now distinguishes scoped coordinator authority/validation failures from factory/transport/infrastructure failures. Invalid/expired input evidence and scoped401/403/404 authorization errors are wrapped while preserving their original errors.Is chain. After ProcessOne successfully records persistent backoff, a direct scoped input rejection returns an idle healthy result instead of making the entire scheduler unready. Direct Recover continues to return the rejection to its caller. Unrecorded rejection and scheduling errors remain visible.

A joined lock-cleanup failure must never be hidden merely because it also contains a scoped rejection. renderRecoveryOutcome suppresses only the direct typed rejection after a confirmed schedule write, preserving joined cleanup/scheduling errors, cancellation, factory-invalid configuration and transport errors. It also preserves already committed done=true progress when schedule recording fails. Seven table-driven cases plus the committed-progress assertion cover those boundaries.

The actual runtime integration's expired-recipe pass now returns false,nil while persisting its failure counter/due timestamp; a new runtime respects that backoff, and the active loop later succeeds after fixture authority/due restoration. Three controlled/live/crash variants passed3 in23.15s on431 migrations. Full Go v2render/workflow passed (22.024s/0.860s) including the error-boundary cases. Documentation verification passed2150 static/54 semantic checks.

This does not classify missing/corrupt object bytes as harmless input failures or suppress database/storage outages. Permanent input rejection still requires a later terminal/review policy rather than infinite silent retries; full-service HTTP/load, public submission and GPU qualification/project completion remain outstanding.


### V26: recovery candidate indexes with accumulated terminal history

Forward-only migration00432 adds a partial runs_v26_recovery_pending index for draft uncancelled unsealed schema1 bulk parents and runs_v26_recovery_child for tenant/UUID-text child matching, including config_hash. No scheduling predicate, authorization rule, lifecycle transition or processing recipe changes. The UUID-text index supports the existing safe JSON ID comparison without introducing casts of untrusted JSON to UUID.

A real PostgreSQL planner test seeds one controlled incomplete batch and2000 completed bulk history rows, analyzes runs, and explains the exact stored candidate SQL under photo_fn_owner. It requires use of runs_v26_recovery_pending, then calls discovery as photo_scheduler and checks the exact tenant/batch result. This is synthetic planner evidence; no production throughput or GPU benchmark is claimed, and the test does not assert selection of the child expression index.

Planner and persistent-backoff/ACL boundary tests passed18 in54.12s on432 migrations. Full controlled/live/crash typed recovery variants passed3 in24.90s, retaining actual MinIO transport, pinned source/NPY materialization, interrupted commit replay and active processor-loop coverage. Documentation checks passed2150 static and54 semantic checks. Complete scheduler HTTP/load verification, public submission, canonical decode provenance, activation/provider/GPU qualification and full V20–V32 implementation remain outstanding.


### V26: shared exact planned-task eligibility for recovery and backoff

Forward-only migration00433 replaces recovery discovery and outcome recording with a shared private v26_render_recovery_pending predicate. It matches each declared child by tenant, ID text, kind/draft/cancellation/seal state, creator, frozen snapshot, manifest config hash, project and actor session. Within its guarded node array, a pending task must match the exact saved task ID, render.compose/1 operation, params artifact and input digest, remain blocked at attempt0 without cancellation, and have an incomplete input closure. Discovery keeps its active tenant/project/session and persistent due-time filters. Outcome recording retains its parent lock, bounds, private fields and scheduler-only grant. The helper has no EXECUTE grant to API/worker/gateway/scheduler roles.

Previously discovery could select an unrelated blocked render task under a matching child, while recording considered any incomplete render task regardless of lifecycle/config identity. Both now use the same planned-task predicate. A late failure when no recoverable planned task remains clears retry metadata rather than deferring the batch because of an unplanned task. This predicate filters recovery eligibility; it is not full manifest validation or authorization/activation, which remain the coordinator and SQL admission's responsibility.

New controlled PostgreSQL tests cover cancellation, queued/failed planned tasks, altered input digest, a different existing params artifact, cancelled/sealed/queued children, and an unplanned blocked render task left behind after the planned task fails. The initial attempted NULL params fixture correctly hit the existing NOT NULL guard; the final test uses a different real tenant artifact without bypassing that constraint. Private helper ACL checks are included. Combined new/planner/backoff matrix passed28 in65.46s on433 migrations; controlled/live/crash typed recovery variants passed3 in22.21s. Documentation verification passed2150 static/54 semantic checks.

The production loop still does not activate unqualified rendering or alter processing recipes. Public batch submission, full scheduler HTTP/load verification, canonical decode provenance, provider/GPU qualification and complete V20–V32 scope remain outstanding.


### V26: simultaneous recovery instances and cancellation during scoped object IO

Extended the actual typed restart integration with two SQLRenderBatchRecovery instances over the existing real scheduler/API pools. The first uses an injected source reader which blocks after actual scoped SQL catalog lookup and advisory-lock acquisition. While it is blocked, the second actual recovery instance must return false,nil without publication. PostgreSQL pg_locks joined to pg_stat_activity independently verifies exactly one granted session advisory lock for the exact batch namespace and an idle owner with NULL xact_start: object IO does not hold an open SQL transaction on that lock connection.

Cancelling the owner must return an error matching context.Canceled with done=false, join its goroutine within a bounded cleanup interval and leave zero transport PUTs. The existing following upload-interruption/recovery path still proceeds; its independent authority-pool lock probe must successfully acquire/release the same namespace, detecting a leaked cancellation lock even if a subsequent owner had reused the same pooled connection. Full replay, real MinIO source/transport checks, pinned NPY materialization and active recovery loop remain included.

Three controlled/live/crash variants passed3 in21.67s on433 migrations. The first cancellation-only run also passed3 in21.75s before adding the independent PostgreSQL transaction-state assertion. The gated source read is an injected cancellation boundary, not an actual storage network stall; the competing instances share service pools rather than running separate scheduler OS processes. This verifies actual session-lock exclusion and cleanup under that boundary, not full RunScheduler HTTP/load behavior. No processing recipe, SQL lifecycle grant or renderer qualification changes were made. Public submission, canonical decoding/GPU execution and complete V20–V32 scope remain outstanding.


### V26: server selection preparation from scoped pinned documents

Added RenderSelectionItem, RenderExecutionChoice and RenderRequestLoader.Load. Runtime/backend/tile/alpha/provider syntax is validated before IO, without claiming runtime qualification. Loading uses a project/version-scoped catalog, exact immutable versions,1MiB bounded canonical JSON, exact length/SHA and repeated current catalog authorization after each read. Storage/read/close/cancellation errors propagate. Source binding/canvas, authored layer/protected-mask metadata and execution resource profile are validated. Recipe child occurrences retain their actual parent/path; roots do not grant descendant access.

SQLRenderBatchFactory.PrepareSelection owns ordered selection values, creates separate role-checked API/scheduler dependencies, loads a fresh version-scoped request per item, and invokes the existing full closure/atomic batch coordinator. It retains returned plans on commit/publication uncertainty and never activates rendering. Policy/identity/execution syntax is checked before factory/load work. Snapshot selection coverage remains authoritative SQL admission's responsibility. Existing Apply is unchanged, and this server entry point is not yet exposed through a public HTTP route.

Controlled full-recipe tests exercise base/protected document loading, unchanged recipe/source/protection bytes, an empty authored LayerStack with/without protection, post-IO scope revocation, invalid runtime SHA before catalog access and changed pinned object bytes. These controlled catalogs do not prove real SQL authorization for authored LayerStack/protection children. Full Go v2render/workflow suites passed (50.425s/1.833s).

The live runtime staging fixture now loads original recipe/source bodies through actual API SQLArtifactCatalog and MinIO pinned versions, comparing them to saved immutable evidence. Its fresh batch is then prepared by actual PrepareSelection using only refs/bindings and frozen execution choices. A controlled transport adapter returns io.ErrUnexpectedEOF before the first actual PUT, after real atomic staging and document reservation. Returned part IDs/config SHA must match independently compiled expected plans. The separate actual workflow processor/loop resumes publication and registration; final independent eight-document/SHA/version/NPY checks remain unchanged. Three controlled/live/crash variants passed3 in31.80s on433 migrations. The preceding loader-only live variant passed3 in29.04s. Documentation passed2150 static/54 semantic checks.

This demonstrates real selection assembly/staging and interrupted publication recovery, not public HTTP submission, qualified registry selection, canonical decode producer lineage or activated GPU rendering. Complete scheduler HTTP/load behavior, authored/generated pipeline SQL lineage and the complete V20–V32 scope remain outstanding.


### V26: reject invalid late selection before object IO

PrepareSelection now validates every owned selection item before constructing dependencies or loading documents: nonzero version, exact lowercase SHA refs/canvas hash, distinct recipe/source artifact IDs and no duplicate selected asset versions. RenderRequestLoader repeats the item validation for individual callers. Distinct versions may share an immutable source artifact; the existing live two-version fixture retains that supported case.

The actual SQL/MinIO staging integration supplies a malformed recipe SHA at the final selection ordinal and, separately, a duplicate final version. A counting source reader requires zero opens and no returned plans for both failures. The unchanged valid selection then still stages through PrepareSelection and resumes via the independent workflow loop, retaining pinned versions, expected config hashes and final inventory checks. Three controlled/live/crash variants passed3 in41.39s on433 migrations. This checks preflight resource behavior and preserves authoritative snapshot/authorization admission; it does not establish public submission, renderer qualification or full project completion.


### V26: selected trusted runtime qualification resolution

Forward-only migration00434 adds API-only resolve_v26_render_qualification. It selects the existing single DBA-enabled admission policy for render.compose/1 and requires an enabled operation/backend, active tenant/unarchived project, matching tenant context, and exact trusted passed report pins across model/runtime/backend/device/dataset. Report metadata must remain ready/verified/unexpired JSON with a real immutable object version. No qualification row, operation enablement or report is created by this production migration.

Added SQLRenderQualificationResolver.Resolve with real API-role/session/tenant/editor/project authorization, exact pin validation and RENDER_UNAVAILABLE/503 when no selected evidence exists. It does not derive provider peaks or activate/prequalify rendering. The actual live runtime fixture invokes it before preparing blocked drafts and confirms the existing disabled/unqualified operation remains unavailable.

A comprehensive PostgreSQL test imports a controlled report through the existing maintenance-only trusted-import function. The initial fixture correctly failed QUALIFICATION_FORBIDDEN until its owner membership was supplied; no guard was bypassed. Default render.compose disablement blocks resolution. Only within the rollback fixture, DBA operation enablement allows the exact five pins. Changes to model/runtime/backend/device/dataset/admission/license, expired report, archived project, deleting tenant and unsupported backend all deny resolution; foreign tenant/project and denied-role ACLs are checked. The one SQL matrix test passed in4.60s on434 migrations. The report carries synthetic metadata and is not GPU/model quality evidence.

Controlled/live/crash typed recovery variants passed3 in35.48s, including the actual Go missing-qualification path, scoped source preparation and independent active recovery. Documentation passed2150 static/54 semantic checks. The earlier targeted Go command compiled the package but matched no tests; it is not counted as runtime qualification evidence. Public HTTP submission, provider peak selection/calibration, qualified GPU activation and complete V20–V32 scope remain outstanding.


### V26: qualification-bound preparation entry point

Added PrepareQualifiedSelection over the shared private selection assembly path. It retains all early selection/execution checks, resolves current API/editor/project-qualified policy before object IO, matches backend/runtime, checks each loaded frozen recipe's model lock and re-resolves the complete qualification pins after IO before coordinator staging. Unchanged PrepareSelection still permits preparation of blocked drafts without claiming qualified execution. No graph is activated and provider peak calibration is not inferred from qualification metadata.

The real SQL/MinIO runtime fixture now calls PrepareQualifiedSelection with an otherwise valid selection/choice while render qualification is absent/disabled. It requires RENDER_UNAVAILABLE, zero returned plans and zero object-reader opens. The existing subsequent unqualified draft preparation, interrupted publication and independent recovery still succeed. Three controlled/live/crash variants passed3 in25.73s on434 migrations; documentation checks passed2150 static/54 semantic.

This verifies the actual missing-qualification path before IO. A positive qualified Go preparation, mismatched model/runtime and post-IO qualification-revocation branches are not yet independently integration-tested. The second resolution is a preflight, not an atomic qualification guarantee at graph activation; the latter, calibrated provider/device assembly, public submission, actual GPU quality evidence and complete V20–V32 implementation remain required outstanding work.


### V26: positive qualified preparation and runtime/revocation boundaries

Added an isolated Go qualification fixture which creates a controlled report and DBA-selected policy, imports it through the actual photo_maintenance trusted-import function, and enables render.compose only in that temporary database for the test. Report metadata is synthetic and its object is not uploaded; no GPU/model measurement or real production qualification is asserted. The fixture restores prior operation enablement and disables its admission policy before the independent workflow recovery process; cleanup uses a separate bounded context and runs before closing its authority pool.

The first full run reached positive preparation but failed teardown because defer admin.Close preceded t.Cleanup. Authority-pool close now uses t.Cleanup registered before policy cleanup. The corrected initial three-variant run passed3 in21.41s. This was fixture lifecycle repair, not a production permission/qualification change.

The actual PrepareQualifiedSelection now runs positively with the trusted controlled policy, real API/editor/project resolution and SQL/MinIO document loading. It stages via the real coordinator and stops at the deliberately injected first transport PUT boundary; returned run IDs/config hashes must match independently compiled expected plans. A wrong but syntactically valid runtime SHA is rejected before any object-reader open or returned plan.

A second actual preparation wraps the real source reader to disable the exact selected admission policy during its first Open. Document reads still use pinned MinIO versions, but the final qualification re-resolution must return RENDER_UNAVAILABLE with no plans. Independent authority SQL confirms the requested parent batch does not exist. The fixture re-enables that policy for the positive test, then restores operation/policy state before active recovery. Existing eight ready transport documents, exact version inventory, NPY loading, cancellation/lock checks and recovery remain covered. Final three controlled/live/crash variants passed3 in40.70s on434 migrations.

Positive Go preparation, wrong runtime before IO and admission revocation during IO are now integration-tested under controlled qualification metadata. Mismatched recipe-model integration, atomic qualification revalidation at graph activation, calibrated provider/device selection, real GPU reports, public submission and complete V20–V32 implementation remain outstanding.


### V26: recipe model mismatch under a matching qualified runtime

The actual qualified selection integration first imports a controlled trusted report/policy for a different model SHA but the same server runtime/backend. PrepareQualifiedSelection must perform real pinned document reads, return RENDER_UNAVAILABLE with no plans, and leave the requested parent absent according to independent authority SQL. This exercises the recipe-model comparison after successful initial qualification resolution rather than only a missing-report or runtime mismatch. The fixture disables that admission policy, selects/imports a separate exact-model policy and continues through the existing positive preparation, runtime rejection, during-IO revocation and independent recovery scenarios.

The isolated operator seed now uses ON CONFLICT DO NOTHING so the second report uses the already authorized operator without rewriting its enabled state. Both selected policy states and prior operation enablement are restored through existing bounded cleanup. Controlled metadata reports remain separate from actual GPU quality/measurement evidence; no production qualification or privilege is changed.

Three controlled/live/crash variants passed3 in78.24s on434 migrations, preserving actual SQL/MinIO source/version/transport checks and worker NPY loading. Preparation's missing qualification, wrong runtime, wrong recipe model, positive matching pins and policy revocation during IO now have integration evidence. Atomic qualification at graph activation, calibrated device/provider dispatch, public submission, real GPU execution and full V20–V32 completion remain outstanding.


### V26: locked live qualification at the SQL activation boundary

Forward-only migration00435 adds private v26_render_qualification_ready and invokes it in seal_v2_graph's existing render branch after structural/input checks. The helper requires a draft unsealed uncancelled run and workflow project, takes shared locks on the live project/operation/selected policy and one exact ready verified unexpired trusted report plus qualification record, resolves the current policy, and compares all frozen nodes' model/runtime/backend pins. It retains existing lens/consent/core activation logic. PostgreSQL lock privileges use narrow identity-column UPDATE grants to photo_fn_owner only; no service-role grants or writes to those identity columns are added.

An initial actual gate call exposed an ambiguous PL/pgSQL project_id variable; it now uses v_project_id. The live typed staging fixture proves the helper accepts the actual persisted matching frozen plan and rejects it after operation/policy restoration to disabled. The typed recovery fixture directly calls scheduler-only seal_v2_graph on structurally ready registered children without qualification and requires false. New helper EXECUTE remains denied to API/scheduler/worker/gateway roles.

Two actual authority transactions additionally hold the qualification gate open and attempt to disable the selected admission policy concurrently with100ms lock_timeout. The peer receives55P03, rolls back, and the owner rolls back cleanly. This directly proves policy revocation serialization; report/provenance shared locks are executed by the positive gate but their separate concurrent mutation cases are not independently exercised.

Final qualification matrix plus controlled/live/crash recovery variants passed4 in26.36s on435 migrations (the earlier lock-specific run passed3 in28.86s). Existing lens document/original seal regressions passed5 in12.74s. Documentation passed2150 static/54 semantic checks. All trusted report metadata in this fixture remains synthetic; no report object upload, GPU benchmark or production operation enablement is asserted.

This closes live qualification revalidation in the SQL activation path and verifies negative public seal behavior, but a complete positive graph activation/executed GPU render remains unproven. Calibrated provider/device dispatch, public batch HTTP submission, authored/generated lineage and complete V20–V32 implementation remain outstanding.


### V26: concurrent report and qualification-record mutation exclusion

Extended verifyRenderQualificationLock to hold the actual successful qualification gate open in one authority transaction while separate peer transactions attempt policy admission revocation, expiry of its exact tenant-scoped report artifact, and deletion of the matching tenant-scoped model_qualifications record. Each peer uses100ms lock_timeout, must receive55P03 and is rolled back before the next case; the owner then rolls back. No expiry/deletion commits. This directly verifies the report and qualification-record shared locks added in435, beyond the prior policy-only concurrency evidence.

The matching predicates retain tenant/operation/version/model/runtime scope. Existing positive/negative qualification selection, pinned SQL/MinIO preparation, interrupted transport, independent recovery and worker NPY checks remain included. Three controlled/live/crash variants passed3 in25.76s on435 migrations. These are controlled qualification metadata and transaction tests, not GPU execution. Positive complete graph activation, device/provider calibration/dispatch, public submission and complete V20–V32 scope remain outstanding.


### V26: immediate qualified preparation replay after interrupted publication

The live staging fixture now repeats actual PrepareQualifiedSelection with the same batch UUID, ordered selection, policy and execution choice after the deliberate first-PUT interruption. Its second publication attempt is deliberately interrupted at the same boundary. Both returned part identities/config hashes must match; independent authority SQL requires unchanged tenant run/task/backend-document counts and reserved_bytes. This verifies replay of atomic batch staging plus pending document reservation without duplicate shells or quota charges.

The existing independent workflow process then resumes the pending publication after qualification fixture restoration, verifies actual ready input closures, and completes its recovery loop. The final independent MinIO document/version inventory and pinned source/NPY materialization remain included and detect unexpected replacement versions. Three controlled/live/crash variants passed3 in23.78s on435 migrations.

This proves immediate replay under a repeated controlled transport failure followed by independent recovery; it does not establish a successful public HTTP idempotency response, executed renderer output or GPU qualification. Public submission, calibrated dispatch/positive execution and complete V20–V32 implementation remain outstanding.


### V26: conflicting ordered-selection replay cannot replace a batch

The actual qualified selection integration reuses the existing batch UUID with the first/last selected frames swapped. Full loading/compilation must produce a different config SHA, but atomic store replay must return ErrExpired before transport publication. The test requires no additional PUT attempts, unchanged tenant run/task/document counts and reserved_bytes, and independently reads every persisted child config_hash to require the original prepared SHA. Returned conflicting compiled plans are retained as evidence without asserting persistence, following existing uncertain-store semantics.

The original ordered batch still proceeds through matching qualification-gate checks and the separate recovery process, retaining exact pinned object/version and NPY checks. Three controlled/live/crash variants passed3 in21.51s on435 migrations. This verifies conflicting replay rejection at the real SQL boundary; mapping it to a public HTTP idempotency conflict and complete V20–V32 implementation remain outstanding.


### V26: distinguish changed replay from unavailable/expired evidence

SQLRenderDraftStore.BeginBatch now calculates the exact canonical requested manifest SHA before transaction work. If parent admission returns false, it queries the existing narrow scoped manifest function within that same scheduler transaction. Only an authorized noncancelled manifest with a differing SHA yields ErrConflict. Missing/denied/cancelled evidence or a matching SHA stays ErrExpired; database/read errors propagate rather than being reclassified as expiration. No new SQL privilege, direct run-table read or object IO is introduced.

The actual changed-order qualified replay integration now requires ErrConflict, retaining all no-publication/count/quota/persisted-child SHA assertions. The expired-root/late-part atomic rollback, unknown-commit replay and independent recovery tests remain included. Three controlled/live/crash SQL/MinIO variants passed3 in25.81s on435 migrations. Full Go v2render/workflow passed22.148s/0.646s.

This supersedes the earlier changed-order fixture's ErrExpired classification and makes409 idempotency conflict distinguishable from410 expired input for a future public route. Public HTTP submission/response mapping itself is not mounted, and complete V20–V32 implementation remains outstanding.


### V26: positive complete SQL graph activation under controlled qualification

The typed restart fixture now imports/selects controlled trusted qualification metadata after all real child input closures are registered and sealed. It calls scheduler-only seal_v2_graph for an actual persisted child inside an explicit transaction and requires true. Fixture-only trusted-role reads of that same uncommitted transaction require run.status=queued, graph_sealed=true and all declared root tasks state=ready at attempt0. The transaction is rolled back before qualification restoration and existing cancellation/recovery checks, so no runnable tasks or activation outbox events from this synthetic qualification become committed dispatch work.

The initial assertions correctly hit the scheduler's existing direct-table permission denial; reads now use SET LOCAL ROLE photo_fn_owner only inside the fixture transaction, with no production grant changes. A second assertion incorrectly expected task.state=queued; inspecting activate_bound_task established the existing canonical ready state before dispatch. The final test checks that state rather than changing production semantics. Fixture policy creation now reuses the exact unique policy for repeated trusted imports, allowing the later runtime preparation test to select the same controlled pins.

Three controlled/live/crash variants passed3 in58.72s on435 migrations, including real MinIO pinned inputs/transport and worker NPY materialization. This establishes positive full SQL activation/state transition for declared roots under synthetic qualification metadata. It does not establish actual dispatch/lease execution, calibrated GPU providers, real measured report objects, sealed-graph replay semantics, public HTTP submission or complete V20–V32 implementation.


### V26: active graph seal replay without duplicate activation effects

Forward-only migration00436 fixes the render wrapper's unconditional draft-only gate: a previously sealed active graph now reaches the existing idempotent core. First activation still checks structural/input authority and locked live qualification. In the controlled positive activation transaction, the actual scheduler calls seal_v2_graph twice; both return true. Independent fixture-role reads require an unchanged run revision and exactly one v2.run.queued event and one queued outbox record before and after replay. The transaction rolls back, so synthetic qualification never commits runnable tasks or dispatch events. Fixture reads use the safely quoted database-owning login obtained from current_user, rather than assuming RESET ROLE escapes the pool's startup role. No production read grants are added.

The three typed Render controlled/live/crash variants plus five Lens document/original regressions passed8 in68.04s on436 migrations. This verifies active replay and preservation of the Lens seal behavior under actual PostgreSQL migrations, with existing pinned MinIO transport/input checks. Controlled qualification metadata is not a real GPU measurement/report object. Public HTTP submission, calibrated provider/device dispatch, actual GPU execution, authored/generated lineage and the complete V20–V32 scope remain outstanding.


### V26/V32: bounded calibrated execution selection and pinned server catalog

Added RenderCalibration.Select with exact model/runtime/backend/device/dataset/ICC matching, both-axis covering samples, deterministic smallest-area selection, retained RAM/VRAM bounds and exact integer ceil20% pixel-ratio scratch/output scaling. Invalid/duplicate/unavailable dimension samples and unsafe scaled counters reject. Added PinnedRenderCalibrationCatalog: canonical≤1MiB strict JSON, independent deployment SHA, bounded profiles and samples, unique qualification tuple and owned immutable data. Unit cases cover opposite aspect ratios with equal area, registry reorder stability, out-of-envelope dimensions, pin mismatch, negative/unsafe costs, scaled overflow, unknown/noncanonical/unpinned manifests, duplicate tuples, cancellation and attempted caller mutation of returned samples.

PrepareCalibratedSelection resolves actual live SQL qualification and operator profile before IO, loads actual scoped pinned source/recipe documents, selects per-source measured overhead/ICC, rechecks qualification and full canonical profile SHA after IO, then uses existing atomic parent/children preparation and publication. Actual SQL/MinIO integration rejects a foreign-device calibration before any object open and a changed evidence SHA after real reads before any parent is staged. Matching preparation uses the actual immutable pinned catalog, followed by interrupted PUT, exact replay with stable counts/quota/config SHA, changed-order conflict rejection and independent workflow recovery/pinned NPY checks. The fixture's dimension samples and zero-overhead costs are deliberately synthetic and are not GPU benchmark evidence.

Full Go v2render/workflow passed21.367s/0.941s. The final controlled/live/crash SQL/MinIO variants with the pinned catalog passed3 in26.55s on436 migrations (the earlier mutable-fixture catalog run passed3 in25.09s). No production operation enablement, GPU workload execution or real calibration is asserted. This adds the server resource-selection/preparation path; mounting public submission, deploying operator-qualified measured manifests, worker device compatibility/dispatch, authored/generated lineage and complete V20–V32 remain outstanding.


### V26: operator calibration file loading at API startup

Config.Load now validates optional PHOTO_RENDER_CALIBRATION_FILE/PHOTO_RENDER_CALIBRATION_SHA256 as a pair, with absolute path and lowercase SHA. API startup loads a configured catalog before database/auth dependency setup and retains it in httpapi.Server.RenderCalibrations; absent configuration preserves startup and leaves the catalog nil. Configured failure returns a generic error without file content/path/credentials. LoadPinnedRenderCalibrationCatalog uses nonblocking open, rejects nonregular files, bounds bytes≤1MiB, checks length and descriptor metadata before canonical/SHA/manifest validation. Tests cover valid pinned regular file, changed bytes, oversize, relative/missing/directory paths and an actual FIFO without waiting for a writer. Existing immutable-catalog tests remain included.

Added optional compose.render-calibration.yaml with required host-file/digest variables, read-only bind and create_host_path=false; base topology does not gain an implicit manifest. Deployment instructions explain authenticated measured profiles, pinning and restart semantics. Actual docker compose merged config --quiet passed with isolated placeholder configuration, without deployment or external connections. Existing deployment topology suite passed6 in0.13s. Go config/v2render/httpapi passed0.429s/22.778s/0.843s; cmd/photo compiled (no package tests). Documentation passed2150 static/54 semantic checks.

This proves loader/config behavior, package compilation and merged deployment configuration. It does not prove a full live API startup against external dependencies, public Render HTTP submission, measured RTX3060 profiles, worker device compatibility/dispatch or complete V20–V32 implementation. These remain required.


### V26: no implicit zero measurements in pinned calibration manifests

Review before public HTTP wiring found that encoding/json could decode omitted/null resource counters as zero and accept case-insensitive field aliases even with DisallowUnknownFields. The catalog now validates exact case-sensitive object shapes at manifest/profile/qualification/sample/provider levels and requires every declared field explicitly present and non-null. A provider_peak must include all four counters; measured zero remains valid only when explicitly recorded. Pin/canonical/typed-bound checks and sample selection remain intact. This prevents incomplete operator records from silently supplying zero provider overhead.

Ten mutation cases cover absent/null peak, each missing counter, null VRAM, renamed uppercase field, duplicate case alias and profile tile-size alias; explicit zero remains accepted. All calibration-focused Go tests passed0.543s. Actual controlled/live/crash SQL/MinIO preparation/replay/recovery variants using the pinned catalog passed3 in25.65s on436 migrations. No GPU measurement or full public Render HTTP admission is asserted. Public submission, dispatch/device matching, empirical calibration and complete V20–V32 remain outstanding.
