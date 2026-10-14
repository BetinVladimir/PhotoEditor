# V29 multi-image producer configuration

Implementation authored, not runtime-verified. Tests/build/typecheck have not run in this implementation phase. Migrations 01139–01158 have not been applied by this work. Both operation registry entries remain disabled. Configuration loading does not qualify models, enable registry entries or install worker/adopter runtimes.

## Producer configuration

| Operation | Configuration path | Required SHA-256 | Scheduler DSN |
|---|---|---|---|
| `optical.panorama` | `PHOTO_V29_PANORAMA_CONFIG` | `PHOTO_V29_PANORAMA_CONFIG_SHA256` | `V29_PANORAMA_SCHEDULER_DATABASE_URL` |
| `optical.focus_stack` | `PHOTO_V29_FOCUS_STACK_CONFIG` | `PHOTO_V29_FOCUS_STACK_CONFIG_SHA256` | `V29_FOCUS_STACK_SCHEDULER_DATABASE_URL` |

An absent configuration path leaves that producer dependency nil. Its HTTP route returns dependency unavailable after authentication/request validation. A configured invalid file, SHA, policy, scheduler role or installation prevents startup. Secrets are never included in errors. Bootstrap uses a 15-second context, a dedicated pool with `role=photo_scheduler`, exact SQL signatures/SECURITY DEFINER/function owner/EXECUTE checks, and denied scheduler access to private pre-claim admission/receipt functions.

Each file is a regular UTF-8 JSON file bounded to 8 MiB. It contains exactly `schema_version: 1` and `projects`. The list has 1–2048 unique `(tenant_id, project_id)` entries. Each entry contains exactly:

- `tenant_id`, `project_id`: nonzero UUIDs.
- `pin`: complete MultiQualificationPin identifying independently accepted original qualification evidence.
- `policy`: complete v2qualification.DeploymentLocks matching operation/version 1/CPU and every pinned model/runtime/device/dataset/issuer SHA.
- `max_pixels`: integer 1–200000000.
- `capacity`: `pixel_count`, `ram_bytes`, `vram_bytes`, `scratch_bytes`, `output_bytes`. Pixel count is the deployment ceiling; RAM/scratch/output are positive safe integers. VRAM must be zero for these CPU operations. The coordinator normalizes pixel count to the exact task requirement after checking the ceiling.

No example fabricated pins are provided. Generate a deployment file from actual trusted qualification imports and exact installed runtime evidence. Decoder rejects unknown fields, duplicate project scope, mismatched operation/policy/pins and unsupported budgets. Services own immutable copies of deployment policy lists. Requests cannot select deployment qualification or capacity.

## HTTP flow

Authenticated write requests use `POST /api/v1/tenants/{t}/projects/{id}/panorama-intents` or `focus-stack-intents`, with `Idempotency-Key` and a bounded JSON body containing exactly `schema_version: 1`, `snapshot_id`, `params`. Params follow the corresponding canonical operation contract. Source WorkingImage bytes are loaded from the accepted snapshot; client-provided originals are rejected.

The service durably claims the canonical intent/session/project/operation before object reads. Same key with a changed intent conflicts. It then resolves all originals, verifies original qualification and physical arrays, publishes Params and transport, registers inputs and admits the graph. Final admission requires the exact durable claim; activation, events/outbox, queued receipt and idempotency response commit atomically. Ambiguous errors attempt historical receipt recovery without reactivation. Receipt state `queued` is historical acknowledgement, not current task state.

## Remaining rollout evidence

Full SQL/object integration, claim/admission rollback and response recovery tests, startup checks against an actually migrated DB, independent photographic registration/focus qualification, client review/Apply workflow and deployment verification remain outstanding. Worker/adopter use their own operation-specific runtime installations. CPU panorama/focus does not replace the later Metal/MPS verification plan with a 10,000,000,000-byte VRAM cap for applicable GPU modules.

## Client authoring — authored, NOT RUN

Collection navigation opens `/collections/[id]/multi` with exact tenant and panorama/focus operation. User/project/operation-scoped MultiPreparationStore freezes ordered source bindings and separate snapshot/intent keys before any POST. Retry after an ambiguous response preserves original request semantics and repeats server authorization; acknowledged receipt is not current task status. Fresh authoring requires current capabilities and revision-consistent collection pages. Saved recovery can be retried even if fresh capabilities are unavailable; the server can still refuse current authority. Rows use thumb640 derivatives only. Result review/Apply remains incomplete and is not claimed by the navigation to the generic run screen.

## Adopted root reader — authored, NOT RUN

Migration 01159 introduces a separate photo_api root read boundary. Existing project editors are authorized independently of the original author's session; the immutable snapshot remains owned by its original author. All ordered original versions, their declared NPY children and the adopted attempt/fence/root must remain valid. SQLAdoptedMultiSource exposes exact root discovery and immutable object metadata; LoadAdoptedMultiResult checks canonical bytes and repeats current metadata after IO. This boundary is not yet wired to a public result review API. Typed output/source child catalogue, quality/preview and explicit Apply remain outstanding. Tests/build/typecheck NOT RUN and migration NOT APPLIED.

## Typed child boundary — authored, NOT RUN

Migration 01160 separates exact adopted output occurrences from original input edges. Output contract/path/role and literal first-visit adoption purpose must match. Original reads bind a frozen frame ordinal and its literal manifest role; a workflow link cannot substitute original source authority. Go JSON child loading checks exact bytes and repeated root/child locator authority. Ordered multi report and registration parsers are present, but the complete catalogue/geometry verification and review/Apply service are still outstanding. No additional producer startup ABI is required yet because these readers are not publicly wired. Tests/build/typecheck NOT RUN; migration NOT APPLIED.

## Original document set — authored, NOT RUN

Migration 01161 adds one bounded original-inventory authority query, avoiding a complete scope replay for each original physical child. LoadAdoptedMultiOriginals owns that inventory, reads exact source JSON outside SQL transactions, validates every ordered binding/canvas/common ICC and manifest edge, then rechecks all saved metadata and root/binding. This proves source documents and current metadata only; it does not prove NPY headers/content or qualify ICC installation. Complete catalogue/geometry/quality/preview/Apply remains outstanding. Tests/build/typecheck NOT RUN; migration NOT APPLIED.

### Multi review Describe HTTP reads (authored, NOT RUN)

GET `/api/v1/tenants/{t}/projects/{id}/panorama-proposals/{task}` and `/focus-stack-proposals/{task}` now use `MultiProposalService`. Authenticated current project access, exact succeeded/adopted root, originals, versioned documents and physical arrays must all pass before a review candidate is returned. Response preserves ordered sources, registrations and measured producer reason; no object locators, automatic Apply or PASS claim. Bootstrap uses photo_api dedicated SQL source plus versioned store. Queries need migrations through 01161; none applied in this phase. Preview, independent quality and explicit Apply/client review remain separate outstanding work.
