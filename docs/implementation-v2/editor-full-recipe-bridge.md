# Editor: immutable full recipe render bridge

Status: implementing; full enabled-module compiler is still open. The immutable Override bridge is implemented in source, unqualified. No test, build, migration or hardware execution was performed during this coding pass. A full recipe render uses the existing V2 quality, ICC, masks, policy and review flow. It is explicitly presented as a render which may require photographer review. An accepted base style and an existing V2 draft are required; no neutral style is substituted.

## Server contract

`PUT /api/v1/tenants/{tenant}/assets/{asset}/v2-draft` retains `expected_revision`, `recipe`, `layers`; optional `editor_bridge` contains `revision` (decimal persisted legacy draft revision), `params` (exact saved params), `document_id` (current V2 draft document UUID). The idempotency fingerprint includes the complete original request. Successful replay is looked up before checking the mutable legacy revision again, after active source/project authorization.

`checkEditorBridge` locks the actor's tenant/version-scoped Editor draft `FOR SHARE`, and compares revision and canonical params. `prepareEditorRecipeBridge` verifies the expected current V2 document/revision. `compileEditorBridgeOverride` validates the override and takes `input_sha256` from the verified source WorkingImage canvas, never the client digest of its envelope. `Publisher.Publish` reserves, pins, verifies and publishes immutable bytes through the existing V2 upload path. `ClosureResolver` resolves the published override using existing source/project scoped catalog authority.

`replaceEditorBridgeRef` replaces only the latest preceding bridge artifact actually present in this stream's recipe, preserving all unrelated semantic/manual overrides and their order. If the old bridge was explicitly removed from the authored recipe, a new bridge is appended. It never clears all overrides. Final `replace_v23_draft` CAS and legacy revision recheck occur in one transaction with the bridge metadata insert. A concurrent legacy save or V2 edit results in a conflict; rejected publication leaves temporary uploads to the existing janitor.

Migration `02445_editor_recipe_bridge.sql` records tenant/document/version/actor/editor revision/override artifact+SHA, forces tenant RLS, grants API SELECT+INSERT and an explicit function-owner SELECT+DELETE allowlist. Physical metadata purge extends the existing confirmed tenant deletion function after its preconditions. The migration is forward only.

## Client lifecycle

`prepareEditorRecipeBridge` captures the saved legacy revision/params and the exact source-bound V2 baseline/layers. `EditorRecipeBridgeStore` persists the body, mutation key and freeze key before sending the mutation. Storage scopes include origin, account, tenant and version; the existing semantic journal lock plus persistent logout generation prevents old clients repopulating cleared attempts.

`submitEditorRecipeBridge` replays the immutable request; validates stream/source/revision and canonical recipe digest; verifies preservation of non-override recipe fields, bounded bridge replacement, and layer digest. It does not adopt arbitrary current server state after an unreadable acknowledgement.

`fullRecipePreview` saves current legacy edits only when no immutable attempt exists. Recovery replays the original attempt without substituting newer local edits. The acknowledged document is frozen with the saved freeze key. Render policy and review barriers are obtained from the existing server flow. Before render admission the existing `EditorRenderCache` persists the render command. The bridge journal is cleared only after this durable handoff. Accepted run identifiers, polling, review-required presentation and signed result expiry reuse existing render mechanisms. An uncertain bridge request is retained; another layer/protection write is blocked while it is pending.

Legacy fixed-global preview remains available for simple strength/global edits without a loaded V2 context. Extended modules and loaded V2 context use full recipe rendering. The explicit full-current-recipe action is available independently of layer panel loading. It never sends extended modules to the global-only preview worker.

## Required qualification

Run the authored bridge Go and TypeScript scenarios, all current Editor/auth DOM and unit suites, database authorization/CAS/idempotency/purge scenarios, and a real image corpus across supported backends. Confirm ICC and masks are unchanged by the orchestration; confirm review-required results never expose accepted derivatives before review. No historical PASS constitutes evidence for this implementation.

## Open full-style compiler integration

The bridge currently publishes a complete Override; that alone does not generate the effect nodes requested by the effective style. `styleplan.Compile` is now the shared production ordered stage compiler, reused by `workflow.CompileApplyV4Plan`. It preserves analysis and human-review stages, geometry/mask-transform order, restoration and final quality. Canonical `extensions.culling` is recognized; portrait detail retains its existing explicitly approved-region contract without adding unrelated model dependencies. These changes are authored and unqualified.

Full Editor coding closure remains open: V2 batch contracts currently compile one `render.compose` node per frame, while geometry, crop, upscale and review-dependent effects require staged accepted intermediates. Required integration is a source/revision-bound single-source build reusing the existing Apply materializer and review flow, followed by final V2 composition without a repeated base style. It must preserve the authored layer stack and non-bridge overrides, exact ICC/model locks, transformed mask lineage, actor/session authority and independent replay. Merely expanding `supported_modules`, adding an unused graph field or rendering only global color does not close this gap.

