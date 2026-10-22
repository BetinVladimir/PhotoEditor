# Culling: photographer selection client

Status: implemented client source; unqualified. Server/worker culling barrier belongs to the parallel Apply module. No tests, builds, migrations, generators or runtime qualification have been executed in this pass.

## Product behavior

The photographer sees every frozen input photograph, grouped by scene/duplicate/burst labels with source-version thumbnails, ranking and AI recommendation. Recommendations preselect the initial result. The photographer may select alternatives or deselect recommendations. Every selected photograph with an eye warning needs an explicit confirmation. At least one photograph must be selected. Originals are never deleted, and the input snapshot is never modified by the client.

The existing `/reviews/[id]` screen embeds `CullingReviewSelection` for `kind=culling`. Generic image accept/reject/skip buttons and raw run-artifact links are excluded for this kind. Foreground/account transitions reuse the existing keyed Review scope; the embedded component aborts reads and mutation reconciliation on unmount. Source thumbnails reuse the existing exact asset/version, URL and expiry guards of `SceneFramePreview`.

## Agreed wire contract

`GET /reviews/{id}/culling-items?after=<opaque cursor>` returns pages of at most 100 items, at most 5000 total. Root fields are exactly:

`review_id, run_id, revision, state, candidate_hash, snapshot_id, snapshot_digest, never_delete, total, items, next_cursor, groups, proposal, selected_item_ids, confirmed_eyes_item_ids`.

`groups` and `proposal` are immutable artifact refs; proposal SHA equals candidate hash. Pending selection arrays are null. Decided arrays are sorted unique UUIDs. Items contain exactly:

`item_id, asset_id, asset_version_id, scene_id, exact_group_id, burst_group_id, rank, score, recommended, manual, eye_review, eye_state`.

Eye state is `open|closed|unknown`. Root pins and receipt state remain identical across every page. The loader rejects duplicate IDs, looping or invalid cursors, incomplete totals, mismatched source/review pins, and changed root selection. No partial inventory is exposed for submission.

`POST /reviews/{id}/culling-decision` with the stored Idempotency-Key sends exactly `{expected_revision,candidate_hash,selected_item_ids,confirmed_eyes_item_ids,never_delete:true}`. Selected IDs are nonempty, unique and drawn from the full frozen inventory. Confirmed IDs are a subset of selected IDs; all selected `eye_review` targets require confirmation. The client never sends a replacement snapshot or collection item updates.

The actor-bound decision hash is SHA256 of canonical JSON:

`{version:'culling_decision_v1',tenant_id,review_id,run_id,actor_id,candidate_hash,snapshot_id,snapshot_digest,selected_item_ids:<sorted>,confirmed_eyes_item_ids:<sorted>,never_delete:true}`.

Receipt is the standard Review View with unchanged ID/run/kind/candidate hash, state `decided`, revision expected+1, decision `accept`, route `selection`, and matching decision hash.

## Functions and lifecycle

- `readCullingPage`: strict root/item/ref/number/state validation and exact requested review binding.
- `loadCullingSelection`: bounded complete pagination; no partial result; abort checks after every response.
- `cullingDecisionIntent`: freezes source, actor, complete inventory, sorted choices, confirmations and decision hash.
- `readCullingDecisionIntent`: revalidates persisted inventory and reconstructs the exact canonical intent.
- `cullingDecisionReceipt`: distinguishes unchanged pending review from an exact accepted receipt and a conflicting actor/selection.
- `recoverCullingDecision`: GET receipt first; if pending, POST the same body/key; verify by GET. Lost successful responses reconcile without a second POST. Authorization loss stops recovery and retains the journal.
- `CullingDecisionAttemptStore`: immutable persisted request, scoped by origin/account/tenant/review; semantic cross-window lock and durable logout generation fence; clear only after matching receipt. No signed media URLs or credentials are stored. Inventory journal is bounded to 6 MiB.
- `CullingReviewSelection`: synchronous submit lock, actor rechecks before/after API boundaries, read/mutation cancellation, pending selection lock, explicit retry, virtual thumbnail cells and access-loss teardown. A failed/uncertain operation never silently creates a new key or substitutes current selection.

## Authored qualification scenarios

Unit coverage includes complete multi-page inventory, duplicate/loop/incomplete/rebound pages, eye confirmation and nonempty selection, exact receipt replay without POST, another actor's hash conflict, uncertain dispatch, denied preflight/mutation and `never_delete` enforcement. Journal coverage includes remount, immutable conflict, receipt-only clearing, account scope, logout and cross-window generation changes. DOM scenarios cover explicit eye choice, immutable retry across remount, partial inventory refusal, ACL teardown, denied receipt, late inventory and late accepted mutation after unmount. All remain unexecuted.

## Backend and final selection CAS (implemented, unqualified)

`backend/internal/review/culling.go` implements all-target frozen pagination, active project-editor authorization before idempotent replay, strict source/projection pins, actor-bound canonical decision hashes and immutable receipt recovery. `backend/internal/httpapi/culling_review.go` exposes the dedicated private, non-cacheable GET/POST routes. Generic image proposal decoding is bypassed only for the dedicated culling projection.

Forward-only migration `02450_apply_culling_decision.sql` locks Apply build → run → review, then stabilizes actor/session/project/source and proposal artifact authority. The final CAS requires graph 4, `cull_review`, an active uncancelled Apply run, exact frozen snapshot/digest, pending revision and proposal hash. Selected IDs must belong to the frozen inventory; every selected eye-review item requires explicit confirmation. The function stores sorted immutable selection, actor-bound hash and audit receipt. Snapshot membership and originals remain unchanged. Only the scheduler function from migration 02449 resumes processing under its lease.

Authored backend unit scenarios cover canonical actor binding, empty/foreign/duplicate choices, missing eye confirmations, stale revisions, invalid projection fields and source pins. HTTP scenarios require authenticated sessions. SQL concurrency, ACL loss during lock waits, replay after acceptance, scheduler adoption and browser/native DOM scenarios remain unexecuted qualification gates. No tests, builds, generators or migrations were executed for this change.