`CompileEditorEffects` now compiles detached full module output from verified accepted base and ordered Override documents. It materializes frozen scalar corrections exactly once, preserves mask bindings/protection/landmarks and ordered photographer decisions, validates complete module contracts and emits the shared ordered stage plan. `style.resolve` supports exact current WorkingImage envelope SHA for legacy Apply and exact current canvas encoded-source SHA for V2 overrides; foreign source hashes still fail. Dedicated effect-build admission and final accepted-base composition remain open integration work.

### Authoritative execution order (2026-10-17)

The Editor pipeline executes the complete authored layer inventory against the
original frozen source pixels first (`editor.layers_precompose`). It then runs
the full effective Style through the existing ordered Apply stages, including
geometry and every human barrier. A sealed canonical result and executed-base
receipt authorize the final identity composition. Final composition does not
execute authored layers, Style modules, session corrections, learning or scalar
deltas again. The original frozen recipe remains immutable provenance.

Precomposition is an explicit intermediate operation, not a complete recipe
preview. It uses actual native typed layer providers and original-pixel Params
bindings, measures clipping and error outside changed support, and retains
review/generated evidence. Missing providers fail admission; no layer is
silently omitted. Geometry acts on the already composited result, so original
layer masks and layer pixels are not falsely relabelled as final-canvas data.

`editor_effect_builds` distinguishes original `source_*`, precomposed
`effect_source_*`/`effect_source_result_*`, final processed `processed_source_*`,
and sealed `result_*`/receipt references. Each transition must bind canonical
producer task/result/attempt/fence. Intermediate Apply completion is distinct
from final client completion. Status exposes build, seal and final run IDs.

Current status: compiler, immutable compiled Style publication, dedicated source
admission, status API, strict client commands and receipt readers are authored,
unverified. Precompose worker adapter, first-stage Apply materialization, canonical
intermediate recording, SQL admission and concrete CPU installation are authored;
no execution or runtime qualification has been performed. Seal/final-run
coordination, source-parent generated inheritance and durable route recovery
must be completed before this module receives coding closure. No execution or
runtime qualification is claimed by these source changes.

The recipe-level `protected_mask` is independently reread through the existing
accepted-source catalog after full closure validation and carried into the compiled
Override as reserved binding `editor.frozen.protected`. Its document hash, protected
flag, original source and canvas are checked; authored bindings are retained and a
reserved-name collision or capacity overflow rejects the compilation. The final
compilation digest includes this binding. Existing transformed-mask consumers then
receive this protection. This closes the missing carrier, not all effect-specific
protection qualification or source-parent generated inheritance.

### Current final-chain source pass

Migration02469 and the production scheduler processor now author the separate
seal run: immutable bounded params publication, current accepted-source parent
proof, initial generated coverage inheritance, exact canonical Apply trace and
independent adopter handoff. The final private recipe compiler preserves the
original freeze through its typed executed-base receipt and clears only effects
already executed. Receipt-bearing identity renders require complete document
and occurrence evidence even with an empty final layer stack.

The new effects journal retains an exact frozen command across uncertain POST
responses, makes accepted recovery read-only, and fences account/logout changes.
It is not yet connected to the Editor route. Final private source admission,
qualified render materialization, activation and final completion remain open
under migration02474. The original recipe has its original geometry; generic
current-source catalog checks must not be weakened to treat it as the processed
source. This source pass does not claim coding closure or runtime qualification.
### Final Editor chain: current authored integration

The Editor now persists an actor/origin/tenant/source-version scoped immutable effects command before dispatch. An accepted command is recovered using status reads only. The UI distinguishes build, seal and final run IDs, mounts presentation outputs only from the final run, and provides an explicit photographer-review link. Uncertain POSTs remain saved; only an accepted terminal command can be acknowledged.

Migration 02474 and the final coordinator retain the original frozen recipe and independently adopted executed-base receipt. Original and processed geometry are traversed as separate authorized provenance branches. Final composition reuses the existing calibrated V2 plan, immutable transport, typed input registration and graph activation. It does not replay authored layers, effective Style or scalar corrections.

Completion requires the current canonical render attempt/fence, independent adoption inbox evidence and existing review-completion gates. Authority withdrawal requests ordinary cancellation for live stage runs. Run reservations are released only after all related runs/tasks are terminal. Pending backend documents retain the existing physical cleanup protocol (`claim_document_cleanup` / `finish_document_cleanup`); they are not treated as removed merely because the Editor origin is terminal.

These are source changes and authored scenarios, not executed qualification. Whole-module source linkage review remains in progress.
