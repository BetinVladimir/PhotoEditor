# V27 CPU assembly runtime

`photo-worker run` supports the composed SQL/objectstore authority factory for `v2.semantic.assemble`. This connection is implemented; physical CPU assembly/upload/commit is verified with controlled upstream fixtures; CPU final adoption is verified with controlled upstream fixtures; real new-result workflow orchestration remains outstanding.

Enable it on a provisioned CPU worker using `PHOTO_SEMANTIC_ASSEMBLY_ENABLED=true`. Omitted value defaults to `false`; only literal `true`/`false` are accepted. The worker must have CPU class/backend, its own registered worker identity/DB principal, v2.2 renderer registration, live CPU resource slot and exact allowed operation. Existing worker TLS, queue, scratch and resource configuration remains mandatory. Registry/qualification activation is separate and is not performed by this flag.

Configure `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, optionally `S3_REGION`, with the same versioned store used by scheduler/gateway. Production requires an HTTPS origin without userinfo, path, query or fragment. HTTP is accepted only with the existing development mode. Provision credentials through the deployment secret mechanism; do not include them in documentation, command logs or source control.

The object client reads exact versions of qualification reports and producer Params only after worker-scoped SQL authorization. Candidate/snapshot/WorkingImage input reads and plan uploads continue through TaskArtifacts and the mTLS gateway. Scoped SQL statements finish before object IO; report/provenance/snapshot authorization is repeated after reads. Upstream GPU qualification is validated as transfer evidence, not claimed as a measurement of the CPU assembly host.

The startup factory uses the configured worker DB connection, never a scheduler connection. Migrations00526–00529 expose only worker-scoped running-lease functions. The existing worker registration-versus-installed-operations gate remains active. Wrong class, incomplete storage configuration and unsupported endpoint fail before worker startup.

Readiness of the scheduler assembly loop and its transition/failure counters are integrated into scheduler endpoints. Worker configuration tests verify CLI forwarding and valid/invalid store setup; SQL tests verify positive CPU scope and revocation. Physical CPU execution/upload/result commit is verified via the real mTLS gateway in the isolated test. CPU final adoption/replay is also verified with controlled upstream producer metadata. Real new-result workflow orchestration and deployment storage permission isolation still require acceptance evidence before declaring V27 complete.

The assembly execution profile reserves32MiB scratch: the retained input index reserves at least16MiB and plan staging may need8MiB. Go compilation, worker admission and forward-only migration00530 agree on32MiB; RAM stays256MiB and document output limit8MiB. Previously sealed8MiB tasks are not rewritten: they fail admission and require a newly admitted workflow/profile. No production migration was applied during these checks.

Direct report/producer object reads use5-second connect and10-second read timeouts with internal HTTP retries disabled. Certificate verification remains required with the trusted CA bundle. Activity/SQL authority checks remain between reads; these are per-block IO timeouts rather than a whole-operation deadline. Task retry policy owns recovery.

The worker object reader classifies network timeouts/connection failures and temporary S3 errors as retryable DEPENDENCY_UNAVAILABLE. Missing keys/versions are ARTIFACT_MISSING; authentication/certificate errors are CONFIG_INVALID. It does not reclassify SHA, JSON or content-binding failures. Error records contain task codes, not endpoint/credential exception text.


### Defect proposal protection inputs (2026-10-05 implementation contract)

`DefectProposeParams.protections` is an optional internal task field containing 0..64 original protected `MaskRef` values. `permanent` remains optional and is combined with this list. Every value must have `protected=true`; duplicate artifact UUIDs within the list or between it and `permanent` are invalid. The adapter requires exact WorkingImage canvas/transform-chain equality and every listed pixel ref in the authenticated lease command inputs before IO. This field does not authorize arbitrary masks: production preparation must supply original source-bound, catalog-authorized protection refs and the necessary person/manual qualification proof.

`prepare_proposals(..., permanent=None, protections=(), ...)` maps original source arrays read-only and initializes the scratch protection as the pixelwise maximum of permanent and additional protections in 256x256 tiles. All values must be finite float32 probabilities in [0,1]; invalid values fail before publication, and scratch is discarded. Facial feature protection is then merged through the existing two-pass face algorithm. There is no sum, multiplication, probabilistic union or intensity clipping. Source arrays are never mutated.

`verify_proposal_protection(..., permanent=None, protections=(), ...)` reopens materialized protection/labels before staging and checks that output protection is at least each original input value, preserves facial feature coverage and has no nonzero label on any positive protection. Cancellation propagates and closes the mapped files/reservation.

Python serialization preserves whether the protections key was originally supplied: old Params omit it; explicitly supplied [] remains []. Go's strict original report model uses an optional pointer to the list for the same distinction, rejecting null and preserving complete canonical roundtrip. Embedded report Params must continue to equal the original task Params bytes.

Worker assembly and original report validation are implemented and tested; automatic production graph preparation/discovery of the complete protection list and retained final transaction admission remain unfinished. This contract is not a production activation certificate.

Server original-evidence validation now resolves every permanent/additional protection pixel ref through `semantic_proposal_defect_declared_protection_object` (migration00668). The exact canonical defect producer must declare that UUID/SHA in its inputs. Ready/verified/unexpired/versioned float32 NPY catalog metadata, exact geometry and magnitude coverage by the published defect protection are checked; all original required pins are repeated after dependency IO. Missing inputs, invalid binaries, weakened coverage or stale pins yield empty evidence. This validates declared protections; independent server-required person/manual/source policy and final transaction proof are still mandatory separate obligations.

`review.BuildDefectProtectionInputs(ctx, source, permanent, additional, manuals)` compiles the protection component from server-selected policy masks and original manual manifests. Each manual entry contains original root UUID/SHA, body and expected target. The function validates original SHA/schema/source binding and derives the protected child from the manifest; manual roots require oriented source coordinates with no transform steps, matching mask.rasterize. Caller-selected protected child UUIDs are not used. Exact duplicates are deduplicated; conflicting complete mask metadata for one UUID denies. Permanent is retained separately, additional protections/pixel inputs/original roots are sorted by UUID for deterministic replay. Returned pointer/raw fields are independently decoded. Limits:64 manual roots,64 additional policy entries,64 final additional protections and4MiB aggregate manual metadata. Overflow/cancellation/malformed input returns an empty plan, never truncation. This is a pure compilation component: catalog/producer/qualification authority and final admission must be supplied and retained by the production service.

The compiler also reserves artifact UUID roles across the entire protection component: source image, manual JSON root and protection NPY must have distinct identities. Equal protections and equal roots may still deduplicate within their own role. A cross-role alias fails with an entirely empty plan before task construction, even if each supplied reference is individually well formed.

`review.BuildDefectProposalTaskInputs(ctx, source, faces, diameter, permanent, additional, manuals)` builds the complete pure task-input component. `DefectProposalFaceInput` exposes the worker's object/landmarks/skin/manual/manual_mode binding; face order is preserved. `DefectProposalTaskInputs` returns canonical `Params` bytes, their SHA256, UUID-sorted deduplicated `Inputs` and sorted original `ManualRoots`. Params always include an explicit protections list, including []; null faces are refused. Diameter bounds are .002 <= low <= high <= .012. Params validation is shared with original report validation, including source geometry, face modes, mask selectors/flags, duplicate face/object identities and protection bounds. Dependencies include source, every landmark, both declared face masks even when inactive, all protection pixels and original manual roots. One UUID cannot have conflicting digests or roles. Equal refs within one role deduplicate. Cancellation or any invalid dependency returns the fully empty result. Canonical serialization owns the returned byte buffer independently of caller mask mutations.

This compiler does not create or enqueue a task. Production preparation must discover and authorize the complete source set, retain original lineage/qualification/transaction proofs, publish Params and construct/admit the actual workflow before activation. It does not perform face inference or manual rerasterization.

Source dependency correction: BuildDefectProposalTaskInputs requires a valid source.Pixels and includes that exact RGB NPY UUID/SHA in Inputs under a distinct source_pixels role. The worker's TaskArtifacts.input rejects undeclared binaries; a WorkingImage JSON ref alone is insufficient. Source RGB cannot alias source JSON, landmark JSON, face-mask NPY, protection NPY or manual root identities.

Production preparation should use `BuildDefectProposalTaskInputsFromImage(ctx, imageRef, imageBody, faces, diameter, permanent, additional, manuals)` with the original catalog-read WorkingImage. It validates original body SHA and strict schema (2 bytes..1MiB), requires float32/work_rec2020_gamma24_v1 as the worker does, derives geometry and pixel child from the original document and then calls the task compiler. Alpha and transform-field refs are not added by this operation: defects.propose does not read those binaries. Qualified source verification still must verify their broader source lineage independently. The wrapper is not catalog, ICC or pixel-value authority.

`semanticproposal.CompileAndPublishDefectParams(ctx, publisher, scope, imageRef, imageBody, faces, diameter, permanent, additional, manuals)` connects the original-image compiler to the existing `v2input.DocumentPublisher`/`artifactjson.Publisher`. Scope must have nonzero tenant/run/graph-owner IDs and purpose task_params; assembly fencing is forwarded unchanged for publisher enforcement. It finishes all pure source/Params/input validation before Publish, sends the exact canonical bytes, checks the returned nonzero artifact UUID and exact SHA, and refuses Params UUID aliases with dependencies. Result `PublishedDefectParams` retains published Params ref, complete compiled input inventory and original manual roots. Any refusal/cancellation yields empty output. Existing owner-scoped publication recovery behavior belongs to the publisher; this wrapper does not delete or duplicate a completed artifact after an unknown result.

The trusted caller must establish original catalog/qualification authority before publication and retained final admission afterwards. This function does not create a run/task row, select manual/person policy sources or enqueue a workflow. Unit publisher tests establish the wrapper boundary, not physical production ownership/admission.

SQL ownership limitation (verified physically): nonzero scope UUIDs do not establish an authorized defect producer. The current reserve_backend_document permits only explicitly supported review/download graph owners; the assembly route requires a live semantic assembly lease and its input inventory. An arbitrary defects.propose task/owner UUID is refused before object PUT and creates no backend_documents row. The wrapper is ready for integration with an authorized publication owner, but the dedicated pre-producer owner/admission path must be implemented before this can publish production defect Params. Do not widen existing SQL owner allowlists merely to make the wrapper succeed; bind the new owner to exact source/operation/graph/fence and qualification state.

Pre-producer manual catalog (migration00669): `semantic_proposal_manual_preparation_objects(tenant,project,actor,session,version,source,sourceSHA,targetPixel,targetSHA)` resolves the unique original rasterized_mask root, original Params and target NPY without requiring any finished defects.propose artifact. It requires current tenant/editor/project authorization, active tenant and unarchived project, accepted asset version whose exact master/source SHA matches, verified live versioned WorkingImage source, and same-project/source canonical succeeded CPU mask.rasterize producer/current finished attempt. Root and target must have exact output roles, produced attempt/fence and membership in canonical task_results.outputs. Root/Params/target metadata must be ready/verified/unexpired/versioned with correct bounded media. LIMIT2 exposes ambiguous root inventory. API-only SECURITY DEFINER with fixed search_path/owner; no new tables or DELETE grants.

`SQLTransferSourceReader.manualPreparationObjects` owns validated catalog metadata and rejects missing/multiple/invalid rows with zero result. It is a preflight catalog resolver, not original-body/rasterization authority or final transaction proof. The complete manual content/Params/vectors/NPY/PNG verification must next be composed independently of a defect result; current finished-defect reader cannot serve as a pre-producer preparation service.

Migration00670 adds `semantic_proposal_manual_preparation_output_object` with the nine original manual catalog arguments followed by output UUID/SHA/role. It requires exactly one00669 root, the same canonical rasterizer result/current attempt, exact child output role and result membership/fences, live verified versioned metadata and bounded NPY/PNG media. Allowed roles are only mask_protected, mask_target_png and mask_protected_png. API-only fixed-search-path SECURITY DEFINER; no DELETE grants/new table. `manualPreparationOutputObjects` returns their ordered catalog pins, validates distinct refs/bucket/media/bounds, refuses any missing/stale/swapped child and returns zero output on failure. It is independent of a completed defect report. This enables the next full original-body/Params/vectors/NPY/PNG preparation verifier; it does not itself verify rasterized bytes or admit tasks.

Migration00671 `semantic_proposal_manual_preparation_vector_object` resolves an external ManualMask JSON by its exact UUID/SHA only when the unique original00669 rasterizer declares it in inputs. Ready/verified/versioned/live JSON metadata is bounded to1MiB; API-only fixed-search-path SECURITY DEFINER. `manualPreparationVectorObject` validates owned catalog pins and returns zero on refusal.

`ReadManualPreparation(ctx, who, tenant, binding, expectedTarget)` composes the independent pre-producer path. It snapshots expected mask metadata before IO; reads/validates original WorkingImage, catalogs original root/Params/target, verifies original SHA-bound root and mask.rasterize Params, verifies inline shapes or exact external vectors document/source geometry, derives all four original output refs, catalogs children, streams target NPY/PNG and protected NPY/PNG equivalence with complete SHA/size/finite-unit checks. It then repeats root/Params/target, children, optional external vector and source catalog/body checks after dependency IO. Success returns owned OriginalManualProtection for compilation; every error/cancellation returns the zero value. It requires no defect result and remains usable while unrelated defect reports expire.

This proves original document binding, binary integrity/NPY-PNG equivalence and live preflight pins. It does not establish exact vector-to-pixel rerasterization equivalence, qualified source ICC/RGB validity, complete policy inventory or retained final transaction admission. Those must be composed by production preparation before queue activation.

`PrepareDefectManualTaskInputs(ctx,who,tenant,binding,color,faces,diameter,permanent,additional)` composes the manual protection component with actual source IO. It snapshots caller policy collections before IO (faces<=64,additional<=64,policy metadata<=4MiB), verifies original source unit RGB/alpha/geometry and deployed ICC, validates initial Params/ref roles, reads every declared manual mask (including inactive masks) through ReadManualPreparation, bounds aggregate retained roots to4MiB, derives protected children and compiles exact canonical worker Params/SHA/input refs. It re-reads manual dependencies after the preparation batch, repeats qualified source verification and exact source bytes, and returns zero on any failure/cancellation. No caller-selected protected child replaces original manifest derivation.

The function prepares only the declared manual component. It does not establish that caller-supplied face/person/permanent/additional policy is complete and qualified, discover all current project masks, retain final SQL admission locks, rerasterize vectors, publish/admit a run or enqueue tasks. Those obligations remain explicit production preparation work.

Worker publication invariant: mask.rasterize reopens materialized target/protected NPY as strict read-only float32 unit arrays before staging any output. `verify_raster(mask,sourceSHA,target,protected,tile,check_active)` reconstructs original vector rasterization tile by tile and requires exact sample equality for both outputs; shape/dtype/contiguity/read-only/source/tile contracts must hold. It derives protected_nonzero independently and compares the preparation flag before publication, preventing incorrect omission/addition of mask_protected_ref. Corruption produces INPUT_INVALID before any stage; cancellation propagates and scratch contexts close. No full-frame RAM reconstruction or rasterization formula change; the cost is a second raster pass. PNG equivalence is additionally checked by the backend original reader.

This is a mandatory worker consistency check. It does not replace a backend vector-to-pixel proof or a retained producer qualification/admission certificate; a controlled synthetic finished task in tests does not establish that this worker check executed.

`review.ResolveManualRasterPlan(ctx,paramsBody,paramsRef,rootBody,rootRef,expected,source,externalBody)` produces an opaque immutable ManualRasterPlan from original verified Params/root/source. Inline vectors are derived directly from Params and unsolicited external bytes are refused; external vectors must match the exact Params-derived UUID/SHA and strict original document. It retains canonical vector bytes, canvas and shape count. CanonicalVectors returns an independent byte copy; no mutable shape/point/radius collections are exposed. Errors/cancellation return zero plan. ReadManualPreparation attaches this plan as OriginalManualProtection.Raster alongside the original root/target. The plan is validated vector metadata, not a raster output certificate; it enables subsequent independent backend sample reconstruction/comparison.

`ManualRasterPlan.RasterizeRegion(ctx,x,y,width,height)` independently reconstructs owned row-major float32 target/protected samples for a source rectangle, bounded to1024x1024 (two output arrays,<=8MiB). Invalid/zero plan, out-of-canvas/oversized region or cancellation returns nil arrays. Shape order follows sequence. Stroke distance, hardness/ramp, four-subpixel polygon coverage and float32 max/subtract/protect composition follow existing Python formulas. Conservative one-pixel-expanded shape bounds skip irrelevant regions; cancellation is checked across rows, pixels and segment/edge loops. This is a computation component, not stored artifact verification or processing admission.

`manual-raster-python.json` contains actual Python float32 bit-pattern samples for six61x47 cases: empty, soft segment, hard single-point protection, polygon plus degenerate subtract stroke, out-of-order mixed polygon/soft stroke, and interleaved protection/subtraction. Go verifies every target/protection sample using7/32/1024-region partitions. This establishes parity for the corpus only. Broader numeric boundary/platform qualification and complete streamed original NPY comparison are required before claiming independent backend raster equivalence.

Backend streamed raster comparison: `masknpy.VerifyRasterProbabilityNPY(ctx,target,protected,width,height,expected)` opens strict original float32 NPY payloads and requests expected samples for row segments<=1024. Both actual streams must have finite unit probabilities and exact float32 bit equality to reconstructed values. Expected lengths must match; callback errors/cancellation propagate. Success requires complete header/shape/size/SHA/EOF for both streams; only then is positive protection count returned. Payload buffers total8KiB plus bounded expected slices; no full-frame allocation. Readers remain caller-owned.

`verifyManualRaster` opens/closes both catalog-pinned NPY streams and supplies ManualRasterPlan.RasterizeRegion. ReadManualPreparation now requires this original-vector-to-target/protected comparison after both PNG equivalence checks and before live pin repetition. Earlier statements that this pre-producer reader does not compare vectors to pixels are superseded. The finished-defect reader still requires integration of the same independent comparison. Corpus/platform numeric and resource qualification, complete policy discovery and final transaction admission remain separate obligations.

Finished-defect integration: ReadDefectFaceMaskRoots now constructs the immutable original raster plan from the verified manual producer Params/root/source and optional original external vectors. After strict target/protected PNG equivalence it requires verifyManualRaster on both original NPY streams, then checks defect protection coverage. Existing producer/catalog/qualification and post-IO pin checks remain unchanged. Any raster mismatch returns no root inventory; callers cannot obtain a partially verified mask list. This supersedes the earlier remaining-work statement for the finished-defect reader. Independent comparison still requires wider numeric/platform/resource qualification and does not replace final transaction admission.

Numeric corpus extension: `tests/fixtures/generate_manual_raster_boundaries.py` regenerates36 deterministic Python production-rasterizer cases using seed20261005. Covers tiny/nextafter radii, hardness0/1/nextafter endpoints, coincident segment points, source-border coordinates, near-degenerate horizontal polygons, shuffled sequence order, add/subtract/protect composition and4097x3/3x4097 canvases crossing stream segment boundaries. Run from repository: `PYTHONPATH=workers .venv-platform/bin/python tests/fixtures/generate_manual_raster_boundaries.py`. Go checks both full original streams and7/32/1024 region partitions for all existing6+new36 cases. Regeneration is byte-identical. This is a42-case numeric corpus, not universal/platform/resource qualification or scientific model acceptance.

`ManualRasterPlan.EstimateStreamWork()` derives conservative integer resource counts for one paired NPY/vector verification pass: pixels, ceil(width/1024)*height row segments, segmentCount*totalPoints bounding checks, pixels*(sum stroke segments +4*polygon edges) sample edge evaluations,8*pixels NPY payload bytes and<=8192 expected segment bytes. Spatial skips are ignored to avoid understating work. Headers/catalog/JSON and repeated passes require separate accounting. Counts use int64 and cover the200M-pixel contract without truncation.

`ManualRasterPlan.CheckStreamBudget(maxPointChecks,maxEdgeEvaluations,maxPayloadBytes)` derives the estimate from the immutable plan and returns zero estimate on invalid/insufficient/negative limits. Callers cannot supply a mutable estimate in place of the plan. Zero limits do not imply unlimited. Deployment-selected qualified limits must be integrated before large object reads and charged across the entire preparation/admission batch. This API is a work bound, not measured CPU time, GPU qualification or final admission; runtime limit selection/enforcement remains unfinished.

Runtime one-pass budget enforcement: SQLTransferSourceReader.ManualRasterLimits contains explicit PointChecks/EdgeEvaluations/PayloadBytes bounds. Both ReadManualPreparation and the manual branch of ReadDefectFaceMaskRoots derive the immutable original plan and check limits before their manual target/protected NPY/PNG verification. verifyManualRaster repeats the check before opening streams. Zero/insufficient limits fail with empty reader output; no unlimited fallback. Existing source/defect evidence IO may occur before the manual branch, so this is not a total IO admission budget. In particular finished-defect evidence can read declared protection inputs before manual raster verification.

Production deployment must supply qualified per-pass limits; fixture values are synthetic test configuration, not hardware calibration. Complete batch and repeated-pass accounting must still be integrated with retained admission, along with headers/JSON/catalog and source/defect evidence IO. Current public activation remains blocked until those production obligations are satisfied.

Aggregate raster scope: SQLTransferSourceReader.ManualRasterBatchLimits supplies explicit point/edge/paired-NPY limits for one top-level call. ReadManualPreparation, readDefectFaceMaskRoots and PrepareDefectManualTaskInputs create a private scope only when no parent scope exists; nested/repeated manual readers share its synchronized meter. Original plan work is reserved atomically before manual NPY/PNG verification. Failed reservations do not change counters; accepted work is not refunded on later IO failure. Distinct top-level calls get distinct meters, preventing accidental process-global quota sharing. Zero limits refuse actual raster work.

This accounts for repeated vector reconstruction passes across a preparation call. Paired payload counts describe raster-comparison reads only; PNG/coverage/source/defect/header/JSON IO, other images/workflows, cross-request retries and durable scheduler admission still need broader qualified accounting. Fixture limits are test policy, not deployment calibration.

Retained manual preflight evidence: `readManualPreparationProof` returns original compiled-manual material plus a private manualPreparationProof containing exact tenant/project/actor/session/source binding, root/Params/target catalog metadata, all three child pins and optional original external vector pin. Only successful full content/raster/budget/post-IO checks produce a proof. Public ReadManualPreparation keeps its existing signature and drops the internal proof. `prepareDefectManualTaskProof` retains all proofs beside compiled task inputs; public preparation remains a content-only wrapper. All error paths return zero original material and no proof inventory.

`recheckManualPreparationProof` refuses identity/source scope mismatch, repeats unique canonical origin/child/vector catalogs and compares exact metadata. Preparation invokes it after source/dependency IO. It is a live preflight repeat, not SQL row locking, policy completeness or final admission authority. Final scheduler transaction must lock/repeat these original catalog and producer/qualification rows before admitting tasks.

Retained-proof scope guard validates nonzero tenant/project/user/session/version/source/root identities and valid original source/canvas digests before catalog access. Exact proof tenant/project/actor/session/version/source UUID/SHA/canvas hash must equal the current requested context. Matching malformed or empty contexts do not authorize a proof. Cancellation is checked first. This isolates preflight evidence across tenants, sessions and source revisions; live catalog/qualification/final lock obligations remain separate.


### Retained manual evidence pin closure (2026-10-05)

`marshalManualEvidencePins` derives canonical JSON exclusively from the private `manualPreparationProof`: six distinct artifacts (rasterized_mask, task_params, mask_target, mask_protected, mask_target_png, mask_protected_png), plus manual_vectors when external vectors were verified. Each role binds artifact_id, sha256, bucket, key, version, bytes and media_type. Bucket comes from the trusted reader configuration. Source UUID cannot alias any evidence artifact. Invalid scope, empty/null version, mismatched digest, wrong media or out-of-contract size returns nil bytes and an error. Roles sort lexically before canonical serialization. `recheckManualPreparationProof` validates this closure before catalog IO.

This is a retained-evidence representation and preflight guard. Final admission must still lock source/auth/version and selected producer/result/workflow/evidence rows, compare these exact pins, repeat unique inventory under the writer barrier, and commit queue admission with its durable receipt. This serializer does not authorize caller JSON or activate semantic-propose HTTP.


### Selected manual transaction locks (2026-10-05)

Forward-only00672 exposes scheduler-only `lock_semantic_proposal_manual_evidence` with ten arguments (tenant/project/actor/session/version/source/source SHA/target pixel/target SHA/exact evidence JSON). It locks selected producer task, attempts/results, all producer artifacts and Params plus source and retained external vectors, run and existing workflow; repeats unique manual root/Params/target catalog and child/vector producer gates and compares exact role-sorted pins. Forward-only00673 grants UPDATE(run_id) on v2_workflows only to photo_fn_owner because PostgreSQL FOR SHARE requires an UPDATE column privilege; no new API/scheduler direct write grant. Direct positive/negative pins and selected artifact/task/result concurrent writer blocking are physically tested.

Caller must still compose source/auth/version locks and complete inventory writer barrier, repeat final inventory under that barrier, and persist admission and receipt atomically. This function alone is not a complete admission certificate. Existing workflow row locking and absent-row phantom behavior require additional physical coverage.


### Private Go manual transaction adapter (2026-10-05)

`SQLTransferSourceReader.lockManualPreparationProof(ctx, tx, who, tenant, binding, proof)` accepts only private retained proof. Before SQL it validates context, transaction, exact project/user/session/tenant/source-version/canvas scope and canonical six/seven object closure. It requires current_user photo_scheduler, calls00672 with privately derived pins, obtains the existing tenant inventory writer barrier through lock_semantic_proposal_person_inventory, then repeats00672 using the same owned pins. False SQL result becomes ErrConflict; dependency errors propagate; any error requires caller rollback. No commit, queue task or public activation occurs in this helper. Source/auth/version locks and complete qualified policy discovery remain independent final admission requirements. Unit coverage checks exact pins/target, sequence, fail-fast on either lock or barrier refusal, wrong role, foreign scope, cancellation and SQL error propagation. Physical SQL behavior was tested separately; the Go adapter has not yet been exercised in the physical preparation fixture or installed into final admission.

Physical coverage update: manual-lock-go-external now independently verifies lockManualPreparationProof after real original-content verification, using photo_scheduler pool/local tenant transactions and exact retained pins. Changed location and expired root deny, restored root accepts. The physical Go adapter is verified, but is still not installed in a complete admission service. Full external scenario and remaining race checks retain separate obligations.

Manual IO race coverage (2026-10-06): independently reported manual-races-external physically verifies original child catalog/source gates, corrupt NPY/PNG outputs, post-read root/child expiry, undeclared/expired external vectors and restored inventory. All failure paths require empty results. Existing complete scenario retains Params compilation/publication and subsequent skin/qualification/admission checks; isolated manual checks do not prove those stages.

Admission actor status locking (2026-10-06): forward-only00674 retains photo.users actor row FOR SHARE in lock_semantic_proposal_inventory after tenant/usage locks and before session/membership locks. This prevents active-status changes between authorization recheck and transaction completion. Physically verified concurrent actor UPDATE timeout, post-release disabled refusal and active restoration. Callers still must compose inventory/source locks, evidence/qualification/barrier checks and durable admission in one transaction; manual evidence helper alone does not lock all authorization rows.

Source/auth scope physical coverage (2026-10-06): inventory transaction blocks concurrent source artifact, asset version, session, project and snapshot writes. Frozen snapshot child inventory remains protected after release by guard_v22_frozen_items; INSERT and DELETE both raise V22_FROZEN_ITEMS_IMMUTABLE and preserve item count. No extra snapshot migration is required for this existing frozen-v2 scope. Other snapshot kinds and deletion/purge interleavings require their own scope-specific checks.

Batch manual transaction composition (2026-10-06): lockManualPreparationProofs accepts1..64 private proofs for one binding. Scope and exact canonical pins are captured before SQL; conflicting duplicate roots reject the entire batch. Unique roots sort by UUID text; all selected evidence rows lock before one shared tenant writer barrier, then all catalog pins are repeated. Single-proof API delegates to this path. An empty manual inventory is handled by caller skipping this helper; it is not an admission certificate. Multi-root physical coverage and final workflow composition remain pending.

Multi-root physical coverage (2026-10-06): lockManualPreparationProofs is exercised with two independent canonical manual producers after original vectors/root/Params/NPY/PNG verification. Identical repeated proof is deduplicated; changed second Params location and expired second root reject the batch; restored second root accepts. Both physical completion markers are required by Python. Final qualified inventory/workflow/admission composition remains pending.

Independent face preparation catalog (2026-10-06):00675 semantic_proposal_face_preparation_object takes tenant/project/actor/session/version/source UUID+SHA/output UUID+SHA/operation and returns output bucket/key/version/bytes plus original Params UUID/SHA/bucket/key/version/bytes. Only canonical face.detect→face_boxes and face.landmarks→face_landmarks @1 are allowed. Selected producer/current attempt/result/output fences and source scope are checked without requiring a completed defect report. CPU/CUDA backend equality is catalog consistency only; exact model/runtime/device qualification and original JSON/Params/detection linkage remain reader obligations. No standalone Face68 arbitrary JSON authority is introduced.

Independent face content preparation (2026-10-06): ReadFacePreparation returns the complete owned source-bound fan2d collection after original detector/landmark/Params verification, declared exact detector input gate00676 and repeat catalog/source checks. It does not require defect results. Callers must separately qualify source pixels/ICC and exact face producer/model/runtime/device reports, retain evidence for final locks, and discover all policy-required faces. A returned face set is preflight content evidence, not activation authority.

Independent selected face qualification (2026-10-06): ReadQualifiedFacePreparation requires landmark/detector refs, two operation-specific reports and independent Expected policies plus trusted source color profile.00677 binds stored task/attempt backend/model/runtime/device to these policies; original report verification, source pixels/ICC, full detection/landmark/Params chain and repeat pins/policy/profile checks precede returning faces. This is selected preflight authority only. Caller must discover complete required collections and retain exact source/output/Params/report/policy evidence for final transaction locks. Actual model/device scientific reports are deployment inputs, not generated from this synthetic fixture.

Retained qualified face proof (2026-10-06): private readQualifiedFacePreparationProof creates immutable scope/policy and exact output/Params/report catalog evidence only after full qualified preflight. Public reader drops this internal proof. recheckFacePreparationProof repeats exact scope/profiles/catalog/report pins and refuses stale qualification; it does not hold final DB locks and is not queue activation authority. Source RGB/ICC metadata closure must separately be retained/locked in final composition.

Selected face qualification locks (2026-10-06):00678 and lockFaceQualificationProof retain exact face policy/import/report rows for both independently configured operations in one scheduler transaction. Canonical private policy/pins are captured before SQL; exact report mismatch denies. Concurrent report/import/policy UPDATEs are physically blocked. Caller still must hold authorized inventory/source scope, producer/attempt/result/output/Params evidence and complete inventory barrier/recheck, then persist durable admission atomically. This adapter is not a standalone admission certificate. Qualification report pin bounds remain2..8MiB.

Selected face producer evidence (2026-10-06):00679 plus lockFaceEvidenceProof bind original landmarks/detector outputs and both Params to exact private role-sorted catalog pins while retaining canonical tasks/current profiles/results/attempts, artifacts, runs and existing workflow rows. The selected exact source/link/profile gates repeat under row locks. Caller must combine source/auth inventory, qualification policy/import/report locks and complete tenant inventory barrier/recheck before durable admission. This primitive alone does not prove inventory completeness or authorize queued work.


## 2026-10-06 — V27 selected face batch transaction composer

Added private `lockFacePreparationProofs`: bounds 1..64, validates every owned proof and canonical qualification/evidence arguments before SQL, rejects conflicting duplicate landmark roots, deduplicates identical proofs, sorts roots, locks selected producers and qualification authority before one tenant inventory barrier, then repeats both lock families. Errors require caller rollback; no commit or queue. Extracted pure qualification argument serializer shared with the existing lock adapter.

Verification: `GOCACHE=/private/tmp/photoeditor-go-cache go test ./internal/semanticproposal` passed (0.974s). New tests verify duplicate suppression, lock/barrier/repeat order, fail-fast rejection at boolean lock/barrier phases, and invalid/conflicting batches with zero SQL calls. These are Go adapter tests; no new physical DB composer test was run in this turn. Previously verified individual SQL lock functions do not substitute for that pending test.

Remaining: physical batch composition, multiple-root qualification ordering/concurrency, complete required inventory discovery, global policy phantom handling, source RGB/ICC closure, and final atomic admission. This composer covers selected proofs only and does not establish complete V27 or V20–V32 implementation.


## 2026-10-06 — V27 face batch physical composition verification

The isolated PostgreSQL/MinIO `manual-lock-go-external` scenario now invokes `lockFacePreparationProofs` with duplicated original retained face evidence in one scheduler transaction. It requires successful composition through the tenant inventory barrier and repeated SQL locks. Independent owner connection updates of both original Params artifacts and both qualification reports must time out with PostgreSQL 55P03 while the transaction holds locks. Mutated landmark Params key and mutated detector qualification report key independently return ErrConflict. Python requires `V27_FACE_BATCH_TRANSACTION_LOCK_OK` so skipped Go coverage cannot pass silently.

Physical command: `PHOTO_TEST_MINIO_TMPFS=1 PYTHONPATH=workers:src:tests .venv-platform/bin/python -m pytest tests/integration/test_v27_semantic_intent_publication.py -k manual-lock-go-external -q --tb=short`. Terminal result: 1 passed, 12 deselected, 44.48s. Test database/migrations and object storage were isolated fixture resources; no production migration/deployment.

Extended Go adapter tests to two distinct landmark roots presented in reverse UUID order with a duplicate, requiring sorted evidence locks, all locks before one barrier, and exact arguments repeated after the barrier. Physical multi-root concurrency remains unverified; global qualification policy phantom prevention and full final admission remain outstanding.


## 2026-10-06 — V27 independent parser manifest/Params catalog

Forward-only migration 00680 extends `semantic_proposal_face_preparation_object` with `face.parse` -> `face_labels_ref`. Existing tenant/session/editor/source/project checks, exact canonical succeeded task/current attempt/fence/result, source input declaration, ready/versioned/unexpired original JSON manifest and Params checks remain unchanged. Fixed search_path, photo_fn_owner ownership, PUBLIC revocation and photo_api execute allowlist remain explicit. The Go catalog operation guard accepts face.parse. No complete parser admission is implied: labels pixel closure, declared detector linkage, content/NPY verification and independent qualification still need composition.

Physical isolated PostgreSQL/MinIO command: `PHOTO_TEST_MINIO_TMPFS=1 PYTHONPATH=workers:src:tests .venv-platform/bin/python -m pytest tests/integration/test_v27_semantic_intent_publication.py -k manual-lock-go-external -q --tb=short`. Result: 1 passed, 12 deselected in 159.84s. Added parser catalog checks compare actual Params artifact ID, reject expired Params, accept restored Params and reject face.detect operation for parser manifest. The SQL function has no defect-output dependency; this fixture did not remove all existing defect producers, so a dedicated pre-defect parser scenario remains pending. Existing face/manual physical checks also passed in the same scenario.


## 2026-10-06 — V27 independent parser labels catalog and detector declaration

Migration 00681 adds API-only SECURITY DEFINER `semantic_proposal_face_parse_preparation_pixels`, fixed search_path/photo_fn_owner/PUBLIC revoked. It composes independent exact parser/detector catalogs and requires parser's declared detector input, source/project authority, canonical current result declaring exact labels artifact, current produced attempt/fence, binary media, 32..400065548 bytes, ready/verified/versioned/unexpired object. No defect output is consulted. Go `faceParsePreparationPixels` validates selected refs and both JSON catalog pairs, rejects aliases across source/manifest/Params/detector/labels, requires exactly one metadata row and exact bucket/version/size. It returns pins only, not content/model/admission authority.

Physical isolated PostgreSQL/MinIO: `PHOTO_TEST_MINIO_TMPFS=1 PYTHONPATH=workers:src:tests .venv-platform/bin/python -m pytest tests/integration/test_v27_semantic_intent_publication.py -k manual-lock-go-external -q --tb=short`: 1 passed, 12 deselected, 122.24s. Original labels catalog succeeds; expired labels fail; restored labels succeed; removing declared detector input fails; restoring inputs succeeds. Physical test covers SQL catalog, not the newly added Go adapter's positive path. Go package test run recorded separately below. Full source/NPY/content/qualification/transaction composition remains outstanding.


## 2026-10-06 — V27 independent original parser content and Face19 NPY

Added `masknpy.VerifyFaceLabelsNPY`: exact hash/size/EOF C-order little-endian uint16 array, source dimensions bounded to 200M pixels, class range 0..18, a 32KiB stream buffer, per-class histogram returned only after complete integrity checks. Failure/cancellation returns zero histogram; readers remain caller-owned. Tests cover histogram, invalid class19, truncation/trailing bytes, changed original hash, stream chunk boundary and cancellation.

Added `ReadFaceParsePreparation`: original source WorkingImage, parser manifest/Params, detector/Params, exact declared detector and original labels catalog; strict `VerifyFaceParseParams`/`VerifyFaceDetectParams`; complete labels NPY; zero-face manifests require all-background labels; metadata and source repeat after IO. Closes labels stream and propagates close errors. Returns zero manifest/counts on any error. This verifies selected content only; qualification, complete RGB/ICC closure, global discovery and final admission locks remain separate.

Go tests `GOCACHE=/private/tmp/photoeditor-go-cache go test ./internal/masknpy ./internal/semanticproposal` passed (masknpy0.501s, semanticproposal0.671s after reader addition). Physical isolated PostgreSQL/MinIO `manual-lock-go-external` passed: 1 passed, 12 deselected in45.32s. Go reads actual original parser/Params/detector/labels, requires positive skin-class histogram and exact original labels ID; expired labels deny with zero output. Python requires `V27_FACE_PARSE_PREPARATION_CONTENT_OK`. The positive fixture contains existing defect producers, although this reader never uses their output refs; a dedicated pre-defect fixture and broader parser IO race tests remain pending.


## 2026-10-06 — V27 independent parser/detector stored qualification profiles

Migration 00682 adds API-only `semantic_proposal_face_parse_preparation_profiles_match`: independent parser labels/detector declaration/catalog gate00681 plus two exact selected task/attempt profiles, operations face.parse/face.detect version1, finished current attempts and exact backend/model/runtime/device hashes against supplied independently selected Expected policies. SECURITY DEFINER/photo_fn_owner/fixed search_path/PUBLIC revoked. Go adapter owns Expected maps, validates operations/version/backend/digests before catalog/SQL, and returns ErrUnqualified on stored profile mismatch. Stored profiles alone do not verify qualification report content or measured inference.

Go `GOCACHE=/private/tmp/photoeditor-go-cache go test ./internal/semanticproposal`: passed0.781s. Physical isolated PostgreSQL/MinIO `manual-lock-go-external`: 1 passed,12 deselected,37.21s. Exact parser/detector Expected profiles succeed; substituted parser RuntimeLockSHA fails ErrUnqualified. Python requires V27_FACE_PARSE_PREPARATION_PROFILES_OK. Prior independent original parser content/labels checks also pass. Full original trusted reports, retained immutable parser proof, locks/global discovery and final admission remain outstanding.


## 2026-10-06 — V27 independently qualified parser preparation

Added `ReadQualifiedFaceParsePreparation`: owned independent parser/detector Expected policies, exact selected producer profiles, trusted imported report catalog pins plus original report bytes verified by v2qualification.Verify, original JSON producer/Params pins and labels pixels pinned before IO, qualified original source pixels/color before and after independent parser content verification. Requires returned manifest to reference the exact caller-selected detector/labels and both original model/backend claims to match independent policies. Repeats original producer/Params/report/pixels pins and profiles after IO; errors return zero manifest/histogram. It remains selected preflight, never queues/commits and does not replace final source/policy/evidence locks or global discovery.

Go `GOCACHE=/private/tmp/photoeditor-go-cache go test ./internal/semanticproposal`: passed0.762s. Isolated PostgreSQL/MinIO `manual-lock-go-external`: 1 passed,12 deselected,41.53s. Actual parser/detector reports and original content return exact labels ID/nonzero class1 histogram. Replacing parser report with landmark qualification report denies with zero output. Python requires V27_FACE_PARSE_PREPARATION_QUALIFIED_OK. Qualification fixture is protocol evidence only, not real model scientific approval; deployment still requires genuine independently produced qualification. Retained immutable parser proof, global discovery/policy phantom handling and atomic admission remain pending.


## 2026-10-06 — V27 retained private parser preparation proof

Private `faceParsePreparationProof` owns comparable value pins for original parser/detector manifest/Params, labels pixels and two qualification reports, plus tenant/project/actor/session/source binding and canonical owned Expected policy string. Reuses the common selected face metadata envelope only; parser-specific scope validator requires face.parse/face.detect operation order and validates labels binary metadata, size/digest and distinct IDs across all pinned artifacts/source. Public qualified reader preserves its API and drops the private proof; private preparer returns zero proof on every failure. `recheckFaceParsePreparationProof` rejects invalid scope before DB then repeats profiles, original producer/Params/report pins and exact declared detector/labels catalog. No commit/queue or source RGB/ICC retained closure is claimed.

Go semanticproposal tests passed1.229s. Physical isolated PostgreSQL/MinIO `manual-lock-go-external`: 1 passed,12 deselected in86.17s. Original retained proof rechecks; foreign tenant ErrInvalid; original parser report expiry denies; restored report rechecks. Python requires V27_FACE_PARSE_PREPARATION_RETAINED_PROOF_OK. The full qualified preparer path still verifies original report/content bytes; retained recheck is catalog/profile repeat only. Remaining final obligations: selected evidence/policy transaction locks, global inventory/policy completeness, source RGB/ICC closure and atomic queued admission.


## 2026-10-06 — V27 selected parser qualification transaction locks

Migration00683 adds scheduler-only SECURITY DEFINER `lock_semantic_proposal_face_parse_qualification`, fixed search_path/photo_fn_owner/PUBLIC revoked, scoped to face.parse/face.detect. Exact selected enabled/admission-enabled/license-cleared policy, trusted passed import and original ready/versioned/unexpired report metadata are matched and locked FOR SHARE. Exact RequiredChecks, issuer/dataset/model/runtime/backend/device and canonical report pin are required. Go private `lockFaceParseQualificationProof` validates retained parser scope and canonical policy/report arguments before SQL and checks photo_scheduler role. Common pure serializer now takes explicit operation order; existing landmark adapter retains its prior order. No permission widening to API, no queued admission.

Go semanticproposal tests passed1.311s. Physical isolated PostgreSQL/MinIO manual-lock-go-external passed:1passed,12deselected,48.05s. Original retained parser proof locks both qualifications; independent writer updates of both original report artifacts time out55P03; changed parser report key returns ErrConflict. Python requires V27_FACE_PARSE_QUALIFICATION_LOCK_OK. Physical policy/import mutation blocking for this new adapter, audit authority row locks, global policy phantom completeness, parser producer/labels evidence locks and final source/inventory/admission composition remain pending.


## 2026-10-06 — V27 selected parser producer/labels evidence locks

Forward-only00684 adds scheduler-only SECURITY DEFINER `lock_semantic_proposal_face_parse_evidence`, photo_fn_owner/fixed search_path/PUBLIC revoked. Exact independent parser/detector profiles and declared labels/detector lineage are checked before locks and repeated afterward. Two distinct selected producer IDs are sorted; task/current and historical attempt/result rows, source/all producer artifacts/Params, runs and existing workflows are held FOR SHARE. Exact original five role pins (parser manifest/Params, detector/Params, labels) are recomputed from catalog and compared to private evidence. No source RGB/ICC closure, qualification locks or complete inventory barrier is implied.

Private Go adapter canonicalizes five sorted pins from scope-validated retained parser proof, checks scheduler role and fails ErrConflict on false SQL. Go package tests passed0.839s. Physical isolated PostgreSQL/MinIO manual-lock-go-external passed:1passed,12deselected,55.25s. Original retained proof locks; independent writer updates of original labels and both Params artifact metadata are blocked55P03; changed original labels key failsErrConflict. Python requires V27_FACE_PARSE_EVIDENCE_LOCK_OK. Task/attempt/result writer checks for this new adapter, full batch barrier composition/global discovery/policy authority and final admission remain pending.


## 2026-10-06 — V27 parser proof batch transaction composition

Added `lockFaceParsePreparationProofs`: 1..64 private immutable selected proofs, scope and canonical evidence/qualification arguments checked for every proof before any SQL; identical root proofs deduplicate, conflicting duplicate roots reject; root UUID ordering; all selected producer/labels and qualification locks before single existing tenant writer barrier; repeat both lock families afterward. Caller rollback on any error; no queue/commit. Source/auth/RGB/ICC closure and complete global discovery/policy phantom prevention remain independent final obligations.

Go adapter tests verify two ordered roots with a duplicate, exact repeat arguments including all five pins, failure stops at evidence/qualification/barrier/recheck phases, invalid/conflicting/oversized batches make zero SQL calls. `go test ./internal/semanticproposal` passed0.738s. Physical isolated PostgreSQL/MinIO manual-lock-go-external passed:1passed,12deselected,50.85s. Original duplicate proof composes through barrier/recheck; changed labels and changed report keys independently reject ErrConflict. Python requires V27_FACE_PARSE_BATCH_TRANSACTION_LOCK_OK. Physical batch covers one selected root with duplicate; multi-root concurrency remains unverified. All prior individual parser/face/manual lock checks also run in this fixture. Full V27 and V20–V32 completion remains unproven.


## 2026-10-06 — V27 retained selected original source closure

Added private `sourcePreparationProof` with owned scope/binding, configured ICC SHA, unitRGB contract and canonical immutable original pins JSON. Capture obtains exact editor-scoped WorkingImage pin plus declared WorkingPixels, optional WorkingAlpha and each declared DenseWarpField catalog pin. Constructor captures before existing full qualified source stream validation and repeats the closure after all IO. Recheck guards tenant/project/actor/session/bucket/source/version/ICC/unitRGB before catalog access, rereads hash-pinned original source metadata and compares the complete original ordered closure. Failure returns zero metadata/proof. Does not lock DB artifacts or retain deployment registry files; no queued admission. ICC is server-verified configuration rather than tenant-object pin.

Go semanticproposal passed0.813s. Physical isolated PostgreSQL/MinIO manual-lock-go-external passed:1passed,12deselected,51.66s. Constructor validates actual source RGB/color; original proof rechecks; foreign tenant rejected ErrInvalid; source RGB artifact expiry denies; restored artifact rechecks. Python requires V27_SOURCE_PREPARATION_RETAINED_PROOF_OK. Optional alpha/dense-warp retained proof physical scenarios, canonical closure argument validation for SQL, final source artifact locks/barrier and composition with full discovery remain pending.


## 2026-10-06 — V27 strict retained source closure decoding before SQL

Added `decodeSourcePreparationPins`: bounds canonical owned JSON to4MiB, requires at least WorkingImage/WorkingPixels, exact root source/canvas, children parent binding, role ordering (optional alpha before geometry), valid artifact/digest/object version/media/minimum size, prohibits unrelated LayerRef authority pointers/configuration. Recheck invokes this pure decoder before catalog SQL. Unit malformed role,parent,version,SHA and injected ExpectedBinding cases reject in pure decoder. Existing reader field nil is not used as evidence of complete SQL mocking; decoder itself is directly asserted.

Go semanticproposal passed0.678s. Isolated PostgreSQL/MinIO manual-lock-go-external passed:1passed,12deselected,42.67s; original closure passes new decoder and existing expiry/foreign-scope checks. Source artifact SQL locks were not added in this turn; they remain the next integration step. Optional alpha/dense warp physical scenarios and global final admission remain unverified.


## 2026-10-06 — V27 selected source artifact transaction locks

Migration00685 adds scheduler-only SECURITY DEFINER lock_semantic_proposal_source_evidence, fixed search_path/photo_fn_owner/PUBLIC revoked. Selected artifact IDs locked FOR SHARE in UUID order; every original pin must match tenant/project/version/editor scope through existing v2_draft_artifact_scope, source/parent binding, ready/verified/versioned/unexpired object and exact metadata. Authorization/scope NULL explicitly deny, missing contract denies. It locks selected pins only; cannot discover missing source children or certify full final authority by itself. Go private adapter strictly decodes retained source proof before SQL, constructs canonical scoped pins and checks scheduler role; errors require rollback.

Go semanticproposal passed0.665s. Physical isolated PostgreSQL/MinIO manual-lock-go-external initial passed43.10s; after explicit SQL NULL fail-closed review final repeat passed44.05s (1passed,12deselected). Exact original source proof locks; independent writer RGB artifact update blocks55P03; changed RGB key rejectsErrConflict. Python requires V27_SOURCE_EVIDENCE_LOCK_OK. Optional alpha/dense warp physical locks, malformed direct SQL argument cases, complete source/auth/version locks, inventory barrier and final admission composition remain pending.


## 2026-10-06 — V27 direct source lock shape enforcement

Forward-only00686 replaces source evidence lock with explicit required shape: exactly one WorkingImage and WorkingPixels at positions0/1, at most one optional WorkingAlpha at position2, remaining DenseWarpField entries only. This closes direct SQL acceptance of duplicated root-only or pixel-only selected arrays. It still cannot infer omitted optional source children without verified original content; private preparer supplies the full captured closure. Extracted common pure source pin serializer for direct argument verification.

Go semanticproposal tests passed0.765s before new physical test. Physical isolated manual-lock-go-external first failed test compilation due nonexistent fixture.Project, fixed to reader.ProjectID. Second run found PL/pgSQL pin alias ambiguity; explicit entries.value/entry.pin qualifiers corrected. Final physical run:1passed,12deselected,59.66s. Direct scheduler SQL root-only duplicate, pixels-only duplicate and reordered root/pixels all return false; original proof still locks and RGB write is blocked. Python requires V27_SOURCE_EVIDENCE_SHAPE_OK. SQL NULL guards and exact metadata checks remain. Full combined proof/inventory admission still pending.


## 2026-10-06 — V27 combined selected source/parser transaction locks

Added private lockFaceParseSourcePreparationProofs. Validates configured ICC/unitRGB source scope and canonical original closure plus every parser evidence/qualification proof before SQL; copies proof slice to protect ownership across DB callbacks; conflicting duplicate roots fail before locks. Locks original source artifacts, composes selected parser/qualification batch with single existing tenant writer barrier and repeated proofs, then repeats source pins. Does not queue/commit; complete global discovery/auth/version locks and atomic admission remain caller obligations. ICC registry files are server configuration, not DB-locked resources.

Go semanticproposal passed0.698s. Adapter tests require source SQL before parser locks, barrier after all locks and exact source arguments repeated afterward; fail-fast at source/evidence/qualification/barrier/recheck; invalid parser batch causes zero source SQL. Physical isolated manual-lock-go-external passed1passed,12deselected48.91s; original retained source and duplicate parser proofs compose in one scheduler TX. Python requires V27_FACE_PARSE_SOURCE_TRANSACTION_LOCK_OK. Physical combined writer race, optional alpha/warp source cases, complete semantic inventory and queued admission remain pending.


## 2026-10-06 — V27 independently selected face collection discovery

Migration00687 adds API-only SECURITY DEFINER semantic_proposal_face_preparation_candidates, fixed search_path/photo_fn_owner/PUBLIC revoked. Discovers canonical source-bound landmark/detector pairs through exact declared inputs and existing independent original catalogs/profile matching, no defect outputs. Ordered by UUID; SQL returns at most65 so Go can reject overflow above64 rather than silently omit records. Go owns Expected policies, validates scope/digests/operations/backend and returns no partial set on failure. Empty selected set is returned as empty, not fabricated faces. This is complete only for matching selected profiles, not all policy-required evidence or scientific/content authority; original reports/content and post-barrier discovery repeat remain mandatory.

Go semanticproposal passed0.667s. Physical isolated manual-lock-go-external passed:1passed,12deselected40.31s. Two source-bound face collections discovered; expiring second original landmark removes exactly that collection and leaves exact first landmark/detector pins. Restored artifact afterward. Python requires V27_FACE_PREPARATION_DISCOVERY_OK. Bound-overflow, foreign scope, concurrent phantom discovery and full required policy inventory remain pending.


## 2026-10-06 — V27 qualified selected face inventory content preparation

Added prepareQualifiedFaceInventory: owns selected Expected policies, discovers every matching source-bound collection, rejects empty inventory, verifies each collection through original qualified content/source/report preparer, retains candidate/complete faces/private proof association, repeats every selected proof and ordered discovery after all IO. Failure returns nil rather than partial collections. This covers selected profile inventory, not global required policy completeness or post-barrier transaction stability. No queued admission or generic public endpoint activation.

Go semanticproposal passed0.830s. Physical isolated manual-lock-go-external passed1passed,12deselected32.57s. Two independently produced landmark/detector collections both pass original reports/content and retain exact proof association; substituted detector report in landmark slot rejects with nil inventory. Python requires V27_FACE_PREPARATION_INVENTORY_CONTENT_OK. IO phantom races, full inventory transaction locks, total metadata/IO admission budgets and required protection/defect discovery remain pending.


## 2026-10-06 — V27 exact selected face inventory transaction repeat

Migration00688 adds scheduler-only SECURITY DEFINER lock_semantic_proposal_face_inventory, fixed search_path/photo_fn_owner/PUBLIC revoked. Bounds inventory1..64, acquires/reenters the existing tenant writer barrier, repeats selected-profile source-bound face discovery and compares exact ordered landmark/detector ref JSON; overflow65 or missing/extra entries denies. Private Go composer owns proofs, validates common Expected pair and unique roots before SQL, locks every selected producer/qualification proof before barrier and repeats them, then invokes exact inventory SQL under the same barrier. No commit/queue/source closure/global policy discovery is implied.

Go semanticproposal passed0.817s. Physical isolated manual-lock-go-external passed1passed,12deselected44.32s. Both discovered qualified collections lock successfully in one scheduler TX; omitting one collection returnsErrConflict despite valid retained remaining proof. Python requires V27_FACE_INVENTORY_TRANSACTION_LOCK_OK. Concurrent phantom writer tests, full source composition, required policy completeness and final queued admission remain pending.


## 2026-10-06 — V27 face inventory phantom writer regression

Extended physical discovered-face inventory lock scenario: while scheduler TX holds all selected evidence/qualification locks and exact inventory barrier/repeat, an independent owner writer TX inserts a new artifact and separately a new pending face producer task cloned from valid selected producer fields, each with new UUID/key. Both inserts must fail PostgreSQL55P03 under local250ms lock_timeout. Writer TX always rolls back; new rows cannot remain even on unexpected success. This exercises insert-trigger barrier behavior rather than only updates of existing locked artifacts. It does not prove complete new qualified face graph publication or every table trigger.

Isolated PostgreSQL/MinIO manual-lock-go-external passed1passed,12deselected96.91s. Python requires V27_FACE_INVENTORY_PHANTOM_WRITER_OK. Existing full selected inventory positive and omitted collection negative tests remain. Global qualification policy insert phantom protection, released-barrier writer success, source/inventory combined admission and remaining V20–V32 functionality remain pending.


## 2026-10-06 — V27 source closure plus exact selected face inventory composition

Added lockFaceSourcePreparationInventory: configured ICC/unitRGB source scope/closure and full selected face inventory arguments validate and own before SQL; source artifact locks precede all selected face producer/report locks and existing barrier; exact selected discovery repeat and source pin repeat afterward. Extracted pure marshalFacePreparationInventory shared with existing inventory composer; requires common canonical Expected pair and unique roots. No queued admission/commit; complete global policy/protection/defect inventory and auth/version authority remain separate.

Go semanticproposal passed1.102s. Unit verifies source-first/barrier/discovery/source-repeat sequence and duplicate inventory rejection with zero SQL. Physical isolated manual-lock-go-external passed1passed,12deselected115.04s; full two-collection inventory accepts in combined source TX, missing collection rejects, fresh artifact/task inserts remain blocked55P03 while combined locks held. Python requires V27_FACE_SOURCE_INVENTORY_LOCK_OK. Broader policy phantom handling, all source geometry scenarios, resource budgets and atomic queued admission remain pending.


## 2026-10-06 — V27 retained original protection evidence

ReadProtection preserves public API and delegates to private readProtectionPreparationProof, retaining original root/pixels object metadata only after existing original MaskRef/NPY/source/producer origin checks and metadata repeats. Private comparable proof owns scope/binding/bucket and both pins. Scope validator refuses aliases, invalid digests, unversioned/wrong media/bounded-size objects. Recheck guards scope before DB, repeats original root/pixels catalog metadata and canonical producer origin. This does not select required person/defect protection policy or prove coverage by itself; generic protected masks remain insufficient as policy substitutes.

Go semanticproposal passed0.682s. Physical isolated manual-lock-go-external passed1passed,12deselected44.95s. Original proof rechecks; foreign tenant ErrInvalid; expired original protection pixels denies; restored pixels rechecks. Python requires V27_PROTECTION_PREPARATION_RETAINED_PROOF_OK. Source/producer/Params complete protection closure, final transaction locks and required policy discovery/coverage composition remain pending.


## 2026-10-06 — V27 selected protection producer and pins transaction locks

Migration00689 scheduler-only SECURITY DEFINER lock_semantic_proposal_protection_evidence, fixed search_path/photo_fn_owner/PUBLIC revoked. Checks original protection origin before/after selected producer/attempt/results/artifacts/Params/run/workflow FOR SHARE locks, then recomputes exact two original object pins. Go scope-validates comparable retained proof before SQL, canonicalizes stable role order, checks scheduler role and failsErrConflict on SQLfalse. Does not verify original Params bytes nor lock v2_task_inputs/interactive declarations; final declaration writer barrier/recheck and required policy coverage remain mandatory.

Go semanticproposal passed0.793s. Physical isolated manual-lock-go-external passed1passed,12deselected65.54s. Original protection locks; independent writer root/pixels metadata updates block55P03; changed original pixel key failsErrConflict. Python requires V27_PROTECTION_EVIDENCE_LOCK_OK. Task/result/input declaration writer regressions, protection batch barrier composition, required policy discovery/coverage and queued admission remain pending.


## 2026-10-06 — V27 protection batch input declaration barrier

Private lockProtectionPreparationProofs owns/validates1..64 selected protected mask proofs before SQL, deduplicates identical roots/rejects conflicting roots, sorts roots, holds producer/pins locks before existing tenant writer barrier and repeats exact selected pins/origin afterward. Any error requires rollback. Does not imply required policy selection/coverage or complete source/auth admission.

Unit dedup/barrier/exact repeat/fail-fast/invalid-before-SQL checks pass. Go semanticproposal passed0.713s. Physical isolated manual-lock-go-external passed1passed,12deselected49.12s: duplicated original proof composes, concurrent source_artifact_id self-update of actual interactive producer declaration fails55P03 while batch barrier held. Python requires V27_PROTECTION_BATCH_DECLARATION_LOCK_OK. This tests existing interactive branch; v2_task_inputs branch races, multiple roots, pre-barrier deletion race and combined full admission remain pending.


## 2026-10-06 — V27 person coverage retains original protected mask pins

selectPersonProtectionProof now captures original protectionPreparationProof before independently qualified person evidence/coverage streaming and repeats exact original protection catalog/origin afterward, retaining those comparable scope/root/pixels pins in private personProtectionProof. Public SelectQualifiedPersonProtectionCoverage return API remains unchanged. Existing person candidate discovery/report/model/coverage checks remain; no caller-provided count promoted to authority. This extends retention only, not final person transaction composer.

Go semanticproposal passed0.737s. Physical isolated manual-lock-go-external passed1passed,12deselected55.41s. Qualified person coverage discovers exact person root with count1 and retains original protection evidence identical to independent protected-mask proof. Python requires V27_PERSON_COVERAGE_PROTECTION_PROOF_OK. Existing TX person locks do not yet consume the retained protection field; source/person/protection full composer and metadata budget update remain pending. Global required defect/face policy discovery and queued admission remain unfinished.


## 2026-10-06 — V27 person transaction locks consume retained protection

LockPersonProducerInTransaction owns person proof slice and scope-validates all retained protected-mask proofs/count/root associations before SQL. For each distinct proof it now holds exact protected producer/root/pixels pins before person evidence/qualification/barrier and repeats protected evidence after barrier before person candidate repeat. Existing rollback/error contracts remain. Transfer preparation charges additional1024 fixed bytes plus retained protection bucket/object-key/version strings for each source/target person proof against MetadataBytes, conservatively per-item like existing person strings.

Go semanticproposal passed0.859s. Physical isolated manual-lock-go-external passed1passed,12deselected54.57s. A private test plan with one placeholder draft task and two real identical qualified coverage proofs invokes the actual transaction adapter; protection root/pixels and person report concurrent updates block55P03. Python requires V27_PERSON_RETAINED_PROTECTION_LOCK_OK. Placeholder plan tests adapter composition only, not real compiled task admission. Full existing transfer-batch-external later scenarios, whole-batch all-locks-before-barrier ordering, metadata boundary regression and global policy/defect final admission remain pending.


## 2026-10-06 — V27 person batch all-evidence-before-barrier ordering

LockPersonProducerInTransaction now deduplicates owned comparable proofs and deterministically sorts by version/person/protection/policy evidence before SQL lock phases. Every selected protection/person producer and qualification is locked before one tenant writer barrier; all lock families repeat afterward; complete selected person candidate checks follow. Replaces prior per-proof barrier sequence that could acquire later producer locks after barrier. No queued admission or global policy phantom handling is claimed.

Unit two distinct synthetic proof identities verify sorted order, single barrier after all locks, exact repeated arguments, fail-fast across both lock passes/barrier/discovery and invalid later protection proof with zero SQL. Synthetic unit identities test adapter sequencing only. Go semanticproposal passed0.829s. Physical isolated manual-lock-go-external passed1passed,12deselected71.43s: actual qualified single proof/dedup adapter and concurrent person/protection artifact blocks remain valid. Physical multi-version batch scenario and full transfer-batch-external regressions remain pending.


## 2026-10-06 — V27 full transfer regression after person batch locking

The original transfer-batch-external integration case completed successfully: 1 passed, 12 deselected in 149.96s. Command: PHOTO_TEST_MINIO_TMPFS=1 PYTHONPATH=workers:src:tests .venv-platform/bin/python -m pytest tests/integration/test_v27_semantic_intent_publication.py -k transfer-batch-external -q --tb=short. This is the full existing transfer path, rather than the manual-lock-go-external early-return adapter scenario. The isolated PostgreSQL/MinIO fixture exercises the existing person-enabled preparation, compilation and later transaction checks with retained protected evidence and the new all-person-evidence-before-barrier ordering. No production migration or deployment was performed.

Go semanticproposal tests also passed (cached). This closes the previously pending original transfer-batch-external regression. It does not establish public queued semantic-propose admission, complete required policy discovery, multi-version person-lock qualification, retained metadata exact-boundary coverage or hardware qualification. The V20–V32 implementation goal remains incomplete.

Related Go regressions passed: internal/v2input (7.592s), internal/v2draft (4.005s), internal/review (1.533s), internal/v2qualification (cached). These package checks do not replace physical concurrency or full product acceptance.


## 2026-10-06 — V27 retained person metadata boundary regression

The full transfer fixture now independently totals retained document/profile bytes, person expected/evidence/qualification strings, and original protected-mask object metadata for both source and target. It invokes actual PrepareTransferInputs with a one-byte-deficient budget (must return ErrInvalid and nil outputs) and the exact budget (must return the full batch). The Python full-transfer wrapper requires V27_TRANSFER_PERSON_METADATA_BOUNDARY_OK. Go semanticproposal passed in 0.928s. Physical transfer-batch-external verification is running as session 26203; no physical pass is claimed in this entry. No production state changed.


## 2026-10-06 — V27 person metadata exact-boundary physical verification

Session 26203 terminated successfully. Full isolated transfer-batch-external: 1 passed, 12 deselected in 170.15s. The Python wrapper requires the new V27_TRANSFER_PERSON_METADATA_BOUNDARY_OK marker from the actual Go preparation test. Actual original source/target person protection proofs and document/profile bytes accept the independently calculated exact retained budget; one byte less returns ErrInvalid with both result slices nil. This closes the specific retained-person metadata boundary regression recorded above. It does not measure total process heap, aggregate object IO or hardware limits, establish multi-version batch qualification, or complete queued public semantic admission. Go semanticproposal passed in 0.928s before this physical run. Full V20–V32 remains incomplete.


## 2026-10-06 — V27 independent selected parser candidate discovery

Migration 00690 introduces API-only SECURITY DEFINER semantic_proposal_face_parse_preparation_candidates (photo_fn_owner, fixed search_path, PUBLIC revoked). Discovery enumerates canonical face.parse manifests, declared source/detector inputs and same-producer label children, delegating canonical output/attempt/result/expiry/source/profile proof to existing 00681/00682 gates. Results are deterministically ordered and capped at 65 so the private Go reader rejects overflow above 64 rather than silently truncating. Expected profiles are owned before SQL; malformed scopes, operations, digests and output aliases deny. This is selected profile discovery only, not complete global required policy authority or final transaction inventory locking.

The physical fixture asserts its independently discovered parser/detector/labels triple and requires V27_FACE_PARSE_PREPARATION_DISCOVERY_OK. Go semanticproposal passed in 0.898s. Isolated manual-lock-go-external is running as session 53648; physical migration/runtime success is not yet claimed. No production migration or public admission activation was performed.


## 2026-10-06 — V27 selected parser discovery verified; qualified inventory preparation

Session 53648 terminated successfully: isolated manual-lock-go-external passed 1 test, 12 deselected in 52.56s. Migration 00690 installed in the disposable database; the fixture found its exact canonical source-bound parser/detector/labels triple and required V27_FACE_PARSE_PREPARATION_DISCOVERY_OK. This verifies one selected parser candidate, not multi-candidate/overflow/phantom behavior.

Private prepareQualifiedFaceParseInventory now owns expected profiles, discovers the bounded complete selected-profile set, rejects empty sets, verifies each original parser/detector/report/source/NPY through readQualifiedFaceParsePreparationProof, retains typed labels/counts/private evidence per candidate, repeats every proof and exact ordered discovery, and returns nil on every failure. This is preflight only; final transaction inventory locks, global required-policy authority and aggregate IO/metadata budgets remain separate obligations. The physical fixture asserts the complete original single-parser content and rejects a foreign landmark qualification report without returning a partial set. Go semanticproposal passed in 1.317s. Physical inventory verification is running as session 26592; success is not claimed yet.


## 2026-10-06 — V27 parser inventory content verified; transactional exact-set gate

Session 26592 terminated successfully: isolated manual-lock-go-external passed 1 test, 12 deselected in 84.50s. The new complete selected-parser preflight accepted original labels/counts/private proof association and denied a foreign qualification report without partial output. This fixture has one parser; multi-parser IO/phantom/overflow remain unverified.

Migration 00691 adds scheduler-only lock_semantic_proposal_face_parse_inventory with fixed search_path, photo_fn_owner ownership and PUBLIC revoked. It reenters the existing tenant writer barrier, re-discovers the selected profile inventory and compares the entire ordered parser/detector/labels set, rejecting empty/overflow sets. Private Go lockFaceParsePreparationInventory validates and owns all proofs before SQL, requires one common canonical selected expected pair and distinct parser roots, locks all producer/qualification evidence through the existing parser batch adapter, then invokes exact-set gate. Any error requires caller rollback. Global required-policy authority, source closure/auth/version composition and public queued admission remain separate.

Physical fixture tests the actual single-parser gate and direct SQL empty-set denial, requiring V27_FACE_PARSE_INVENTORY_TRANSACTION_LOCK_OK. Physical verification is running as session 67958; no pass is claimed yet. No production migration or deployment occurred.


## 2026-10-06 — V27 parser exact-set transaction verified; source composition

Session 67958 terminated successfully: isolated manual-lock-go-external passed 1 test, 12 deselected in 72.19s. Migration 00691 installed and exact original single-parser inventory was accepted under the scheduler transaction; direct empty inventory was denied. Multi-parser omission/phantom behavior and global policy authority remain unverified.

Private lockFaceParseSourcePreparationInventory composes value-owned qualified original source pins with exact selected parser inventory. It validates source identity/ICC/unit-RGB and every parser inventory input before SQL, locks source artifacts first, then all parser evidence/qualifications plus tenant barrier and exact discovery, then repeats source pins. Caller must roll back errors and still supply complete global required policies, authorization/version scope and final atomic admission. Unit checks cover source-before-parser/barrier/exact-set ordering, exact source argument repeat and duplicate parser inventory rejection before SQL. Physical fixture invokes the combined adapter and requires V27_FACE_PARSE_SOURCE_INVENTORY_LOCK_OK; physical verification remains pending.


## 2026-10-06 — V27 source/parser inventory verified; phantom writer regression

Session 8150 terminated successfully: isolated manual-lock-go-external passed 1 test, 12 deselected in 52.63s, requiring V27_FACE_PARSE_SOURCE_INVENTORY_LOCK_OK. Actual qualified original source closure and exact single-parser inventory compose successfully in one scheduler transaction. This is not full public queued admission.

The physical fixture now holds this combined transaction while an independent owner connection attempts INSERT of a fresh artifact and a fresh parser task cloned from the existing canonical producer. Each write uses a separate transaction, a 250ms local lock timeout and rollback, and must fail with PostgreSQL 55P03. This tests the existing tenant AFTER-mutation barrier against new rows rather than only selected-row updates. It does not construct a complete new qualified parser graph, prove released-barrier writer success or cover every trigger table. Python requires V27_FACE_PARSE_INVENTORY_PHANTOM_WRITER_OK. Physical verification is running as session 11532; no pass claimed yet.


## 2026-10-06 — V27 parser phantom writes verified; release regression

Session 11532 terminated successfully: isolated manual-lock-go-external passed 1 test, 12 deselected in 58.23s. Independent fresh task/artifact INSERTs failed55P03 while the actual source/parser exact inventory transaction held the tenant barrier; the Python wrapper required V27_FACE_PARSE_INVENTORY_PHANTOM_WRITER_OK. These insertions are inventory writer probes, not a complete new qualified parser graph.

The fixture now reuses both exact writer statements after explicitly rolling back the holding transaction. Each independent writer transaction retains250ms lock_timeout, must insert exactly one row, then rolls back so no probe rows persist. V27_FACE_PARSE_INVENTORY_WRITER_RELEASE_OK is required. This distinguishes an effective temporary barrier from unrelated insertion failure or a leaked lock. Physical verification is running as session53240; success is not claimed yet. All production state remains unchanged; global policy/multi-parser races and final queued admission remain pending.


## 2026-10-06 — V27 parser writer release verified; role isolation regression

Session53240 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in57.23s. After holding transaction rollback, both exact independent INSERT probes succeeded with one affected row and were rolled back; V27_FACE_PARSE_INVENTORY_WRITER_RELEASE_OK was required.

The fixture now checks effective EXECUTE ACLs for parser discovery00690 and exact inventory lock00691 across API/scheduler/worker/publisher/portal/auth/gateway/maintenance/semantic-admission roles. Discovery permits API only and locking permits scheduler only; actual denied-role NULL-argument calls must raise InsufficientPrivilege in rolled-back transactions. It verifies photo_fn_owner ownership, SECURITY DEFINER and fixed search_path from pg_proc. These checks run before fixture mutation and do not alter privileges. Physical verification is running as session90932; result pending. Full global policy discovery, multi-parser races and queued semantic admission remain incomplete.


## 2026-10-06 — V27 parser function role isolation verified; expiry/exact pins regression

Session90932 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in57.01s. Effective EXECUTE privileges and actual denied-role calls confirm API-only selected parser discovery and scheduler-only inventory locking across the tested service roles; owner/SECURITY DEFINER/fixed search_path checks passed.

The fixture additionally expires the original labels artifact, requires selected discovery to become empty, restores expiry and requires the identical triple. It also directly invokes the locked inventory SQL gate with a changed labels SHA in the otherwise original canonical inventory and requires false. Python requires V27_FACE_PARSE_DISCOVERY_EXPIRY_OK and V27_FACE_PARSE_INVENTORY_EXACT_PINS_OK. Physical verification runs as session11417; pending. These checks do not establish multi-parser omitted subsets, IO-time phantom races, full global policy authority or queued admission.


## 2026-10-06 — V27 parser discovery expiry and exact inventory pins verified

Session11417 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in66.14s. Expiring original parser labels removed the candidate from discovery; restoring expiry returned the identical triple. Direct scheduler inventory gate denied the otherwise original inventory with changed labels SHA. Required expiry/exact-pin markers, service-role isolation, writer blocking and post-rollback release checks all passed in this fixture.

Current implementation still has separate source+face and source+parser transaction composers. A future complete semantic admission composer must validate all families before SQL and lock every selected producer/report/source family before the common tenant barrier; naively invoking these separate composers in succession would acquire later family locks after the first barrier. This remaining composition obligation is not closed by the independent successful tests. Global required-policy authority, complete defect preparation/publication, queued admission and V20–V32 product completion remain unfinished.


## 2026-10-06 — V27 shared source/face/parser transaction composer

Private lockFaceAndParseSourceInventory owns both selected inventories, validates source scope/color/canonical pins and both inventories/evidence serialization before SQL, sorts each family, then locks source plus every face/parser producer and qualification before one common tenant barrier. It repeats the same full lock pass after the barrier and invokes both exact inventory SQL gates. Gate functions reenter the already-held advisory barrier; they do not introduce a different barrier. Caller rollback is mandatory on error. This composes selected source/face/parser families only: manual/person/protection/global-policy authority and final queued admission remain obligations.

Synthetic unit checks prove27-call ordering for one face/one parser, exact argument repeat across both12-call passes, fail-fast at every call and invalid later parser with zero SQL. Go semanticproposal passed0.832s. Physical fixture composes actual qualified source, two discovered face collections and one parser in a scheduler transaction, requiring V27_FACE_AND_PARSE_SOURCE_INVENTORY_LOCK_OK. Isolated physical verification runs as session59574; no pass claimed yet. No production migration or activation.


## 2026-10-06 — V27 shared source/face/parser composer physically verified

Session59574 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in76.72s. Actual qualified source closure, two discovered face collections and one parser compose through lockFaceAndParseSourceInventory; the required V27_FACE_AND_PARSE_SOURCE_INVENTORY_LOCK_OK marker passed. This verifies selected families only, not global policy/manual/person/protection completeness or queued public admission.

The fixture now also calls the combined adapter with one discovered face collection omitted (must ErrConflict) and altered original parser labels object key (must ErrConflict). Every probe rolls back its transaction. Python requires V27_FACE_AND_PARSE_INVENTORY_REJECTION_OK. Physical verification is running as session93174; result pending.


## 2026-10-06 — V27 combined inventory rejection verified; selected manual composition

Session93174 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in60.53s. The actual combined source/face/parser adapter denied an omitted discovered face collection and changed parser labels object key, requiring V27_FACE_AND_PARSE_INVENTORY_REJECTION_OK.

Shared private lockSelectedSourceInventories now supports original selected manual closures. lockFaceParseManualSourceInventory requires1..64 manual proofs; all scope checks and canonical evidence serialization happen before SQL, pointer-backed vectors become owned strings, identical roots deduplicate and conflicting roots deny. Sorted manual evidence locks join source/face/parser locks before the same common barrier and repeat afterward; neither manual batch adapter nor a second early barrier is invoked. Existing source/face/parser wrapper behavior is preserved. Manual inventory completeness and global policies/person/protection/final admission remain separate obligations. Unit29-call ordering/dedup/exact-repeat and invalid-manual-before-SQL checks pass; Go semanticproposal passed1.036s. Physical selected manual composition verification runs as session30420, requiring V27_FACE_PARSE_MANUAL_SOURCE_LOCK_OK; result pending.


## 2026-10-06 — V27 selected manual shared composition verified; denial/recovery probes

Session30420 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in64.98s. Actual retained original manual closure joined source/two face collections/parser in the common adapter before its shared barrier; V27_FACE_PARSE_MANUAL_SOURCE_LOCK_OK was required. This does not establish complete manual inventory discovery.

The fixture now invokes this combined adapter with changed manual Params object key (must ErrConflict), expired original manual root (must ErrConflict), then restored expiry (must accept). Every scheduler probe rolls back. Python requires V27_FACE_PARSE_MANUAL_REJECTION_OK. Physical verification runs as session32329; result pending. No production state modified. Required policy/person/protection completeness, metadata/IO budgets and queued semantic admission remain unfinished.


## 2026-10-06 — V27 combined manual denial and restoration verified

Session32329 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in86.86s. The combined source/face/parser/manual adapter denied changed original manual Params object key and expired manual root, then accepted restored original evidence. Python required V27_FACE_PARSE_MANUAL_REJECTION_OK. This closes these specific combined-adapter regressions, not complete manual inventory discovery.

Inspection before adding person evidence to the common composer found that existing LockPersonProducerInTransaction prevalidates retained protection scope/count associations but passes owned Expected/Evidence/Qualification strings to SQL without a pure canonical person-pin shape check first. A shared composer must validate all later person inputs before its initial source/other-family SQL; existing per-family person SQL rejection is insufficient for that zero-SQL-on-invalid-input invariant. Next implementation must establish this private canonical person prevalidation and retain actual original pins; do not invent plan/task authority to enable public admission. Global policy/defect completeness and public queued admission remain unfinished.


## 2026-10-06 — V27 private person proof pre-SQL shape/scope validation

validPersonPreparationScope validates original retained protection scope/association/count, source/person/child/protection/qualification artifact identity separation and digests, bounded canonical Expected/Evidence/Qualification JSON, mask.person1/backend, exactly PersonParams+PersonReport pins, qualification report association, bucket/object key/version/JSON bounds/backend before SQL. LockPersonProducerInTransaction now applies it to every owned proof before current_user or any evidence lock. Original bytes/qualification and SQL canonical producer/candidate gates remain independent obligations; this shape check is not an authority certificate. Synthetic sequencing fixtures now carry typed canonical person pin shapes rather than empty strings. Go semanticproposal passed0.820s. Physical original qualified person adapter regression runs as session10374; result pending. Shared person composition and complete global required policy authority remain unfinished.


## 2026-10-06 — V27 canonical person prevalidation physically verified

Session10374 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in57.51s. Actual original qualified person coverage proofs passed validPersonPreparationScope and the existing person/protection scheduler lock adapter. This fixture's plan remains the documented placeholder adapter test, not complete queued workflow admission.

Additional unit regressions mutate the later person proof in a two-proof plan: noncanonical/unknown-field expected JSON, missing/noncanonical evidence, mismatched qualification report identity, aliased child, null version, foreign bucket, backend mismatch and malformed qualification. Every case requires ErrInvalid and zero SQL calls, including no current_user query. These tests establish the intended pre-SQL invalid-shape boundary; original scientific qualification, live SQL authority and global policy completeness remain separate. Shared person admission composition remains unfinished.


## 2026-10-06 — V27 selected person/protection joins shared transaction

Private lockSelectedPersonSourceInventory requires1..64 privately qualified person coverage proofs, validates all canonical person/protection scopes against the same exact source binding before SQL, owns comparable values, deduplicates identical roots and denies conflicting roots, sorts person roots. Protection producer/pins, person evidence and qualification locks join the shared source/face/parser/manual lock pass before its common tenant barrier and repeat afterward. Exact person candidate matches are repeated after both face/parser exact inventory gates. No nested person adapter/early barrier is invoked and no task plan authority is manufactured. Public queued admission/global required-policy discovery/manual inventory completeness remain separate obligations.

Physical fixture composes original source, two faces, parser, manual mask and qualified person coverage, requiring V27_PERSON_SHARED_SOURCE_INVENTORY_LOCK_OK. Physical verification has been launched; no physical pass claimed. Existing Go package checks cover preceding adapters; dedicated shared-person ordering/failure boundary tests remain required.


## 2026-10-06 — V27 shared person composition physically verified; ordering checks

Session41978 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in56.79s. Original qualified person/protection evidence joined source/two faces/parser/manual in the shared transaction without a fabricated task plan; required V27_PERSON_SHARED_SOURCE_INVENTORY_LOCK_OK passed. This selected evidence composer still does not prove full required-policy/manual inventory discovery or queued admission.

Dedicated synthetic shared-person unit checks verify38-call ordering (17 family calls before common barrier, identical17-call repeat, face/parser inventory gates then person candidate repeat), duplicate-person deduplication, fail-fast on every call and invalid later person evidence with zero SQL. Initial compilation exposed a missing v2input test import; it was corrected before rerunning. These tests verify adapter sequencing, not original model scientific qualification.


## 2026-10-06 — V27 shared person live qualification denial regression

The physical fixture now repeats actual lockSelectedPersonSourceInventory after disabling the selected person qualification policy and after expiring the original qualification report; each must return ErrConflict despite retained valid preflight proof. After restoring both, the same original proof must be accepted. Every scheduler transaction rolls back and mutations remain confined to the disposable fixture. Python requires V27_PERSON_SHARED_QUALIFICATION_REJECTION_OK. Physical verification runs as session14328; no pass claimed yet. This tests selected qualification liveness, not global required-policy phantom completeness or final queued admission.


## 2026-10-06 — V27 shared person live qualification verified

Session14328 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in92.05s. Shared source/face/parser/manual/person composition denied disabled selected person policy and expired original person qualification report and accepted their restoration; V27_PERSON_SHARED_QUALIFICATION_REJECTION_OK passed. This is selected live authority verification only.

Inspection confirms v2_qualification_policies is global (no tenant_id), so tenant inventory writer triggers do not protect new policy-row phantoms. Holding selected policy FOR SHARE does not establish complete required policy discovery or freeze additions. A dedicated global policy authority/discovery barrier remains required before public queued admission. Added unit rejection cases require conflicting same-root person proofs and foreign source-version bindings to fail with zero SQL. Full V20–V32 remains incomplete.


## 2026-10-06 — V27 global qualification policy writer barrier primitive

Migration00692 adds an exclusive transaction advisory barrier to all INSERT/UPDATE/DELETE/TRUNCATE statements on global v2_qualification_policies through a SECURITY DEFINER AFTER STATEMENT trigger. Scheduler-only lock_semantic_proposal_qualification_inventory validates live editor/tenant scope, acquires the matching shared transaction barrier and repeats authorization. Owner photo_fn_owner, fixed search_path and PUBLIC revoke are explicit. Shared reader locks allow concurrent independent admissions; writers serialize. Deadlock requires full caller rollback/retry. No broad direct role grants or policy activation are added.

This primitive is not yet integrated into the common composer and does not discover or compare required policy inventory; callers must re-discover exact policies after locking. Physical fixture separately holds the authenticated scheduler barrier and requires an independent existing-policy update to fail55P03; service ACL checks include the new function. INSERT phantom, released writer, exact policy discovery and full composer integration remain pending. Physical verification runs as session98122; no pass claimed. Production migrations not applied.


## 2026-10-06 — V27 global policy barrier verified; INSERT phantom/release probes

Session98122 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in144.52s. Migration00692 installed in the disposable database; authenticated scheduler shared global barrier blocked an independent policy UPDATE55P03. Function owner/SECURITY DEFINER/search_path/role ACL checks passed.

The fixture now uses both UPDATE and fresh policy INSERT (new operation identity, copied valid policy fields). Each must fail55P03 under the barrier. After holding transaction rollback, identical statement shapes must affect exactly one row and are rolled back. Python requires V27_GLOBAL_POLICY_PHANTOM_AND_RELEASE_OK. This proves writer mechanics only, not required policy discovery or common admission integration. Physical verification runs as session36706; result pending. Production policies/migrations remain unchanged.


## 2026-10-06 — V27 policy phantom/release verified; bounded operation catalog snapshot

Session36706 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in136.55s. UPDATE and fresh policy INSERT both failed55P03 while shared global barrier held; both statement shapes affected one row after release and were rolled back. V27_GLOBAL_POLICY_PHANTOM_AND_RELEASE_OK passed.

Migration00693 adds API-only complete policy-row enumeration for explicit operations mask.person/face.detect/face.landmarks/face.parse/defects.propose/semantic.transfer, including disabled rows, ordered by UUID with65 overflow sentinel. Scheduler-only exact snapshot gate acquires global barrier then compares all rows, denying empty/>64/>4MiB snapshots. New private reader bounds the returned JSON. This proves stability of that explicit operation catalog, not required-model selection, coverage of any other operations, or scientific qualification. It is not yet integrated with the shared evidence composer. Physical original snapshot/empty denial and role ACL checks run as session90227; pending. No production migration applied.


## 2026-10-06 — V27 operation policy snapshot verified; stale nonempty regression

Session90227 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in72.96s. Migration00693 installed; exact original operation catalog snapshot accepted, empty snapshot denied, API/scheduler effective grants and actual denied-role calls passed. This snapshot covers the explicit six-operation allowlist, including disabled rows, not every system policy or required-model selection.

The fixture now changes selected person admission_enabled after releasing the original global barrier, invokes the snapshot gate in a fresh scheduler transaction with the original nonempty snapshot and requires false; restores the policy and requires the same original snapshot true. Each probe rolls back. Python requires V27_GLOBAL_POLICY_STALE_SNAPSHOT_OK. Physical verification runs as session49577; pending. Integration with shared evidence composer and required-model matching remain unfinished.


## 2026-10-06 — V27 stale snapshot verified; shared policy/evidence composition

Session49577 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in85.45s. Original nonempty catalog snapshot was denied after person admission policy changed and accepted after restoration. V27_GLOBAL_POLICY_STALE_SNAPSHOT_OK passed.

Private qualificationPolicySnapshot retains owned catalog JSON with tenant/project/actor/session. lockPolicySelectedSourceInventory checks scope/bounds before SQL and joins the complete selected evidence composer. All source/face/parser/manual/person producer locks precede the global exact policy snapshot gate; that gate holds the shared global barrier. Tenant barrier and full evidence repeat follow; exact face/parser/person gates and final policy snapshot repeat run while both barriers remain held. Existing private adapters retain prior behavior without policy snapshots; public admission remains unavailable. Required-model selection, unlisted-operation completeness and final atomic queued admission are still obligations. Physical combined original policy/evidence regression runs as session42301; pending. Dedicated shared-policy ordering/invalid-scope unit checks remain to add.


## 2026-10-06 — V27 shared global policy/evidence composition verified

Session42301 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in69.09s. Original scoped policy snapshot joined source/two face collections/parser/manual/person/protection in lockPolicySelectedSourceInventory; required V27_POLICY_SHARED_SOURCE_INVENTORY_LOCK_OK passed. This is explicit operation catalog stability and selected evidence composition, not required-model matching or queued public admission.

Synthetic unit checks now verify40-call composition:17 evidence calls before global snapshot gate, tenant barrier afterward, identical17 evidence repeats, final exact global snapshot argument repeat. Failures at every call stop further SQL. Empty/null/object/malformed/65-entry snapshots and foreign session deny before SQL. Synthetic policy entries test ordering/bounds only; actual row equality is proved by the separate physical SQL gate.


## 2026-10-06 — V27 server-required qualification policy selection

Private selectQualificationPolicies decodes bounded explicit-operation snapshot rows and selects DBA admission-enabled version1 policies for server-required operations. Missing/ambiguous/disabled/unlicensed/invalid-backend/digest/issuer/check/evidence rows deny with nil output. RequiredChecks are sorted; qualification fixture/calibration/evidence sets are owned, bounded and duplicate-checked. No newest-model selection or synthetic fallback. Exact original report verification, scope validation and snapshot stability remain separate requirements. Unit cases cover required/duplicate/unknown/missing operation, policy gates and malformed/duplicate sets. Physical real person policy identity/model/dataset selection regression launched; no physical pass claimed. This component is not yet enforced as expected-policy matching across all shared evidence families.


## 2026-10-06 — V27 required policy physical probe compile repair

Session27508 terminated with failure:1 failed,12 deselected in21.68s. The new integration policy selection assertion referenced personExpected outside its lexical scope, so the tagged Go test did not compile; no physical selection success was established. The assertion now decodes Expected from the actual retained qualified person proof instead, preserving its intended model/dataset/issuer comparison. Tagged integration compile-only check was launched before the physical retry. The same manual-lock-go-external scenario is rerunning as session64326; pending. Prior physical shared evidence/policy results are unchanged; server selection matching across all evidence families remains unfinished.


## 2026-10-06 — V27 real required selection verified; full Expected comparison

Session64326 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in75.49s after the documented integration compile repair. Real person policy selection matched retained model/dataset/issuer; V27_REQUIRED_POLICY_SELECTION_OK passed.

sameQualificationPolicyExpected owns inputs, normalizes RequiredChecks order (duplicate checks deny) and empty allowlist maps, then compares the entire Expected structure canonically. matchSelectedEvidencePolicies selects active DBA policies for landmarks/detect/parse/person and compares every selected evidence Expected, including all model/runtime/device/dataset/issuer/check/fixture/calibration/evidence/license fields. Original report/content and inventory locks remain separate. This matcher is not yet enforced by the shared admission adapter. Unit cases cover runtime/issuer/fixture/evidence/check differences; physical original full-family comparison runs as session90471, requiring V27_SELECTED_EVIDENCE_POLICY_MATCH_OK; pending. Defect/transfer expectations and all-system required-model completeness remain unfinished.


## 2026-10-06 — V27 full Expected matching verified and required before SQL

Session90471 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in69.13s. Original face/parser/person Expected structures fully matched their DBA-selected admission policies; V27_SELECTED_EVIDENCE_POLICY_MATCH_OK passed.

lockPolicySelectedSourceInventory now requires matchSelectedEvidencePolicies before entering the shared SQL composer. Unit sequencing fixture carries complete synthetic active policies/Expected rather than ID-only snapshots; foreign person runtime Expected must return ErrUnqualified with zero SQL. Physical original composition plus mismatched Expected preflight denial is rerunning; no new pass claimed yet. This enforces selected landmark/detect/parse/person policies only. Defect/transfer policy matching, complete manual discovery, aggregate budgets and queued public admission remain unfinished.


## 2026-10-06 — V27 mandatory evidence policy matching verified; execution policy matcher

Session72295 terminated successfully: isolated manual-lock-go-external passed1 test,12 deselected in75.82s. Mandatory selected evidence Expected matching in the policy-aware composer accepted original proofs and rejected foreign person runtime Expected before SQL; required V27_REQUIRED_POLICY_PREFLIGHT_DENIAL_OK passed.

Private matchProposalExecutionPolicies additionally selects both defects.propose and semantic.transfer admission policies and compares full caller-server Expected structures, denying missing defect policy or foreign transfer device profile. Synthetic unit tests cover these cases and original full pair. This helper is not yet integrated into shared transaction admission; original execution qualification reports/calibration/cost authority remain separate. Initial write failed due filesystem sandbox access; access was refreshed and the code saved successfully. No production migration/deployment. Full V20–V32 remains unfinished.


## 2026-10-06 — V27 defect/transfer policy matching in shared adapter

Private lockProposalPolicySourceInventory now validates scoped policy snapshot, requires full defects.propose and semantic.transfer Expected equality with active DBA policies, then delegates to the policy/evidence shared composer (which independently requires selected face/parser/person policy matches before SQL). Missing execution policies or foreign transfer model return ErrUnqualified before any SQL. It creates no task plan, queue or activation authority. Original defect/transfer qualification reports/calibration and measured execution costs remain separate required obligations.

Synthetic unit fixture supplies all six complete active policies and verifies successful40-call composition, missing execution policy rejection with zero calls and foreign transfer Expected rejection with zero calls. This is adapter/policy logic verification, not physical execution qualification. Production admission remains unavailable until remaining obligations are implemented.

### 2026-10-06 — V27 retained original transfer qualification preparation

`qualification_read.go` now retains a private, comparable scoped proof after original report/calibration reads, document verification and repeated authorized metadata lookup. The public VerifyQualification delegates to the same path. Expected policy maps/slices are copied and canonicalized (4 MiB bound); incomplete authenticated scope or cancellation returns no proof. The proof retains exact calibration/report object versions and scoped IDs, but is not a lock, admission receipt or qualification authority by itself. Transaction composition, original defect qualification and cost gates remain incomplete.

Validation: `go test -tags integration ./internal/semanticproposal` passed, including cancellation and all four missing-scope pre-IO rejection cases. This invocation compiles integration-tagged code but does not establish physical PostgreSQL/MinIO positive preparation verification. No production migration or deployment performed.

### 2026-10-06 — V27 exact retained transfer qualification lock

Added forward-only migration `00694_v27_transfer_qualification_evidence_lock.sql`: scheduler-only SECURITY DEFINER function, fixed search_path, explicit PUBLIC revoke. It invokes the existing live semantic.transfer qualification gate (holding policy/qualification/report/calibration row locks), then compares the exact retained ordered calibration/report metadata including bucket, key, version and bytes. Added private Go adapter with pre-SQL scope/policy/object bounds validation and caller-owned transaction semantics. It does not create a workflow or authorize queue admission. Audit-import authority mutation races and complete proposal composition remain separate work.

Validation: tagged Go package tests passed (1.222s). Physical isolated PostgreSQL/MinIO `manual-lock-go-external` passed (170.07s), applying all migrations including 00694 and preserving the existing shared source/evidence contour. This physical case does not exercise a positive invocation of the new retained transfer qualification function; dedicated positive/revocation/metadata-change/ACL probes remain required. No production migration/deployment.

### 2026-10-06 — V27 physical retained transfer qualification probes

Added direct physical Go adapter probes using the fixture's original verified report/calibration and trusted semantic.transfer locks: original proof accepted in a scheduler transaction; changing the retained object version independently for calibration and report returns ErrConflict. Python requires `V27_TRANSFER_QUALIFICATION_RETAINED_LOCK_OK` in the early shared-lock scenario.

Tagged Go tests passed (1.827s). Initial full transfer-batch-external physical attempt failed at the existing 180-second Go context deadline during restored skin-policy verification, before these new probes (pytest 209.23s); not counted as success. Moved the same original-document probes to the early shared-lock phase without removing prior assertions or increasing timeout. Physical manual-lock-go-external then passed (103.67s), including the required new marker. Revocation, new-function ACL checks, import-audit locking and full admission composition remain incomplete.

### 2026-10-06 — V27 retained transfer qualification revocation and ACL

Physical tests now disable the selected semantic.transfer policy admission flag, require ErrConflict for the retained original-document proof, restore the flag, and require success. Actual SET LOCAL ROLE calls to the new SQL function require SQLSTATE 42501 for photo_api, photo_worker, photo_publisher, photo_portal, photo_auth, photo_gateway, photo_maintenance and photo_semantic_admission. Scheduler positive invocation remains covered by the original proof test. Python requires both new revoke/ACL markers.

Validation: after fixing the test's missing pgx import, tagged Go tests passed (0.885s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (74.01s). This proves selected-policy revoke/recovery and the listed role denials; import-audit mutation locking and integration into complete atomic proposal admission remain unfinished. No production deployment or migration applied.

### 2026-10-06 — V27 transfer import authority row locking

Forward-only 00695 replaces the retained transfer qualification evidence gate, retaining existing authorization/policy/object checks and acquiring FOR SHARE on one exact matching tenant/qualification/policy import audit row. Missing import confirmation returns false. A matching authority row remains locked until the caller transaction ends; this specifically strengthens the retained semantic.transfer gate, not every qualification function. No new role grants introduced.

Tagged Go tests passed (1.158s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (89.72s), with required marker V27_TRANSFER_QUALIFICATION_IMPORT_LOCK_OK: independent owner UPDATE and DELETE of matching imported qualification audit rows both return lock timeout SQLSTATE 55P03 while the scheduler proof gate holds the transaction. Previous positive, stale-version, policy revoke/recovery and denied-role probes also remain required. Lock release writer probes and other qualification families, complete discovery/atomic admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 shared original transfer qualification composition

Extracted pure bounded marshalTransferQualificationPreparation preflight from the lock adapter. Added private lockTransferQualifiedSourceInventory: scoped original transfer proof must match the selected active semantic.transfer policy; selected face/parser/person policies also match before SQL. Common selected-source composer now has an internal optional transfer lock hook, invoked in both lockAll passes before and after global/tenant inventory barriers. Existing callers use the unchanged wrapper with no hook. Locks include original report/calibration objects and matching imported authority row through 00695. No defect original qualification, measured cost gate or queued run is supplied by this adapter.

Validation: tagged Go package tests passed before and after fixture integration; physical PostgreSQL/MinIO manual-lock-go-external passed (83.77s) with required V27_TRANSFER_QUALIFIED_SHARED_SOURCE_LOCK_OK marker. Fixture executes the common composer with retained original report/calibration, source, two face collections, parser, manual and selected person evidence. Restored context-error priority in standalone lock adapter afterwards and reran tagged Go tests. Failure-position sequencing tests for the new hook and full admission remain outstanding.

### 2026-10-06 — V27 shared transfer failure sequencing

Extended synthetic transaction tests for lockTransferQualifiedSourceInventory: 44 calls, original transfer qualification before both inventory barriers, exact same transfer pin arguments repeated after barriers, and every failure position 1..44 must stop SQL immediately. A null object version is rejected with zero SQL calls. Fixture uses explicitly synthetic policy/object metadata; it does not attest original report bytes, scientific quality, rollback execution, queue admission or hardware capacity. Original positive composition remains covered by the preceding physical fixture.

Validation: `go test -tags integration ./internal/semanticproposal` passed (0.937s). No production migration/deployment. Original defect qualification, measured cost gates, full atomic workflow admission and the remaining V20–V32 deliverables remain unfinished.

### 2026-10-06 — V27 dedicated original defect qualification reader

Added forward-only 00696 dedicated API-only semantic_proposal_defect_qualification_object: operation defects.propose version 1, authorized tenant/project editor, exact report ref, active licensed admission policy, matching imported qualification/audit evidence and live immutable report metadata. Existing face reader operation allowlist is unchanged. Added private readDefectQualificationPreparation: own Expected, canonical bounded policy, original pinned report bytes verified with v2qualification.Verify, repeated live catalog metadata, scoped retained proof only on complete success. This is not a final transaction lock or queue admission authority.

Tagged Go tests passed (0.942s), including cancellation, foreign operations and missing tenant rejecting with no proof/IO. Physical PostgreSQL/MinIO manual-lock-go-external passed (59.23s), applying 00696 and preserving prior source/transfer evidence checks. This does not test a positive dedicated defects.propose original-report read; a separate imported report fixture, revoke/content/race/ACL probes and transaction composition remain required. No production migration/deployment.

### 2026-10-06 — V27 physical original defect qualification preparation

Integration fixture now separately imports a synthetic contract-valid defects.propose qualification report with exact original MinIO object bytes and active CPU policy. Go reads the original report through the dedicated 00696 API reader, verifies qualification bytes and repeats catalog metadata, requires empty proof after admission flag revocation, restores policy and requires identical retained proof, and rejects a face.landmarks report for defects.propose. Python requires V27_DEFECT_ORIGINAL_QUALIFICATION_PREPARATION_OK. The fixture is not actual model/RTX3060 scientific qualification.

Tagged Go tests passed (1.040s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (72.78s), including all previous shared source/transfer evidence markers. Defect report content/expiry races, reader ACL checks, retained defect transactional locks and complete admission/cost gates remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 retained defect qualification transaction lock

Added forward-only 00697 scheduler-only lock_semantic_proposal_defect_qualification, fixed SECURITY DEFINER/search_path, explicit PUBLIC revoke. Exact retained report metadata must match the live licensed active defects.propose policy and tenant/project imported qualification; FOR SHARE locks policy/qualification/report and a matching import audit row. Added private Go adapter with bounded canonical Expected and scoped report metadata validation before SQL. Caller must roll back failures; adapter does not queue work.

Tagged Go tests passed (1.181s). Physical PostgreSQL/MinIO manual-lock-go-external passed (66.64s) with required V27_DEFECT_ORIGINAL_QUALIFICATION_LOCK_OK marker: original verified proof succeeds and stale retained report version returns ErrConflict. Dedicated concurrent defect authority writer/ACL/revoke probes, shared composition, measured cost gates and full admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 combined execution qualification composition

Extracted pure marshalDefectQualificationPreparation preflight and added private lockExecutionQualifiedSourceInventory. Both retained original defect/transfer proofs are scoped/validated before SQL and must match selected active execution policies; selected face/parser/person policies also match. Defect then transfer authority locks run in both common lockAll passes, before and after inventory barriers. The existing selected source/evidence checks remain in the same caller transaction. Adapter freezes qualification authority, not measured cost approval, original defect producer Params validation, full discovery or queued workflow admission.

Tagged Go tests passed (1.036s). Physical PostgreSQL/MinIO manual-lock-go-external passed (62.94s), with required V27_EXECUTION_QUALIFIED_SHARED_SOURCE_LOCK_OK marker: both original qualification proofs compose with verified source, face/parser/manual/person evidence and policy inventory. Existing positive/stale-version/revoke/transfer ACL checks remain required. Combined failure-position tests and full admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 combined execution qualification failure sequencing

Synthetic tests now cover the combined adapter's 48 SQL calls: defect then transfer qualifications before inventory barriers, exact repeated arguments for both after barriers, and every failure position 1..48 stops immediately. Foreign session, null defect report version, oversized defect report metadata, malformed retained Expected and invalid later transfer proof all reject before any SQL. Fixtures explicitly test sequencing/shape only; they do not prove rollback execution, original scientific qualification or queue admission. Original combined proof composition remains covered by the preceding physical scenario.

Validation: tagged Go package tests passed (1.118s). No production migration/deployment. Measured cost gates, complete admission and remaining V20–V32 requirements remain unfinished.

### 2026-10-06 — V27 measured transfer resource lower bounds

Retained transfer qualification proof now includes Metrics from the same original verified Report; document verification returns that parsed report without re-reading/re-parsing it. Public VerifyQualificationDocuments signature is preserved. PrepareTransferInputs reads the retained qualification proof and rejects installed additional RAM/VRAM/scratch costs below ceil(original report peaks). Bounds reject negative, NaN, infinite and unsafe values. This is a conservative lower bound, not proof of capacity across resolutions, trusted measured-cost installation, aggregate concurrency, GPU quality or complete admission. Zero fixture measurements remain contract-valid and convey no hardware capacity evidence.

Validation: tagged Go package tests passed, including each resource one byte below its rounded bound and malformed measured values. Physical transfer preparation regression has not been rerun for this change; required before final admission integration. No production migration/deployment. Full V20–V32 objective remains incomplete.

### 2026-10-06 — V27 measured transfer preparation physical regression

Isolated PostgreSQL/MinIO transfer-batch-external passed (145.67s) after original verified report Metrics were retained and compared to installed additional RAM/VRAM/scratch lower bounds. This full scenario covers preparation, original object reads, person-protection/metadata budget boundaries, publication and existing workflow plan checks. Required existing markers remain enforced. Fixture report resource peaks are zero; this proves compatibility of the preparation path, not hardware capacity or rejection of nonzero understated report costs. Unit tests cover nonzero rounded resource lower bounds. A physical nonzero-report cost rejection probe and trusted cost installation, measured resolution scaling, aggregate scheduling and complete public admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 verified original report resource metrics binding

Extended original qualification document test with nonzero fractional RSS/scratch and integer VRAM peaks in the canonical report. Exact updated report SHA verification returns identical Metrics; rounded installed costs pass, one-byte-understated VRAM rejects. Supplying changed report bytes under the old SHA returns error and no parsed report/checks, so unverified metrics cannot reach the resource lower-bound gate through this helper. This is original-byte unit verification, not physical qualification import, GPU measurement or complete capacity proof.

Tagged Go package tests passed (1.271s). Physical nonzero report preparation denial, trusted cost installation, resolution scaling and full admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 physical nonzero measured transfer cost rejection

Qualification fixture supports optional resource metrics; only transfer-batch-external injects nonzero synthetic original report peaks (RSS 101, VRAM 202, scratch 303 bytes). The report is uploaded as an immutable MinIO version, hashed and imported through existing qualification authority. Go preparation requires ErrInvalid with nil inputs/documents when installed VRAM is 201, then proceeds with exact covering costs. Python requires V27_TRANSFER_MEASURED_COST_DENIAL_OK. Other fixture modes retain prior defaults. These numbers are test vectors, not hardware measurements.

Tagged Go tests passed (1.371s). Full isolated PostgreSQL/MinIO transfer-batch-external passed (175.14s), including the new denial marker and existing metadata/protection/publication assertions. Trusted cost installation, resolution scaling, aggregate resource admission and public complete workflow remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 retained qualification metadata accounting

PrepareTransferInputs now charges retained original transfer qualification metadata before source processing: canonical Expected, both original object bucket/key/version strings and 1024-byte structure reserve. Existing per-item/person/protection charges remain. Physical exact-boundary test independently adds this evidence metadata to its expected budget, then requires one-byte-short denial with nil outputs and exact-budget success. The budget is conservative metadata accounting, not a measured heap or total process concurrency bound.

Tagged Go tests passed (1.327s). Full isolated PostgreSQL/MinIO transfer-batch-external passed (200.68s total pytest time), including updated exact metadata boundary and nonzero measured VRAM denial. No test deadlines increased or assertions removed. Trusted capacity installation, aggregation and full public workflow admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 transfer proof retained through workflow compilation

PrepareTransferInputs now retains one private original qualification proof shared by prepared task inputs (already charged once in the metadata budget). Transfer compiler requires consistent proof presence across the batch, exact tenant/project/actor/session and frozen qualification pin, and identical proofs for every target. WorkflowPlan receives its own comparable proof copy, not an alias of preparation. Legacy internal scaffold with all proofs absent remains available, not public admission authority.

Tagged Go tests passed (1.151s). Full isolated PostgreSQL/MinIO transfer-batch-external passed (159.49s) with required V27_TRANSFER_PLAN_QUALIFICATION_OWNERSHIP_OK: compiled plan retains the original proof and mutation of prepared proof actor does not change the plan copy. Existing cost, metadata and person-protection checks remain required. Durable serialization/recovery and final admission lock integration remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 plan-retained transfer authority in registration

LockQualificationInTransaction now routes plans containing a retained original transfer proof through the exact object/import-authority lock adapter. Frozen config pin must equal retained proof pin; Expected is taken from privately verified retained bytes and scoped proof validation repeats tenant/project/actor/session. Registration and admission wrappers already call this shared gate, so prepared plans now lock exact original report/calibration metadata and import confirmation. The all-proof-absent legacy internal scaffold still uses its original policy gate; this does not make it public queue authority. Final complete source/defect discovery and admission remain outstanding.

Tagged Go tests passed (1.404s). Full isolated PostgreSQL/MinIO transfer-batch-external passed (177.57s), preserving prepared proof ownership, measured cost rejection, metadata boundaries and registration/replay checks with the stronger gate. Dedicated malformed plan retained-proof rejection and durable recovery remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 qualification plan scope rejection

LockQualificationInTransaction now rejects cancellation first and explicitly binds plan tenant/actor plus frozen config project/auth-session to the authenticated request before either legacy or retained-proof SQL path. Added synthetic tests for foreign tenant/actor, config project/session, retained-vs-frozen policy pin mismatch and cancellation; all require zero SQL calls. These tests prove pure rejection, not original byte verification or physical admission.

Tagged Go tests passed (1.324s). Physical registration positive scenario has not been rerun after these additional guards; preceding physical run covers the retained-proof gate before the guard change. Full public admission, recovery and the V20–V32 objective remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 physical registration after qualification scope hardening

Full isolated PostgreSQL/MinIO transfer-batch-external passed (170.81s) with the stronger LockQualificationInTransaction plan tenant/actor and frozen config project/auth-session preflight. Existing prepared proof ownership, nonzero measured VRAM rejection, metadata exact-boundary, draft creation, registration and replay assertions remain enforced. This closes the pending positive regression for the last guard change; it does not establish public queued admission, durable proof recovery, complete original source/defect discovery or all V20–V32 deliverables. No production migration/deployment.

### 2026-10-06 — V27 required retained qualification presence

Transfer compiler marks plans that were built with an original qualification proof as requiring that proof. LockQualificationInTransaction rejects a required-but-missing proof before SQL instead of downgrading to the internal legacy pin-only scaffold. Added synthetic missing-proof rejection test alongside scope/pin/cancellation cases. Marker is private and in-memory only; this is not durable recovery or persisted authority.

Tagged Go tests passed (1.216s). Physical compiler/registration regression has not been rerun after adding the private presence marker. Full durable reconstruction, public queued admission and all remaining V20–V32 requirements remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 physical required qualification presence regression

Physical transfer compiler test now requires the original-proof presence marker, strips the proof from a plan copy and requires ErrInvalid from LockQualificationInTransaction, then proceeds with the original plan. Python requires V27_TRANSFER_PLAN_REQUIRED_QUALIFICATION_OK in the complete transfer scenario. This proves in-memory downgrade rejection, not persisted recovery semantics.

Tagged Go tests passed (1.425s). Full isolated PostgreSQL/MinIO transfer-batch-external passed (167.62s), confirming compiler presence marker, retained proof copy ownership, original plan admission/registration/replay and prior cost/metadata/person checks. Durable reconstruction and complete public queued admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 original qualification reconstruction on draft replay

Extended preparer recovery test: created draft must retain an original qualification proof and its required marker; switching to a fresh scheduler pool and revoking selected qualification policy must return ErrInvalid and no draft/proof result. Restoring policy permits replay with the same manifest and an equal but independently allocated qualification proof. This verifies the existing deterministic re-preparation path rereads/reverifies evidence, rather than trusting persisted metrics or serializing proof authority. It is a fresh scheduler connection test in the same test process, not an actual service process restart or independent recovery daemon.

Tagged Go tests passed (1.093s). Isolated PostgreSQL/MinIO preparer-fresh passed (20.08s), with required V27_PREPARER_QUALIFICATION_RECOVERY_OK marker. Partial-manifest/session-revocation recovery variants remain to be rerun with this stronger assertion; full public admission remains unfinished. No production migration/deployment.

### 2026-10-06 — V27 partial and revoked-registration qualification recovery

Isolated PostgreSQL/MinIO preparer-partial and preparer-registration-revoke both passed (48.24s combined). Required recovery marker verifies retained qualification presence, refusal while its policy is revoked and independently rebuilt original proof on replay after restoration. Existing partial failure assertions retain the completed input chunk, require draft/unsealed state and no task-input/manifest registration after injected manifest failure; session-revocation assertions require no input registration until authorization is restored. These are recovery via deterministic re-preparation/new scheduler pool in the same process, not a process-restart daemon or complete public queued admission. No production migration/deployment.

### 2026-10-06 — V27 physical defect qualification authority and role probes

Added physical defect authority mutation checks while original defect qualification lock is held: matching import audit UPDATE and DELETE and report artifact UPDATE must each return SQLSTATE 55P03 via an independent owner connection. Added actual SET LOCAL ROLE/SQL invocation denial checks for photo_api, photo_worker, photo_publisher, photo_portal, photo_auth, photo_gateway, photo_maintenance and photo_semantic_admission: each must return 42501 from the scheduler-only defect lock function. Python requires both new authority/ACL markers.

Tagged Go tests passed (1.555s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (68.49s), retaining original defect/transfer and combined source evidence checks. Writer release probes, policy revoke at transaction lock, durable full evidence recovery and public complete admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 source evidence retained through transfer preparation

PrepareTransferInputs now uses original qualified source preparation proofs for reference and each target, preserving general HDR semantics (unitRGB=false). Retained canonical source artifact pins are charged before further source processing. Prepared inputs carry private reference/target proofs; compiler validates binding, tenant/project/actor/session and source pin structure and copies proofs into WorkflowPlan. Existing internal all-source-proof-absent scaffold remains; mixed evidence rejects. This retains validated original refs, not final source transaction authority. Source proof copy ownership failure tests and final registration source-lock composition remain required.

Tagged Go tests passed (1.400s). Full isolated PostgreSQL/MinIO transfer-batch-external passed (167.28s), including updated metadata boundary accounting for source evidence, original source IO, measured cost denial and registration/replay. No production migration/deployment. Full V20–V32 objective remains incomplete.

### 2026-10-06 — V27 retained original source locks in draft/registration

Added LockPreparedSourcesInTransaction: validates complete scoped reference/target proof sequence before SQL, rejects conflicting duplicate bindings, deduplicates identical proofs, sorts artifact/version identities and locks exact original source pins through existing scheduler-only gate. Sets transaction tenant only after scheduler-role check. BeginDraftInTransaction and RegisterWorkflowInputsInTransaction invoke it before inventory locks and repeat before SQL draft creation/input registration, retaining row locks throughout. Internal all-proof-absent scaffold remains; proof-presence downgrade guard, dedicated failure sequencing and concurrent source mutation probes in these wrappers remain outstanding.

Tagged Go tests passed before/after the tenant setup adjustment. Isolated PostgreSQL/MinIO preparer-fresh, preparer-partial and preparer-registration-revoke all passed (82.39s combined), confirming new source locks coexist with original qualification recovery and deterministic draft/input replay. Full external batch regression, complete public source/defect inventory admission and queued workflow remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 required prepared source presence and scope

Transfer compiler marks original source proofs mandatory when supplied. LockPreparedSourcesInTransaction rejects required-but-empty evidence before SQL and explicitly rejects nil tenant/run/project/user/session. Added pure denial tests for missing mandatory proofs, foreign tenant/actor, nil project, incomplete source count, invalid later scoped proof and cancellation, all requiring zero SQL calls. Private marker is in-memory; durable recovery still reconstructs proof through the preparation path.

Tagged Go tests passed (1.144s). Isolated PostgreSQL/MinIO preparer-fresh passed (29.33s), confirming prepared required-source plans still create/replay drafts with exact source locks and qualification recovery. Dedicated positive ordering/failure-position source-plan tests and full external batch regression remain outstanding. No production migration/deployment. Full V20–V32 objective remains incomplete.

### 2026-10-06 — V27 prepared source lock sequencing

Added synthetic positive/failure tests for LockPreparedSourcesInTransaction. Four source proofs (two repeated reference/target pairs) deduplicate into two source locks after scheduler role and tenant setup, sorted by artifact/version. Every SQL failure position 1..6 including tenant Exec stops immediately. Conflicting duplicate binding evidence rejects with zero SQL calls. Test uses synthetic canonical source pins and proves sequencing, not original image bytes, actual rollback or complete admission.

Tagged Go tests passed (1.372s). Prior physical preparer-fresh regression remains applicable; no production behavior changed in this test-only step. Full external batch regression, source wrapper concurrent mutation probes and complete public workflow admission remain unfinished. No production migration/deployment.

### 2026-10-06 — V27 physical prepared source hold/release

Preparer integration test now obtains every artifact ID from retained canonical source pins, holds LockPreparedSourcesInTransaction in a scheduler transaction and requires independent owner UPDATE to fail with SQLSTATE 55P03 for each. After rollback, the same updates must affect one row in a separate bounded writer transaction, then roll back. Python requires V27_PREPARER_SOURCE_LOCK_RELEASE_OK. Test fixture does not cover every geometry/alpha/warp variant; release evidence applies to selected original pins in this scenario.

After correcting the test's source pin field reference, tagged Go tests passed (1.156s). Isolated PostgreSQL/MinIO preparer-fresh passed (33.64s), including source hold/release and qualification recovery. Partial/session-revocation variants with the new marker and full external regression remain pending. No production migration/deployment. Full V20–V32 objective remains incomplete.

### 2026-10-06 — V27 final prepared source/qualification metadata recheck

PrepareTransferInputs now rechecks every distinct retained original source proof after all later object IO and compares exact original report/calibration locations through the live qualification catalog before returning a prepared batch. Drift/error returns nil inputs/documents; final registration row locks remain mandatory and independent. Repeated identical source proofs are deduplicated for this final metadata phase. This closes a preparation metadata gap, not complete atomic admission.

Tagged Go tests passed (1.757s). Isolated PostgreSQL/MinIO preparer-fresh, preparer-partial and preparer-registration-revoke passed (85.45s combined), retaining source hold/release and qualification recovery assertions. Dedicated during-read mutation probes and full external batch regression after this change remain pending. No production migration/deployment. Full V20–V32 objective remains incomplete.


### 2026-10-06 — V27 qualification revocation during later object IO

Added a physical preparer probe that revokes the selected transfer qualification policy while opening the original protection mask, after initial qualification report verification. PrepareTransferInputs must observe the final live catalog recheck and return ErrInvalid with nil inputs and documents. The fixture restores the policy and continues the existing preparation/recovery checks. Python requires V27_PREPARER_QUALIFICATION_LATE_IO_RACE_OK for every preparer variant.

Tagged Go semanticproposal tests passed (1.545s). Isolated PostgreSQL/MinIO preparer-fresh passed (24.90s); preparer-partial and preparer-registration-revoke passed (53.84s combined). All sessions completed with exit code zero. This verifies policy revocation at the selected later IO boundary; it does not prove all source mutation races, full process restart recovery, scientific model qualification or complete public queued admission. Full external batch regression after the latest source registration/final metadata changes remains pending. No production migration or deployment. Full V20–V32 objective remains incomplete and active.


### 2026-10-06 — V27 full external batch regression after source registration checks

The existing transfer-batch-external physical PostgreSQL/MinIO scenario completed successfully after retained source transaction locks and final source/qualification metadata rechecks: 1 passed, 12 deselected in 179.56s, terminal exit code zero. Python requires the external batch preparation/publication markers, retained source proof checks, selected evidence/qualification lock assertions, plan qualification ownership/presence, person protection checks and measured-cost denial. This verifies the existing isolated scenario; it does not prove complete public queued admission, real model science, multi-tenant GPU load or every late IO source mutation. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 source expiry during protection mask IO

Added a physical preparer probe that expires the retained reference RGB artifact while opening the original source protection root, after initial reference source validation. PrepareTransferInputs must fail with nil inputs/documents. The fixture preserves and restores the exact original expires_at value and requires V27_PREPARER_SOURCE_LATE_IO_RACE_OK in Python. This boundary precedes target processing: the denial may occur during subsequent source reads; it does not independently prove the final post-all-IO recheck boundary.

Tagged Go semanticproposal tests passed (1.201s). Isolated PostgreSQL/MinIO preparer-fresh passed (22.56s); preparer-partial and preparer-registration-revoke passed (49.49s combined). All sessions terminated with exit code zero. Production logic is unchanged in this test-only step; previous full external batch regression remains applicable. Final-boundary source mutation, all geometry/alpha/warp variants, scientific model qualification and complete public queued admission remain pending. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 source expiry after target RGB validation

Extended the integration-only object-open hook with an optional matching-open ordinal (default remains second). The preparer probe expires retained reference RGB on the third shared protection-root open: source protection, selection separation, then target protection. This occurs after both reference and target RGB validation. The fixture requires the hook to fire at exactly three matching opens and preparation to return an error with nil inputs/documents, restores the exact expires_at value, then continues existing source lock/recovery checks. Python requires V27_PREPARER_SOURCE_FINAL_IO_RACE_OK in all preparer variants. This tests expiry during target protection IO, not arbitrary mutation after every final check or every source geometry variant.

Tagged Go semanticproposal tests passed (1.521s). Isolated PostgreSQL/MinIO preparer-fresh passed (32.67s); preparer-partial and preparer-registration-revoke passed (61.86s combined), all terminal exit code zero. Production source is unchanged; complete public queued admission, model scientific qualification and remaining V20–V32 remain pending. No production migration/deployment. Goal remains active.


### 2026-10-06 — V27 retained defect qualification resource metrics

readDefectQualificationPreparation now retains the Metrics value returned by original-byte v2qualification.Verify in its private value-owned proof, only after the repeated live catalog lookup succeeds. Revocation/error still returns the zero proof. The proof constructor uses named fields. The isolated defect report fixture contains synthetic RSS=401, VRAM=0 and scratch=503; Go requires those exact values and the restored qualification proof must remain equal. Python requires V27_DEFECT_QUALIFICATION_RESOURCE_METRICS_OK in the manual-lock-go-external scenario.

Tagged Go semanticproposal tests passed (1.388s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (72.62s), terminal exit code zero, including qualification authority locks and combined selected-source locks. This is retained original-report evidence, not installed defect-cost enforcement, hardware benchmark or complete admission. Wiring measured costs into a complete defect execution preparer and full public queued workflow remains pending; entire V20–V32 goal remains incomplete and active. No production migration/deployment.


### 2026-10-06 — V27 defect execution qualification cost lower bounds

Added private readDefectExecutionQualification preparation boundary. It rejects unsafe/negative installed additional costs before IO, verifies the original defect report and repeated catalog through readDefectQualificationPreparation, then requires installed RAM/VRAM/scratch to cover ceil of verified report peaks using the existing measured-cost validator. Failure returns the zero proof. It is not yet wired into a complete defect task execution coordinator and does not calculate image-dependent loader/operator overhead or aggregate concurrency capacity.

Physical manual-lock-go-external now requires exact synthetic RSS=401/scratch=503 costs to pass with the original retained proof, while RAM=400 or scratch=502 must return ErrInvalid and zero proof. Python requires V27_DEFECT_MEASURED_COST_DENIAL_OK. Tagged Go semanticproposal tests passed (1.185s); isolated PostgreSQL/MinIO scenario passed (75.03s), terminal exit code zero. These are synthetic resource values, not RTX3060 measurements. Full defect profile compilation, complete queued admission and remaining V20–V32 remain pending. No production migration/deployment; goal remains active.


### 2026-10-06 — V27 CPU defect proposal resource profile compiler

Added v2input.CompileSemanticDefectProfile for the existing CPU defects.propose worker. It accepts bounded dimensions, exact aggregate declared input file bytes, trusted additional qualification costs and independent RAM/VRAM/scratch/output ceilings. Output reservation mirrors prepare_proposals float32 protection/uint16 labels and mask_png.png16_bound, plus 24MiB control-document headroom. Scratch includes all input downloads plus output reservation and additional costs; RAM conservatively includes full declared input/output residency, 3GiB sequential bounded-ROI headroom and additional costs. All arithmetic checks the JSON safe-integer ceiling and all resource ceilings independently. The compiler generates CPU/float32/max_pixels/resource_estimate JSON and rejects overflow or capacity shortage without a profile.

Go v2input tests passed (7.834s), semanticproposal tests passed (1.827s). New tests cover exact capacity, one-byte-short RAM/scratch/output ceilings, output lower bound, excessive/unsafe dimensions and aggregate overflow. This pure compiler does not discover or attest input closure, read qualification reports, establish actual measured ROI peaks or authorize tasks. The 3GiB allowance is conservative planning headroom, not hardware/scientific qualification. Wiring exact catalog-derived sizes and readDefectExecutionQualification into the full execution preparer/admission remains pending. Existing Apply behavior is unchanged. No physical integration test was claimed for the new compiler; no production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 exact declared defect input byte accounting

Added SemanticInputSize and CompileSemanticDefectProfileFromInputs. The wrapper requires one exact size entry for Params and each declared input; validates UUID/SHA/positive safe byte counts, rejects duplicate IDs, missing/extra metadata, mismatched SHA, repeated declared inputs, Params aliasing an input, Params above 1MiB and aggregate overflow. It sums each file once and delegates to the CPU defect resource compiler. Ordering of inputs/catalog entries does not change the generated profile. Catalog authorization and discovery of the complete original dependency closure remain caller obligations, not properties of this pure helper.

Go v2input tests passed (8.656s), semanticproposal tests passed (1.905s). New tests compare exact aggregate output to the underlying compiler, check input permutation and reject incomplete/duplicate/stale/zero/oversized metadata. No physical catalog integration was claimed. Exact ready artifact lookup, transaction rechecks, qualified defect preparer integration and full queued workflow remain pending. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 qualified defect profile composition

Added private SQLTransferSourceReader.compileDefectExecutionProfile. It preflights retained original defect qualification scope/canonical policy/report pins, requires CPU backend, checks installed costs against verified report metrics, converts published original dependency refs and invokes exact declared-file profile compilation. Cancellation/error returns no profile. This pure composition has no SQL or publication side effects; supplied sizes still require authorized ready catalog lookup and final locks, and it is not yet a full execution coordinator.

Go semanticproposal tests passed (1.214s), v2input passed from cache. New synthetic-proof tests cover positive qualified profile, foreign session refusal, understated RAM refusal and cancellation. No original-report physical integration or complete input-closure discovery was claimed in this step. Catalog-backed sizes, complete qualified source/producer inventory and public queued workflow remain unfinished. No production migration/deployment; full V20–V32 remains active and incomplete.


### 2026-10-06 — V27 original-report qualified profile physical check

The manual-lock-go-external fixture now compiles a CPU defect profile from the retained original-byte verified defect qualification proof. Fixture-owner SQL obtains exact SHA/byte counts for the existing Params and WorkingImage root; the pure compiler receives this explicitly limited two-file declared set and test dimensions 24x20. Positive costs produce a profile; scratch below the original report peak denies with no profile. Python requires V27_DEFECT_ORIGINAL_QUALIFIED_PROFILE_OK.

Tagged Go semanticproposal tests passed (1.271s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (72.97s), terminal exit code zero. This proves composition with an original report and physical metadata; the owner lookup is test scaffolding and does not prove API catalog authorization or complete worker source-array/face/mask input closure. Dimensions are test inputs, not inferred from original image bytes by this compiler. Catalog discovery/admission integration, hardware/scientific qualification and remaining V20–V32 remain unfinished. No production migration/deployment; goal stays active.


### 2026-10-06 — V27 contract-scoped input size reader

Added v2draft.ReadInputSizes using the existing ArtifactCatalog interface (SQLArtifactCatalog already repeats editor/project/version authorization for exact contracts). The helper owns a bounded canonical copy of all LayerRef descriptors before callbacks, rejects malformed/duplicate identities before catalog IO, validates exact digest/positive safe size/key/version, retains each full object location, then repeats all lookups after the batch. Drift, lookup failure or cancellation returns nil sizes. Final admission locks and complete dependency discovery remain separate requirements; the helper does not broaden SQL catalog contracts or grants.

Go v2draft tests passed (3.838s), semanticproposal passed (2.019s), v2input passed from cache. New fake-catalog tests cover exact repeated lookup accounting, version drift, lookup denial and duplicate preflight with zero IO. Physical SQL size discovery and wiring the reader into the qualified defect coordinator remain pending, along with public queued admission and remaining V20–V32. No production migration/deployment. Goal remains active and incomplete.


### 2026-10-06 — V27 physical authorized source input size catalog

manual-lock-go-external now exercises ReadInputSizes through SQLArtifactCatalog with the real photo_api pool and exact retained source LayerRef descriptors. Every returned UUID/SHA/byte count must match the already verified source pins. A foreign tenant must return an error and nil sizes. After fixture-owner expiry of source RGB, size lookup must fail without partial results; after restoration it succeeds. Python requires V27_SOURCE_AUTHORIZED_INPUT_SIZES_OK. No catalog contract or ACL was widened.

Tagged Go semanticproposal tests passed (1.366s); isolated PostgreSQL/MinIO manual-lock-go-external passed (81.53s), terminal exit code zero. This proves authorization/availability for this selected source closure, not all Params/face/parser/manual contracts, full dependency discovery or final transaction stability. Qualified defect coordinator integration, complete queued workflow and remaining V20–V32 remain pending. No production migration/deployment; goal remains active and incomplete.


### 2026-10-06 — V27 catalog-backed qualified defect profile composition

Added private prepareCatalogDefectExecutionProfile. It owns a bounded canonical copy of published Params/dependencies and contract descriptors, copies expected policy, requires descriptors to match the exact declared UUID/SHA set without duplicates, then preflights retained qualification scope, CPU backend, measured-cost lower bounds and dimensions before catalog callbacks. ReadInputSizes supplies actual sizes with repeated location checks; final profile compilation uses those sizes. Preflight uses minimal placeholder sizes only to reject invalid requests early; no such estimate is returned or used for final capacity. Any error returns no profile.

Go semanticproposal tests passed (2.651s), v2draft and v2input passed from cache. Synthetic catalog test requires four lookups for two files and a positive qualified profile; duplicate descriptor set rejects before any catalog IO. Physical SQL coverage of this combined method, exact source-derived dimensions, full original dependency discovery and final atomic queued admission remain pending. The injected catalog is internal trusted wiring, not caller-supplied public authority. No contract/grant widening, production migration or deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 final live defect qualification recheck

Added recheckDefectQualificationPreparation: exact private scope/canonical Expected/report pin preflight followed by repeated live imported qualification object lookup; metadata drift conflicts and revoked policy denies. prepareCatalogDefectExecutionProfile invokes it after catalog size reads and actual profile compilation, returning no profile on failure. The synthetic catalog composition test now requires refusal without a live qualification DB rather than allowing an offline positive profile; the separate pure compiler test remains positive.

Tagged Go semanticproposal tests passed (2.635s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (226.46s), terminal exit code zero. Python requires V27_DEFECT_QUALIFICATION_FINAL_RECHECK_OK; physical original proof recheck succeeds, fails after selected policy revocation and succeeds after restoration. Dedicated revocation inside catalog callbacks and full combined-method SQL catalog coverage remain pending; these checks do not provide atomic admission or real model scientific evidence. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 original canonical defect Params catalog adapter

Added private defectProfileCatalog. Params lookup accepts only the exact original Params pin with no auxiliary descriptor fields, obtains its object through the existing exact-source canonical defect evidence authorization, and verifies returned identity. Other contracts delegate to existing SQLArtifactCatalog with the fixed source version/project. This grants no arbitrary new task Params access and widens no SQL contract/ACL. It is useful for already canonical producers, not yet creation of new proposal tasks.

Physical fixture now calls prepareCatalogDefectExecutionProfile with this API-backed catalog for original Params and WorkingImage root, requires byte-identical profile to independent exact-size compilation, and rejects a foreign Params reference. Python requires V27_DEFECT_AUTHORIZED_PROFILE_CATALOG_OK. Tagged Go semanticproposal tests passed (1.909s); isolated PostgreSQL/MinIO manual-lock-go-external passed (171.93s), terminal exit code zero. The two-file set is explicitly limited and does not prove all native worker array/face/mask input closure; dimensions remain fixture arguments. Dedicated during-catalog qualification revocation, full source-derived coordinator and new task publication/admission remain pending. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 qualification revocation inside profile catalog lookup

Added an integration-only ArtifactCatalog wrapper that revokes the selected defect qualification policy immediately after the first successful delegated API-backed lookup. prepareCatalogDefectExecutionProfile must return ErrUnqualified, nil profile and a confirmed fired callback. The fixture restores the policy and requires byte-identical profile recovery through the original catalog. Python requires V27_DEFECT_PROFILE_CATALOG_REVOCATION_OK.

Tagged Go semanticproposal tests passed (1.643s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (168.63s), terminal exit code zero. This validates the final live qualification recheck for the selected during-catalog boundary; it does not prove atomic queued admission, a complete native dependency closure, all concurrent mutation boundaries or model/hardware scientific qualification. Test-only change, no production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 source-derived defect execution profile

Added private prepareSourceDefectExecutionProfile. It owns published dependency descriptors/policy before IO, verifies original defect qualification and measured-cost bounds, verifies ICC and unit-interval original RGB source with retained proof, derives dimensions from that source, requires every selected source closure descriptor exactly among declared inputs, then invokes catalog-backed profile preparation. Before returning, it rechecks retained source metadata and live defect qualification again. It never accepts caller dimensions and returns nil profile on any failure. Full face/parser/manual/person completeness and final transactional locks remain separate obligations.

Go semanticproposal passed (2.561s), v2draft/v2input cached; tagged semanticproposal passed (2.567s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (171.17s), terminal exit code zero. Fixture supplies original Params plus all retained source nodes through the API-backed catalog and requires profile pixel_count to equal independently prepared original source dimensions. Omitting RGB/source closure must return ErrInvalid and nil profile; Python requires V27_DEFECT_SOURCE_DERIVED_PROFILE_OK. This uses synthetic qualification data and an already canonical producer; new proposal task publication, full additional producer input closure and queued admission remain pending. No production migration/deployment. Entire V20–V32 remains incomplete and active.


### 2026-10-06 — V27 original Params native dependency verification

Added review.VerifyDefectProposalDeclaredInputs. It verifies the original Params SHA and bounded strict document, validates exact source geometry through existing proposal validator, rejects malformed/duplicate declared identities and Params self-alias, then requires exact Image/RGB/permanent/additional protection/landmark/skin/manual refs used by the native worker. Extra original geometry/manual roots are allowed and still require separate catalog authorization. This does not select all required producers or certify mask/face content.

Go review passed (1.533s), semanticproposal passed (2.136s). Existing multi-face compilation fixture now checks complete original inputs, removes each native dependency in turn and requires denial, and rejects changed original Params bytes. Wiring original Params IO and this check into prepareSourceDefectExecutionProfile remains pending; prior source-derived profile alone does not prove Params dependency completeness. No physical integration claimed for this new validator, no production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 original Params IO integrated into source profile preparation

prepareSourceDefectExecutionProfile now requires a Params descriptor, authorizes its exact object through the selected catalog, reads original pinned bytes, reads/decodes original WorkingImage and invokes VerifyDefectProposalDeclaredInputs before preparing the resource profile. It repeats exact Params object lookup after byte validation. Changed Params, missing native dependencies or catalog drift return no profile. The physical source-derived fixture now includes the original permanent protection root and pixels named by Params, via existing exact-source ReadProtection and SQL catalog contracts; the previous source-only declared set was insufficient.

Tagged Go semanticproposal passed (3.150s), review passed (2.832s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (71.89s), terminal exit code zero, including source-derived profile and during-catalog qualification revocation. Existing unit tests deny each omitted native dependency and changed Params; a dedicated physical omission of permanent protection in this new combined path remains pending. Producer discovery completeness, original face/mask scientific checks, new task publication and final queued admission remain unfinished. No SQL grant/contract widening or production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 physical Params-required protection omission and stale SHA

Extended the source-derived profile physical scenario with two failures while retaining the complete source/RGB closure. First removes only the original Params permanent protection pixel from both published inputs and catalog descriptors; second keeps that UUID but changes its SHA consistently in both collections. prepareSourceDefectExecutionProfile must return ErrInvalid and nil profile in both cases, independently of source closure validation. Python requires V27_DEFECT_PROFILE_REQUIRED_PROTECTION_OK.

Tagged Go semanticproposal tests passed (1.652s); isolated PostgreSQL/MinIO manual-lock-go-external passed (91.52s), terminal exit code zero, retaining the positive source-derived profile, original Params IO and qualification revocation checks. This verifies original Params dependency membership for the selected permanent mask, not all actual mask bytes, face/parser discovery or atomic queued admission. Test-only changes; no production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 original Params byte corruption in source profile preparation

Added a physical profile probe using the existing corruptDefectOpen test reader. The exact Params location is obtained through the original canonical-producer API catalog; one byte is changed in the returned object stream while catalog SHA/version remains intact. prepareSourceDefectExecutionProfile must fail with nil profile and a confirmed fired corruption hook. A subsequent call with the original reader must recover a byte-identical source-derived profile. Python requires V27_DEFECT_PROFILE_ORIGINAL_PARAMS_BYTES_OK.

Tagged Go semanticproposal tests passed (1.213s); isolated PostgreSQL/MinIO manual-lock-go-external passed (79.96s), terminal exit code zero. This proves original Params byte verification in the combined profile path, not arbitrary face/mask byte integrity, scientific qualification or full queued admission. Test-only changes; no production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 physical original Params mask validation

Added review.DefectProposalMaskInputs, deriving fresh bindings from strict SHA-verified original Params and existing exact-source geometry validation. prepareSourceDefectExecutionProfile now deduplicates mask artifacts named by those Params, obtains exact objects through the selected catalog, checks NPY media type, opens original streams, verifies float32 mask NPY shape/digest/value range with masknpy and repeats each object lookup. Any byte/metadata/close error returns no profile. This preserves algorithms and does not add producer/scientific authority to generic catalog reads.

Go semanticproposal passed (1.409s) with review cached; tagged semanticproposal after fixture edit passed (1.916s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (78.53s), terminal exit code zero. The positive source profile now validates the original permanent mask; corrupting its stream while keeping catalog pins intact must fail with nil profile and confirmed hook. Python requires V27_DEFECT_PROFILE_ORIGINAL_MASK_BYTES_OK. Physical face skin/manual/additional-mask cases, complete producer discovery/qualification, new task publication and final queued admission remain pending. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 original landmark document validation in profile preparation

Added review.DefectProposalFaceInputsFromParams (owned strict SHA/source-bound bindings) and VerifyDefectProposalFaceDocument. The latter verifies original landmark bytes, resolves exact selected object from a fully source-validated FaceLandmarks collection or validates the legacy standalone Face68 path, then checks skin/manual object IDs against the resolved face. prepareSourceDefectExecutionProfile reads each declared landmark through its existing catalog descriptor, verifies the original document and repeats object metadata lookup before continuing. No producer/model qualification is inferred from JSON fields and no catalog grant/contract was widened.

Go review passed (0.877s), semanticproposal passed (1.216s). Existing original Python landmark fixture now verifies positive proposal binding, missing selected object denial and changed bytes denial. Physical multi-face catalog/profile coverage remains pending; prior permanent-mask-only physical fixture does not prove the new landmark branch. Complete required producer selection, independent face qualification, new task publication and final queued admission remain unfinished. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 profile FaceLandmarks scoped catalog

Extended private defectProfileCatalog with FaceLandmarks lookup through existing facePreparationObjects for operation face.landmarks. Descriptor auxiliary fields must be absent and any supplied canvas must match the fixed source binding; the returned object must match the exact requested ref. Existing canonical producer/source API gate remains the authority. Other contracts and SQL grants are unchanged; document claims do not establish scientific qualification.

Tagged Go semanticproposal tests passed (2.466s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (123.27s), terminal exit code zero. Python requires V27_DEFECT_PROFILE_LANDMARK_CATALOG_OK: original scoped landmark lookup succeeds, foreign canvas and non-landmark artifact fail. Full multi-face profile, skin/manual catalog composition and independent producer qualification remain pending, alongside new task publication and queued admission. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 face mask binding validation for collection and standalone faces

VerifyDefectProposalFaceDocument now independently checks supplied skin/manual mask binding validity, expected selector, unprotected state, exact source canvas/canonical transform chain and resolved face object ID. This closes reliance on only the surrounding Params validator when the exported face verifier is called directly. Original document SHA/face geometry checks remain mandatory; no producer/model scientific qualification is inferred.

Go review passed (1.020s), semanticproposal passed (1.481s). Original Python face fixtures exercise positive source-bound skin, wrong mask object ID, wrong canvas/selector, and positive legacy standalone Face68 with denial for a foreign mask object. No physical multi-face profile claim was made; that integration and full qualified producer discovery remain pending along with new task publication/queued admission. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 actual capacity preflight before mask/landmark content IO

prepareSourceDefectExecutionProfile now invokes actual catalog-backed cost/profile validation after original Params dependency validation and before opening face landmark/mask streams. Final catalog/profile and source/qualification rechecks remain after content IO; the early result is discarded and never authorizes execution. Original source/qualification/Params verification still occurs before this capacity phase; this is not a bound on every preparation IO.

Tagged Go semanticproposal tests passed (1.422s). Isolated PostgreSQL/MinIO manual-lock-go-external passed (94.14s), terminal exit code zero. With RAM limit 1GiB, preparation must return ErrInvalid and nil profile while a corruption probe for the required permanent mask remains unfired (no mask open). Python requires V27_DEFECT_PROFILE_CAPACITY_BEFORE_MASK_IO_OK. Positive source profile, original Params/mask corruption and qualification revocation assertions remain included. Physical landmark IO capacity refusal, aggregate tenant concurrency and final queued admission remain pending. No production migration/deployment. Full V20–V32 remains incomplete and active.


### 2026-10-06 — V27 source profile descriptor preflight before qualification IO

prepareSourceDefectExecutionProfile now rejects nil catalog, empty/oversized declared input lists and descriptor count mismatch before canonical serialization. After owning the bounded snapshot, validateDefectProfileDescriptors checks exact UUID/SHA correspondence, nonempty contracts, unique input and descriptor IDs, and the original Params contract before reading qualification/source objects. The validated descriptor index is reused for source closure and later original content verification. No scope/content/qualification guard is bypassed.

Go semanticproposal passed (1.666s), review/v2input passed from cache. New tests use an empty reader and instrumented catalog: missing/duplicate/contractless descriptors must return ErrInvalid with nil profile and zero catalog callbacks; duplicate declared inputs deny. No new physical regression was claimed for this preflight-only ordering change. Full multi-face profile integration, required producer qualification and queued admission remain pending. No production migration/deployment. Entire V20–V32 remains incomplete and active.

### 2026-10-06 — V27 catalog profile descriptor preflight

`prepareCatalogDefectExecutionProfile` now shares the source-path descriptor validator before catalog IO: exact declared UUID/SHA set, nonempty contracts, Params contract, duplicate rejection. Nil catalog and input/node count bounds are checked before canonical ownership copying. This removes weaker duplicated validation from the catalog path.

Validation: `go test ./internal/semanticproposal ./internal/review ./internal/v2input` passed (semanticproposal 1.145s; other packages cached). New negative tests cover wrong Params contract, missing input contract and malformed digest with zero catalog callbacks. No physical PostgreSQL/MinIO rerun in this change; no queued admission or complete V27 implementation claimed.

### 2026-10-06 — V27 physical regression after descriptor preflight

Terminal integration result: `PHOTO_TEST_MINIO_TMPFS=1 PYTHONPATH=workers:src:tests .venv-platform/bin/python -m pytest tests/integration/test_v27_semantic_intent_publication.py -k manual-lock-go-external -q --tb=short` passed: 1 passed, 12 deselected, 84.93s, exit 0. Disposable PostgreSQL/MinIO applied the repository migrations. Existing required markers cover authorized original file accounting, source-derived profile, permanent protection dependencies, corrupt original Params/mask byte denial, capacity denial before mask IO, landmark catalog authorization, qualification revocation and final recheck. This verifies regression of the catalog descriptor unification against this original-source scenario.

Limits: this mode does not establish multi-face profile completeness, model scientific qualification, RTX 3060 performance, new task Params publication authority or complete queued admission. Full V20–V32 goal remains incomplete.

### 2026-10-06 — V27 typed face and mask profile dependencies

Source profile preparation now requires `FaceLandmarks` for each declared landmark document and `MaskPixels` for each mask array before opening that role's bytes; exact UUID/SHA matching alone cannot authorize a generic Artifact contract. The physical original-source test adds a wrong-mask-contract denial alongside the unchanged valid profile path.

Validation: Go semanticproposal/review/v2input tests passed (semanticproposal 1.514s). Disposable PostgreSQL/MinIO publication test, `-k manual-lock-go-external`, terminated exit 0: 1 passed, 12 deselected in 108.82s. This confirms the new mask-contract negative scenario and existing original source/Params/mask/qualification cases. Full multi-face scientific qualification and queued admission remain incomplete; this is no claim of V27 or V20–V32 completion.

### 2026-10-06 — V27 retained manual root accounting

Descriptor preparation now rejects retained PublishedDefectParams.ManualRoots unless every root matches an exact declared input UUID/SHA, is distinct from Params, and appears once. The count is bounded by the declared input count. Manual provenance cannot silently escape catalog file accounting through this preparation path.

Validation: semanticproposal/review/v2input Go tests passed, semanticproposal 1.425s. Added positive declared-root validation and zero-catalog-IO negative checks for missing root, changed SHA, duplicate roots and Params alias. This pure descriptor change was not rerun physically. Original root content verification, producer qualification and durable admission remain separate outstanding composition requirements. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 pure profile provenance closure parity

Extracted `validateDefectPublishedRefs` and invoked it from both `compileDefectExecutionProfile` and catalog descriptor validation. Pure compilation now rejects unaccounted manual provenance roots, duplicate roots, Params aliases and changed root digests; it also bounds and validates the declared reference set before allocating converted inputs. Catalog and source preparation retain the same checks through the shared validator.

Validation: Go semanticproposal/review/v2input passed (semanticproposal 1.288s). Added pure compiler negative cases using an otherwise valid qualification/cost fixture, so rejection is attributable to malformed manual provenance rather than missing qualification. No physical rerun for this pure validation change. These references do not independently prove original manual content or grant queued authority; full V20–V32 remains incomplete.

### 2026-10-06 — backend-wide compilation and unit regression

`go test -tags integration -run '^$' ./...` completed exit 0 for all backend packages: integration-tagged sources compile, but integration tests were not executed by this command. `go test ./...` initially failed under sandbox because httptest loopback bind was prohibited; rerun with local network execution permission completed exit 0 for all packages (v2render 32.630s, other packages passed/cached). No source changes were needed for these environment failures.

Reviewed defects.propose worker reserve formula against semantic defect profile: array reservation 6*pixels+16384 plus png16_bound is represented in the compiler's output estimate. The extra 3GiB RAM headroom remains a planning allowance, not measured RTX/model qualification. Whole-backend unit success does not prove V20–V32 completion, public queued admission or full end-to-end scientific quality.

### 2026-10-06 — defect worker quality regression

Executed seven platform suites: test_defect_propose, test_defect_propose_adapter, test_defect_labels, test_defect_repair, test_defect_repair_adapter, test_defect_review and test_v26_render_defects. Terminal result: 44 passed in 7.68s, exit 0. Sandbox denied pytest cache write only; no test failed.

Inspected scenario coverage: dark/red spot proposals and exact protection; low-confidence/textureless behavior and determinism; bound landmark collection; wrong-object mask denial; protection maximum union and undeclared protection denial; float32 repair without uint8 quantization; approved-only support windows; stale/substituted review, labels, current pixels and protection rejection; alpha preservation; resource denial before artifact IO. These are fixture-based algorithm/adapter checks, not measured real model corpus or RTX3060 qualification. Full V27 admission/service completion and V20–V32 remain unproven.

### 2026-10-06 — V27 shared landmark collection IO

Source profile preparation reads each exact original landmark document once per preparation while still resolving and validating each face independently. Each reuse follows a fresh authorized catalog lookup and requires identical object metadata; per-face post-validation lookup and final declared catalog recheck remain. Cache lifetime is one preparation and cannot survive a request.

Go semanticproposal/review/v2input passed (semanticproposal 1.394s). Unit test proves two uses cause one Open/Close, metadata version drift denies, and cancellation denies cached access. No physical multi-face profile test added yet; full scientific qualification and queued admission remain incomplete.

### 2026-10-06 — V27 original landmark cache ownership

Retained landmark bytes now use an immutable string and each cached access returns an owned byte slice. A consumer cannot mutate the verified original retained for a later face. Tests additionally verify corrupted original bytes fail without populating the cache and a subsequent original-byte read succeeds with both streams closed.

Go semanticproposal/review/v2input passed (semanticproposal 2.009s). This verifies byte ownership and corruption behavior in isolation, not complete physical multi-face profile composition or queued admission. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 physical original landmark byte regression

Extended manual-lock-go-external physical scenario with dedicated required marker V27_DEFECT_PROFILE_ORIGINAL_LANDMARK_BYTES_OK. It authorizes an original landmark artifact through API catalog, verifies versioned original bytes, repeats catalog authorization before reuse, confirms returned-buffer mutation cannot alter retained bytes, and corrupts the original transport while preserving metadata to prove rejection without cache population.

Disposable PostgreSQL/MinIO test completed exit 0: 1 passed, 12 deselected in 80.26s. Existing source/Params/mask/resource/qualification checks remain required. This tests the landmark IO helper on real stored fixture artifacts, not a full multi-face source profile, scientific model qualification or new queued task authority. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 full two-face source profile fixture

Connected existing MultiFaceDefectPins fixture to prepareSourceDefectExecutionProfile using original producer Params, both exact landmark collections, qualified source ICC/RGB closure and permanent protection root/pixels. A new required V27_DEFECT_PROFILE_MULTIFACE_SOURCE_OK marker proves successful nonempty profile preparation through the complete source path, including per-face original document/object-mask binding checks and mask byte verification.

Disposable PostgreSQL/MinIO test manual-lock-go-external terminated exit 0: 1 passed, 12 deselected in 68.67s. This two-face fixture uses two different landmark documents, not repeated faces sharing one collection; it is synthetic evidence, does not qualify real face models or independently establish manual producer provenance/queued admission. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 face-qualified original profile composition

Added private prepareFaceQualifiedDefectExecutionProfile for existing canonical defect origins. It preflights exact descriptors/Params origin, owns face expectations, reads qualified original detection/landmark producers, prepares the source-derived byte/resource profile, then repeats face qualification and compares the canonical resolved face set. Final SQL admission remains separate; this method does not authorize arbitrary new Params.

Physical two-face fixture succeeds through the composed path; wrong trusted landmark runtime lock returns ErrUnqualified and no profile. Required marker V27_DEFECT_PROFILE_FACE_QUALIFIED_OK. Disposable PostgreSQL/MinIO test completed exit 0: 1 passed, 12 deselected in 74.65s. Qualification reports here are synthetic fixtures, not proof of scientific model quality. Complete parser/manual/person composition, final all-family locking and queued admission remain outstanding; V20–V32 incomplete.

### 2026-10-06 — V27 composed profile ownership and final retained checks

Face-qualified profile composition now copies bounded Published/Nodes before any IO and owns defect expectations. It retains original defect qualification and qualified source proof before face IO, then checks those exact proofs after the last face qualification read. Returning a profile cannot skip those retained source/qualification checks merely because inner source preparation already finished.

Go semanticproposal/review/v2input passed (semanticproposal 1.194s). Disposable PostgreSQL/MinIO manual-lock-go-external regression terminated exit 0: 1 passed, 12 deselected in 90.17s, including composed two-face positive and wrong-runtime denial. No dedicated revocation probe during the final face pass yet; this result does not prove that race or queued admission. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 defect revocation during final face IO

New required physical marker V27_DEFECT_PROFILE_FINAL_FACE_REVOCATION_OK. The object reader hook disables the original defect qualification policy on the second face qualification report Open (the final face pass, after inner source/profile preparation). The composed method returns ErrUnqualified, no profile, and the hook must have fired. Restoring the exact policy permits the composed two-face profile again.

Disposable PostgreSQL/MinIO manual-lock-go-external terminated exit 0: 1 passed, 12 deselected in 87.40s. This specifically proves the final retained defect qualification check under that revocation timing. It does not prove final source expiry timing, all-family transaction locking, new Params publication or queued admission. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 source expiry during final face IO

New required marker V27_DEFECT_PROFILE_FINAL_FACE_SOURCE_EXPIRY_OK. On the second original face qualification report Open, the fixture expires the selected WorkingPixels artifact after inner source/profile preparation. Composition must return an error and no profile with the hook confirmed fired. Restoring the original source artifact permits profile preparation again.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 112.43s, with both final-face defect policy revocation and source-expiry probes required. This establishes those preparation rejection timings on stored synthetic fixtures, not final atomic admission locking or real model/hardware qualification. Full V20–V32 incomplete.

### 2026-10-06 — V27 mask-qualified profile composition (not physically verified yet)

Added private prepareMaskQualifiedDefectExecutionProfile: owns descriptors/expectations, retains original defect/source proof, validates qualified parser/detector mask roots, requires every discovered root and pixel in the exact declared accounting set, composes face-qualified profile, repeats complete mask inventory and retained source/defect checks. Existing origin only; no queued authority.

Go semanticproposal/review/v2input tests pass. New composed skin/manual profile has not yet been physically exercised; next validation must use existing original SkinFaceDefectPins fixture and denied qualification/omitted-root cases. This is incomplete composition work, not complete V27 or V20–V32.

### 2026-10-06 — V27 physical skin-qualified profile composition

Connected original SkinFaceDefectPins, original landmark, skin root/pixel descriptors, source ICC/RGB and permanent mask to prepareMaskQualifiedDefectExecutionProfile. The complete composed path returns a profile with valid parser/detector and face qualification fixtures. Wrong trusted parser runtime lock denies with no profile. Required marker V27_DEFECT_PROFILE_SKIN_QUALIFIED_OK.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 129.22s. Root omission denial and final parser revocation timing have not yet been added to this composition test. Synthetic qualification, existing canonical producers only; scientific model quality and queued admission remain incomplete. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 skin provenance root omission denial

Required physical marker V27_DEFECT_PROFILE_SKIN_ROOT_REQUIRED_OK proves omission of the original skin root from both declared inputs and descriptors returns ErrInvalid and no profile while its mask pixels remain declared. Existing successful complete skin profile and wrong-parser-runtime rejection remain required.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 107.18s. Final parser revocation timing and complete retained all-family admission remain outstanding. Synthetic fixture reports only; full V20–V32 incomplete.

### 2026-10-06 — V27 cross-family qualification recheck after mask IO

Mask-qualified profile retains exact original face and parser/detector qualification objects before mask IO and checks all four after the final mask pass. Added required V27_DEFECT_PROFILE_FINAL_MASK_FACE_REVOCATION_OK probe: second parser qualification report Open revokes landmark policy, after inner face/profile preparation; composition must return ErrUnqualified/no profile and confirmed hook. Fixture restores the policy before continuing.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 124.47s. This proves cross-family face policy revocation denial at final mask timing; it does not establish atomic all-producer locking, arbitrary new Params authority or queued admission. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 qualification family preflight

Both composed profile paths validate required qualification operation/version families before qualification/source IO. Defect profile requires defects.propose v1 CPU; faces require face.landmarks/face.detect v1; mask composition requires face.parse/face.detect v1. Original qualification content/model/runtime/license verification remains mandatory in the readers.

Go semanticproposal/review/v2input passed (semanticproposal 1.346s). Unit tests cover supported optional mask configuration, unsupported defect GPU backend, face version mismatch and wrong mask operation. No physical rerun for this early pure-validation change. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 final mask parser/detector revocation coverage

Expanded final-mask qualification hook to independently revoke landmark, parser and detector policies. Each hook fires on the second parser report Open after inner face/source profile preparation; every case must return ErrUnqualified and no profile, then restore the exact policy before the next case. Added required V27_DEFECT_PROFILE_FINAL_MASK_PARSER_DETECTOR_REVOCATION_OK marker alongside the face marker.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 170.97s. This verifies the three policy revocation preparation paths, not all-family atomic producer locking or public queued admission. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 face producer validation after mask IO

Mask-qualified composition now repeats ReadQualifiedDefectFaceSet after the final mask pass, before final retained qualification/source/defect checks. Original face producer profiles and content authority no longer rely solely on the earlier inner face profile preparation when later mask IO has occurred.

Go semanticproposal/review/v2input passed. Dedicated final-mask producer-profile mutation probe and physical regression of this additional pass remain pending; no atomic admission lock claim. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 final mask landmark producer-profile mutation

Added required V27_DEFECT_PROFILE_FINAL_MASK_PRODUCER_CHANGE_OK probe. On the second parser qualification report Open, after inner face preparation, the fixture changes the landmark task attempt runtime_lock_sha. Composition must return ErrUnqualified/no profile with the hook confirmed fired. Restores the original trusted runtime lock before later fixture checks. This exercises the new face producer validation after final mask IO separately from policy revocation.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 162.88s. Existing source expiry, mask/Params corruption, multi-face, skin composition, root omission and family policy revocations remain required. No atomic admission or complete V20–V32 claim.

### 2026-10-06 — V27 retained source execution preparation

Refactored source profile core into prepareSourceDefectExecutionEvidence, returning a private comparable defectSourceExecutionPreparation with immutable profile/descriptors/expected strings and original source/defect qualification proofs. Existing JSON-returning helper delegates to the core; all failures return zero evidence. This permits later compiler/admission composition to retain exact proof scope instead of discarding it at the JSON boundary.

Go semanticproposal/review/v2input passed; negative tests verify invalid/cancelled preparation returns zero retained evidence. No physical regression of this refactor yet; no retained face/parser/manual or final queued admission authority claimed. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 physical retained source execution proofs

Original source-profile fixture now calls prepareSourceDefectExecutionEvidence and verifies retained source proof equals the earlier original retainedSourceProof and defect qualification equals originalDefect. It decodes the immutable descriptor snapshot and checks Params/input/node retention; existing profile dimensions and all denial markers remain required. New marker V27_DEFECT_SOURCE_EXECUTION_RETAINED_PROOFS_OK.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 146.27s. This confirms the retained-evidence refactor against physical original artifacts; no full all-family proof or queued admission authority claimed. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 retained face execution preparation

Face-qualified core now returns private comparable defectFaceExecutionPreparation: retained source execution evidence, exact canonical origin pins, immutable resolved face/expected snapshots and original qualification object pins. Captures the latter before face IO and compares them after the final face pass. Existing JSON helper delegates without changing its output contract; errors return zero evidence.

Go semanticproposal/review/v2input passed, with explicit cancelled/invalid zero-evidence tests. Physical regression and positive retained-face evidence assertions remain pending for this refactor. No complete original producer metadata lock or queued authority claimed; full V20–V32 incomplete.

### 2026-10-06 — V27 physical retained face execution proofs

Two-face fixture now directly prepares defectFaceExecutionPreparation and requires exact canonical origin pins, retained original source/defect proofs, two resolved Face68 entries and nonempty expected snapshot. Every retained face qualification pin must match the original report UUID/SHA with a retained version. New mandatory marker V27_DEFECT_FACE_EXECUTION_RETAINED_PROOFS_OK.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 163.71s. Existing source/profile/skin/corruption/revocation/producer drift checks remained required. This validates face retained-evidence refactor; full original evidence transaction locks and queued authority remain incomplete, as does V20–V32.

### 2026-10-06 — V27 retained mask execution composition

Mask-qualified core now returns private comparable defectMaskExecutionPreparation, retaining the entire face/source preparation, immutable original mask-root inventory and qualification expectations, plus all four original face/parser qualification object pins. JSON helper delegates to the retained core; every failure returns zero evidence and context cancellation is checked before success.

Go semanticproposal/review/v2input passed. New cancelled/invalid zero-evidence tests pass. Physical positive retention assertions and regression of mask-core refactor remain pending. Original producer location proofs and atomic queue admission are not yet complete; full V20–V32 remains incomplete.

### 2026-10-06 — V27 physical retained mask execution proofs

Skin fixture directly prepares defectMaskExecutionPreparation, requiring exact original mask root inventory, origin/source/defect proofs and all four retained face/parser qualification report pins with versions. Required marker V27_DEFECT_MASK_EXECUTION_RETAINED_PROOFS_OK. Existing corruption, root omission, family revocation and producer-profile mutation checks remain enabled.

First run reached the new retained-mask marker but failed the old 180-second Go context during later revocation checks (226.95s pytest total); it was not a passing run. Expanded manual-lock-go fixture alone now has bounded 300s context / 360s go-test / 390s subprocess limits; other modes retain 180/210/240. Rerun completed exit 0: 1 passed, 12 deselected in 186.21s. These timings are test harness execution, not deployment performance qualification. Full original producer locks/queued admission and V20–V32 remain incomplete.

### 2026-10-06 — V27 declared input location retention

Added v2draft.ReadInputEvidence retaining exact ref/contract/object key/version/SHA/byte metadata after the same repeated catalog checks; ReadInputSizes delegates to it. Source execution preparation stores bounded immutable canonical file evidence, recompiles resource accounting from retained sizes and denies mismatch before final source/qualification rechecks. These pins can accompany later transaction locks; they are not locks themselves.

Go v2draft/semanticproposal/v2input passed (3.822s/1.277s/cached). New unit check verifies exact retained locations; existing size drift/denial tests exercise delegated core. Physical retention assertion/regression remains pending. Full V20–V32 incomplete.

### 2026-10-06 — V27 physical declared file pin retention

Added mandatory V27_DEFECT_SOURCE_EXECUTION_FILE_PINS_OK: retained InputEvidence count/ref/contract must match full original source descriptors and each exact object key/version/SHA/bytes/media type must match stored tenant artifact metadata. This exercises ReadInputEvidence and source-profile re-accounting through the physical retained core.

Disposable PostgreSQL/MinIO manual-lock-go-external completed exit 0: 1 passed, 12 deselected in 159.51s. Existing source/face/mask retained proofs and denial scenarios remain required. Final transaction locks and queued admission remain outstanding; full V20–V32 incomplete.

### 2026-10-06 — V27 retained file pin integrity preflight

Source core now validates its retained file result before returning: principal/tenant/project/source scope, original defect qualification scope, canonical bounded descriptors/files, exact ref/contract set, duplicate rejection, safe positive bytes and versioned locations, plus exact source closure object matching. decodeDefectExecutionFilePins provides owned decoded locations for later lock composition and denies changed source metadata.

Go semanticproposal/v2draft/v2input passed. Physical regression and dedicated mutation tests for this newly connected pure integrity preflight remain pending. No SQL file-lock implementation or queued admission claimed; full V20–V32 incomplete.

### 2026-10-06 — V27 retained file mutation rejection verified

Retained file decoder now requires canonical qualification expectations and exact source closure descriptors, including parent, canvas and path. Physical manual-lock-go-external test requires original file decoding and rejects modified source object version and duplicate file evidence; mandatory marker V27_DEFECT_SOURCE_EXECUTION_FILE_PIN_MUTATION_DENIED_OK.

Disposable PostgreSQL/MinIO run completed exit 0: 1 passed, 12 deselected in 230.45s. Added unit test independently rejects changed source parent UUID, canvas hash and descriptor path. Go semanticproposal/v2draft/v2input completed exit 0 (1.405s/cached/cached). An initial unit compile failed due to nonexistent ParamsContract constant; corrected to established "Params" contract before passing rerun.

This proves retained preflight integrity for these mutations, not atomic admission or full producer locking. All-declared-file transaction locks, original producer proof propagation and queued admission remain outstanding; V20–V32 remains incomplete.

### 2026-10-06 — V27 retained execution source transaction composition

Added lockDefectExecutionSourceInTransaction: validates retained canonical file/source/qualification evidence before SQL, requires photo_scheduler, sets transaction-local tenant context and locks original source closure then defect qualification in the caller transaction. Stops on every failure; caller owns rollback and must compose remaining producer/file/inventory/auth barriers. No object IO or queued authority is introduced.

Go semanticproposal/v2draft/v2input passed (1.244s/cached/cached). Unit test validates ordering and injected failure at all six SQL steps. Physical integration now invokes this helper on the original retained execution preparation and rolls back. Verification launched as session 67587; terminal result pending. Python mandatory marker assertion addition failed due to quoting mismatch; Go assertion was saved before launch and executes in selected fixture. Remaining declared files and producer closures are not locked by this helper. Full V20–V32 remains incomplete.

### 2026-10-06 — V27 retained execution transaction physical positive

Session 67587 completed exit 0: manual-lock-go-external 1 passed, 12 deselected in 180.22s. Original retained execution preparation passed source/qualification lock composition in PostgreSQL. Added the previously missing mandatory Python marker assertion. Expanded physical check now attempts concurrent source-pixels and qualification-report artifact UPDATE under 250ms lock_timeout: must fail 55P03 while held and succeed after rollback. This expanded concurrent-write check is not yet verified by this result.

No all-declared-file or remaining producer atomic locks claimed. Full V20–V32 incomplete.

Integration-tag semanticproposal compilation passed (0.513s, no tests executed). Expanded concurrent-write physical regression launched as session 13850; terminal result pending.

### 2026-10-06 — V27 physical concurrent source/qualification locks and original face producer proofs

Session 13850 completed exit 0: 1 passed, 12 deselected in 222.08s. Expanded retained execution transaction check rejects concurrent artifact UPDATE for source pixels and original defect qualification report with 55P03; both writes succeed after rollback. Mandatory marker assertion included.

ReadQualifiedDefectFaceSet now delegates to readQualifiedDefectFaceEvidence, which returns original private facePreparationProof for every deduplicated landmark/detection pair. Captures producer root/Params object pins and original qualification objects under exact principal/source scope; rechecks each before returning, and returns no partial faces/proofs on failure. Added two-face physical assertion expecting two original proof pairs and live rechecks. Go semanticproposal/v2input passed (1.270s/cached). New producer proof physical regression pending; immutable execution preparation propagation and transaction composition remain outstanding. Full V20–V32 incomplete.

Original face producer proof physical regression launched as session 27416; terminal result pending.

### 2026-10-06 — V27 original face proof retention and transaction composition

Session 27416 completed exit 0: 1 passed, 12 deselected in 175.60s. Two-face fixture proves exact original FaceLandmarks/FaceDetection producer proof pairs and live rechecks.

Defect face execution preparation now owns up to 64 original facePreparationProof values in a fixed private value array (matching existing selected-face lock batch limit), count and final producer rechecks. This retains exact producer root/Params and qualification object metadata with original principal/source scope, without mutable slice aliases. Added lockDefectExecutionFacesInTransaction: validates retained source file evidence and exact FaceLandmarks set/count/no duplicates/object metadata; validates original expectations/qualifications; composes source+defect qualification locks, existing face producer/qualification/inventory barrier locks, then repeats source+defect qualification locks. Caller owns rollback and remaining parser/manual/file/auth/admission composition.

Go semanticproposal/v2input passed after composition (1.320s/cached). Additional malformed-count/scope and cancelled-context tests deny before SQL. Physical fixture asserts retained producer values equal original proofs and invokes scheduler transaction composition; mandatory V27_DEFECT_FACE_EXECUTION_TRANSACTION_OK. This new composition physical regression remains pending. Full V20–V32 incomplete; no queued admission claimed.

Malformed/cancelled execution lock unit tests passed with semanticproposal (1.344s). Face execution transaction physical regression launched as session 61599; terminal result pending.

### 2026-10-06 — V27 physical face execution composition and retained parser proofs

Session 61599 completed exit 0: 1 passed, 12 deselected in 201.35s. Original face proofs match values retained by execution preparation and scheduler source/face qualification/producer/inventory lock composition succeeds. Existing concurrent source/qualification writer denial checks remain required.

ReadQualifiedDefectFaceMaskRoots now delegates to readQualifiedDefectFaceMaskEvidence. Captures exact original parser manifest/Params, detector document/Params, label pixels and both original qualification objects from verified skin dependency evidence; deduplicates by parse ref, rejects conflicting pins and rechecks private faceParsePreparationProof before return. Mask execution preparation retains proofs by value in a fixed private array and repeats live rechecks after later face/source/mask IO. Manual mask proof retention is separate and still outstanding.

Go semanticproposal/v2input passed before composition (1.268s/cached); integration-tag compilation after composition passed (0.633s, no tests executed). Added physical original parser proof count/value/recheck assertions and mandatory V27_DEFECT_MASK_ORIGINAL_PARSER_PROOFS_OK. New physical regression pending. Parser transaction composition and all-declared-file/queued admission remain incomplete; full V20–V32 incomplete.

Go semanticproposal/v2input after mask execution composition passed (1.522s/cached). Parser proof retention physical regression launched as session 89448; terminal result pending.

### 2026-10-06 — V27 original parser retention physical positive and transaction composition

Session 89448 completed exit 0: 1 passed, 12 deselected in 178.17s. Original parser proof count, retained value equality and live recheck assertions passed through mask execution preparation.

Added lockDefectExecutionParsersInTransaction. Before SQL validates bounded canonical retained four qualification expectations, face expectation/report consistency, canonical mask roots, skin-root/parser count consistency (using mask selector, independent of ManualMode), unique exact-scoped parser proofs and original qualification pins. Composes source/face locks, selected parser/labels/qualification/inventory barrier locks, repeats source/face locks. Caller must roll back on error; derived mask roots/pixels, manual evidence, remaining files/auth and queued admission are separate obligations.

Go semanticproposal/v2input passed after initial composition (1.276s/cached). Added missing/invalid count and cancellation rejection before SQL. Physical test invokes original retained parser preparation in scheduler transaction and requires V27_DEFECT_PARSER_EXECUTION_TRANSACTION_OK; new physical regression pending. Full V20–V32 incomplete.

Go semanticproposal/v2input after selector correction and missing/cancelled tests passed (1.247s/cached). Parser execution transaction physical regression launched as session 4357; terminal result pending.

### 2026-10-06 — V27 physical parser locks and owned manual execution proof

Session 4357 completed exit 0: 1 passed, 12 deselected in 199.57s. Retained parser execution transaction composition passed with mandatory marker and prior source/face/mutation gates.

Added private defectManualExecutionProof: full original manual preparation metadata plus optional vector stored by value, reconstructing a fresh vector pointer when consumed. readDefectManualExecutionProofs reads complete original closure for every manual selector in verified root inventory, requires exact root identity, deduplicates consistent target pins and repeats live closure rechecks. Mask execution preparation owns bounded private manual proof array and final rechecks; manual transaction composition is still outstanding. Unit mutation checks prove original and reconstructed vector pointers cannot change retained metadata.

Go semanticproposal/v2input after mask composition passed (1.244s/cached). Physical helper assertion compares canonical complete evidence pins against existing original manual preparation, requiring V27_DEFECT_MANUAL_EXECUTION_ORIGINAL_PROOFS_OK. New physical regression pending. Full V20–V32 remains incomplete.

Owned manual execution proof physical regression launched as session 42219; terminal result pending.

### 2026-10-06 — V27 retained manual transaction preflight and composition

Added decodeDefectExecutionManualProofs validating canonical bounded mask inventory, exact unique manual target/root set and retained count, pointer-free owned vector state, original scope and full canonical evidence pins. Both root and target object locations must match retained declared execution files with MaskRef/MaskPixels contracts; missing or changed locations deny before SQL.

Added lockDefectExecutionManualInTransaction: preflights owned complete manual proofs, composes parser/face/source locks, existing selected manual closure locks plus inventory barrier, then repeats parser/face/source locks. Caller owns rollback; other derived/files/auth barriers and queued admission are separate. Added incomplete/count/cancelled denial tests before SQL. Initial Go semanticproposal/v2input passed (1.243s/cached); subsequent expanded unit result recorded separately. Manual execution positive transaction regression is not yet implemented. Existing manual ownership physical session 42219 still pending; that scenario does not exercise these new methods.

Full V20–V32 remains incomplete.

Expanded manual transaction preflight unit tests passed: semanticproposal 1.321s, v2input cached. Session 42219 confirmed live by polling; no terminal result yet.

### 2026-10-06 — V27 manual execution ownership physical positive and transaction scenario

Session 42219 completed exit 0: 1 passed, 12 deselected in 174.95s. Original complete manual closure evidence pins matched owned manual execution proof; prior parser/face/source retention and lock assertions remained required. This result does not prove subsequently added manual transaction methods.

Expanded physical fixture now restores the original manual defect report temporarily, builds exact original manual Params/source/protection/landmark/root/target descriptors, prepares mask execution and requires one retained manual proof with no skin parser. Decodes exact retained manual target/root pins, invokes scheduler manual+parser/face/source transaction composition, requires concurrent target UPDATE denied with 55P03 and success after rollback, then restores report expiry expected by existing scenarios. Mandatory V27_DEFECT_MANUAL_EXECUTION_TRANSACTION_OK. Integration-tag compilation succeeded; new runtime result pending.

Remaining derived mask/file authority and queued admission still incomplete; full V20–V32 incomplete.

Integration-tag compilation completed exit 0 (0.539s, no tests executed). Manual execution transaction physical regression launched as session 55554; terminal result pending.

### 2026-10-06 — V27 manual execution fixture closure gap

Session 55554 failed exit 1: 1 failed, 12 deselected in 175.88s. Prior source/face/parser transaction and manual ownership markers passed; new original manual execution preparation rejected ErrInvalid before transaction. Fixture original manual Params explicitly includes protections=[manual_protected], but newly assembled profile descriptors omitted that required protected pixel input. Added exact original retained manual protected pixel with MaskPixels contract and original manual root parent; retained existing closure validator. No passing manual execution transaction claim yet.

Manual execution physical regression with required protected-pixel input launched as session 94553; terminal result pending. All-declared-file SQL lock remains unimplemented.

### 2026-10-06 — V27 physical manual transaction positive and declared-file SQL locks

Session 94553 completed exit 0: 1 passed, 12 deselected in 175.07s. Required protected pixel input corrected original manual execution fixture closure. Full manual execution preparation/retention/decode/transaction composition passed; concurrent target UPDATE was denied 55P03 and succeeded after rollback.

Added forward-only migration 00698 lock_semantic_defect_execution_files, scheduler-only execution with fixed search_path/owner. Authorizes existing canonical defect origin, rejects duplicate IDs and nonexact Params count, locks all declared artifact rows sorted by UUID and compares exact metadata/parent pins. Params retains origin binding; FaceLandmarks retains independent producer lookup; remaining explicit allowlist contracts retain v2_draft_artifact_scope authorization. Added private Go adapter consuming retained original file/descriptors only. This does not authorize new task Params or queue admission; producer/policy/inventory/auth composition remains required.

Physical manual transaction now also invokes declared-file locks with mandatory V27_DEFECT_DECLARED_EXECUTION_FILE_LOCKS_OK. New migration/runtime verification pending; full V20–V32 incomplete.

Go semanticproposal/v2input with declared-file adapter passed (1.464s/cached). Migration 00698 physical regression launched as session 13891; terminal result pending.

### 2026-10-06 — V27 execution inventory lock composition

Added lockDefectExecutionInventoryInTransaction: rejects missing retained manual inventory before SQL, locks exact declared files, composes retained source/face/parser/manual evidence and inventory barriers, repeats declared-file locks. Caller owns rollback and remaining original-origin/auth/global-policy/admission barriers; no object IO, commit or queue authority is introduced.

Go semanticproposal/v2input passed (1.813s/cached); incomplete and cancelled preparation tests deny before SQL. Migration 00698 physical session 13891 remains live; no result claimed. Newly added inventory composition is not yet exercised by that running fixture. Exact original defect origin metadata retention/locks, authorization/global policy composition and queued admission remain incomplete; full V20–V32 incomplete.

### 2026-10-06 — V27 declared-file lock migration physical positive

Session 13891 completed exit 0: 1 passed, 12 deselected in 237.86s. Disposable database applied migration 00698, scheduler adapter locked exact original manual execution declared files with mandatory marker, existing manual target writer rejection/rollback release assertions passed. This proves physical positive file lock call, not all-contract mutation denial or full admission.

Physical fixture now invokes complete lockDefectExecutionInventoryInTransaction in place of manual-only composition and requires V27_DEFECT_EXECUTION_INVENTORY_TRANSACTION_OK; retains explicit file adapter call and concurrent target denial/release. This new composition regression is pending. Full V20–V32 incomplete.

Complete selected defect execution inventory composition physical regression launched as session 57721; terminal result pending.

### 2026-10-06 — V27 execution inventory composition physical positive and retained defect origin

Session 57721 completed exit 0: 1 passed, 12 deselected in 189.11s. Complete selected execution inventory transaction composition passed with required marker, prior declared-file/manual/parser/face/source gates and concurrent manual target rejection/release.

Added private value-owned defectOriginPreparationProof retaining exact original Params/proposal/report/labels/protection object metadata with principal/project/source scope. Initial origin captured before face dependency verification and repeated after preparation; mask core repeats after later mask/manual IO. Validates exact ordered unique refs, immutable locations, SHA/media/byte limits and original source binding. Declared-file adapter rejects missing/foreign origin proof before SQL. Physical fixture requires exact origin pins and live metadata proof recheck. This is preflight only: SQL currently does not consume all five retained origin metadata locations; original producer transaction locks/global auth/policy/admission remain outstanding.

Go semanticproposal/v2input result and physical regression recorded separately. Full V20–V32 incomplete.

Go semanticproposal/v2input after origin retention composition passed (1.343s/cached). Origin retention physical regression launched as session 64707; terminal result pending.

### 2026-10-06 — V27 retained origin physical positive and producer transaction locks

Session 64707 completed exit 0: 1 passed, 12 deselected in 191.53s. Retained original defect origin pins and metadata recheck passed through face/mask execution preparation and prior selected execution inventory gates.

Added migration 00699 scheduler-only original defect evidence locks: original task, run, optional workflow, canonical result, exact attempt/fence, then all five original artifact rows ordered by UUID; canonical origin rechecked after locks and exact ordered role/ref/location/media/byte metadata matched. Added private Go adapter consuming only scoped retained proof. Unit scope/invalid location/media/duplicate-ref and every SQL failure stop checks passed (semanticproposal 1.414s, v2input cached).

Execution inventory composition now locks/rechecks original producer proof before and after declared-file and selected source/face/parser/manual phases. New mandatory V27_DEFECT_ORIGIN_EXECUTION_TRANSACTION_OK. This newly connected migration/composition physical regression pending. Authorization/global policy barriers and queued admission remain incomplete; full V20–V32 incomplete.

Migration 00699 and connected original producer transaction physical regression launched as session 31888; terminal result pending. No concurrent producer-row writer probe or migration ACL-denial coverage claimed yet.

### 2026-10-06 — V27 origin transaction physical positive and policy snapshot composition

Session 31888 completed exit 0: 1 passed, 12 deselected in 192.19s. Migration 00699 and connected original defect producer metadata locks passed the full selected inventory scenario. Expanded fixture now requires concurrent original report artifact and task_attempt profile no-op UPDATE denied with 55P03, then both succeed after rollback; mandatory V27_DEFECT_ORIGIN_CONCURRENT_WRITE_DENIED_OK. Integration-tag compilation passed (0.610s, no tests executed). Expanded physical regression launched as session 19580; terminal result pending.

Added matchDefectExecutionPolicySnapshot matching defect/landmark/detector/parser expectations to one original DBA snapshot; mixed detector expectations or missing parser admission deny. Added lockDefectExecutionPolicyInventoryInTransaction composing selected inventory locks, full policy snapshot lock and repeated selected inventory. This helper is not yet physically connected/tested; final authorization/admission remain separate. New matching unit result recorded separately. Full V20–V32 incomplete.

Policy snapshot matching unit tests passed: semanticproposal 1.349s, v2input cached. No policy composition physical result claimed.

### 2026-10-06 — V27 origin concurrent writer physical positive and owned policy execution

Session 19580 completed exit 0: 1 passed, 12 deselected in 160.33s. Original defect report artifact and task_attempt profile UPDATE probes denied 55P03 while selected execution inventory locks held; both succeed after rollback. Mandatory origin concurrent writer marker passed.

Added private comparable defectPolicyExecutionPreparation retaining complete mask/source/producer execution plus original full qualification policy snapshot. Wrapper owns descriptors and expectations before catalog IO, validates planned expectations against original snapshot, prepares all evidence, matches retained expectations and repeats exact full snapshot before success. Transaction helper consumes this retained snapshot via existing policy/inventory composition. No external or client policy JSON is promoted to queue authority.

Go semanticproposal/v2input passed (1.273s/cached); integration-tag compilation result recorded separately. Physical manual fixture now uses policy-qualified preparation and transaction adapter, requiring V27_DEFECT_POLICY_EXECUTION_TRANSACTION_OK and prior file/source/producer/concurrent writer gates. New runtime result pending. Final authorization/admission, new-task Params publication, durable workflow/queue/API/client continuation and full V20–V32 remain incomplete.

Integration-tag compilation completed exit 0 (0.551s, no tests executed). Policy-qualified execution physical regression launched as session 27625; terminal result pending.

### 2026-10-06 — V27 policy execution physical positive and principal scope locks

Session 27625 completed exit 0: 1 passed, 12 deselected in 171.30s. Original full policy snapshot retained through manual execution preparation, matched selected expectations and held through policy/inventory transaction composition; prior origin concurrent writer denial/release and declared-file gates remained required.

Added migration 00700 scheduler-only live principal/project/source scope locks: active tenant and user, exact unrevoked/unexpired session, membership, project and optional ACL, source asset/version/artifact. Rechecks editor authorization and wall-clock session expiry after locks. Added Go adapter consuming exact scoped retained unitRGB source closure. Authorized policy execution now invokes scope locks before and after full selected data/policy composition. Caller still owns atomic admission/rollback. Added original-scope, foreign session/tenant, missing closure, unitRGB denial and each SQL failure stop tests.

Go adapter test results recorded separately. Physical fixture requires V27_DEFECT_AUTHORIZED_EXECUTION_TRANSACTION_OK; new migration/runtime result pending. Queued admission/new-task publication/workflow/API/client and full V20–V32 remain incomplete.

Authorization adapter unit tests passed (semanticproposal 1.412s, v2input cached); post-composition Go result and migration 00700 runtime session recorded separately. No ACL denial/concurrent principal revocation coverage claimed yet.

Post-authorization-composition semanticproposal/v2input passed (1.247s/cached). Migration 00700 authorized execution physical regression launched as session 6312; terminal result pending.

### 2026-10-06 — V27 authorization lock physical positive and principal writer probes

Session 6312 completed exit 0: 1 passed, 12 deselected in 163.21s. Migration 00700 applied in disposable database; policy execution preparation/inventory composition succeeded with initial/final live principal/project/source scope locks. Existing declared-file, policy, producer and concurrent origin writer gates passed.

Expanded writer probe list now includes original active user status and exact session revoked_at UPDATE, requiring 55P03 while transaction held and success after rollback. Mandatory V27_DEFECT_AUTHORITY_CONCURRENT_WRITE_DENIED_OK. New probes not yet verified. New-task Params publication still requires authorized graph-owner/fencing design and implementation rather than relaxing existing canonical-origin catalog. Full V20–V32 incomplete.


### 2026-10-06 — V27 principal writer regression started

Integration-tag compilation completed exit 0 (0.620s, no tests executed). Physical manual-lock-go-external regression launched as session 47935 to verify concurrent user/session UPDATE rejection while authorized execution locks are held and release after rollback. Terminal result pending; full V20–V32 remains incomplete.


### 2026-10-06 — V27 principal writer locks verified; execution ACL regression

Session 47935 completed exit 0: 1 passed, 12 deselected in 202.43s. Concurrent original active-user status and exact session revoked_at updates were rejected with 55P03 while the authorized execution transaction held locks; both succeeded after rollback. Mandatory V27_DEFECT_AUTHORITY_CONCURRENT_WRITE_DENIED_OK passed.

Added real SQL invocation denial probes for all three functions introduced by migrations 00698–00700 across eight non-scheduler service roles, requiring 42501 and mandatory V27_DEFECT_EXECUTION_LOCK_ACL_OK. Integration-tag compilation passed (0.640s, no tests executed). New ACL probes remain unverified until physical regression completes. Full V20–V32 remains incomplete.


Execution-lock ACL physical regression launched as session 53982; terminal result pending.


### 2026-10-06 — V27 execution SQL ACL verified; scope substitution probes

Session 53982 completed exit 0: 1 passed, 12 deselected in 202.32s. All three scheduler-only gates from migrations 00698–00700 denied real invocation with 42501 for each of eight other service roles; mandatory V27_DEFECT_EXECUTION_LOCK_ACL_OK passed.

Added scheduler SQL-level negative probes independently of Go retained-proof preflight: substituted tenant, project, actor, session, asset version, source artifact, source digest, and mismatched transaction tenant context must all return false. Each probe rolls back its own transaction. Mandatory V27_DEFECT_EXECUTION_SCOPE_SUBSTITUTION_DENIED_OK is pending physical verification. Full V20–V32 remains incomplete; new defect-task Params publication still needs its own authorized graph owner/fence, distinct from final assembly Params.


Integration-tag compilation passed (0.647s, no tests executed). SQL scope substitution physical regression launched as session 11716; terminal result pending.


### 2026-10-06 — V27 execution scope SQL negative verification

Session 11716 completed exit 0: 1 passed, 12 deselected in 185.61s. Eight direct scheduler-role SQL probes returned false for substituted nonexistent tenant/project/actor/session/version/source IDs, incorrect source SHA, and mismatched transaction tenant context. Each negative transaction was rolled back. Mandatory V27_DEFECT_EXECUTION_SCOPE_SUBSTITUTION_DENIED_OK passed together with original positive execution, eight-role ACL denial, producer/principal concurrent writer rejection and rollback release. These probes do not establish every cross-tenant case with real foreign objects or full queued admission.

Current new-task publication investigation: migration 00505 scopes task_params to the final v2.semantic.assemble expansion; its single owner/purpose document cannot also own defect Params. Legacy review.defects expansions are repair branches of completed defect reviews, not initial proposal-task ownership. New proposal-task Params need their own authorized owner and fencing linked to retained preparation and input registration; this remains unimplemented. Full V20–V32 remains incomplete.


### 2026-10-06 — V27 immutable Params preparation/publication split

Added PreparedDefectParams with private immutable canonical Params, digest and input/manual-root strings; PrepareDefectParams compiles once from original WorkingImage and native defect inputs. PublishPreparedDefectParams decodes fresh dependency collections, validates bounds/digest/scope before publication, checks returned digest and input aliasing, and preserves cancellation/error empty-result behavior. CompileAndPublishDefectParams delegates through these phases, preserving existing owner-scoped publisher enforcement. This permits retaining compiled payload across preparation phases without exposing mutable Params/dependency collections; it does not create new graph ownership or queued authority.

Go semanticproposal/v2input passed initially (1.384s/cached) and after tests (1.442s/cached). New tests mutate a prior publication body and returned input refs, then verify a second publication from the same prepared value preserves exact original canonical bytes/dependencies; zero and incorrect digest preparations are rejected before publisher calls. Full V20–V32 and native new-task owner/fencing/registration remain incomplete.


Integration-tag compilation passed (0.559s, no tests executed). Post-Params-split physical regression launched as session 50258; terminal result pending.


### 2026-10-06 — V27 immutable manual Params and original proofs

Session 50258 completed exit 0: 1 passed, 12 deselected in 182.23s. Existing SQL publication denial and execution authority/data/policy regressions passed after Params preparation/publication split.

Added private defectManualParamsPreparation: exact compiler output frozen as PreparedDefectParams alongside at most 64 original manual evidence proofs, retaining external vector metadata by value. prepareManualDefectParams delegates to complete existing native source/manual preparation and returns zero on errors. Both pure and catalog/manual preparation now use freezeCompiledDefectParams with digest/bounds/context checks. New buffer-mutation, changed-digest, empty-output and cancellation tests passed (semanticproposal 1.463s, v2input cached) before the shared-constructor refactor; subsequent checks recorded separately. Added mandatory physical V27_DEFECT_MANUAL_PARAMS_RETAINED_OK comparing exact original Params/digest and retained manual-root proof. New physical coverage remains pending. This still does not grant new task owner/lease/queue authority or complete V20–V32.


After shared-constructor and manual fixture integration, Go semanticproposal/v2input passed (1.619s/cached); integration-tag compilation passed (0.534s, no tests executed). Native immutable manual Params physical regression launched as session 35693; terminal result pending.


### 2026-10-06 — V27 manual Params closure recheck and fixture routing fix

Session 35693 completed exit 1: Python mandatory V27_DEFECT_MANUAL_PARAMS_RETAINED_OK missing; underlying Go scenario passed but manual-lock-only returned before the newly inserted branch. This run does not verify immutable manual preparation.

Added recheckManualDefectParams: bounds/digest and exact manual-root/dependency closure preflight, original scope and complete proof checks before catalog IO, then every retained original manual proof live recheck. Missing or conflicting proofs return invalid; no publication/queue authority. Go semanticproposal/v2input passed (1.377s/cached) before fixture integration. New scenario moved into executed policy/manual block while original report is live, reads pinned original Params, prepares exact matching immutable payload, rechecks closure, and rejects lost proof count. Mandatory retained/recheck markers remain pending rerun. Full V20–V32 incomplete.


Integration-tag compilation passed (0.550s, no tests executed). Corrected native manual Params preparation/recheck physical regression launched as session 73431; terminal result pending.


### 2026-10-06 — V27 native manual Params physical verification and source retention

Session 73431 completed exit 0: 1 passed, 12 deselected in 187.36s. Correct executed branch verifies exact original immutable manual Params/digest, original manual closure live recheck, and missing-proof rejection, with both mandatory retained/recheck markers.

Manual Params preparation now captures original qualified unit-RGB sourcePreparationProof before native manual compilation and repeats it after the batch. Recheck requires exact tenant/project/principal/session/bucket/source binding and complete retained source pins, then repeats live source checks before and after manual closure reads. This closes the previously absent source retention, including no-manual preparations. New source-retention runtime result pending. Full V20–V32 and new task owner/fencing/queue admission remain incomplete.


Source-retaining manual Params Go checks passed (semanticproposal 1.405s, v2input cached); integration-tag compilation passed (0.548s, no tests executed). Physical regression launched as session 31596; terminal result pending.


### 2026-10-06 — V27 source-retained Params verified; native publication boundary

Session 31596 completed exit 0: 1 passed, 12 deselected in 159.91s. Native manual Params/source proof preparation and before/after source/manual closure rechecks passed original fixture and missing-proof denial.

Added publishManualDefectParams connecting exact frozen native payload to existing DocumentPublisher with original source/manual closure rechecks before and after publication, strict matching tenant/purpose/nonzero run/owner, cancellation and empty results on failure. SQL publisher remains owner/fencing authority; no queued task is created. Go semanticproposal/v2input passed (1.233s/cached) before fixture integration. Physical scenario now invokes real scheduler SQL publisher with unregistered owner, requires ErrStale, no object PUT and zero backend document reservations; mandatory V27_DEFECT_NATIVE_PARAMS_OWNER_DENIED_OK pending. Full V20–V32 and authorized new-owner creation remain incomplete.


Native publication integration-tag compilation passed (0.547s, no tests executed). Physical scheduler SQL publication-owner denial regression launched as session 17439; terminal result pending.


### 2026-10-06 — V27 native Params SQL publication boundary verified

Session 17439 completed exit 0: 1 passed, 12 deselected in 157.76s. Native publishManualDefectParams consumed frozen Params with original source/manual rechecks; real scheduler SQL publisher rejected the unregistered graph owner with ErrStale, object PUT was not called, and backend_documents reservation count remained zero. Mandatory V27_DEFECT_NATIVE_PARAMS_OWNER_DENIED_OK passed together with retained source/manual preparation, closure rechecks, missing-proof denial, transaction locks, ACL and scope-substitution gates.

This proves the denied-owner path only. Successful new-owner publication, post-publication revocation regression, defect task creation/input registration and queued admission remain unimplemented or unverified. Legacy review.defects expansions describe repair branches of completed reviews; final semantic assembly owns its own Params document. Neither existing owner is authority for arbitrary initial defect proposal tasks. Full V20–V32 remains incomplete.


### 2026-10-06 — V27 native Params accepted-version document publication

Added v2draft.Publisher.PublishDefectProposalParams: verifies exact WorkingImage digest/strict float32 working profile/source metadata, canonical native Params and complete declared inputs before using the existing accepted-version draft document publication lifecycle (00627/00628). Document identity is stable across retries, existing quota/fencing/cleanup retained. This is source-version scoped document ownership, not legacy review repair or final assembly expansion ownership. It does not create a task or authorize queue admission.

Added private publishManualDefectVersionParams consuming frozen original source/manual preparation, rechecking all retained source/manual evidence before and after publication, reading the original source document, validating returned digest/nonaliasing, and returning empty on failure. Physical fixture now requires successful real API-role document publication and replay returning identical Params artifact under one stable document ID; mandatory V27_DEFECT_VERSION_PARAMS_PUBLICATION_OK. Go v2draft/semanticproposal passed (3.189s/1.441s initial, cached/1.176s after integration); integration-tag compilation passed (0.544s/0.952s, no tests executed). Physical regression launched as session 89702; terminal result pending.

Success of this newly added path remains unverified until terminal runtime result. Earlier blocked owner path remains intact. Task graph creation, full producer/person/policy integration, input registration and final queue admission are still incomplete; full V20–V32 remains incomplete.


### 2026-10-06 — V27 successful new Params publication and exact metadata evidence

Session 89702 completed exit 0: 1 passed, 12 deselected in 203.32s. Real API-role accepted-version native Params publication succeeded and replay returned identical artifact/ref, with mandatory V27_DEFECT_VERSION_PARAMS_PUBLICATION_OK. Source/manual retained proof checks and denied legacy owner path remained intact.

Added private defectVersionParamsProof capturing exact new document identity/ref/object metadata and immutable native preparation. Read path validates returned input/root inventories, reads exact pinned Params object, requires original frozen bytes, repeats document metadata and source/manual checks. Added forward-only migration 00701: API-only exact source-version/project/principal/document Params catalog function and scheduler-only transaction lock for source authority, document row and new artifact exact metadata. New Go lock adapter consumes private retained proof; caller owns broader producer/policy/manual locks and admission. No purge allowlist changes/new tables or production migrations.

Go semanticproposal/v2draft passed (1.358s/cached); integration-tag compilation passed (0.641s/cached, no tests executed). Physical fixture now reads new Params proof and locks it in scheduler transaction, requiring V27_DEFECT_VERSION_PARAMS_PROOF_LOCK_OK. Migration 00701 and new runtime scenario remain pending. Full V20–V32 incomplete.


Migration 00701 and new Params metadata proof/lock physical regression launched as session 54019; terminal result pending.


### 2026-10-06 — V27 new Params metadata proof physical verification and native task compiler

Session 54019 completed exit 0: 1 passed, 12 deselected in 173.61s. Disposable database applied migration 00701; real accepted-version Params proof read exact object version/body, repeated source/manual/document scope, and scheduler transaction locked source authority plus document/artifact metadata with mandatory V27_DEFECT_VERSION_PARAMS_PROOF_LOCK_OK. Concurrent new-document writer/ACL denial coverage not yet claimed.

Added v2input.FrozenSemanticDefectTask and CompileSemanticDefectTask: deterministic workflow/operation/ordinal identity, strict original float32 WorkingImage/binding/canonical Params, native declared dependencies, explicit contract set and unique entries, exact authorized file sizes including new Params, CPU resource profile from existing conservative estimator. Owns Params/profile/input cursor. DraftTask returns CPU defects.propose/1 without queue authority. PublishSemanticDefectTask builds immutable input transport and binds only the frozen cursor, using shared private binding validation also used by transfer tasks. No existing task activation behavior was broadened.

Tests verify deterministic identity/digest, independent returned profile, caller buffer/inventory mutations, native transport publication and changed transport SHA denial. Go v2input/semanticproposal/v2draft passed (8.461s/1.844s/3.268s then 6.439s/1.383s/cached after expanded tests). Physical fixture now compiles native task from actual new Params proof and original authorized declared-file metadata, requiring V27_DEFECT_PUBLISHED_TASK_COMPILE_OK. This new runtime scenario is pending. SQL task creation/input registration/final admission and full V20–V32 remain incomplete.


Integration-tag compilation passed (semanticproposal 0.544s, v2input 0.961s, no tests executed). Native published defect task compilation physical regression launched as session 81695; terminal result pending.


### 2026-10-06 — V27 actual new Params task compilation verified; composed transaction

Session 81695 completed exit 0: 1 passed, 12 deselected in 169.84s. Native defects.propose frozen task compiled from actual newly published Params and exact previously authorized file metadata, with CPU draft/ref checks and mandatory V27_DEFECT_PUBLISHED_TASK_COMPILE_OK. No SQL task creation/activation was exercised.

Added lockPublishedDefectExecutionInTransaction: exact retained source proof equality, native input membership in original qualified file inventory and nonaliasing, new manual proof membership in complete original qualified manual set, full policy match, original authorized execution lock composition, exact new Params metadata lock, then repeated original execution locks. Caller owns separate task/input admission and rollback. This composes new Params authority with existing original producer/source/manual/parser/face/policy stability; final new Params required-protection/content policy admission remains a separate obligation. New mandatory V27_DEFECT_PUBLISHED_EXECUTION_LOCKS_OK physical coverage pending. Full V20–V32 remains incomplete.


New published Params/original execution composition Go checks passed (v2input cached, semanticproposal 1.982s, v2draft cached); integration-tag compilation passed (0.835s/cached, no tests executed). Physical regression launched as session 3087; terminal result pending.


### 2026-10-06 — V27 blocked native preparation draft

Session 3087 completed exit 1 in 328.02s: underlying Go context reached its 300s suite deadline during original manual Params closure recheck, before the newly composed execution transaction gate. That gate remains unverified. The expanded manual-lock-only scenario now has context480s, Go process540s, Python subprocess570s; worker/pipeline timeouts and all validation/resource bounds are unchanged.

Added private defectPreparationDraftPlan compiling a deterministic reference preparation child run/workflow from exact original request, intent, target snapshot, retained qualified inventory, source/manual preparation, exact new Params metadata, native task and conservative reservation (output plus8MiB transport headroom). Measured qualification costs cannot be understated. Source/manual and Params metadata repeated around preparation. Native task/config/snapshot/request/key identity is preflight checked before SQL. beginDefectPreparationDraftInTransaction acquires inventory locks in tenant/usage order, then intent/snapshot/combined original execution/new Params locks, and uses existing V2 admission idempotency to create/resume only blocked unsealed draft. No queue service/activation/HTTP ticket introduced.

Physical fixture now creates and pending-replays the blocked draft under photo_scheduler in one rollback-only owner-backed test transaction, then returns to test-owner visibility to assert draft/unsealed/blocked state without broadening production table grants. New mandatory V27_DEFECT_BLOCKED_DRAFT_CREATE_REPLAY_OK pending. Go semanticproposal/v2input/v2draft passed (2.625s/cached/cached initially,1.142s/cached/cached after plan integration). Full V20–V32 remains incomplete.


Blocked defect stage integration-tag compilation passed (semanticproposal0.714s, v2input cached, no tests executed). Expanded physical regression with context480s/process540s/subprocess570s launched as session1148; terminal result pending.

### 2026-10-06 — native defect draft quota and complete large input transport

Physical session 1148 failed (1 failed, 12 deselected, 161.60s) after published execution locks and task compilation passed: create_v2_draft_shell correctly rejected a 1 GiB reservation against a 1 GiB tenant limit with existing usage. The new integration scenario now reserves 64 MiB, above its calculated output plus mandatory 8 MiB headroom; production quota guards are unchanged. A repeat physical run is pending.

CompileSemanticDefectTask now exposes only Params, WorkingImage and WorkingPixels as sorted direct bootstrap refs. Every declared dependency remains in the frozen cursor, digest and input manifest. This removes the incompatible all-input direct list for inventories above the transport limit of 127. Regression compiles and publishes a 64-face, 131-entry inventory with exactly three direct refs; targeted test passed (0.475s). Go v2input and semanticproposal packages passed before the new regression (7.986s / 1.498s). Native draft registration/replay, final admission and full V20–V32 remain incomplete.

Physical repeat session 1694 completed PASS: 1 passed, 12 deselected in 193.42s. This confirms the mandatory V27_DEFECT_BLOCKED_DRAFT_CREATE_REPLAY_OK gate together with published Params proof, combined original qualified execution locks and frozen task compilation. The earlier quota refusal was a fixture reservation issue, not a reason to relax admission. The isolated test started before the direct-reference change; the new 131-entry bootstrap transport is verified by its separate Go regression, not by this physical run. Full native input registration and queue activation remain pending.

### 2026-10-06 — native defect input registration and exact replay gate

Added forward-only migration 00702: scheduler-only semantic_defect_preparation_registration_state, distinct from the existing semantic.transfer gate. It accepts 3..4097 complete input entries and exactly three bootstrap refs, requires the native v27_defect_prepare config/task/input digest/Params binding, an unsealed draft and blocked unattempted task. Missing is returned only for wholly absent input state; replay requires exact ordered entries, profile, manifest/chunks/direct refs, ready input closure and task-owned ready backend transport documents. Partial or mismatching state is conflict; no queue activation or metadata repair occurs.

registerDefectPreparationInputsInTransaction binds transport to the frozen inventory before SQL, reuses the draft entry point to reacquire the complete original inventory/intent/snapshot/qualified source-policy-producer/manual and published Params locks, then registers missing inputs and requires an exact replay result afterward. Replays make no additional registration writes. Caller must roll back errors. Unit packages passed (semanticproposal 1.411s, v2input cached). Physical session 89700 is pending, covering committed blocked draft, actual MinIO transport publication and two registrations in one scheduler transaction. Full final admission/activation and V20–V32 remain incomplete.

Physical session 89700 failed: 1 failed, 12 deselected in 163.32s. Committed blocked draft and physical input transport publication succeeded; the first registration returned V2 admission idempotency conflict. Registration/replay is not yet proven. Added contextual error wrapping to distinguish draft authority, pre-write replay state and post-write replay state, preserving the original sentinel via %w. Added mandatory Python assertion for the new registration gate. Next action is diagnose the exact failing equality and retain all strict scope/closure checks.

### 2026-10-06 — exact defect profile and registered draft recovery

Diagnostic physical session 63553 failed (1 failed, 12 deselected, 178.89s), explicitly locating conflict in post-write registration replay state. SQL seal adds renderer_version v2.2; the native defect profile previously omitted it, so strict replay correctly refused equality. CompileSemanticDefectProfile now pins renderer_version v2.2 before draft creation; capacity test explicitly checks it. No profile fields are ignored to permit replay. Go packages passed after compiler change (v2input 8.346s, semanticproposal 1.673s).

Added forward-only migration 00703 for registered native defect draft recovery. Only a one-task semantic draft with v27_defect_prepare schema 1 and exact task/digest/Params binding may substitute its fully registered transport profile after verifying the frozen base profile and ready closure. The original strict shell matcher still compares configuration, workflow, tasks, edges and reservation exactly. All other workflows delegate to the existing wrapper. Physical session 65373 is pending; no successful native registration gate is claimed yet. Final queue activation and full V20–V32 remain incomplete.

Physical session 65373 completed PASS: 1 passed, 12 deselected in 217.24s. Mandatory V27_DEFECT_INPUT_REGISTRATION_REPLAY_OK passed with actual committed blocked draft, physical MinIO input chunks/manifest, first scheduler registration, strict post-write equality and second registration through the registered-shell recovery path. The registration transaction is rolled back by this fixture; durable registration across a new transaction/restart remains unverified. No queue activation is performed or claimed. Go v2input/semanticproposal passed (7.904s/cached). Next work: durable registration replay, negative scope/ACL/partial-state coverage, current new-Params content/person/required-protection policy and final queue admission; full V20–V32 remains incomplete.

### 2026-10-06 — durable defect registration recovery and persistence coordinator

Extended the physical fixture to commit input registration, reopen through a separate scheduler pool/connection and replay in a fresh transaction. It then substitutes the manifest UUID (same frozen entry inventory) and requires ErrConflict, followed by test-owner confirmation that the durable run remains draft/unsealed and the task blocked. Mandatory gates: V27_DEFECT_INPUT_DURABLE_REPLAY_OK and V27_DEFECT_INPUT_MANIFEST_SUBSTITUTION_DENIED_OK. Physical session 3845 is pending.

Added persistDefectPreparationInputs: DB-only retryable draft transaction, external immutable frozen transport publication, then DB-only retryable registration transaction. Uses existing deterministic task/document identities and strict source/policy/manual/Params locks; failures retain a resumable draft. This helper does not admit/activate the task and is not yet wired to the public HTTP workflow. Integration compilation passed (1.158s). Physical fixture exercises underlying stages, not this new coordinator helper. Full V20–V32 remains incomplete.

Physical session 3845 completed PASS: 1 passed, 12 deselected in 230.95s. Mandatory durable replay and manifest substitution denial gates passed. Registration committed, resumed on a new scheduler pool/transaction, and substituted manifest was rejected with ErrConflict while persisted run/task remained draft/unsealed/blocked. Existing original evidence/qualification/source/manual/writer checks in the selected scenario also completed. Go packages with the persistence coordinator passed (semanticproposal 1.333s, v2input cached). The coordinator helper itself still requires physical coverage and public workflow integration; queue activation and the full V20–V32 objective are not complete.

### 2026-10-06 — native preparation coordinator physical coverage and protection-content verifier

Physical fixture now rolls back its initial creation/replay inspection, then invokes persistDefectPreparationInputs to create a fresh durable draft, publish real transport and commit input registration. It later invokes the coordinator again after durable registration/recovery and manifest substitution denial, requiring identical manifest and input digest. Mandatory gate V27_DEFECT_PERSISTENCE_COORDINATOR_OK; session 46501 pending.

Added review.VerifyDefectRequiredProtections: canonical source-bound native Params are decoded through the existing exact mask parser, then every trusted required protected mask must remain with identical full binding. Missing/substituted/conflicting protections are rejected; cancellation and bounded required set are checked. This helper does not grant catalog/SQL authority and is not yet wired to the preparation/admission path. Initial unit fixture lacked permanent protection; test now explicitly constructs a source-bound required mask instead of assuming the fixture contains one. Queue admission and full V20–V32 remain incomplete.

Physical coordinator session 46501 completed PASS: 1 passed, 12 deselected in 203.63s. Coordinator first invocation created a fresh durable draft, published real transport and committed registration; second invocation preserved manifest and input digest. Durable replay and substitution denial gates also passed.

Required protection verifier is now wired into prepareDefectPreparationDraft: before/after checking original producer proof, read exact original Params, derive all protected masks from validated source-bound content, verify newly published Params retain every full binding, then retain canonical required mask/source bytes privately in the plan. beginDefectPreparationDraftInTransaction repeats this content verification before inventory/SQL locks; original source/producer/policy and Params locks remain mandatory. This is preparation-stage content preservation, not full final queued admission. Unit packages passed (review 0.657s, semanticproposal 1.186s, v2input cached). Physical session 80448 is pending for the wired change.

Physical wired-protection session 80448 completed PASS: 1 passed, 12 deselected in 214.51s. The selected scenario now exercises required-protection preservation during plan construction and every draft/registration/coordinator replay, including actual durable transport publication and manifest substitution denial. Targeted required-protection negative regression passed (0.489s): dropped mask, substituted identity, changed binding and cancellation. No image-processing algorithm changes. These checks do not establish native face/person content qualification or final queued admission; full V20–V32 remains incomplete.

### 2026-10-06 — native Params preserve qualified face bindings

Added review.VerifyDefectQualifiedFaceBindings: bounded source-bound Params are parsed by the exact existing face parser, then the complete ordered face inventory must match the independently qualified original face bindings. Each canonical face binding includes landmark ref/hash, nullable object identity, skin/manual mask geometry and provenance, and manual mode. Dropped, reordered or substituted faces are rejected; empty qualified face sets remain valid. The helper establishes content matching only; existing original producer/qualification/source locks are still mandatory.

prepareDefectPreparationDraft derives qualified face bindings from the exact pinned original Params between original producer rechecks, verifies new published Params, and retains canonical face JSON in its private plan. beginDefectPreparationDraftInTransaction repeats verification before SQL inventory/admission operations. This preparation stage reuses a canonical existing qualified producer; discovery/inference for a truly initial native proposer remains separate unfinished work. Unit packages passed (review 0.756s, semanticproposal 1.898s, v2input 10.509s), including missing face, changed landmark and face order regressions. Physical coordinator session 46103 pending. Full final queue/person admission and V20–V32 remain incomplete.

Physical session 46103 completed PASS: 1 passed, 12 deselected in 216.10s. The existing real manual-protection/source-qualified fixture now passes native face content comparison during plan construction, initial durable coordinator execution, registration recovery, substituted manifest denial and coordinator replay. Negative content regressions are unit-level; cross-tenant native Params and new gate denied-role scenarios still require dedicated physical coverage. No queue activation or worker processing occurs in this test; final native person/policy admission and initial producer discovery remain incomplete.

### 2026-10-06 — physical privileges and native registration state substitutions

Extended actual denied-role invocation matrix to lock_semantic_defect_version_params (00701) and semantic_defect_preparation_registration_state (00702). The eight API/worker/publisher/portal/auth/gateway/maintenance/semantic-admission roles must receive SQLSTATE 42501. No grants were expanded. Added mandatory gate V27_DEFECT_NATIVE_PARAMS_REGISTRATION_ACL_OK.

After durable coordinator success, registration-state helper first proves exact replay under scheduler and tenant context, then changes run ID, Params hash, input digest and task ID individually and requires conflict. These are scope/identity substitutions against existing registered state, not a claim of real foreign-tenant object isolation. Mandatory gate V27_DEFECT_REGISTRATION_STATE_SUBSTITUTION_DENIED_OK. Integration compilation passed (0.631s). Physical session 43042 pending; full V20–V32 remains incomplete.

Next native person admission must use one real source-bound person coverage proof and its matching DBA mask.person policy, link its protected pixel to exact native Params, then hold protection/person inventory and qualified producer locks. Existing LockPersonProducerInTransaction requires two proofs per semantic.transfer task; duplicating proofs solely to satisfy that interface would be a false transfer model. Extracting the shared per-proof locking logic while preserving the original transfer cardinality is the appropriate reuse path. No native person gate or queue activation is claimed here.

Physical session 43042 completed PASS: 1 passed, 12 deselected in 221.41s. Both new functions denied the eight non-scheduler roles with actual SQLSTATE 42501. Exact registered state replayed, while changed run, task, Params hash and input digest each returned conflict. Existing durable coordinator and content checks in this scenario also passed.

Extracted lockPersonProtectionProofsInTransaction from LockPersonProducerInTransaction. The transfer entry point preserves its original two-proofs-per-task requirement and delegates the unchanged scope validation, scheduler role check, deterministic dedup/order, protection/person/qualification locks, inventory barrier and candidate rediscovery. Shared helper accepts real independently prepared proofs for future one-source native admission; it adds explicit bounded proof count and nonzero actor/session/tenant/project preflight. The physical session above began before this refactor and is not evidence for its runtime path. Unit verification recorded separately; native person admission and queue activation remain incomplete.

### 2026-10-06 — native defect person coverage preparation

Added defectPersonAdmissionPreparation and validate/prepare/lock helpers. Validation requires exact source/version/principal/protection scope, a valid original DBA policy snapshot and matching mask.person expectation, then verifies the independently covered protected pixel ref/hash is actually used by a protected mask in exact native Params. Preparation reruns person candidate discovery, qualification and coverage against original protection proof and requires full retained-proof equality. SQL composition reuses exact blocked draft locks, one real person coverage proof through the shared lock helper, and final original defect/source/policy locks. No synthetic duplicate transfer proofs, queue activation or worker dispatch occur.

Unit packages passed (semanticproposal 1.575s, review/v2input cached; later snapshot preflight semanticproposal 1.537s). Dedicated content test passed (0.759s): matching native protected mask is accepted; missing person-protected pixel is rejected by validation and preparation before any catalog IO (reader has no DB/object dependencies). Physical session 80563 pending, covering real person preparation, source-version substitution rejection and SQL person locks alongside durable native coordinator. These helpers are not yet wired to a public final admission endpoint; full V20–V32 remains incomplete.

Physical session 80563 completed PASS: 1 passed, 12 deselected in 300.59s. Mandatory V27_DEFECT_PERSON_ADMISSION_PREPARATION_LOCK_OK passed: real mask.person qualification/coverage rediscovery matched retained proof, the native Params contained its protected pixel, a different source-version proof was rejected, and shared protection/person/qualification/inventory/candidate locks composed with the registered native draft and original policy/source locks. The shared lock refactor is now physically exercised by both prior transfer-proof checks and the native one-proof path in this scenario. These are fixture-backed protocol checks, not real model corpus/hardware qualification. Final native queue admission, HTTP integration and full V20–V32 remain incomplete.

### 2026-10-06 — native graph activation bypass guard

Inspection found generic seal_v2_graph checks registry/transport/quota but does not require the new native defect source/person/content/policy admission composition. Added forward-only migration 00704: renamed existing implementation to seal_v2_graph_pre_native_defect and revoked scheduler/public execution; wrapper returns false for v27_defect_prepare config or any defects.propose task, delegating other graphs unchanged. This closes the generic activation path until a dedicated complete native authorization/seal function is implemented. It does not enable native queue admission, alter Apply, expand delete privileges or assert model readiness.

Physical fixture calls generic seal as scheduler on the actual registered native draft and requires false, then calls the underlying function directly and requires SQLSTATE 42501. Mandatory gate V27_DEFECT_GENERIC_SEAL_BYPASS_DENIED_OK. Session 7814 pending. The generic false check alone does not prove the graph was otherwise registry-eligible; dedicated qualified activation and positive queue/outbox/replay evidence remain required. Full V20–V32 remains incomplete.

Physical session 7814 completed PASS: 1 passed, 12 deselected in 284.52s. Generic seal rejected the registered native draft and direct scheduler call of the underlying implementation received 42501; prior native person/transport/content checks also completed.

Review then identified a graph-membership inspection race before generic sealing locks. Migration 00704 now acquires tenant, usage and run FOR UPDATE in established admission order before inspecting native config/tasks and delegating. This fences controlled task insertion/config changes through the same run/FK locks. Added mandatory V27_DEFECT_GENERIC_SEAL_RUN_FENCE_OK: no-op concurrent run revision writer must receive 55P03 while the rejecting guard transaction is open, then succeed after rollback. Session 26011 pending; no positive dedicated native activation is claimed.

Physical session 26011 completed PASS: 1 passed, 12 deselected in 275.74s. The generic native seal guard rejected activation, held the run fence (concurrent no-op writer received 55P03), released it after rollback, and denied direct scheduler execution of the underlying implementation with 42501. Dedicated native qualified queue activation remains unimplemented.

Worker inspection found defects.propose additional protections checked only command.inputs, incompatible with the native three-bootstrap-ref transport. Added TaskArtifacts.require_declared_input, using the existing verified V2InputIndex (or exact legacy allowlist) without opening/downloading an artifact. The adapter now preflights each additional protection through that interface. No mask/tensor processing algorithm changed. Updated adapter test-double interface; new real disk-backed V2 index regression accepts an entry absent from direct refs and rejects changed hash/foreign UUID. Worker closure/adapter/proposal/labels/repair suites passed: 31 passed in 4.59s. This is protocol/regression evidence, not scientific model or RTX qualification. Full V20–V32 remains incomplete.

### 2026-10-06 — exact content envelope for dedicated native activation

Added marshalDefectActivationContent and privately retained original Params/WorkingImage text in the preparation plan. The envelope carries schema 1, three exact artifact refs and text bodies, preserving original bytes through JSON string transport for future SQL digest validation. It checks bounded byte hashes, original producer Params ref, strict float32 working profile and source canvas binding, new canonical Params, complete qualified faces and required protected masks. beginDefectPreparationDraftInTransaction repeats envelope validation before SQL mutations; no object IO or queue authority is granted.

Go packages passed (semanticproposal 1.484s, review/v2input cached). Dedicated roundtrip/corruption/cancellation regression passed (0.665s): exact bodies survive JSON transport; changed WorkingImage/original/native text or canvas binding returns no envelope. Physical session 93668 pending for the wired constructor/coordinator path. A complete dedicated SQL qualification/seal function is still required; generic native seal remains blocked and full V20–V32 remains incomplete.

Physical session 93668 completed PASS: 1 passed, 12 deselected in 288.90s. Exact content envelope validation now ran in native plan/draft entry, initial durable coordinator, input registration replay, person lock composition and coordinator replay. Existing role/scope substitutions and generic seal bypass/fence checks also completed in the selected fixture. The envelope is still a preparation component: it does not itself grant SQL qualification/seal authority or activate the task. Dedicated full SQL proof validation, positive atomic queue/outbox/replay and public HTTP integration remain required; full V20–V32 is not complete.

### 2026-10-06 — SQL exact activation-content matching

Added forward-only migration 00705 with scheduler-only semantic_defect_activation_content_matches(jsonb). The bounded schema-1 envelope requires exact ref keys, valid distinct nonzero UUIDs and SHA256, string bodies with per-document byte bounds and SHA verification using public.digest over original UTF8 text. Parsed WorkingImage must use float32/work_rec2020_gamma24_v1; both Params bind to its exact ref, retain the same ordered bounded face array, and keep every originally protected mask with its full JSON binding. Function has fixed search_path, owner photo_fn_owner, PUBLIC revoked; it performs no writes, catalog authorization, model qualification or queue activation. This content matcher is one component of the future full dedicated seal path, not a complete JSON-schema/producer admission certificate.

Physical test directly matches the actual plan envelope and changes each of WorkingImage, original Params and new Params text individually, requiring false on each hash mismatch. Mandatory gate V27_DEFECT_SQL_CONTENT_HASH_MATCH_OK; session 34958 pending. Integration compilation result recorded separately. Full final dedicated native queue admission and V20–V32 remain incomplete.

### 2026-10-06 — SQL content matcher physical result and transaction integration

Physical session 34958 completed PASS: 1 passed, 12 deselected in 283.40s. The actual three-document envelope matched in SQL; changing each document text without changing its SHA was rejected. Native draft admission now invokes the SQL matcher after source/producer/published Params evidence locks and before BeginAdmission. This adds no activation authority; generic native seal remains blocked. Wired-path verification pending; full V20–V32 remains incomplete.

Wired SQL matcher unit/package verification passed: semanticproposal 1.190s; review and v2input cached pass. Physical wired-path verification is running as session 28248; no terminal result yet.

### 2026-10-06 — Go/SQL distinct document identity parity

Go activation-content preparation now rejects reuse of an artifact UUID among WorkingImage, original Params and native Params, matching migration 00705. Equal Params bytes remain allowed when separately published immutable document identities differ. Regression deliberately aliases original/native Params with valid matching hashes and requires rejection before SQL; targeted Go test passed (0.930s). Physical session 28248 remains live with no terminal output observed; do not restart. This does not implement final queue admission.

### 2026-10-06 — Wired content matcher verified; provenance composition prepared

Physical session 28248 completed PASS: 1 passed, 12 deselected in 409.35s. The SQL matcher now executed inside native draft/coordinator/registration/person admission preparation transactions. Distinct Go artifact identity regression and reusable validated origin-pin serialization passed targeted tests (1.245s). Added migration 00706 composing exact content refs and text byte sizes with existing source/editor, original producer and newly published Params metadata locks. Migration 00706 has not yet been physically applied/tested or wired to Go; it grants no queue authority. Full dedicated seal and V20–V32 remain incomplete.

### 2026-10-06 — Content/provenance SQL composition wired

Added lockDefectContentProvenance: reconstructs and compares the private exact content envelope before SQL, serializes validated original five-artifact evidence and exact published Params metadata, then calls migration 00706. Native begin now invokes this composed check after existing full source/model/manual/Params locks and before creating/resuming admission. No object IO or activation authority is added. Package and physical verification pending; full V20–V32 remains incomplete.

Go package checks completed PASS: semanticproposal 1.688s; review/v2input cached. Physical migration 00706 and wired transaction verification running as session 11825; no terminal result yet.

### 2026-10-06 — Reject substituted content envelope before SQL

Added provenance entry regression requiring nil, empty replacement object and whitespace-appended envelope to return ErrInvalid with zero transaction calls against an otherwise valid retained document plan. Exact private canonical envelope is required before any SQL provenance check. Targeted activation-content/origin-lock tests passed (1.191s). Physical session 11825 still live; no terminal result observed. Dedicated graph activation and full V20–V32 remain incomplete.

### 2026-10-06 — Registered native person admission composition

Added lockRegisteredDefectPersonAdmission: binds the privately frozen task to published transport before SQL, composes native content/source/producer/Params and real person locks, requires exact already-registered closure state replayed, and repeats live scope/full policy after registry inspection. It never registers missing transport or seals/queues a graph. Not yet wired into physical fixture/final activation. Package verification pending; physical 00706 session 11825 still live. Full V20–V32 remains incomplete.

Registered admission composition Go packages PASS: semanticproposal 3.065s, v2input cached. Physical session 11825 remains live without terminal output; new registered-admission helper is not physically verified yet.

### 2026-10-06 — Missing native transport fails before admission SQL

Extended activation-content fixture with a real CompileSemanticDefectTask and its exact Params/WorkingImage/pixel ledger. Calling registered person admission with missing published transport must fail with zero transaction calls, before content/person/policy/admission SQL. Targeted regression passed (1.191s). This is preflight evidence only, not positive physical registered admission or queue activation. Physical migration 00706 session 11825 remains live.

### 2026-10-06 — Content/provenance physical verification completed

Session 11825 PASS: 1 passed, 12 deselected in 380.83s. Migration 00706 and wired SQL content/provenance composition ran through native plan/draft/coordinator/registration replay and person locks. Integration compilation PASS (0.654s, no tests run). Physical fixture now calls registered person admission with exact durable transport and rejects substituted manifest; mandatory gate V27_DEFECT_REGISTERED_PERSON_ADMISSION_OK. This new fixture verification is pending. No graph activation or full V20–V32 completion claimed.

Registered person admission physical verification running as session 89235; no terminal result yet.

### 2026-10-06 — Owned registered transport proof packet

Extracted freezeDefectRegistrationProof and schema-1 private defectRegistrationProof for bounded ordered rows, manifest/chunks/direct JSON and entries count/SHA. Existing registration SQL uses the owned packet without changing its arguments or activation behavior. This prepares complete transport evidence for dedicated composed SQL admission. Packages PASS (semanticproposal 1.502s, v2input cached); regression for cursor failure closes the cursor, returns no partial proof, and cancelled preflight never opens it PASS (0.665s). Physical session 89235 remains live and predates this extraction; it does not verify the extraction. Final queue admission/full V20–V32 remain incomplete.

### 2026-10-06 — Registered person admission physical PASS and digest hardening

Physical session 89235 PASS: 1 passed, 12 deselected in 255.78s. Exact registered native transport composed with person/source/full policy/content/provenance locks passed; substituted manifest returned conflict. This run predates registration-packet extraction. The packet now recomputes canonical full-entry SHA before SQL, rejects excess entries at claimed count, and checks cancellation before return. Packages PASS (semanticproposal 1.492s; v2input cached). Added positive exact-list and changed-UUID/excess-entry rejection regressions; targeted result and latest physical verification recorded next. No queue activation or complete V20–V32 claim.

Registration packet regressions PASS (0.621s): exact list accepted; changed UUID or excess entry rejected with no partial proof; failure cursor closed. Latest physical verification running as session 9627, no terminal result yet.

### 2026-10-06 — Frozen task/registration binding envelope

Added private schema-1 defectRegistrationBinding and marshalDefectRegistrationBinding. It derives task UUID, Params ref, input digest and profile exclusively from the frozen native task, binds exact published transport, recomputes complete entry SHA through freezeDefectRegistrationProof, and emits bounded canonical JSON. It grants no SQL provenance/registration/queue authority and is not yet wired to the dedicated SQL gate. Package verification pending. Physical session 9627 remains live and predates this helper. Full V20–V32 remains incomplete.

Frozen registration binding package checks PASS: semanticproposal 1.632s; v2input cached. Positive binding-envelope physical verification remains pending.

### 2026-10-06 — Frozen registration envelope roundtrip verified

Using a real native frozen task and immutable memory transport publication, the binding envelope roundtrip preserves task UUID, Params ref, input digest, count and complete entries SHA; replaced entries SHA produces no envelope. Targeted activation-content/registration proof regressions PASS (0.567s). Physical session 9627 is still live; this roundtrip is not physical SQL qualification or queue authorization. SQL binding-envelope gate remains to be added and full V20–V32 is incomplete.

### 2026-10-06 — Registration packet physical PASS; SQL binding gate wired

Physical session 9627 PASS: 1 passed, 12 deselected in 226.89s, verifying packet extraction and canonical full-entry SHA during durable registration and registered person admission. Added forward-only migration 00707, scheduler-only semantic_defect_registration_binding_state: bounded exact schema keys, typed nonzero task/Params UUIDs, SHA/count/profile checks, then complete persisted native registration replay validation via 00702. Registered person admission now passes the frozen canonical binding packet through this SQL gate. It remains a registration component, grants no model/person/queue authority itself, and does not activate. Package/latest physical results follow; full V20–V32 incomplete.

SQL binding gate package checks PASS: semanticproposal 1.440s; v2input cached. Physical migration 00707 wired verification running as session 46034; no terminal result yet.

### 2026-10-06 — Source/defect qualification/full policy packet prepared

Added marshalDefectSourceAuthority with schema-1 bound source/version/canvas, exact source metadata pins, retained canonical defects.propose expectations, exact QualificationReport pin and complete global policy snapshot. Before serialization it validates policy scope and all selected defect/face/parser expectations, decodes authorized file pins and validates the retained defect qualification. Bounded canonical packet, no object IO or SQL/queue authority. It is not yet wired or physically verified; face/parser/manual/person proof packets and final combined SQL gate remain required. Physical session 46034 still live; package result follows.

Source authority packet package checks PASS: semanticproposal 1.296s; v2input cached. These checks establish compilation/regression compatibility, not positive packet or SQL authority verification.

### 2026-10-06 — SQL registration binding PASS; source authority component wired

Physical session 46034 PASS: 1 passed, 12 deselected in 243.91s. Migration 00707 exact binding gate ran in registered person admission and rejected manifest substitution. Added forward-only migration 00708 lock_semantic_defect_source_authority: bounded exact source/version/canvas binding, source scope/metadata locks, full global policy snapshot barrier and selected imported defects.propose qualification locks, then final scope recheck. Go full-policy execution locks now invoke the composed packet after existing complete evidence/policy locks. This is not a complete source canvas-byte/face/parser/manual/person or queue admission certificate. Latest package and physical verification pending; full V20–V32 incomplete.

Source authority composition package checks PASS: semanticproposal 1.095s, v2input cached. Physical migration 00708 wired verification running as session 27498; no terminal result yet.

### 2026-10-06 — Original qualified face authority packet prepared

Added marshalDefectFaceAuthority, bounded canonical array retaining exact landmarks/detector refs, producer metadata, original expectation pair and both qualification policy/report pins. Before serialization it requires complete one-to-one coverage of authorized landmark files, exact original object metadata and scope/expectation/qualification equality; missing/duplicate producers are invalid. Empty qualified face inventory is permitted. No object IO/SQL activation authority; not yet wired or positively physically verified. Physical session 27498 remains live; full V20–V32 incomplete.

Face authority packet package checks PASS: semanticproposal 1.166s; v2input cached. These are compilation/regression checks, not positive serialized producer or SQL gate verification.

### 2026-10-06 — Parser/label authority packet prepared

Added bounded canonical schema-1 marshalDefectParserAuthority retaining original face-mask roots and parser labels/pixels/detector refs, complete producer metadata, canonical parser/detector expectations and qualification/report pairs. Preflight mirrors existing parser proof scope/count/qualification/root-presence guards and rejects duplicate labels producers. Root derivation and all source/manual/person/final SQL admission obligations remain separate. Not yet wired or positively physically verified. Physical migration 00708 session 27498 remains live; package results follow. Full V20–V32 incomplete.

Parser authority packet package checks PASS: semanticproposal 2.740s; v2input cached. These checks establish compilation/regression compatibility, not positive serialized parser or SQL authority verification.

Physical session 27498 completed PASS: 1 passed, 12 deselected in 201.09s. Migration 00708 and wired source/defect qualification/full policy composition ran through native durable preparation and registered person admission. This run predates face/parser authority serializers, which still require positive physical verification and SQL integration. Final native graph activation/full V20–V32 remain incomplete.

### 2026-10-06 — Real source/face/parser packet roundtrip and foreign-version gate

Physical fixture now builds source, face and parser authority packets from original qualified execution evidence, decodes and compares exact scope/full policy, producer counts and mask roots; all three must reject a substituted source-version binding. Mandatory V27_DEFECT_AUTHORITY_PACKETS_ROUNDTRIP_OK. Parser packet now checks source file-proof scope even with zero parsers, preventing empty parser inventory from bypassing binding validation. Integration compilation PASS (0.710s; no tests run), latest package check pending. Physical verification running as session 82116; no terminal result yet. This verifies serialization components, not complete SQL face/parser/final queue admission. Full V20–V32 incomplete.

Latest packet/source-scope package checks PASS: semanticproposal 1.283s; v2input cached. Physical session 82116 remains pending.

### 2026-10-06 — Complete original manual authority packet prepared

Added marshalDefectManualAuthority: canonical bounded root/target/evidence entries derive only from decodeDefectExecutionManualProofs, requiring complete original manual-root coverage, exact authorized root/target object metadata and nonalias original retained proof. Evidence includes raster root/Params, target/protected arrays and PNGs plus external vectors when present. Empty manual inventory emits [] only after source/file/root validation. No SQL or queued authority; not yet wired/positively physically verified. Physical source/face/parser packet session 82116 remains live. Full V20–V32 incomplete.

Manual authority packet package checks PASS: semanticproposal 1.560s; v2input cached. These establish compilation/regression compatibility, not positive manual serialization or SQL verification.

### 2026-10-06 — Authority packet fixture failure diagnosed

Physical session 82116 FAILED: 1 failed, 12 deselected in 191.03s at source policy packet roundtrip byte comparison. Retained policy snapshot is raw PostgreSQL JSONB text (spaces/key order); packet canonicalization intentionally changes formatting while SQL policy equality is JSONB content equality. Fixture now compares complete canonical policy JSON instead of raw DB text. This does not weaken exact document byte/hash checks. No positive packet verification claimed; repeat pending.

Corrected authority packet physical repeat running as session 7457; no terminal result yet. SQL face/parser/manual composition has not been added or activated. Full V20–V32 remains incomplete.

### 2026-10-06 — Authority packets physical PASS; selected producer SQL composition wired

Physical corrected session 7457 PASS: 1 passed, 12 deselected in 277.54s, including source/face/parser packet roundtrip and substituted version rejection. Added forward-only scheduler-only 00709 lock_semantic_defect_selected_producers: bounded typed selected face/parser/manual packets, duplicate selected producer rejection, exact expectation-policy pair, existing producer/object/qualification locks, manual root-to-evidence identity, final source scope recheck. Go full-policy execution now invokes it after original complete locks and source authority composition. This selected-evidence component does not prove all catalog/root/content/person relationships or grant atomic queue admission. Latest package/physical results follow; full V20–V32 incomplete.

Selected producer composition package checks PASS: semanticproposal 1.602s; v2input cached. Physical migration 00709 wired verification running as session 19092; no terminal result yet.

### 2026-10-06 — Reusable exact declared-file authority pins

Extracted marshalDefectDeclaredFileAuthority from existing file lock entry: validates exact original producer scope/ref, full retained file inventory and original Params descriptors, then owns canonical original refs plus full metadata and parent-edge pins. Existing lock_semantic_defect_execution_files call uses the same packet; no SQL permissions or activation behavior changed. Added cancellation checks before and after serialization. This prepares typed file authority for complete future SQL registration closure mapping. Physical 00709 session 19092 remains live and predates extraction; latest package results follow. Full V20–V32 incomplete.

Declared-file packet package checks PASS: semanticproposal 1.789s; v2input cached. Latest physical extraction verification pending; session 19092 tests prior compiled code and migration 00709.

### 2026-10-06 — One-real-person and protected-mask authority packet prepared

Added marshalDefectPersonAuthority with exact person root/child, protection root/pixels, count, retained model expectations/report/producer evidence and protected object pins. Preflight reuses native person admission validation against complete policy and actual protected pixels in Params, plus exact protection scope validation. It owns bounded canonical JSON and does not invent duplicate person proof or grant SQL/queue authority. Not yet wired/positively physically verified. Physical 00709 session 19092 remains live; latest package result follows. Full V20–V32 incomplete.

Person authority packet package checks PASS: semanticproposal 1.600s; v2input cached. These establish compilation/regression compatibility, not positive coverage packet or SQL verification.

### 2026-10-06 — Person/protection authority serialization regression

Extended native person admission regression: valid covered Params serialize exact person root/child, protection root/pixels/count and both ordered protection metadata pins. Removing covered pixels requires ErrInvalid and nil envelope. Targeted test PASS (0.719s). This is synthetic retained-proof unit evidence, not physical coverage or complete SQL gate authority. Physical 00709 session 19092 remains live. Full V20–V32 incomplete.

Physical session 19092 completed PASS: 1 passed, 12 deselected in 269.30s. Migration 00709 composed selected face/parser/manual producer and qualification locks ran through native full-policy preparation and registered person admission. This run predates declared-file serializer extraction and person authority helper/tests, which still need physical integration. Selected producer locks are not complete queued admission. Full V20–V32 remains incomplete.

### 2026-10-06 — Selected person/protection SQL composition wired

Added forward-only scheduler-only migration 00710 lock_semantic_defect_person_authority: bounded exact packet, count typing/bounds, exact three-document content matcher and source ref, covered protected-pixel presence in native Params, scope/protection/person/qualification locks repeated around tenant inventory barrier, selected candidate rediscovery and final scope check. Native person locks now invoke this after existing shared evidence locks. This does not prove actual pixel coverage by SQL or authorize graph activation; complete closure/model/root/content/final admission remains required. Go packages PASS (semanticproposal 1.283s; v2input cached). Physical migration 00710 verification running as session 44938; no terminal result yet. Full V20–V32 incomplete.

### 2026-10-06 — Unified privately retained native admission evidence packet

Added schema-1 marshalDefectAdmissionEvidence: deterministic child run/workflow and tenant/project/actor guards, exact source/full policy, original producer metadata/refs, declared file parent pins, exact three-document text, new Params document metadata, face/parser/manual/person evidence and exact frozen registration packet. All components derive from private retained preparation; bounded 32 MiB canonical packet with cancellation checks and no object/SQL IO. Actual SQL cross-binding, coverage-content authority, complete catalog discovery and atomic graph admission remain required. Not yet wired/positively physically verified. Physical 00710 session 44938 remains live; package result follows. Full V20–V32 incomplete.

Unified admission packet package checks PASS: semanticproposal 1.368s; v2input cached. These are compilation/regression checks, not positive packet or atomic SQL queue verification.

### 2026-10-06 — Unified packet frozen-task coherence

Unified admission evidence now rejects invalid request SHA, changed deterministic draft key, wrong shell cardinality, Params ref differing from frozen task and any full shell-task JSON mismatch before person/other proof serialization. This mirrors existing draft preflight at packet boundary so later SQL receives a coherent candidate, not independently valid contradictory pieces. No activation authority added. Physical 00710 session 44938 remains live and does not exercise this new packet helper; latest package result follows. Full V20–V32 incomplete.

Unified packet coherence package checks PASS: semanticproposal 1.789s; v2input cached. Positive packet and mutation physical verification remain pending.

### 2026-10-06 — Person/protection SQL PASS; unified packet physical fixture

Physical session 44938 PASS: 1 passed, 12 deselected in 249.43s, verifying migration 00710 and wired native person/protection/content composition through registered admission. New fixture builds unified admission packet from real prepared evidence and checks tenant/project/actor/session/run/request/document plus nested task/Params/person/protection cross-bindings. Changed run, shell task Params SHA, Params identity, document size and invalid request SHA must yield no packet. Mandatory V27_DEFECT_UNIFIED_ADMISSION_PACKET_OK; latest verification pending. This remains packet/selected-lock verification, not complete SQL atomic queue admission. Full V20–V32 incomplete.

Unified packet integration compilation PASS (0.842s; no tests run). Physical unified packet and latest declared-file/person helper verification running as session 35354; no terminal result yet.

### 2026-10-06 — Exact config/policy text for SQL hash binding

Unified admission packet now retains canonical config_text and policy_text strings. It requires exact canonical shell config bytes and recomputed config SHA equal retained ConfigSHA; canonical full policy SHA must equal shell PolicySHA. This supplies exact UTF8 digest inputs and decoded-policy identity for a future SQL cross-binding gate; no config/policy authority is granted by text alone. Physical unified packet session 35354 remains live and predates this change; latest package result follows. Full V20–V32 incomplete.

Config/policy text packet package checks PASS: semanticproposal 1.949s; v2input cached. Positive latest packet physical validation still pending.

### 2026-10-06 — Unified packet physical PASS; composed SQL evidence gate wired

Physical session 35354 PASS: 1 passed, 12 deselected in 268.42s, verifying unified packet scope/nested cross-bindings and five rejection mutations. Added migration 00711 lock_semantic_defect_admission_evidence: bounded exact envelope, tenant/usage/run fences, blocked semantic run ownership, exact config text SHA/JSON and policy text SHA/JSON, persisted native config links to source/Params/task/digest/principal, original producer ref ordering, then composed source/content/provenance/files/selected producer/person/registration locks and final scope recheck. Registered person admission now invokes it after existing full locks. It grants no seal/queue authority and still does not establish all pixel coverage/root/content/catalog completeness requirements. Latest package/physical results follow; full V20–V32 incomplete.

### 2026-10-06 — Composed admission verification in progress

Previous package run 82175 passed semanticproposal (1.859s) and v2input (cached). Physical session 72815 was re-polled on continuation and remains live without a terminal result; no restart was performed. It verifies migration 00711 and latest config/policy text packet wiring. Positive packet verification alone does not establish complete SQL admission, pixel coverage or queue activation. Full V20–V32 remains incomplete.

### 2026-10-06 — Composed admission physical PASS and SQL substitution coverage

Session 72815 completed PASS: 1 passed, 12 deselected in 208.75s, covering migration 00711 and canonical config/policy text packet wiring. Package session 5356 passed semanticproposal and v2input (cached). Added eleven direct SQL rejection cases in the actual registered scheduler transaction: tenant, project, actor, session, run, Params document, parent run, request SHA, exact config text, exact policy text and unknown envelope field. Each must return false; mandatory V27_DEFECT_COMPOSED_ADMISSION_SUBSTITUTION_DENIED_OK prevents silently skipping the checks. Latest isolated physical session 5766 is running; no terminal result yet. These UUID substitution cases do not establish independent real-tenant coverage. Complete SQL closure/catalog/pixel authority and atomic native queue activation remain pending; full V20–V32 incomplete.

### 2026-10-06 — Native closure consistency before composed admission

Added validateDefectAdmissionClosure and invoked it from the private unified packet builder. Every frozen transport row except newly published Params must match a retained original file ref and mapped contract; duplicate file/row identities, a second Params, mismatched new Params, missing native Params references and substituted direct bootstrap refs are rejected. Existing VerifyDefectProposalDeclaredInputs validates actual retained native Params bytes against WorkingImage/RGB, all faces and protection/mask references. Direct refs must be exactly new Params, WorkingImage and RGB. Geometry/alpha/mask pixel contracts map to NpyArray; selected producer, complete catalog discovery, physical bytes and SQL authority remain separate. This Go guard is not a substitute for independent SQL closure validation.

Unit fixture uses a real compiled frozen task and published in-memory transport: positive closure plus changed row SHA, wrong contract, missing row, duplicate direct ref and changed Params identity. Initial SHA mutation accidentally retained the same Params SHA due to sorted row order; fixed to an explicit different zero SHA. Final session 35590 PASS: semanticproposal 1.208s, v2input cached. Live physical session 5766 still pending and predates this Go guard; no restart, migrations or physical fixture edits during its run. Full V20–V32 remains incomplete.

### 2026-10-06 — SQL substitution PASS and independent native closure gate

Physical session 5766 PASS: 1 passed, 12 deselected in 190.19s, covering eleven direct composed SQL envelope substitutions. Added forward-only migration 00712 semantic_defect_admission_closure_matches: bounded transport rows/count, unique file/row identity, typed nonzero refs, exact file ref/contract mapping except separately pinned newly published Params, required native Params face/skin/manual/protection/permanent refs, required WorkingImage/RGB contracts and exact three direct refs. Public composed evidence function wraps this independent matcher before prior locks. Prior function renamed pre_closure and scheduler EXECUTE revoked; function owner retains the internal call. No seal/queue authority is introduced. This does not establish independent canonical input digest, complete catalog discovery, mask pixel dependency completeness or actual physical coverage.

Physical fixture adds positive SQL matcher and four negative cases: SHA, contract, missing WorkingImage and duplicate direct ref. Mandatory V27_DEFECT_SQL_CLOSURE_MATCH_OK. Integration compile session 82635 passed (0.613s, no tests run) before the final missing-row selection refinement. Isolated latest physical session 65554 running; verifies newest Go closure guard, SQL migration and fixture. Full V20–V32 remains incomplete.

### 2026-10-06 — Dense geometry native contract consistency

Found compiler mapped legacy DenseWarp but omitted actual catalog DenseWarpField. Centralized defectNativeInputContract shared by draft compilation and admission consistency; both geometry spellings, RGB, alpha and mask pixels map to NpyArray while document roles remain unchanged. Go session 75550 PASS semanticproposal 1.505s and v2input cached. Live physical session 65554 remains pending, predating this mapping refactor. Inspection of 00710 and person admission fixture confirms protected Params may reference protected_pixels directly; migration 00712 currently requires MaskRef for those links and needs runtime-result-guided correction after the live scenario terminates. No production migration or activation. Full V20–V32 incomplete.

### 2026-10-06 — Native NPY-mask closure correction after physical failure

Physical session 65554 completed FAIL: 1 failed, 12 deselected in 192.42s; native registered admission rejected at the new 00712 gate before direct matcher tests. Inspection proves native protected masks reference covered NPY pixels (00710 enforces this), whereas new matcher incorrectly required MaskRef for mask links. Corrected required mask entries to exact ref containment; each actual transport row still must match retained catalog file ref and contract, and existing source/protection/producer locks remain mandatory. Face landmarks retain explicit FaceLandmarks requirement.

Added a distinct direct NPY mask unit case using actual native Params compilation and retained MaskPixels catalog mapping. Initial reuse of RGB as mask correctly failed conflicting dependency identity; fixture now uses a distinct mask UUID/SHA, additional NpyArray row and file pin. Go session 49536 PASS semanticproposal 1.360s, v2input cached. Isolated corrected physical session 21105 running, no terminal result yet. 00712 has only been tested on disposable DBs, not applied to production. No queue activation; full V20–V32 incomplete.

### 2026-10-06 — Selected document child inventory consistency

Go unified packet closure check now requires every retained file whose parent is a selected input to appear as the exact mapped transport entry, and validates parent SHA equality. Incomplete parent metadata is rejected. This catches missing retained mask pixels, alpha/warp arrays and other selected document children, but does not prove the retained catalog itself contains every actual content dependency; independent SQL/physical-content completeness is still needed. Unit cases missing geometry child, complete child and substituted parent SHA passed: session 89203 semanticproposal 1.343s, v2input cached. Physical session 21105 remains live and predates this Go change; migrations and physical fixture were not edited during that run.

Inspected compileCaptureInventory: input_digest is canonical SHA of entries, operation, task, version; entries_sha is canonical array SHA. A future independent SQL digest check can serialize the strict typed ASCII ref/contract envelope without attempting a general JSON canonicalizer. No queue activation; full V20–V32 remains incomplete.

### 2026-10-06 — Corrected native closure physical PASS; independent digests

Physical session 21105 PASS: 1 passed, 12 deselected in 205.06s, covering corrected NPY-mask handling, positive SQL closure and four closure substitution failures, plus eleven envelope mutations. This predates latest retained-child/digest Go changes. Go session 66453 PASS semanticproposal 1.164s and v2input cached after adding strict UUID ordering and recomputed entries_sha/input_digest to the private packet guard; positive extra-child and NPY-mask fixtures recompute their complete hashes.

Added forward-only 00713: SQL independently reconstructs the canonical ASCII-only typed entries envelope, requires strict UUID order, compares canonical array SHA and task/operation/version/entries input SHA, and requires selected-parent child presence with exact parent SHA. No general JSON canonicalization is asserted: prior gate limits contracts and UUID/SHA strings to an escaping-free alphabet. Previous matcher becomes owner-only internal helper; scheduler uses the new composed matcher through existing evidence gate. Added explicit entries SHA, input SHA and row-order SQL mutations. Selected retained inventory checks still do not prove complete physical/catalog discovery or pixel coverage. Latest isolated session 8657 running; no terminal result. No queue activation or production changes; full V20–V32 incomplete.

### 2026-10-06 — Go digest substitution regressions

Extended the private closure unit fixture with explicit changed entries_sha, changed input_digest and reordered rows. These supplement existing changed SHA/contract/missing input/direct ref/Params checks. Session 85251 PASS semanticproposal 1.733s and v2input cached. Live physical 8657 remains pending and was polled without restart; it contains the analogous SQL mutations for 00713. Inspection confirms selected producer gate 00709 validates chosen proofs but does not independently bind the complete face/parser/manual inventory to original Params; this remains a required admission gap. No seal/queue authority introduced. Full V20–V32 incomplete.

Supporting native content/catalog packages also passed (session 67874): review cached; v2draft 4.278s. This is bounded unit verification of affected dependency contracts, not full native worker/SQL authority completion. Physical 8657 still awaits terminal output.

### 2026-10-06 — Physical unified closure failure; typed array dependency refinement

Session 8657 terminal FAIL: 1 failed, 12 deselected in 203.54s at unified packet construction before SQL matcher checks. Consequently 00713 runtime digest matching is not yet proven. Inspection identified an overbroad child-inventory rule: a selected landmark/evidence document may contain provenance dependencies outside native worker consumption. Refined Go and 00713 child requirement to retained array roles (WorkingPixels/WorkingAlpha/MaskPixels/DenseWarp/DenseWarpField) of selected WorkingImage/MaskRef documents. Full original declared catalog and producer/root authorization remain independently mandatory; no such locks were removed. This refinement is a hypothesis for the observed generic ErrInvalid until the next physical result, not a proven root cause.

Added wrapped ErrInvalid diagnostics for entries SHA, task digest and missing/mismatched retained array role, without object paths or artifact identities; assertions use errors.Is. Go session 23392 PASS semanticproposal 1.432s, v2input cached. Latest isolated physical session 86113 running. No production migrations or native queue authority; full V20–V32 incomplete.

### 2026-10-06 — Physical diagnostics identify missing WorkingAlpha; source compiler fixed

Session 86113 terminal FAIL: 1 failed, 12 deselected in 177.20s. Diagnostic now proves unified packet rejected a retained WorkingAlpha dependency. This corrects the earlier unproven hypothesis: missing native source array, not merely a provenance-document edge, is the confirmed current failure. Kept typed array completeness checks. BuildDefectProposalTaskInputsFromImage now adds exact validated WorkingImage alpha and dense inverse_field refs, deduplicates matching refs, rejects source/RGB collisions or contradictory digests, caps 4096 and sorts fresh inputs. Params bytes/digest and face/protection settings are unchanged. Native file authority, resource compilation and input digest consume the expanded array inventory.

Added actual WorkingImage fixture cases with alpha and dense geometry, verifying exact array inclusion and unchanged Params. Existing original-image test now compares exact dependency inventory instead of assuming two refs for transformed images. Initial runs failed the obsolete two-ref expectation; final session 62594 PASS: review 0.712s, semanticproposal and v2input cached. Latest isolated physical result follows; 00713 SQL digest runtime still unproven. No queue activation/production apply; full V20–V32 incomplete.

Corrected source-array physical verification is running as session 74032; no terminal result yet.

### 2026-10-06 — Source-array collision/cancellation regressions

Extended real WorkingImage source fixture coverage: RGB identity reused for alpha must yield no Params/Inputs; alpha and dense warp sharing UUID with different SHA must yield no partial compilation; cancelled context yields context.Canceled/no Params. Compiler also rechecks cancellation after sorting the final bounded dependency inventory. Existing Params bytes/digest preservation and exact alpha/warp membership tests remain. Physical session 74032 remains live and predates the final cancellation recheck; no restart or migration/physical-fixture edits while live. No native queue activation; full V20–V32 incomplete.

Source-array latest package session 81246 terminal PASS: review 1.003s, semanticproposal 1.613s, v2input 9.087s. Physical session 74032 remains pending.

### 2026-10-06 — Worker propose regression verification while physical run continues

Session 83816 terminal PASS: 13 worker defect proposal/adapter tests in 4.66s. Pytest emitted a cache-write permission warning before the repository write grant; test results were successful and cache warning does not constitute processing failure. Source compiler Params preservation and existing worker protection/declared-input checks remain covered at unit scope; no hardware/scientific quality claim is made. Physical session 74032 remains live and was repeatedly polled without restart; no terminal SQL digest/alpha integration verdict yet. No migrations or physical fixture edits during the run. Full V20–V32 incomplete.

### 2026-10-06 — Native source arrays and SQL canonical digest physical PASS; ACL matrix

Session 74032 terminal PASS: 1 passed, 12 deselected in 260.55s. This verifies actual native WorkingAlpha inclusion, unified packet, typed source-array dependencies and migration 00713 canonical entries/task digests through composed registered admission. It also covers seven SQL closure mutations and eleven envelope substitutions. It predates the final compiler cancellation recheck; that change passed package checks separately. Physical verification does not prove actual leased native worker execution, complete mask/root catalog authority, full scientific coverage or queue activation.

Added real-role EXECUTE rejection matrix: scheduler cannot invoke pre_closure/pre_digest internal functions; eight non-scheduler runtime roles cannot invoke those or current composed evidence/closure gates. Each separate rollback-only transaction must receive PostgreSQL 42501; mandatory V27_DEFECT_COMPOSED_ADMISSION_ACL_OK. Latest isolated physical verification pending. Full V20–V32 incomplete.

ACL integration compile session 10517 PASS (0.662s, no tests run). Latest isolated physical session 50201 running; no terminal result yet.

### 2026-10-06 — Private landmark catalog/Params/producer set reconciliation

Added validateDefectLandmarkInventory to unified packet construction. It derives unique landmark refs from native Params faces, requires exact catalog FaceLandmarks set and one producer entry per unique ref; duplicate producer, omitted producer and changed UUID/SHA are rejected. Multiple face objects may share a landmark document without requiring duplicate producer proof. This is retained set consistency, not physical landmark-object/model authority or independent SQL admission completeness; existing face evidence and qualification locks remain mandatory. Unit fixture covers shared-document positive plus four substitutions. Physical ACL session 50201 remains live and predates this change; no restart/migration/physical-fixture edits while it runs. Full V20–V32 incomplete.

Landmark inventory package session 42744 terminal PASS: semanticproposal 1.581s, v2input cached. Latest physical ACL session 50201 remains pending.

### 2026-10-06 — Landmark catalog completeness and explicit empty inventory

Landmark inventory guard now rejects null file/producer arrays while accepting explicit empty arrays with native faces=[]; this matches the selected SQL producer gate array semantics. Added absent catalog landmark and unrelated catalog landmark negatives, explicit empty positive and null array negatives. Package session 50508 PASS semanticproposal 1.297s, v2input cached. Physical ACL session 50201 still live and predates landmark inventory changes; repeatedly polled without restart. SQL landmark-set reconciliation and latest positive physical verification remain pending. Full V20–V32 incomplete.

### 2026-10-06 — ACL physical PASS; independent SQL landmark inventory

Physical session 50201 terminal PASS: 1 passed, 12 deselected in 238.18s, including runtime-role EXECUTE denials and scheduler pre_closure/pre_digest bypass denial. It predates latest Go landmark guard. Added forward-only migration 00714 semantic_defect_landmark_inventory_matches: exact unique native Params landmark refs, exact retained FaceLandmarks catalog array, exact one-per-ref producer array; malformed/null/duplicate/foreign/missing refs reject, shared-face document identity is allowed. Current evidence function wraps this predicate before existing closure/digest/source/producer/person/registration locks. Previous evidence function becomes owner-only pre_landmarks helper with scheduler EXECUTE revoked. It remains retained-set consistency, not physical landmark object/content, discovery or complete mask/root authority; no seal/queue grant.

Physical fixture adds real positive landmark set plus missing/foreign UUID/changed SHA/duplicate producer negatives; mandatory V27_DEFECT_SQL_LANDMARK_INVENTORY_OK. ACL matrix includes new predicate and pre_landmarks helper. Latest isolated physical session 52127 running, no terminal result. Full V20–V32 incomplete.

### 2026-10-06 — Ordered native face-mask/root packet reconciliation

Added validateDefectMaskRootInventory to the unified packet: derives all ordered native face skin/manual bindings, requires exact retained roots count/order and canonical binding equality (ordinal/ObjectID/landmarks/ManualMode/full mask), valid nonzero root refs and consistent SHA for shared roots. This does not validate physical root bytes or replace source/producer/model/discovery locks. Added positive and missing/wrong ordinal/foreign landmarks/selector/extra root unit cases. Physical session 52127 remains live and predates this helper; no migration/physical-fixture edit or restart during the run. Independent SQL root binding and actual root-content completeness still pending. Full V20–V32 incomplete.

Ordered mask/root package session 97311 terminal PASS: semanticproposal 2.294s, v2input cached. Physical 52127 still pending.

### 2026-10-06 — Mask/root identity and shared-root regression coverage

Extended ordered mask/root tests: changed ManualMode, changed nullable face ObjectID, changed full mask confidence, explicit empty roots positive, null roots negative, consistent root shared by two ordered faces positive and contradictory SHA for the same shared root negative. Package session 52209 terminal PASS: semanticproposal 1.519s, v2input cached. These validate retained packet binding semantics, not real root-byte parsing/physical coverage/SQL model authority. Physical landmark/ACL session 52127 remains live, repeatedly polled without restart; SQL mask/root inventory still pending and full V20–V32 incomplete.

### 2026-10-06 — Landmark SQL physical PASS; ordered mask/root SQL gate

Physical session 52127 terminal PASS: 1 passed, 12 deselected in 272.00s, verifying Go landmark inventory, SQL00714 landmark-set positives/substitutions and expanded real-role ACL denials. It predates latest Go mask/root helper. Added forward-only 00715 semantic_defect_mask_root_inventory_matches: exact ordered native skin/manual binding fields, exact root cardinality, typed nonzero root refs, shared-root SHA consistency, and bidirectional manual root/target reference matching. Current composed evidence function wraps it before existing landmark/closure/digest/provenance/source/producer/person locks; previous function becomes owner-only pre_roots helper, scheduler EXECUTE revoked. This proves retained bindings only, not actual root-byte content, scientific coverage, discovery completeness or queue authority.

Fixture adds real positive plus missing/ordinal/landmarks/extra-root mutations and mandatory V27_DEFECT_SQL_MASK_ROOT_INVENTORY_OK; ACL matrix includes new function and internal helper. Latest isolated physical session 50488 running, no terminal result yet. No production migration/queue activation; full V20–V32 incomplete.

### 2026-10-06 — Verified wait and skin/parser lineage implementation path

Physical session 50488 confirmed live; bounded 50s poll (exec cell2607 resumed to completion) returned the same live handle with no terminal verdict. No restart, migration or physical-fixture edit. Inspected 00661 semantic_proposal_defect_skin_parse_objects: it binds selected mask pixels to parser via actual skin producer task inputs, source/project, canonical parser result, finished attempt/fencing, output refs and exact object metadata. Selected parser labels pixels are segmentation labels, not the derived skin mask pixels, so comparing their refs directly would be incorrect. Next independent SQL skin/root completeness gate should reuse this existing owner callable relation to require every skin root mask to have exactly one compatible selected parser and every selected parser to be used. Existing producer evidence locks should fence those same rows; relation-only STABLE lookup is not sufficient for concurrent admission. Full V20–V32 incomplete; no queue activation.

### 2026-10-06 — Physical mask/root SQL operator failure corrected

Physical 50488 terminal FAIL: 1 failed, 12 deselected in 272.02s, at registered native admission with PostgreSQL 42725 operator is not unique: unknown - unknown. The 00715 root ref exact-key expression lacked parentheses around root->Root before JSONB key subtraction. Corrected to ((root->Root)-artifact_id-sha256). This is an SQL precedence error; no predicate/coverage/authority requirement was relaxed. 00715 has only been used in disposable migration chains and not applied to production. Isolated corrected physical session 70340 running, no terminal result. Skin/parser lineage gate remains pending until this composed gate is verified. Full V20–V32 incomplete; no queue activation.

### 2026-10-06 — Admission component diagnostics and verified wait

Unified packet builder now wraps validation failures with input closure/landmark inventory/mask root inventory component names while preserving wrapped error identity and nil packet on failure. No artifact IDs, object locations or credentials are added. Package session 63257 terminal PASS: semanticproposal 1.308s, v2input cached. Corrected physical 70340 confirmed live; 50s poll (exec cell2615 awaited to completion) returned same running handle without terminal result. No restart or migration/physical fixture edits. Documentation 19-runtime-protocol still requires trusted frozen complete cursor, atomic registered graph seal and declared-input gateway binding; current draft/lock verification is not full V27 completion or native worker execution. Full V20–V32 incomplete.

### 2026-10-06 — Corrected roots gate physical PASS; selected skin/parser lineage

Physical 70340 terminal PASS: 1 passed, 12 deselected in 247.27s, verifying corrected 00715, Go mask/root binding helper, positive/substitution checks and runtime-role ACLs. Added forward-only 00716: existing composed evidence locks first, then skin producer/artifact/run/result/attempt shared locks, exact mask root identity via existing00660 catalog relation, exactly one compatible selected parser via existing00661 task input/result/attempt/fence relation, and every selected parser used by some skin root. Segmentation label pixels are compared to selected parser pixels, not derived mask pixels. Final live scope recheck retained. Previous entry function becomes owner-only pre_skin helper; scheduler EXECUTE revoked. No queue activation or physical/scientific coverage claim.

Fixture substitutes a valid-format foreign skin root UUID while preserving original binding/catalog/Params, expecting current composed gate false; mandatory V27_DEFECT_SKIN_ROOT_LINEAGE_DENIED_OK. ACL matrix includes pre_skin bypass denial. Latest isolated physical session 22633 running, no terminal result yet. Complete discovery/cross-tenant/races/root-byte authority/native worker/queue/public flows and full V20–V32 remain unfinished.

### 2026-10-06 — Root packet schema parity and verified lineage wait

Go mask/root inventory check now requires parser schema_version=1, matching 00715 and existing selected producer gate; unknown version test rejects before database access. Package session 9362 terminal PASS semanticproposal 1.486s and v2input cached. Physical skin/parser session 22633 confirmed live; bounded 50s poll (exec2627 awaited to completion) returned same running handle with no terminal output. No restart/migration/physical-fixture changes. Current gates lock draft/registered transport only; atomic native seal/queued/outbox, public workflow and complete V20–V32 remain unfinished.

### 2026-10-06 — Verified continued skin-lineage integration wait

Session 22633 remains confirmed live after three bounded 50s polls (exec2629,2630,2631 each awaited to completion) returning the same running handle without terminal output. No restart, migration/physical fixture edits or PASS claim. Previous turn made schema parity progress; this continuation is a verified wait. Correctness of 00716 and latest skin root substitution/ACL checks remains unproven pending the terminal result. Full V20–V32 remains active and incomplete.


### 2026-10-06 — Skin lineage fixture correction (verification pending)

Physical process 22633 ended with 1 failed, 12 deselected in 559.03s. The native stage is manual-only; the new negative asserted a skin root in that packet and failed before testing skin lineage. The failure left defectPersonTx open and pool shutdown waited until the Go timeout. This is fixture/cleanup failure, not evidence of SQL performance failure or a skin-lineage success.

Added unconditional deferred rollback for that transaction. Forward-only migration 00717 extracts the exact skin/parser correspondence checks into owner-only lock_semantic_defect_skin_lineage; the composed scheduler gate first executes all pre_skin admission checks and then invokes this component. The component grants no queue/seal authority. The physical fixture now uses its separately retained real skin preparation and original skin origin pins, requires positive correspondence and rejects changed root UUID, changed segmentation-pixel UUID and duplicate parser entry. Go package tests pass and integration-tag compilation passes; physical process 2382 is running, so the SQL assertions remain unverified. Full V20–V32 is incomplete.

Physical process 2382 failed in 234.30s (1 failed, 12 deselected): direct owner-only component invocation used the scheduler transaction and correctly returned SQLSTATE 42501. Deferred rollback prevented the prior pool shutdown timeout. Corrected the skin fixture to use a separate owner transaction with unconditional rollback; expanded the ACL matrix to require denial for the component under every tested application role, including scheduler. Skin correspondence remains unverified pending the rerun.

Physical process 96664 failed in 215.86s (1 failed, 12 deselected): skin component positive returned false under the separate owner transaction. Inspection found that transaction lacked app.tenant_id, required by its final execution-scope gate. Fixture now sets the tenant locally and explicitly requires the execution-scope gate to pass before lineage assertions. No production permission or scope check was weakened. Verification remains pending.

Physical process 99864 was intentionally interrupted (terminal exit 2, 258.70s) after inspection identified a fixture lock inversion: its separate owner transaction requests tenant FOR SHARE while defectPersonTx retains tenant FOR UPDATE. This is not a lineage verdict. Replaced the second transaction with SET LOCAL ROLE photo_fn_owner on the existing transaction, restoring photo_scheduler immediately after component assertions. The test connects as the disposable owner and normally assumes scheduler role; production scheduler grants remain unchanged. Original unconditional transaction rollback remains in place.

Physical process 59444 PASSED: 1 passed, 12 deselected in 242.71s, exit zero. Python requires skin lineage and composed ACL markers. Real retained skin preparation passes the owner-only correspondence component; changed root UUID, segmentation pixel UUID and duplicate parser entry are denied. All tested application roles, including scheduler, are denied direct component execution. Normal registered manual native admission passes the composed 00717 wrapper. This is not an end-to-end native skin queue/scientific/GPU qualification claim. Full V20–V32 remains incomplete.

Added forward-only migration 00718: owner-only semantic_defect_policy_expectations_match requires exactly one enabled/licensed admission v1 policy for each required defects/face/parser/person operation, constructs full Expected values including bounded unique checks and fixture/calibration/evidence sets, and matches source, person and every selected face/parser expectation. Composed admission calls this before the previous full gate, which retains global policy barrier/re-discovery. Added physical positive and four negative assertions plus ACL coverage. Process 78418 is running; migration/runtime assertions remain unverified. The Python mandatory policy marker still needs adding after this run terminates (its current test still fails on any Go assertion failure). Full V20–V32 remains incomplete.

Physical process 78418 PASSED: 1 passed, 12 deselected in 227.56s, exit zero. Migration 00718 composed admission accepts the real registered manual native packet; owner-only component requires full policy equality and rejects changed fixture set, empty evidence set, changed calibration set and duplicate admission policy. Application-role ACL matrix passes for the new component and the revoked previous admission entry. Added Python mandatory V27_DEFECT_SQL_POLICY_EXPECTATIONS_OK marker afterward; Python AST parses. Go assertions already required the marker path to execute before this successful physical exit; the added outer marker assertion itself has not been rerun. Dedicated atomic native sealing/queued admission, complete public continuation and broader V20–V32 remain incomplete. No production migration or deployment.

Added forward-only migration 00719 and private anchor {intent_text,snapshot_text} to the retained admission packet. Go packet generation verifies canonical intent/request hash and pinned target snapshot bindings against prepared inventory. SQL anchor component derives deterministic native UUID, binds exact intent/snapshot SHA and request SHA, re-discovers and locks full ordered target inventory, locks run-derived published intent document and snapshot/recipe heads. The new wrapper validates anchor and removes only this extension before the previous strict packet gate; all prior source/content/producers/person/policy/registration checks remain. Physical assertions cover positive and altered intent/snapshot bytes or request hash, plus owner-only ACLs and mandatory Python marker. Process 77935 is running; migration/physical verification pending. No queue activation authority or production deployment. Full V20–V32 remains incomplete.

Physical process 77935 PASSED: 1 passed, 12 deselected in 225.83s, exit zero. Composed 00719 gate and exact native intent/snapshot anchor assertions pass, including changed intent bytes, snapshot bytes and request hash denial; mandatory Python policy/anchor markers and application-role ACL matrix pass.

Added forward-only migration 00720: owner-only native graph/budget component follows all previous evidence gates, requires one exact blocked uncancelled unattempted defects.propose CPU task, no edges/expansions, semantic v2.2 workflow with exact intent snapshot, enabled operation/output policy, bounded output estimate and one live actor-owned reservation with ledger coverage and output+8MiB headroom. It grants no queue/seal authority. Go rejects extra shell edges/workflow-type drift. Added physical budget positive, reservation expiry and run/task cancellation negatives under savepoints, plus ACL and mandatory Python marker. New physical verification running; full V20–V32 remains incomplete. No production migration/deployment.

Physical process 59646 FAILED: 1 failed, 12 deselected in 183.19s, composed admission returned conflict. Inspection found 00720 incorrectly copied generic V2 v2_enabled/document-output conditions: native defects.propose has five legacy output roles and is not enabled in generic V2 registry. Corrected the preparation-only component to require its actual registered CPU backend and exact five native role/media/count policies. This does not enable generic V2, grant queue authority or synthesize a document output. Native gateway/result compatibility and dedicated activation remain required. Verification pending.

Physical process 59534 PASSED: 1 passed, 12 deselected in 222.63s, exit zero. Corrected 00720 actual native output-policy contract, composed admission, live budget, expired reservation denial and run/task cancellation denial all pass with mandatory marker and owner-only ACLs. Worker adapter/preparation suite process 40893 PASSED: 13 tests in 4.38s; retains five outputs, PNG/labels equality and protection exclusion.

Gateway audit: operation_registry defects.propose is not generic-V2-enabled. Existing V2 gateway dispatch/input/upload/claim checks require that flag; guard_v2_result_root accepts exactly one document-role root, while native worker publishes five roles (defect_labels, defect_protection, defect_labels_png, defect_candidates, defect_propose_report). DefectProposeReport links candidates/protection/PNG; candidates links labels. Existing original-producer SQL currently binds all five canonical outputs. Next implementation must retain native contracts and verification through gateway/result publication and native adoption before enabling queue/seal; adding a synthetic document role or merely toggling v2_enabled is insufficient. No runtime queue activation, production migration or deployment. Full V20–V32 remains incomplete.

Added forward-only migration 00721: owner-only native result readiness validates running sealed semantic v2.2 task/run, native config task/digest/Params identity, exact current attempt/fence and five ordered unique native output refs against ready verified artifact/output_upload metadata. V2 result trigger delegates native defect results to this check; all other V2 single-document rules remain unchanged. Registry v2_enabled and queue remain unchanged/closed. Added draft-result denial and component ACL assertions. Physical verification running; this scenario cannot prove positive leased native gateway publication, which remains required. No scientific/RTX qualification or full V20–V32 completion claim.

Physical process 27423 PASSED: 1 passed, 12 deselected in 234.77s, exit zero. Migration 00721 installed, all existing composed admission assertions and application-role ACLs pass. The draft-result assertion used empty outputs, so it proved shape denial only, not the valid-five-output draft-state branch. Corrected that assertion afterward to submit five syntactically valid real fixture references; this amended assertion is pending verification. This does not prove positive leased native gateway publication or review adoption; dedicated seal/dispatch/claim/adoption remain unfinished. Full V20–V32 stays incomplete.

Physical process 27050 PASSED: 1 passed, 12 deselected in 223.53s, exit zero. Amended native draft-result assertion now submits five syntactically valid fixture references; task remains blocked/unsealed and the native readiness gate denies before publication. Mandatory marker and application-role ACLs pass. This is no positive leased-native-output evidence. Worker runtime serializes OperationResult outputs without a generic-V2 root rewrite, so its five roles can be retained; gateway dispatch/claim still requires explicit registry support. Next required work: dedicated atomic native seal with full preparation proof locks and exact idempotency record/ticket/event handling; native dispatch/input/upload/result positive physical scenario; canonical adoption/review continuation. Generic core seal cannot be reused unchanged because it requires a document role. V20–V32 remains incomplete, with no production migration/deployment or registry enablement.

Added forward-only 00722 seal_semantic_defect_preparation and private Go sealDefectPreparationInTransaction. Fresh Go path repeats registered preparation/person/source locks; SQL checks live authorization, exact saved config/policy/idempotency binding, full composed evidence/budget, explicit V2 operation capability, then atomically seals/activates, emits one run event/outbox and completes the exact 202 ticket. Sealed same-request replay authenticates principal and returns saved ticket without activation. Consent-bearing native workflow links are denied pending their separate consent contract. No global registry flag change. Physical fixture temporarily enables the operation under a savepoint, checks same ticket replay and exactly one event/outbox, then rolls everything back; ACL added for non-scheduler roles. Runtime verification is pending; positive dispatch/lease/worker/gateway/review continuation and complete V20–V32 still required. No production migration/deployment.

Native Go seal now checks context cancellation, requires photo_scheduler and installs local app.tenant_id before ticket lookup/seal. This addresses replay in a new transaction without inherited GUC context. Physical process 80975 is still live and compiled the earlier Go version; it must not be treated as verification of this new context change. Runtime/committed replay and lease/gateway checks remain required.

Physical process 80975 PASSED: 1 passed, 12 deselected in 270.77s, exit zero. 00722 dedicated seal returns identical saved ticket twice and creates exactly one queued run event/outbox in the disposable savepoint; ACL assertions pass. It tested the older Go version before explicit context initialization. Added reset app.tenant_id before replay and explicit queued/sealed run + ready task assertions to verify the new context behavior. Rerun pending. Operation capability and activation/events still rolled back together; no production registry enablement. Committed replay, real dispatch/lease/worker/gateway and review continuation remain incomplete, as does full V20–V32.

Physical process 80642 PASSED: 1 passed, 12 deselected in 292.16s, exit zero. Explicit scheduler/local tenant setup handles replay after cleared GUC; queued/sealed run and ready task assertions pass, along with same-ticket and exactly-one queued event/outbox.

Extended the disposable native savepoint with an actual resource node, CPU slot and registered qualified v2.2 worker; reserve_dispatch uses the frozen task resource estimate, then photo_worker claim_task must return claimed with a positive fence and lease/deadline context. No task state/fence is manually patched. Registry/lease/events/resources all roll back with the savepoint. Verification pending; this is SQL dispatch/claim, not actual worker execution, mTLS uploads, result publication, GPU/scientific qualification or full V20–V32 completion.

Physical process 12573 PASSED: 1 passed, 12 deselected in 282.71s, exit zero. Real native reserve_dispatch and photo_worker claim_task return queued/claimed with lease/fence context, after dedicated seal; no manual running-state patch. Savepoint rolls back capability, resources and task transition.

Extended the leased native fixture with photo_gateway reads for all registered closure rows, manifest and chunks, requiring exactly one matching SHA per pin. Stale fence, different worker and certificate mismatch must yield no input rows. Verification pending; this is SQL gateway authorization, not an mTLS transfer or actual native algorithm/result/review execution. Full V20–V32 remains incomplete.

2026-10-06: Physical process 68454 PASSED: 1 passed, 12 deselected in 334.33s. Native leased gateway reads validate all closure/manifest/chunk pins and reject stale fence, worker and certificate mismatch. Added pending real immutable-object uploads through begin_output/complete_output for the five native roles and positive metadata readiness plus stale-fence denial. Uses existing fixture bytes; does not execute the algorithm, rebind report links, prove mTLS transfer, commit TaskResult or review adoption. All SQL state remains disposable/savepoint-scoped. Full V20–V32 remains incomplete.

2026-10-06: Integration-tag Go compilation PASSED (semanticproposal, no tests run). Physical five-output upload/metadata verification launched as process 7415 and remains pending; do not treat compilation or launch as runtime success.

2026-10-06: Added private sealRegisteredDefectPreparation coordinator: validates context/pool, owns a DB-only retryable transaction around the full dedicated seal, resets result on retry, returns zero ticket on callback/commit failure and exposes ticket only after commit. Immutable publication stays outside this transaction. Go semanticproposal/v2input tests PASSED (1.337s/cached). The method is not yet wired to public native admission or proven by a committed physical scenario; running physical process 7415 compiled the earlier implementation and only exercises the in-transaction seal.

2026-10-06: Physical process 7415 FAILED (232.57s): all five begin_output/real immutable Put/complete_output calls succeeded, but metadata readiness returned false. Claim only started tasks; run remained queued because 00611 only starts approved Apply runs. Added forward-only 00723 replacement of public claim wrapper (same OID), preserving Apply behavior and atomically starting only sealed, uncancelled exact native semantic v2.2 runs with matching task/digest and ready closure. Added explicit run-running assertion. Runtime rerun pending.

2026-10-06: Physical process 67546 PASSED: 1 passed, 12 deselected in 282.93s. 00723 starts native run during actual claim; real immutable uploads of all five roles through SQL gateway and metadata readiness pass, stale fence denied. Extended next pending scenario through commit_task_success, same-result replay, canonical result singleton and slot/reservation release. Uses copied real fixture bytes, not algorithm execution or rebinding/report content adoption; SQL remains disposable savepoint-scoped. Full V20–V32 remains incomplete.

2026-10-06: Physical process 14111 PASSED: 1 passed, 12 deselected in 289.88s. Native five-role TaskResult passes actual result trigger/commit_task_success, identical replay returns same result, exactly one canonical result remains and slot/reservation release passes. Copied fixture report bytes are not a content adoption proof. Added 00724 API-only native canonical result catalog with tenant/editor/project/source/current completed attempt, exact five role/upload/object metadata and existing origin gate. ReadNativeDefectResultEvidence resolves this catalog, runs full existing document/pixel/PNG/protection verification, then repeats catalog identity. Go tests passed before SQL addition (1.465s/cached); physical catalog assertions and complete content path pending. Full V20–V32 remains incomplete.

2026-10-06: Physical process 41251 PASSED: 1 passed, 12 deselected in 284.19s. Native canonical catalog returns exact result/output/Params pins and denies a foreign tenant; prior TaskResult/upload/claim assertions pass. Added pending committed sealRegisteredDefectPreparation scenario at the end of manual-lock test: fresh scheduler transaction commits admission, separate scheduler pool replays ticket, separate owner connection observes queued/sealed run and ready task. Disposable registry capability restored to false on exit. This enables future actual mTLS/worker scenario but does not yet execute the algorithm, validate newly generated report content or adopt review. Full V20–V32 remains incomplete.

2026-10-06: Physical process 30693 PASSED: 1 passed, 12 deselected in 259.18s; dedicated coordinator commits admission and independent pool replay/owner observation succeeds. Added pending native mTLS scenario: real reserve_dispatch/claim with actual client certificate, TaskArtifacts/V2InputIndex and CPU defects.propose adapter, real five-role uploads, labels/PNG/protection/document links, commit/replay and independent Go ReadNativeDefectResultEvidence. Gateway tests PASSED (0.611s) after allowing loopback listener; worker probe AST parses. New integrated physical scenario pending. No production registry enablement or full V20–V32 completion.

2026-10-06: Physical process 60917 FAILED (272.75s): committed preparation and real native claim succeed; mTLS declared input downloads pass, but TaskArtifacts V2InputIndex rejects SQL fallback contracts_sha=341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815 before algorithm execution. 00534 emits/accepts this immutable pin, missing from worker allowlist. Added exact SQL_TRANSPORT_SCHEMAS_SHA256 compatibility pin and included it in disk-backed closure tests; unknown-hash denial retained. Verification pending. No scientific qualification or full V20–V32 completion claim.

2026-10-06: Physical process 75918 FAILED (309.16s): exact SQL transport compatibility succeeds; real CPU algorithm fails on plan_roi DEGENERATE_FACE_LANDMARKS for retained fixture face. Existing report/skin-mask contracts support degenerate_landmarks skip. Added shared proposal-specific ROI planner: only this exact geometry error becomes skipped face evidence; other ROI/budget errors propagate, degenerate masks still receive bounded finite/unit validation, permanent/additional protections remain max-unioned. Materialized-protection verifier follows the same skip contract. Regression test checks skipped report, no labels, exact required protection, NaN denial and cleanup. Runtime verification pending; full V20–V32 incomplete.

2026-10-06: Added mixed-valid/degenerate face regression: labels, protection and candidate tuple for valid faces remain bit/exactly equal with an additional skipped degenerate face. Proposal/adapter suite process 1320 PASSED: 15 tests in 4.06s. Physical CPU/mTLS scenario process 23144 remains live/pending; do not infer runtime success from these adapter tests. Full V20–V32 remains incomplete.

2026-10-06: Physical process 23144 FAILED (300.56s): real CPU algorithm/mTLS five-output publication and commit/replay now pass; independent Go content verifier fails with missing catalog row. Existing 00668 protection lookup requires direct producer.inputs, while native task only bootstraps Params/image/RGB and consumes immutable registered V2 closure. Added 00725 owner-only semantic_defect_declares_input and replaced only defect-consumer membership predicates in required protection, selected landmark and face-mask roots; original source/canonical producer/current result/object/provenance gates remain intact. Native branch requires succeeded task, sealed uncancelled semantic v2.2 run, exact config task/digest, ready closure and same project/tenant exact UUID/SHA typed membership. Runtime verification pending. Full V20–V32 incomplete.

2026-10-06: Physical process 28438 FAILED (37.17s), existing unfinished-attempt denial detected an accidental downgrade: 00725 landmark replacement copied 00652 instead of latest 00655 completed-attempt implementation. Corrected only that replacement to 00655 with the single membership predicate changed; finished_at/backend/profile guards and original ACLs preserved. 00660 face-mask definition already retains completed mask/parser attempts. Runtime rerun pending. Full V20–V32 incomplete.

2026-10-06: Physical process 64808 PASSED: 1 passed, 12 deselected in 332.83s. Corrected 00725 preserves unfinished-attempt denial and original checks. Committed native admission, actual reserve/claim, exact closure mTLS downloads, real CPU algorithm with degenerate-face report, real five-output mTLS uploads/MinIO versions, commit/replay and independent Go full document/NPY/PNG/protection evidence verification all pass. This is one controlled fixture, not full V27/public rollout, scientific/model/RTX qualification, parent continuation or entire V20–V32 completion. Added pending actual-result selected landmark/manual-mask provenance reads, five scope substitution refusals with zero result and owner-only helper ACL matrix.

2026-10-06: Physical process 21756 PASSED: 1 passed, 12 deselected in 309.97s. Actual native result passes selected landmark and manual-mask original-root/vector/NPY/PNG provenance reads, five scope substitutions fail with zero result, and declaration helper stays owner-only across application roles. Scope substitutions use unknown UUIDs, not a separately seeded real foreign tenant. Integrated these proven input provenance readers into ReadNativeDefectResultEvidence itself and added returned Faces/MaskRoots before final canonical-catalog recheck. Public native admission/parent continuation, real foreign-tenant tests, concurrency/revocation and model/hardware qualification remain required. New production-boundary runtime verification pending; full V20–V32 incomplete.


2026-10-06: Physical process 25952 PASSED: 1 passed, 12 deselected in 264.41s. Production ReadNativeDefectResultEvidence now verifies selected landmark and manual-mask provenance alongside actual CPU/mTLS five-output content and final canonical catalog recheck. Unknown scope UUID refusals and helper owner-only ACL pass. Added pending real second-tenant fixture with its own owner/session/project/accepted source version and shared actor membership. Five cross-tenant cases require empty results and zero object-store reads via denial spy, including the same actor belonging to both tenants. Physical process 38496 launched for this extended scenario; result pending. Full V20–V32, public native admission/parent continuation and model/hardware qualification remain incomplete.


2026-10-06: Physical process 38496 PASSED: 1 passed, 12 deselected in 302.32s. Real second-tenant scope checks pass after actual CPU/mTLS result publication: foreign owner scope, shared-member foreign scope, foreign project, foreign source version and foreign actor all return errors with empty result/provenance and zero object-store reads. The second tenant has actual committed owner/session/project/accepted-version metadata and the original actor is its editor. Controlled synthetic second-tenant metadata suffices for authorization-before-IO tests; no scientific/hardware qualification or full product-wide isolation is implied. Public native admission/parent continuation and full V20–V32 remain incomplete.


2026-10-06: Added pending 00726 scheduler-only lock_semantic_defect_native_result plus private nativeDefectAdmissionProof retained only after complete reader verification. Reader captures/rechecks exact original object pins and PNG metadata, then canonical catalog. DB-only lock composes live principal/project/source authority, tenant inventory writer barrier, original producer/attempt/result/object locks, output-upload and PNG locks, and exact result/ordered outputs/Params/object metadata equality. Altered retained canonical result UUID must be denied. This component does not qualify models/selected input producers, admit parent/review, grant candidate approval or queue a parent. Go suites process 83070 PASSED (semanticproposal 1.984s, v2input cached). Physical process 88721 pending; full V20–V32 incomplete.


2026-10-06: Physical process 88721 FAILED: 1 failed, 12 deselected in 308.44s. Full native content read passed; new positive SQL canonical-result lock returned false. Corrected missing parentheses around origin JSON array selection before subtracting metadata keys in 00726; PostgreSQL arithmetic precedence otherwise attempts subtraction from the integer index, caught as invalid input by the refusal handler. No authorization predicate was relaxed. Runtime rerun required; full V20–V32 incomplete.


2026-10-06: Corrected 00726 full physical rerun launched as process 62113; result pending. No live process was restarted on an observation timeout: prior process 88721 terminated with a real test failure before this rerun.


2026-10-06: Physical process 62113 PASSED: 1 passed, 12 deselected in 256.79s. Corrected 00726 SQL native canonical-result lock accepts the private proof returned after actual CPU/mTLS result verification and denies a replaced canonical ResultID; transaction rollback releases locks. Actual five-output publication/content/input provenance and real foreign-tenant/shared-member zero-IO refusal tests remain green in this same controlled fixture. No parent workflow or review is advanced by this component. Full V20–V32, native first-producer preparation, consent/public admission, final qualification/parent continuation and scientific/model/hardware qualification remain incomplete.


2026-10-06: Added 00727 API parent association predicate and scheduler parent/run lock, plus ReadNativeDefectResultForRequest and private nativeDefectParentProof. Reader recompiles owned parent request/key, validates actor/workflow/request hash/canonical intent, binds original intent and snapshot refs, requires deterministic native child run and exact saved parent/source/request/session association before object IO and after full result verification. lockNativeDefectContinuation composes existing canonical output lock with parent association recheck under native task/run locks. It does not activate parent, qualify models, approve review or emit continuation. Go suites process 96941 PASSED: semanticproposal 1.792s, v2input cached. Physical process 17082 launched with real parent-positive/transaction-lock and different-parent zero-object-IO refusal cases; result pending. Full V20–V32 remains incomplete.


2026-10-06: Physical process 17082 FAILED: 1 failed, 12 deselected in 260.41s. Canonical-result transaction lock passed; parent binding failed with SQLSTATE 42883 because 00727 called an unavailable uuid-ossp function. Corrected to existing photo.semantic_uuid_v5, the same deterministic helper used by 00719 native admission anchor. No extension installation or authority relaxation. Parent binding runtime rerun required; full V20–V32 remains incomplete.


2026-10-06: Corrected parent binding physical rerun launched as process 79183; result pending. Prior process 17082 terminated with an explicit SQL dependency failure before this rerun.


2026-10-06: Physical process 79183 PASSED: 1 passed, 12 deselected in 284.59s. Corrected 00727 parent binding uses existing semantic_uuid_v5. Actual native result is readable only for the owned original parent request with exact saved source/intent/snapshot/session/request SHA and deterministic child association. Composed canonical-result/parent transaction locks pass, and a separately compiled different parent request is refused before object IO. Prior CPU/mTLS five-output content/provenance, result-ID substitution refusal and real second-tenant checks remain green. Native protection NPY still requires a registered source/result-bound MaskRef derivation for transfer, and complete policy/qualification/parent admission is not yet implemented. Full V20–V32 remains incomplete.


2026-10-06: Added BuildSemanticDefectProtectionDocument as the content conversion stage for native transfer continuation. It verifies complete original Params/report/proposal/source links using VerifySemanticDefectEvidence, extracts the report's existing complete inline protection, canonicalizes it and validates the MaskRef contract. No pixel geometry/selector/confidence/model fields are fabricated; emitted document contains exactly original inline protection fields and retains the original NPY ref. Added fixture checks for exact canonical inline bytes, transfer ResolveSemanticMaskPixels compatibility and empty-output refusal on mismatched Params SHA. Immutable publication and durable parent/source/canonical-result provenance registration remain required before consumption. This helper alone does not publish/register a root or qualify/activate the parent; full V20–V32 remains incomplete.


2026-10-06: Go verification process 16488 PASSED: v2input 8.089s and semanticproposal 1.449s. The new inline protection conversion tests verify exact original canonical MaskRef bytes, original pixel ref, source-bound transfer compatibility and no output on Params hash mismatch. No new physical publication/registration path is claimed.


2026-10-06: Added prepareNativeDefectProtection private publication preparation. It reads full native content/provenance for the exact owned parent request, reads original source, compiles original report inline MaskRef with complete Params/report/proposal links, verifies original protection pixels ref and repeats full parent/native content verification after source IO. Exact retained private parent/result/original-object/PNG metadata must remain equal. Stable document UUID derives from parent run and native task; owned document bytes are retained as a string with original pixels and parent proof. This is a publication input only, not a registered root; immutable publication/durable provenance registration and full admission remain required. Go process 52220 PASSED (semanticproposal 1.548s, v2input cached). Physical process 57467 launched for actual worker document preparation; result pending. Full V20–V32 incomplete.


2026-10-06: Physical process 57467 PASSED: 1 passed, 12 deselected in 296.87s. Actual native worker result yields original inline MaskRef preparation with stable document UUID/original NPY ref and unchanged full parent/result proof across source IO. Added PublishSemanticProtectionDocument through existing accepted-version draft reservation/quota/fencing/immutable object publication/recovery, after source-bound protected MaskRef validation. Added pending physical publication/replay check and denial of unregistered derived root by existing canonical protection origin. Durable native result/source/parent provenance registration remains required; no parent activation or full V20–V32 completion is claimed.


2026-10-06: Go process 85848 PASSED: v2draft 3.708s, semanticproposal 1.564s, v2input cached. Physical process 86885 launched for immutable MaskRef publication/replay and unregistered-origin denial; result pending. Native provenance registration and full parent admission remain incomplete.


2026-10-06: Physical process 86885 PASSED: 1 passed, 12 deselected in 372.21s. Actual CPU/mTLS native output is converted to the original inline protected MaskRef, published through real accepted-version immutable object upload and replayed with the same root ref. Existing VerifyProtectionOrigin refuses the backend-created derived root before dedicated provenance registration, so publication does not falsely impersonate a canonical worker output. Earlier parent/canonical-result and second-tenant gates also remain green. Durable native source/result/parent registration and root consumer lookup/admission remain incomplete; full V20–V32 remains incomplete.


2026-10-06: Added 00728 semantic_native_protections tenant-scoped immutable provenance registration with RLS, bounded anchor and source/parent/task/canonical result/root/pixels/report pins. Scheduler-only registration validates live scope, inventory barrier and native parent; compares canonical result, deterministic derived document identity, actual report SHA, exact inline protection JSON and published ready accepted-version upload/artifact metadata. Conflict replay requires all stored provenance fields equal. Go registerNativeDefectProtection composes full canonical-result/parent locks in one DB-only retryable transaction after immutable publication. New table added first to existing finish_tenant_deletion explicit allowlist, preserving its original cleanup guards/body and granting DELETE only to photo_fn_owner for the already-authorized purge. No direct application DML grants. Actual report text is retained privately with preparation. Added pending physical registration/replay and changed-report-byte refusal; consumer lookup/admission still required. Go process 32383 PASSED (semanticproposal 1.540s, v2draft/v2input cached). Physical process 56682 launched, result pending; full V20–V32 incomplete.


2026-10-06: Physical process 56682 FAILED: 1 failed, 12 deselected in 422.01s. Native canonical/parent/content/MaskRef publication checks passed; first provenance registration failed SQLSTATE 42501 on semantic_native_protections. SELECT FOR SHARE requires UPDATE privilege; new registry intentionally grants only SELECT/INSERT and explicit purge DELETE to photo_fn_owner. Removed unnecessary saved-row FOR SHARE instead of widening UPDATE authority. Insert-only registry replay is serialized by INSERT ON CONFLICT; source/principal/native result/run/upload/artifact locks remain, including tenant authority locks blocking active-to-deleting transition. Runtime rerun required; consumer lookup/full parent admission and full V20–V32 remain incomplete.


2026-10-06: Corrected provenance registration physical rerun launched as process 42221; result pending. Prior process 56682 terminated with explicit privilege failure before this rerun. No application UPDATE/DML permission was added.


2026-10-06: Physical process 42221 PASSED: 1 passed, 12 deselected in 326.07s. Corrected insert-only provenance registration succeeds and exact replay succeeds; altered report bytes are refused. Native canonical/parent/content/publication and tenant checks remain green. Added pending 00729 API-only exact-parent registered root/original NPY catalog and ReadRegisteredNativeDefectProtection: full parent/native original bytes, exact published MaskRef bytes versus original report inline mask, exact original NPY metadata, repeated registered object/catalog authority including final root recheck after dependency IO. This does not activate parent or qualify models. Generic protection-origin lookup intentionally unchanged; transfer wiring/final admission/GC retention/purge runtime qualification remain required. Full V20–V32 incomplete.


2026-10-06: Go process 24366 PASSED: semanticproposal 1.364s, v2draft/v2input cached. Physical process 51789 launched for 00729 registered protection content/catalog reads against actual native worker publication/registration; result pending. Full V20–V32 remains incomplete.


2026-10-06: Physical process 51789 PASSED: 1 passed, 12 deselected in 290.83s. 00729 exact-parent registered root/original NPY catalog and ReadRegisteredNativeDefectProtection verify actual published MaskRef bytes against the original report and canonical protection NPY metadata, then repeat full native proof and final registered root catalog after object IO. Added PublishRegisteredNativeDefectProtection coordinator to own canonical request, prepare exact parent-bound document, immutable publish, DB-only register and final registered read, returning zero on errors. Stable identities recover partial durable state; this component does not qualify models or activate/admit transfer parent. Added pending real committed coordinator replay test; full V20–V32 incomplete.


2026-10-06: Go process 72793 PASSED: semanticproposal 1.626s, v2draft/v2input cached. Physical process 44328 launched for protection coordinator committed replay and full registered content verification; result pending. Transfer policy/qualification/source protection separation/admission wiring remains required. Full V20–V32 incomplete.


2026-10-06: Physical process 44328 PASSED: 1 passed, 12 deselected in 364.26s. PublishRegisteredNativeDefectProtection coordinator committed replay returns the same verified root/pixels/document after preparation, immutable publication, registration and final exact-parent registered content read. Added pending VerifyNativeSelectionSeparation retaining private registered object/parent proof from the verified reader, streaming selected and original protection NPY through existing nonempty/zero-positive-overlap validator, and repeating selected/root/native metadata after all dependency IO. Existing generic mask origin is unchanged; no model qualification or transfer admission is bypassed. Transfer person coverage and complete final admission/wiring remain required; full V20–V32 incomplete.


2026-10-06: Go process 20016 PASSED (semanticproposal 1.416s, v2input/masknpy cached); after final selected/root metadata recheck ordering, process 10262 PASSED semanticproposal 1.401s. Physical process 69672 launched for native selection separation in the real worker/registered-protection scenario; result pending. Full V20–V32 remains incomplete.


2026-10-06: Physical process 69672 FAILED: 1 failed, 12 deselected in 359.76s. All native content/registration/coordinator checks passed; new separation gate returned masknpy.ErrInvalid while the test assumed success. Added an independent oracle reading actual selected MaskRef/NPY and actual canonical native protection NPY from pinned MinIO versions with exact SHA/byte/type/shape/finite/unit checks. Integration expectation now requires zero-count ErrInvalid on independently proven positive overlap, or exact independently counted selected pixels when disjoint. No production protection/geometry threshold changed. Runtime oracle result still pending; no successful separation/refusal claim yet. Full V20–V32 incomplete.


2026-10-06: Physical oracle/separation rerun launched as process 4389; result pending. Prior process 69672 terminated with an explicit assertion failure before rerun. The independent oracle consumes exact pinned object bytes; a real protected overlap must be refused, not silently cleared.


2026-10-06: Physical process 4389 PASSED: 1 passed, 12 deselected in 306.43s. Native selection separation agrees with independent SHA/byte/type/shape/finite/unit-checked actual selected/protection NPY oracle: protected overlap requires zero-count ErrInvalid, disjoint masks require exact selected count. All earlier native content/registration/coordinator/tenant checks pass in the same controlled fixture. Added pending VerifyNativePersonProtectionCoverage: independent exact-source person discovery, full original person evidence and qualification report/profile verification, streaming positive person support coverage by native registered protection, then exact person evidence/qualification/candidate/array/native catalog rechecks after IO. Complete policy inventory/final TX admission, transfer wiring and full V20–V32 remain incomplete.


2026-10-06: Go process 63811 PASSED (semanticproposal 1.432s, v2input/masknpy cached); after retaining original person mask/evidence before qualification IO, process 10508 PASSED semanticproposal 1.217s. Physical process 93738 launched for qualified native person coverage in the full controlled CPU/mTLS/registration scenario; result pending. Full V20–V32 remains incomplete.


2026-10-06: Physical process 93738 PASSED: 1 passed, 12 deselected in 325.17s. Native registered protection covers the independently selected qualified mask.person root with exact count 1 while retaining original evidence/report/profile/model authority and post-IO catalog rechecks. Added pending private nativePersonProtectionProof, 00730 scheduler-only lock_semantic_native_protection_evidence and lockNativePersonProtection composing native canonical/parent locks, exact registered root/upload/original NPY locks, original person producer/evidence locks and qualification/policy/report locks. No object IO spans this transaction component. Added retained-proof success and substituted person-evidence refusal tests; full policy inventory/source/transfer graph admission and all V20–V32 remain incomplete.


2026-10-06: Go process 17669 PASSED: semanticproposal 1.853s, v2input/masknpy cached. Physical process 48615 launched for 00730 registered native/person proof transaction locks and substituted-evidence refusal; result pending. Full V20–V32 remains incomplete.


2026-10-06: Physical process 48615 PASSED: 1 passed, 12 deselected in 503.43s. 00730 and lockNativePersonProtection accept the privately retained actual registered native/person coverage proof, retain original native result/parent/root/upload/NPY/person evidence/qualification locks and reject substituted person evidence []. Same controlled fixture preserves actual CPU/mTLS content, root publication/registration/coordinator replay, independent NPY separation oracle and qualified person coverage. This does not prove full policy/source/selection/admission race matrix, real model/RTX qualification, transfer wiring/parent activation or entire V20–V32. No live process retained after this terminal result.


### 2026-10-06 — retained native selection separation proof

`verifyNativeSelectionSeparation` now retains a private proof only after successful nonempty zero-positive-overlap NPY verification. The proof contains exact selected MaskRef and MaskPixels refs/object versions, selected pixel count, and registered native protection/parent authority. The selected root catalog pin is captured before selected content validation and rechecked after all object IO. Every refusal returns a zero private proof. Public API remains unchanged.

Go tests: semanticproposal passed (1.383s); v2input/masknpy passed (cached). Physical integration run started; result pending. This is evidence retention only: scheduler TX lock for selection pins and complete parent transfer admission remain outstanding.


### 2026-10-06 — selection proof physical check and TX component

Physical run 96831 passed: `1 passed, 12 deselected in 326.47s`. This verifies retained-selection reader compatibility with the actual CPU/mTLS native pipeline and existing scope/qualification checks.

Added forward-only `00731_v27_native_selection_evidence_lock.sql`: scheduler-only function composes the original full proposal inventory lock, requires exact root/child pin shape and parent edge, locks both artifact rows and matching upload rows, repeats scope, compares complete immutable object metadata. No new table rights. `lockNativeSelectionSeparation` composes native result/parent, registered native protection pins, and selection locks in the caller transaction; no object IO or graph activation. Its serializer binds original intent digest/source/parent request/session and emits retained versions only.

Unit tests passed: semanticproposal 1.236s; v2input/masknpy cached. Added tests for changed snapshot/session/root/SHA/media/null version and empty selection; integration now requires zero private proof on overlap and tests exact/changed-version selection locks only on independently proven disjoint selection. Positive native TX admission remains unproven if current fixture follows overlap branch; no fabricated success. New migration physical verification pending.


### 2026-10-06 — 00731 physical load and positive selection fixture

Run 30209 passed: `1 passed, 12 deselected in 320.16s`. It loads 00731 and verifies actual native pipeline overlap refusal with zero private proof; it does not establish positive selection TX admission. Added separate `manual-lock-go-disjoint-external` case with a real immutable user mask selecting only pixel (0,0). Independent bounded/hash-checked NumPy reads of actual selected/protected NPY must prove exactly one selected pixel and no positive overlap. The case requires `V27_NATIVE_DEFECT_SELECTION_TRANSACTION_LOCK_OK`, including rejection of modified retained NPY object version. Added actual role EXECUTE/owner/search_path checks for 00731. Positive physical run pending.


### 2026-10-06 — composed native reference protection component

Added private `nativeReferenceProtectionProof`, `prepareNativeReferenceProtection`, and DB-only `lockNativeReferenceProtection`. Preparation requires both nonempty/zero-overlap selection and independently qualified person coverage against exactly the same registered root/original NPY/native parent proof, then repeats exact selected root/child catalog versions after all later person object IO. Every error returns zero proof. Lock composes both existing components in one caller-owned scheduler transaction; any refusal requires complete rollback. No public endpoint, target protection policy, transfer activation or graph mutation is added by this component. Physical positive composition test remains required; separate selection positive run is still pending.


### 2026-10-06 — positive selection TX proof established

Run 46189 passed: `1 passed, 13 deselected in 290.86s`. The dedicated one-pixel fixture independently verifies actual original CPU-worker protection NPY has zero positive overlap with the real selected mask. Required Go marker establishes successful 00731 selection lock and denial of a changed retained NPY version; role tests verify scheduler-only execute. This proves positive selection component, not parent graph activation or full transfer.

Added mandatory composed-reference preparation/TX test to the disjoint case: successful combined selection and qualified-person proof, refusal of mixed registered root versions, refusal of empty person evidence, rollback after each. Physical combined test pending.


### 2026-10-06 — composed proof test deadline correction

Run 88756 failed after 428.45s. Required markers prove selection TX and qualified person TX passed; additional full composed-reference preparation exhausted the shared 90s Go context (`context deadline exceeded`, 90.17s), so combined success is NOT established. Expanded only integration budgets to context150s/Go180s/subprocess210s for the added complete dependency reads. Production verification, model quality, scope and evidence gates unchanged. Retry pending. GC registry retention gap identified in latest00350 reference predicate; migration not yet applied.


### 2026-10-06 — composed native reference proof PASS and GC regression

Run 63432 passed: `1 passed, 13 deselected in 243.00s`, with mandatory combined reference preparation/TX marker, exact selection native pins, qualified person original proof, mixed registered root version denial and empty person evidence denial. Test budgets only were expanded after prior 90s deadline failure. No parent graph activation is proven.

Added isolated owner-seeded metadata GC regression; seed is explicitly not native content/producer admission. After correcting test syntax/run domain/V2 fixture mismatch, run24276 failed at first retained-child assertion (`False`,10.38s), reproducing missing registry edges. Added 00732 preserving complete latest00350 V2 reference body and function OID/ACL, plus retention of native original pixels/report/source when ready root has direct existing V2 references; retirement without remaining references releases retention. No new DML grants; adds tenant-scoped lookup indexes. Regression plus existing tenant-cleanup tests running (90383); filled-registry purge and physical object cleanup proof remain outstanding.


00732 verification: run90383 passed, `4 passed in 16.26s`: native child retention while root ready, cross-tenant refusal, maintenance GC claim refusal, retirement releasing child to actual fenced GC claim (rollback), restoration, and prior cleanup token/allowlist tests. Added filled-registry SQL-only metadata purge case as maintenance with wrong-token refusal; no object-store success is implied by this synthetic metadata fixture. Run52836 pending.


Filled-registry metadata purge run52836 reached successful maintenance `finish_tenant_deletion` but the owner verification SELECT failed because `SET LOCAL ROLE` persists after nested savepoint release. Fixed test-only restoration with `SET LOCAL ROLE NONE`; no grants changed. Run13580 pending.


Run13580 passed: `1 passed in 4.31s`. Filled owner-seeded registry metadata is removed by actual maintenance fenced purge, wrong token refused, artifacts removed and tenant becomes deleted. This proves SQL metadata lifecycle only; physical cleanup of real stored versions and full native parent transfer/activation remain outstanding. No live test process remains from these runs.


### 2026-10-06 — real maintenance purge integration

Refactored isolated metadata fixture and added actual `TenantCleaner` integration (maintenance SQL role, real MinIO versioned objects, Batch=1, checkpoint-before-finish, metadata retained after first object pass, neighbor tenant pin checked every pass, final full version listing empty). Registry row is owner-seeded lifecycle data, explicitly not native producer/model authority.

Initial run12246 failed because fixture attempted UPDATE immutable artifacts.bucket; corrected fixture to put real versions and INSERT exact metadata initially, preserving immutable guards and ACL. Original metadata GC/purge case passed in that run. Maintenance Go unit tests passed (cached). Corrected two-case physical run21411 pending; no production state is affected.


Corrected physical run21411 failed at commit on deferred task_results→task_attempts FK, revealing incomplete owner-seeded test metadata (rollback-only case had not committed). Added explicit finished synthetic CPU attempt row; no native result/content authority is inferred. Run98461 pending with all FK/immutability guards enabled.


Run98461 passed: `2 passed in 20.47s`. Real production TenantCleaner invokes maintenance SQL role and MinIO PurgeTenantBatch with Batch=1; at first pass registry metadata remains, multiple checkpoint passes occur, all owned object versions (including duplicate root versions) disappear before final metadata purge, neighbor tenant pin remains, final full version listing is empty and artifacts/registry rows are zero. Fixture is committed owner-seeded lifecycle data, not a claim of native model/producer admission. Native CPU/mTLS provenance and combined proof are independently verified by63432; full parent transfer/activation and real-registry cancellation/revocation matrices remain outstanding.


### 2026-10-06 — V25 GC compatibility and next continuation gate

Run53805 passed: `1 passed, 2 deselected in 18.53s`, preserving actual user-mask publication/recovery/hold/release behavior after00732. Reviewed parent transfer compiler/registration: only legacy protection/person proofs are retained today; native reference integration and per-target authority must be added without promoting generic origin. Also confirmed native preparation freezes full policy SHA in run config, but current combined reference component does not retain/lock this original global policy snapshot at continuation. Next required work is an exact native-parent policy SHA gate plus current snapshot retention/lock, before integrating native proof into transfer admission. Scientific model/report qualification remains separately required.


### 2026-10-06 — native original policy continuation gate (00733)

Added API exact-parent policy SHA predicate and scheduler-only parent+full-policy snapshot lock. Predicate requires original frozen native run config policy SHA and v2_workflow policy SHA both equal retained canonical policy digest. Combined native reference preparation now owns expected values, retains current full operator snapshot, requires all five original producer policy families and exact independent person expectations, compares canonical digest with native preparation and repeats snapshot after IO. Combined TX verifies private scope/digest and composes existing snapshot/parent locks. It does not certify original model reports, target policies, consent, quota or full graph activation.

Go tests passed semanticproposal1.643s, v2input/masknpy cached before extra integration negative cases. Added policy body/SHA replacement refusal and actual role ACL checks. Physical run pending.


### 2026-10-06 — native retained person/policy consistency

Added `matchNativeReferencePolicy` before combined TX SQL: canonical retained snapshot SHA, all five required admitted/enabled/licensed producer families, and retained original independent person Expected must match the same snapshot. New test rejects foreign person runtime, substituted policy digest and revocation of every required family even when the modified snapshot has a valid new digest. This supplements the live00733 snapshot/hash lock; it does not qualify original face/defect reports or admit the full transfer graph. Physical run24138 remains running; unit result pending when this note was appended.


### 2026-10-06 — 00733 physical PASS and live policy revocation test

Run24138 passed: `1 passed,13 deselected in343.64s`, proving current original policy SHA gate/lock and policy body/SHA substitution refusals in actual CPU/mTLS context. Unit86474 passed semanticproposal1.541s; v2input/masknpy cached.

Added real isolated DBA revocation of selected face.parse policy after combined proof preparation. Required checks: new preparation returns zero proof before any object Open; stale retained proof TX refused and rolled back; bounded restoration then original TX proof accepted again. Restoration is deferred even on test failure. New mandatory marker; runtime pending. This tests global policy stability, not qualification-report revocation or full parent activation.


### 2026-10-06 — native defect/face/parser qualification component

Added private `nativeDefectQualificationReports` (explicit deployment report refs), `nativeDefectQualificationProof`, preparation and DB-only lock. Preparation reads original policy, verifies exact-parent native content and policy SHA, original defects qualification report, and declared native landmark/parser producers through existing qualified readers; repeats native result, retained producer/qualification metadata and policy after IO. Private producer counts prevent partial proof sets. TX matches all retained producer Expected families to original snapshot and composes native parent/result, defects qualification, original face/parser evidence/report locks, and native global policy lock. No parent graph or transfer admission yet.

Go tests before count/recheck refinement passed semanticproposal1.153s; v2input/masknpy cached. Refined tests running7950; live policy revocation physical run70077 still running. New qualification component is not physically qualified yet and must be exercised on actual native declared producers, including report revocation/race cases.


Run70077 failed257.27s: native proofs passed until test DBA UPDATE enabled=false violated existing qualification_admission_requires_enabled CHECK. No production defect is indicated; revocation fixture must disable admission_enabled together with enabled. Corrected test-only paired revocation/restoration (selected original row had both true). No constraint weakened. Refined Go tests7950 passed cached. Retry pending.


### 2026-10-06 — original native producer sets and policy revocation verification

Physical run50276 PASSED: 1 passed,13 deselected in358.20s. Actual CPU/mTLS native processing, independent disjoint selection, registered native/person/selection TX proofs and real paired face.parse policy revocation/restoration were verified. Revocation disables enabled and admission_enabled together, preserving the existing CHECK; zero object IO refusal and stale TX refusal precede restored original TX success.

Go run61528 passed semanticproposal1.484s (v2input/masknpy cached). Producer retention now preserves canonical landmark/detector and parser/detector pairs, rather than counts or primary roots alone; shared detectors are valid, duplicate pairs/conflicting digests and detector substitution are rejected. Go run33846 passed semanticproposal1.629s, other packages cached.

Extended physical fixture requires prepareQualifiedNativeDefectResult and lockQualifiedNativeDefectResult on original native Params with deployment-selected defects/landmark/detector/parser reports and deployed ICC. Original must pass; dropped producer evidence with adjusted counts, substituted detector and wrong qualification report must refuse. This extension is pending physical verification; no native transfer graph or public activation completion is claimed. Full V20–V32 remains incomplete.

Integration-tag compile42508 passed semanticproposal0.654s (no tests run). Extended physical native producer qualification run10067 is live; no outcome claimed.


### 2026-10-06 — native reference continuation composition

Added private nativeReferenceContinuationProof and composeNativeReferenceContinuation. It owns producer proof slices and requires identical original native parent/canonical result, identical full global policy snapshot/SHA, matching selection/person registered proof and retained original producer-pair sets/counts. lockNativeReferenceContinuation composes qualification and reference protection locks in the caller-owned transaction without object IO or commit; errors require complete rollback. Target/source pixel/graph/resource/quota admission is still required.

Initial compile/unit run21660 passed semanticproposal1.257s with v2input/masknpy cached. Added focused mixed-parent, mixed-policy/digest, person-parent, partial count/set and unverified-proof rejection checks; run63895 result pending at write time. Physical run10067 remains live and tests the prior qualification component, not this newly added aggregate lock. Full V20–V32 remains incomplete.

Run63895 PASSED: semanticproposal1.785s, v2input/masknpy cached. Mixed native continuation rejection tests are unit evidence only; physical aggregate lock verification remains required.


### 2026-10-06 — native manual producer qualification fixture correction

Physical run10067 FAILED262.99s. Native/reference/person/policy revocation/tenant isolation components passed; new qualification preparation returned no error,faceCount1,parserCount0. Original native Params use manual_mode=true and skin=null; expecting one parser producer was a fixture error. Changed expectation to one landmark and zero parser evidence and explicitly asserts the actual original native Params manual declaration. Missing empty parser declaration is tested by removing its retained canonical set, not by treating an already empty producer list as corruption. This does not qualify a native skin/parser path; a separate actual skin producer scenario remains required.

Extended physical test now also retains the original verified native reference proof and composes it with original qualified defect evidence. Same scheduler TX must accept original, refuse mixed canonical result and refuse missing person evidence after qualification locks; all cases roll back. Retry pending. No production guards/ACLs or qualification requirements weakened. Full V20–V32 remains incomplete.

Integration compile71993 PASSED0.639s (no tests run). Physical retry65598 is live with corrected original manual declaration and required aggregate continuation TX marker; no outcome claimed.


### 2026-10-06 — retained native face qualification reports without producer roots

Added nativeFaceReportProof and bounded scoped report preparation/recheck/DB-only locks for face.landmarks, face.detect and face.parse. Server-selected detector report must be identical in landmark/parser report configuration. Preparation verifies original report bytes using existing qualified readers, retains exact metadata, repeats all report authority after IO. Validation rejects missing families, mixed operation policy, tenant/project/actor/session/bucket drift, duplicate report artifacts, digest/media/version errors. TX uses existing scheduler-only face and face.parse qualification functions, no new grants or migration. This retains qualification authority independently of whether the original native Params have producer evidence for a family; it never fabricates producers.

Initial semanticproposal run41020 passed1.470s. Added report-pin unit tests initially failed compile due a wrong test-only field name DeviceQualificationSHA; corrected to existing DeviceProfileSHA. Refined run3540 pending at write time. Helper is not yet connected to prepareQualifiedNativeDefectResult/lockQualifiedNativeDefectResult; current physical run65598 tests the previous component and remains live. Integration and physical report revocation verification remain required. Full V20–V32 remains incomplete.

Run3540 PASSED: semanticproposal1.401s, v2input/masknpy cached. New report component remains independently tested, not yet wired into native continuation.


### 2026-10-06 — original native face report identity retention

NativeFaceReportProof now retains independent originalReports object pins in addition to working report evidence. Marshal/lock/recheck refuse changing any report identity or immutable object metadata, even when replacing with a different artifact matching the same expected policy. Added alternate-qualified-report substitution rejection. Run37855 PASSED semanticproposal1.262s, v2input/masknpy cached. No native qualification wiring yet: physical retry65598 remains confirmed live, and its source/fixture is left unchanged until terminal. Full native report integration, native skin/parser physical path and full V20–V32 remain incomplete.


Physical run65598 PASSED: 1 passed,13 deselected in299.38s. Original native manual Params qualification (landmark1/parser0), original defects report and report substitution rejection, independent person/selection and aggregate qualification+reference TX positive, mixed-result refusal and missing-person-evidence late refusal/rollback all verified. This does not qualify a native skin/parser producer path or full transfer graph admission.

Connected nativeFaceReportProof to native defect qualification preparation and TX lock. Preparation retains and repeats all three face-family report pins regardless of producer count; lock prevalidates retained original reports before SQL and locks each original policy/import/report in the same caller transaction. Added physical missing-parser-report rejection even with parserCount0. New wiring requires a separate physical rerun; previous PASS does not prove it.


### 2026-10-06 — producer qualification/report identity binding

Go run96510 PASSED semanticproposal1.861s, v2input/masknpy cached after wiring retained original reports into native qualification. Physical run36385 remains live and has no outcome yet.

Added matchNativeFaceProducerReports: original landmark evidence must use the retained landmark/detector report objects, parser evidence the retained parser/detector objects. Policy equality alone cannot substitute a different report artifact. Unit cases cover all four producer/report associations and changed independent original report set, plus legitimate manual-only zero parser producers. Helper is not yet connected to preparation/lock while the physical run is live; source/fixture under physical verification is unchanged. Full V20–V32 remains incomplete.

Report binding helper unit run46579 PASSED semanticproposal1.166s. Production wiring and physical substitution verification remain pending.


### 2026-10-06 — retained native reports physically verified and producer binding connected

Physical run36385 PASSED: 1 passed,13 deselected in278.74s. Original native manual result, independent qualification reports for landmark/detector/parser (parser producer count0), missing parser-report refusal, original combined qualification/reference TX and negative cases are physically verified. All previous native/person/selection/policy/tenant isolation checks still pass.

Connected matchNativeFaceProducerReports to final native qualification preparation, before TX SQL and native reference continuation composition/matching. Each original producer must retain the exact deployment-selected report objects; compatible alternate report identity is insufficient. Added physical changed landmark-report case. New binding wiring requires a new physical run; prior PASS proves the preceding report integration only. Native skin producer path, full transfer/target admission and V20–V32 remain incomplete.


### 2026-10-06 — native reference source closure composition

Added nativeSourceContinuationProof, matchNativeSourceScope, composeNativeSourceContinuation and lockNativeSourceContinuation. They bind the original independently verified WorkingImage/RGB/alpha/warp closure and deployed ICC SHA to the exact native origin's tenant/project/actor/session/bucket/source version/canvas. Transfer gamma-encoded source evidence must use unitRGB=false; this is separate from stricter unit RGB defect producer preparation. Composition owns nested private producer proof slices; TX locks source evidence and complete native reference continuation together without object IO/commit. All errors require caller rollback. Target evidence/full graph/transfer quota admission remains separate.

Added unit refusals for foreign tenant/project/actor/session/bucket/version/canvas/ICC, wrong range contract and missing source closure. Physical run63033 remains confirmed live for prior producer/report binding wiring; this new source component is not yet physically verified or connected to public transfer admission. Full V20–V32 remains incomplete.

Run41452 PASSED: semanticproposal1.265s, v2input/masknpy cached. Source/native composition physical verification remains pending.


### 2026-10-06 — native report binding physically verified; original source continuation fixture

Physical run63033 PASSED: 1 passed,13 deselected in237.03s. Native qualification now binds each actual landmark producer to its exact deployment-selected qualification reports; changed landmark report identity refuses, retained parser report remains mandatory despite zero manual-case parser producers. Original combined qualification/reference TX and all previous policy/person/selection/tenant checks pass.

Added actual source pixel/ICC preparation to native fixture and composeNativeSourceContinuation. Scheduler TX requires original source/native evidence success; changed original WorkingPixels object version and missing later person evidence must refuse with complete rollback. No grants/guards/migrations altered. Native source composition remains pending physical verification, and target/full transfer/public activation/native skin producer/V20–V32 requirements remain incomplete.


### 2026-10-06 — native source continuation preparation

Added prepareNativeSourceContinuation: original parent authorization/read, actual pinned transfer reference source pixel/ICC preparation, original native producer/report qualification, independent person/selection protection, strict same-parent/full-policy composition, original source metadata recheck after dependency IO and retained face report metadata recheck. It owns person Expected before IO, returns zero source/proof on every error/cancellation and publishes no draft/graph/quota/activation. Existing generic protection origin checks are unchanged. This is reference-side preparation for later complete transfer integration; target evidence remains required.

Physical run22159 remains confirmed live for the preceding source/native TX fixture; it does not verify this new preparer. Go run12964 result pending at write time. Full V20–V32 and public native transfer admission remain incomplete.

Go run12964 PASSED semanticproposal1.245s, v2input/masknpy cached. Added native preparation zero-authority cancellation/foreign-actor/missing-session/project/task preflight checks; run29913 pending at write time.


### 2026-10-06 — native preparer final authority repeat

Strengthened prepareNativeSourceContinuation after its last source document IO: repeat original defects qualification report, all retained landmark/parser producer/report metadata, exact current native canonical result catalog, original parent/policy SHA and complete global policy snapshot. Drift returns zero source/proof before composition. This closes preparation-time authority drift after later source reads; final scheduler TX locks remain mandatory. No object IO spans a TX and no existing guards/ACLs changed. Go run29913 from preceding turn PASSED semanticproposal1.262s. Refined run96808 pending at write time.

Physical run22159 remains confirmed live for original source/native TX fixture and does not invoke this new preparer. Full native preparer physical verification, target transfer integration and V20–V32 remain incomplete.


Physical run22159 FAILED271.67s: preceding native/reference/qualification proofs passed, original source continuation TX returned conflict. Inspection of 00686 source lock confirms mandatory app.tenant_id; the fresh aggregate TX called source lock before native lock initialized that local context. Fixed production aggregate to verify current_user=photo_scheduler and set transaction-local tenant context before source lock. No permission, RLS or source guard is weakened. Go refined preparer run96808 PASSED semanticproposal1.157s, v2input/masknpy cached. Retry required; native preparer physical verification remains pending.

Source context correction Go run59916 PASSED semanticproposal2.803s, v2input/masknpy cached. Physical retry9592 is confirmed live. Earlier integration compile39025 PASSED0.656s after adding the missing test canonical import.


### 2026-10-06 — native/source continuation context fix physically verified

Physical run9592 PASSED: 1 passed,13 deselected in412.90s. Original native/source pixel/ICC continuation scheduler TX passes after local tenant context initialization; changed WorkingPixels version and missing later person evidence refuse with full rollback. Previous native qualification/report binding, policy revocation/recovery, selection/person and real tenant isolation checks pass.

Added actual prepareNativeSourceContinuation invocation on the same original native parent and deployed report/person/ICC configuration. Its original source pins,parent,policy and dimensions/gamma must equal independently prepared evidence. Prepared proof must pass TX; removed person evidence must refuse/rollback. New physical preparer verification is pending. Full target transfer/native skin/public graph admission and V20–V32 remain incomplete.


### 2026-10-06 — native transfer reference compiler documents

Added readNativeTransferReferenceDocuments and private owned nativeTransferReferenceDocuments. It requires complete private source/native evidence and exact principal/project/bucket/reference/selected-mask binding, repeats original parent authorization before object IO, reserves bounded raw-document metadata budget before document IO, reads original WorkingImage/user MaskRef/registered native MaskRef using exact SHA/version pins, validates deployed ICC and both selected/protected pixel refs, and repeats source/registered protection/selection root+pixels metadata and global policy after IO. Generic protection origin checks remain unchanged. Caller must still lock complete native/source proof and qualify/lock all targets before transfer admission. No public graph activation or draft mutation.

Initial Go run81957 PASSED semanticproposal1.139s, v2input/masknpy cached. Refined run8765 pending at write time. Physical run76558 remains confirmed live for native preparer fixture; it does not exercise this new document reader. Physical reader/budget/version-refusal verification and transfer integration remain pending. Full V20–V32 remains incomplete.

Refined Go run8765 PASSED: semanticproposal/v2input/masknpy cached. Native transfer document reader physical verification remains pending.


### 2026-10-06 — complete native compiler document budget

Native transfer reader now uses nativeTransferDocumentBudget to reserve all three bounded raw document lengths before document IO. Exactly three mandatory documents, each2 bytes..8MiB, and a positive JS-safe total budget are required. Failure returns zero reserved bytes; oversized/int64 overflow lengths are refused before int conversion. Unit checks cover exact24MiB acceptance, one-byte-short refusal, invalid budget/length ranges and partial/extra sets.

Run4830 pending at write time. Physical run76558 remains confirmed live for complete native source preparer, not this reader/budget extension. Reader physical byte/version/budget tests and transfer integration remain outstanding; V20–V32 remains incomplete.

Go run4830 PASSED: semanticproposal2.930s, v2input/masknpy cached. Physical native transfer reader verification remains pending.


### 2026-10-06 — native transfer document reader qualification preflight

Before any document IO, readNativeTransferReferenceDocuments now repeats the complete global policy snapshot against original native authority and rechecks original defects plus retained landmark/detector/parser qualification metadata. After document IO it repeats those report checks before final full policy equality. Revoked/changed policy or selected report authority cannot silently select a fallback. Final native/source/person/selection TX locking remains mandatory.

Go run75166 pending at write time. Physical76558 remains confirmed live for the preceding preparer fixture and does not exercise document reader. Reader physical identity/version/budget/refusal checks and public transfer integration remain pending; V20–V32 remains incomplete.

Go run75166 PASSED semanticproposal/v2input/masknpy cached. Physical reader verification remains pending; preparer run76558 remains live.


### 2026-10-06 — complete native source preparer physically verified

Physical run76558 PASSED: 1 passed,13 deselected in437.37s. Actual native source preparation retains the identical independently verified original source pixel/ICC pins, native parent/full policy and dimensions/gamma. Prepared original source proof passes scheduler TX; missing person evidence refuses/rolls back. All earlier source version, producer/report/policy and tenant isolation cases pass.

Extended native physical fixture with readNativeTransferReferenceDocuments using exact sum of original three object lengths. Actual raw SHA of source/selected/protection documents must match original refs, retained document proof must pass scheduler TX; one-byte-short budget and changed selected MaskRef object version must return zero documents/proof with zero object IO. New reader physical verification remains pending. Transfer target/full graph/public activation/native skin and V20–V32 remain incomplete.


### 2026-10-06 — native transfer input preparation branch

Refactored PrepareTransferInputs through a shared internal preparation pipeline; ordinary callers preserve the nil-native path. Added private prepareNativeTransferInputs and nativeReference evidence carrier in TransferTaskInputs. Native branch requires independently prepared original source documents/proof, same scoped reference/protection and exact configured person Expected/report. It verifies hash-pinned WorkingImage and selected/protected child bindings, retains native source proof, and uses unchanged target pixel/ICC/protection/person, transfer qualification/cost/resource, metadata and inventory checks. Native document/proof metadata is charged before owning raw document copies and batch allocation.

Go run9973 PASSED semanticproposal1.193s, v2input/masknpy cached before metadata refinement. Workflow compiler/admission does not yet consume the native carrier; current person pairing refuses native inputs rather than admitting a graph without evidence. Full native plan/registration/public integration is required next. Physical reader run77812 remains live; it does not qualify native target batch preparation. Full V20–V32 remains incomplete.


### 2026-10-06 — native compiler documents physically verified; plan evidence carrier

Physical run77812 PASSED: 1 passed,13 deselected in246.94s. Actual native WorkingImage/selection/registered protection raw SHA values match original refs; document reader retained proof passes scheduler TX. One-byte-short budget and changed selected object version refuse with zero documents/proof and zero object IO. Previous complete native preparer/qualification/source/policy/person/tenant checks pass.

Added nativeReferenceRequired/private nativeReference to WorkflowPlan and bindNativeTransferPlanReference. Compiler requires native reference evidence for every target, identical owned source/native proof, exact selected/protected refs/children, independent qualified target person evidence, and semantic.transfer Expected matching original full native policy snapshot. It owns producer proof slices and carries target person proofs separately from original native source person proof. Mixed legacy/native batches, stripped native-required state and inconsistent source/qualification/document bindings refuse. Generic protection origin rules remain unchanged.

Go run57046 PASSED semanticproposal1.201s, v2input/masknpy cached before final mixed/stripped guard refinement. Prior preparation metadata run75255 PASSED semanticproposal1.248s, others cached. Native plan scheduler locking/registration, actual native target preparation/plan physical verification and public activation remain incomplete; existing generic protection/person gates still refuse native registration. Full V20–V32 remains incomplete.


Native source-plan locking now requires consistent nativeRequired/pointer state, exact source proof equality at every reference ordinal, and the complete native source/qualification/person/selection/global policy lock in the same caller TX after normal source locks. Missing source evidence cannot use the internal scaffold return. This connects native authority to existing draft/registration source-lock gates; target person/protection registration still requires native-specific composition next. Refined compiler run71257 PASSED semanticproposal1.462s, other packages cached. Source-plan run22901 pending at write time. No native plan physical verification or public activation is claimed.


### 2026-10-06 — native plan target protection/person transaction gates

Added validateNativeTransferPlanScope shared preflight: consistent required/pointer state, exact complete target/source/protection/person cardinalities, original scoped source and native private proof, exact parent request/intent/selection binding, reference root/NPY identities and target person bindings. Source, person and protection gates all repeat this before SQL. Native reference ordinals use complete native/source TX locks; target ordinals retain existing generic protection provenance locks and independently qualified person producer locks. Native reference skips never mark generic target proofs as already locked. No SQL origin predicate or grant widened.

Initial Go run51798 PASSED semanticproposal1.695s, v2input/masknpy cached. Added before-SQL refusals across all three gates for missing private proof, stripped required state, missing native authority and missing target person evidence. Run4756 pending at write time. Prior source-plan run22901 PASSED semanticproposal1.436s; final source-plan refinement40439 PASSED1.486s, others cached. Native target preparation/compiler/all-gate physical verification, input graph SQL registration/replay and public activation remain pending. Full V20–V32 remains incomplete.


### 2026-10-06 — native transfer target preparation/compiler/gate physical fixture

Added actual native transfer target preparation using original semantic.transfer qualification/measured costs, deployed ICC, independently qualified person policy and actual covering legacy target protection. Publishes original owned transfer Params/snapshot, compiles WorkflowPlan from actual pinned native/target documents and requires retained native reference plus one qualified target person proof. One scheduler TX must accept source/protection/person gates together; missing native proof, missing target person and altered native pixel version must refuse with full rollback. No input graph registration, queueing or activation is claimed.

Previous gate unit run4756 PASSED semanticproposal1.355s, v2input/masknpy cached. New full native plan physical fixture compile/run pending. Native target graph registration/replay/public lifecycle and full V20–V32 remain incomplete.


### 2026-10-06 — Native transfer draft and registration continuation

Prior physical run 44566 passed: 1 passed, 13 deselected in 262.14s. It verified original native source preparation, qualification/report bindings, bounded transfer reference documents, real target preparation, plan compilation and composed source/protection/person transaction gates. It did not verify native transfer draft creation or input registration.

Extended the physical native content scenario to create the transfer draft, replay pending admission from a fresh scheduler pool, publish transport, roll back input registration, commit registration, replay registration through the fresh pool and reject a substituted manifest. Integration-tag compilation passed (semanticproposal 0.749s). Physical process 44910 is running; these new assertions are not yet verified. No production migration or deployment. Full V20–V32 remains incomplete.


### 2026-10-06 — Native transfer registration physical result

Process 44910 completed successfully: 1 passed, 13 deselected in 298.92s, exit zero. The required V27_NATIVE_DEFECT_TRANSFER_REGISTRATION_REPLAY_OK marker verifies BeginDraft created the original native transfer plan, pending admission replay used a fresh scheduler pool, transport publication succeeded, registration succeeded in a rollback-only transaction, registration then committed, durable registration replay committed through the fresh pool and a substituted manifest was refused. Earlier native source/qualification/document/target evidence gates also passed in this same scenario.

The rollback-only call verifies successful execution followed by rollback; this new native scenario does not yet independently query persisted row counts after each step. Exact stored input rows, rollback zero-row assertions and unchanged replay counts should be asserted next. Public native draft preparer wiring, activation/events/cancellation, full worker continuation and V20–V32 completion remain outstanding. No production migration/deployment.


### 2026-10-06 — Native transfer persisted input assertions

Added independent owner-observer SQL checks to the physical native transfer scenario. Expected ordinary input entries come from the frozen graph cursor; transport rows use exact published chunk/manifest refs and reserved ordinals. Checks compare every persisted ordinal, artifact ID, SHA, contract and project, reject extra/missing rows, and require the task manifest flag to agree with registration. Before registration and after rollback there must be zero rows/no manifest. After commit, fresh-pool replay and refused manifest substitution the exact original rows must remain. Integration compilation passed (semanticproposal 0.818s). Physical process 39485 is running; these new assertions are unverified until its terminal result. Public native preparer/activation and remaining V20–V32 remain incomplete. No production deployment/migration.


### 2026-10-06 — Native draft preparer shared recovery path

TransferDraftPreparer now delegates legacy Prepare and internal prepareNativeDraft to one prepareDraft implementation. Native preparation consumes the private original native reference document bundle, checks principal/source/project/bucket/selection/root authority and composes the original proof before intent publication, then uses prepareNativeTransferInputs. Params/snapshot compilation, BeginDraft, transport publication and RegisterWorkflowInputs remain shared. Result is a registered blocked draft only, with no activation or public native configuration enabled.

Go semanticproposal/v2input/masknpy passed before the added prepublication native scope gate (semanticproposal 1.219s); the final gate also passed semanticproposal/v2input/masknpy (semanticproposal 1.148s). Physical process 39485 remains live and covers the earlier persisted-input fixture, not this new preparer. Native preparer durable replay requires a new physical assertion after that process terminates. Full V20–V32 remains incomplete.


### 2026-10-06 — Native preparer prepublication person authority

Extracted validateNativeTransferReference as a pure shared native reference gate. Both draft preparation before PublishIntent and native target preparation now require nonempty retained compiler documents, original tenant/actor/session/source/project/bucket/root scope, complete independently composed native source proof, mandatory person protection, canonical person qualification policy equality, exact original qualification report ID/SHA and original selection root. This closes the prior preparer ordering gap where person-report/configuration refusal happened only after intent publication. No object or SQL IO occurs in this gate.

The initial refactor compilation rejected an unused canonical import; removed it. Final Go semanticproposal/v2input/masknpy passed (semanticproposal 1.346s). Physical process 39485 remains live and uses the earlier compiled implementation; it does not verify this refactor or the new preparer. New physical preparer replay remains required after that run terminates. Full V20–V32 remains incomplete; no production activation/migration.


### 2026-10-06 — Persisted native inputs passed; preparer recovery next

Physical process 39485 passed: 1 passed, 13 deselected in 256.27s, exit zero. Native stored-input assertions independently confirmed exact ordinal/artifact/SHA/contract/project rows, zero rows/no manifest before registration and after rollback, and unchanged exact rows after commit/fresh-pool replay/refused manifest. It used code compiled before the shared native reference gate refactor. Added an integrated prepareNativeDraft pending recovery assertion with unchanged original manifest, required native proof mode and exact persisted rows. This new assertion and final refactor require the next physical run. Full V20–V32 remains incomplete.


### 2026-10-06 — Internal native transfer activation transaction

Added sealNativeTransferDraftInTransaction for original unsealed native transfer drafts. It requires native-required mode and retained proof, recompiles the idempotent request/key and checks original actor/run/workflow/hash, runs the full RegisterWorkflowInputsInTransaction evidence/transport locks in the same transaction, invokes the existing generic seal and validates the resulting saved queued ticket. Every error returns an empty ticket and requires caller rollback, including post-seal ticket failures. Existing SQL seal preserves quota/input/operation guards and atomically writes run event/outbox/receipt. No public route or production operation activation is enabled. Completed ticket replay is explicitly not implemented by this helper and remains a separate authenticated recovery requirement.

Physical process55577 still runs the earlier native preparer recovery fixture; it cannot verify this newly added activation helper. Dedicated rollback/commit/event/outbox/replay/cancellation physical coverage remains required before claiming activation complete. Full V20–V32 remains incomplete.


### 2026-10-06 — Native preparer recovery passed

Physical process55577 passed: 1 passed, 13 deselected in275.24s, exit zero. Integrated prepareNativeDraft recovered the original pending run/workflow, preserved the original transport manifest and native-required proof mode, and retained exact stored input rows. It also exercised the shared pure native reference validation refactor. Newly added activation helper passed Go semanticproposal/v2input/masknpy (semanticproposal1.214s) but was not part of that physical compilation. Added physical activation+rollback assertions: valid queued ticket, restored draft/unsealed run, no queued run events and unchanged persisted inputs. Commit/event/outbox/complete replay/cancellation/worker execution remain outstanding. Full V20–V32 remains incomplete.


### 2026-10-06 — Native final gate retains independent transfer authority

Inspection of activation reuse found that native plan scope checked source/person/protection cardinalities but did not explicitly require retained transfer qualification mode/proof. A mutated private plan could therefore remove that proof and fall back to the less strict generic qualification path. validateNativeTransferPlanScope now requires sourcesRequired, transferQualificationRequired and a nonnil transfer proof. It repeats semantic.transfer expected-policy equality against the original native global policy snapshot before SQL. All source/protection/person gates share this check. Added zero-SQL negative cases for missing qualification and stripped source-required mode. Go semanticproposal/v2input/masknpy passed (semanticproposal1.309s).

Physical process35722 remains live and compiled before this strengthening; it verifies the prior activation rollback fixture only. New checks require another physical native plan/activation run after it terminates. Full V20–V32 remains incomplete. No new database privileges, migration or production activation.


### 2026-10-06 — Native activation refusal diagnosed

Physical process35722 failed in276.35s at original activation (ErrConflict); native preparation, exact input persistence and integrated preparer recovery passed. Inspection of the established operation-registry acceptance confirms semantic.transfer/v1 intentionally has v2_enabled=false. Core seal correctly refuses disabled operations. Fixture now first requires that disabled operation refusal/empty ticket, then temporarily enables only semantic.transfer in its disposable owner database for positive activation rollback and defers restoring disabled state. No production registry/default/permission/guard was changed. This correction and strengthened native qualification gates require rerun. Full V20–V32 remains incomplete.


### 2026-10-06 — Native activation rollback passed; durable commit next

Physical process54695 passed: 1 passed, 13 deselected in303.43s, exit zero. It verifies intentionally disabled semantic.transfer refuses with empty ticket, disposable owner capability enablement permits native activation, and rollback restores draft/unsealed state, zero queued run events and unchanged input rows. Strengthened native source-required/transfer-qualification/policy scope gates passed in this same physical compilation. Production registry remains disabled.

Added durable commit coverage: same queued ticket, persisted queued/sealed run, ready original task, exactly one queued run event and outbox and unchanged input rows. These new assertions are pending. Authenticated completed replay, cancellation, actual transfer GPU execution/review and remaining V20–V32 remain incomplete.


### 2026-10-06 — Shared transaction authority for completed recovery

Extracted lockWorkflowAdmissionEvidence from RegisterWorkflowInputsInTransaction with unchanged lock order: source, inventory, published intent, target snapshot, transfer qualification, protection, person and source recheck. Registration still validates request/principal/session/plan/transport before invoking this chain, then requires exact missing/replayed registered inputs. This extraction prepares authenticated completed recovery to use the same live authority checks instead of treating saved ticket existence as authorization. Completed recovery itself is not implemented yet; sealed-run closure equality, receipt/request/shell binding and fresh-pool nonduplicating replay remain required.

Physical process75061 remains live and compiled before this extraction; it tests native activation durable commit/event/outbox, not the refactor. Go checks for the extraction are reported separately. No SQL migration or production activation. Full V20–V32 remains incomplete.

Shared admission lock extraction Go verification passed: semanticproposal1.133s, v2input/masknpy cached. Physical75061 remains running.


### 2026-10-06 — Durable native activation passed; completed replay implementation

Physical75061 passed: 1 passed, 13 deselected in255.14s, exit zero. Original native activation committed a queued/sealed run, ready task, exactly one queued event and outbox, unchanged exact inputs and identical queued ticket after rollback/retry. Production operation remains disabled.

Added forward-only00734 scheduler-only sealed input-state verifier retaining exact Params/digest/profile/row/transport/backend-document/closure checks and binding original workflow/config. It never registers or activates. Added replayNativeTransferInTransaction: checks original request/key/plan/session, binds transport, repeats full live authority locks, checks each exact sealed closure and returns the original saved queued ticket without seal/event writes. It retains strict original-session evidence; fresh user session continuation is not added. Go semanticproposal/v2input/masknpy passed1.349s. Added fresh-pool positive repeated completed replay and foreign-session/changed-manifest refusals with unchanged inputs/event/outbox counts. Physical verification of00734/refactor/replay pending. Full V20–V32 remains incomplete.


### 2026-10-06 — Native activation transaction recovery wrapper

Added internal activateNativeTransfer around original seal and authenticated completed replay. It validates frozen request/plan/native scope, holds the full admission authority chain before reading receipt, selects completed replay only when an original receipt exists, and otherwise seals the original draft. Same-tenant authority locking prevents receipt observation racing another original admission. It contains no object publication or public route enablement. nativeTicketWithRetry resets results per attempt and returns zero on every commit error; only server-confirmed aborted transactions are retried. Unknown commit is recovered by a subsequent explicit durable invocation, not by blindly retrying a seal.

Added meaningful unit tests: unknown commit returns no queued ticket and causes one attempt; confirmed aborted commit rechecks work and returns only the final committed ticket. Physical54254 remains live and compiled before this wrapper; it covers00734/low-level completed replay only. Wrapper physical verification and public integration remain pending. Full V20–V32 remains incomplete.


### 2026-10-06 — Native activation preflight completion

activateNativeTransfer now rejects mismatched tenant/actor/session and invalid frozen transport before opening a transaction; original native scope/qualification requirements also run before SQL. Go verification reported separately. Physical54254 remains live and compiled before this wrapper, testing low-level completed replay/00734 only. No physical wrapper success is claimed. Full V20–V32 remains incomplete.


### 2026-10-06 — Native completed replay physically passed

Physical54254 passed: 1 passed, 13 deselected in313.99s, exit zero. Forward-only00734 applied successfully in disposable PostgreSQL. Low-level authenticated completed replay from a fresh pool returned the identical original ticket twice, refused changed manifest and foreign session, retained exact input rows and left exactly one queued event/outbox. Shared admission lock extraction was exercised. Production operation remains disabled.

Added physical activateNativeTransfer wrapper recovery with repeated original calls and changed-manifest/foreign-session refusals, unchanged inputs and exactly one event/outbox. New wrapper assertions remain pending. Unknown commit behavior has unit evidence only; concurrent original admission, fresh activation through wrapper, cancellation, actual GPU transfer/review and remaining V20–V32 remain outstanding.

Wrapper fixture now uses activateNativeTransfer for the original commit as well as repeated completed recovery; low-level activation rollback remains independently covered. Integration compilation passed0.573s. Physical process79818 is running; original/replay wrapper checks remain unverified until completion. Native activation preflight Go checks passed1.339s in the preceding turn.


### 2026-10-06 — Native activation wrapper passed; cancellation acceptance next

Physical79818 passed: 1 passed, 13 deselected in316.22s, exit zero. Original commit used activateNativeTransfer; repeated wrapper recovery from fresh pool preserved the original ticket/input rows and one queued event/outbox, rejecting foreign session/manifest. Unknown commit has unit-only evidence; no concurrent original admission proof yet.

Added actual v2workflow.Service Get/Cancel current-revision and idempotent cancellation replay, production cancel_idle_task/finalize_v2_run, cancelled run/task and released zero-byte run quota checks. Historical native activation recovery must return the original saved ticket without reviving cancellation/quota or duplicating queued event/outbox. These new cancellation assertions remain unverified. Full V20–V32 and actual GPU transfer/review remain incomplete. Production registry remains disabled.


### 2026-10-06 — Public V27 proposal integration audit

Current-source inspection: httpapi/semantic_propose.go defines SemanticPropose.Propose and exposes POST /api/v1/tenants/{t}/semantic-intents, but repository search finds no concrete Propose implementation beyond this interface/call; server.SemanticPropose remains an injectable dependency. The native prepare/activation/replay helpers are private components and are not a public service. Existing native physical fixtures start from independently seeded legacy/native producer context; they do not establish first-request producer bootstrap.

Required next integration includes deployment-owned tenant qualification selection, initial native producer preparation/admission/recovery from a new authorized intent, native root/source continuation reconstruction, target transfer preparation, durable activation/replay, concrete Propose response mapping and startup installation. Caller report/protection/policy claims cannot stand in for independent authority. CPU fixture evidence is not real-model scientific/RTX3060 qualification. Physical20409 remains live and tests cancellation/replay, not this public integration. Full V20–V32 remains incomplete.


### 2026-10-06 — Native cancellation passed; reconstructed coordinator next

Physical20409 passed: 1 passed, 13 deselected in290.34s, exit zero. Actual v2workflow.Service cancellation used current revision and stable idempotent response; production cancel_idle_task/finalize_v2_run settled cancelled task/run and released zero-byte run quota. Historical native wrapper replay returned the original acceptance ticket without reviving cancelled state/quota or duplicating queued event/outbox; exact inputs remained unchanged.

Added private prepareAndActivateNativeTransfer coordinator reconstructing original source/native/report/person authority and bounded reference documents before preparing and activating/replaying the frozen transfer. PreparedTransferDraft retains original published intent privately. prepareDraft handles completed BeginDraft outcome by reconstructing transport without attempting registration of a sealed run; activation replay verifies exact sealed closure before returning any ticket. No public Propose or first-producer bootstrap is enabled. Initial Go implementation passed1.158s; additional dependency/reservation/root preflight checked separately. Added physical reconstruction before and after cancellation; these new checks are pending. Full V20–V32 remains incomplete.

Coordinator final dependency/reservation/root preflight Go verification passed: semanticproposal1.247s, v2input/masknpy cached. Integration fixture compilation passed0.586s. Physical80669 remains live; it rebuilds native source/report/protection evidence from original context and reconstructs the completed transfer plan before and after actual cancellation. This is not first-native-producer bootstrap or public Propose installation, and its runtime result is not yet known.


### 2026-10-06 — Reconstructed cancelled replay refusal and ready-only fix

Physical80669 failed in336.95s. Reconstructed coordinator replay before cancellation passed, as did native admission/replay/actual cancellation finalization. Reconstructing after cancellation failed with artifactjson.ErrStale (document reservation stale): PublishWorkflowTasks called the normal transport reservation path, whose cancellation guard correctly refuses cancelled runs. Direct previously retained-plan replay had passed; reconstructed publication was the gap.

Added forward-only00735 scheduler-only read_v27_native_ready_transport. It checks live original actor/session/tenant/project ACL, sealed original semantic workflow, ready verified nonexpired immutable artifact/backend-document metadata, exact SHA/bytes/bucket and registered transport input link. No reservation/quota/upload/state write. nativeReadyTransportPublisher verifies deterministic object key and exact generated-body SHA/size and returns only existing ready refs. prepareDraft selects it only for completed native outcome. Existing reservation cancellation guards remain unchanged. Initial compilation caught Ref.ID/Object bucket type mismatches; corrected them and artifact SQL column names against current schema. Final Go semanticproposal/v2input/masknpy passed1.233s. New migration/runtime correctness remains pending; full V20–V32 incomplete.


### 2026-10-06 — Scoped ready transport errors and proposal HTTP mapping

Ready-only transport publisher maps absent authorized ready row to v2input.ErrExpired and bounds object version length to1024; it never falls back to publication. Go semanticproposal/v2input/masknpy passed1.286s. Added proposal-specific HTTP error translation for declared v2input invalid/conflict/expired, artifactjson conflict/stale/quota and unqualified-model errors:422/409/410/409/503 respectively. Unknown infrastructure failures retain existing handling, with wrapped-error tests. The proposal route uses this classifier; no concrete Propose service or startup installation is added.

Physical79871 remains live and compiled before these Go error changes; it verifies00735 read-only ready transport and reconstructed cancelled coordinator. New error behavior has separate Go verification only. Full V20–V32 remains incomplete.


### 2026-10-06 — Ready transport session JSON path corrected

Physical79871 failed in453.09s at reconstructed coordinator replay before cancellation: ready lookup returned no row (draft-expired classification). Earlier native admission, exact inputs, completed replay and wrapper all passed. Inspection of actual v2input.SemanticDraftConfig confirms the JSON field is v27_semantic, not semantic. Corrected00735 original-session predicate to config_snapshot->v27_semantic->auth_session_id. All original actor/project/ready artifact/registered transport guards remain unchanged; no privilege broadening. The database was disposable and no production migration was applied. Corrected lookup and reconstructed cancelled replay require a fresh physical run. New Go expiry/version bounds and HTTP error classification also require their respective evidence; prior Go httpapi0.670s/semanticproposal1.286s passed. Full V20–V32 remains incomplete.


### 2026-10-06 — Reconstructed cancelled coordinator passed; scope denials next

Physical13546 passed: 1 passed, 13 deselected in358.86s, exit zero. Corrected00735 ready-only transport lookup applied successfully and reconstructed coordinator replay succeeded before and after actual cancellation. Reconstructed native proofs, exact original transport/input closure and saved ticket remain stable; cancelled run/task, released zero-byte run quota and exactly one queued event/outbox remain intact. Go scoped ready error changes were included; HTTP proposal classifier retains separate Go evidence.

Added direct ready lookup positive/repeat and tenant/actor/session/task/purpose/SHA/size/bucket refusals, unchanged stored inputs and scheduler/API EXECUTE ACL assertions. These new scope assertions remain pending. Public Propose/initial native bootstrap, concurrent original admission, real GPU scientific qualification/transfer/review and remaining V20–V32 remain incomplete. Production registry disabled.


### 2026-10-06 — Ready transport scope assertions compiled; physical verification running

Integration-tag compile of semanticproposal passed (session75488, 0.783s, no tests to run). Physical pytest session44020 started for manual-lock-go-disjoint-external with the new ready-only transport scope and role ACL assertions. Its result is pending; the previous physical13546 pass does not cover these additions. No production migration or deployment performed. The public Propose service and first native producer bootstrap remain required V27 work.


### 2026-10-06 — Ready transport scope verified; preparation activation retry wired

Physical session44020 passed: 1 passed, 13 deselected in325.48s. It verifies the original/native transfer reconstruction before and after cancellation plus ready transport wrong-scope, purpose, SHA, size and bucket refusals and scheduler/API EXECUTE ACL assertions. Random foreign UUID refusals are not independent real-tenant positive evidence.

After that terminal run, sealRegisteredDefectPreparation was changed to use nativeTicketWithRetry, repeating the full SQL admission callback only for confirmed PostgreSQL aborted transactions (40001/40P01). Unknown commit outcomes still yield no ticket and no blind retry. Added a unit case for aborted work followed by authority refusal, requiring a zero ticket. Package tests passed before the additional unit case (1.382s); final package result and the new physical retry-wiring run are tracked separately. No production migrations/deployment performed. Public Propose and first-producer orchestration remain incomplete.

Final semanticproposal package tests, including aborted-work followed by refusal, passed in1.500s (session33982). Physical session88324 is live and pending for the changed preparation activation; session44020 does not verify that later code edit.


### 2026-10-06 — Pending native producer admission coordinator

Added private admitPendingDefectPreparation: independent live person admission preparation precedes draft creation and transport publication; existing persistence publishes outside SQL; final activation uses the confirmed-abort retry helper and returns a ticket only after commit. This accepts an internally compiled original/pending plan. Completed admission reconstruction is deliberately a separate retained-closure path, not implemented by re-reserving completed transport. It does not yet select deployment qualifications, compile the first plan or install public Propose.

New unit guards reject absent source/person authority before SQL or publication, and cancelled context before dependencies. Physical session88324 remains pending for the prior retry-wiring revision; its existing fixture does not invoke this new coordinator. Positive physical coordinator coverage must be added after that frozen scenario finishes.


### 2026-10-06 — Preparation activation retry physically verified

Physical session88324 passed: 1 passed, 13 deselected in400.11s. This proves the changed sealRegisteredDefectPreparation retry wiring preserves the original committed admission and durable replay in the CPU/mTLS fixture. It does not inject a real deadlock/unknown commit, nor prove scientific GPU qualification.

After terminal completion the original native preparation commit in the fixture was switched to admitPendingDefectPreparation, using the actual pending draft, transport publisher and independently retained person coverage. The Python harness now requires V27_DEFECT_NATIVE_PENDING_COORDINATOR_OK. This later coordinator change remains pending physical verification. Unit preflight guards previously passed in1.303s (session22963).

Integration-tag compile for the pending coordinator fixture passed in0.643s (session78464). Physical session97676 started and is pending. No production migration or deployment performed. First-plan compilation and trusted qualification selection, completed coordinator reconstruction, public Propose installation and actual GPU quality acceptance remain required.


### 2026-10-06 — Completed native preparation recovery audit

Physical session97676 is confirmed live. Inspection found that sealDefectPreparationInTransaction skips lockRegisteredDefectPersonAdmission when v2_admission_ticket already exists; SQL00722 sealed branch checks live editor, saved config/policy hashes and receipt but returns before full lock_semantic_defect_admission_evidence. Therefore retained completed preparation replay is not yet proof of repeated original source/person/full policy and exact immutable input closure validation. Pending coordinator scope must not be represented as complete first-producer restart recovery. Next implementation requires a scheduler-only read-only completed closure gate plus all original live authority checks, refusing mutated transport and revoked/expired proofs without writes. Existing draft gates call beginDefectPreparationDraftInTransaction and cannot simply be reused unchanged for sealed runs. No changes to the running fixture or migrations made in this audit.


### 2026-10-06 — Pending producer coordinator passed; completed replay gate added

Physical session97676 passed: 1 passed, 13 deselected in378.80s. It invoked admitPendingDefectPreparation on the actual pending native draft, repeated ready input publication/registration, committed activation and recovered the same receipt through a fresh pool.

Added forward-only00736: private completed registration checks compare the exact immutable rows/transport/profile for sealed defects.propose graphs; private completed evidence composes original anchor, full policy expectations, closure digest, landmark/mask inventories, source/producers/content/person and skin lineage. The public scheduler seal wrapper serializes through tenant/usage/run locks before selecting draft/completed branch, and on completed replay rechecks full evidence before the existing durable receipt return. Its renamed previous seal and new component functions are private. No reservation, activation or event writes are added to completed replay.

Fixture now refuses a changed manifest and an expired original person protection on completed replay, then restores protection and requires the same ticket. Compilation and physical verification for this later migration are pending separately. No production migration or deployment performed; public Propose and first-plan/server qualification orchestration remain incomplete.

Integration-tag compile passed in0.635s (session63559). Physical session90218 started for migration00736 and completed replay negative cases; result pending. Previous session97676 does not verify00736.


### 2026-10-06 — Completed native preparation replay passed; read-only and ACL assertions added

Physical session90218 passed: 1 passed, 13 deselected in344.69s. Migration00736 applies and preserves pending coordinator commit and completed original replay. Changed manifest and expired original person protection return no ticket; restoring the protection permits the same saved ticket. This is not yet public Propose/GPU qualification or unknown-commit fault evidence.

After terminal completion added actual SQL-role invocation denials for private completed base/evidence and renamed pre-completed seal, including scheduler. Added exact owner-observed before/after graph/task/input/event/outbox/receipt comparison across refused and restored successful replay, with required V27_DEFECT_NATIVE_COMPLETED_READ_ONLY_OK marker. These later assertions await compile and physical verification separately.

Integration-tag compile for the additional completed read-only/ACL assertions passed in0.658s (session18969). Physical session9100 is live and pending. Next runtime work: reconstruct completed defect preparation transport through authorized ready-only immutable lookup, then integrate pending/completed coordinator recovery; public first-plan compilation/trusted policy selection and Propose installation remain incomplete.


### 2026-10-06 — Completed producer recovery coordinator scaffold

Added private recoverCompletedDefectPreparation: reselects independent person admission authority, regenerates frozen transport bytes using nativeReadyTransportPublisher, then invokes the full completed seal gate. It never calls pending admission, registration or a writable publisher. It requires the subsequent ready-lookup migration to authorize the exact defects.propose stage and original v27_defect_prepare session; existing00735 authorizes semantic.transfer only, so this new recovery path is not yet usable/physically verified. That migration must be added after physical session9100 finishes; the active fixture/migrations remain frozen.

This reconstructs transport from a retained private compiler plan, not a complete public raw-intent restart bootstrap. First-plan reconstruction, trusted deployment qualification selection and Propose installation remain required.


### 2026-10-06 — Completed replay read-only/ACL passed; transport recovery enabled

Physical session9100 passed: 1 passed, 13 deselected in373.36s. Actual SQL-role calls, including scheduler, cannot execute private completed base/evidence or renamed original seal. Before/after graph/task/input/event/outbox/receipt observations remain identical across refused and restored replay.

After terminal completion added forward-only00737 extending ready-only transport lookup to exact defects.propose/1 CPU scope with original v27_defect_prepare session, task/Params/input digest and deterministic child-run binding. Existing semantic.transfer path and all common ready artifact/project/editor/document-input restrictions remain. New fixture reconstructs the completed producer's transport twice via recoverCompletedDefectPreparation and requires original ticket; the existing state observer includes these calls in the unchanged-state comparison. This later migration/recovery scope is not yet physically verified. No production migration/deployment performed.

Integration-tag compile passed in0.551s (session32876). Physical session78042 started for00737 and completed transport reconstruction; result pending. Session9100 does not verify this later migration/recovery change. Remaining full V27 work includes trusted first-plan reconstruction/qualification selection, public Propose installation, actual GPU transfer/adoption/review and quality qualification.


### 2026-10-06 — Original qualified Params reconstruction

Added private prepareOriginalDefectParams for first-plan assembly: validates original private policy/origin scope, rechecks origin before/after bounded document read, reconstructs faces/diameter/permanent and all additional protections through existing manual content preparation, and requires exact original Params text/SHA and qualified source proof. This prevents first-plan construction from silently dropping protection fields. It is not public authority selection, first-plan publication or queue admission.

Physical session78042 remains live for00737 completed transport recovery; its frozen scenario does not invoke the new Params reconstruction helper. Positive integration coverage must be wired after that process finishes. No active fixture or migration edits performed while it runs.


### 2026-10-06 — First producer plan assembly from qualified origin

Added private prepareFirstDefectPlan: owns snapshot/inventory buffers, prechecks configured dependencies and canonical parent/intent/target identity, reconstructs exact original qualified Params, publishes a deterministic accepted-version Params document using UUIDv5(parent run,"semantic.defect.params:reference"), reads its original proof and compiles the recoverable blocked stage through prepareDefectPreparationDraft. It does not perform queue admission or select qualification authority.

Physical session78042 remains pending for00737 completed transport reconstruction. The running fixture/migrations are unchanged; it does not call this later first-plan builder. Integration wiring must move its deterministic Params document ID to parent command scope and exercise the new builder after terminal completion. Public Propose orchestration/installation and original trusted qualification selector are still incomplete.


### 2026-10-06 — Completed producer transport recovery passed; first-plan builder wired

Physical session78042 passed: 1 passed, 13 deselected in367.38s. Migration00737 supports exact native preparation ready transport scope; two recoverCompletedDefectPreparation calls reconstruct refs and return the original ticket. State observer proves graph/tasks/input registry/events/outbox/receipt unchanged across recovery and denial cases. This recovery still uses a retained private compiler plan and original session; public raw-intent bootstrap is not complete.

After terminal completion switched the fixture to prepareOriginalDefectParams and prepareFirstDefectPlan. Params document UUIDv5 now derives from parent command run, matching the new builder. Two independent builder calls must produce identical canonical draft shells, original Params ref and same document ID. Added required V27_DEFECT_FIRST_PLAN_RECONSTRUCTION_OK marker. These later edits await compilation/physical verification. No production migration or deployment performed.

Integration-tag compile passed in0.673s (session79763). Physical session6198 started for first-plan/Params reconstruction; result pending. Session78042 does not cover these later edits. Server authority selection and asynchronous parent proposal continuation remain required; internal native child ticket must not be substituted for a completed public parent proposal workflow.


### 2026-10-06 — First native stage pending/completed orchestration

Added private firstDefectPreparation and TransferDraftPreparer.prepareAndAdmitFirstDefectStage: rebuilds the first plan from selected qualified execution, reselects independent person authority, observes scoped durable receipt, and selects original/pending writable publication versus completed ready-only transport recovery. Receipt existence is only branch selection; final gates repeat full evidence checks. No SQL transaction spans document IO. It returns only the internal child-stage acceptance, not a public parent proposal ticket.

semanticproposal package tests passed in1.223s (session85425). Physical session6198 remains live for the preceding first-plan fixture revision and does not invoke this new stage orchestrator. Positive pending/completed integration wiring must follow its terminal result. Public parent asynchronous continuation and deployment authority selection remain incomplete.


### 2026-10-06 — First plan reconstruction passed; stage orchestrator wired

Physical session6198 passed: 1 passed, 13 deselected in352.89s. prepareOriginalDefectParams preserves original exact Params/protections and prepareFirstDefectPlan reconstructs identical canonical draft shells and deterministic parent-scoped Params document/ref twice; pending admission, completed recovery and transfer continuation remain successful.

After terminal completion switched pending native commit and two fresh-pool completed recovery calls to prepareAndAdmitFirstDefectStage. It now reconstructs the first plan on each call and selects the scoped durable receipt branch; fixture requires V27_DEFECT_FIRST_STAGE_ORCHESTRATOR_OK. Existing graph/input/event/outbox/receipt observer spans completed calls. Compilation and physical verification of this later orchestrator wiring are pending separately. It is still an internal child-stage path, not public parent Propose.

Integration-tag compile for first-stage orchestrator wiring passed in0.548s (session12219). Physical session84984 started; result pending. Session6198 does not verify these later orchestrator calls. No production migration/deployment performed.


### 2026-10-06 — Policy-selected first producer qualification profiles

Added private prepareSelectedDefectExecution: derives all defects.propose/face.landmarks/face.detect/face.parse Expected profiles from the full current DBA policy snapshot, requires the same original detection report for face/parser families, invokes existing full scientific-report/source/producer verification, and compares the original policy before/after preparation. Deployment report refs remain private server pins; no client-supplied Expected or newest/backend fallback enters this method. Automatic discovery of those deployment refs and source descriptors remains separate required work.

Physical session84984 remains live for first-stage orchestrator wiring and does not call this later selector; positive selector fixture wiring awaits its terminal result. No running fixture/migration edits performed. Public parent proposal continuation/installation remains incomplete.


### 2026-10-06 — First-stage orchestrator passed; policy selector wired

Physical session84984 passed: 1 passed, 13 deselected in364.39s. prepareAndAdmitFirstDefectStage rebuilds the original first plan, commits pending admission and reconstructs completed transport/ticket twice with a fresh scheduler pool, preserving observed graph/input/events/receipt state.

After terminal completion switched first execution preparation to prepareSelectedDefectExecution. Expected profiles now come from the full authoritative DBA policy rather than fixture-supplied Expected arguments. Fixture rejects changed parser detection report before returning any retained policy proof, then requires V27_DEFECT_SERVER_POLICY_SELECTION_OK on valid original refs. This is policy profile selection with private report refs; automatic scoped report discovery remains separate. Compilation and physical result for this later fixture edit are pending. No production migration/deployment performed.

Integration-tag compile passed in0.893s (session4513). Physical session95748 started for server policy selector; result pending. Session84984 does not verify the later selector wiring.


### 2026-10-06 — Face qualification report catalog discovery

Added private discoverFaceQualificationReport over the existing API-only semantic_proposal_face_qualification_object. It selects at most two distinct refs for a server-selected face profile under live tenant/project editor scope and requires exactly one valid ref; absent or multiple distinct reports yield no candidate. Existing SQL enforces trusted import/policy/audit/ready version checks. This is catalog selection only: original report bytes and scientific qualification remain required in preparation. No newest-model/report fallback is used. Defect report discovery still needs its own equivalent reader; no claim of full deployment-authority discovery.

Physical session95748 remains pending for the prior policy-selector fixture and does not invoke this later discovery helper. Integration positive/ambiguity/stale-scope coverage remains required after terminal completion. No active fixture/migration edits performed.


### 2026-10-06 — Policy-bound face report discovery preparation

Added prepareDiscoveredFaceDefectExecution: reads the original full DBA policy, discovers unique scoped refs for face.landmarks/detect/parse using the existing trusted registry reader, supplies one shared detection ref to both producer families, runs full prepareSelectedDefectExecution and requires the same policy before/after. Candidate discovery alone never returns execution authority. The defect report remains a private deployment pin; its equivalent discovery reader and public parent proposal lifecycle remain outstanding.

Physical session95748 remains live for the prior selector fixture and does not invoke this later discovered preparation method. Positive discovery and ambiguity/stale-scope integration checks must follow its terminal result. No running fixture/migration edits performed.


### 2026-10-06 — Policy selector passed; full report discovery wired

Physical session95748 passed: 1 passed, 13 deselected in358.89s. The first-stage pipeline uses Expected derived from the original DBA policy and rejects substituted detection report before returning proof; prior plan/admission/recovery/transfer invariants remain green.

After terminal completion added forward-only00738 API-only semantic_proposal_defect_qualification_candidates, using the existing complete scoped trusted-import/policy/audit/ready-object predicates and at most two distinct candidates. Shared private discovery now handles all four required operations. prepareDiscoveredDefectExecution obtains every report ref from the registry, then runs original full scientific/content/producer verification with policy stability checks. The fixture now invokes this path and verifies each selected report becomes unavailable on expiry and returns identically after restore; requires V27_DEFECT_REPORT_DISCOVERY_OK. Duplicate-candidate and explicit foreign-scope discovery coverage still remain to be added; no production or real-model quality certification inferred from synthetic fixture.

Integration-tag compilation and physical verification of these later edits are tracked separately. No production migration/deployment performed. Public parent proposal orchestration/installation remains incomplete.

Integration-tag compile passed in0.671s (session49892). Physical session44400 started for00738 full report discovery and expiry/restore assertions; result pending. Previous session95748 does not verify these later changes.


### 2026-10-06 — Full report discovery passed; descriptor ownership corrected

Physical session44400 passed: 1 passed, 13 deselected in373.32s. API-only00738 applies; prepareDiscoveredDefectExecution selects all four required report refs, performs the full existing qualification/source/producer checks, and each expired report is refused then recovered identically after restore. First-plan/stage/recovery and transfer invariants remain green. No real-model scientific certification inferred from synthetic qualification reports.

After terminal completion added snapshotDefectDescriptors and invoke it in selected/discovered execution wrappers before first policy/catalog SQL IO. It deep-copies dependency arrays and nested LayerRef fields through bounded canonical JSON and repeats identity validation. Unit coverage mutates caller inputs/manual roots/nodes/nested binding after snapshot, proving no alias reaches owned descriptors. Added actual SQL-role invocation ACL checks for00738 API reader; all non-API execution roles must receive42501. Later unit and physical results tracked separately. Public parent proposal and full server source descriptor discovery remain incomplete.

Unit/package tests including descriptor alias isolation passed in1.475s (session36757). Integration-tag compile and subsequent physical run for the descriptor snapshot/00738 ACL revision are tracked by current tool handles; no earlier physical pass covers this later edit.

Integration-tag compile passed in0.616s (session72079). Physical session3203 started for descriptor snapshot before SQL and00738 role ACL assertions; result pending. No production migration/deployment performed.


### Descriptor snapshot and discovery ACL physical verification

Physical pytest session3203 completed successfully: 1 passed, 13 deselected in 366.04s, exit zero. The manual-lock-go-disjoint-external fixture verifies descriptor snapshot integration and actual SQL role EXECUTE restrictions on 00738 together with its existing report discovery and CPU/native recovery chain. This is not evidence of full public Propose, real GPU/model qualification, concurrent tenant load or complete V20–V32 delivery.

Added private prepareOriginDefectParams to reconstruct exact original Params from scoped catalog provenance before execution-profile assembly. The existing execution-qualified wrapper retains policy checks and exact source-proof equality. Source/manual verification, full protections and byte/SHA equality are preserved. Unit/package verification pending; no new physical acceptance claimed for this refactor.

Origin-first Params refactor package verification: Go session21943 PASSED semanticproposal in 1.493s, including absent-provenance and cancelled-context refusal before SQL. Full physical replay after this refactor remains pending; earlier session3203 tested the previous implementation.


### Origin-derived dependency reconstruction

Added private prepareOriginDefectDependencies: read exact scoped original producer proof; reconstruct canonical original Params through source/manual verification; decode owned declared input/manual-root inventories; require original Params SHA equality and full declared-ref validation; repeat source/manual/origin authority after assembly. No publication or ticket is produced. Optional alpha/geometry dependencies are inherited from the existing BuildDefectProposalTaskInputsFromImage compiler, not guessed from generic JSON refs. Caller-selected origin pins and typed descriptor discovery remain gaps; this new helper is not yet wired into public Propose.

Go package session9186 PASSED semanticproposal in 2.121s and focused diff whitespace check passed. Physical session37660 remains live and tests the preceding original-Params refactor, not this newly added helper. Full V20–V32 implementation remains incomplete.


### Broad backend verification interrupted by disk exhaustion

Go session82782 (go test ./...) failed to compile/link multiple packages with "no space left on device" and was interrupted; terminal exit1 confirmed. Some packages passed, but this run does not prove complete backend correctness and build failures cannot be attributed to source code. Filesystem observation initially showed 1.2GiB available, later 2.9GiB after temporary build cleanup. Sequential retry go test -p1 ./... started as session68632 and remains pending. No unrelated files/cache were deleted. Physical pytest session37660 remains live; no restart or acceptance claim.


### Protection-root catalog discovery

Added discoverProtectionRoot: active editor tenant transaction, exact child ID/SHA, same canonical producer root, closed protection output-role allowlist, existing source/version/protection provenance SQL gate, bounded result cardinality2; no candidate or ambiguity returns zero ref. readDiscoveredProtection verifies original MaskRef/NPY via existing reader, requires exact child match and repeats discovery after IO. This is catalog/content preparation, not scientific/person qualification or admission, and is not yet wired into public Propose. Added physical fixture comparison to the original required-protection root and exact document body; runtime acceptance pending. Underlying query work is not claimed bounded solely by LIMIT2.

Package session39062 PASSED semanticproposal1.737s before the new fixture assertion. Physical session37660 FAILED after615.54s at native policy revocation because its overall150s context expired; preceding native selection/person checks passed. This is not a successful native end-to-end run. Disk exhaustion and broad build overlapped that run; causation remains unproven. Sequential backend session68632 still pending.

Sequential full backend session68632 (go test -p 1 ./...) PASSED, terminal exit0. This verifies ordinary backend package build/tests; external integration tests without fixture environment are skipped. It compiled semanticproposal before the protection discovery fixture assertion, so it does not prove that new physical scenario.


### Original-content typed descriptor assembly and root disambiguation

Added prepareOriginDefectDescriptors and prepareOriginSelectedDefectExecution. Original producer Params/dependencies, source pins (including alpha/geometry), declared landmarks, full face-mask roots and retained manual target/protected-child proofs generate exact typed descriptors; unknown dependency roles and conflicting identities fail rather than falling back to generic Artifact. Additional verified mask roots are accounted in declared file inputs. The new internal path feeds existing server report/policy selection; public origin discovery, parent admission and whole-system delivery remain incomplete. Package38755 passed2.274s before the execution wrapper was added; physical acceptance of descriptor assembly remains pending.

Physical session42748 FAILED41.31s at protection-root discovery: multiple same-producer roots satisfied the existing SQL provenance predicate. That predicate proves producer output membership, not exact MaskRef-to-child content pairing. Discovery now reads up to128 canonical scoped root candidates, bounds total document bytes to4MiB, resolves original MaskRef content and selects exactly one exact child; matching duplicates still conflict. No SQL authority check weakened. The earlier limit2 claim is superseded by this explicit candidate/content bound; underlying database work is not proved bounded. Full physical rerun required.


### Origin-first internal stage assembly

Added prepareAndAdmitOriginDefectStage with private originFirstDefectPreparation. It owns the canonical command, snapshot bytes and parent target inventory before evidence IO, checks command/intent/snapshot identities, reconstructs typed original dependency descriptors and server-selected qualification, then delegates to the existing locked first-stage admission/completed recovery path. Caller no longer supplies a prebuilt execution proof or descriptor list on this internal path. Original producer pins, independent person proof and measured resource policy remain private inputs; automatic original producer discovery/new-source prerequisite scheduling and public parent Propose remain unimplemented. This is an internal child ticket, never public parent acceptance.

Package session5984 PASSED semanticproposal1.394s; focused diff whitespace check passed. Physical session35501 remains live, verifying earlier root-content disambiguation and original Params reconstruction, not the new origin-first stage method. No claim of runtime acceptance of the new stage.


### Root-content discovery physical pass; origin-first stage fixture connected

Physical pytest35501 PASSED: 1 passed,13 deselected in414.87s, terminal exit0. The fixture requires exact discovered required-protection root/pixels/document bytes and existing original Params/native/recovery/SQL authority markers. This verifies root content disambiguation and the preceding original Params refactor, not the newly added descriptor/stage assembly. Previous native timeout did not recur; no timeout bound was enlarged, but its precise cause is not established.

Changed the physical original admission and two completed recovery calls to prepareAndAdmitOriginDefectStage with original producer pins instead of a prebuilt execution proof/descriptor inventory. The same persisted state/ticket assertions remain; added mandatory V27_DEFECT_ORIGIN_FIRST_STAGE_OK marker. Compilation and full physical verification pending. No public parent Propose, new-source producer bootstrap, GPU qualification or complete V20–V32 claim.


### Origin-first stage physical pass and early resource preflight

Physical pytest36654 PASSED: 1 passed,13 deselected in349.65s, terminal exit0. Mandatory origin-first marker proves original admission and two completed recovery calls reconstruct descriptors and server qualification from original producer pins; exact original ticket and unchanged persisted-state assertions remain. Existing native transfer/cancellation/recovery chain also passed. This does not prove public parent Propose, original producer discovery, fresh-source prerequisite scheduling, skin/parser inference or actual GPU/model qualification.

Added early origin-selected execution preflight after that run's publication test was compiled: cancellation; pure8x8/input1 minimum-budget check; server full required-policy selection; original defects qualification/report measured-cost validation; authorized source document dimensions; conservative source-JSON byte lower-bound profile; then full descriptor/qualification assembly and exact initial-vs-final policy snapshot equality. Lower bounds do not replace complete catalog file accounting or aggregate concurrency admission. Package45655 passed1.315s before new early-refusal unit cases; newer unit/package and physical verification pending. Test36654 does not verify the new preflight order.


### Exact-Params original producer discovery

Added private discoverDefectOriginForParamsSHA: server-supplied exact Params SHA, active editor tenant transaction, source/project/version scope and existing canonical completed producer predicate; joins original Params to exact output-role refs and rejects zero/ambiguous result, invalid/doubled ref IDs or digest mismatch. It does not choose newest output, accept client authority, read scientific reports, create prerequisites or grant admission. API tasks SELECT already exists in00010; no new grants/migration. Candidate cardinality is bounded2, but underlying database work is not proved bounded. Existing native result/parent lookup remains separate; this reader supports only producers accepted by the original semantic_proposal_defect_origin predicate.

Go package69438 PASSED semanticproposal1.290s. SQL physical selection, ambiguity/foreign-scope tests and connection to the origin-first stage remain pending. Exact server Params SHA derivation for new sources and first prerequisite scheduling remain unresolved implementation work. Physical session59540 continues testing earlier resource-preflight changes; it does not verify this new reader.


### Params-SHA producer discovery connected to origin-first stage

originFirstDefectPreparation now supports a private server-selected paramsSHA instead of preselected original output pins. Nonempty SHA and explicit origin pins are mutually exclusive and invalid digest fails before IO. Canonical command/snapshot/inventory ownership and identity checks precede discovery; selected provenance then enters complete original descriptor, scientific qualification and first-stage admission/recovery checks. No newest fallback or public client authority is introduced. Deriving the exact server Params SHA for fresh sources, prerequisite scheduling and public parent Propose remain incomplete.

Package1457 PASSED semanticproposal2.194s and focused whitespace check passed. Physical session59540 still runs earlier preflight order with explicit pins; it does not verify the new SHA-discovery branch or its SQL query. Full physical branch verification and ambiguity/foreign-scope cases remain required.


### Early qualification/resource preflight physical pass; SHA discovery fixture

Physical pytest59540 PASSED: 1 passed,13 deselected in377.05s, terminal exit0. This verifies early qualification/resource preflight and exact policy snapshot comparison alongside original admission/two completed recovery and existing native/cancellation gates. It used explicit producer pins, not SHA discovery; no claim of public parent Propose or real GPU/model science.

Switched original stage admission and both completed recovery calls to private server Params SHA with no original output refs. Added exact expected-origin comparison, missing Params SHA refusal with zero pins and changed-source-ID refusal with zero pins; mandatory PARAMS_SHA_DISCOVERY_STAGE marker. The changed source is an invalid binding, not a full second-tenant/accepted-source positive scenario. Ambiguous producer and real foreign tenant discovery tests remain required. Compilation and physical verification pending.


### Params SHA is not producer identity

Physical pytest73047 FAILED: 1 failed,13 deselected in173.08s at exact Params discovery. Fixture intentionally contains two same-Params canonical producers: one preserves required manual protection, the other has valid metadata but deficient output coverage, already rejected by full ReadDefectEvidence. SHA alone cannot identify the authoritative original execution. No newest/lowest-ID fallback added and no content gate weakened.

Replaced SHA-only reader with discoverDefectOriginForProducer, requiring the server-selected original prerequisite/cache task ID plus exact Params SHA. Tenant/project/accepted source/canonical attempt/output gates remain mandatory; original content and qualification still run before admission. Origin-first mode now requires both task and SHA and forbids mixing them with explicit pins. This is not complete automatic cache/prerequisite selection: deriving that durable server task ID for fresh sources remains work. Fixture uses its existing exact original task, checks expected refs, wrong task/SHA/source zero-result failures, and requires EXACT_PRODUCER_DISCOVERY_STAGE marker across initial admission and two recovery calls.

Package50046 PASSED semanticproposal1.372s before the new wrong-task fixture assertion. Physical rerun remains pending. Full public Propose and V20–V32 remain incomplete.


### Exact producer capture groundwork (2026-10-06)

Added private selectDefectOriginProducer and defectOriginSelection. It validates complete original pins and scope before SQL, verifies original provenance, reads the proposal producer under authenticated tenant/editor scope, then requires exact rediscovery of all five original refs by producer task ID and Params SHA. A shared Params SHA cannot select a different execution. The result conveys identity only, not scientific acceptance or admission authority. This helper is not yet wired into immutable draft config or restart recovery: persisting and rechecking the selection under the existing locked gates remains required.

Package test session8024 passed in1.453s before adding refusal tests. Session3758 passed in2.068s with missing-provenance and cancellation refusal tests using an uninitialized pool to prove no SQL entry. Positive database capture remains unverified; the helper was added separately without modifying the active physical fixture or migrations. Physical exact-producer integration session38919 was polled and confirmed live with no new output; no terminal result is claimed. Existing public parent admission, fresh-source prerequisite selection and full V20–V32 completion remain outstanding.


### Persisted original producer identity (2026-10-06)

Physical exact-producer integration session38919 PASSED: 1 passed,13 deselected in380.86s, exit0. Scope includes exact task+Params discovery, wrong-task/SHA/source refusal, initial origin-first admission and two completed recovery calls, and existing native/cancellation gates. It predates the config change below.

prepareDefectPreparationDraft now captures the exact original producer from all retained original pins and stores original_producer {producer_task_id, params_sha256} inside the canonical config snapshot. A second exact capture must match before returning the plan. Existing content/qualification/person/admission gates remain; this field is not yet a standalone trusted restart input. A scoped saved-selection reader and locked binding of persisted selection remain required. Historical drafts without the field need explicit compatibility handling rather than silently changing their config hash. Package session57954 PASSED (see tool result); new physical verification is pending and must not be confused with38919.


### Scoped saved producer locator (2026-10-06)

Added readSavedDefectOrigin and decodeSavedDefectOrigin. The read-only API/editor transaction reads the deterministic child run only for its original creator and currently authorized project. Decoding binds project, original session, parent run, request SHA, source, intent and snapshot; the original_producer object must have exactly task ID and valid Params SHA. The reader checks current intent SHA and performs full catalog provenance rediscovery for that producer. A saved locator does not itself establish content science, person coverage, or locked admission. This helper is not yet connected to stage recovery; a final locked config-to-original-producer binding remains required.

Old snapshots lacking original_producer refuse automatic locator decoding. Rebuilding an old draft with the new config shape would alter its config hash; the explicit-evidence compatibility path needs implementation/verification before claiming historic recovery support. Unit cases exercise exact context, changed project/session/parent/request/source/intent/snapshot and missing legacy selection. Session64339 passed1.282s before these cases; session18451 result is recorded by the current tool output. Physical session25949 remained live when polled, so its result is still pending. Active fixture and migration files were not changed during that run; no physical saved-reader verification claimed.


### Historical immutable config compatibility (2026-10-06)

Adding original_producer changes config_hash. Added preserveDefectConfig in draft construction: read a saved deterministic child run under the original creator and currently authorized project; verify its stored canonical hash; retain it only if byte-equivalent after canonicalization to the complete rebuilt current config or exactly that config with original_producer omitted. All other project/request/source/selection drift refuses. No saved config is mutated, and absence of an existing run creates the new shape normally. Existing locked admission still checks exact config, authority and closure. This compatibility lookup grants no admission authority. Old drafts still cannot supply automatic saved producer discovery without original_producer; explicit original evidence is required.

Unit cases cover exact current and legacy preservation, project/request/source drift, null selection and invalid stored hash. Session62013 passed1.420s before these cases; session20867 final result is in the current tool output. Physical session25949 was confirmed live with no output and predates this compatibility addition; active fixture and migrations remain frozen. Saved-reader integration, final locked producer-to-config binding and physical historic compatibility verification remain required. V20–V32 remain incomplete.


### Saved origin locked binding and automatic stage restart (2026-10-06)

Physical25949 FAILED in253.20s: first-stage phase completed, native result content/transaction lock reached, then native parent association refused with idempotency conflict. Inspection of00727 proved its exact native-config projection rejected the added original_producer field. Added forward-only00739: preserve the exact parent anchor projection while allowing only a validated original_producer object; wrap original and completed admission evidence gates after their existing full authority/content locks, then compare stored original producer task/Params SHA with the exact retained original pins under row locks. Private wrapped functions and selection matcher are not granted to API/scheduler; the existing current admission surface retains its role grant. No content/qualification/person checks removed.

Connected origin-first restart mode: only if explicit origin pins and task/SHA are absent, read the scoped saved locator and rediscover original refs, then run the existing full execution/person checks and locked admission/recovery. Fresh-source missing-selection is an error, not SHA-only discovery. Legacy exact config preservation added before this run remains included. Both completed recovery calls now remove supplied producer ID and Params SHA to exercise the saved path. Package37407 passed1.298s before fixture change;9732 final result is in the current tool output. Physical92541 started with00739 and the no-task/SHA recovery fixture; result pending. Migration syntax/runtime, final identity mutation denials and real historical-config physical recovery are not yet proven; no public parent/GPU/full V20–V32 completion claim.


### Saved locator integrity and refusal preflight (2026-10-07)

readSavedDefectOrigin now validates request/source/canvas/intent/snapshot digests and required source identity before SQL. It reads config_snapshot plus config_hash with a1MiB JSONB text bound, verifies the canonical stored hash before decoding, and then uses the existing exact-producer provenance discovery. decodeSavedDefectOrigin runs canonical JSON validation before typed decoding. This integrity check remains preparatory; scientific qualification, independent person coverage and locked admission still follow.

Added refusal cases for null/array/zero-task/invalid-SHA/unknown-field producer objects and missing source metadata/cancellation before an uninitialized SQL pool. Package77078 passed1.270s after code change before extra cases;64522 final result is in the current tool output. Physical92541 was polled and confirmed live without a new result; it covers00739 and saved-mode recovery, but was started before this integrity preflight. Its result must not be claimed as verification of later code. No fixture/migration edits made while it is active; V20–V32 and public parent admission remain incomplete.


### Saved configuration API authorization correction (2026-10-07)

Correction to prior live note:92541 became terminal at the last poll, FAILED in93.91s.00739 migrated successfully far enough to run the first Go phase, but draft compatibility query directly invoked private v2_editor_authorized under photo_api and received42501 at draft construction. No00739 locked-identity or saved-recovery pass is claimed.

Added forward-only00740 semantic_defect_saved_config: API-only bounded read through SECURITY DEFINER private project/session/editor authorization, current app tenant, exact run/creator/project/kind and native-config scope. Returns existing config and hash only; no writes/admission/model authority and no new grant on v2_editor_authorized. Both compatibility and restart readers now call this dedicated surface. Package64522 passed1.197s with refusal cases;46372 final result is in the current tool output. New physical40569 is pending with00739/00740, compatibility and saved-mode recovery. Fixture and migrations now remain frozen until its terminal result. Final identity mutation and actual historic config recovery still need physical assertions; full V20–V32 remains incomplete.


### Saved configuration isolation scenarios prepared (2026-10-07)

Added standalone read-only TestIntegrationSavedDefectConfigIsolation against the existing owned native worker fixture. It locates the native child run, checks an authorized config read, then requires zero rows for invalid session/actor/project/run and the fixture's real foreign tenant/principal. ACL assertions require only API access and exclude scheduler, worker and PUBLIC (PUBLIC checked through pg_proc ACL grantee0, not a nonexistent public role). No fixtures, migration files, tenant records or objects are mutated by the new test.

Package97397 passed1.405s before the PUBLIC ACL correction; a final package compile/test was started after that change. Ordinary package execution skips this integration without its owned env. It is not yet added to the Python physical invocation, and no runtime isolation pass is claimed. Physical40569 remained confirmed live with no output at the latest poll; its active fixture and migrations were not edited. Next after terminal: fix actual failures if any, include the standalone isolation test in the native fixture invocation and run physical saved-identity/historical-config negative cases. Full V20–V32 remains active and incomplete.

Package compile after PUBLIC ACL correction initially failed before tests because sandbox denied a standard Go cache file (operation not permitted). Escalated retry57523 used the same command; its authoritative final result is in the tool output. This is distinct from any physical database isolation evidence.


### Early stage resource bound (2026-10-07)

Package57523 PASSED semanticproposal6.045s after the PUBLIC ACL correction. The saved-config isolation integration still skips without owned fixture and has not yet been included in Python physical invocation. Physical40569 was polled twice and remains live without output; do not restart or change its fixture/migrations.

Moved the pure minimum defects worker resource profile check to the beginning of prepareAndAdmitOriginDefectStage, after cancellation, before either explicit producer discovery or saved-locator SQL. This prevents a manifestly impossible budget from entering DB lookup. It is only a lower bound; current qualification and full authorized image/input file accounting still follow. A proposed test using incomplete context was discarded because it could pass through unrelated validation and would not prove the ordering. Existing meaningful execution refusal tests remain. Package9666 started after this change and is still live; no final pass claimed. Full V20–V32 and public parent prerequisite scheduling remain outstanding.


### Resource preflight package verification (2026-10-07)

Package9666 PASSED semanticproposal6.040s, exit0. It covers the stage lower-resource preflight and existing package tests; external integration scenarios skip without owned environment. Physical40569 was polled repeatedly and confirmed live, with no new output. Its fixture and migrations remain frozen; no timeout or restart inferred from silence. Standalone saved-config foreign-tenant/ACL tests are compiled but have not run physically. No completed saved-mode integration result or full V27 completion claimed.


### Physical first-stage timeout and isolation invocation (2026-10-07)

Physical40569 terminal FAILED525.22s; first Go phase FAILED481.64s at committed native admission with context deadline exceeded. All earlier draft construction/persistence/SQL authority markers reached, including composed ACL and original concurrent-writer checks; no saved recovery/native worker/isolated-reader pass claimed. Separate read-only process inspection confirmed the actual Go test running near its limit, not an orphaned handle. Root cause of extra duration is unproven; no timeout increased and no science/authority checks removed.

Added first-stage remaining-budget and stage-duration logs (seconds only, no pins/secrets) for diagnosis. Connected standalone SavedDefectConfigIsolation alongside NativeDefectContent in the physical worker phase and made its marker mandatory. Its own30s context and the existing native150s/subprocess360s limits remain. New physical run started only after40569 terminal, with these tests and latest preflight. Package76261 final result is in the current tool output; new physical result pending. Full public parent/fresh prerequisite/model-GPU/history-negative requirements remain outstanding.


### Original preparation failure phase context (2026-10-07)

Package76261 PASSED semanticproposal2.353s. Physical47999 remains confirmed live with no output at this poll. Fixture/migrations unchanged during that run; no new physical pass claimed.

Added wrapped error context for original execution preparation, descriptor reconstruction, execution qualification, draft reconstruction and independent person authority. Error causes remain available via %w/errors.Is; failures still return zero ticket/proof. No new timeout, fallback, cached scientific acceptance or reduced validation introduced. This diagnoses whether the prior late original-admission timeout is in repeated evidence IO, plan building or admission; repeated operations were inspected but no optimization is claimed without phase/runtime evidence. Package69935 final result is in the current tool output. Complete fresh-source prerequisite scheduling and public parent Propose remain separate required work.


### Preparation error-context verification (2026-10-07)

Package69935 PASSED semanticproposal4.516s, exit0. This confirms package behavior with %w phase context; external runtime tests remain skipped without owned fixtures. Physical47999 confirmed live through session poll and read-only process inspection: the first Go test process was running, not terminal; no additional physical run started. Mandatory saved-config isolation scenario is included in this run's later native phase, but has not yet produced evidence. Current fixture/migrations remain unchanged. No confirmed diagnosis of prior timeout, no saved-recovery/isolation pass or full V27 completion claimed.


### Prepared selection substitution refusal (2026-10-07)

Added validatePreparedDefectSelection to marshalDefectAdmissionEvidence before admission SQL. The owned saved original producer must equal the privately retained exact task ID and original Params SHA; nonempty original Params identity/digest is mandatory. Different task with same Params, changed saved Params and substituted original Params refuse. Historical config may omit the locator; it still requires private original identity/evidence and cannot supply automatic restart discovery. Existing final SQL origin locks remain mandatory; no claim this pure gate establishes science, provenance or admission alone.

Unit cases specifically exercise two independent task IDs sharing the same Params digest and require conflict, plus saved/original SHA substitution. Package47032 final result is in current tool output. Physical47999 remains live and its already-running first Go binary predates this added pure gate; do not attribute its result to later source. Active physical fixture/migrations unchanged. Full original-identity SQL mutation, real historical compatibility, public parent and fresh prerequisite requirements remain outstanding.


### Preliminary whole-plan estimate and closure failure context (2026-10-07)

User requested completion percentage and continued implementation. Preliminary implementation estimate is50–60%, explicitly a coarse judgement with substantial uncertainty, not a computed acceptance metric or production-readiness claim. The normative work package file still marks V20–V32 planned and is not a trustworthy reflection of source progress. Existing platform/V20–V26 source and historical runtime evidence are substantial but remain incompletely accepted. V27 public parent/fresh prerequisites/full GPU/adoption flows and V28–V32/whole-system hardware/model/end-to-end gates remain required. A requirement-by-requirement audit is necessary for a defensible verified percentage; no scope reduction or complete status.

Package47032 PASSED semanticproposal9.824s with exact-task/same-Params substitution refusal. Physical47999 remains live when polled, still no terminal result; no saved recovery/isolation pass claimed. Added %w context to blocked draft persistence, input-closure publication and registration to distinguish these from planning and final SQL admission in a future failure. No timeout, retry, admission or imaging algorithm changed. Package50725 started, result pending. Active physical fixture/migrations unchanged.


### Producer capture transaction consolidation (2026-10-07)

Package50725 PASSED semanticproposal8.271s with closure persistence/publication/registration error context. Physical47999 remains live at the poll; no result or latency cause inferred.

selectDefectOriginProducer now combines its first full original-provenance predicate and proposal producer-ID read in one authenticated tenant/editor query. Previously these were two separate tenant transactions. All17 predicate arguments remain exact (project/principal/session/source and five original refs), no row returns the prior invalid-origin classification. Exact producer discovery, ambiguity refusal, equality of all five rediscovered pins and final provenance recheck remain unchanged, as do final SQL locks/scientific/person checks. This removes one transaction per capture; no claim that it fixes prior deadline or produces measured speedup. Package48469 final result is in tool output. Active physical first Go binary predates this consolidation, and fixture/migrations remain frozen. Full V20–V32 remains incomplete.


### Earlier unrelated timeout and SQL latency aggregates (2026-10-07)

Physical47999 terminal FAILED600.20s; first Go phase FAILED480.22s at existing final-mask policy-revocation case1294, before new draft/original-stage admission/recovery. Expected ErrUnqualified was replaced by a context deadline; this is a test failure, not acceptance of revoked evidence. No saved-mode/native worker/isolation pass. Failure moved substantially earlier than40569, so attributing it solely to saved producer capture is unsupported. Public API audit also confirms main Server still omits SemanticPropose; route guards nil with503, requiring an actual durable parent queued receipt. Internal child tickets must not be exposed as that parent.

Added test-only pgx QueryTracer aggregates for API pool/config copies: static group label, count, summed duration and maximum duration, plus complete test elapsed time. Does not log SQL text/args/principals/DSN; summed query durations are not an exclusive critical-path metric. Owner pools independently constructed without cfg copy are not included. Added no timeout, reduced check or inferred hardware diagnosis. Package39348 final result in tool output. New physical run started after47999 terminal, with measurements and latest producer transaction consolidation/prepared-selection gate; result pending. Full V20–V32 remains active.


### Fresh-source detector input preparation (2026-10-07)

Added PrepareSemanticFaceDetectParams and owned SemanticFaceDetectPreparation. It verifies exact WorkingImage/source SHA and canvas through ResolveSemanticSourceMetadata, builds existing SFD Params (sfd_long1024_bgr_v1, longside1024, existing NMS0.3, bounded server score/maxFaces, supplied model lock), and preserves RGB, optional alpha and every dense-warp field in sorted typed input entries. Ambiguous child IDs refuse; repeated identical geometry refs deduplicate. Params bytes are canonical and no original image bytes/pixels are changed. Cancellation returns zero preparation.

This implements input preparation for fresh prerequisites only, not a queued GPU task. Source/catalog/ICC authorization, real model/report qualifications, measured resource profile, frozen Params publication/input closure, native face producer lineage, scheduling and parent continuation remain mandatory. Worker registration confirms face.detect resource_class=gpu; no CPU task or whole Apply pipeline substituted.

Unit cases use all five Python WorkingImage fixtures (including alpha and dense warp), compare exact child identities/contracts and deterministic Params, and refuse foreign canvas/cancellation. Package82148 before added cases and56652 with cases: authoritative results in current tool output. Physical89503 remains live, with its own diagnostics; this helper is not wired into that fixture and no physical detector inference is claimed. Full V20–V32 remains active.


### SFD worker Params byte agreement (2026-10-07)

Package56652 PASSED v2input32.067s with all five exact-source/alpha/geometry/determinism/canvas/cancellation preparation cases. Generated three frozen SFD Params vectors by calling the real Python worker FaceDetectParams and canonical_bytes: default score0.8/max64 and valid boundaries0.5/max1,0.99/max100. Source bytes preserve the original first WorkingImage fixture's exact JSON representation and SHA; canvas comes from the worker Canvas model. No model inference/weights/GPU or source pixels are generated/changed.

Added TestSemanticDetectorParamsMatchPythonWorker requiring exact Go canonical Params byte agreement with these vectors. Focused package98914 result in current tool output; this is contract evidence only, not native task scheduling, producer qualification or public parent admission. Physical89503 remains confirmed live and does not invoke the new fresh-source helper; its fixture/migrations unchanged. Full V20–V32 remains active.


### V27 native parent JSONB operator correction (2026-10-07)

Physical PG/MinIO run 89503 failed after 650.82 seconds in NativeDefectContent: PostgreSQL 42725, ambiguous unknown-minus-unknown operator in native parent binding. SavedDefectConfigIsolation passed all seven scoped cases, including foreign tenant/principal, and emitted its mandatory marker. This is partial evidence, not a passing full chain.

Added forward-only migration 00741: parentheses force extraction of original_producer JSONB before key subtraction. Migration 00739 remains unchanged. Reverification started as session 51431; outcome pending. Do not claim full runtime acceptance.

Go v2input focused run 72721 passed (0.673 seconds) after shared face-source closure extraction and addition of FAN2D Params preparation. FAN preparation verifies exact FaceBoxes bytes, original detector Params and source binding before constructing inputs. It is not yet wired to task scheduling; dedicated FAN tests and GPU/model qualification remain outstanding.


### FAN2D exact detector/source binding tests (2026-10-07)

Focused Go run 65916 passed: v2input 0.551 seconds, TestSemantic(Landmark|Detector). New TestSemanticLandmarkExactDetectorBinding covers all five existing WorkingImage fixtures, preserves source/pixel/alpha/dense-warp input closure plus exact FaceBoxes, rejects changed output bytes and original Params bytes, foreign-source output even with its correct digest, source artifact alias and detector-model mismatch even with correct Params digest. These are content preparation tests; canonical producer authority, science, GPU scheduling, publication and locked admission are not established by them. Physical migration recheck session 51431 was polled and remained live with no new output.


### BiSeNet input preparation (2026-10-07)

Added PrepareSemanticFaceParseParams with exact-source child closure and original SFD output/Params digest and content binding. Uses actual worker FaceParseParams: bisenet_face19, crop512 preprocessing, expansion1.35. FaceParseParams is exported from review with an internal alias retaining existing fixtures. Parsing consumes FaceBoxes directly; FAN2D remains a parallel prerequisite branch, not an invented parse input.

Run 54057 passed: v2input5.264s, review0.927s, selected TestSemantic|Test.*FaceParse. New parse preparation test covers all five WorkingImage fixtures, exact worker Params and input closure, changed bytes, foreign source with correct digest, artifact alias and detector model mismatch. This does not prove production producer authority, qualification, queue registration or actual GPU segmentation. Physical run51431 remains live on latest poll; no full acceptance claim.


### Face-skin input preparation (2026-10-07)

Added PrepareSemanticFaceSkinParams: verifies original parser Params/FaceLabels19/detector bytes and exact source geometry through existing VerifyFaceParseParams; retains only worker-required FaceLabels19 and its exact uint16 NpyArray child. Rejects map/root/source/Params/detector aliases. Does not modify class1 skin derivation or confidence algorithm. Physical uint16 content, canonical producer/attempt, qualification and queue admission remain separate mandatory gates.

Go93987 passed (v2input6.205s, review0.732s) selected semantic/original detector checks. Dedicated Go22903 passed v2input0.524s: all five WorkingImage fixtures preserve exact parser map, reject changed manifest bytes, source-map alias with correctly updated manifest digest and foreign source with correctly updated digest. Physical migration test51431 was re-polled and remains live; outcome pending.


### Independent person Params preparation (2026-10-07)

Added PrepareSemanticPersonParams using existing LRASPP Params contract, exact WorkingImage source closure, long_side768 and allowlisted guided_tile256/512/1024. No face/skin-derived person coverage and no inference algorithm changes. Existing private semanticPersonParams type reused.

Go11816 passed v2input0.510s for person preparation plus detector/landmark/parse/skin preparation checks. New person test covers all five original WorkingImage fixtures, including alpha and dense-warp child references, deterministic Params/input preparation, wrong canvas refusal and cancellation. Model/GPU qualification, producer admission and scheduler wiring remain required. Physical run51431 was polled again and remains live without new output.


### SFD native input closure (2026-10-07)

Added CompileSemanticDetectorClosure: re-prepares exact source children and canonical SFD Params, rejects Params/child UUID aliases and mismatched published Params SHA, freezes deterministic face.detect task inventory through existing compileCaptureInventory, retains bounded Params/source/pixels bootstrap and full manifest cursor. No caller-supplied input inventory accepted. Qualified GPU profile, blocked task creation, publication/registration, producer lineage and queue activation remain outstanding.

Go51890 passed v2input0.595s before new closure assertions. Go84292 passed v2input0.477s after closure checks on all five source fixtures: exact Params transport, bounded bootstrap, changed published digest and source alias refusal. Physical51431 remained live at latest poll; no outcome inferred from silence.


### Migration00741 runtime pass and SFD capacity compiler (2026-10-07)

Physical PG/MinIO pytest session51431 PASSED: 1 passed,13 deselected,396.18s. Scope: manual-lock-go-disjoint-external V27 fixture, full migration chain through00741, initial origin-first admission, saved-producer completed recoveries, native CPU defect content/parent binding and mandatory saved-config tenant/principal isolation. Earlier42725 failure is corrected. This does not establish new face preparation/task helpers, actual GPU inference, parser-positive chain, public SemanticPropose installation or overall V27 acceptance.

Added CompileSemanticDetectorProfile: CUDA-only qualified positive VRAM peak; 200MP bounded canvas; exact input byte quantity, conservative 128bytes/pixel conversion envelope plus1GiB worker/candidate headroom; additional trusted model/inference costs; bounded1MiB control output; safe-integer overflow and each capacity limit refusal. It is a conservative capacity compiler, not measured RTX3060 performance. Exact catalog-size aggregation and locked scheduler integration remain outstanding.

Go99230 passed v2input0.547s compilation/existing detector tests. Go65090 passed v2input0.590s after new capacity tests:24MP at32GiB/12GiB budgets, exact resource boundaries and one-byte insufficiency per RAM/VRAM/scratch/output, zero/negative/overflow input and absent GPU qualification peak refusal. No real model hardware claim.


### Exact SFD declared-input accounting (2026-10-07)

Added CompileSemanticDetectorProfileFromInputs. Shared existing defect accounting as exactSemanticInputBytes without changing its semantics: exact Params plus declared-file identity/SHA match, unique bounded positive sizes, bounded Params1MiB and safe-integer sum. Missing/extra/duplicate/hash-substituted rows and duplicate declared refs fail. Catalog tenant/editor/ready authorization remains required outside this pure compiler.

Go89808 PASSED v2input0.452s selected detector/defect-profile tests. New SFD test proves summed1536 bytes against direct profile, order independence and invalid metadata/alias refusal; existing defect capacity/exact-file tests passed after helper extraction. No physical SQL or GPU run performed for this helper. Next integration remains frozen native task plus authorized catalog-size reader, qualified profile and locked registration.


### Frozen native SFD task compiler (2026-10-07)

Added FrozenSemanticDetectorTask / CompileSemanticDetectorTask / PublishSemanticDetectorTask / BindPublishedTransport. Compiler revalidates Params/source, derives exact source input refs, calculates qualified CUDA profile from complete sizes, owns deterministic face.detect ID/key and immutable profile/closure. Draft resource_class is gpu. Publication uses existing immutable BuildInputClosure and frozen transport verifier, without creating/registering/activating database tasks. Source/size catalog authority, deployment qualification, locked registration and scheduler wiring remain necessary.

Go94725 passed v2input0.811s before task assertions. Go97725 passed v2input0.644s after new assertions on all five source fixtures: frozen task matches closure digest, gpu route, caller mutation of returned DraftTask profile does not mutate frozen state, missing size refuses compilation. Size values and model costs are synthetic unit inputs, not catalog/GPU measurements. Publication path compiled but has not yet dedicated runtime coverage.


### Catalog-backed SFD preparation and transport refusal (2026-10-07)

Added private prepareCatalogDetectorTask in semanticproposal: owns source bytes before callbacks, derives exact contract descriptors without accepting caller inventory, minimum-size pure preflight before IO, then existing ReadInputSizes contract-scoped repeated catalog lookup and final exact-size task compilation. It does not itself qualify original source/model or activate tasks. Dedicated catalog mutation/refusal tests remain outstanding.

Go27420 compiled semanticproposal0.692s (no selected tests) and passed existing detector tests v2input0.972s. Added publication assertions on all five source fixtures: full input count, frozen digest and Params; mismatched transport SHA and truncated count refusal. Go78030 outcome recorded separately from compilation if available. SQL registration/main wiring and actual GPU run remain incomplete.

Go78030 PASSED v2input0.680s including the new SFD publication/transport refusal assertions. This is in-memory publication evidence, not PostgreSQL registration or object-store runtime.


### SFD catalog mutation/refusal coverage (2026-10-07)

prepareCatalogDetectorTask now also requires catalog sizes for original WorkingImage JSON and canonical Params to equal retained exact byte lengths. Added TestCatalogDetectorTaskRefusesRevocationAndMutation: valid scoped repeated reads, revoked lookup on recheck, changed immutable version on recheck, incorrect Params size and canceled-before-IO; failures cannot expose a usable DraftTask. Unit catalog checks actor/session/tenant propagation; this is not physical RLS evidence.

Go18185 PASSED semanticproposal0.886s. Database registration, deployment/model qualification integration and queue activation remain outstanding.


### Qualified SFD preparation (2026-10-07)

Added prepareQualifiedCatalogDetectorTask. Owns expected deployment and source bytes; requires face.detect/v1/cuda, validates source/Params closure before report IO, obtains scoped original face qualification artifact, reads exact pinned bytes, verifies report through v2qualification.Verify, enforces measured cost lower bounds, rechecks live report before and after catalog-backed preparation. Does not activate tasks; physical source pixels/ICC proof and final transaction qualification/source/input locks remain mandatory.

Go62077 passed semanticproposal0.655s existing selected catalog/measured-cost checks. Go3662 passed semanticproposal0.560s including new wrong operation/version/backend and canceled-before-IO refusal. These negative tests and compilation do not establish the full positive report path; positive report mutation/import-policy integration tests remain outstanding. SQL registration and queue wiring not yet complete.


### Original source verification composed with SFD (2026-10-07)

Added sourceDetectorPreparation and prepareSourceDetectorTask. Uses existing readQualifiedSourcePreparationProof with unitRGB=true to verify actual RGB/alpha/geometry and deployment ICC, reads original WorkingImage, composes qualified catalog SFD task, and rechecks retained source pins after later IO. Retains private source proof for final source-evidence transaction locks. No HDR clipping, face preprocessing change, SQL activation or public child ticket. Model/report/policy and task-input locks remain to be composed at admission.

Run87319 selected existing qualified-detector/catalog/native-source/source-reader checks; no dedicated positive physical test of the new composition yet. Overall V27 scheduling and public service remain incomplete.

Go87319 PASSED semanticproposal0.752s for the selected existing checks; confirms compilation and covered refusal paths, not full positive physical SFD preparation.


### Retained SFD qualification proof (2026-10-07)

Qualified detector preparation now retains private actor/session/tenant/project/bucket scope, original QualificationReport object identity, canonical expected deployment and verified measured metrics alongside frozen task. Source preparation retains this qualification proof too. Added marshalDetectorQualificationProof and recheckDetectorQualificationProof for strict retained scope/deployment/report pin validation and repeated live lookup; final transactional policy/report locking is still not implemented by these helpers.

Go93965 PASSED semanticproposal0.662s selected existing detector/catalog/native-source tests after return-value refactor. Go13988 verifies compilation with existing selected detector/catalog tests; dedicated positive retained-proof and SQL-lock runtime tests remain outstanding. No queue activation or public V27 completion claimed.


### SFD source/report transactional lock composition (2026-10-07)

Added lockDetectorQualificationProof using existing scheduler-only lock_semantic_proposal_face_qualification (migration00678), with strict retained proof validation and current_user check. Added lockSourceDetectorPreparation composing original source and original report/import-policy locks in the caller transaction; caller must rollback failures. It requires retained unitRGB and deployed ICC SHA plus frozen task. Snapshot/policy writer barriers, input registration, quota, persisted shell/receipt and activation remain separate mandatory work. No new grants/migration or automatic task activation.

TestDetectorProofRetainsExactScopeAndDeployment proves valid synthetic retained serialization, rejects tenant/actor/session/project/bucket, null object version, mismatched object hash and changed deployment substitutions, and rejects nil lock transaction. Go38138 PASSED semanticproposal0.557s before lock composition. Go62424 selected proof/catalog/qualified detector tests; this is unit/compiler evidence, not physical row-lock concurrency. Physical SFD admission tests remain outstanding.


### SFD task-to-qualification immutable binding (2026-10-07)

Qualified SFD preparation now retains canonical DraftTask SHA alongside original report proof. Source/report lock composition verifies the frozen task against that SHA before any SQL locks, preventing exchange of task ID, Params, input digest or execution profile after report verification. This supplements rather than replaces original scope/report/producer authority.

Go3567 PASSED semanticproposal0.750s before new assertions. Added fingerprint checks to valid catalog fixture: exact frozen task accepted, changed retained SHA rejected as ErrConflict. Go9851 runs these selected proof/catalog/deployment cases. Still incomplete: physical positive source/report/SFD admission, complete persisted parent/child workflow, task input size pin locking and activation.


### Retained original SFD input objects (2026-10-07)

Catalog preparation now uses existing ReadInputEvidence (same repeated scoped lookups) and retains immutable canonical exact InputEvidence alongside frozen task. Qualification/source composition propagates these retained inputs. Full object versions, sizes, SHA and contracts remain available for final SQL pin locking instead of being discarded into aggregate size totals. JSON bounded4MiB; no output of object keys or secrets.

Go85530 PASSED semanticproposal0.548s after return-value refactor. Added exact retained count/version/SHA/size assertions to valid catalog test. Go81143 selected proof/deployment/catalog tests. These remain unit/compiler evidence; task input SQL locking and full persisted workflow/activation remain outstanding.


### Exact retained SFD inventory preflight (2026-10-07)

Added FrozenSemanticDetectorTask.VerifyInputInventory and decodeDetectorInputEvidence. Canonical retained JSON bounded4MiB, bounded unique descriptors and nonempty immutable object versions/valid sizes/hash consistency, complete frozen task inventory digest comparison. Source/report admission lock composition runs this preflight before its SQL lock callbacks. No catalog/input authority is granted by this decoder; physical object version/size/expiry/auth locks still need implementation.

Go83554 PASSED semanticproposal0.651s and v2input1.063s selected SFD/proof/catalog tests. New assertions accept exact retained inventory and reject truncated/duplicate evidence. No physical input SQL registration/activation evidence.


### SFD source catalog contract correction (2026-10-07)

Inspection of SQLArtifactCatalog exposed that worker NpyArray descriptors cannot authorize original source arrays. prepareCatalogDetectorTask now derives WorkingPixels/WorkingAlpha/DenseWarpField catalog contracts with exact source ParentRef; WorkingImage retains CanvasHash. Retained evidence decoder maps only these source-array contracts back to worker NpyArray before frozen inventory verification. Test catalog now refuses raw NpyArray and unparented source arrays.

Params authorization remains unresolved: generic SQLArtifactCatalog cannot yet prove newly published detector Params. Dedicated Params provenance/registry adapter and physical full admission are still required; do not treat preparatory generic catalog helper as production-ready. No broader scope grants introduced. Run64606 covers selected catalog/proof/deployment unit tests, not physical catalog/SQL locks.


### Accepted-version SFD Params publication (2026-10-07)

Added v2draft.Publisher.PublishSemanticDetectorParams: derives canonical SFD Params from exact source metadata, rejects document UUID alias with source children, then uses existing accepted-version Params document publication lifecycle (quota/fencing/cleanup) through publish(false,true). Does not accept client-authored Params or activate tasks. Separate Params catalog provenance adapter still needs implementation; full physical publication/admission tests remain outstanding.

Go6336 selected existing draft Params and detector catalog/proof tests; this confirms compilation/existing checks only, not dedicated SFD publisher runtime coverage. No migration/grant changes.


### Accepted-version Params catalog adapter (2026-10-07)

Added detectorParamsCatalog. Exact sole Params root authorized via existing semantic_defect_version_params_object ownership SQL (migration00701 is metadata-generic accepted-version Document ownership, not defect scientific content). Source children delegated unchanged to source catalog; wrong Params ID/parent or source-contract alias rejected before lookup. Source detector composition wraps catalog with exact document/ref and retains document UUID for final Params lock. Existing SFD canonical derivation/report checks supply operation/content consistency. No new SQL grants.

Go25064 PASSED semanticproposal0.999s; v2draft compiled0.427s with no selected tests. Added adapter refusal/delegation checks. Go29513 selected adapter/catalog/proof tests. Physical positive Params ownership, version metadata locking and full queue admission remain outstanding.


### SFD accepted-version Params transactional lock (2026-10-07)

Added ParamsByteCount, marshalDetectorParamsPin and lockDetectorParams. Requires exact frozen Params UUID/SHA/byte length/application-json metadata and document owner; scheduler-only call uses existing lock_semantic_defect_version_params accepted-version metadata ownership gate (00701). Source/report lock composition preflights the pin and locks Params after source/qualification. No new grants or activation. Original source child versions must also be matched against source proof; this is still an outstanding composition requirement.

Go66010 PASSED semanticproposal0.563s and v2input0.953s compilation/existing selected tests. Added valid Params pin and missing-document refusal assertions to catalog fixture, Go90136. Physical published-document lock/concurrency and persisted workflow/queue tests remain outstanding.


### Exact SFD input objects matched to source verification (2026-10-07)

Added verifyDetectorSourceInputPins: every non-Params retained catalog object must exactly match original verified source pin identity/contract/full immutable object metadata; complete unique source set required, duplicate same-source geometry only deduplicated if identical. Called after full source preparation and before transaction source/report/Params locks. Thus resource-accounted source versions/sizes cannot diverge from actually verified and locked source evidence. Params remains separately document-locked.

Go69011 PASSED semanticproposal0.682s selected detector/catalog tests. Added positive source-pin composition and changed RGB object-version refusal. Fixture media types corrected to source array octet-stream vs JSON. This is unit evidence; persisted SFD shell, queue activation and physical composed locks still outstanding.


### Parent-bound stable SFD child-stage identity (2026-10-07)

Added detectorPreparationIDs: UUIDv5 child run semantic.face.detect:reference and existing WorkflowID. Source detector preparation now takes parent run, derives child workflow internally, requires ordinal0 and retains parent; final lock composition verifies exact frozen task ID against derived child identity. Retry identity cannot vary with caller-supplied workflow. Request/config hash, durable child shell/receipt and SQL activation still need implementation.

Go61724 PASSED semanticproposal0.567s before composition change. New identity tests prove deterministic retries, different parents and disjoint detector/defect child IDs, zero parent refusal. Go26392 selected detector/catalog checks after source preparation signature/lock update. No physical persisted workflow admission claim.


### Parent-bound SFD blocked draft plan compiler (2026-10-07)

Added compileDetectorDraftPlan/detectorDraftPlan. Recompiles owned parent request; binds actor/session, source, published intent hash, exact target snapshot and inventory; selects original face.detect policy and matches retained qualification; verifies task stage/fingerprint/source-input/Params pins; validates output reservation; builds deterministic semantic child DraftShell with blocked task and immutable parent/request/source/intent/snapshot/Params-document/task/input/policy config. No database shell creation, registration, quota mutation or activation yet.

Go25671 selected existing detector/catalog checks, compilation only for new plan. Dedicated positive/negative plan fixtures and physical draft admission remain outstanding. SQL generic shell operation/config allowlist compatibility must be verified before calling BeginAdmission. Overall V27 remains incomplete.


### Blocked SFD draft transaction composition (2026-10-07)

Added beginDetectorDraftInTransaction. Recompiles owned parent request/anchor and exact child shell/config/tasks before SQL; composes existing parent inventory, intent and snapshot locks, source/report/Params locks and global qualification-policy snapshot barrier, then existing BeginAdmissionInTransaction. Returns only created/pending blocked child identity; never seals/activates tasks. Caller rolls back errors. Original request key retained privately for shell reconstruction.

Inspected foundation create_v2_draft_shell: operation registry permits face.detect/gpu, with generic semantic config/project shape. Registered transport replay still needs dedicated detector handling analogous to00703; no such replay migration added yet. Go82492 selected existing detector/catalog tests confirms compilation only for the new transaction composition; positive draft-plan/SQL runtime tests remain outstanding. Full scheduler activation/public service remain incomplete.


### SFD registered-shell replay migration (2026-10-07)

Added forward-only00742 wrapper around existing v2_pending_shell_matches. Only v27_detector_prepare semantic child handled specially; existing defect/transfer paths delegated unchanged. Registered transport profile is accepted only after exact face.detect/v1/gpu task, Params ref, input digest, base profile and v2_input_closure_ready comparison. Child run bound to parent UUIDv5 reference detector stage and empty edges; prior exact shell matcher retains full original persisted equality. Function remains private, no new scheduler/API direct grants.

Physical PG/MinIO regression launched as session67841 with existing manual-lock-go-disjoint-external fixture, all migrations through00742. Outcome pending; fixture and migrations frozen while live. Existing fixture primarily establishes defect/transfer regression and SQL migration installation, not dedicated positive SFD registered replay. Such SFD runtime fixture and full admission remain outstanding.


### SFD draft transport publication composition (2026-10-07)

Added publishDetectorDraftTransport: verifies stable parent/run/workflow/task, retained task qualification fingerprint and exact shell task; publishes native immutable input transport outside DB transaction, then freezes complete bounded registration packet using existing operation-independent freezeDefectRegistrationProof. Caller must invoke only after blocked shell commit. No SQL registration/replay/activation implemented by this function.

Go45787 selected detector/catalog/registration-proof checks: compilation and existing tests, no dedicated positive transport-plan test yet. Physical00742 regression session67841 remains live on latest poll. Migrations and physical fixture remain frozen; dedicated SFD registration-state SQL will follow after that run terminates.


### Positive synthetic SFD draft/transport composition (2026-10-07)

Extended TestCatalogDetectorTask valid fixture with real Go request/snapshot compilation, policy selection, stable parent-bound SFD task, retained source/input/report metadata, draft-plan compilation and in-memory transport publication/registration packet. Child shell identity/task retained; changed shell Params SHA refuses publication. Report metadata/model costs/array content are synthetic: this does not qualify inference or prove SQL admission.

First compile failed due duplicate local test variable declarations; corrected without production changes. Go42979 PASSED semanticproposal1.072s. Physical67841 was re-polled and remains live, no new output; migrations and physical fixture remain frozen. Full SQL registration/replay/activation and positive original report/source runtime still outstanding.


### Immutable complete SFD draft fingerprint (2026-10-07)

Added private detector plan seal over owned parent request/key, published intent/inventory, shell/config/reservation, snapshot, policy/input/source objects, document/parent/report/metrics/task fingerprint. Compiler stores seal; publication and SQL Begin verify it before side effects. No DB authority granted. Canonical JSON whitespace is intentionally semantically equivalent; initial test wrongly expected whitespace-only Config change rejection and was corrected to semantic added-key mutation. String-backed retained policy/input bytes still exact.

Go11395 exposed the incorrect whitespace test; Go32661 reruns selected detector/catalog tests with reservation/config/policy/input mutation refusals plus positive draft/transport scenario. Physical67841 remains live on last poll; migrations/physical fixture remain frozen. SQL registration/replay/activation still outstanding.


### SFD registration replay gate and composition (2026-10-07)

Physical67841 PASSED:1 passed,13 deselected,319.87s through00742; existing manual-lock-go-disjoint-external defect/native/saved-config regression, not dedicated SFD replay.

Added forward-only00743 semantic_detector_preparation_registration_state mirroring exact existing defect gate for face.detect/v1/gpu: stable parent child identity, draft blocked attempt0, exact Params/input/profile/config, registered rows/transport/direct/chunks/manifests and ready backend-document ownership. Returns missing/replayed/conflict; never repairs partial state or activates. Scheduler-only grant, private function ownership photo_fn_owner. Added registerDetectorInputsInTransaction: frozen transport binding, recompile/re-lock draft authority, exact registration-state lookup, RegisterInTransaction only for missing, mandatory post-write replayed check. Caller rollback required.

Go89417 selected detector/catalog/general registration-proof tests, compilation only for new SQL interaction. New physical regression launched after prior terminal pass; outcome pending. Dedicated SFD SQL admission/registration/activation fixture still outstanding.
# SFD registration boundary verification (2026-10-07)

The Go detector registration reader now rejects a mismatched task ID, operation,
operation version, resource class, or empty tenant/run before opening the input
cursor or issuing SQL. Cancellation is checked first. Unit tests use a transaction
that would panic if SQL were reached for invalid task shells.

The synthetic catalog/draft/transport fixture now asserts every one of the 13
arguments passed to the detector registration-state SQL function, including the
child run, task, Params, execution profile, complete rows, manifest, chunks,
direct inputs, count and entries digest. It verifies propagation of missing,
replayed and conflict responses; this is a Go boundary test, not proof of the
actual SQL function or GPU inference.

Verification: `go test ./internal/semanticproposal ./internal/v2input -count=1`
passed (semanticproposal 1.575s; v2input 5.284s). The existing physical integration
run through migration 00743 is still running at this checkpoint. No positive
native SFD SQL registration or genuine GPU qualification is claimed.
# Native FAN2D input closure and migration regression (2026-10-07)

Added `CompileSemanticLandmarkClosure`: it re-verifies original SFD Params and
FaceBoxes against the exact WorkingImage, derives source pixel/alpha/geometry
inputs, and freezes the FAN2D task inventory with its own canonical Params.
The original SFD Params remain provenance proof and are not declared as current
task Params. Task identity uses face.landmarks and the supplied workflow ordinal.
Three direct bootstrap refs are Params, WorkingImage and its exact RGB pixels;
FaceBoxes and all source children remain in the complete registered manifest.

Tests cover all five existing Python WorkingImage fixtures, including alpha and
dense geometry. They inspect the complete cursor and reject Params aliases to
the source, detector Params and FaceBoxes, as well as a wrong Params digest.
`go test ./internal/v2input ./internal/semanticproposal -count=1` passed
(5.325s and 1.155s respectively). This is input compilation, not GPU admission,
scientific qualification or complete native FAN2D scheduling.

The physical `manual-lock-go-disjoint-external` regression finished successfully:
1 passed, 13 deselected in 326.62s. It installs migrations through 00743 and
checks the existing manual defect/transfer/native CPU flow. It does not exercise
a positive SFD child registration, GPU inference or the new FAN2D closure.
# Native BiSeNet input closure (2026-10-07)

Added `CompileSemanticFaceParseClosure` for face.parse. It derives the full
source inventory and exact FaceBoxes from verified original SFD Params/output,
freezes its own canonical BiSeNet Params, and keeps the original detector Params
outside the worker input inventory as provenance proof. Source, detector output,
original Params and current Params cannot alias. The published Params SHA must
match the canonical generated bytes. BiSeNet consumes FaceBoxes directly and
can execute independently of FAN2D once producer authority is confirmed.

The existing five Python WorkingImage fixtures now test complete parser closure
cursors, required FaceBoxes and own Params, bounded direct inputs, no accidental
detector Params registration, and refusal of aliased or changed Params. Full Go
package tests passed: `go test ./internal/v2input ./internal/semanticproposal
-count=1` (5.358s and 1.182s). This proves compilation and existing unit
regressions, not producer SQL admission, GPU qualification or native scheduling.
# Native skin-mask input closure (2026-10-07)

Added `CompileSemanticFaceSkinClosure` for the existing CPU worker operation
`mask.face_skin`. The exact worker inventory and direct bootstrap contain three
artifacts: own canonical Params, original FaceLabels19 manifest, and its original
uint16 NpyArray map. Source and upstream detector/parser documents remain
verification evidence, not worker downloads. The compiler verifies metadata
lineage and rejects own Params aliases to upstream roots, label map, source or
source children, as well as incorrect Params bytes/digest.

All five existing WorkingImage fixtures now exercise the full skin input cursor,
three direct refs and substitution refusals. Verification passed:
`go test ./internal/v2input ./internal/semanticproposal -count=1` (6.458s and
1.244s). This does not prove physical uint16 classes, original producer SQL
authority, admitted resource profile, scheduler wiring or end-to-end GPU chain.
The existing worker skin algorithm and its quality behavior were not changed.
# Independent LRASPP input closure (2026-10-07)

Added `CompileSemanticPersonClosure` for the existing GPU worker operation
`mask.person`. It freezes own canonical LRASPP Params with the complete exact
WorkingImage, RGB, optional alpha and geometry child inventory. It derives
inputs from source content and does not infer person coverage from face/skin
artifacts. Direct bootstrap refs are own Params, WorkingImage and RGB pixels.
Aliases between Params and any source artifact, or a changed Params SHA, fail.

All five existing Python WorkingImage fixtures now inspect the complete cursor,
its source children, own Params, direct refs and substitution refusals. Full Go
package tests passed: `go test ./internal/v2input ./internal/semanticproposal
-count=1` (6.198s and 1.517s). This is source/input compilation evidence only;
qualified LRASPP costs, actual GPU inference, producer admission, scheduling and
native end-to-end protection remain unverified. Image algorithms were unchanged.
# Skin-mask CPU resource profile (2026-10-07)

Added `CompileSemanticFaceSkinProfile` and its exact-catalog-size adapter.
The CPU-only profile accounts for all downloaded files, a 256MiB baseline,
16 bytes per pixel of RAM residency/temporaries, float32 NPY with bounded header,
the worker's exact conservative PNG16 bound, and a bounded MaskRef document.
Scratch includes downloads and the output envelope. VRAM is zero. Pixel count
is bounded to 200 million and all sums respect the JSON exact-integer limit.

The 24MP test checks every calculated allocation, admits exact capacity, refuses
one byte less in each RAM/scratch/output limit, and rejects invalid dimensions
and overflow. Full Go tests passed: `go test ./internal/v2input
./internal/semanticproposal -count=1` (5.769s and 1.197s). This is a conservative
capacity bound derived from the worker, not measured runtime or production
scheduler integration. Existing processing algorithms were unchanged.
# Frozen CPU skin task and transport (2026-10-07)

Added `FrozenSemanticFaceSkinTask`, task compilation, publication and binding of
published transport. Compilation owns the exact three-input closure and CPU
profile using exact declared file sizes. It derives stable mask.face_skin task
identity and node key; returned DraftTask profiles are copies. Binding rechecks
the complete frozen transport digest/count/direct refs before registration.

Five WorkingImage fixtures exercise CPU routing, Params identity, immutable
profile ownership, in-memory publication, refusal of truncated transport and
missing catalog sizes. Full package tests passed: `go test ./internal/v2input
./internal/semanticproposal -count=1` (5.480s and 1.164s). Synthetic file sizes
in these tests are not physical catalog evidence. Original parser producer,
physical map validation, SQL admission and scheduler integration remain separate
unfinished requirements; no inference or queue activation is claimed.
# FAN2D resource profile (2026-10-07)

Added `CompileSemanticLandmarkProfile` and exact-file accounting adapter.
The cuda profile includes all declared downloads, 1GiB baseline, conservative
128 bytes per source pixel for color/alpha conversion and temporaries, and
additional qualified model/inference RAM, VRAM and scratch peaks. It reserves
8MiB for the landmark document including repeated per-face geometry. Face crops
are processed sequentially by the existing worker. No hardware measurement is
inferred from these bounds or synthetic qualification costs.

The 24MP test checks exact estimates, admits exact capacity, rejects one byte
less in every resource limit, and refuses missing qualified VRAM. Full Go tests
passed: `go test ./internal/v2input ./internal/semanticproposal -count=1`
(6.855s and 1.292s). Task composition, actual qualified artifact/producer admission,
GPU runtime and scheduler integration remain unfinished. Algorithms unchanged.
# Frozen FAN2D task and publication (2026-10-07)

Added `FrozenSemanticLandmarkTask`, task compilation, publication and exact
published-transport binding. Compilation derives stable face.landmarks identity,
verifies original SFD output/Params, owns canonical FAN2D Params and complete
source closure, and computes the cuda resource profile from exact declared file
sizes and qualified model costs. Draft profiles are returned as independent
copies; published transport must match the original frozen inventory.

All five WorkingImage fixtures test GPU routing, Params identity, profile
ownership, in-memory publication, truncated transport refusal and missing file
size refusal. Full Go packages passed: `go test ./internal/v2input
./internal/semanticproposal -count=1` (6.581s and 1.283s). Synthetic sizes/costs
are unit fixtures, not genuine catalog/qualification evidence. Producer SQL
admission, actual GPU execution and scheduler orchestration remain unfinished.
# BiSeNet resource profile (2026-10-07)

Added `CompileSemanticFaceParseProfile` and exact-file-size adapter. The cuda
profile includes source downloads, 1GiB baseline, conservative 128 bytes per
pixel for color conversion, full-frame labels/confidence/restored arrays and
temporaries, plus qualified inference/model costs. Output includes uint16 NPY,
8192 header bytes and a 1MiB FaceLabels19 manifest envelope. Downloads and output
are included in scratch; VRAM must have a positive qualified bound.

The 24MP test validates all calculated amounts, exact-capacity admission, one
byte short refusal in every resource dimension and missing qualified VRAM.
Full package tests passed: `go test ./internal/v2input ./internal/semanticproposal
-count=1` (5.861s and 1.259s). This is a conservative capacity bound, not measured
GPU behavior. Frozen task composition and actual producer/scheduler admission
remain unfinished. Existing BiSeNet processing was unchanged.
# Frozen BiSeNet task and publication (2026-10-07)

Added `FrozenSemanticFaceParseTask`, compilation, publication and exact published
transport binding. The task owns canonical BiSeNet Params, verified source/SFD
inventory and cuda profile calculated from exact catalog sizes and qualified
costs. Task identity and routing are face.parse/v1/gpu. Draft profile copies do
not mutate the frozen plan; shortened transport cannot bind back to it.

All five WorkingImage fixtures verify routing, Params, profile ownership,
in-memory publication, truncated transport and missing size refusal. Full Go
packages passed: `go test ./internal/v2input ./internal/semanticproposal -count=1`
(5.464s and 1.177s). These synthetic size/qualification fixtures do not prove
actual catalog authority, producer admission, GPU inference or scheduling.
Native SQL admission and orchestration are still unfinished; algorithms unchanged.
# LRASPP resource profile (2026-10-07)

Added `CompileSemanticPersonProfile` and exact-file-size adapter. The cuda
profile includes complete source downloads, 1GiB baseline, 128 bytes per pixel
for conversion/probability/output residency and temporaries, plus qualified
model/inference costs. Output includes float32 NPY, bounded header, the worker's
PNG16 bound and separate 1MiB envelopes for MaskRef and PersonReport. Scratch
includes inputs and output; qualified VRAM must be positive.

24MP tests verify exact estimates, exact limits and refusal when any budget is
one byte short, and reject missing qualified VRAM. Full Go tests passed:
`go test ./internal/v2input ./internal/semanticproposal -count=1` (5.517s and
1.196s). Costs are synthetic unit inputs, not measured RTX3060 qualification.
Frozen task composition, installation/runtime identity gates and scheduling
remain unfinished. The existing LRASPP/refinement algorithms were unchanged.
# Frozen LRASPP task and transport (2026-10-07)

Added `FrozenSemanticPersonTask`, compilation, publication and exact transport
binding. It owns independent source inventory, canonical LRASPP Params and cuda
profile, derives mask.person task identity, and verifies complete published
transport against the frozen task. Model/source authority and SQL scheduling
remain separate required gates.

Five source fixtures test GPU routing, Params identity, profile ownership,
in-memory publication, truncated transport refusal and missing catalog size.
An initial test compile failed due to a local variable name collision, corrected
without production behavior changes. Final full Go package tests passed:
`go test ./internal/v2input ./internal/semanticproposal -count=1` (5.952s and
1.583s). Synthetic size/cost inputs do not constitute physical catalog evidence,
installed-runtime qualification, actual inference or scheduler integration.
# Complete native inventory substitution verification (2026-10-07)

Added shared boundary tests for frozen FAN2D, BiSeNet, skin-mask and LRASPP
inventory verification across all five WorkingImage fixtures. Exact inventories
and reordered catalog descriptors pass; missing, extra, duplicate, changed
contract and changed SHA descriptors fail. Cancellation is preserved. These
tests cover the descriptor binding required before retaining catalog objects for
admission; they do not authorize catalog access or replace producer SQL locks.

Full Go package tests passed: `go test ./internal/v2input
./internal/semanticproposal -count=1` (5.453s and 1.429s). No processing algorithm
or transport authority was changed. Physical native admission and execution
remain unfinished.
# LRASPP scoped catalog preparation (2026-10-07)

Added `prepareCatalogPersonTask`. It derives source catalog descriptors with
exact WorkingImage parent/canvas bindings, preflights source/Params/capacity
before IO, reads twice-authorized immutable object evidence, verifies exact
source and Params byte lengths, then compiles the LRASPP task from catalog file
sizes. Full object pins are retained as bounded canonical evidence. Raw worker
NpyArray descriptors are never passed to the scoped catalog.

Unit tests across five source fixtures verify full pins and repeated lookups,
and refuse revoked access, changed object versions and changed Params size.
These use a simulated catalog; actual SQL catalog access, model qualification,
original source byte/ICC proof and final admission locks are still separate.
Full Go package verification was run; no image algorithm was changed.
# Retained LRASPP catalog evidence validation (2026-10-07)

Added `decodePersonInputEvidence` using the shared source-only native evidence
decoder already used by SFD. Each concrete frozen task still verifies its own
operation-bound complete inventory digest. LRASPP catalog preparation invokes
this validation before returning retained pins. It requires bounded canonical
JSON, unique full metadata, non-null object versions, exact SHA and positive
safe file sizes, then maps scoped source child contracts to worker NpyArray.

Five source fixtures reject missing/duplicate entries, null versions, wrong
object SHA, zero sizes, changed contracts and noncanonical retained JSON. Full
Go tests passed: `go test ./internal/semanticproposal ./internal/v2input -count=1`
(1.461s and 5.174s). This validates retained metadata consistency; final source
object pin comparison, SQL locks and qualified scheduling remain unfinished.
# LRASPP exact source-object join (2026-10-07)

Added `verifyPersonSourceInputPins`, sharing the original SFD source-object join.
Every non-Params catalog entry must match the original source proof in artifact,
contract and full object metadata; missing source entries and changed versions,
keys or sizes fail before SQL. Params retain a separate document-authority gate.

Five source fixtures exercise exact joins and object substitution refusal. The
initial synthetic proof fixture incorrectly used catalog UUID order; it was
corrected to the existing source proof order (image, pixels, optional alpha,
geometry), without relaxing production validation. Final full tests passed:
`go test ./internal/semanticproposal ./internal/v2input -count=1` (1.207s and
6.134s). Actual source proof construction/ICC verification, qualification and
locked scheduling are still separate unfinished requirements for LRASPP.
# LRASPP source preparation composition (2026-10-07)

Added `prepareSourcePersonTask`: it composes existing physical RGB/alpha/geometry
and ICC verification in unit-interval mode, original WorkingImage reading,
accepted-version Params document catalog scope, exact catalog task preparation,
source authority recheck and full source object join. No HDR clipping or image
algorithm changes are introduced. Model/report qualification and locked SQL
admission remain separate mandatory unfinished steps.

Added stable parent-owned person preparation identity, independent of detector
stage and parent run; identity tests verify retries, parent separation and empty
parent refusal. Full Go packages passed: `go test ./internal/semanticproposal
./internal/v2input -count=1` (1.558s and 5.385s). These tests compile the source
composition and exercise identity/catalog boundaries, not a positive physical
source-person preparation or actual GPU/scheduler execution.
# Qualified LRASPP preparation composition (2026-10-07)

Added `prepareQualifiedCatalogPersonTask` and private person qualification proof.
It requires mask.person/v1/cuda, verifies the exact pinned trusted report through
the existing person qualification authority and scientific verifier, checks
provided costs against measured report peaks, rechecks report objects around
catalog IO and retains a canonical DraftTask fingerprint with full scope/report.
Source-person preparation now invokes this qualified path, owns expected values
before callbacks and retains the proof alongside original source objects.

Tests verify wrong deployment operation/version/backend and cancellation refuse
before IO; existing catalog/source boundaries remain covered. Full Go packages
were run successfully. This composes report verification code but does not prove
a positive physical LRASPP preparation, genuine installed GPU qualification,
final SQL report locks or scheduling. Existing image algorithms unchanged.
# LRASPP retained report transaction boundary (2026-10-07)

Added exact person qualification proof serialization, authority recheck, task
fingerprint binding and scheduler-only lock through the existing
lock_semantic_proposal_person_qualification SQL function. Scope, deployment and
immutable report metadata are checked before issuing SQL. Source-person
preparation verifies its task fingerprint and rechecks the report after source
object comparison before returning the retained proof.

Unit tests assert every SQL lock argument, reject the API role and a refused
lock, and cover scope/deployment/object substitution. Full Go package tests
passed (semanticproposal 1.644s; v2input 7.066s) before the final source composition
call; semanticproposal was rerun after that addition. These are simulated
transaction boundary tests, not physical native LRASPP admission or execution.
# LRASPP source/report/Params lock composition (2026-10-07)

Added `lockSourcePersonPreparation` and exact Params pin/lock helpers. Before
SQL it checks stable parent-owned task identity, source unit-RGB/ICC binding,
task qualification fingerprint, complete source objects, report scope and own
Params metadata. It composes existing source locks, person qualification locks
and accepted-version Params document ownership lock in the caller transaction.
Every failure requires rollback; parent snapshot/policy barriers, registration
and activation remain separate unfinished operations.

Five catalog fixtures verify exact Params pin generation and missing document
refusal. Full Go tests passed: `go test ./internal/semanticproposal
./internal/v2input -count=1` (1.317s and 6.164s). This confirms helper compilation
and simulated boundaries, not physical positive whole LRASPP admission. No new
SQL permissions or processing algorithm changes were introduced.
# LRASPP parent-owned draft plan and transport (2026-10-07)

Added person draft-plan compilation, complete private plan fingerprint and
transport publication composition. The plan binds original request/idempotency,
published intent, target inventory/snapshot, source, report, Params, selected
person qualification policy and output reservation. It creates a stable blocked
child shell with v27_person_prepare config and one mask.person GPU task. No SQL
admission or task activation is performed by these helpers.

Fingerprint mutation tests cover quota, request key, parent, Params document,
catalog/source objects, policy and task qualification, plus cancellation. Full
Go package tests passed. These tests prove fingerprint ownership and compilation,
not positive whole-plan assembly, physical draft admission or publication after
a committed shell. SQL replay support and orchestration remain unfinished.
# LRASPP blocked draft transaction and replay migration (2026-10-07)

Added forward-only migration 00744: native v27_person_prepare registered-shell
replay verifies stable parent-derived child run, one mask.person/v1/gpu task,
exact Params/digest/profile and ready closure before using the original exact
shell matcher. Other workflow/config replay delegates to the prior wrapper.
Mixed person/detector/defect preparation configs fail. No new public permissions.

Added `beginPersonDraftInTransaction`: recompile owned plan/config/task, compare
complete shell/key, lock parent inventory/intent/snapshot, exact original source,
person report and Params, then qualification policy snapshot and bounded quota
admission. Only created/pending matching child outcomes are accepted. Caller
rolls back any failure; this does not register inputs, seal or activate a run.

Go package verification was run. Physical manual-lock-go-disjoint-external
regression through 00744 was started and is pending at this checkpoint. It tests
migration installation and the existing defect/transfer CPU regression, not
positive native LRASPP draft or GPU scheduling. Freeze migrations and physical
fixtures until this active run ends.
# Complete synthetic LRASPP plan assembly (2026-10-07)

Added full synthetic person-plan fixtures for all five WorkingImage variants.
They construct parent request/intent, exact target snapshot and inventory,
parent-derived child task, selected cuda person policy, full source object pins,
qualification task fingerprint and retained report metadata. Compilation then
builds the blocked child shell and publishes its exact input transport in memory.
Child identity and registered packet counts are asserted.

`go test ./internal/semanticproposal -count=1` passed (1.335s). These are positive
whole-plan compilation/publication tests with synthetic qualification metadata;
they do not call actual SQL draft admission or run GPU inference. Physical
regression through 00744 remains active at this checkpoint; migrations/physical
fixtures remain frozen until it finishes.
# Native plan scope fingerprint correction (2026-10-07)

The person and detector plan fingerprints now include full private source and
qualification scope: tenant/project/actor/session/bucket, source binding, ICC,
unit-RGB flag and report contract/backend. Previously the fingerprint retained
object pins but omitted these companion private fields; final admission rechecked
scope, but publication integrity must also reject their mutation.

Positive person fixtures now assert publication refuses mutated source or report
session, ICC, unit-RGB flag, report backend and source binding. Full Go package
tests were run successfully after the correction. The live physical migration
regression remains pending and its already compiled test binary does not include
this later Go source change. No migration or physical fixture was changed.
# LRASPP registration composition and regression restart (2026-10-07)

Added exact frozen transport registration composition and forward-only migration
00745 providing person preparation registration-state checks. The gate locks
blocked draft run/task, checks parent identity and exact task/Params/profile,
complete ordered input/transport rows and ready task-owned manifest/chunks.
It returns missing/replayed/conflict, never activates or repairs partial state.
Go tests assert all 13 gate arguments and wrong task/cancellation refusals.

The previous physical regression failed after 218.99s at a later Go compilation:
a newly edited person fixture lacked encoding/json. This was corrected and Go
tests passed. The physical fixture compiles more than one test binary, so freezing
only physical fixture files was insufficient. The replacement physical run
through 00745 is pending. Freeze the entire semanticproposal package, physical
fixtures and migrations while it is live. No positive native LRASPP SQL
registration or GPU execution is claimed by the regression.
# Preserved Python mask/adapter regression verification (2026-10-07)

While the physical migration regression is live, backend packages, migrations
and physical fixtures remain frozen. Ran existing Python adapter/numeric tests:
test_mask_png, test_face_parse, test_face_landmarks_adapter, test_mask_refine,
test_face_skin: 14 passed in 1.14s. A harmless pytest cache write warning occurred
on the first run; test execution succeeded. Person mask adapter/config/measurement
tests: 16 passed in 3.44s, including runtime identity changes before/during
inference, strict outputs, softmax/refinement and publication boundaries.

These tests use simulated engines where applicable and do not qualify installed
weights, real GPU inference or image quality on RTX3060. No image algorithms
were changed. The active physical run through 00745 remains pending; its result
and positive native SQL registration still must be verified separately.
# Broad semantic worker regression (2026-10-07)

Ran `env PYTHONPATH=workers:src:tests .venv-platform/bin/python -m pytest
tests/platform -k semantic -q --tb=short -p no:cacheprovider`: 403 passed,
1180 deselected in 12.96s. This covers the current semantic worker contracts,
authority/adoption/replay boundaries, qualification, resource and transfer tests.
These unit/platform tests do not substitute for native SQL admission, installed
GPU qualification, real weights or end-to-end public API execution.

Current source inspection still shows SemanticPropose as an HTTP dependency but
no wiring in backend/cmd/photo/main.go. Connecting it remains mandatory for V27
completion. The physical migration regression through 00745 is still active;
backend source/migrations/physical fixtures were not changed during this run.
# Full backend compatibility verification (2026-10-07)

`go test ./... -count=1` passed across the complete backend after the native
SFD/LRASPP/FAN2D/BiSeNet/skin preparation changes. This includes cmd/photo,
HTTP API, semanticproposal/review, v2draft/input/workflow/render, queue,
tenant/auth and other backend package unit tests. Environment-gated physical
tests are not implied by this command; the separate physical regression through
00745 remains active and must finish independently.

Current inspection confirms no production implementation of the public Propose
method in semanticproposal and no SemanticPropose wiring in main. Completing
native prerequisites, durable parent-run orchestration and public admission is
mandatory; the passing backend suite does not establish overall V27 completion.
# Physical migration regression through 00745 completed (2026-10-07)

The exact live replacement manual-lock-go-disjoint-external pytest run completed:
1 passed, 13 deselected in 378.73s. Migrations through 00745 install successfully
and the existing manual defect/transfer/native CPU and saved-config regression
passes. The backend package, migration chain and physical fixture were frozen
during this run, including its later Go binary compilation.

This does not exercise positive native SFD or LRASPP registration/replay gates,
GPU inference or public orchestration. Those require dedicated runtime fixtures.
The physical run is terminal; the development freeze can now be lifted.
# Native registration SQL role coverage added (2026-10-07)

Extended the physical execution-gate ACL matrix with detector and person native
registration-state functions. Eight non-scheduler roles (API, worker, publisher,
portal, auth, gateway, maintenance and semantic admission) must receive SQLSTATE
42501, not a semantic conflict or an allowed call. Go package compilation/unit
tests were run. The isolated physical regression was started and is pending.

This adds actual SQL permission coverage, not positive native draft registration
or activation. Freeze the backend package, migrations and physical fixture while
the new live regression performs multiple Go compilations.
# Dedicated positive native registration gate fixture started (2026-10-07)

Added separate Python integration fixture for SFD and LRASPP SQL registration
state: owner-seeded blocked native child shell, real registered input rows and
ready task-owned transport documents, then missing -> replayed assertions and
changed/missing entry refusal. It asserts the run remains unsealed/draft. This
is metadata-only gate testing, not complete source/model/scientific admission or
GPU execution; the seeded artifact payloads are intentionally not inference data.

The dedicated isolated database run passed: 2 passed in 10.36s. This proves the
two real SQL gates accept exact completed registration and reject changed rows.
The existing native SQL ACL regression remains pending. Backend package,
migrations and the long-running fixture remain unchanged/frozen.
# Positive registered-shell replay SQL verification (2026-10-07)

Extended the dedicated SFD/LRASPP metadata-only fixture with a quota reservation
and original pre-registration draft profile. After actual input registration,
the real v2_pending_shell_matches accepts the exact original shell despite the
stored manifest-enriched profile. It rejects changed digest, Params SHA,
operation, resource class, backend profile, quota bytes and parent config.

Dedicated physical tests passed: 2 passed in 14.49s. This verifies migrations
00742/00744 registered-shell replay through the real SQL chain, alongside
00743/00745 registration gates. It does not authorize a production draft or
activate a task; source bytes, qualification and public orchestration remain
separate. The existing long native ACL regression is still active.


## Native registration isolation and activation evidence (2026-10-07)

The dedicated PostgreSQL registration-state test passed both SFD and LRASPP cases
(2 passed, 5.70s). It verifies missing/exact replay, registered-shell matching,
foreign tenant/task rejection, app.tenant mismatch, and preservation of the five
registered input rows and transport profile after refusals. The run stays draft
and unsealed. These are synthetic metadata fixtures, not actual inference.

The physical manual-lock-go-disjoint-external regression passed
(1 passed, 13 deselected, 352.94s), through migration 00745. Its expanded ACL
matrix confirms the native detector/person registration functions reject eight
non-scheduler roles. This result does not prove fresh native GPU admission.

Added marshalDetectorRegistrationBinding and marshalPersonRegistrationBinding:
verify the private plan seal, bind the exact frozen published transport, require
the matching GPU operation/version and preserve Params/profile/input digest and
all registration rows/chunks/manifest/direct roots in canonical bounded JSON.
The packet confers no activation authority. Positive original-source fixtures
check the complete packet and reject wrong operation, changed transport count
and cancellation. semanticproposal Go tests passed (1.320s).

Still required: dedicated native SQL authority validation and atomic activation,
real native admission/producer proofs, durable parent orchestration and public
service wiring, and RTX3060 model/resource/quality acceptance. V27 is incomplete.
The overall 50–60% code-volume estimate is approximate, not release readiness or
a measured fraction of accepted requirements.


## Native admission evidence and strict SQL registration envelope (2026-10-07)

`marshalDetectorAdmissionEvidence` and `marshalPersonAdmissionEvidence` now
recompile their owned plans, match the complete fingerprint, join exact source
objects to the frozen input inventory, select the original operation policy,
validate retained qualification/Params pins and bind the published transport.
The canonical schema-1 packet retains tenant/project/actor/session, child and
parent run, request SHA, original config/policy text, intent/snapshot text,
source binding, ICC SHA, unit-RGB flag, source pins, expected qualification,
report pin, Params document/pin, file closure and registration binding.
Packets are limited to32MiB; cancellation propagates before/after assembly.
A syntactically valid ICC digest is mandatory, but this packet itself does not
verify deployed ICC bytes: the original physical source preparation still owns
that check. No graph is activated by packet construction.

Positive original-source fixtures passed for SFD/LRASPP, including alpha and
dense geometry. Tests additionally reseal invalid plans and prove independent
refusal of foreign source tenant/project/actor/session/bucket, malformed ICC
and missing unit-RGB proof. `go test ./internal/semanticproposal -count=1`
passed (1.479s). Fixtures use synthetic qualification metadata; they do not
prove genuine scientific qualification or actual model inference.

Forward-only migration00746 adds scheduler-only
`semantic_native_registration_binding_state(tenant,run,operation,binding)`.
It accepts only face.detect/mask.person, exact schema-1 packet fields, bounded
integer count, valid nonzero UUID/hash refs, allowed source input contracts,
unique row/direct artifact IDs, and exact manifest/chunk/direct shapes. It
then delegates to the matching operation's registered-state gate. It never
activates a task or repairs partial inputs.

Dedicated full-chain PostgreSQL tests passed both cases (2 passed,5.40s):
missing/exact replay, wrong-operation rejection, malformed envelope refusals,
overflow, duplicate rows/direct refs and ACL rejection for eight non-scheduler
roles. Final registered rows/profile remain unchanged and runs unsealed draft.
The first attempt failed in the test harness when an expected ACL error was
swallowed before its savepoint could roll back; moving the exception assertion
outside the savepoint fixed the harness. No production migration was applied.

Remaining: dedicated SQL source/policy/anchor/content cross-binding and atomic
native seal, native model admission/producer proofs, durable parent orchestration,
public API wiring and RTX3060 acceptance. V27 and the full goal remain incomplete.


## Native source SQL cross-binding (2026-10-07)

Forward-only migration00747 adds two scheduler-only boundaries:
`semantic_native_file_closure_matches` verifies that each retained source object
and the separately pinned Params object match the complete file inventory and
registered rows. It preserves original WorkingImage parent relationships and
maps WorkingPixels/WorkingAlpha/DenseWarpField to the worker NpyArray contract.
Duplicate file roots, changed object versions/sizes, omissions, unsupported
contracts and substituted registration rows are rejected. This metadata match
is not a source-content validator or admission authority.

`lock_semantic_native_source_authority` then requires exact tenant/project/actor/
session, unit-RGB proof and syntactically valid ICC digest, SFD or LRASPP CUDA
operation/version, current full qualification-policy snapshot, independently
trusted imported qualification report, original source evidence scope and
accepted-version Params document ownership. It locks the retained metadata in
the caller's transaction. It does not validate actual deployed ICC bytes by
itself, lock the full parent intent/snapshot anchor, reserve quota, validate the
whole graph or activate any task. Those remain composed admission obligations.

Go wrappers lockDetectorAdmissionSourceInTransaction and
lockPersonAdmissionSourceInTransaction construct the complete original packet
before SQL, require photo_scheduler and preserve SQL errors/false-authority
conflicts/cancellation. Caller owns tenant setting, transaction and rollback.
No production orchestrator invokes these helpers yet.

Dedicated PostgreSQL tests through00747 passed both native cases
(2 passed,4.44s). They prove positive exact metadata closure, order-independent
file matching, mutated version/size/parent/inventory/row rejection and ACL denial
for eight non-scheduler roles on both new functions. Fixtures are metadata-only;
the complete trusted-report/source-authority positive path remains unverified.
No production migration was applied. Atomically sealing native GPU preparation,
parent orchestration and public-service wiring remain outstanding.


## Positive native source authority and concurrent revocation (2026-10-07)

Added `tests/integration/test_v27_native_source_authority.py` with accepted
source-version metadata, producer-bound WorkingPixels, ready Params document,
a live editor session and a qualification imported through the actual
photo_maintenance import_v2_qualification boundary. The imported report is a
synthetic test report, not measured scientific/model evidence.

Both face.detect and mask.person now exercise the positive composed
lock_semantic_native_source_authority path under photo_scheduler on a physical
isolated PostgreSQL database through migration00747. Independent component
assertions cover exact file cross-binding, current full policy snapshot,
trusted qualification, original source scope and accepted-version Params.
Changed tenant/session/ICC/unit-RGB/backend/object version/report/document/files/
policy are refused; disabling admission policy prevents subsequent admission.

Separate committed fixtures and two real database connections prove transaction
locks block concurrent updates to admission policy, model qualification status,
report verification, source RGB object version, Params document state and editor
session revocation (250ms lock timeout). After rollback, policy disable commits
and the next source-authority attempt returns false. Runs remain unsealed draft.
All four cases passed (4 passed,12.96s). No production migration was applied.

The first fixtures correctly failed because the report pin had an extra media
field and the reused layer Params artifact lacked output_role=document. The
fixture now uses the exact native pin/document contracts; runtime validation was
not weakened. No image-processing algorithm was changed.

This closes the previously unverified positive *SQL source-authority* branch.
It does not prove Go physical source-byte/ICC verification joined to this packet,
full native graph admission/activation, real GPU inference/quality/budgets,
parent orchestration or public API wiring. Dedicated anchor/graph/atomic seal
contracts remain required before marking V27 complete.


## Native graph and quota barrier (2026-10-07)

Forward-only migration00748 adds scheduler-only
`lock_semantic_native_graph_budget(tenant,run,evidence)`. It locks active tenant,
usage, draft run, semantic v2.2 workflow, every task and the run reservation.
It requires exactly one blocked unattempted uncancelled native SFD/LRASPP GPU
CUDA task, no edges/expansions, ready input closure, existing registry backend
and exact established output-role policies. Registry v2_enabled/qualification
activation remain separate obligations and are not enabled by this migration.
Output budget must be a positive safe integer; exactly one live user/run quota
with files=0 must cover output+8MiB and fit held tenant reserved_bytes.
The function does not activate tasks, seal runs or emit events/receipts.

Dedicated PostgreSQL tests through00748 passed both native operations
(2 passed,6.69s). Positive graph/quota matches refuse wrong operation/actor,
one-byte-insufficient quota, nonzero quota files, cancellation of run/task,
expired reservation and insufficient tenant usage. Restoring each isolated
fixture restores success. Eight non-scheduler roles are denied execution.
Registered rows and original replay shell remain intact; runs remain draft.
Fixtures contain synthetic metadata, not GPU execution or source-byte evidence.

Remaining composed native admission obligations include exact parent request/
intent/snapshot anchor, full config/policy/source/registration cross-binding,
atomic seal/activation/receipt/outbox, physical Go source preparation and trusted
report handoff, durable parent orchestration and public-service wiring.
No production migration or image-processing algorithm was changed.


## Native intent/snapshot anchor boundary (2026-10-07)

Forward-only migration00749 adds scheduler-only
`lock_semantic_native_admission_anchor(tenant,run,evidence)`. It selects the
operation-specific detector/person config, verifies tenant/run/schema, stable
child UUID derived from original parent, project/session/request/source binding,
original intent SHA, snapshot SHA and canonical request SHA. It locks active
tenant/usage, compares the complete live target inventory against snapshot items,
then locks the original parent-owned intent and snapshot documents plus recipe
heads through the established proposal boundaries. It does not activate tasks.
The native boundary does not impose the CPU-defect helper's preserve_persons=true
restriction; required native-stage selection remains the orchestrator's duty.

The isolated PostgreSQL chain through00749 accepted the function and two existing
native registration/graph regression cases passed (2 passed,6.76s), including
empty-packet refusal and denied execution for eight non-scheduler roles.
That run preceded removal of the inherited preserve-persons restriction; final
migration verification follows separately. Positive full native anchor with
published intent/snapshot/recipe fixtures is still unverified. Atomic composed
native seal and production orchestration/public service remain incomplete.


## Positive native anchor and composed evidence boundary (2026-10-07)

`test_v27_native_anchor.py` now verifies both detector/person anchors against
original parent-owned ready intent/snapshot metadata, a complete target snapshot,
ready user-mask binding and current EditingRecipeV3 stream/document metadata.
Exact canonical request/intent/snapshot hashes pass. Changed child, parent,
request SHA, session/project, snapshot ordinal/text and missing recipe head fail.
The fixture permits preserve_persons=false; stage necessity is selected elsewhere.
It does not publish actual object bytes or prove recipe/image/model quality.

Forward-only migration00750 adds scheduler-only
`lock_semantic_native_admission_evidence(tenant,run,evidence)`. It bounds and
validates the packet, composes the complete native anchor barrier, locks the
actual draft child run/workflow, checks original config/policy byte hashes,
rejects mixed/extra preparation config fields and cross-binds project/session/
parent/request/source/Params document/Params ref/task/input digest/policy.
It then composes retained source/qualification locks, exact registered-transport
replay and native graph/quota barrier. It performs no sealing/activation/events.

The isolated PostgreSQL suite through00750 passed8 cases (23.72s):2 positive
anchor cases,4 source-authority/revocation cases and2 registration/graph/ACL cases.
The new composed boundary itself is verified only for creation, empty-packet
refusal and eight non-scheduler role denials. A single positive complete packet
joining all these fixtures to the actual registered child remains outstanding;
passing individual components is not proof of whole native admission.
No production migration or image-processing algorithm was changed.

Remaining: positive composed native admission (including physical Go source/ICC
proof handoff), atomic native seal/activation/receipt/outbox, durable public parent
orchestration, production wiring and genuine RTX3060 scientific/resource gates.
The full V20–V32 goal remains active and incomplete.


## Positive composed native admission SQL (2026-10-07)

Added test_v27_native_admission.py, joining original parent-owned intent/snapshot
and current recipe-head metadata, real imported fixture qualification, accepted
source/producer-bound RGB, owned Params document, stable child/workflow/task UUIDs,
original config/policy hashes, real reserve/complete input document calls, real
input batch registration/sealing and live output+transport run quota.

Both SFD and LRASPP now pass the complete
lock_semantic_native_admission_evidence path through00750 on isolated PostgreSQL.
Extra packet fields, changed Params document/input digest, changed source/report
versions and substituted resource profile are refused; the original packet still
passes after refusals. Final run/task states remain draft/unsealed/blocked: this
function does not activate. The first run correctly returned quota because the
small shared test tenant's storage limit was below its synthetic16MiB reservation;
the isolated fixture now explicitly provisions1GiB storage, without weakening
runtime quota enforcement.2 cases passed (9.63s).

Refactored the existing anchor fixture into seed_native_anchor. Follow-up standalone
anchor plus composed admission regressions passed4 cases (14.24s).
This proves composed physical SQL metadata gates, not actual object-byte/ICC
verification, valid native inference Params, scientific qualification or GPU
execution. Synthetic recipe/Params/report metadata is intentional and must not
be used to infer renderer/model correctness. No production migration or image
processing algorithm was changed.

Next required work: dedicated atomic native seal/activation/idempotency receipt/
event outbox and its replay/rollback tests; physical Go preparation joining genuine
source bytes/ICC/report to this complete SQL boundary; parent orchestration and
public API wiring; actual RTX3060 model/quality/resource acceptance. Full goal
and V27 remain incomplete.


## Native atomic seal and exact receipt replay (2026-10-07)

Forward-only migration00751 adds seal_semantic_native_preparation plus separate
completed detector/person registration-state, strict completed binding and
completed evidence validators. First admission locks the complete native
anchor/source/qualification/registered-input/graph/quota authority, requires
explicit existing v2 registry enablement for CUDA, rejects consent links lacking
a native consent contract, and atomically seals the run, activates the bound
task, publishes principal queued event/outbox and stores the V2.Admit202 ticket.
The stable keys remain detector-prep/person-prep:<child UUID>.

Sealed receipt replay rechecks live original anchor/source/qualification,
config/policy hashes and exact sealed input/profile/transport closure with one
task and no edges/expansions. It does not require a still-live output reservation
or reset execution state, and never activates or emits another event. Changed
retained input digest fails replay. Functions are scheduler-only; production
registry v2 enablement is not changed by the migration.

Dedicated SFD/LRASPP SQL cases through00751 passed (2 passed,4.67s), including
positive complete admission, queued run/ready task, exact202 receipt, no duplicate
queued event/outbox on retry, malformed input refusal and eight-role ACL denial.
A forced event-cursor exhaustion after task activation raises23514; a savepoint
rollback restores draft/unsealed run, blocked task, absent receipt and no run
outbox, proving the write set is transactional. Restoring cursor permits normal
activation. Earlier initial seal/replay cases passed2 in6.38s.

These are physical isolated PostgreSQL metadata fixtures with synthetic imported
reports and opaque Params/recipe bodies. They do not prove genuine native adapter
Params, object-byte/ICC/scientific qualification, GPU inference or queue delivery.
No production migration/registry or image-processing algorithm was changed.

Next: compose private Go frozen-plan/published-transport evidence with this seal,
physical original-byte/report handoff tests, durable parent native-stage
orchestration and public-service wiring, cancellation/recovery/unknown-commit
acceptance, and real RTX3060 model/quality/resource gates. V27/full goal remain
incomplete; SQL atomic admission is now positively verified within this scope.


## Go native atomic seal handoff (2026-10-07)

Added sealDetectorPreparationInTransaction and sealPersonPreparationInTransaction.
Each reconstructs the complete canonical evidence from its privately frozen
original plan and exact published transport before SQL. Shared sealing requires
photo_scheduler, sets transaction-local tenant context and invokes the dedicated
seal_semantic_native_preparation boundary. It accepts only a bounded queued
JobTicket matching the retained child run/workflow. NULL becomes conflict;
malformed/foreign/wrong-state receipts fail; SQL/cancellation errors propagate.
Caller owns commit/rollback and must complete immutable publication outside TX.
No independently read old receipt bypasses completed evidence replay validation.

Positive original-source plan fixtures exercise both typed methods, capture exact
SQL packet/scope and verify absent/malformed/foreign/wrong-state receipts, foreign
role, tenant-setting/seal SQL errors, cancellation and nil transaction refusal.
`go test ./internal/semanticproposal -count=1` passed (1.683s).
These handoff tests use a fake transaction. The physical SQL path is separately
covered by00751 tests; the complete Go+SQL+actual source-byte/ICC/objectstore
preparation path remains unverified. Broad backend regression is pending below.

Production public native-stage orchestration is not wired by these helpers.
Parent-ticket durability, scheduling dependencies and restart/unknown-commit/
cancellation paths remain required. No processing algorithm was changed;
full V20–V32 goal remains active and incomplete.


## Native preparation transaction/publication coordinator (2026-10-07)

Added persistDetectorPreparationInputs and persistPersonPreparationInputs using
the existing scheduler database.InTransaction boundary. Each persists its fully
locked blocked draft, commits, publishes the privately frozen exact transport
outside SQL, then re-locks authority and registers that transport in a second
transaction. Failure leaves deterministic recoverable draft/document slots and
returns no usable PreparedSemanticTask. Cancellation is checked between phases.
No SQL callback spans object-store publication.

Added sealRegisteredDetectorPreparation and sealRegisteredPersonPreparation:
invoke dedicated typed sealing through nativeTicketWithRetry, which retries only
SQL and returns no usable ticket after callback/commit failure. Completed replay
still uses dedicated live-evidence validation, not an old-ticket shortcut.
These helpers are not yet wired to the public parent workflow orchestrator.

Coordinator tests passed in `go test ./internal/semanticproposal -count=1`
(1.719s). Tests prove publication is outside an active transaction and follows
successful draft commit; failed draft/commit prevents publication, failed
publication prevents registration, registration/commit errors return no usable
transport, and cancellation after draft stops publication. Harness transactions
are synthetic; actual full Go+PostgreSQL+objectstore qualification/source/ICC
handoff remains outstanding. Previously recorded physical SQL seal tests retain
their separate scope. No processing algorithm or production configuration changed.

Remaining: complete native source Params publication/planning factory, physical
Go handoff acceptance, durable parent orchestrator/public service and downstream
FAN/BiSeNet/skin stage scheduling, recovery/cancellation/unknown-commit acceptance,
real RTX3060 scientific/quality/resource gates and remaining V20–V32 scope.
The original goal remains active and incomplete.


## Source-verified native Params publication factories (2026-10-07)

Added v2draft.Publisher.PublishSemanticPersonParams, deriving canonical independent
LRASPP Params and complete source inputs through PrepareSemanticPersonParams.
It uses the established accepted-version document quota/fencing/storage lifecycle,
validates identity/document and refuses document/source input aliasing. Existing
SFD publication uses the same lifecycle. No task authority is granted by upload.

Added publishSourceDetectorVersionParams and publishSourcePersonVersionParams.
They first verify actual original RGB/alpha/geometry and deployed ICC with unit
RGB semantics, read the exact original WorkingImage, derive canonical native
Params, publish their version-scoped document and verify returned Params SHA.
They recheck the retained source object/version/identity proof after publication.
No SQL transaction spans object IO. Model report qualification, private frozen
resource/task compilation, locked admission and queue activation remain later
separate obligations. A failed post-publication recheck never returns a usable
ref; the published immutable document remains under the existing cleanup policy.
No source clipping or processing algorithm change was introduced.

`go test ./internal/v2draft ./internal/v2input ./internal/semanticproposal -count=1`
passed (3.104s/7.324s/2.407s). These package regressions prove compilation and
covered pre-existing canonical task/source behavior; they do not prove the new
factory's positive physical SQL+objectstore+source-byte path. That remains required
before declaring complete native preparation, along with original trusted-report
handoff, durable parent orchestration/public service and real hardware acceptance.
The full goal remains active and incomplete.


## Native publication configuration preflight (2026-10-07)

Native source Params publication now validates identity/version/source/document
UUIDs and known source-document aliasing before source reads. SFD validates the
model digest, finite score in[0.5,0.99] and max_faces1..100; independent LRASPP
validates model digest and guided tile256/512/1024 before any physical source IO.
In particular NaN/Infinity cannot trigger expensive source reads before failing
canonical Params serialization. Valid adapter settings and processing are unchanged.

New preflight tests use nonnil poison SQL/object dependencies, so accidentally
reaching source IO fails rather than being hidden by nil-reader validation.
Malformed SFD scores, unsupported guided tiles and cancellation refuse before IO.
The first targeted semanticproposal run passed1.699s; final poison-dependency
verification result is recorded below. Positive actual-byte publication/factory
handoff remains outstanding; this change does not prove native end-to-end readiness.
The original full goal remains active and incomplete.


## Composed native planning factories (2026-10-07)

Added prepareDetectorDraftPlan and preparePersonDraftPlan. Both reconstruct and
own CompileRequest from the original tenant/actor/key/body, require its exact
run/workflow/request SHA and current actor/session, read the server qualification
policy snapshot, select the required native operation and refuse non-CUDA policy.
Params document IDs are UUIDv5(parent,semantic.<operation>.params:reference).
Factories publish source-verified canonical Params, build qualified original
source/frozen input/task/resource evidence with the selected model/report and
explicit budget, reread the complete original policy snapshot and compile the
owned parent intent/snapshot anchor plus blocked draft plan. No admission/queue
activation occurs here. Stable publication slots reject changed bytes on retry.

The separate mutable Request.Intent field is never trusted instead of original
body; foreign run/workflow/request SHA, actor/session or key is rejected before
policy/source publication. Tests prove owned canonical request/body and refusal
of substituted identity, plus cancellation. semanticproposal passed
`go test ./internal/semanticproposal -count=1` (1.606s; earlier compile regression
1.605s). These tests cover request ownership and package regression, not the new
factory's positive real SQL/objectstore/source/ICC/report handoff.

Remaining required verification/implementation: that full physical factory path,
durable parent native-stage orchestration/public service, FAN/BiSeNet/skin native
admission and dependencies, recovery/cancellation/unknown-commit acceptance and
actual RTX3060 model quality/resource qualification. Existing image processing
is unchanged; full V20–V32 goal remains active and incomplete.


## Native registered transport recovery (2026-10-07)

Forward-only migration00752 adds scheduler-only
semantic_native_registered_transport(tenant,run,task,actor,session). It locks
run/task/workflow, requires the original actor and current editor session, builds
the exact stored Params/profile/rows/direct/chunks/manifest binding and validates
it through the appropriate registered or completed native binding gate. Only
exact replay returns bounded metadata schema/task/manifest/chunks/count/entries SHA.
No object keys/credentials, repair, publication, activation or ticket are returned.

This recovery closes a required building block for unknown-commit/restart handling:
a caller must not blindly repeat draft creation after a potentially committed seal.
The retained transport can instead be rebound to the private frozen plan, followed
by dedicated sealing's complete current evidence/receipt replay checks. The Go
recovery decoder/coordinator branch is still outstanding; this migration alone
is not full orchestrator recovery.

Dedicated composed SFD/LRASPP SQL cases through00752 passed2 in5.28s: exact
transport returned before and after sealing, foreign task/actor refusal, eight-role
ACL denial and existing seal/replay/rollback assertions. The first run failed on
an ambiguous state variable/table-column reference; the new migration query now
explicitly qualifies artifact columns. No production migration was applied.
Fixtures remain synthetic metadata, not physical source-byte/model qualification.
Public orchestration and whole V20–V32 acceptance remain incomplete.


### Native transport recovery in Go (2026-10-07)

Implemented scheduler-only transaction recovery against migration 00752 with explicit tenant/run/task/actor/session scope. SQL NULL is a conflict, never authorization to reconstruct an existing graph. JSON decoding is bounded to 1 MiB, rejects unknown fields/trailing data, checks schema/task identity, native count bounds, chunk cardinality, digests and distinct document references. Typed detector/person recovery verifies the private plan seal and recomputes the frozen input closure through BindPublishedTransport. Transaction helpers recover and invoke full dedicated admission sealing; callers must commit before exposing a ticket. These helpers do not yet wire the public parent orchestrator.

Validation: `go test ./internal/semanticproposal` PASS (3.784s). Tests cover role/scope propagation, malformed or substituted transport, SQL/scope failures, cancellation and zero results on failure; existing detector/person source fixtures exercise exact frozen recovery and rejection of altered entry digests. This is Go unit/fixture validation, not a new PostgreSQL/object-store integration or GPU qualification run. Existing physical migration-00752 evidence remains separate.

Overall code-volume estimate remains approximately 50–60%, not a measured requirement-acceptance percentage. Public semantic orchestration, complete native downstream stages, real model/RTX 3060 qualification, full end-to-end acceptance and remaining V20–V32 work are outstanding. The implementation goal remains active.


### Native preparation transactional resume (2026-10-07)

Added internal resumeDetectorPreparation/resumePersonPreparation coordinator boundaries. They validate the retained private plan, reread registered transport, bind it to the original frozen input closure and call dedicated full admission sealing under a single scheduler transaction. Only confirmed aborted SQL transactions are retried; every retry repeats recovery and admission. Unknown commit outcomes return no usable ticket and require a later explicit durable resume. No draft creation or object publication occurs in this path.

Validation: `go test ./internal/semanticproposal` PASS (4.712s). Detector/person source-plan fixtures now run combined recovery+sealing transaction checks: successful commit, serialization-aborted commit with complete revalidation, unknown commit with zero ticket/no blind retry, missing saved transport and substituted entry digest with rollback and no seal call. This evidence uses Go transaction fixtures; it does not prove a live PostgreSQL/object-store/GPU/public-API end-to-end flow. Public parent orchestration remains outstanding.


### Connected native-stage bootstrap (2026-10-07)

Added nativePreparationBootstrap for the initial face.detect and mask.person stages: server-selected current policy/source-byte/ICC and qualification checks → deterministic Params publication → frozen draft plan → committed blocked shell → transport publication outside transactions → committed registration → reread persisted transport and full atomic admission. Dependencies are validated before IO, including common object bucket. Child tickets remain internal and must not be returned as parent semantic API acceptance.

The bootstrap retains the exact private plan as an internal recovery checkpoint after successful preparation, including persistence/admission errors, but always returns a zero ticket on error or cancellation. A failed preparation returns no checkpoint. Recovery still requires the registered transport/full admission boundary; incomplete or conflicting state cannot authorize reconstruction. This is an in-process checkpoint, not durable parent-state persistence.

Validation: `go test ./internal/semanticproposal` PASS (2.114s). New sequencing tests cover failed preparation, persistence, admission and cancellation at each phase, checkpoint retention, no downstream calls after failure and no failed receipt exposure. Dependency/cancellation preflight is also checked. These tests do not prove positive real bootstrap IO or public-service integration. Dedicated live PostgreSQL + object-store acceptance and durable parent orchestration remain next work; real model/hardware evidence remains outstanding.


### Physical native bootstrap and policy serialization fix (2026-10-07)

Added an isolated PostgreSQL/MinIO native-bootstrap case and TestIntegrationNativePreparationBootstrap. The fixture stores actual WorkingImage, float32 RGB, alpha, manual-mask and recipe bytes with verified pinned metadata and imports explicitly synthetic qualification reports. It runs the connected source/ICC/Params/qualification/frozen-plan/bootstrap path for face.detect and mask.person, publishes real Params and input chunk/manifest objects, commits registration and full admission, then rereads transport and replays each identical ticket. The final test inspects both queued/sealed child runs, both ready tasks and exactly two committed 202 receipts, revokes the original session and proves both replay calls refuse with zero ticket. No model inference or GPU benchmark is executed; installed resource costs and report checks are fixture evidence only.

The first physical runs exposed two issues: the fixture budget incorrectly used zero qualified VRAM and too little RAM (corrected to a positive declared 128 MiB VRAM cost and 2 GiB RAM limit without weakening compilers), and real PostgreSQL jsonb policy inventory bytes were not canonical. qualificationPolicyInventory now retains JCS bytes after bounded JSON validation, aligning frozen policy text, hashes, initial admission and recovery. This fixes actual native admission rather than bypassing the canonical-evidence requirement. Factory/bootstrap errors now include fixed phase descriptions with wrapped causes and no scope/object secrets.

Verified results:
- Final native bootstrap + replay + committed-state checks + session revocation: 1 passed, 14 deselected, 14.15s.
- Existing physical transfer preparer fresh path regression: 1 passed, 14 deselected, 13.29s.
- Go semanticproposal regression suite: PASS, 1.705s (dedicated integration tests require their isolated environment and skip in ordinary unit runs).

Native stages are admitted, not executed in this evidence. Durable parent orchestration, FAN/BiSeNet/skin scheduling, public semantic API wiring, original model/hardware quality qualification and full V20–V32 acceptance remain incomplete. No production migration or registry enabling was performed; v2_enabled changes are restricted to the disposable test database.


### Parent-bound detector result discovery foundation (2026-10-07)

Forward-only migration 00753 adds photo.semantic_native_detector_result_catalog, executable only by photo_api. It scopes discovery to the authenticated tenant/project/actor/session, original parent/request SHA, deterministic native detector child and exact configured task/input digest. It requires a sealed uncancelled native CUDA run, succeeded canonical attempt/fence, one face_boxes output, exact Params pin, ready verified unexpired artifact and matching completed output-upload metadata. Existing face-preparation catalog joins additionally enforce accepted-version source and original finished attempt/object authority. Discovery is read-only preflight; it grants no downstream admission and does not replace byte/model/qualification verification or final locks.

Validation: isolated test_v27_native_admission.py through 00753 passed 2 cases in 6.29s, preserving detector/person composed sealing/recovery/rollback checks. Added API-only grant assertions across 8 roles and proved blocked/registered tasks cannot be read as completed detector results. This verifies migration creation, ACL and incomplete-result refusal, not the positive succeeded-output branch. A real completed SFD output/Params-content handoff, typed Go reader, downstream FAN/BiSeNet preparation and transactional parent orchestration remain outstanding. No production migrations were applied.


### Native model result ingestion and positive detector discovery (2026-10-07)

A positive succeeded-SFD metadata fixture exposed a real runtime gap: the generic V2 result trigger accepted only document roots and rejected face_boxes, so native detector outputs could not reach canonical result state. Forward-only migration 00754 adds the private semantic_native_preparation_result_ready predicate and dedicated trigger branches for parent-native face.detect/mask.person, retaining the existing generic and native-defect branches. The predicate binds native config/task/input digest, sealed running workflow, CUDA v2.2 profile and current attempt/fence; it checks exact ordered roles/media, unique output refs, ready verified unexpired artifacts and completed output uploads with identical task/attempt/fence/object metadata. SFD accepts one face_boxes JSON; LRASPP requires person_mask, person_mask_png, person_mask_ref, person_report. Service roles receive no direct EXECUTE grant on the predicate.

Physical test_v27_native_admission.py through 00754: 2 passed in 8.31s. Added positive canonical SFD result insertion and exact parent-bound catalog result/Params identity, seven scope substitutions, cancellation, finished-attempt backend mismatch, fencing mismatch and output verification/publication refusal. Added positive four-output LRASPP result insertion, reordered/partial/duplicate/extra output refusal through both predicate and actual insert trigger, per-output incomplete publication refusal and 8-role private predicate ACL. Fixtures use synthetic output metadata, not actual model-generated FaceBoxes/MaskRef/PersonReport bodies or inference quality evidence.

An existing long physical native defect→transfer→assembly regression was launched as exec session 92978; its result is pending and must be polled before reporting pass or starting a replacement. Typed Go detector-result reader/content verification and FAN/BiSeNet scheduling remain outstanding. No production migration was applied.


### Completed SFD content handoff in Go (2026-10-07)

The pending exec session 92978 completed successfully: existing physical manual-lock-go-external native defect→transfer→assembly regression, 1 passed / 14 deselected in 236.52s. This validates the previous result-guard extension against that existing physical contour; it does not prove GPU inference.

Implemented a parent-plan-bound nativeDetectorResultCatalog Go reader for migration 00753. It validates the private plan/scope, exact frozen Params, canonical result identity, bounded strict JSON refs, distinct source/output/Params and digest syntax, and maps missing results to conflict. readCompletedDetectorPreparation validates current policy and imported qualification, actual original unit-RGB/alpha/geometry/ICC source proof, pinned output and Params object bytes, FaceBoxes/Params linkage and CUDA/model binding. After object IO it repeats exact catalog/object/source/qualification/policy checks and returns no partial output on failure. This is preflight content evidence, not authorization to admit FAN/BiSeNet or advance a parent workflow.

Extended the isolated native-bootstrap test to publish real synthetic FaceBoxes bytes to MinIO, register corresponding completed output metadata and canonical result through the actual 00754 trigger, and verify full Go content handoff. Final physical result: 1 passed / 14 deselected in 11.65s. The first content fixture omitted the mandatory no_faces warning and was correctly rejected; the fixture was corrected without relaxing the parser. The imported qualification and generated FaceBoxes are explicitly synthetic; no SFD inference or scientific quality claim is made. The prior Go decoder/regression run passed in 1.821s; final `go test ./internal/semanticproposal` regression passed in 1.688s.

Downstream FAN/BiSeNet/skin task scheduling and final evidence locks, durable parent orchestration/public API, real model/hardware acceptance and remaining V20–V32 scope are still outstanding.


### Original-SFD-bound FAN/BiSeNet Params publication (2026-10-07)

Added accepted-version immutable PublishSemanticLandmarkParams and PublishSemanticFaceParseParams publishers, using existing quota/reservation/fencing/cleanup protocol. The compilers verify exact original WorkingImage/FaceBoxes/SFD Params bytes before any publication; documents cannot alias source inputs or SFD Params. The private completed detector result now retains verified source and detector Params bytes for downstream compilation.

Added publishNativeFaceStageParams for server-selected face.landmarks/face.parse CUDA policy models. It reads the exact completed parent-owned SFD, builds deterministic document IDs from the original parent and operation, publishes canonical downstream Params, checks returned reference/hash and rereads complete SFD/source/qualification/policy evidence after publication. Any changed producer, object, source or policy returns no usable downstream Params proof. This is input preparation, not FAN/BiSeNet scientific qualification, resource-budget admission or activation.

Validation: `go test ./internal/semanticproposal ./internal/v2draft` PASS (2.207s / 2.099s). Physical native-bootstrap PostgreSQL/MinIO case PASS: 1 passed / 14 deselected in 13.15s, now including both actual FAN and BiSeNet Params publication and identical-ref/document replay from the canonical synthetic SFD output. The disposable native case uses CUDA downstream qualification policies; existing non-native fixture policies remain unchanged. Reports and SFD output remain synthetic; no FAN/BiSeNet inference or GPU benchmark is executed.

Qualified downstream catalog/frozen task construction, full admission locks and scheduling, skin derivation, durable parent orchestration and public API wiring remain outstanding. Processing algorithms were not changed and no production migration was applied.


### Qualified catalog-backed FAN/BiSeNet frozen tasks (2026-10-07)

Implemented prepareNativeFaceTask for face.landmarks and face.parse. It derives stable child/workflow/task identities from the original parent and operation, validates deterministic Params ownership and current original SFD content, independently reads and verifies the imported operation-specific CUDA qualification report and installed measured-cost lower bounds, then compiles existing frozen task/profile implementations from exact catalog sizes. Params use the accepted-version document catalog; FaceBoxes use a scoped canonical SFD catalog adapter; original source arrays retain contract-scoped catalog authorization.

Every source evidence row must exactly match the byte-verified retained source proof, including ref/contract/object key/version/hash/bytes. Supplied source catalog substitution cannot reduce GPU accounting. Catalog evidence is reread after compilation; SFD/source/policy evidence is repeated, and downstream report authority is rechecked after all object IO. The returned private frozen task/report/input evidence is preparation only: it does not register tasks, reserve graph quota, lock admission authority or queue FAN/BiSeNet.

Validation: `go test ./internal/semanticproposal` PASS in 1.806s, including stable/distinct parent/operation identities, cancellation before dependency IO, and source size/version/contract/missing/duplicate/foreign substitution refusal. Final physical native-bootstrap PostgreSQL/MinIO case passed 1 / 14 deselected in 15.29s: both qualified frozen downstream tasks compiled with exact Params and stable task IDs; substituting LRASPP qualification for either operation failed with no frozen task. Reports remain synthetic imported test evidence; no FAN/BiSeNet inference or hardware qualification is claimed.

Next work remains downstream graph plans/registration/full admission locks and result handling, skin stage, durable parent orchestration/public API and real model/hardware acceptance. Entire V20–V32 goal remains incomplete; processing algorithms are preserved.


### Transactional original-SFD barrier for downstream admission (2026-10-07)

Forward-only migration 00755 adds scheduler-only lock_semantic_native_detector_result. It locks the authenticated active tenant/editor/session/project/accepted-version/source scope, exact deterministic parent detector run/config, task/workflow, attempts/results, original output/Params artifacts and upload records. After locks it reruns the parent-bound catalog and compares schema/result/output/Params plus exact sorted object pins. No queue/admission authority is granted by this component alone.

completedNativeDetector now retains verified original object metadata. The Go lockCompletedDetectorInTransaction wrapper validates the owned private plan/scope, exact frozen Params, original source/output/Params body digests, distinct object pins and original byte sizes, verifies scheduler role, sets tenant context and submits fixed bounded evidence. False SQL result is conflict; caller must rollback failures and commit only after full downstream admission.

Validation:
- Isolated native admission SQL tests through 00755: 2 passed in 7.00s; exact positive result/object lock, changed result/Params/ref/version/unknown-field refusal and scheduler-only ACL across 8 service roles.
- Go semanticproposal regression: PASS, 1.960s.
- Physical native-bootstrap PostgreSQL/MinIO + typed retained SFD lock: 1 passed / 14 deselected in 13.86s. Exact completed-byte evidence passed; substituted object version failed. While the scheduler transaction retained its locks, independent writer transactions attempting task cancellation, output completion invalidation and session revocation all hit PostgreSQL lock_timeout (55P03). Writers were rolled back; no destructive production operation occurred.

This is a final detector dependency barrier, not full FAN/BiSeNet registration/sealing or model inference. Downstream qualification/source/document/policy/anchor/graph/quota admission composition, native output handling, skin stage, durable parent orchestration/public API and full V20–V32 acceptance remain incomplete. All new migrations were applied only to disposable test databases.


### Owned blocked FAN/BiSeNet graph plans (2026-10-07)

Added nativeFaceDraftPlan and compiler/fingerprint validation. A plan owns the exact original detector request/snapshot/intent/policy anchor, independently scoped downstream qualification/report/metrics, canonical SFD result identity, deterministic Params document, retained input objects, concrete FAN/BiSeNet frozen task and positive output-plus-document storage reserve. Private task preparations now retain tenant/project/actor/session/parent/bucket/policy scope for replay/admission validation. Compiler rebuilds the parent plan, joins actual source evidence, verifies complete mapped frozen input inventory, rejects foreign concrete task type/identity/Params/qualification and derives stable single-task semantic child/workflow IDs. Config contains native operation and original SFD run/task/result/output/Params refs. Fingerprint covers owned shell, task and every retained evidence/scope field. Cancellation returns no usable plan.

These plans are blocked preparatory metadata only; no run/task registration, queue admission or child/parent acceptance ticket is created by compilation. SQL must independently validate the new v27_face_prepare config and compose existing source, qualification, detector-result, anchor, registration and quota locks.

Validation: final physical native-bootstrap PostgreSQL/MinIO case passed 1 / 14 deselected in 20.01s, including compilation/fingerprint checks for both operations, mutation of sealed storage budget refusal, tenant/project/actor/session/parent/bucket/policy substitution refusal and insufficient storage reservation refusal. Final `go test ./internal/semanticproposal` PASS in 1.698s. Qualification and detector content in this fixture are explicitly synthetic; no model inference/hardware qualification is claimed.

Downstream persistence/transport registration and full SQL admission/sealing, native result handling/skin, durable parent orchestration/public API and all remaining V20–V32 acceptance remain incomplete. Existing processing quality algorithms are unchanged.


### Native FAN/BiSeNet blocked persistence and physical transport (2026-10-07)

Added beginNativeFaceDraftInTransaction. It verifies the private sealed plan, recompiles the owned original parent and downstream shell, matches the exact retained completed SFD result, and preflights bounded original Params/report object pins before SQL. In the caller-owned scheduler transaction it locks original inventory/intent/snapshot, verified unit-RGB/alpha/geometry/ICC source plus original SFD qualification/Params, completed canonical SFD output/attempt/objects, independently imported downstream qualification, accepted-version downstream Params and full policy snapshot. It then uses the existing deterministic V2 blocked-draft admission. Only created/pending for the exact child run are accepted; caller must roll back every failure. No object IO occurs in the transaction. No task registration, sealing, activation or 202 child/parent ticket is returned.

The prior native transport/binding implementation was verified with a memory publisher (Go PASS 1.882s; physical upstream fixture 1 passed / 14 deselected in 15.47s). That earlier result established downstream typed publication logic, not actual downstream MinIO writes.

The fixture now actually persists both native downstream graphs in disposable PostgreSQL, repeats creation as pending, verifies unsealed draft state/no ready tasks/no committed 202 receipts, and rejects a changed SFD result. It then publishes actual FAN/BiSeNet chunks/manifest through the scoped artifactjson publisher into MinIO and verifies immutable replay and complete registration binding. Separate memory-publisher checks still exercise injected publication failure, transport count substitution and changed sealed-plan rejection before IO. Native operation registry enabling is restricted to the disposable test fixture; production capability configuration was not changed.

Validation: first physical blocked-persistence case passed 1 / 14 deselected in 17.88s (Go PASS 1.853s). Final physical PostgreSQL/MinIO case including real downstream transport and replay passed 1 / 14 deselected in 14.06s; final Go semanticproposal PASS 1.814s. No real GPU/model inference is exercised; original reports and SFD contents remain explicitly synthetic. No production migration/deployment occurred; image processing algorithms are unchanged.

Remaining: composed downstream SQL transport registration/admission/sealing/recovery, native results/skin, durable parent orchestration/public API and complete V20–V32 acceptance. Added completion-assessment.md documenting the approximate 50–60% implementation estimate and why it is not a measured acceptance/release percentage.


### FAN/BiSeNet registered input closure and coordinator (2026-10-07)

Forward-only 00756 adds dedicated scheduler-only semantic_face_preparation_registration_state and semantic_face_registration_binding_state, without widening SFD/LRASPP registration/admission allowlists. The face state locks exact child run/task, requires an unsealed uncanceled draft/blocked attempt-zero CUDA task, stable parent-owned face child ID and exact operation/config/Params/digest. It compares complete normal input rows, transport rows/contracts/order, task direct inputs, manifest/profile/count/digest and run/task-owned ready backend documents. Exactly one Params/WorkingImage/FaceBoxes row must match the retained config refs. Strict schema/ref/contract/count/unique validation bounds the envelope. Missing means wholly unregistered only; partial, substituted or mismatched state is conflict. Neither function activates or repairs tasks.

A face-only pending-shell replay wrapper preserves the original exact shell matcher, allowing the same blocked shell to be reopened after successful immutable registration only when full closure is ready. Other semantic/native workflows retain the previous wrapper chain.

Added registerNativeFaceInputsInTransaction: bind concrete frozen transport and complete evidence before SQL; repeat full original inventory/intent/snapshot/source/SFD qualification/Params/completed detector locks, downstream qualification/accepted-version Params/policy locks through beginNativeFaceDraftInTransaction; accept exact registration replay or register wholly missing inputs, then require exact post-registration replay. Caller must roll back all failures. Added persistNativeFacePreparationInputs: committed blocked draft, publication outside SQL, separate fully rechecked registration transaction; no queue or acceptance ticket.

Physical isolated PostgreSQL/MinIO test passed both FAN/BiSeNet input registration/replay, coordinator replay, actual published transport, exact unsealed/blocked attempt-zero state, schema-extra/task/count/operation substitutions rejected and ACL verified over 8 service roles. Revoking the original session denies downstream registration and coordinator replay, returning no usable prepared task. Initial physical registration case: 1 passed / 14 deselected in 13.88s, Go PASS 1.808s. Final combined fixture and original SFD/LRASPP composed-admission regressions: 3 passed / 14 deselected in 22.42s, Go semanticproposal PASS 1.880s.

The fixture's qualification reports and completed SFD bytes remain synthetic; no GPU inference, scientific qualification or RTX 3060 capacity is established. Migration applied only to disposable tests. Existing photo processing algorithms and production capability switches were unchanged. Remaining V27: full composed downstream SQL admission/sealing/recovery, native outputs/skin, durable parent orchestration/public API. Entire V20–V32 objective remains incomplete.


### Complete downstream face admission evidence and SQL barrier (2026-10-07)

Added nativeFaceAdmissionEvidence/marshalNativeFaceAdmissionEvidence. The bounded schema-1 packet retains complete face proof, complete original parent SFD proof with its registered transport, and exact completed detector result/object evidence. The face plan is seal-verified and recompiled for current owned scope; both concrete frozen transports are independently bound; retained original source/ICC/policy/anchor, accepted Params pins, independent downstream report, actual input inventory and canonical config texts are preserved. Completed SFD lock evidence marshalling was extracted from the existing SQL wrapper without relaxing its body SHA/size/object/scope validation. The generic Go evidence serializer accepts face stages only as data construction; existing SFD/LRASPP SQL allowlists are unchanged.

Forward-only 00757 adds scheduler-only lock_semantic_face_graph_budget and lock_semantic_face_admission_evidence. The latter requires a strict nested envelope and strict face field/config sets, binds all original scope/request/anchor/policy/source evidence exactly to the detector packet, invokes original completed-detector admission authority (including original source/qualification/accepted Params/registered closure/current intent/snapshot), locks exact completed canonical SFD attempt/result/object pins through 00755, joins the downstream config to original detector run/task/result/output/Params, checks independently imported downstream qualification and accepted-version Params, and joins the exact FaceBoxes object version to the completed dependency. Existing pure source/Params closure checks cover all remaining files; dedicated 00756 registration checks independently cover the full actual face closure. Exact single-task/no-edge/no-expansion graph, existing FAN/BiSeNet output policies, positive CUDA resource profile and live original run quota are retained before success. No graph is sealed, activated or repaired and no receipt is created.

Added lockNativeFaceAdmissionInTransaction for scheduler-role/tenant context and bounded evidence submission; false returns conflict, caller must rollback failures. No object IO occurs inside this transaction. Both new SQL functions retain fixed search_path and explicit scheduler-only ACL.

Validation: Go semanticproposal initially PASS 1.767s. Physical PostgreSQL/MinIO fixture plus existing SFD/LRASPP admission regressions through 00757: 3 passed / 14 deselected in 18.13s. Both face stages passed full current-evidence barrier; outer extra field, actor scope, detector result identity, downstream qualification version, FaceBoxes object version, original source version and profile substitutions were rejected. ACLs validated across eight service roles. A final physical fixture also changed the actual disposable run quota reservation to 1 byte, verified admission conflict, restored original reservation and verified acceptance for both operations: 1 passed / 14 deselected in 19.45s. Final Go semanticproposal PASS 1.814s.

All reports/completed SFD image contents remain synthetic; this establishes transaction/evidence boundaries, not real inference or model/hardware quality. No production migration/deployment occurred and image algorithms were unchanged. Remaining: atomic downstream sealing/receipt replay, transport recovery, native output handling/skin, durable parent orchestration/public API and full V20–V32 acceptance. Capability enabling remains restricted to isolated test fixtures.


### Sealed FAN/BiSeNet evidence replay boundary (2026-10-07)

Forward-only 00758 adds scheduler-only semantic_face_completed_registration_state, semantic_face_completed_binding_state and lock_semantic_face_completed_evidence. These separate functions require an already sealed native face graph, retain the full exact transport/profile/rows/backend-document checks, and never treat missing or partial inputs as recoverable creation. Full completed evidence repeats original detector/intent/snapshot/source/qualification/Params/result/object authority and independently verifies downstream qualification/accepted Params/full closure and exact one-task/no-edge/no-expansion graph. It does not require a fresh original run reservation for receipt replay, does not alter task attempts/state and does not activate, seal or emit receipts/events. Existing SFD/LRASPP gates are unchanged.

Physical isolated PostgreSQL/MinIO fixture rejects the complete packet while the face graph is unsealed. Explicit owner metadata simulation sets only graph_sealed=true, verifies the full completed evidence, then restores graph_sealed=false before continuing. This deliberately tests the sealed evidence boundary, not real atomic activation, a queued run, receipt replay or inference. ACLs for all three functions were checked across eight service roles. Combined fixture and SFD/LRASPP admission regressions through 00758: 3 passed / 14 deselected in 26.73s. Go semanticproposal PASS 1.840s.

Remaining: actual downstream atomic sealing/202 receipt/event and exact receipt replay, transport recovery, native output acceptance/skin, durable parent orchestration/public API, real model/hardware/scientific evidence and full V20–V32 acceptance. Migration was applied only to disposable test databases; photo algorithms and production capability settings are unchanged.


### Atomic native face sealing and exact receipt replay (2026-10-07)

Forward-only 00759 adds scheduler-only seal_semantic_face_preparation. It requires the bounded strict original face/detector/result envelope, original authenticated actor/session/project, active tenant/usage locks, exact child config/policy hashes and original face-prep V2.Admit record/request identity. First sealing requires current complete 00757 admission evidence and live graph quota, CUDA-enabled operation registry and absence of unsupported consent links. It atomically seals/queues the run, activates the bound task, advances the tenant cursor, writes one queued event and outbox item and commits the exact 202 ticket to the idempotency record. Invalid evidence returns no ticket; activation/cursor/receipt races raise and the caller rolls back. Sealed replay checks the exact original stored ticket through full current 00758 evidence and neither resets task attempts nor emits another event.

Added sealNativeFacePreparationInTransaction / sealNativeFacePacketInTransaction and sealRegisteredNativeFacePreparation. Pure bounded proof construction precedes dedicated SQL submission; scheduler role and tenant context are checked. Malformed/missing/canceled/failed receipts return a zero ticket. The existing nativeTicketWithRetry transaction boundary returns a ticket only after commit and retries only confirmed aborted serializable/deadlock transactions; unknown commit outcomes cannot expose a usable ticket.

Physical isolated PostgreSQL/MinIO fixture now performs actual atomic sealing for both FAN and BiSeNet (not merely graph_sealed metadata simulation). Forced event cursor exhaustion returns 23514, zero ticket and leaves the run draft/unsealed, its task blocked/attempt-zero and no event. Restoring the cursor allows initial sealing and identical replay. Each child has exactly one queued sealed run, ready attempt-zero task, 202 receipt, queued event and outbox item. Scheduler-only ACL checked across eight service roles. Revoking the original session denies the existing face receipt replay and returns a zero ticket. Synthetic sealed-evidence simulation from 00758 is still separately scoped in the test.

Validation: Go semanticproposal PASS 1.886s. Combined physical fixture and existing SFD/LRASPP admission regressions through 00759: 3 passed / 14 deselected in 23.44s. This establishes real PostgreSQL queue admission/receipt/event atomicity and actual MinIO input transport, not real ML inference/scientific/hardware quality. No production migration/deployment or capability enabling occurred; source image algorithms remain unchanged.

Remaining: face transport recovery and restored sealing; native downstream outputs/result guards and skin; durable parent orchestration/public API; real GPU/model/calibration/photographic acceptance and full V20–V32 implementation. Child preparation receipt is not parent proposal completion.


### SQL-only native face recovery and restored sealing (2026-10-07)

Forward-only 00760 adds scheduler-only semantic_face_registered_transport. It locks the exact face run/task/workflow, requires original actor, original configured session and project plus current editor authorization, reconstructs all actual registered rows/chunks/direct/profile evidence from PostgreSQL and accepts only exact dedicated face registration or sealed binding replay. Missing/partial/foreign state returns NULL, never draft recreation or transport repair. It exposes only bounded schema/task/manifest/chunks/count/digest recovery evidence. Existing SFD/LRASPP recovery remains unchanged.

Added recoverNativeFaceTransportInTransaction with private plan seal, scheduler-role/tenant checks, strict bounded decode and re-binding to the original concrete frozen FAN/BiSeNet task. Added resumeNativeFacePreparation: each confirmed-aborted retry rereads the original SFD transport first, then actual downstream transport and repeats full native face sealing/current admission in the same transaction. No publication, draft creation or IO call is part of this recovery path. A usable ticket is returned only after successful commit; errors and unknown commit outcomes return zero through the existing tested native retry boundary.

Physical isolated PostgreSQL/MinIO fixture verifies absence before registration yields no transport, registered metadata matches the actually published manifest/count/digest, and foreign task/actor/session cannot recover. Actual first downstream sealing, forced cursor-exhaustion rollback and exact sealed receipt replay now run through SQL-only recovery for both FAN and BiSeNet. Revoked-session recovery returns a zero ticket. New recovery ACL verified across eight service roles.

Validation: Go semanticproposal PASS 1.985s. Combined physical fixture and SFD/LRASPP admission regressions through 00760: 3 passed / 14 deselected in 29.11s. Final physical native-bootstrap fixture with explicit recovery ACL checks: 1 passed / 14 deselected in 19.20s; this compiles/runs the edited physical Go fixture. Reports/completed SFD contents remain synthetic: no actual inference, RTX 3060 benchmark or scientific model qualification is claimed. Migration applied only to disposable test databases, algorithms and production capability switches unchanged.

Remaining V27: downstream native output/result guards and content handoff, skin stage, durable original parent orchestration/public API and real inference/quality evidence; full V20–V32 goal remains incomplete. Recovery still requires the retained original owned plan; durable reconstruction of the parent workflow is separate unfinished work.


### Native FAN/BiSeNet output root guard (2026-10-07)

Forward-only 00761 introduces private semantic_native_face_result_ready and extends the existing task_results trigger only for native v27_face_prepare face.landmarks/face.parse. Expected ordered outputs are one face_landmarks JSON or face_labels binary followed by face_labels_ref JSON. Current CUDA/v2.2 task/run/config/workflow, sealed running run, uncanceled scope, positive exact task attempt/fence, unique refs, ready/verified/unexpired original output objects and fully matching completed output_uploads are required. Existing detector/person/defect and generic V2 result paths are preserved. The predicate has no direct service EXECUTE grant.

New synthetic SQL metadata tests prove positive trigger insertion for both native face output shapes, rejection of missing/extra/unknown-field output refs, and refusal after output upload completion is invalidated. Existing SFD/LRASPP admission/result regressions also pass. Final combined result: 4 passed in 9.50s. Initial fixture runs exposed omitted required V2 fail_fast and explicit inputs=[]; only fixtures were corrected, no database invariant was relaxed. These tests do not read image/model output bytes or execute inference; exact content handoff, additional fence/cancel/order negative coverage and real photographic acceptance remain pending.

Migration applied only to disposable databases. Full V27 orchestration/content handoff/skin and V20–V32 implementation remain incomplete.


### Native face output denial coverage (2026-10-07)

Extended the real SQL trigger tests for both FAN/BiSeNet with changed hashes/foreign artifact refs; BiSeNet reversed order/duplicate/missing labels; zero or mismatched attempt/fence; task/run cancellation, queued instead of running and unsealed run; invalidated artifact verification/produced fence; changed upload object version/bytes/media type and incomplete upload. Each metadata mutation runs in a rollback savepoint and the restored positive predicate is checked afterward. Negative trigger insertion is exercised, not just pure readiness. The private predicate has no direct EXECUTE for all eight service roles.

Changed SHA/foreign artifact refs are rejected earlier by the existing artifact-reference trigger (ARTIFACT_REFERENCE_MISSING), whereas native shape/attempt/fence/output metadata failures raise V27_NATIVE_FACE_OUTPUTS_UNVERIFIED. The test now distinguishes these exact existing failure paths; no production function or invariant was weakened. Combined native-face and SFD/LRASPP SQL tests: 4 passed in 11.25s.

This closes previously missing SQL fence/cancel/order/metadata/ACL evidence. Fixtures remain synthetic metadata, not object-byte validity or real inference. Completed result discovery/content handoff, skin, durable parent orchestration/public API, actual model/hardware quality and full V20–V32 acceptance remain unfinished.


### Parent-bound native face result catalog (2026-10-07)

Forward-only 00762 adds API-only semantic_native_face_result_catalog. It requires current authenticated editor scope, exact original parent/request/session and stable face child run, succeeded uncanceled CUDA task, sealed uncanceled native run, current canonical task result/attempt/fence and exact ordered FAN/BiSeNet outputs. Every output must retain matching ready/verified/unexpired task-produced object and completed upload metadata. Original detector task/result/output/Params and original source binding are independently joined to the completed parent-owned SFD catalog. Downstream Params must match the configured task ref and accepted-version server document ownership. It returns result_id, the full ordered outputs array and Params; no queue/recovery/skin authority is delegated by lookup alone.

Validation: migration and native-face/SFD/LRASPP SQL regressions initially passed 4 tests in 10.69s. Stronger final fixture uses stable face run/workflow/task, current original parent/request/session, valid verified outputs and succeeded canonical result, but deliberately lacks a completed original SFD; API discovery returns no rows. API-only ACL checked across eight roles. Final combined SQL result: 4 passed in 10.18s.

Positive completed face discovery with actual original SFD/accepted downstream Params/object bytes is NOT yet proven; this turn establishes catalog implementation, migration/ACL/negative dependency evidence only. Byte-verified content decoding/handoff, skin, durable parent orchestration/public API and real GPU/model/quality acceptance remain pending. No production migration/deployment or image algorithm changes occurred; full V20–V32 goal remains incomplete.


### Typed native face result lookup (2026-10-07)

Added nativeFaceResultCatalog and SQLTransferSourceReader.nativeFaceResultCatalog. Lookup verifies/recompiles the original owned private face plan, uses the original parent request/actor/session/project and frozen task ID under tenant editor scope, and calls the dedicated API-only 00762 catalog. No rows means conflict; every failure/cancellation returns a zero catalog. Strict bounded decoding preserves ordered outputs, requires exactly one FAN or two BiSeNet outputs, exact frozen Params, valid nonzero unique digest refs and no alias with original source/SFD output/SFD Params/own Params; the returned result cannot reuse the original SFD result ID. This is preflight lookup, not content validation or downstream admission authority.

Go decoder tests cover both operations, correct packet, malformed/null/object/empty/trailing/unknown-field outputs, duplicates, protected-ref aliases, nil result and substituted frozen Params. Go semanticproposal PASS 1.857s. Physical native-bootstrap PostgreSQL/MinIO fixture exercises the actual Go/API lookup for both actually queued face stages and requires conflict plus a zero catalog, preventing premature completed output discovery: 1 passed / 14 deselected in 25.12s.

Positive lookup with completed native face outputs and accepted original SFD/Params, byte-verified content decoding, skin and durable parent orchestration remain pending. No model inference/quality/hardware qualification is claimed. No production migration/deployment or image processing algorithm change occurred; V20–V32 remains incomplete.


### Positive native face completed catalog with physical object publication (2026-10-07)

Extended the actual native-bootstrap fixture after real FAN/BiSeNet input publication/registration/sealing/replay. Each stage now publishes immutable synthetic output bytes into MinIO, records exactly matching original task/attempt/fence/output-role/object/upload metadata, ingests the result through the real native output trigger and marks that exact result canonical/succeeded. Existing original completed SFD and actually accepted downstream Params remain intact. The Go/API 00762 catalog positively returns the exact canonical result ID, frozen Params and ordered output refs for both stages. Invalidating any individual output upload completion makes discovery return conflict and a zero typed catalog; completion metadata is restored afterward.

Final physical PostgreSQL/MinIO native-bootstrap case: 1 passed / 14 deselected in 16.69s. The edited Go fixture was compiled/run by this test. This closes positive catalog discovery evidence that was explicitly missing in the previous entries. Output bodies are deliberately synthetic placeholders (JSON fixture marker and four binary bytes), not valid full model-output contracts or scientific results. MinIO publication/hash/object metadata is real; typed content decoding/handoff/skin compilation and real inference/quality remain unproven.

No production migration/deployment, capability enabling or image algorithm change. Full V27 parent orchestration/skin and V20–V32 completion remain incomplete.


### Byte-verified completed native FAN content (2026-10-07)

Added completedNativeLandmarks/readCompletedNativeLandmarks. It verifies the owned native face plan/operation, looks up exact completed face result, rereads the original qualified source/SFD, joins the exact output/accepted Params object pins and reads their immutable versioned bytes through ReadPinnedSemanticDocument. Existing review.VerifyLandmarkDetection, VerifyLandmarkParams and ParseFaceLandmarkCollection prove original SFD/source/geometry, params/model consistency and full landmark document rules. CUDA/model-lock claims must match the retained original server policy. After IO it repeats completed face catalog, exact object pins, original source/SFD bytes/catalog, full original policy (through detector read) and current independently imported FAN report authority. Every failure/cancellation returns a zero private content proof; no downstream admission or SQL-lock permission is delegated.

Physical fixture FAN output is now a canonical valid synthetic empty fan2d collection with exact image/SFD refs, CUDA/model/preprocessing claims, zero mean confidence and required no_faces warning. This closes positive byte/content validation for the no_faces branch using real published MinIO bytes, original SFD and accepted Params. BiSeNet output placeholders are still deliberately only catalog fixtures, not valid full label content.

Validation: initial physical byte-verified FAN case 1 passed / 14 deselected in 19.93s, Go semanticproposal PASS 1.869s. Added cancellation-before-IO unit check and physical content reader refusal/zero proof while its output upload completion is invalidated. Final Go semanticproposal PASS 1.957s; final physical native-bootstrap PostgreSQL/MinIO 1 passed / 14 deselected in 22.93s. Original scientific/source/ICC/image processing algorithms were reused without threshold relaxation or clipping.

Nonempty FAN inference/photographic quality, BiSeNet labels bytes/NPY content and content handoff, skin stage, durable parent orchestration/public API, actual hardware/model qualification and full V20–V32 completion remain pending. No production migration/deployment/capability enabling occurred.


### Native BiSeNet content wrapper and invalid-document refusal (2026-10-07)

Added completedNativeFaceParse/readCompletedNativeFaceParse. It binds the owned original face.parse plan, current completed catalog and completed original SFD, reads exact immutable parse manifest/Params pins, delegates original manifest/Params/detector/uint16 labels verification to existing ReadFaceParsePreparation (including full NPY/hash/class-count/no_faces checks), joins labels binary and detector refs to the exact catalog outputs/original SFD, and checks CUDA/model-lock claims against server-selected original policy. After IO it repeats catalog, exact object pins, original source/SFD body/catalog and imported qualification authority. It returns only private verified labels/counts/original bytes/pins; errors return zero proof/counts and grant no downstream admission authority.

Physical fixture intentionally retains invalid BiSeNet manifest/binary placeholders with valid real MinIO publication and completed metadata. The new reader rejects that content and returns zero result/body/counts, distinguishing catalog availability from valid model content. This case fails before valid-label NPY streaming because the manifest is deliberately malformed; it does not establish the positive NPY/content path. Initial compilation exposed incorrect copied private struct fields; those were fixed without altering any existing verifier.

Validation: Go semanticproposal PASS 1.893s; physical native-bootstrap PostgreSQL/MinIO case including invalid BiSeNet content refusal and valid synthetic no_faces FAN content: 1 passed / 14 deselected in 20.06s. Positive native BiSeNet manifest/NPY content evidence, skin-stage and parent orchestration/public API remain pending, as do real model/hardware/photographic qualification and full V20–V32 completion. No production migration/deployment/capability enabling or image algorithm relaxation occurred.


### Positive native BiSeNet manifest/NPY content and closure compatibility (2026-10-07)

Replaced BiSeNet placeholder output fixture with a canonical source/SFD-bound FaceLabels19 manifest and real little-endian uint16 NPY bytes (400x300, all background class 0), preserving exact CUDA/model/preprocessing/no_faces/geometry claims. Both outputs are actually published as immutable MinIO objects and ingested under original attempt/fence/completed upload metadata. The native content reader must return the exact canonical result and 120000 background labels with every other class count zero, exercising the full streamed hash/dtype/shape/class/no_faces verifier.

The first positive test found a production compatibility gap: legacy semantic_proposal_face_parse_preparation_pixels required the SFD in parser.inputs, while native tasks retain that dependency in registered full closure and expose only Params/source/pixels directly. Forward-only 00763 preserves the legacy direct-input branch and adds a dedicated native branch requiring sealed uncanceled face.parse config, exact original SFD output/task/run/canonical result and parent/request/session/source linkage, exact FaceBoxes row in the actual registered task inventory and ready full closure. Existing source/editor/canonical producer/object verification remains mandatory; no generic source or metadata check was removed.

Validation: initial positive attempt failed at this legacy closure gate; after 00763 native-bootstrap passed 1 / 14 deselected in 16.62s. Final physical PostgreSQL/MinIO native-bootstrap plus existing preparer-fresh regression passed 2 / 13 deselected in 29.24s. Physical Go fixtures were compiled/run by those tests. This establishes byte-verified synthetic no_faces BiSeNet content and legacy/native reader compatibility, not real CUDA model inference, foreground segmentation quality or hardware qualification.

No production migration/deployment/capability enabling or image-quality threshold relaxation. Skin-stage compilation/admission, durable parent orchestration/public API, actual model/hardware/photographic acceptance and full V20–V32 remain incomplete.

## Native skin Params publication (2026-10-07)

Добавлены PublishSemanticFaceSkinParams и publishNativeSkinStageParams: accepted-version server publication, запрет alias Params/source/detector/labels, исходная source/parser/SFD metadata chain, проверенный completed BiSeNet reader до и после публикации, сравнение полного evidence и канонического SHA Params. Это подготовка параметров без допуска или запуска CPU-задачи. go test ./internal/v2draft ./internal/v2input ./internal/semanticproposal проходит. Физическая публикация skin-stage в этом этапе ещё не проверена; качество GPU и итоговый proposal не подтверждены.

## Physical native skin Params and CPU preparation (2026-10-07)

Extended native-bootstrap validates real accepted-version skin Params publication and immutable replay after byte-verified BiSeNet manifest/uint16 NPY; clearing completed_at on either parser output refuses skin evidence with zero result. Added prepareNativeSkinTask: original parser proof reread, deterministic parent-owned run/workflow, accepted Params pinned bytes, authoritative label/manifest sizes, frozen CPU profile and repeated parser/label/Params object checks. The fixture compiles mask.face_skin with real input sizes and refuses RAM/scratch/output limits of one byte. No registration, SQL admission, qualification override, queue activation or scientific inference is implied.

Verification: go test ./internal/semanticproposal passed (1.752s); v2draft/v2input passed earlier in this turn. Isolated native-bootstrap physical test passed twice: publication checks 23.25s; expanded CPU preparation checks 23.30s (1 passed, 14 deselected each). Model reports and GPU outputs remain synthetic. Next: owned skin graph and locked CPU dependency/admission, followed by result handoff and parent orchestration.

## Owned native CPU skin draft graph (2026-10-07)

Added nativeSkinDraftPlan compiler and verification fingerprint. The private preparation retains original tenant/project/actor/session/parent/bucket scope; compiler recompiles the parent BiSeNet plan, checks stage/result/Params/input inventory, stable parent-owned run/workflow/task identities, CPU profile and output reserve plus 8 MiB. Single task, no edges; config records original request/source/intent/snapshot/policy and exact parser dependency. Fingerprint includes shell, original plans, frozen task, actual objects, parser/control/source bytes and label counts. This is graph metadata preparation; no new SQL privilege, admission, qualification waiver, queueing or worker execution.

Validation: go test ./internal/semanticproposal passed (1.826s). Disposable PostgreSQL/MinIO native-bootstrap passed (1 passed, 14 deselected, 22.20s), including positive graph, altered-reserve fingerprint rejection, foreign preparation session and insufficient reserve refusal. GPU/model outputs remain synthetic. Remaining: locked CPU registration/admission and complete parent orchestration/result handoff.

## CPU skin resource evidence recompilation (2026-10-07)

Closed a graph compilation gap: retained object sizes now recompile the full frozen skin task/profile using the original source/parser/detector bytes and exact input inventory. The resulting DraftTask must equal the original frozen task. Parser manifest/Params pins must match catalog references and actual control-byte lengths; label/own Params SHA metadata must match their refs. Changed label or Params byte counts cannot obtain a graph seal using the former resource profile. This remains preparation, not SQL admission.

Validation: semanticproposal Go tests passed (1.820s); isolated PostgreSQL/MinIO native-bootstrap passed (1 passed, 14 deselected, 22.49s), including both new size-substitution refusals. No production migrations or GPU inference executed. Next work remains dedicated skin dependency/registration SQL gates and blocked-draft persistence.

## Native BiSeNet dependency transaction lock for skin (2026-10-07)

Forward-only migration 00764 adds scheduler-only lock_semantic_native_parse_result (fixed search_path, fn_owner, PUBLIC revoked). Locks original SFD via existing 00755 exact evidence; repeats current original source/editor authorization; holds parser run/task/workflow/attempts/results/artifacts/uploads and compares current API catalog against bounded exact result/ordered outputs/accepted parser Params/file metadata. Go lockNativeSkinDependencyInTransaction verifies/recompiles owned skin graph, serializes original SFD plus parser object pins, requires scheduler role, sets tenant locally and returns conflict on stale dependency. No object IO, graph persistence, task activation or SQL permission expansion of previous admission gates.

Validation: Go semanticproposal tests passed (1.930s); disposable native-bootstrap physical test passed (1 passed, 14 deselected, 23.42s). Positive dependency lock, actual NPY upload completed_at=NULL refusal, restoration and positive lock verified. Migration applied only disposable databases. Concurrent-writer/role ACL tests for this specific new function still pending. Next: blocked skin persistence and complete registration/admission gate.

## Blocked native CPU skin draft persistence (2026-10-07)

Added beginNativeSkinDraftInTransaction. Caller owns scheduler transaction and must roll back every failure. Recompiles sealed owned skin graph, locks original inventory/intent/snapshot, qualified original detector/source, current imported parser qualification, exact completed SFD/parser dependency via 00764, accepted-version own Params and full qualification policy snapshot. Uses existing BeginAdmission only for exact created/pending child. No object IO, registration, seal or queue activation.

Validation: Go semanticproposal passed (1.811s); disposable PostgreSQL/MinIO native-bootstrap passed (1 passed, 14 deselected, 24.31s). Physical created then pending replay yields exactly one draft/unsealed run with blocked attempt=0 task. Existing native original/face checks remain exercised. Registered skin replay requires dedicated future gate; new dependency ACL/concurrent writer tests remain pending. Next: publish transport after committed blocked draft and verify/register exact CPU closure with repeated authority locks.

## Physical native skin input transport publication (2026-10-07)

Added publishNativeSkinDraftTransport: verifies private graph seal and stable parent-owned run/workflow, exact declared/frozen task, publishes existing CPU compiler closure through ScopedPublisher and freezes complete registration proof. Checks task/Params/input digest and repeats graph seal after object IO. Must be called after committed blocked draft and outside SQL; final registration must repeat authority. No registration, repair, seal or queue permission added.

Validation: semanticproposal Go tests passed (1.921s); disposable native-bootstrap PostgreSQL/MinIO passed (1 passed, 14 deselected, 19.38s). Actual skin transport contains three inputs; immutable manifest/input digest replay passed; changed Count binding refused. Next: dedicated bounded skin registration packet and SQL registration/missing/replay gate; registered shell replay and admission remain incomplete.

## Dedicated native CPU skin registration and replay (2026-10-07)

Added bounded marshalNativeSkinRegistrationBinding independently of GPU-only binding. Forward-only 00765 adds skin-only raw/envelope registration state and registered-shell replay wrapper; preserves prior workflow matcher chain. Exact CPU operation, own Params, three rows (Params/FaceLabels19/NpyArray), direct refs matching ordered parser outputs, task-owned ready immutable manifest/chunks and complete closure required. Only wholly missing registration can be inserted; partial/conflicting state is refused. registerNativeSkinInputsInTransaction binds frozen transport, repeats blocked draft authority locks, then requires missing→registered→replayed or existing replay. No task activation.

Validation: Go semanticproposal tests passed (3.573s). First physical run exposed SQL operator precedence in direct-ref containment; corrected parentheses. Rerun disposable PostgreSQL/MinIO native-bootstrap passed (1 passed, 14 deselected, 33.19s), including real skin registration then replay. No production migrations. Dedicated malformed-packet/role/concurrency matrices and admission/sealing remain pending. Next: coordinator and full CPU admission barrier.

## Native CPU skin preparation coordinator (2026-10-07)

Added persistNativeSkinPreparationInputs using the existing preparation coordinator: committed blocked graph, object publication outside SQL, fresh transaction with full original authority/dependency locks and exact registered closure. Errors return zero prepared task; helper does not activate or seal. Registered replay reuses immutable published manifest.

Validation: semanticproposal Go tests passed (4.166s); disposable PostgreSQL/MinIO native-bootstrap passed (1 passed, 14 deselected, 100.59s). Verified coordinator after physical registration retains exact task/manifest and blocked attempt=0. Waited on the same live process throughout; no duplicate test launch. Admission/sealing, SQL recovery, revoke/ACL/concurrency matrices and parent orchestration remain pending.

## Native CPU skin revocation refusal (2026-10-07)

Extended physical fixture retains actual registered skin graph/transport. After irrevocably revoking the original fixture session, dependency lock, registration replay and preparation coordinator all refuse. Coordinator returns zero task/manifest, exposing no usable transport. No production sessions changed.

Validation: semanticproposal Go tests passed (6.098s); disposable PostgreSQL/MinIO native-bootstrap passed (1 passed, 14 deselected, 143.27s). Same live handle was polled until terminal; no duplicate run. This proves current-session authorization refusal, not skin task execution, scientific GPU quality or full release acceptance. Next: complete CPU admission/sealing evidence and remaining role/concurrency checks.

## CPU skin SQL role and malformed registration matrix (2026-10-07)

Physical fixture checks EXECUTE ACL on 00764 dependency lock and both 00765 skin registration functions across photo_api/scheduler/worker/publisher/portal/auth/gateway/maintenance: only scheduler allowed. After real registration, malformed envelope extra field, foreign task UUID, incorrect entry count and face.parse operation each return conflict. No previous GPU binding allowlist widened.

Validation: semanticproposal Go tests passed (7.112s); disposable PostgreSQL/MinIO native-bootstrap passed (1 passed, 14 deselected, 151.44s). Same running handle retained through completion. Full CPU admission/sealing/recovery and concurrent-writer verification remain pending; these scoped authorization checks are not product release acceptance.

## Skin parser dependency concurrent writers (2026-10-07)

Physical test holds lockNativeSkinDependencyInTransaction in a scheduler transaction. Separate inspector transactions with lock_timeout=250ms attempt parser cancellation, NPY output_uploads.completed_at removal and original session revocation; all return PostgreSQL 55P03. Writers and lock transaction roll back. This establishes retained parser/upload/current authorization locking through transaction end; no task executes.

Validation: semanticproposal Go tests passed (4.688s); isolated PostgreSQL/MinIO native-bootstrap passed (1 passed, 14 deselected, 110.93s). Existing ACL, malformed registration, original detector locks and revoked coordinator refusals remain exercised. CPU graph admission/budget/sealing and durable parent orchestration are still incomplete.

## CPU skin graph and live quota barrier — physical verification pending (2026-10-07)

Added forward-only 00766 lock_semantic_skin_graph_budget and lockNativeSkinGraphBudgetInTransaction. Go adapter repeats blocked graph authority checks, requires exact registered skin binding, then holds tenant usage/run/task/workflow/quota checks. SQL validates one blocked CPU skin task, no edges/expansions, complete closure, registry backend and three established output roles, live user reservation covering output plus 8 MiB. This is pre-seal evidence, not activation or full autonomous SQL admission.

Go semanticproposal tests passed (7.494s). Extended physical fixture covers positive barrier, actual reservation reduced to one byte refusal/restoration, scheduler-only ACL. Physical run failed during migration setup with PostgreSQL DiskFull, before executing test assertions (26.98s). Read-only df confirmed host and development PostgreSQL volume 100% used, about 146–147 MiB available. No data deleted or production migrations applied. 00766 and quota tests are NOT physically verified. Full CPU sealing/recovery remains incomplete; restore disk capacity before physical acceptance.

## Skin quota envelope tightening; physical service unavailable (2026-10-07)

Rechecked disk: 2.1 GiB available. Retried exact physical native-bootstrap handle once after external state changed; failed before migrations because development PostgreSQL refused port55432 (1.36s). Read-only Docker inspect then returned daemon HTTP500; container state could not be established, no restart attempted. Previous DiskFull evidence remains historical, latest blocker is unavailable PostgreSQL/Docker.

Tightened 00766 budget packet to exact four keys, 16 KiB bound, string actor/snapshot/task and exact registration task_id field. Migration and quota assertions remain physically unverified; no successful rerun is claimed. CPU sealing and recovery remain incomplete. Restore development Docker/PostgreSQL before physical acceptance.

## Native skin cancelled and unsealed boundary refusal (2026-10-07)

Added native_skin_fail_closed_test.go. Cancelled requests with deliberately nil DB/object infrastructure refuse Params publication, task preparation, coordinator, transport publication and registration packet creation with context.Canceled and zero proof/task/manifest. Unsealed plans refuse coordinator/binding before infrastructure access. These tests verify interruption and evidence non-disclosure boundaries independently of development Docker.

Validation: go test ./internal/semanticproposal passed (1.853s). No physical retry or migration acceptance claimed; 00766 remains unverified following DiskFull then unavailable PostgreSQL/Docker. Complete CPU sealing/recovery and parent orchestration remain outstanding.

## Blocked registered CPU skin transport recovery (2026-10-07)

Added 00767 semantic_skin_registered_transport and recoverNativeSkinTransportInTransaction. Requires scheduler role/current actor/session/project and exact registered 00765 binding; reconstructs manifest/chunks/count/entries digest without object publication or input repair; Go decoder and frozen BindPublishedTransport independently validate. SQL intentionally returns NULL for graph_sealed until a completed-binding recovery gate is implemented. This interim blocked recovery does not satisfy full seal/receipt retry requirements.

Go semanticproposal tests passed (2.151s before fixture extension); extended physical fixture now checks missing recovery refusal, exact registered recovery and role ACL, but assertions and 00766/00767 remain physically unverified while development Docker/PostgreSQL is unavailable. No production migration applied. Next: complete skin admission/sealing/completed recovery and rerun physical tests when service recovers.

## Bounded native skin dependency evidence composition (2026-10-07)

Extracted marshalNativeSkinDependencyEvidence for subsequent composed admission/sealing. Recompiles owned sealed plan, embeds existing exact original SFD packet, hashes actual retained parser manifest/Params bytes and checks their refs/file sizes/media before returning bounded32KiB canonical packet. Existing SQL lock uses this same serializer; evidence serialization remains non-authoritative. Cancelled packet creation returns nil before DB/object access.

Verification: semanticproposal Go passed (1.948s before added cancellation assertion; latest command result recorded in task output). TCP probe confirms development PostgreSQL still unavailable. No physical migration or inference proof added; 00766–00767 physical verification and full CPU admission/seal/recovery remain outstanding.

## Skin Params source-child alias exclusion (2026-10-07)

Publisher now resolves exact source metadata and rejects Params document IDs aliasing RGB, alpha or any dense inverse warp before publication. Existing source/parser/detector/root/label alias checks remain. This closes a gap where task compilation refused aliases but standalone Params publication did not explicitly exclude original source children.

Validation: Go v2draft passed (4.407s), semanticproposal passed (3.109s before fixture extension). Physical fixture adds explicit source RGB/alpha/geometry alias refusal assertions; these assertions have only compiled, not executed because development PostgreSQL was unavailable at last probe. 00766/00767 and full admission/sealing remain unverified/incomplete. No quality algorithm changed.

## Composed native skin admission evidence packet (2026-10-07)

Added nativeSkinAdmissionEvidence and marshalNativeSkinAdmissionEvidence. Recompiles private skin plan; embeds full original native face admission packet (including independent parser qualification/source/SFD proof and both registered transports), canonical completed-parser packet, exact skin config text/accepted Params pin and registered skin transport. Rejects stale seals/object size/media/hash and bounds packet32MiB. Does not grant SQL authority, seal, activate or issue a receipt. Existing GPU allowlists remain unchanged.

Go semanticproposal passed (1.894s before test extension). Added cancelled serializer zero-evidence assertion and physical fixture positive packet composition; physical assertion has not executed while development PostgreSQL is unavailable. Pending: dedicated composed SQL gate, sealed replay proof and atomic skin sealing plus physical acceptance of00766–00767.

## Composed CPU skin SQL admission barrier — unverified physically (2026-10-07)

Added forward-only00768 lock_semantic_skin_admission_evidence and Go adapter. Strict bounded packet links completed original face.parse evidence (00758), original SFD/source/qualification/policy, current parser result locks (00764), exact deterministic skin parent/config/Params/dependency refs, accepted own Params, skin registration (00765) and live graph/quota (00766). Existing GPU gate permissions unchanged. Scheduler-only, fixed search_path, PUBLIC revoked. Does not activate or seal. SQL and full physical acceptance remain unproven.

Go semanticproposal passed (1.827s before fixture extension); positive composed admission and ACL assertions added to physical fixture but not executed. Existing development PostgreSQL availability blocker prevents physical verification of00766–00768. Next: run physical packet mutation/positive matrix, implement completed skin binding/receipt replay and atomic sealing.

## Skin composed admission substitution fixture (2026-10-07)

Extended physical native-bootstrap with six00768 packet mutations: extra field, foreign canonical parser result, original session, original parent, accepted Params version and registration count. Each must return false under scheduler tenant scope. The fixture rebuilds independent packet values per mutation and never logs sensitive packet contents.

Current TCP probe: development PostgreSQL port55432 unavailable. Mutation assertions and00766–00768 migration acceptance therefore remain NOT physically executed. Go compilation/regressions pass as recorded in current task output. Complete atomic skin seal/receipt/completed recovery remains outstanding; goal not complete.

## Sealed CPU skin completed-evidence SQL gate — physical proof pending (2026-10-07)

Added forward-only00769: separate semantic_skin_completed_registration_state, strict completed binding envelope and lock_semantic_skin_completed_evidence. Sealed branch repeats original completed qualified parser/source/SFD and accepted Params/exact registered transport checks; no missing-registration branch, no state/attempt reset, no original live quota requirement. Rejects cancelled run/task. Checks one task/noedges/noexpansions. Does not create receipt, activate or grant permission to repair. Scheduler-only functions with fixed search_path and PUBLIC revoked.

ACL fixture extended for all three functions. Go compilation/regressions pass as recorded in task output. SQL00766–00769 remains physically unverified with unavailable development PostgreSQL; no receipt replay or actual sealed run proof claimed. Next: atomic skin seal and completed transport recovery update, then full physical positive/mutation/replay/rollback acceptance.

## Native CPU skin atomic sealing implementation — physical acceptance pending (2026-10-07)

Added forward-only00770 seal_semantic_skin_preparation and Go transaction/retry adapters. Requires current original actor/session/project, exact original request/current skin config/policy, skin-prep idempotency record. First seal requires00768 full admission and v2_enabled CPU registry, rejects unsupported consent links, atomically seals/queues/activates task and writes one event/outbox/202receipt. Sealed replay requires exact stored ticket and00769 current completed evidence, no task reset or duplicate event. Ticket escapes only confirmed successful commit; existing nativeTicketWithRetry governs serialization/deadlock retries and unknown commit returns zero. No production flags enabled.

Go semanticproposal passed (1.867s before extended cancellation/ACL assertions); cancellation returns zero ticket, ACL fixture extended. SQL00766–00770 remains physically unverified, actual first seal/replay/cursor rollback acceptance is still pending; implementation is not a claim of runtime readiness. Completed transport recovery and result handoff remain unfinished.

## SQL-only skin sealed transport recovery and resume (2026-10-07)

00771 replaces skin registered transport lookup to validate sealed runs with00769 completed binding, while blocked runs retain00765 registration checks. Added resumeNativeSkinPreparation: recover original SFD, parser and skin transports in dependency order within each nativeTicketWithRetry transaction, then full00770 skin seal/receipt replay. No object IO, draft creation, input repair or ticket before successful commit. Retained in-memory private plan is still required; durable reconstruction is not implemented by this helper.

Go semanticproposal passed (2.770s before cancelled resume assertion); cancellation returns zero ticket before SQL. SQL00766–00771, actual first seal/replay/cursor rollback and sealed transport assertions remain physically unverified with development PostgreSQL unavailable at last probe. No production registry switch enabled. Next: extend actual isolated firstseal/replay/rollback fixture and restore physical verification.

## CPU skin first seal/replay/rollback acceptance fixture prepared (2026-10-07)

Extended native-bootstrap to enable mask.face_skin only in disposable operation registry, force MAX event cursor and require23514/zero ticket plus draft/unsealed/blocked attempt0 rollback, restore cursor, then run resumeNativeSkinPreparation twice with exact queued ticket and one event/outbox/202receipt. After original session revocation, recovery must expose zero ticket. Existing publication/budget/mutation/ACL/locking assertions remain.

Go semanticproposal passed (1.881s before rollback extension). Current development PostgreSQL TCP probe: unavailable. All new runtime assertions are compiled only, NOT executed;00766–00771 physical acceptance remains pending. No production switch or migration applied. Next: restore physical verification and fix any actual SQL/activation failures before claiming skin startup completion.

## Native CPU skin output attempt/upload SQL guard — unverified physically (2026-10-07)

Added forward-only00772 private semantic_native_skin_result_ready and native skin branch in guard_v2_result_root. Confirms existing worker output order [skin_mask,skin_mask_png,skin_mask_ref] and media from operations/mask/face_skin.py. Requires current CPU/v2.2, sealed running uncancelled run, matching positive attempt/fence, exact unique refs, ready verified immutable objects and matching completed uploads (role/bytes/SHA/bucket/key/version/media/task/attempt/fence). Existing SFD/FAN/BiSeNet/defect/generic branches retained. Helper has no service EXECUTE grant.

Go regressions passed; private helper ACL assertions added to physical fixture but not executed. No completed skin output content, actual worker inference or SQL00766–00772 runtime proof claimed. Next: canonical skin result catalog/content verification and restore physical PostgreSQL/MinIO acceptance.

## Exact native skin result catalog and bounded decoder (2026-10-07)

Added00773 API-only semantic_native_skin_result_catalog: current original editor/session/request, stable CPU skin child, succeeded canonical current attempt/fence, ordered three matching completed uploads, own accepted-version Params and exact parent BiSeNet result/source dependency. Go nativeSkinResultCatalog recompiles retained private plan, uses tenant editor transaction and bounded strict decoder, refuses missing/foreign/duplicate/upstream alias refs and foreign Params with zero result. No source-wide latest fallback or admission permission.

Go semanticproposal passed (1.857s before added malformed decoder matrix); decoder matrix covers missing/duplicate/Params alias/foreign Params/unknown/trailing/null/upstream alias. SQL00766–00773 and actual skin output content remain physically unverified. Next: physical result outputs/catalog acceptance, byte-level NPY/PNG/MaskRef validation and parent handoff.

## Native skin physical derivation verifier helper (2026-10-07)

Added verifyNativeSkinDerivation: reuses review.VerifyFaceSkinParams, masknpy.VerifyFaceSkinNPY, confidence checks and existing probability PNG verifier. Validates retained label/mask/PNG refs, immutable object metadata/media; streams pinned byte/SHA label→float32 exact skin derivation then checks PNG corresponds to probability mask. Closes streams on both success/failure; cancelled/absent reader refuses. No processing algorithm changed.

Go semanticproposal passed (1.798s before nil-reader guard). Helper is not yet connected to a complete source-authorized native skin content reader, so no runtime content verification or admission claim. SQL00766–00773 remains physically unverified. Next: authoritative object pins, full root/Params read, repeat catalog/pins/parent evidence after IO and physical acceptance.

## Native skin immutable objects and complete content reader — physical proof pending (2026-10-07)

00774 adds API-only exact current result object pins for [mask,PNG,MaskRef,Params], scoped through00773 current canonical result/actor/session/original request. Go nativeSkinResultObjects requires four exact ordered refs, bucket/version/bytes/media; wrong/incomplete inventory returns zero. readCompletedNativeSkin reads pinned MaskRef/Params, independently revalidates qualified source/BiSeNet, resolves exact labels, verifies original Params/source/mask/label derivation, confidence and PNG equivalence using existing validators. Repeats catalog/object/parser/labels metadata after IO; stale evidence returns zero. Cancelled reader stops before IO. No processing algorithm changed.

Go semanticproposal passed (3.186s before cancelled-content test extension). SQL00766–00774 and successful physical skin content/worker execution are unverified. Reader code is implemented, not runtime-qualified. Next: isolated valid/invalid skin output fixture and parent workflow handoff, durable plan reconstruction/API integration.


### Native skin physical-byte regression — 2026-10-07

Added `TestNativeSkinDerivationPhysicalBytes`: actual uint16 Face19 NPY, float32 skin NPY, hashed immutable object pins and 16-bit grayscale filter0 PNG. The complete verifier accepts class1→1 / other classes→0, rejects a rehashed but semantically incorrect probability (.5), returns zero on failure and closes every opened stream. The test uses the worker PNG contract; ordinary Go PNG encoding applies filters outside that contract.

Validation: `go test ./internal/semanticproposal ./internal/masknpy -count=1` passed (4.219s / 1.755s). This verifies byte parsing and derivation in memory, not PostgreSQL migrations, MinIO, actual model quality or end-to-end release readiness. Overall development estimate remains 50–60%; goal remains active.


### Native skin adversarial bytes — 2026-10-07

Extended the complete native skin byte regression with independently rehashed fractional, NaN, infinity, wrong-class, truncated and trailing-byte NPY masks, plus rehashed PNG CRC corruption, truncation and trailing data. Every case must refuse with zero pixel count and close all opened streams. These tests exercise the complete metadata/NPY/PNG verifier rather than only individual parsers.

`go test ./internal/semanticproposal ./internal/masknpy -count=1` passed (4.281s / 0.458s). Current TCP probe confirms PostgreSQL 127.0.0.1:55432 unavailable; no migration/runtime acceptance inferred from these tests. Scope and overall estimate remain unchanged.


### Native skin submission coordinator — 2026-10-07

Added internal `startNativeSkinPreparation` and private `nativeSkinSubmission`. The coordinator validates cancellation, infrastructure and storage reservation bounds before publication; reowns the sealed parser plan; publishes server Params; freezes the CPU task; compiles the skin graph; commits blocked metadata; publishes transport outside SQL; registers with repeated authority; then recovers the detector/parser/skin transports and atomically seals via the existing transaction retry helper. Only a confirmed committed ticket returns with the owned plan. Every error returns a zero submission. Existing registered submissions use `resumeNativeSkinPreparation`, avoiding transport republishing in the recovery path.

Validation: `go test ./internal/semanticproposal -count=1` passed (1.882s), including cancellation before infrastructure access. Positive SQL execution of the new coordinator is unverified while PostgreSQL remains unavailable. This is an internal orchestration entry point; public API wiring, durable plan reconstruction and full parent orchestration are still incomplete.


### Cancellable bounded Face19 skin conversion — 2026-10-07

Worker `skin_probability(labels, check_active)` now converts uint16 Face19 labels in row blocks of at most 16384 elements, checks cancellation before processing, between blocks and before returning, and rejects empty or oversized canvases and out-of-range labels even in the last block. It preserves exact class1→float32 1 / other classes→0 and supports noncontiguous views without reshaping or copying the full input. The required output remains full-frame float32; temporary comparisons are bounded instead of whole-frame. `execute` passes `ctx.check_active`. Existing conservative admission resource estimates are unchanged.

Validation: `env PYTHONPATH=workers:src:tests .venv-platform/bin/python -m pytest tests/platform/test_face_skin.py tests/platform/test_mask_png.py tests/platform/test_face_parse.py -q` — 12 passed, 1.26s. New tests cover exact noncontiguous ordering, cancellation within a multi-block row, late invalid class and empty canvas. This is CPU algorithm/format validation, not actual GPU quality, PostgreSQL/MinIO or full worker execution acceptance.


### Full CPU skin adapter and materialized-output gate — 2026-10-07

`mask.face_skin.execute` now reopens the actual written float32 NPY through bounded `read_array(..., unit_interval=True)` and compares each block against original mapped Face19 class1 before staging any output. A valid probability with wrong derivation (.5), corrupted layout/value or cancellation refuses the operation and releases scratch. Temporary comparisons stay bounded; the existing resource estimate remains conservative.

Added `tests/platform/test_face_skin_adapter.py`: executes the real CPU operation with real scratch files, NPY mapping and PNG encoding, using in-memory artifact registration/staging doubles. Checks exact three-output order, pixel agreement, original canvas/transform/model metadata, nonempty confidence .9, empty-mask confidence0/review, progress and cleanup; undeclared input refusal; cancellation; injected materialized NPY corruption before staging.

Validation: face_skin adapter + algorithm + PNG + face_parse tests — 17 passed in 3.86s. These tests do not establish real object uploads, database activation, model quality or end-to-end parent completion. PostgreSQL acceptance and public/durable orchestration remain outstanding.


### Physical PNG verification before native skin staging — 2026-10-07

Added `algorithms/mask_png_verify.py::verify_png16`: parses actual filter0 grayscale PNG16, verifies ordered IHDR/IDAT/IEND, exact canvas and CRC, rejects unknown chunks, oversized chunks, truncated data, additional compressed streams/pixels and trailing bytes. Compressed chunks and inflated buffers are capped at 64KiB; comparison/quantization uses at most16384 pixels per block, with cancellation and generator/file cleanup. `mask.face_skin.execute` compares the actual PNG against the reopened, Face19-verified NPY before either output is staged.

Validation: `test_mask_png_verify`, full `test_face_skin_adapter`, skin algorithm, PNG writer and face parser — 23 passed in 2.69s. Coverage includes random probability PNG spanning multiple chunks/row blocks, final pixel mismatch, CRC/truncation/trailing-byte failures, cancellation, and injected actual worker PNG corruption with no uploads and scratch cleanup. No claim of PostgreSQL/MinIO, real model quality or full parent acceptance.


### Native completed skin downstream evidence — 2026-10-07

Added private `nativeSkinCompletedEvidence` and `marshalNativeSkinCompletedEvidence`: repeats the complete byte-verified skin/parser read, requires exact retained proof equality, then binds the original composed admission proof, current canonical result ID/outputs/Params, four ordered immutable object pins and verified skin pixel count into a bounded32MiB canonical packet. Cancellation/errors expose no packet. This is evidence data only; the SQL dependency lock and parent handoff remain to be implemented and tested.

Validation: backend `go test ./...` completed successfully (cached and executed package checks; database fixtures without configured infrastructure are not claimed as executed). After the new evidence builder, `go test ./internal/semanticproposal -count=1` passed1.855s, including pre-IO cancellation. Positive completed-skin SQL/object-store evidence and full parent orchestration remain unverified.


### Completed skin dependency SQL lock — physical proof pending (2026-10-07)

Added forward-only00775 `lock_semantic_native_skin_result` with fixed search_path, fn_owner ownership, PUBLIC revoked and scheduler-only EXECUTE. Bounded exact-key completed-evidence packet must match explicit original tenant/project/actor/session/parent/request scope. The gate composes00769 full completed skin/source/parser authority, locks skin task/attempt/result/artifact/upload rows, rereads canonical skin catalog and00774 exact object pins, and compares current result/outputs/Params/four ordered files. Skin pixel count is bounded metadata from the physical byte verifier; SQL does not independently derive it and this function alone grants no task admission.

Added SQL-only `lockNativeCompletedSkinInTransaction`: reowns sealed plan, enforces scheduler role/tenant scope, calls the gate and refuses false. Caller must create byte-verified packet before opening parent transaction and roll back any error.

Validation: `go test ./internal/semanticproposal -count=1` passed2.028s including cancellation before SQL. Current TCP probe PostgreSQL55432 unavailable. Migration parsing/application, ACL matrix, positive result and concurrent writer locking are NOT yet physically verified; parent admission integration remains incomplete.


### Native skin result physical fixture prepared — 2026-10-07

Extended native-bootstrap integration scenario after committed skin first-seal/replay: creates a synthetic CPU attempt and actual immutable zero-skin float32 NPY, grayscale filter0 PNG16 and bound MaskRef; registers exact completed upload metadata; inserts canonical result through the00772 guard; reads complete byte-verified skin content; builds downstream packet; invokes00775 completed dependency lock. Also checks missing completion on each of the three uploads exposes zero content proof, and adds00775 scheduler-only ACL coverage across eight service roles. The no_faces labels remain explicitly synthetic; this fixture does not run a model.

Validation: `go test ./internal/semanticproposal -count=1` passed1.933s, verifying compilation and local tests. The integration scenario is prepared but NOT executed: Docker API socket currently absent (daemon not running), PostgreSQL unavailable. No database/container data was modified by diagnostics. Positive00766–00775 migration/runtime acceptance, this physical fixture and parent/public/durable orchestration remain outstanding.


### Completed skin lock adversarial/concurrency fixture — prepared only (2026-10-07)

Expanded native-bootstrap completed-skin dependency coverage: eight exact-packet substitutions (extra field, result, Params, object version/bytes, missing output, negative count and foreign original session) must refuse with ErrConflict. While the original completed lock is held, separate writer transactions with250ms lock_timeout must return55P03 for task cancellation, upload completion removal, PNG object-version change and original-session revocation; writers roll back and dependency lock is released explicitly.

Validation: `go test ./internal/semanticproposal -count=1` passed2.206s (compilation/local tests). SQL assertions remain prepared but unexecuted; no claim of physical rejection/concurrent-lock proof. Parent/public/durable orchestration and actual hardware/model acceptance remain outstanding.


### Native FAN/skin to defect Params preflight — 2026-10-07

Added private `prepareNativeDefectFaceParams` and retained `nativeDefectFacePreparation`: requires owned FAN and skin plans sharing the original detector seal; reads both current byte-verified results; checks exact completed detector/source equality; binds each original FAN object ID to the source-wide Face19 skin artifact in inline defect inputs; compiles via existing `PrepareDefectParams`; repeats both content reads and refuses any changed proof. Up to64faces and explicit empty no_faces list follow existing defect contract. No new mask artifact or admission privilege is created; original masks/geometry/confidence/model identity remain retained.

Added cancellation-before-IO assertion and prepared native-bootstrap synthetic no_faces FAN/skin→defect Params scenario. `go test ./internal/semanticproposal -count=1` passed1.865s before fixture extension; after fixture extension the package passed1.799s. Positive physical fixture, nonempty real model quality, defects qualification/publication and parent admission remain outstanding.


### Nonempty native defect face binding — 2026-10-07

Extracted `bindNativeDefectFaceInputs`, used by native preflight. It verifies bounded nonnull face list, unique original object IDs, Face68 geometry/points against mask canvas/transform, valid artifact/model digests, source-wide unprotected skin selector and finite unit confidence, and rejects landmark/mask artifact identity alias. Output masks own their transform bytes and model-lock pointer, preserving retained producer evidence across caller mutation.

New local test uses nonempty Face68 fixture with two original object IDs, checks exact mask/landmarks/canvas/confidence bindings, independent copied geometry/model storage and refusal of duplicate object IDs, foreign canvas and artifact alias. Legacy skin fixture has no model-lock; the test explicitly assigns a synthetic native model digest and does not imply real inference. `go test ./internal/semanticproposal -count=1` passed2.038s. Physical FAN/skin→defect workflow, publication/qualification/SQL admission and actual model quality remain unverified/incomplete.


### Completed FAN dependency for native defect preparation — pending SQL proof (2026-10-07)

Added forward-only00776 scheduler-only `lock_semantic_native_landmark_result`: checks bounded exact-key packet and explicit original actor/session/project/tenant/parent/request; composes758 completed qualified FAN admission; locks task/attempt/result/artifact/upload rows; rereads canonical one-output FAN catalog and exact ordered landmark/Params object pins. Fixed search_path, fn_owner ownership and PUBLIC revoke.

Added `marshalNativeLandmarkCompletedEvidence` with repeated complete physical content read and retained equality before canonical packet, and SQL-only `lockNativeCompletedLandmarksInTransaction` reowning sealed plan/current scheduler scope. Cancellation returns no evidence before IO. Extended native-bootstrap prepared FAN positive-lock and eight-role ACL assertions. `go test ./internal/semanticproposal -count=1` passed1.814s.

These local tests confirm compilation and cancellation;00776 migration/runtime/ACL/concurrent writer behavior remains unexecuted while Docker/PostgreSQL unavailable. Combined FAN+skin dependency assembly, native defect qualification/publication/admission and full parent/public/durable orchestration remain incomplete.


### Combined FAN/skin dependency preflight and transaction adapter — 2026-10-07

Added private `nativeDefectDependencies`, `prepareNativeDefectDependencies`, immutable digest and `lockNativeDefectDependenciesInTransaction`. Both completed byte-verification packets are assembled before parent transaction; proof binds their bytes, original plan seals and exact prepared Params/inputs/manual roots. Same original detector seal is required. SQL-only adapter validates retained digest and locks exact completed FAN followed by skin in one caller-owned transaction. All errors require rollback; this does not replace defect qualification/Params registration/full graph admission.

Cancellation-before-IO tests added. Prepared native-bootstrap now retains registered FAN transport and composes both dependency packets and locks after synthetic no_faces defect Params preparation. Local semanticproposal package passed1.915s before fixture extension; after fixture extension passed1.880s. Positive SQL integration remains unexecuted; production migrations/parent/public/durable orchestration and actual model acceptance remain incomplete.


### Exact native defect Params binding before dependency locks — 2026-10-07

Added `verifyNativeDefectPreparedParams`, invoked before composed dependency evidence creation and before either SQL lock. Verifies retained WorkingImage, exact compiled Params SHA and required declared inputs through the existing strict defect verifier; reconstructs expected ordered FAN object/skin bindings and compares canonical Faces; requires every retained manual root among declared inputs. Rejects incomplete landmark catalog, empty proof and cancellation before accessing producer data. This closes the gap where retained Params strings were sealed without revalidating their original native Faces contents. Manual/source/qualification SQL authority still must be composed separately.

Local semanticproposal package passed1.787s before boundary tests and after boundary tests the package passed1.787s. Positive SQL/MinIO/native-defect admission and nonempty model quality remain incomplete/unverified; prepared native-bootstrap now traverses this added check when infrastructure returns.


### Accepted-version publication of original native defect Params — 2026-10-07

Added `publishNativeDefectVersionParams`: verifies owned FAN/skin seals/common detector and exact original compiled Params binding; repeats complete FAN/skin content before and after IO; publishes stable `semantic.defect.params:reference` document through existing `v2draft.PublishDefectProposalParams` accepted-version quota/fencing; validates returned SHA and excludes document/artifact alias with original dependencies. Returns zero on error/cancellation. Manual/protection authority and scientific defects qualification/admission remain independent mandatory gates.

Added cancellation-before-IO test and prepared native-bootstrap positive publication/identical-ref replay after synthetic no_faces FAN/skin Params preparation. `go test ./internal/semanticproposal -count=1` passed1.890s; actual PostgreSQL/MinIO publication/replay remains unexecuted. No capability or queue activation added.


### Independent CPU native defect-stage qualification — 2026-10-07

Added private `nativeDefectStageQualification` and `prepareNativeDefectStageQualification`: validates original owned FAN/skin plans and exact defect Params binding/common detector; selects `defects.propose` from current server qualification policy; requires original retained full policy equality and CPU backend; verifies the exact report via existing qualification reader and measured resource-cost lower bounds; rereads both completed native producers and rechecks report/policy after IO. Retains scoped report/policy/costs and both original plan seals. No caller-supplied Expected policy, task publication or admission authority is accepted.

Cancellation-before-IO covered. Local `go test ./internal/semanticproposal -count=1` passed1.897s. Positive current-policy/CPU report and physical native defect execution preparation remain unexecuted; exact catalog sizes/frozen-task compilation, qualification locks and complete admission remain next requirements.


### Native CPU defects qualification transaction composition — 2026-10-07

Added `lockNativeDefectStageInTransaction`: requires qualification bound to original FAN/skin plan seals and retained scoped full server policy; selects CPU defects.propose Expected; checks installed costs against verified measured peaks and serializes/validates exact report proof before SQL; locks both exact completed native dependencies, current full policy snapshot and qualification report in one caller-owned scheduler transaction. Uses existing policy/report lock functions; no new privileges, object IO, commit or task activation. Every failure requires caller rollback.

Cancellation-before-SQL covered. `go test ./internal/semanticproposal -count=1` passed2.068s. Positive composed SQL gate is not yet physically verified. Exact catalog-size/input inventory/frozen CPU task, protected/manual authority, graph budget and full native defects admission/public/durable orchestration remain incomplete.


### Exact native defect task file inventory — 2026-10-07

Added pure `compileNativeDefectInventory` and typed selected file pins: validates original native Params binding and exact own Params SHA/byte count; decodes retained full original WorkingImage/RGB/alpha/dense-warp source pins; selects only actually declared FAN collection and raw skin NPY; accepts additional protection/manual pins only when separately supplied for declared refs. Requires every wanted file exactly once, rejects missing/extra/aliased/mismatched pins/media/version/SHA/size, and returns stable sorted task entries and actual catalog file sizes. No object/catalog lookup authority is granted by supplied additional pins; final protected/manual locks remain required.

Added cancellation-before-data-access and prepared native-bootstrap accepted Params-object/no_faces exact inventory path. Local tests passed2.016s before fixture extension; after fixture extension passed1.811s. Positive SQL/object-store inventory and complete frozen/native defect admission remain unverified/incomplete.


### Native frozen CPU defect task compiler — 2026-10-07

Added `nativeDefectTaskPreparation` and pure `compileNativeDefectTask`: validates retained combined dependency digest; reowns both original FAN/skin plans and seals; matches scoped current full server policy and CPU defects Expected/report; validates installed costs against measured peaks; assembles exact declared inventory/sizes; invokes existing `CompileSemanticDefectTask` with stable `semantic.defect.prepare:reference` run/workflow/task identity and actual file counts. Retains immutable serialized entries/sizes and original dependency/qualification/Params pins. No IO, registration, admission or queue authority. Additional protection/manual pins still require independent original authority at final locking.

Local semanticproposal tests passed1.911s, including cancellation-before-compilation. Focused existing core defect compiler/profile tests passed0.459s. Positive native qualified-report + real catalog integration, graph compilation/budget/registration and full native admission remain incomplete/unverified.


### Owned blocked native defect draft graph — 2026-10-07

Added `nativeDefectDraftPlan`, compiler and seal verifier. Recompiles the complete frozen task from retained selected pins/actual file sizes/current qualification at exact profile limits; requires identical task/inventory/sizes; enforces CPU operation/stable existing defect-preparation run/workflow/task IDs and output+8MiB reservation within JS safe integer. Config uses distinct `v27_defect_native_prepare` marker with original parent/source/intent/snapshot/session/policy/Params and canonical FAN/skin result references. Seal explicitly retains report object pins/backend/contract/metrics, policy/costs, dependency digest, additional file pins and shell. Selected additional pins are copied into preparation for recompile. Cancellation exposes zero plan.

New route has NO Begin/registration/activation fallback; new SQL gates must be implemented before use. Existing imported-origin route is untouched. Local semanticproposal tests passed1.807s before cancellation extension; after cancellation extension passed1.826s. Positive native qualification/pins/graph and physical SQL admission remain unverified.


### Blocked native defect Begin and transport publisher — 2026-10-07

Added beginNativeDefectDraftInTransaction: reowns the complete sealed plan; locks original inventory, published intent, snapshot, source/SFD preparation, both completed FAN/skin dependencies, current CPU qualification policy/report and accepted-version Params pins in the caller transaction; only creates/replays blocked metadata. Protected/manual inputs and additional pins explicitly fail until their original authority is implemented. Caller must roll back every failure; no queue ticket or activation is exposed.

Added publishNativeDefectDraftTransport for object publication outside SQL after blocked draft commit. Validates stable run/workflow, exact single declared frozen task, returned Params/input digest/task identity and transport proof; checks the plan seal again after publication. Registration and activation remain separate and unimplemented for this native branch.

Local semanticproposal tests passed 1.922s for Begin and 1.889s after transport/cancellation coverage. Positive SQL/object-store admission remains unverified; PostgreSQL/Docker runtime is unavailable. This does not advance release acceptance or the estimated 50–60% implementation completion.


### Native defect registration transport binding — 2026-10-07

Added marshalNativeDefectRegistrationBinding: requires a sealed native plan, exact CPU defects.propose v1 task and exactly one declared task/no edges; compares canonical declared task with the frozen task; binds published transport using existing frozen defect compiler; freezes transport proof and verifies task/Params/input digest identities. Canonical packet is bounded to 4MiB; cancellation returns no proof. Separate native binding does not broaden the original GPU registration operation allowlist or imported-origin defect route.

Focused semanticproposal tests passed 1.885s, including cancelled, unowned and forged-plan refusals. Positive qualified native binding/catalog path is still unverified. SQL registration/replay, budget/admission, activation, result guarding, durable reconstruction and protected/manual input authority remain required before this branch is usable end to end.

Backend compatibility check: `go test ./...` passed; semanticproposal executed in 1.692s, other packages mostly cached. This is not evidence of executing database fixtures or real-model/GPU quality acceptance.


### Native defect blocked SQL registration and replay — 2026-10-07

Added forward-only migration 00777 (not applied to production): separate strict bounded native defects.propose CPU binding envelope, private raw missing/replayed registration helper and registered-shell replay wrapper that delegates other branches to the previous matcher. Exact original frozen input entries are now retained in the sealed native config and compared by SQL; duplicate refs, wrong contracts, mixed branch markers, task/run/Params/digest/profile mismatches, partial closure/backend-document ownership and non-blocked state are rejected. No activation or model authority is granted. Envelope is scheduler-only; raw helper has no service EXECUTE grant.

Added registerNativeDefectInputsInTransaction: binds original frozen transport, repeats complete Begin/source/FAN/skin/policy/report/Params authority in the same caller transaction, checks missing/replayed state, registers only missing transport, and verifies exact replay afterward. Every error requires caller rollback. Cancellation-before-SQL covered. Local semanticproposal tests passed 1.922s then 1.809s after cancellation coverage. Integration ACL assertions prepared for eight roles; migration SQL and positive registration/replay/concurrency remain physically unverified. Full native budget/admission/activation/result guards, protected/manual authority and durable/public orchestration remain incomplete.


### Native defect two-transaction persistence and live budget — 2026-10-07

Added persistNativeDefectPreparationInputs: commits only blocked draft metadata, publishes frozen input closure outside SQL, then registers with repeated original authority in a second transaction. Reuses existing coordinator failure/cancellation semantics; no task ticket/activation. Failed publication or registration leaves deterministic blocked slots for future recovery.

Added lockNativeDefectGraphBudgetInTransaction: repeats original Begin authority and exact registered binding before separate graph budget gate. Forward-only migration 00778 (not applied) locks active tenant usage, original draft run/workflow, exact single blocked CPU task/no edges or graph expansions, native marker/stable child identity, completed input closure, operation registry and exact five established defect output roles/media. Locks one live user-owned reservation covering output+8MiB and retained tenant reserved usage. No activation/event/sealing authority. Scheduler-only gate; eight-role physical ACL assertion prepared.

Local semanticproposal tests passed 1.933s before final ACL/test formatting changes. Cancellation-before-infrastructure covered for coordinator and budget. SQL migrations 00777–00778, positive native qualification/registration/budget and object-store transaction boundaries remain physically unverified. Native full admission/activation/result guards, protected/manual authority and durable/public orchestration remain required.


### Complete native defect admission packet — 2026-10-07

Added nativeDefectAdmissionEvidence and marshalNativeDefectAdmissionEvidence. Reowns the sealed complete native task/graph, retains both previously physically verified FAN and skin dependency packets, current full policy, exact CPU qualification Expected/report object pin, accepted-version Params immutable pin and strict frozen registration binding. Bounds canonical complete packet to 32MiB and Params pin to 16KiB. Cancelled/unowned requests expose no packet; no SQL, queue activation or admission authority is granted by serialization. Existing imported-origin branch is unchanged.

Positive qualification/catalog integration and composed SQL admission gate remain required. Evidence retains prior verified packets; live SQL locks must revalidate current state before activation. Protected/manual input authority, native activation/result guards and durable/public orchestration remain incomplete.


### Composed native defect SQL admission barrier — 2026-10-07

Added forward-only migration 00779 (not applied): strict bounded native packet/config marker; exact original parent/request/source/intent/snapshot/policy linkage; FAN and skin result/task/output/Params linkage; stable child/document identity; exact frozen inventory and registration. Composes existing complete FAN/skin result locks, current full qualification policy identical to original retained policy, CPU defect Expected/report lock, accepted-version Params lock, exact registered replay and live graph budget in one caller transaction. Fixed search_path, fn_owner owner, PUBLIC revoked, scheduler-only EXECUTE. No activation/receipt/object IO. Added Go lockNativeDefectAdmissionInTransaction with sealed recompile/serialization, scheduler-role check, scoped tenant setting and conflict-on-refusal. Cancellation stops before SQL.

Local semanticproposal tests passed 1.885s. Eight-role ACL assertion prepared. SQL positive admission/refusal/concurrency and migration execution remain physically unverified; this is not release acceptance. Activation/replay/result guards, protected/manual original authority and durable/public orchestration remain required.


### Native defect atomic sealing and current-evidence replay — 2026-10-07

Added forward-only migrations 00780–00781 (not applied). 00780 adds private sealed registration helper, strict scheduler-only binding replay and full completed-evidence gate, retaining current original FAN/skin, policy/report, Params and exact registered closure authority without requiring a pre-seal quota. 00781 atomically seals/queues/activates CPU task and writes queued event/outbox plus idempotent 202 receipt; original defect-prep key and request hash are required. Receipt replay repeats current complete evidence and editor authorization. Consent links are rejected until explicit native consent admission is implemented. SQL failures roll back all mutation.

Added native defect seal helpers and nativeTicketWithRetry orchestration: bounded returned ticket must match run/workflow/queued; no ticket escapes failed callback/commit. Cancellation-before-infrastructure tests and scheduler-only ACL assertions prepared. Focused package passed 1.946s before final cancellation/ACL extension. Positive SQL activation, registry acceptance, event/receipt atomicity, replay, race/cursor exhaustion and migrations remain physically unverified. Native result guards and durable transport/plan reconstruction, protected/manual authority and public parent orchestration remain required.


### Native defect registered transport recovery — 2026-10-07

Added forward-only migration 00782 (not applied): scheduler-only recovery of exact existing registered native defect transport for blocked or sealed state. Locks original run/task/workflow, verifies original actor/session/project editor authorization and native marker, reconstructs complete saved binding, then requires exact appropriate registration/completed replay gate before returning bounded transport metadata. Missing/partial/substituted state never authorizes repair, publication or recreation.

Added recoverNativeDefectTransportInTransaction with original frozen task binding and scoped scheduler transaction; resumeNativeDefectPreparation recovers and repeats full composed dependency/qualification admission plus atomic sealing in the same nativeTicketWithRetry transaction. Prior dependency evidence is retained in owned plan and revalidated by SQL; no object publication occurs during recovery. Durable plan reconstruction after process restart remains separate and incomplete. Cancellation exposes no transport/ticket; eight-role ACL assertion prepared. Positive SQL recovery/replay/concurrency and migrations remain physically unverified.


### Native defect five-output result fence — 2026-10-07

Added forward-only migration 00783 (not applied): private native-input defect result guard and narrow trigger branch preserving existing skin/FAN/SFD/person/imported-defect/generic V2 branches. Requires stable native child identity, CPU v1 renderer profile, exact current task attempt/fence, noncancelled sealed running run and semantic workflow. Requires exactly five ordered established defect roles/media; unique nonzero refs excluding Params; positive ready verified immutable artifacts produced by that task/attempt/fence; completed matching output-upload rows with equal bytes/SHA/bucket/key/version/media. Mixed branch markers fail. No direct service EXECUTE on helper; trigger owns invocation. Eight-role physical ACL assertion prepared.

No positive native result insertion or physical SQL refusal/race testing has executed yet. Package compatibility check does not prove migration behavior. Native result catalog/object readers and physical NPY/PNG/candidate/report validation, protected/manual authority, durable reconstruction and public orchestration remain incomplete.


### Native-input defect canonical result discovery — 2026-10-07

Added forward-only migrations 00784–00785 (not applied): API/editor-scoped exact current canonical defect result catalog with original parent/request/session, stable child identity, current task attempt/fence, complete FAN+skin canonical dependency identities/source linkage, ordered five ready verified completed-upload artifact pins and accepted-version Params. Exact object endpoint returns only five selected outputs plus own Params for matching result ID. No source-wide latest-result substitution or admission grant.

Added nativeInputDefectResultCatalog reader distinct from existing imported-origin reader, privately owned plan recompile, tenant editor transaction and strict bounded five-output decoder. Rejects output aliases to any declared input or Params and upstream result identity. Missing rows become conflict; cancellation exposes no catalog. Local package passed 1.877s before final decoder/cancellation tests. Physical SQL/migration/catalog and content-byte verification remain unverified/incomplete.


### Native defect exact result object pins — 2026-10-07

Added nativeInputDefectResultObjects: reowns complete sealed native plan, requires five catalog outputs and exact original Params; tenant/editor-scoped query selects only six current result refs in ordinal order. Requires exact ref/bucket/key/immutable version/media/positive size and established defect output byte ceilings (Params bounded to 1MiB); propagates SHA from selected catalog and rejects missing/extra/misaligned rows with zero result. Cancellation before SQL tested. These are catalog object pins, not byte-content or scientific-quality proof.

Local semanticproposal tests passed 1.902s before final explicit per-role size ceilings. Actual pinned NPY/PNG/candidate/report verification and recheck after object IO remain required; migration/catalog execution remains unverified.


### Native defect physical content handoff — 2026-10-07

Added completedNativeInputDefect and readCompletedNativeInputDefect. Reads exact current native catalog; physically rechecks original completed FAN and skin against retained proof; selects six immutable pins; SHA/size-verifies own Params/candidates/report documents and requires exact compiled Params bytes. Reuses existing semantic defect Params/report/candidate binding validators, protection/labels linkage, FaceMask metadata and PNG ref parsing; existing streaming PNG-label equivalence and protected-label-map NPY verification enforce physical pixel consistency. Declared protected/manual coverage is explicitly rejected until its original authority/coverage route is implemented. Re-reads exact catalog/object pins and physically rechecks FAN/skin after IO; any stale state or cancellation exposes zero proof. No SQL admission/model quality claim.

Local semanticproposal passed 1.930s before final cancellation test. Positive real native result documents/pixels, injected corrupt/stale streams and catalog/object-store integration remain unverified. Full protected/manual authority, durable plan reconstruction and public parent orchestration remain incomplete.


### Initial native defect submission coordinator — 2026-10-07

Added nativeDefectSubmission/startNativeDefectPreparation from owned completed FAN/skin face preparation and original registered producer transports. Builds byte-verified combined dependency evidence, independently selects/verifies current CPU defects report/costs, publishes accepted-version Params, rereads exact immutable Params object and byte equality, compiles actual-file-size CPU task/owned graph, commits blocked draft then publishes/registers transport outside SQL, and resumes full SQL admission/seal through committed-ticket retry. Resource/reservation bounds are checked before IO; errors expose no plan/ticket. Existing registered submissions use resume, not this initial route. Protected/manual inputs remain unsupported by current native admission and are not silently stripped.

Cancellation-before-infrastructure test added. Positive qualified native CPU initial submission, current model quality and physical SQL/object-store scenario remain unverified. Public parent orchestration, durable plan reconstruction and full protected/manual scope remain required.


### Native defect early authority scope boundary — 2026-10-07

Extracted shared validateNativeDefectInputAuthorityScope used before initial dependency IO/Params publication and again before Begin SQL writes. Explicitly rejects protected/permanent/manual roots/additional pins while their independent original authority is incomplete; does not strip inputs. Unsupported initial submissions no longer publish Params before later rejection. Covered empty allowed scope, permanent/additional masks, manual roots, additional pins, malformed data and cancellation.

`go test ./...` passed; semanticproposal executed 1.885s, other packages mostly cached. Full protected/manual scope remains a requirement, not removed from the goal. Physical SQL/object-store/real-model acceptance remains unproven.


### Native defect exact bootstrap transport audit — 2026-10-07

Audit identified that native SQL registration constrained direct refs to three declared inventory members, while the frozen task specifically requires Params + original WorkingImage + original RGB pixels. Added exact sorted direct_inputs to privately recompiled/sealed config, derived from original byte-verified source metadata; both blocked/sealed SQL registration now require exact direct array equality. Full admission/completed evidence schema and transport equality include this field. This closes SQL ambiguity among other declared NPY refs without changing existing imported-origin/GPU routes.

Existing generated but physically unapplied migrations 00777/00779/00780 updated consistently with new config schema. Physical migration/replay acceptance remains unverified. Durable reconstruction, protected/manual authority, public orchestration and full V20–V32 acceptance remain incomplete.


### Physical native-bootstrap infrastructure revalidation — 2026-10-07

Ran real native-bootstrap integration command; setup failed with PostgreSQL connection refused at 127.0.0.1:55432 before database creation/migrations/object-store setup (1.05s). Docker socket absent, psql unavailable. No positive migration/native acceptance is established. Hardened initial database fixture connection error to a concise failure without parameter/local-variable traceback (pytrace=False). Verified rerun with --tb=line: same infrastructure failure, 14 deselected/1 setup error in 0.31s; access parameters omitted. No credentials or production state changed.

Remaining implementation can proceed independently; infrastructure is not evidence of goal completion and does not justify marking the whole goal blocked while useful work remains.


### Native defect completed byte-verified evidence packet — 2026-10-07

Added marshalNativeDefectCompletedEvidence: physically rereads complete native result and producer content, requires exact retained completed proof/current result ID, serializes full current native admission and ordered six immutable object pins/result/Params. Reuses shared explicit pin fields and bounded canonical 32MiB packet; no SQL capability granted. Cancellation exposes no packet. Downstream SQL dependency lock remains required; positive real native result/catalog/object-store scenario remains unverified.


### Native defect completed result transaction lock — 2026-10-07

Added forward-only migration 00786 (not applied): strict bounded completed native defect packet and original tenant/project/actor/session/parent/request scope; composes full current completed native admission, locks task/attempt/result/artifact/output-upload rows, rereads exact canonical native result and six immutable ordered object pins, and requires complete packet equality. No byte verification is performed in SQL; packet must come from prior physical verifier. Scheduler-only EXECUTE, fn_owner ownership and fixed search_path. Added lockNativeCompletedDefectInTransaction with sealed plan recompile, scheduler role and scoped tenant check, conflict-on-refusal and cancellation-before-SQL test; eight-role ACL assertion prepared.

Positive SQL result-lock/revocation/upload/catalog mutation concurrency and migrations remain physically unverified. Public parent integration, durable plan reconstruction, full protected/manual authority and V20–V32 acceptance remain incomplete.


### Server-owned defect bootstrap integration — 2026-10-07

Added nativePreparationBootstrap.bootstrapDefect and typed original producer transports: validates configured shared reader/publishers/scheduler and resource/reservation bounds; physically prepares native FAN/skin Params then invokes the complete initial native defect publication/register/admission/seal coordinator. Ticket remains a child receipt and cannot substitute parent acceptance. Only currently implemented unprotected native scope is wired; full protected/manual scope remains required.

Inspection confirms existing transfer coordinator expects imported-origin native defect provenance, so direct replacement would bypass/change that contract. Public parent continuation requires an explicit new-native adapter and corresponding complete authority locks; it is not implemented by this helper. Cancellation/unconfigured bootstrap tests prepared; positive qualified end-to-end bootstrap remains unverified.


### New-native defect protection document preparation — 2026-10-07

Added nativeInputDefectProtectionPreparation/prepareNativeInputDefectProtection: reads full byte-verified current new-native result; builds established semantic protection document from exact own Params/report/candidates/original source; requires generated pixels equal verified protection output; repeats complete physical/current result after compilation and rejects any drift. Uses stable existing semantic.defect.protection task document identity. Retains distinct private new-native plan/result, never forges the imported-origin parent proof or registered root. Cancellation exposes no document/pixels.

This prepares parent transfer input but does not publish/register/authorize it; new-native publication and continuation locks still required. Positive native content/protection and public parent transfer integration remain unverified/incomplete.


### New-native protection accepted-version publication — 2026-10-07

Added publishNativeInputDefectProtection: uses existing quota-fenced accepted-version semantic protection publisher after full physical native result preparation; requires returned deterministic document ID/exact SHA; repeats complete result/content preparation after PUT and rejects drift/cancellation with zero root. Retains a distinct private published preparation. Publication explicitly does not register producer authority or authorize transfer; original completed-result and published-object registration locks remain required.

Positive new-native publication/replay/revocation races remain physically unverified; public parent transfer and protected/manual scope remain incomplete.


### New-native protection provenance registration — 2026-10-07

Added forward-only migration 00787 (not applied): distinct new-native registration function reuses existing insert-only semantic_native_protections table, existing scope/retention/GC/purge permissions; composes full completed native result lock, exact original anchor/source/version/session, canonical result/report/body hashes, protected mask/pixel identity and ready accepted-version upload/object. Exact idempotent replay; no new table/service DELETE or parent activation. Added scheduler transaction adapter building full physical packet before SQL and repeating result locks. Corrected publication adapter to respect separate backend document ID and allocated artifact ID, checking nonzero nonpixel root and exact SHA rather than equating these identities.

Local package passed 1.922s before artifact-identity correction. Positive SQL registration/publication/replay/concurrency remain unverified. Registered-root reader and new-native parent transfer continuation still required.


### Registered new-native protection object reader — 2026-10-07

Added forward-only migration 00788 (not applied): separate API/editor-scoped exact registered new-native root/pixel reader. Requires original anchor/session/source/version and new-native task config, current canonical defect result/dependencies, exact immutable provenance registration/result/report/pixel identity, ready accepted-version Document upload/root and original defect protection object. Existing imported-origin reader unchanged. Added registeredNativeInputProtectionObjects with private plan recompile, original Params/catalog consistency, tenant editor transaction, exact two refs/bucket/version/media/size/ordinal and missing/extra-row refusal. This selects metadata, not physical content or transfer admission.

Semanticproposal passed; positive SQL/read/revocation/content integration remains unverified. Final physical registered-root reader and parent continuation still required.


### Registered new-native protection physical reader/coordinator — 2026-10-07

Added readRegisteredNativeInputProtection: re-verifies full physical native result against retained proof; requires exact two registered root/pixel pins and equality to verified result protection pin; SHA/size reads root document, exact expected bytes and source-bound protected MaskRef pixels. Rechecks registered pins and full physical result after IO, returning zero on drift/cancellation. Added private publishRegisteredNativeInputProtection coordinator for publication, SQL registration and final physical reading without object IO spanning SQL. Result is private registered provenance, not parent approval/admission.

Cancellation-before-IO covered; semanticproposal passed 1.894s before coordinator and final package check passed (tool output). Positive real PostgreSQL/MinIO/qualified models and parent transfer integration remain unverified/incomplete.


### Registered new-native protection transaction authority — 2026-10-07

Added forward-only migration 00789 (not applied): locks original execution scope/person inventory, full completed native result, exact immutable provenance record, root/pixel artifacts and accepted-version upload; compares exact two selected registered object pins. Separate scheduler-only function/fixed search_path preserves imported-origin route. Added SQL-only lockRegisteredNativeInputProtection composing current completed result and verified root/body/pixels consistency, original anchor and exact object pins in caller transaction. Caller must roll back every error; this is reference authority, not target qualification or parent graph activation.

Package check passed. Positive SQL locking/revocation/upload/provenance races and migrations remain physically unverified. Parent transfer adapter and durable plan reconstruction/protected-manual scope remain incomplete.


### New-native source/defect reference continuation — 2026-10-07

Added nativeInputSourceContinuation composition and transaction adapter. Pure scope binding requires original new-native plan, exact parent run/request/source tenant/project/actor/session/bucket/ICC and complete identical retained RGB/alpha/warp source proof; verified registered root/body/pixels must match preparation. SQL adapter locks original complete source plus completed native result/protection provenance in one caller transaction without object IO. Person coverage, transfer model, target authority and parent graph admission explicitly remain separate mandatory work. Cancellation before SQL exposes no proof.

Package checks passed. Positive new-native continuation and full transfer integration remain unverified/incomplete.


### New-native reference person qualification/coverage — 2026-10-07

Added prepareNativeInputReferencePerson: composes exact new-native source/protection scope, rereads server full policy identical to original native qualification policy, selects original mask.person expected and uses existing independently qualified actual person-pixel coverage reader for registered protection root. Rechecks complete registered physical result/root, original source and policy after coverage IO. Added lockNativeInputReferencePerson composing source/registered-native authority, current full policy snapshot and original person coverage locks in one caller transaction. Reference coverage only; target coverage, transfer qualification and full parent graph integration remain required.

Package check passed. Positive new-native person coverage/current-policy refusal and SQL transaction integration remain physically unverified; existing scope/coverage readers may reject incomplete roots rather than generate fallback.


### New-native person selected-policy consistency audit — 2026-10-07

Strengthened lockNativeInputReferencePerson to validate scoped full policy and exact canonical person Expected equality to mask.person selected from that policy before SQL source/protection locks. Independent generic person qualification locks remain unchanged. Added cancellation-before-IO/SQL coverage for preparation and lock. This prevents privately retained person proof from using a different enabled expected policy than the original native policy snapshot.

Full backend compatibility check executed; physical SQL/new-native person coverage and complete transfer integration remain unverified.

### New-native selection separation — 2026-10-07

Overall implementation estimate remains 50–60%, not release acceptance. Added a distinct native-input selection proof, without converting imported-origin protection authority. The verifier reads source-bound selection objects and registered native protection, verifies pinned NPY bytes using zero-overlap/nonempty mask validation, then repeats selection, protection and original-source checks. Cancellation produces no authority before object/database IO. Package Go tests passed; this is not a positive database/runtime acceptance result. Transfer transaction wiring, durable recovery and physical validation of migrations 00766 onward remain incomplete. Dedicated test PostgreSQL was unavailable at the last physical attempt; no production migration was executed.

### New-native selection transaction binding — 2026-10-07

Added SQL-only `lockNativeInputSelectionSeparation`: repeats original source/protection composition, requires exact retained registered proof, locks native input source and completed protection evidence, then locks the two selected-mask object pins through existing migration 00731. No object IO occurs inside the transaction. Pin serialization is shared with the imported-origin route; native request scope remains independently bound to original detector intent, tenant, run/request digest, session, project and bucket. Tests cover valid serialization and rejection of snapshot/tenant/session mutation, empty selection, null object version and incorrect SHA, plus cancellation before transaction access. Package Go tests passed (1.906s). This helper is not yet wired into final transfer graph admission; SQL execution and positive physical native transfer acceptance remain unverified. Overall plan is not complete.

### New-native transfer reference documents — 2026-10-07

Implemented a distinct typed reference document package for original native-input source authority: WorkingImage, unprotected selected MaskRef and registered protected MaskRef. Preparation independently rereads person coverage and nonempty/zero-overlap selection before and after document IO, reserves the complete document budget, SHA-verifies pinned documents and resolves exact mask children/source binding. A SQL-only combined lock validates all three document SHA values and mask children, then composes source/protection/person/policy and selection locks in the caller transaction. Imported-origin authority is not converted. Package tests pass, including cancellation, invalid budgets and absent transaction refusal. Final transfer compiler/coordinator wiring and positive DB/object-store acceptance remain incomplete; this is not full V27 completion.

### New-native person coverage provenance correction — 2026-10-07

Inspection found that new-native reference person preparation still called generic `selectPersonProtectionProof`, which requires generic protection preparation authority. Replaced it with distinct native-input person coverage: unique independent candidate, exact qualification/report and producer evidence, SHA-pinned streaming required-person vs registered-native-protection coverage, then repeated candidate/evidence/object/report/protection checks. Reference proof now retains a dedicated native-input person proof rather than a generic protection proof. SQL locking repeats person evidence, qualification and unique candidate under the tenant inventory barrier after the caller locks original source and completed native protection; no generic protection provenance is manufactured. Cancellation/missing-TX tests added and existing cancellation expectations updated. `go test ./...` passed (semanticproposal 1.884s). Positive physical DB/object-store coverage remains unverified; final compiler/coordinator integration remains outstanding. This correction supersedes earlier journal statements that native reference person coverage used the existing generic reader successfully.

### New-native transfer batch preparation — 2026-10-07

Added distinct typed staging inputs and native-input batch preparation. Pure validation binds source/protection/person/selection, exact document SHA and children, deployment person report/expected and full selected semantic.transfer/person policy. Preparation preserves existing target inventory, independent transfer qualification/measured resource checks, scoped qualified target sources, generic target protection/person coverage, frozen profiles, metadata limits and end-of-IO inventory/source/report checks. Target person evidence/location metadata is charged even though reference uses native authority. Full physical reference is reread after target IO and compared exactly. Tests verify cancellation returns no partial batch and missing person policy refusal; package Go tests pass. Staging type intentionally cannot enter the legacy compiler: distinct native-input compiler binding and final admission are still required. No SQL/physical acceptance claim.

### Distinct native-input transfer compiler — 2026-10-07

Added `nativeInputTransferWorkflowPlan` and a dedicated typed compiler from native staging inputs. It checks exact reference equality for all targets, original source pins, per-target source/person/protection scope, full transfer qualification proof/pin/expected equality, reference/person selected policies and exact reference document bytes. Frozen semantic tasks are compiled by the existing document-aware compiler; the core retains target ordinal, source and protection pairs, target person proofs and qualification. The distinct wrapper preserves native-input reference authority and does not enter generic/imported-origin admission. Cancellation and empty batch tests added; package Go tests pass. Final native-input scope validation, complete transactional admission/seal, coordinator wiring and positive SQL/object-store fixture remain unfinished. No admission ticket or SQL migration was executed.

### Native-input transfer prepared-input transaction gate — 2026-10-07

Added complete typed plan scope validation: task/frozen/source/protection/person cardinalities, original request/run/tenant/project scope, frozen-to-draft equality, target ordinal/source/person protection and policy/report equality, exact reference authority and independently marshalled transfer qualification. Recompiles core WorkflowPlan from retained snapshot/frozen tasks to verify config/configSHA/policy/snapshot consistency. Added SQL-only prepared-input gate composing original inventory, native reference/person/selection/completed protection, transfer qualification, all target sources and target person/protection locks. Tests reject incomplete/mixed authority before any SQL and cancellation before transaction access. Package Go tests pass. This gate alone does not register, seal or activate graphs; final transport/quota/snapshot admission and coordinator remain unfinished, as does physical SQL validation.

### Native-input transfer frozen inventory binding — 2026-10-07

Strengthened new-native plan scope validation to bind actual frozen worker inventory to locked authority rather than only matching task metadata. Added bounded compiler-owned `FrozenSemanticTask.InputEntries` (no external cursor/object IO). Each ordinal checks ProtectionPin roots, then compares exact Params/intent/calibration, source/target WorkingImage and all verified source pixel/alpha/warp pins, selected root/pixels and protected roots/pixels against frozen entries. Missing/extra/changed/duplicate entries refuse; shared identical desired refs are deduplicated. Unit tests cover these mutations. `go test ./internal/semanticproposal ./internal/v2input` passed (2.406s/5.817s), then semanticproposal after new mutation tests passed (1.928s). This validates Go contracts only; new-native registration/seal/coordinator and positive DB/object-store acceptance are still outstanding.

### Native-input transfer transport registration — 2026-10-07

Added a distinct SQL-only transport registration path for the native-input wrapper plan. It validates full scope and published inventory/intent, binds only compiler-owned frozen transport, repeats full prepared reference/target authority locks, published intent, snapshot and qualification, then handles exact missing/replayed registration for every task. All errors require caller rollback; activation is not performed. Tests reject cancelled/nil/incomplete registration before SQL; semanticproposal tests pass (1.972s). Actual SQL registration with new-native protected provenance is not yet proven, and blocked-draft/coordinator/seal/recovery remain outstanding. No positive runtime acceptance or full-plan completion claim.

### Native-input transfer blocked draft begin — 2026-10-07

Added distinct blocked-draft begin: recompile original request from key/body, verify original run/workflow/request SHA and full typed native plan, exact published intent/inventory, construct reserved-byte shell, lock complete native reference/target/publication/snapshot/qualification admission evidence, then call existing blocked admission. Only created/pending states return draft metadata; expired/foreign-run/unknown/conflict and sealed replay refuse without an exposed admission. Tests cover cancellation and invalid actor before SQL, successful blocked outcome parsing and all refusal states. Package Go tests pass (1.923s). No graph activation, external publication or migration was performed; physical blocked-draft success and new-native registration remain unverified. Coordinator, seal/recovery and full V20–V32 completion remain pending.

### Native-input transfer persistence coordinator — 2026-10-07

Added blocked commit → external frozen transport publication → registration commit coordinator for a complete native-input transfer plan. Rebuilds physical completed-defect evidence before draft SQL and again after publication, rereads full reference documents after publication, and registers only compiler-bound transports. No SQL transaction spans object IO; no graph activation or queued ticket is exposed. Tests verify exact stage order and no result after begin, first commit, publication, registration or second commit failure. Package Go tests pass. This is a prepared-plan persistence coordinator, not yet the public request/API wiring; physical SQL acceptance, activation and authenticated durable recovery remain outstanding.

### Native-input transfer initial activation — 2026-10-07

Added initial unsealed activation wrapper: validates complete typed plan and frozen transport, rereads physical reference and requires exact retained equality, rebuilds completed-native-defect packet before SQL, then uses commit-aware ticket retry. Transaction recompiles original keyed request, repeats full registration/admission locks, seals through existing SQL graph quota/activation contract, and checks a bounded queued ticket against exact run/workflow. Tests cover ticket identity/state/malformed refusals, cancellation and missing scheduler; semanticproposal tests pass. This is not authenticated sealed replay/restart recovery or public API wiring. Positive physical registration/seal and compatibility with live SQL guards remain unverified; no production migration or graph activation was executed during this turn.

### Native-input transfer sealed replay — 2026-10-07

Added read-only sealed replay for the distinct native-input wrapper: recompile original keyed request, validate full plan/published identity, bind exact frozen transports, repeat all native/reference/target/intent/snapshot/qualification locks, and verify each stored sealed task/closure through existing completed-state SQL before returning the bounded original queued receipt. Activation now locks authority before observing receipt and chooses exact replay or initial seal in the same commit-aware transaction. Replay performs no registration/seal/publication/event writes. Tests cover cancelled/nil/incomplete replay before SQL; semanticproposal passes (1.976s). This supports replay of an already reconstructed plan with retained transports; restart reconstruction and loading stored transfer transports are still missing. Positive physical SQL execution and API wiring remain unverified.

### Stored native-input transfer transport recovery — 2026-10-07

Added forward-only migration 00790 with scheduler-only exact registered transfer transport lookup, original creator/session/editor authorization and existing blocked/sealed registration-state validation. Added SQL-only recovery under complete original admission evidence locks: load each saved transport, strictly decode and bind it to the compiler frozen inventory, then replay exact sealed receipt or seal the registered blocked graph. Missing/partial/foreign transport refuses; no object publication or repair. Go tests pass; cancellation/incomplete scope tests added. Migration 00790 was not executed and has no physical SQL acceptance yet. Full plan reconstruction after process restart, public coordinator/API wiring and positive database/object-store acceptance remain outstanding.

### Native-input transfer physical resume boundary — 2026-10-07

Added commit-aware resume wrapper for reconstructed plans, loading registered transports through SQL recovery without publisher/draft creation. Shared physical preflight now rereads full reference, transfer qualification/measured costs, each qualified target source and target person/protection coverage, repeats reference after later target IO and finally rechecks original target source/protection and qualification pins. Activation and post-publication registration coordinator use this complete preflight. Added cancellation/missing scheduler tests; semanticproposal Go tests pass. Full durable reconstruction of native producer/transfer plans, API wiring and positive SQL/object-store validation are still outstanding; migration 00790 remains unapplied/unverified.

### Native-input recovery ACL and physical attempt — 2026-10-07

Recovery now recompiles original key/body and rejects run/workflow/request/actor mismatch before any SQL locks. Expanded native-bootstrap integration ACL coverage: 00787 registration, 00789 protection lock and 00790 saved transfer transport are scheduler-only across eight service roles; 00788 protection object reader is API-only. Package Go tests pass. Retried dedicated native-bootstrap pytest with safe short traceback: setup failed because PostgreSQL at 127.0.0.1:55432 refused connection (1 error, 14 deselected, 1.06s). No migrations executed. Docker socket and local postgres/pg_ctl executables were absent in this check. SQL/migration/runtime acceptance remains unproven; implementation can still progress without the physical fixture.

### Native-input transfer initial request orchestration — 2026-10-07

Added internal server-owned `prepareAndActivateNativeInputTransfer`: preflight deployment dependencies/configuration, keyed request, physical native reference/person/selection, scoped intent publication, complete target batch preparation, deterministic Params and target snapshot publication, distinct frozen plan compilation, blocked/publication/registration coordinator, then commit-aware initial activation. All failures return no ticket. Cancellation and unavailable dependencies refuse before publication; semanticproposal Go tests pass (1.969s). This is not a public API implementation: independent native authority selection/reconstruction and initial-vs-recovery routing remain necessary. SQL and positive physical runtime acceptance remain unproven while dedicated PostgreSQL is unavailable. Full V20–V32 goal remains incomplete.

### Native-input transfer reconstruction boundary — 2026-10-07

Added reconstruct-and-resume request boundary with no document/transport publisher dependency. It consumes server-loaded original published intent/inventory, snapshot pin/body and Params refs; recompiles keyed request, rereads physical native reference, prepares complete live target batch, compiles distinct frozen plan and resumes saved registered transport through commit-aware recovery. Missing/cancelled metadata/dependencies refuse before IO. Strengthened plan scope: target protection root/pixel must exactly equal retained target-person protection evidence, closing a child-ref substitution gap. Go semanticproposal tests pass (3.033s). The server reader loading persisted metadata and original native producer plans, routing/API integration, migration execution and positive physical acceptance remain unfinished. This boundary does not itself prove restart recovery end-to-end.

### Native-input saved transfer config reader — 2026-10-07

Added forward-only migration 00791 with API-only read of bounded original transfer config/hash using tenant/project/editor/creator/session authorization. Go reader verifies canonical config hash, original project/session/snapshot, selected qualification, exact deterministic task ordinals and source/target protection refs, then returns only original snapshot and Params pins. Cancellation/absent authority tests added; Go package tests pass. Migration is not physically executed. This is locator loading only: physical saved snapshot/Params reads, original producer reconstruction and full routing/API integration remain unfinished. Do not treat config locators as completed admission authority.

### Saved transfer config positive locator and mutation tests — 2026-10-07

Added positive saved-config locator decoding for a two-target original configuration and mutation tests for session, deterministic task ordinal, duplicated Params ID, snapshot ID, target protection and config hash. Decoder now requires exact typed canonical config shape (unknown fields refuse) and unique Params artifacts across task ordinals. These are config/locator tests, not a positive qualification, database or transfer runtime acceptance result. Semanticproposal tests pass (2.048s). Physical saved snapshot/Params reader, full native producer reconstruction/API integration and migration/runtime validation remain outstanding.

### Physical saved transfer snapshot reader — 2026-10-07

Added forward-only migration 00792, API-only original document object lookup for saved snapshot (-1) and Params task ordinal (0–4999). Requires original creator/session/project/editor authorization, exact config pin, deterministic document UUID, ready upload and verified versioned artifact. Added physical snapshot reader with metadata budget, exact SHA-pinned bytes, normative snapshot digest/ID/count verification and post-IO object-pin recheck. Cancellation/invalid ordinal tests added; semanticproposal Go tests pass. Migration 00792 remains unapplied/unverified physically; Params content validation, full original producer reconstruction and API recovery integration remain pending.

### Physical saved transfer Params and recovery integration — 2026-10-07

Added saved Params payload validation: reserve whole batch metadata/object-byte budget before payload reads, SHA-read each exact saved object, strictly decode Params and compare canonical bytes plus original intent/reference/target/model/calibration, then repeat all object pins after later IO. Reconstruct-and-resume now loads actual saved config, requires exact caller snapshot/Params pins, reads physical saved snapshot, compares bytes and verifies all saved Params before rebuilding the plan. Positive Params contract test and mutation tests cover all binding fields/unknown fields. Go semanticproposal tests pass; no database/runtime acceptance claim. Full native producer plan reconstruction, public routing and migrations 00790–00792 physical checks remain outstanding.

### Native-input transfer saved metadata loading and original config match — 2026-10-07

Added load-and-resume boundary that internally discovers original saved config/snapshot/Params and retains published intent/inventory from original private detector authority. Reconstructed transfer config SHA must equal saved original SHA, and saved metadata is reread after all physical/reconstruction IO to reject drift. Expanded integration ACL fixture to API-only saved config/document readers (00791/00792) across eight service roles. Cancellation/dependency tests added; semanticproposal Go tests pass. The original native producer/person proof still must be reconstructed by a server reader; no claim of complete process-restart recovery or API integration. SQL migrations and positive physical acceptance remain unverified.

### Original native-input defect chain locator — 2026-10-07

Added API-only bounded original native-defect saved config reader (migration 00793) and Go locator decoder. Verifies canonical config hash plus original project/parent/request/session/source/intent/snapshot, deterministic defect task and Params document, and original landmarks/skin run/task identities. Locator is not qualification/content/admission authority; actual original Params/producers must still be read and reconstructed. Cancellation/absent scope tests added; semanticproposal Go tests pass. Migration 00793 and positive SQL/runtime acceptance are unverified. Full producer reconstruction, protected/manual input authority and public API integration remain outstanding.

### Physical original native defect Params reader — 2026-10-07

Added API-only original accepted native-defect Params object reader (forward-only migration 00794), scoped to original parent/draft creator/session/project/config pin and deterministic Params document. Go reader SHA-verifies physical canonical bytes and repeats saved config locator and original object pin after IO. This returns original bytes for subsequent producer reconstruction, not qualification/content/admission authority. First normal Go test failed at linker with no space left on device; disk check showed 191 MiB available. Same package tests succeeded using `go test -ldflags='-s -w' ./internal/semanticproposal` (1.909s), which removes debug symbols only; no unrelated data/cache was deleted. Cancellation test added. Migration/runtime acceptance remains unverified and full producer reconstruction/API integration remains outstanding.

### Saved native defect Params recovery boundary — 2026-10-07

Added internal restoreNativeInputDefectFaceParams: sealed original FAN/skin plans, exact request/intent and saved producer identities, physical producer rereads, recompilation and declared-input/face validation, exact saved Params digest/bytes, followed by saved config/object/body rereads. No replacement publication occurs. Cancellation refuses before IO. Original producer-plan reconstruction and public API recovery routing remain outstanding; this boundary alone is not process-restart acceptance. Integration ACL assertions now cover API-only readers 00793/00794 across eight service roles, but migrations remain physically unverified. Normal semanticproposal package tests passed (4.153s). Earlier disk failure was resolved by cleaning only repository Go build cache; preceding normal package rerun passed (6.392s). Overall implementation estimate stays 50–60%, an expert range rather than measured acceptance coverage.

### Original native producer plan reconstruction — 2026-10-07

Closed a persistence gap for newly compiled original stages: detector/FAN/parser/skin/native-defect configs now retain versioned recovery metadata with exact qualification-report ref (except unqualified CPU skin derivation), original additional model costs and compiled RAM/VRAM/scratch/output estimates plus reservation. Detector/FAN/parser private plan fingerprints also cover retained additional costs. Frozen processing profiles, Params algorithms and GPU allowlists were not changed. Existing stored configs are not rewritten or given fabricated recovery inputs; graphs without this metadata cannot use the new reconstruction path. Compatibility with already persisted native drafts therefore still needs physical validation and a deliberate recovery strategy.

Added forward-only 00795, API-only bounded original stage config/task/profile locator reader scoped to tenant, project/editor, actor/session, parent/run and operation allowlist. Go validates canonical config hash, deterministic stage/run/task/Params-document identity, request/source/intent/snapshot bindings, input digest, exact recovery schema and profile limits. This locator is not registration/qualification/result authority. Reviewed task schema and corrected Params SHA lookup to join ready verified artifacts rather than assuming a task Params-SHA column.

Added restoreDetectorDraftPlan, restoreNativeFaceDraftPlan, restoreNativeSkinDraftPlan and restoreNativeInputFaceProducers. They SHA-read original ready Params, physically requalify current original report/source and completed producers, recompile with saved costs, require exact original config hash/task/input digest/profile/reservation, repeat Params/config reads and policy after later IO. Detector Params are compared byte-for-byte with canonical compiler output. These paths accept no publishers and do not create drafts, transports or tickets. Full transport/dependency/native-defect/protection recovery and public API integration remain outstanding.

Unit tests exercise exact original recovery inputs/report ownership, all five stage locators, scope/hash mutations, invalid schema/profile budgets, synthetic CPU-skin qualification refusal and cancellation before IO. semanticproposal tests passed after changes (1.929s); final semanticproposal/v2input checks passed (1.952s/cached). Added physical integration assertions comparing all four reconstructed producer seals with originals and expanded eight-role ACL assertions for 00795. Physical native-bootstrap pytest could not start: dedicated PostgreSQL 127.0.0.1:55432 refused connection (1 error, 14 deselected, 1.05s); no migrations executed. Thus migrations 00766–00795 and positive producer reconstruction runtime remain unverified. Full V20–V32 objective stays active and incomplete.

### Native producer transport, defect plan and protection restoration — 2026-10-07

Added SQL-only recovery of original detector/FAN/parser/skin transports in one scheduler transaction. All four private plans/scopes are checked before SQL; existing registered-transport readers and frozen BindPublishedTransport validators are reused. Missing/foreign/partial state returns no transport packet. The pool wrapper returns bindings only after successful commit, including retries of the complete DB-only callback; object IO is outside these transactions.

Added original native-defect dependency reconstruction: saved Params are compared with physically reverified original FAN/skin inputs; completed dependency evidence uses recovered transport; all four transport bindings are recovered again after later physical IO and must remain identical. Added restoreNativeInputDefectDraftPlan using saved qualification report/costs/profile/reservation, exact Params object, dependency requalification, pure recompilation, original config/task/input-digest/profile match, and final saved config/object/policy rechecks. This still requires independently selected original diameter/protected/manual inputs; unsupported native manual/protected authority is not fabricated or bypassed.

Added forward-only 00796, API-only original registered-protection locator through existing immutable provenance table and exact object/result reader. restoreRegisteredNativeInputProtection reconstructs the protection body from byte-verified completed native defects, loads an existing registered root, validates exact raw-byte SHA/document/result, performs existing physical registered-root checks, then rereads the locator. It does not publish, repair or register a replacement. Expanded eight-role ACL fixture to this function.

Added cancellation/incomplete-plan refusal before SQL and exact protection body/document/result mutation tests. Native-bootstrap fixture now compares the aggregate recovered four transports with original transport; fixture compiles but remains physically unexecuted. Initial compile found a missing reflect import in the fixture, which was fixed. Final semanticproposal/v2input checks passed (1.914s/cached). Dedicated PostgreSQL was unavailable on the last physical attempt; no new claim of migration, model-quality or end-to-end recovery acceptance is made. Migrations 00766–00796 remain physically unverified. API routing, original protected/manual input selection, full request-level recovery and V20–V32 acceptance remain incomplete; objective stays active.

### Original native transfer recovery orchestration — 2026-10-07

Added internal restoreAndResumeNativeInputTransfer, connecting original saved detector/FAN/parser/skin reconstruction, native-defect dependency/qualification/config reconstruction, commit-aware saved defect transport read, physical registered protection reconstruction and independent person coverage to existing saved transfer reconstruction/admission recovery. It does not accept externally supplied private producer/person/defect authority and has no publisher or draft-creation dependency. Exact reconstructed protection must equal deployment-selected SourceProtection. Final transfer recovery still repeats physical content and complete SQL admission authority before returning a commit-confirmed ticket. All failures return no ticket/bindings.

The original published intent/inventory and snapshot plus original diameter/protected/manual input selection still must be loaded by the server. This is not yet a standalone public API recovery route, and unsupported native protected/manual authority is not bypassed. Added cancellation/missing-dependency refusal tests, including saved defect transport recovery. semanticproposal tests passed (1.990s). No new physical PostgreSQL/MinIO/model runtime result; migrations 00766–00796 remain physically unverified. Original metadata/input selection, public initial/recovery routing and full V20–V32 acceptance remain outstanding. Goal stays active.

### Original native intent/inventory and snapshot loading — 2026-10-07

Added forward-only 00797, API-only original intent object locator anchored to the saved deterministic detector run, original actor/session/project/version, config intent pin and deterministic intent document. Ready verified immutable object/upload metadata must match. Go readNativeInputSavedPublishedIntent reads complete live inventory before/after physical original intent bytes, requires exact canonical keyed request body, reserves metadata budget before object IO and repeats original object pins. No publication occurs.

Added loadOriginalAndResumeNativeInputTransfer, which loads intent/inventory and saved transfer config/snapshot internally, rechecks saved config, then invokes original producer/defect/protection/person/transfer reconstruction. Original diameter/protected/manual input selection and public initial/recovery API routing remain server implementation duties; this is not a public route or full request acceptance result. Expanded native-bootstrap fixture with original published-intent equality and eight-role ACL assertions for 00797; fixture compiles but is physically unexecuted. Cancellation/absent-budget/dependency tests added.

Initial normal Go build failed due disk exhaustion; stripped-link retry also failed while writing a package archive and did not verify changes. Inspected only repository work/go-sqlc-cache (497 MiB, ignored standard Go build-cache README/hex layout), then cleaned that reproducible build cache with GOCACHE=<repo>/work/go-sqlc-cache go clean -cache. Source files, processing evidence, databases, module cache and object data were retained. Free space then approximately 699 MiB. Normal semanticproposal tests passed (1.959s). Migrations 00766–00797 and positive SQL/model/complete recovery runtime remain physically unverified. Full V20–V32 goal remains active and incomplete.

### Original defect processing range loading — 2026-10-07

Added review.DefectProposalDiameterFromParams: exact raw-byte SHA, strict full original Params schema and source/canvas/transform/mask/face validation precede returning the original diameter_face_fraction range. It does not establish mask provenance or model/admission authority. Added readNativeInputSavedDefectDiameter: loads original accepted Params, reads the pinned source WorkingImage, validates the original range, then repeats saved Params/config/object observations after source IO.

Original transfer recovery loader no longer accepts a repeated diameter argument: it derives the value from saved original Params before producer/defect reconstruction, preserving custom original ranges rather than substituting defaults. Protected/manual input authority selection and public routing still remain unfinished and are not bypassed. Added positive custom-range test (.003–.008) and SHA/image/canvas/bytes/reversed-bounds/unknown-field refusals. review and semanticproposal tests passed (0.917s/1.845s). No migration or physical model/runtime acceptance claim; prior migrations 00766–00797 remain unverified in dedicated PostgreSQL. Full V20–V32 objective remains active.

### Original protected/manual policy declarations — 2026-10-07

Added review.DefectProposalInputPolicyFromParams, returning original diameter, permanent protection, additional protections and complete per-face skin/manual bindings only after exact SHA, strict full Params schema and source/canvas/transform/mask/face validation. JSON decoding owns every returned mutable collection/pointer; mutation of a selected policy must not modify the saved original. Existing diameter helper now uses the same full decoder.

Added readNativeInputSavedDefectPolicy with the existing original saved Params/object/config reads, source WorkingImage binding and post-source-IO saved packet rereads. This closes policy-declaration loading only: declarations are locators, not independent manual/protected authority. Original raster/vector proof readers remain separate; native authority scope refusal has not been removed, and native per-face manual binding, protected input selection, final physical/SQL proof integration and public routing remain unfinished.

Extended positive original-policy/custom-range and ownership tests; previous source/SHA/schema mutation refusals still apply. Cancellation before saved policy IO exposes no mask declarations. Initial review/semanticproposal checks passed (0.905s/1.947s); final review/semanticproposal checks including cancellation passed (cached/2.199s). No new migration or physical runtime acceptance claim. Full V20–V32 objective stays active.

### Native face bindings and original manual preflight — 2026-10-07

Added verifyNativeInputSavedFaceBindings: bounded owned copies of saved face declarations must exactly match independently verified original FAN/skin bindings, excluding only manual declarations that require their own producer authority. Missing faces/foreign landmarks and cancellation refuse; original collections are not mutated.

Added readNativeInputSavedManualInputs: validates all original private producer plan scopes, loads source-bound saved policy, physically rereads original native landmarks/skin, checks exact face bindings, then uses existing independent original manual raster/vector closure reader before a defect producer is required. Retains owned proof objects and original manual root bytes; deduplicates only equal complete proofs for the same pixel ref; bounds retained root metadata to 4 MiB. Every manual proof and both native producer results are reread after the complete batch, followed by saved policy and deployment-policy rechecks. No imported FAN/skin proof or empty fallback is substituted.

This is internal preflight only. Native defect input-authority refusal remains in place: manual Params binding, extra inventory/qualification evidence, final transaction locks, protected-input selection and public routing still need integration before these inputs can execute. Tests cover original fixture face bindings, foreign landmarks, missing faces and cancellation before manual IO. Earlier package checks passed (1.938s/1.996s); final semanticproposal checks passed (1.937s). No physical SQL/model/manual end-to-end acceptance claim. Full V20–V32 remains active and incomplete.

### Original manual worker inventory pins — 2026-10-07

Added nativeInputManualInventoryPins and retained pins in nativeInputManualSelection. Checks complete independent manual evidence shape, exact saved original root bytes/SHA/size and target artifact binding before selecting root MaskRef, target NPY and protected NPY. Deduplicates only identical artifact/ref/object/contract pins and orders deterministically. Manual task Params, PNGs and external vectors remain complete retained proof evidence for eventual admission locking; they are not added as undeclared defect-worker downloads.

Selection is integrated at the end of native manual preflight, after physical producer/manual/policy rereads. Native defect admission refusal has not been relaxed: actual manual Params binding, final transaction proof locks, protected-source policy and public routing remain outstanding. Unit shape/pin tests cover exact declared files, foreign target and cancellation; these are not raster/model runtime acceptance. semanticproposal tests passed (1.941s). No new migrations or positive SQL/manual end-to-end claim; full V20–V32 goal stays active.

### Original manual Params recompilation and exact replay — 2026-10-07

Native manual preflight now recompiles the original complete defect Params from physically verified original native source/FAN/skin bindings, saved diameter/permanent/additional declarations and verified original manual roots. It uses existing review.BuildDefectProposalTaskInputsFromImage and freezeCompiledDefectParams, preserving automatic derivation/deduplication of manual protected children. The complete compiler output is retained as immutable strings alongside the saved Params ref; exact saved bytes and digest must match before the selection returns. Changed originals are refused rather than replaced.

Added verifyNativeInputManualParamsReplay with bounded payload, original ref, compiler digest and exact byte comparison. Tests cover exact custom-range byte binding, changed ref/SHA/bytes/compiler defaults and cancellation. These are replay-binding tests, not a positive native manual processing/admission result. semanticproposal/review tests passed (1.920s/cached). Native input-authority refusal remains: protected policy proof selection, manual-aware native Params verification/seals, complete SQL proof integration and public routing still need implementation. No new migrations or physical model/runtime claim. Full V20–V32 goal stays active.


### Original native manual transaction lock component — 2026-10-07

Added lockNativeInputManualSelectionInTransaction and retained tenant/project/actor/session/parent/source binding in the private manual selection. Before any SQL, the component verifies all original native producer seals, exact request scope, immutable Params digest/ref, and equality between retained policy and the complete Params declarations. It rederives the exact manual worker inventory and requires one original independently verified proof per distinct selected manual pixel reference. This prevents a retained policy from silently dropping manual claims declared in Params.

For manual proofs, the component uses the existing scheduler-only lockManualPreparationProofs: complete original root/Params/target/protected/PNG/vector evidence is locked before and after the person-inventory writer barrier. The caller must still lock native source/model/policy/Params evidence and roll back every error. This component performs no publication, graph registration or activation; the native authority refusal remains in place until complete admission integration. Empty manual selection does not grant protected-input authority.

semanticproposal package checks passed (1.930s and final 1.943s); cancellation and incomplete original producer scope are refused before SQL. No new migration. This is not physical SQL/manual processing acceptance: migrations 00766–00797 remain physically unverified with the test PostgreSQL endpoint unavailable. Public routing, protected-source authority, manual-aware native preparation/admission and complete end-to-end acceptance remain open. The full V20–V32 goal is active and incomplete. Completion remains an expert estimate of approximately 50–60% of implementation volume, not a measured acceptance percentage.


### Full native manual Params/source revalidation — 2026-10-07

Replaced the transaction component's partial Params declaration decoder with verifyNativeInputManualSelectionParams. The retained original WorkingImage bytes must match the bound source SHA; the complete strict review Params decoder checks source identity, geometry, transform chain, diameter, face/mask bindings and unknown fields. The decoded full policy must equal the retained original selection. The same verifier now runs before saved manual preflight returns. Original source bytes are retained as an immutable string, not a caller-owned byte slice.

Tests construct real compiled Params from a WorkingImage fixture with a custom .003–.008 range and verify exact binding, changed source bytes/ID, changed policy, unknown Params fields even after digest recomputation, and cancellation. These are source/schema binding tests with empty faces, not native manual raster or SQL acceptance. An initial build caught an unused import, which was removed; subsequent package tests passed (1.967s; final integrated check 1.876s). No admission refusal was relaxed, no migration was added, and original model/source/Params transaction composition and public routing remain incomplete. Full V20–V32 goal remains active.


### Exact native manual compiler closure replay — 2026-10-07

verifyNativeInputManualSelectionParams now recompiles the complete worker closure from retained original WorkingImage, strictly decoded original policy and original manual roots. All immutable PreparedDefectParams fields (Params bytes/digest, input refs, manual root refs) must equal the compiler output. Missing dependencies, additional undeclared artifacts and changed/null root lists cannot pass by leaving the Params payload unchanged. Manual count and total retained root bytes are bounded before recompilation (64 roots, 4 MiB). This runs in both saved preflight and the transaction lock component and performs no external object IO.

Extended mutation tests for missing/extra input refs and changed/null roots; package semanticproposal tests passed (1.944s). Positive fixture is a real WorkingImage compilation with empty faces, not a positive manual producer or admission runtime. Existing independent manual evidence locks remain required; source/model/policy/Params transaction composition, protected authority and public routing still need completion. No migration added and no physical database acceptance claimed. Full V20–V32 goal remains active and incomplete.


### Manual target/protected-child compiler replay fixture — 2026-10-07

Added a positive compiler replay test with an original WorkingImage fixture, a source-bound manual-root fixture, a face carrying both skin and manual target bindings, and the automatically derived protected child. Exact full Params/input/root replay succeeds while preserving the custom .003–.008 diameter. Removing the original manual root, changing its bytes, changing its target binding or dropping retained root refs is refused. The protected child must be present in the decoded policy.

semanticproposal package tests passed (2.736s). This test constructs source bindings from fixtures and checks compilation/replay only; it does not prove native FAN/skin producer provenance, manual raster pixels, SQL locks or end-to-end admission. Native admission scope refusal remains, full transaction composition and public routing remain outstanding. No new migration or processing-quality change. Full V20–V32 goal remains active and incomplete.


### Retained native manual producer binding recheck — 2026-10-07

Saved manual selections now retain the physically reread original FAN/skin observations and all four original detector/FAN/parser/skin plan seals. Added verifyNativeInputManualProducerBindings, invoked before saved preflight returns and before transaction evidence locks. It requires exact producer seals and tenant/project/actor/session/parent/source scope, identical retained detector observations across FAN and skin, exact original WorkingImage bytes, and per-face declarations matching independently native-derived face/skin bindings (manual declarations verified separately). It then repeats complete strict Params/compiler closure replay. No object IO is performed inside the transaction component.

semanticproposal tests passed (1.971s; final 1.952s). Package success is not positive SQL/native manual end-to-end acceptance. This component retains/revalidates original observations but does not replace producer/result SQL locks. Native admission refusal, full model/source/policy/Params/manual transaction composition and public route integration remain incomplete. No migration added. Full V20–V32 remains active.


### Native producer/manual dependency transaction composition — 2026-10-07

Added nativeInputManualDependencies with original producer plans, retained manual selection, completed FAN/skin evidence packets and a digest binding original producer seals/request scope/Params/source/input/root refs and complete manual evidence pins. prepareNativeInputManualDependencies marshals physically verified completed producer packets from original transports before any admission transaction, then rereads the complete saved manual selection and requires equality before exposing the packet.

lockNativeInputManualDependenciesInTransaction verifies retained native bindings and packet digest, locks original completed FAN then skin, then invokes complete original manual evidence locks in the same caller-owned transaction. Every error requires rollback; there is no object IO, graph publication, registration or activation. Existing native admission refusal remains. Qualification, source policy and Params registration/admission composition and public routes are still incomplete; this private component is not yet wired to full admission.

semanticproposal tests passed (2.008s). Added empty/cancelled proof refusal before SQL and cancelled preflight zero-proof checks. No positive SQL/runtime acceptance or new migration. Full V20–V32 goal stays active and incomplete.


### Native manual completed packet/result correspondence — 2026-10-07

Added verifyNativeInputManualCompletedPackets to preparation and transaction composition before any completed-result SQL locks. Strict bounded packets must match retained original FAN/skin result IDs, output refs, Params refs and every ordered file role/ref/bucket/key/version/size/media; skin pixel count must match, and embedded admission cannot be absent/null. Unknown fields and trailing JSON are refused. Existing SQL producer locks still independently validate embedded admission authority; this helper is correspondence, not authority.

Tests cover synthetic packet correspondence and changed result/Params/file-version/pixel count, unknown/trailing JSON and null admission. Package semanticproposal checks passed (1.888s; final 1.833s). Synthetic packets do not prove original producer provenance or physical/SQL end-to-end processing. Full admission integration, qualifications, protected policy and public routes remain incomplete; no native admission refusal relaxed or new migration added. Full V20–V32 goal remains active.


### Unified original native/manual dependency recovery — 2026-10-07

Added restoreNativeInputManualDependencies: verifies original private producer scopes, recovers all four original transports in a scheduler-only transaction, loads and physically validates original saved manual declarations, constructs the combined FAN/skin/manual dependency packet, then recovers all transports again and requires equality after object IO. Current original deployment policy is rechecked before returning. Physical reads are outside transport-read transactions; no replacement artifacts are published and no defect task is admitted.

Cancellation and incomplete original scope return no dependency proof. semanticproposal tests passed (2.016s). This is an internal recovery composition, not public route wiring or positive SQL/manual end-to-end acceptance. Native defect admission refusal remains; qualification, Params registration, protected-source authority and admission integration are outstanding. No migration added. Full V20–V32 remains active and incomplete.


### Independent native manual CPU qualification and stage locks — 2026-10-07

Added nativeInputManualStageQualification bound to the exact original manual dependency seal. Preparation validates original producer bindings and completed packets, selects current server-owned defects.propose CPU policy, physically reads the original qualification report/costs, rereads the full saved manual selection, and rechecks report and deployment policy. No imported or bare-face proof conversion is used.

Added lockNativeInputManualStageInTransaction: validates qualification/dependency/seal/policy/cost/report bindings before SQL, composes original FAN/skin/manual evidence locks, then locks current policy snapshot and the original CPU qualification report in the same caller-owned transaction. Rollback every error. Params registration, protected-source authority and graph admission/activation remain separate outstanding integration gates; native admission refusal remains unchanged.

semanticproposal tests passed (1.907s; final 3.203s). Cancellation refuses preparation and stage locks before SQL. No positive database/native manual end-to-end claim or new migration. Full V20–V32 goal remains active and incomplete.


### Saved original qualified manual stage recovery — 2026-10-07

Added restoreNativeInputQualifiedManualStage and a distinct private nativeInputRestoredManualStage. The recovery reads original defects.propose saved configuration, original report/cost locators and accepted Params object, restores original manual/native dependencies, requires exact saved Params identity, then prepares independent CPU qualification from saved report/costs. Params object and complete saved stage are reread after physical qualification/manual IO; current original deployment policy is rechecked. No caller default report/cost/diameter replaces the original and no artifact is republished.

Cancellation exposes no dependency or qualification authority. semanticproposal package tests passed (2.172s). This is original qualification recovery composition, not task compilation/config equivalence or graph admission; native admission refusal remains unchanged. Params registration, protected-source authority, task/admission integration and public routes remain outstanding. No migration or positive physical database/native manual end-to-end claim. Full V20–V32 stays active and incomplete.


### Original native manual worker inventory compilation — 2026-10-07

Extracted assembleNativeDefectInventory as private pure file assembly; the existing bare native compiler still runs its unchanged original Params verifier before assembly. Added compileNativeInputManualInventory with a separate original manual authority path: verifies original producer bindings, dependency digest and completed packets, exact saved Params ref, then independently rederives and matches original manual root/target/protected pins. It assembles exact worker-declared refs and sizes using existing file type/version/SHA/size/duplicate/missing/extra checks. No caller additional pins are accepted; independently protected inputs lacking their own authority cannot be injected.

Cancellation and incomplete original proof expose no entries/sizes. semanticproposal tests passed (1.915s; final 3.065s). This is internal inventory assembly, not qualified task/config compilation or admission. No native admission refusal relaxed, no migration added, and no positive SQL/manual end-to-end acceptance claim. Full V20–V32 remains active and incomplete.


### Original restored manual CPU task compilation — 2026-10-07

Added distinct nativeInputManualTaskPreparation and compileNativeInputRestoredManualTask. The compiler requires original deterministic saved defect run/task IDs, exact saved/selected Params refs, original saved CPU report ref and measured costs, qualification/dependency/producer seals and current original policy. It validates the complete independent CPU report, compiles exact original manual inventory, and invokes CompileSemanticDefectTask with saved RAM/VRAM/scratch/output estimates and original source/Params/binding. Canonical inventory and size strings are retained. No bare native/imported admission proof is manufactured.

Cancellation/incomplete restored stages expose no task preparation; semanticproposal tests passed (1.996s). Full saved config/profile equivalence still requires draft reconstruction; graph registration/admission/activation and protected-source authority/public routes remain incomplete. No native admission refusal relaxed or new migration. No positive SQL/manual end-to-end acceptance claim. Full V20–V32 remains active and incomplete.


### Canonical original manual draft reconstruction — 2026-10-07

Extracted private assembleNativeDefectDraftShell for pure canonical config/shell assembly; existing bare native draft compilation still revalidates its original task/authority before invoking it. Added distinct nativeInputManualDraftPlan and compileNativeInputManualDraft: compiles the independently qualified original manual task, reconstructs canonical original native config with deterministic IDs, original producer/result/Params refs, dependency/config digests and saved recovery budgets, then requires matchNativeInputSavedStagePlan for full saved config/profile/input/Params identity. Manual authority stays in its distinct prepared wrapper; assembly data cannot pass the unchanged bare native admission verifier.

Initial build found one stale local task name in the extracted assembler, fixed before verification. No publication/registration/admission/activation or new migration; physical SQL/manual end-to-end verification and public routes remain outstanding. Full V20–V32 remains active and incomplete.

Final semanticproposal package verification passed (1.925s); this does not prove a positive original manual saved-config or SQL runtime fixture.


### Sealed original manual draft recovery — 2026-10-07

Added integrity digest/verification for the distinct original manual draft, binding canonical shell, inventories, dependency/qualification seals, original report object/metrics/policy/costs, Params object and saved config/profile/recovery metadata. Added restoreNativeInputManualDraft: composes original qualified stage recovery and exact canonical draft reconstruction, then rereads original saved Params object/config and current original deployment policy before returning a verified sealed draft. No publication, registration or admission.

Tests cover fingerprint mutations (shell config, dependency, saved profile/resources and Params object version), cancellation and zero-plan cancelled recovery. Synthetic fingerprint fixtures are integrity tests only, not original SQL/admission authority. Initial package verification passed (1.947s). Full admission, protected-source policy/public routes and positive physical SQL/manual end-to-end acceptance remain unfinished. No migration added; full V20–V32 remains active and incomplete.


### Original manual native registered transport recovery — 2026-10-07

Added scheduler-only original manual transport recovery. Before any SQL it verifies the distinct draft integrity seal and fully recompiles the original qualified task/canonical saved config, requiring exact shell/seal equality. It reads only existing semantic_native_defect_registered_transport, strictly decodes it and binds it to the reconstructed immutable worker task. No object IO, publication, draft recreation or admission. The pool wrapper exposes transport only after successful transaction commit; failure/unknown outcome/cancellation exposes none.

Tests verify cancellation and synthetic-but-fingerprinted incomplete drafts are refused before SQL. semanticproposal tests passed (2.509s). Positive registered manual transport/database/native manual end-to-end acceptance remains unverified. Graph admission/sealing, independently protected source policy and public routes remain incomplete; no native admission refusal relaxed or migration added. Full V20–V32 goal remains active.


### Original manual registration transport binding — 2026-10-07

Extracted private pure marshalNativeDefectTaskRegistrationBinding; the existing bare native entry point still validates its original draft seal. Added marshalNativeInputManualRegistrationBinding which verifies the distinct manual seal, fully recompiles original task/config and requires exact shell/seal equality before binding original transport. Shared binding checks task operation/version/resource class, exact single-task shell, immutable worker profile/Params/input digest and registered transport graph identity. No graph publication, registration writes or queue permit.

Cancellation exposes no registration binding; semanticproposal tests passed (1.940s), including existing native registration tests. Positive manual registration/SQL acceptance remains unverified. Full manual admission evidence/sealing, protected source authority/public routes are still incomplete; no native authority refusal relaxed or migration added. Full V20–V32 remains active.


### Complete distinct original manual admission packet — 2026-10-07

Added nativeInputManualAdmissionEvidence and its marshaler. After complete original manual task/config recompilation and seal equality, it binds original completed FAN/skin packets, deployment policy, independent CPU qualification/report pin, original Params object pin, exact registered task transport and every complete original manual evidence pin. The outer packet explicitly separates native and manual evidence and is bounded to 32 MiB.

Existing 00780/00781 bare-native SQL allowlists do not accept this outer packet; no attempt is made to strip manual evidence or route around those gates. SQL composition/sealing for the complete manual packet is still outstanding, so this marshaler cannot queue the task. Cancellation and synthetic incomplete drafts expose no packet; semanticproposal tests passed (1.971s). No migration added and no positive database/manual end-to-end acceptance claim. Full V20–V32 remains active and incomplete.


### Native recovery metadata SQL admission compatibility — 2026-10-07

Found a concrete integration failure: all eight original native detector/face/skin/defect admission and completed-evidence functions rejected the server-generated recovery field added to canonical configs. Added forward-only migration 00798 replacing those eight definitions with only the recovery allowlist extension and a guard requiring an object with schema_version=1 when present. Existing config hashes, source/producers, qualification, registration, scope, owner and role ACLs remain unchanged; legacy configs without this optional field remain accepted by these gates (cold recovery without retained metadata remains separately unsupported).

Static verification confirmed eight replacement definitions, eight recovery schema guards, preserved eight revoke/grant ACLs, balanced Goose statement markers, and exact prior function-body equality after reversing only the two intentional edits. Migration is not applied or physically SQL-verified; unavailable test PostgreSQL remains the runtime acceptance limitation. No manual admission/activation bypass introduced. Migrations 00766–00798 require physical acceptance. Full V20–V32 remains active and incomplete.


### Recovery SQL regression and original manual Params admission bytes — 2026-10-07

Added a source regression comparing all eight 00798 function definitions to their original migration definitions after reversing only the intentional recovery allowlist/schema edits. This checks preservation of the complete prior body/ACLs, balanced Goose blocks and forward-only rollback refusal. Test passed (1 passed, 0.05s); it is not PostgreSQL migration/runtime acceptance.

The distinct manual admission packet now includes params_text with the exact physically verified original compiler/saved Params bytes, already SHA/size/source/policy/closure bound by preceding validators. This closes missing data for eventual SQL comparison of declared manual/protected masks against independent producer evidence. Existing bare-native SQL is not given this outer packet; complete manual SQL admission/sealing remains outstanding. semanticproposal tests passed (1.974s). No additional migration or queue permit. Full V20–V32 stays active and incomplete.


### Complete original manual SQL admission evidence gate — 2026-10-07

Added forward-only migration 00799 with scheduler-only lock_semantic_native_manual_admission_evidence and a Go transaction adapter. The distinct bounded outer packet must carry original Params bytes matching source/SHA/size, unique original manual roots, exact selected manual targets, corresponding protected children and their worker inventory refs. Manual-only protected authority is explicit: unrelated permanent/additional protected masks refuse. Existing native admission/completed gates are composed according to graph state, followed by original manual evidence locks in deterministic root order before and after the person inventory writer barrier. No activation, receipt creation or queued ticket.

Go package compile/tests are separate from physical SQL acceptance. The test PostgreSQL endpoint remains unavailable; 00799 is not applied or physically verified (no local SQL parser available). Migrations 00766–00799 require physical acceptance and this new gate still needs positive/negative database fixtures before use. Full activation/public route/protected policy integration and V20–V32 remain incomplete and active.


### Original manual atomic sealing and receipt resume — 2026-10-07

Added forward-only migration 00800: scheduler-only seal_semantic_native_manual_preparation first composes complete original manual SQL evidence gate, then calls unchanged original native atomic seal while manual locks/person-inventory writer barrier remain held. Receipt replay repeats the complete outer gate, not just stripped native evidence. Added Go transaction seal adapter and resumeNativeInputManualPreparation with full canonical recompilation and physical CPU/manual/native selection rechecks before SQL retries. Each retry recovers original registered transport and repeats complete manual admission; nativeTicketWithRetry exposes a ticket only after commit.

Cancellation/synthetic incomplete drafts refuse before SQL. Eight-role integration ACL fixture now includes 00799/00800 functions; fixture is compiled, not physically executed. Initial semanticproposal package check passed (2.167s). Migrations 00766–00800 remain physically unverified with PostgreSQL unavailable; positive SQL/manual graph activation/receipt fixtures and public initial/recovery routing are still outstanding. No production migration applied or bare native authority refusal relaxed. Full V20–V32 remains active and incomplete.


### Original manual restore/resume coordinator and lock order — 2026-10-07

Added restoreAndResumeNativeInputManualPreparation to compose server-loaded original saved draft reconstruction and current-evidence atomic resume. The distinct manual inventory compiler now requires 1–64 independent manual proofs, matching its SQL gate; bare tasks use their existing path. Cancellation/incomplete originals refuse with no ticket. semanticproposal tests passed (2.233s).

Reviewed 00799 transaction ordering and aligned tenant/tenant_usage/run locks with the existing native atomic seal to avoid an introduced run-before-tenant lock inversion. Made deterministic manual-root sorting explicitly refer to the outer evidence row. 00799/00800 remain unapplied and physically unverified; no positive SQL/manual activation/replay claim. Initial manual graph generation, public routing and complete acceptance remain open. Full V20–V32 stays active and incomplete.


### Initial original manual/native Params preparation — 2026-10-07

Added distinct unpublished nativeInputManualInitialPreparation and prepareNativeInputManualInitialParams. It owns bounded server-selected face declarations before IO, physically verifies original FAN/skin face bindings, independently resolves each selected original manual raster/vector producer, deduplicates only equal complete proofs and bounds retained root bytes. Compiles Params with automatic original protected children and custom selected diameter, derives exact manual inventory pins, rereads all original manual and FAN/skin evidence and current policy before returning. It does not need a preexisting saved defect stage.

The accepted Params ref stays empty; the deterministic local document identity is used only transiently for strict schema decoding and is never exposed as accepted artifact authority. Publication, independent CPU qualification, initial graph ownership/registration and admission integration still need composition. No bare native refusal relaxed or new migration. Cancellation/incomplete producer scope exposes no Params/proof/ref; initial semanticproposal check passed (1.985s). No positive native manual initial/database/quality end-to-end claim. Full V20–V32 stays active and incomplete.


### Accepted-version original manual Params publication — 2026-10-07

Added distinct nativeInputPublishedManualParams and publishNativeInputManualInitialParams. It requires an unpublished initial selection, validates original producer/schema/compiler bindings with a transient document identity, fully reconstructs independent manual/native preparation before and after accepted-version publication, and rejects ref/SHA or input/document aliases. Publishes exact original compiler Params through existing v2draft.PublishDefectProposalParams fencing/quota/source checks; only the returned accepted artifact ref is retained. No artifact is substituted for an original failed proof.

Cancellation/incomplete originals expose no published ref; semanticproposal tests passed (2.117s). Initial graph ownership/qualification/registration/admission composition and public routing still need completion. No new migration or positive original manual objectstore/database/processing acceptance claim. Full V20–V32 remains active and incomplete.

Broader backend verification after shared native draft/registration changes: go test ./... completed successfully (including v2render 23.351s). This run does not provide physical PostgreSQL migration or objectstore/model/GPU end-to-end acceptance.


### Initial accepted manual Params physical proof — 2026-10-07

Added nativeInputPublishedManualParamsProof and readNativeInputPublishedManualParams. This initial path uses existing accepted-version Params catalog authority instead of saved defect config, so it works before a defect draft exists. It validates distinct original native/manual compiler bindings, fully reconstructs independent original initial preparation, reads the pinned accepted Params bytes and exact saved/compiler SHA, reconstructs original manual/native evidence again after Params object IO, and repeats the accepted catalog pin before returning. Captures exact Params object metadata for later dependency/task registration locks.

Cancellation/incomplete original scope exposes no published ref or Params proof. No new migration or admission grant. Initial dependency/qualification/task ownership and graph registration integration plus public routes/positive physical acceptance remain unfinished. Full V20–V32 remains active and incomplete.


### Initial original manual/native dependency assembly — 2026-10-07

Extracted shared original FAN/skin/manual packet assembly; the saved-recovery wrapper still repeats original saved Params/manual selection authority after assembly. Added distinct nativeInputManualInitialDependencies and prepareNativeInputManualInitialDependencies for accepted newly published Params before any saved defect draft exists. It recovers all four original model transports, physically reads accepted Params/manual/native evidence, assembles completed dependency packets, rereads full accepted proof and all original transports after producer IO, requires equality, and rechecks original policy. Exact Params object metadata is retained alongside dependencies for later graph registration.

Cancellation/incomplete originals expose no dependency seal or accepted Params pin. No new migration or initial graph admission/activation. Independent initial CPU qualification/task compilation/draft registration and public route integration remain outstanding; no positive SQL/objectstore/model/manual end-to-end claim. Full V20–V32 stays active and incomplete.


### Initial independent manual CPU qualification — 2026-10-07

Added nativeInputManualInitialStage and prepareNativeInputManualInitialQualification. It requires exact initial accepted Params proof/selection binding, independently selects current server-owned defects.propose CPU policy/report/costs, then physically rereads accepted Params and original manual/native evidence and requires equality before report/policy rechecks. Does not require a saved defect draft or manufacture recovery metadata.

Shared private CPU qualification routine preserves original producer/dependency/packet validation and current report/cost/policy checks. Saved-recovery and initial wrappers retain distinct original saved or accepted-Params rechecks; callbacks are private internal composition only. Cancellation/incomplete originals expose no stage qualification. Initial task compilation/draft registration/admission and public route integration remain outstanding; no migration or positive physical SQL/manual quality acceptance claim. Full V20–V32 remains active and incomplete.


### Initial original manual CPU task compilation — 2026-10-07

Added distinct nativeInputManualInitialTaskPreparation and compileNativeInputManualInitialTask. It requires exact accepted initial Params/selection binding and compiles the original manual/native worker inventory with independent CPU policy/report/costs and actual server-selected RAM/VRAM/scratch/output limits. No saved recovery metadata is fabricated. Shared pure compileNativeInputManualTask preserves dependency/qualification/producer seals, current policy/report, inventory and immutable worker compilation checks; restored task entry point retains its additional original saved run/task/report/cost/Params checks.

Cancellation/incomplete initial stage exposes no task/entries/sizes. Initial canonical draft ownership/registration/activation and public routes remain incomplete. No new migration or positive database/manual/GPU quality acceptance claim. Full V20–V32 stays active.


### Initial original manual canonical blocked draft — 2026-10-07

Added distinct nativeInputManualInitialDraftPlan. Compilation reconstructs the initial task from its actual immutable CPU resource profile, requires exact task/inventory/size equality, and uses shared pure canonical shell assembly to retain original Params/source/intent/snapshot/producer/result refs, dependency/config digests and report/cost/resource recovery metadata. Reservation is validated by nativePlanRecoveryFromTask; the resulting draft remains blocked with its own integrity fingerprint binding qualification, Params object and dependency evidence. No bare native admission certificate or saved recovery state is fabricated.

Cancellation/incomplete initial tasks expose no draft. Initial owner transaction, transport publication/registration, manual admission/activation and public routes still need composition. No new migration or physical database/model/manual end-to-end acceptance claim. Full V20–V32 remains active and incomplete.


### Initial original manual blocked-graph owner transaction — 2026-10-07

Added beginNativeInputManualDraftInTransaction: fully recompiles the distinct initial draft and requires exact integrity/shell equality before original inventory/intent/snapshot, qualified detector source, completed FAN/skin/manual/CPU policy-report, and accepted Params object locks. Only then invokes generic BeginAdmissionInTransaction for the exact deterministic graph/key/request digest, accepting created/pending blocked states only. No task activation or queued ticket; every error requires rollback.

Added commit-aware beginNativeInputManualDraft wrapper with physical initial qualification/accepted Params/manual/native evidence rechecks before SQL. Commit failure/unknown outcome/cancellation exposes no draft outcome. Cancellation/incomplete draft test refuses before SQL. Initial semanticproposal package verification passed (1.946s). No new migration or positive physical owner transaction claim. Transport publication/registration, complete initial activation/public routes and physical database/manual acceptance remain unfinished. Full V20–V32 stays active.


### Initial original manual worker transport publication/registration — 2026-10-07

Added publishNativeInputManualInitialTransport: requires distinct draft seal/full canonical recompilation, physically rechecks original initial CPU/Params/manual/native evidence before and after worker transport publication, verifies exact task/Params/input digest and immutable transport binding, and retains original registration proof. Caller must first commit blocked owner state; object IO is outside SQL.

Added registerNativeInputManualInitialInputsInTransaction: recompiles and binds exact original transport, repeats complete blocked owner/source/anchor/native/manual/qualification/Params locks, accepts only missing or exact replayed registration, registers exact graph inputs and requires exact replayed binding afterward. Graph remains blocked; rollback every error. Initial build caught incorrect PreparedSemanticTask field access, fixed to Graph.Built. Cancellation/incomplete draft refuses publication/registration before SQL. No migration or positive objectstore/SQL registration acceptance claim. Initial atomic activation/coordinator/public routes and full physical acceptance remain outstanding; full V20–V32 active.


### Initial manual atomic registration/admission — 2026-10-07

Added initial-plan admission packet construction without manufacturing restored-stage authority. Initial and restored entry points independently recompile their plans before sharing pure packet assembly and the SQL ticket adapter. Initial registration and the complete manual evidence gate now run in one transaction; the pool wrapper physically rechecks accepted Params, manual/native evidence and CPU qualification before SQL retries and returns a ticket only after commit. Already sealed owners continue to use the separate saved-plan recovery path.

Verification: semanticproposal package tests passed after the shared refactor; added negative coverage that incomplete plans and cancelled contexts produce no admission packet, SQL calls or ticket. This does not verify a successful PostgreSQL/object-store execution. Migrations 00766–00800 remain physically unverified; public API routing remains outstanding. Overall completion stays an expert estimate of 50–60%, not acceptance coverage.


### Initial manual launch coordinator — 2026-10-07

Added `startNativeInputManualInitialPreparation`: commits the blocked owner, publishes the frozen input transport outside SQL transactions, then registers and admits through the complete manual gate in one commit-aware transaction. Each phase retains its existing physical evidence rechecks. Failed publication leaves the blocked deterministic owner; already sealed owners must use saved-plan recovery. Public routing is still not wired.

Verification: `go test ./internal/semanticproposal` passed (4.060s). Coordinator tests verify phase ordering, exact transport forwarding, no later phase after errors, no ticket on failed/unknown admission commit even when a callback returns one, and cancellation after blocked-owner commit preventing publication. These callback tests do not prove successful PostgreSQL/object-store execution. Physical migration verification remains outstanding; the full V20–V32 goal remains active.


### Server-owned initial manual bootstrap — 2026-10-07

Added `nativePreparationBootstrap.bootstrapManualDefect`: validates server dependencies/resource budget, resolves original manual inputs from server-selected face locators, publishes accepted Params, recovers and verifies original producer transports, prepares complete dependencies/current CPU qualification, compiles the immutable task/draft, and invokes the commit-aware initial launch coordinator. Errors expose neither a usable plan nor a ticket. The private submission is a child-stage receipt, not a semantic parent API receipt.

Verification: `go test ./internal/semanticproposal` passed (2.059s), including absent-server-dependency/cancelled-context refusal. Successful database/object-store execution remains unverified. Routing inspection found no semanticproposal imports outside its package in backend Go sources; integrating the public parent continuation remains outstanding, rather than merely exposing this child ticket. Migrations 00766–00800 remain physically unverified. The full V20–V32 goal is active.


### Initial manual result catalog — 2026-10-07

Added a distinct initial-manual result locator which independently verifies/recompiles the original manual draft, enforces exact shell/seal equality, queries the existing tenant/editor-scoped native defect result catalog, checks original Params and five outputs against the frozen task, rejects aliases to any original input and to FAN/skin result identities, and returns conflict for absent results. The bare-native reader/authority restrictions are unchanged. Migration 00784 was inspected: it binds succeeded CPU task, parent/request/session, canonical result attempt/fence and verified original uploads; inspection is not runtime SQL verification.

Verification: semanticproposal tests pass; added incomplete-plan/cancelled-context refusal coverage. This result locator is not physical output verification or downstream protection authority. Manual pixel/protection coverage and SQL downstream composition remain to implement before parent API integration. Migrations 00766–00800 remain physically unverified; full V20–V32 objective remains active.


### Initial manual physical output verification — 2026-10-07

Added separate initial-manual output-object reader for the exact six output/Params pins (ordinal, reference, bucket, version, media, bounded bytes), retaining original-plan recompilation and catalog scope. Added `readCompletedNativeInputManualInitialDefect`: reads and verifies Params/proposal/report, exact source/labels/protection references, label PNG equivalence and pixel validity, exact compiler-owned protection declarations, and probability-mask coverage of every independently verified original manual protected child by the produced protection. Rechecks original CPU qualification, accepted Params/manual/native evidence before and after physical IO, plus current result catalog/object pins and FAN/skin results. Bare-native protected/manual refusal is unchanged.

Verification: semanticproposal package tests pass after fixing a duplicate result type introduced during extraction. Negative tests refuse incomplete/cancelled object/content reads before evidence exposure. A successful manual output/coverage fixture and actual SQL/object-store execution are still required; downstream manual SQL admission/protection publication and public parent routing remain outstanding. Migrations 00766–00800 remain physically unverified. Full V20–V32 goal remains active.


### Manual protection declaration mutation checks — 2026-10-07

Extracted pure complete declaration-to-original-pin matching from the physical manual result reader. It rejects absent original proofs, invalid child references, conflicting immutable pins for the same reference, omitted/duplicate/unknown declarations, changed geometry or SHA, and independently permanent protection outside this manual-only path. It returns no partial pins on failure. The physical reader still independently verifies original authority and probability-mask coverage; the pure matcher grants no authority.

Verification: semanticproposal package tests include a positive two-original-mask matching case and mutation cases for omission, SHA substitution, duplication, geometry alteration, unknown reference, conflicting object pin, and unsupported permanent protection. These are declaration tests, not a positive end-to-end manual result fixture. Physical SQL/migration verification and downstream manual parent integration remain outstanding; the full V20–V32 objective remains active.


### Initial manual protection publication and completed packet — 2026-10-07

Added distinct initial-manual protection preparation/publication types, retaining the original manual plan without converting it into bare-native authority. The preparation builds the protection document only from physically verified Params/report/proposal and exact output pixels, rereads the complete result and binds a deterministic original-task document. Publication uses accepted-version quota-fenced document publication and compares complete physical evidence before and after object IO. Added a downstream completed packet retaining full nested manual admission (never unwrapped to bare-native admission).

Verification: semanticproposal tests passed after protection publication additions (11.278s); incomplete/cancelled authority publication/preparation tests added. A separate package rerun covers the completed packet addition. Successful SQL/object-store publication/registration remains unverified. Manual downstream SQL registration and public parent integration remain outstanding; migrations 00766–00800 remain physically unverified. Full V20–V32 goal remains active.


### Manual completed-result SQL gate and protection registration — 2026-10-07

Added forward-only migration 00801: exact manual completed-result scope binds tenant/project/actor/session/parent/request, locks the full original manual admission, then invokes unchanged native completed-output checks while manual locks remain held. Added 00802: validates full manual completed-result authority before delegating immutable protection-root registration to the existing exact provenance function. Both are SECURITY DEFINER with fixed search_path, photo_fn_owner ownership, PUBLIC revoke and scheduler-only execute; no new table grants. Added distinct Go SQL adapters and manual protection registration using physically verified complete packets. Expanded existing integration role ACL inventory with both functions.

Verification: prior completed-packet package test finished successfully (13.806s). Goose migration-marker tests pass (6 tests, 2.42s); this does not validate SQL runtime/syntax. A current semanticproposal rerun is still being observed. Migrations 00766–00802 are physically unverified/unapplied, and positive database/object-store manual registration and downstream parent routing remain outstanding. Full V20–V32 goal remains active.


### Registered initial manual protection reader and lock — 2026-10-07

Added separate manual registered-protection objects/physical reader and publish-register-read coordinator. Reader checks exact registered root/NPY pins, SHA-pinned document content and resolved protection pixels, repeats manual result verification, then repeats registry/object metadata. Added migration 00803 and a Go SQL adapter: complete manual result authority remains locked before delegating exact immutable registered-root/upload/NPY evidence locks. Existing bare-native functions/types remain unchanged. Expanded role-ACL integration inventory; physical execution remains unverified.

Verification: previous package rerun completed (13.882s), manual registered reader tests passed (2.717s), and latest coordinator/lock package tests passed (2.264s). Goose markers: 6 passed (0.35s). These checks do not prove PostgreSQL syntax/runtime, successful object-store registration or public routing. Migrations 00766–00803 remain physically unverified/unapplied. Manual source-continuation integration and public parent orchestration remain outstanding; full V20–V32 goal remains active.


### Manual source continuation boundary — 2026-10-07

Added a distinct `nativeInputManualSourceContinuation`, retaining original source preparation and separately registered initial-manual protection without converting into bare-native/imported-origin authority. Composition binds original tenant/project/actor/session, parent run/request, source version and ICC, exact root document/pixels and original detector source. The transaction adapter locks original source closure and complete registered manual protection/result evidence together; incomplete or cancelled proof is rejected before any SQL call. Person coverage/transfer-model/target qualification and full parent graph admission remain mandatory separate steps.

Verification: semanticproposal package tests cover incomplete/cancelled composition and SQL refusal. No claim of positive SQL/object-store execution or public integration. Migrations 00766–00803 remain physically unverified/unapplied; manual transfer graph integration and public parent routing remain outstanding. Full V20–V32 goal remains active.

Verification correction for the manual source continuation entry: the default package test failed during linking with `no space left on device` before tests ran. Repository Go caches were already negligible; module cache and database/object-store/evidence data were preserved. A reduced-debug test binary rerun (`go test -ldflags=-w ./internal/semanticproposal`) was started; no success claim until its terminal output is observed.

Reduced-debug rerun also failed at linker output mapping with no space left on device; current source-continuation tests remain unexecuted. No database/object-store/module/evidence data deleted.


### Manual qualified reference-person coverage — 2026-10-07

Disk availability recovered to approximately 8 GiB without repository data deletion. Previous manual source-continuation tests then passed (1.989s). Added distinct manual reference-person coverage/proof, policy preparation and transactional locks: independently selects the unique original mask.person producer, validates its qualification and complete person evidence, physically verifies pixel coverage by the exact registered manual protection, and repeats person candidate, report, artifact and manual protection evidence after IO. Source/manual protection and qualification-policy/person locks compose in one caller transaction. No manual proof is converted into bare-native authority.

Verification includes missing-authority/cancelled reference refusal before SQL and cancelled coverage refusal. Positive physical person/manual coverage fixture, original policy/provider evidence and database execution remain required. Migrations 00766–00803 are physically unverified/unapplied. Manual transfer documents/workflow and public parent routing remain outstanding; full V20–V32 goal remains active.


### Manual transfer reference documents and selection separation — 2026-10-07

Added distinct manual selection-separation proof/read/locks and manual transfer-reference document preparation/locks. Exact original selection pixels must be nonempty and disjoint from physically verified registered manual protection. Immutable selection/document pins, original source/parent request, person qualification/coverage and complete manual protection are retained and reread after IO. Reference, selected-mask and protection documents are SHA-bound and independently resolved to exact pixels before transactional composition. Existing selection SQL gate is reused only after complete manual source/protection authority has been locked; no new generic or bare-native authority is fabricated.

Verification includes incomplete/cancelled selection/document refusal and no SQL on missing documents. The package rerun is observed separately; these tests do not prove positive end-to-end masks or SQL execution. Manual transfer task/workflow compilation and public parent routing remain outstanding; migrations 00766–00803 remain physically unverified/unapplied. Full V20–V32 goal remains active.


### Manual transfer task staging and workflow compiler — 2026-10-07

Added distinct manual transfer task-input and workflow-plan types, complete reference validation, target input preparation and immutable batch task/workflow compilation. Preparation retains manual reference person/source/selection authority, independent qualified transfer report/measured costs, target source/protection/person evidence, metadata budgets and frozen execution profiles. Rechecks all retained source/qualification/inventory/reference evidence after IO. Workflow compiler checks per-target exact binding/documents/qualification/source references and retains manual reference outside the generic core. No manual authority is converted into imported-origin/bare-native authority. Actual graph begin/register/seal must use the dedicated manual admission path, which is still to implement.

Verification: package rerun observed separately; compiler tests reject cancelled/empty batches without partial plans. Positive physical/database manual graph execution remains unverified. Public API/parent durable orchestration and migrations 00766–00803 verification remain outstanding; full V20–V32 objective stays active.


### Manual transfer graph transaction adapters — 2026-10-07

Added complete manual workflow scope validation, original reference/target input locking, blocked draft creation and transport registration. Scope rederives canonical graph configuration and exact per-task closure, checks original request/session/project and source/target authority. Admission repeats full prepared inputs around intent/snapshot/qualification locks. Initial seal registers inputs and seals the graph within the caller transaction, returning only an exact queued parent ticket; caller must commit before exposing it. Pure inventory/outcome/ticket helpers are shared without sharing authority types.

Verification: semanticproposal begin/register tests passed (2.009s), including incomplete/cancelled graph refusal before SQL. A subsequent package run covers initial seal compilation. Commit-aware physical activation, sealed recovery/replay and public orchestration still need integration. Positive PostgreSQL/object-store manual graph execution remains unverified; migrations 00766–00803 remain physically unverified/unapplied. Full V20–V32 goal stays active.


### Manual transfer activation and registered-transport recovery — 2026-10-07

Added manual physical recheck, commit-aware activation, sealed receipt replay and registered-transport recovery/resume. Physical reads repeat full manual reference/source/person/protection and all target source/person/protection/qualification evidence before SQL. Activation repeats complete admission and routes existing receipts through stored closure checks. Recovery reads only original registered transport under those same locks, rejects missing/stale/partial state, binds exact frozen tasks, then seals or replays. No recovery publisher/draft recreation; no ticket escapes failed transaction commit. Shared pure ticket decoding remains tested by the original suite.

Verification: `go test ./internal/semanticproposal` passed (1.844s), including cancelled/incomplete manual seal/replay/recovery and refusal before SQL/object IO. Duplicate pure decoder test removed afterward; unique manual boundary tests retained. This does not prove successful physical SQL/object-store execution or durable parent loading/public routing. Migrations 00766–00803 remain physically unverified/unapplied. Full V20–V32 objective remains active.


### Exact saved manual plan and registered protection restoration — 2026-10-07

Added `restoreNativeInputManualInitialPlan`: loads the fully verified original saved manual draft, independently rereads accepted Params via initial physical verifier, requalifies using original saved report/costs, recompiles using original saved resource/reservation limits, requires exact saved-stage and original-shell equality, then repeats original saved draft observations. This is verified reconstruction for downstream manual readers, not initial admission or publication. Added separate saved manual protection locator/verification/restoration: accepts only the existing exact document/result/body SHA/root and rereads original manual registered protection. Missing state is never recreated.

Verification: reconstruction compiled/package tests passed (1.929s); added registered-manual document/result/hash mutation tests and cancelled restoration test, correcting duplicate test names before final rerun. No positive PostgreSQL/object-store restoration proof. Original parent transfer plan loader/public orchestration still outstanding; migrations 00766–00803 remain physically unverified/unapplied. Full V20–V32 objective stays active.


### Manual parent graph reconstruction and original recovery — 2026-10-07

Added manual parent reconstruction from original saved transfer config, Params and snapshot, requiring exact config SHA and repeated saved observations before registered-transport resume. Added server-owned original producer recovery: restores all four original native face plans, original manual task/transport, exact manual result/registered protection and independent qualified reference-person proof, then loads/reconstructs/resumes the original parent. No masks, costs, limits or producer authority supplied by the caller; no publisher or draft reconstruction in recovery. Existing strict saved-transfer config/snapshot/Params readers are reused as data readers.

Verification: semanticproposal package rerun covers cancelled/missing reconstruction dependencies; fixed temporary copied-test duplicate/unimplemented signature before rerun. Positive original-manual/database/object-store restoration remains unverified. Top-level intent loader/public parent orchestration remain outstanding; migrations 00766–00803 physically unverified/unapplied. Full V20–V32 goal stays active.


### Top-level manual original loader and blocked graph coordinator — 2026-10-07

Added `loadOriginalAndResumeNativeInputManualTransfer`: authenticated request compilation, original saved intent/inventory loading, strict saved parent config/snapshot read and repeated config observations precede full original-manual recovery. No caller-supplied manual/permanent/pin authority and no publisher exists in this entry. Added `persistNativeInputManualTransfer`: commits blocked parent graph, publishes frozen worker transport outside SQL, physically rechecks complete manual reference/target evidence and rebuilds complete manual result packet, then registers transport with full evidence locks in a committed transaction. Shared commit-aware stage helper retained.

Verification: semanticproposal tests cover cancelled/missing top-level loader/original-recovery dependencies; package rerun observed separately. Successful PostgreSQL/object-store/durable recovery is unverified. Initial request preparation and public API/durable parent scheduling still remain; migrations 00766–00803 remain physically unverified/unapplied. Full V20–V32 objective remains active.


### Initial manual parent request preparation — 2026-10-07

Added `prepareAndActivateNativeInputManualTransfer`: authenticated request compilation, complete manual reference document validation, accepted intent/Params/snapshot publication, independent target input preparation, exact batch workflow compilation, blocked graph persistence/transport registration and commit-aware activation. Server supplies the privately verified original manual reference-person proof; client intent cannot manufacture it. Restart reconstruction stays separate and never republishes.

Verification: `go test ./...` across the backend passed; semanticproposal 1.886s, other packages passed/cached. This is not PostgreSQL/object-store end-to-end verification. Routing clarification: the HTTP endpoint `/api/v1/tenants/{t}/semantic-intents` and SemanticPropose interface already exist, but no concrete Propose service is implemented in semanticproposal or wired into main; earlier statements about absent public routing meant this missing service wiring, not absence of the HTTP handler. Server-owned route selection/durable preparation orchestration remain outstanding. Migrations 00766–00803 physically unverified/unapplied; full V20–V32 objective stays active.


### HTTP-compatible manual recovery service boundary — 2026-10-07

Added `ManualRecoveryService.Propose` implementing the existing HTTP propose signature specifically for an existing original manual parent. Canonical intent compilation and authenticated identity/key validation precede full original-manual loading/reconstruction/commit-aware resume. A response is constructed only from an exact queued parent run/workflow ticket; child tickets, foreign workflows and blocked states produce no response/resource ID. Cancellation after resume exposes no receipt. This recovery-only adapter is deliberately not assigned as the general create service in main; first-time durable preparation orchestration/route selection still needs implementation.

Verification: semanticproposal/httpapi package rerun includes positive response formatting, child/workflow/blocked ticket rejection and cancelled/anonymous service refusal. Formatting tests are not durable-admission proof. Positive DB/object-store/service execution and public production wiring remain unverified. Migrations 00766–00803 physically unverified/unapplied; full V20–V32 objective remains active.


### Physical manual probability-coverage fixture — 2026-10-07

Revalidated localhost PostgreSQL test port outside the socket-restricted sandbox: connection refused on 127.0.0.1:55432. No production migrations executed. Added a physical byte-stream fixture for `verifyManualProtectionCoverage`, using version-pinned float32 NPY required/protected masks through the existing object reader. Positive coverage, weakened probability rejection, SHA substitution rejection and close-error propagation are checked, together with exact original object versions and closing both streams on success/failure. This adds physical content evidence beyond declaration-only tests; it does not prove original database producer authority or complete manual workflow execution.

Verification: targeted Go test rerun observed separately. Full V20–V32 goal remains active; PostgreSQL/migrations 00766–00803, first-time durable orchestration and public API wiring remain outstanding.


### Original manual child to initial parent continuation — 2026-10-07

Added `prepareAndActivateManualParentFromSubmission`: checks the private original manual child ticket/plan, independently recompiles exact child shell, binds frozen transport and exact authenticated parent request, physically verifies/publishes/registers the original manual protection, rejects conflicting configured source protection, selects source protection only from verified registration, verifies qualified original person coverage, then invokes the complete initial manual parent preparation/activation. Missing child outputs are refused; no child receipt is exposed as parent acceptance and no substitute authority is fabricated.

Verification: negative cancelled/incomplete child test and package rerun observed separately. This is internal server continuation, not the still-missing durable polling/orchestration/public create service. No successful PostgreSQL/object-store complete child-to-parent fixture yet. Migrations 00766–00803 remain physically unverified/unapplied. Full V20–V32 goal remains active.


### Original native stage-state observation — 2026-10-07

Added forward-only migration 00804 and private state reader for durable preparation orchestration. SQL delegates original saved-stage scope/authorization and requires its exact config SHA before returning original task/run state and graph seal state. Go first verifies complete saved stage identity/config metadata, then compares exact original task; absent/stale rows are refused. This is observation only: succeeded does not grant physical output/qualification/protection authority, and no parent receipt can be created from a state string. SECURITY DEFINER with fixed search_path/photo_fn_owner, PUBLIC revoke and API-only execute; no table grants.

Verification: package and Goose-marker runs observed separately. SQL runtime/syntax and positive original-stage state lookup remain unverified because local PostgreSQL is unavailable. Durable scheduler integration and public create service remain outstanding; migrations 00766–00804 physically unverified/unapplied. Full V20–V32 objective remains active.


### Persisted native preparation phase interpretation — 2026-10-07

Added preparation phase interpretation based on actual task/run domains in migration 00010. Distinguishes blocked unsealed draft, waiting sealed task/retry, succeeded observation and failed/cancelling/cancelled/skipped states. Rejects unknown states, nil task identity and impossible live/unsealed or completed/pending combinations. Saved-state reader now validates each observation through this interpreter. Phase is not admission, physical-result authority or a parent ticket.

Verification: transition-table tests cover queued/running/retry waiting, success, terminal failures/cancellation and inconsistent/unknown-state rejection; semanticproposal package rerun observed separately. Durable scheduler callbacks/initial admission and public create service are still to implement. PostgreSQL/migrations 00766–00804 and positive complete workflow execution remain unverified. Full V20–V32 objective remains active.


### Bounded original manual child continuation step — 2026-10-07

Added a server-owned bounded manual parent step: reads exact original defect stage state, returns waiting/blocked/failed without any parent ticket, and only on succeeded observation restores original face/manual plans, full qualification evidence, committed original child receipt and registered transport before independently verifying/publishing manual protection and admitting the initial parent. No sleeping, status-derived output authority or synthetic child ticket. Missing/cancelled dependencies refuse before IO. Durable scheduling persistence and choosing existing-parent recovery vs initial continuation remain the caller's still-unimplemented orchestration duties; this is not a complete scheduler/public create service.

Verification: semanticproposal package rerun observed separately. No positive PostgreSQL/object-store continuation evidence; migrations 00766–00804 remain physically unverified/unapplied. Full V20–V32 goal remains active.


### Existing manual parent routing during child continuation — 2026-10-07

Added scoped saved-parent existence observation through existing SECURITY DEFINER saved-config reader. Manual child-to-parent continuation now selects complete original saved-parent loading/reconstruction/replay for an existing parent, and only absent parents take initial publication/blocked graph admission. Existence is a routing hint only: recovery still validates exact request/config/snapshot/Params/qualified inputs and committed receipt. Concurrent initial callers may still receive conflict and retry; no stale-state fallback can bypass gates or duplicate a parent identity. Durable retry scheduling remains unimplemented.

Verification: cancelled/missing-scope probe refusal plus semanticproposal rerun observed separately. Positive SQL existing-vs-absent parent routing remains unverified because PostgreSQL unavailable. No new migration required. Public create orchestration and migrations 00766–00804 runtime verification remain outstanding; full V20–V32 objective remains active.


### 2026-10-07 — V27 original stage observation verification

Extended the native bootstrap integration fixture with API-only ACL checks for `semantic_native_input_saved_stage_state`, an original completed skin-stage observation, and rejection of a different config hash. These observations do not grant physical output or parent admission authority.

Validation: `go test ./internal/semanticproposal` passed (1.922s). The integration fixture remains skipped without its isolated PostgreSQL/object-store configuration. An explicit local socket probe of 127.0.0.1:55432 returned ConnectionRefusedError; no physical database acceptance or migration execution is claimed.

Overall completion remains an expert estimate of 50–60% of implementation scope, not an acceptance percentage. Open work includes durable V27 request identity and orchestration, general create-service/API wiring, remaining V28–V32 requirements, database/object-store integration acceptance, and GPU/quality validation. The original preparation request key is currently retained in memory but absent from saved detector recovery metadata; durable recovery must address this while preserving exact replay of existing saved configs.


### 2026-10-07 — V27 persisted original request identity

New original detector draft configs persist the parent idempotency key in optional `recovery.request_key`. The strict recovery decoder accepts this locator only for face.detect, validates the key, and binds its deterministic run identity to the original tenant/actor/parent. Explicit empty/null/invalid fields and keys belonging to another parent are refused. This metadata does not replace qualification, object provenance or SQL admission gates.

Existing configs remain unchanged: restoration and every detector-plan recompile preserve the original presence/absence of the recovery key and reproduce the original config bytes/hash and compiler seal. No database rows are rewritten or migrations applied. Old saved configs without a key still require the original client key.

Added `restoreNativeInputSavedRequest`: reads the authorized original detector stage, recompiles the exact original request using the saved key, and repeats the saved-stage read to reject changed evidence. Manual parent advancement uses this path when the in-memory anchor key is absent. Original request body, intent/snapshot pins, authenticated scope and other anchor fields are still required; this is not yet a complete durable scheduler or arbitrary-run discovery mechanism.

Tests cover legacy/current exact detector replay, strict optional-key decoding, cross-operation rejection, invalid explicit keys, different-parent key rejection, and cancellation before IO. The PostgreSQL/object-store bootstrap fixture additionally checks original request recovery, but remains physically unexecuted without its isolated fixture. `go test ./...` passed across backend; semanticproposal completed in 1.956s. GPU/model and physical database acceptance are not claimed. Durable orchestration, initial API wiring, other V27 gaps and V28–V32 remain open.


### 2026-10-07 — V27 original anchor loading by tenant and parent run

Added `loadNativePreparationAnchor` and `advanceSavedNativeManualParent`: authenticated server callers can restore original request/key, published intent/inventory and pinned snapshot from tenant/parent IDs without retaining an in-memory request body. Server configuration still supplies the verified working color profile, metadata budget, scheduler and artifact catalog.

Forward-only migration 00805 adds API-only `semantic_native_input_saved_anchor_document`. It delegates original saved-stage authorization, pins the original config hash, matches immutable intent/snapshot refs, deterministic upload document IDs, completed ready Document uploads and verified catalog objects. It returns read-only locators and grants no admission/output authority. No migrations or database writes were executed.

The Go loader bounds config/profile/document bytes and accumulated metadata, verifies actual versioned object bytes/SHA through the existing pinned reader, recompiles the original request, invokes the full saved-stage decoder, compares snapshot bindings with the complete current target inventory, and repeats object pins, saved stage and inventory after IO. Missing legacy keys require the original client key; no fabricated identity or repair is performed.

Tests cover locator parsing, changed config SHA, absent/invalid pins, missing legacy request identity, malformed profiles and cancellation before IO. Extended the isolated native bootstrap fixture with complete original-anchor equality, insufficient-budget rejection and API-only ACL coverage. That fixture remains physically unexecuted while PostgreSQL/object-store fixtures are unavailable. `go test ./internal/semanticproposal` passed in 1.904s; `go test ./...` passed across backend. Statement marker and whitespace checks passed; neither proves SQL runtime acceptance.

Remaining work includes leased durable scheduling and stage advancement, initial create/API wiring, other V27 gaps, remaining V28–V32 scope, physical migrations/integration and GPU/photo-quality acceptance.


### 2026-10-07 — V27 durable manual-parent continuation scheduling

Forward-only migration 00806 adds owner-guarded continuation scheduling columns to the existing original detector run: enabled, token/deadline, next attempt, capped failure count and done. No new tenant table or DELETE allowlist change is introduced. Manual-only continuation is enabled in the same transaction as complete manual child sealing/receipt replay, through the original complete manual evidence gate; bare native drafts are not opted into this queue.

Scheduler-only claim/record functions discover project-scoped original actor/session context with live editor authorization, use SKIP LOCKED claims and persisted due times, and reject stale/expired tokens. Waiting uses five seconds; retry errors use capped exponential delay up to five minutes. Completed scheduling requires an existing sealed parent semantic graph and exact original V2.Admit queued receipt; terminal failure requires observed original defect task/run failure. Scheduling metadata does not grant model/output/admission authority.

Added SQLNativeManualParentRecovery with bounded attempts and transaction-free session advisory serialization across object IO. Unknown lock/unlock outcomes close the pinned session rather than returning it to the pool. Lease duration exceeds the bounded attempt timeout, and retry recording is fenced by token. The recovery path calls the full saved-anchor, original manual/native qualification, physical object, parent creation/replay and committed receipt boundaries. Outcome tests reject child/wrong-workflow/blocked tickets and tickets attached to waiting/failed observations; cancellation prevents claiming work.

Validation: semanticproposal passed in 1.856s before the final startup-validation strengthening; final go test ./... passed across backend. Physical PostgreSQL lease concurrency, token expiry, migration application and end-to-end queue execution remain unverified. The PostgreSQL parser is unavailable; source marker/Go checks are not SQL execution evidence. The scheduler-only ACL inventory was extended for the new functions but its isolated database fixture remains unexecuted.

The project-scoped ProcessOne component is implemented; production service-loop/factory wiring is still missing. Full initial-stage orchestration, remaining V27 authority branches, V28–V32 and GPU/photo-quality acceptance remain open. No production migrations, deployment or commits were performed.


### 2026-10-07 — scheduler lifecycle integration for native parent recovery

Added `RunSchedulerWithNativeRecovery` and a server-owned `SchedulerNativeRecoveryFactory` boundary. When supplied with a qualified processor, scheduler startup checks the saved-state/document, atomic manual sealing and fenced scheduling functions; missing installation fails before work is claimed. The processor runs in an independent existing expansion loop, contributes to scheduler readiness, is cancelled/joined during shutdown, and only then releases factory-owned resources. Shared scheduler/storage resources remain owned by the scheduler. Failed/cancelled/incomplete factory startup cleans up created resources.

The existing CLI `RunScheduler` delegates with no native factory. Consequently native continuation is NOT enabled in default CLI startup yet. A production factory still needs per-continuation trusted qualification and protection selection, API-role source/catalog access and publication dependencies. Current TransferPreparation requires source/target protection refs and qualification policy; inventing static refs or reports to make startup appear complete would bypass the required authority. This integration provides the runtime lifecycle boundary, not proof of a fully deployed native create/continuation path.

Tests cover factory resource lifetime, failed/missing dependencies, cancellation before invocation and during startup, disabled startup, and missing installation dependencies. `go test ./internal/workflow ./internal/semanticproposal` passed (workflow 0.654s); the final `go test ./...` passed after adding installation checks. Whitespace checks passed. Physical database/queue/GPU acceptance remains unverified; no migrations or deployment were performed.

Next required work: production qualified factory and default CLI configuration, dynamic original per-job source/target protection policy, full native initial-stage/API orchestration, physical migration/lease tests, remaining V27 branches and V28–V32.


### 2026-10-07 — original per-parent protection configuration recovery

Split deployment model/resource validation from complete protection-ref validation. Existing `validateTransferConfiguration` still requires both; transfer compilation, physical preparation and admission retain their complete configuration checks. Native recovery startup can validate deployment policy before reading original per-request refs, and still requires configured person qualification for queue processing.

Added `restoreNativeInputTransferProtectionConfiguration`: uses the original authorized saved-parent config reader, validates exact schema/hash/scope/qualification pin and node ordinals, selects one consistent original source root and target roots in original order, and repeats the saved config after extraction. Existing nonempty deployment-provided refs must match exactly; conflicting roots are refused. The returned refs are locators, not physical mask authority. Complete source/person/protection byte checks and admission locks remain required by subsequent reconstruction.

`loadAndResumeNativeInputManualTransfer` now restores this per-parent configuration before complete reconstruction. This removes the need for static project mask refs for recovery of an already saved parent. It does not select new target masks for initial creation or construct the production runtime factory. Initial parent creation with absent target policy still refuses; default CLI native recovery remains disabled.

Tests cover dynamically restored original refs, original ordinal preservation, detached configuration, inconsistent source roots, configured-ref mismatch, config/session/ordinal/qualification-related schema checks and cancellation before IO. `go test ./...` passed across backend (semanticproposal 2.048s). Whitespace checks passed. No migrations were added/applied in this step and no physical database/GPU acceptance is claimed.

Remaining work: initial target protection selection with genuine policy authority, runtime dependency factory/CLI activation, full native/API orchestration, physical migration and lease-concurrency fixtures, remaining V27 branches and V28–V32.


### 2026-10-07 — initial manual-parent target protection discovery

Added source-bound target protection selection for initial manual parent preparation when no target roots were explicitly configured. Candidate discovery reuses the existing authenticated protection-origin gate and canonical producer root/child provenance. Selection requires one unique root/pixel pair and refuses absent or ambiguous candidates. It does not choose newest, substitute a person mask, create an empty mask or repair missing producer evidence.

For each original target binding, selection physically verifies the protected MaskRef/NPY and independent qualified person coverage, accounts bounded metadata, repeats candidate discovery after streaming, repeats all earlier candidates after later target IO, and repeats the complete target inventory. Complete configuration validation follows selection. Subsequent transfer input preparation and transactional admission still independently verify and lock full original authority.

Integrated selection into `prepareAndActivateManualParentFromSubmission` after genuine source protection/person verification and only for a new parent; existing parents retain exact saved-config recovery. Explicit deployment target roots retain the original validation path. The supported discovery route concerns canonical task-produced roots accepted by the existing protection-origin gate; other protection provenance branches and generation of missing target protection are still open.

Tests cover unique locator selection, ambiguity including duplicate candidates, malformed refs, root/child aliasing and cancellation before IO. They do not prove physical producer/qualification authority. `go test ./internal/semanticproposal` passed in 1.990s; `go test ./...` passed across backend, and whitespace checks passed. Positive physical PostgreSQL/object-store target discovery and concurrent mutation fixtures remain unexecuted. No migrations or deployment occurred.

Remaining work: runtime dependency/qualification factory and CLI activation, complete automatic production of missing preparation stages/protections, physical migration/lease/selection acceptance, remaining V27 authority branches and V28–V32.


### 2026-10-07 — per-reference runtime artifact catalog factory

Added SQLNativeParentArtifactCatalogFactory with explicit API-role validation and original project/asset-version catalog binding. Native parent recovery may now construct its catalog per recovered original request through CatalogForReference, rather than sharing a catalog pinned to one version across project jobs. The saved anchor is loaded under the original parent advisory lock, and factory scope must match its authenticated actor, tenant, project and original reference version. Physical catalog Lookup authorization and full producer/qualification/admission checks remain unchanged.

Tests cover exact original scope/version routing, wrong actor/project rejection before callbacks, nil/error factory results and cancellation before SQL or after callbacks. The isolated native bootstrap fixture now uses the production per-reference factory for complete producer reconstruction and rejects a scheduler-role source catalog; this database fixture remains physically unexecuted. go test ./... passed; semanticproposal passed in 6.828s before the final integration-fixture extension, which was subsequently compiled/tested separately.

Production qualification selection, complete factory assembly/CLI activation, missing preparation stages/protection generation, real database/lease/object-store acceptance and remaining V27–V32 scope remain open. No new migrations, deployment or commits occurred.


### 2026-10-07 — person qualification policy/report discovery

Added mask.person report discovery through the existing authenticated person qualification object gate, requiring one unique report for the server-selected profile. Added selectNativePersonProtectionPolicy: reads the current admission-enabled DBA policy inventory, selects the unique person profile, reads the actual version-pinned imported report bytes, verifies SHA/content with v2qualification.Verify, repeats the original object pin and unique discovery, and repeats the policy snapshot. Bounded report/policy metadata is charged before IO. Discovery alone confers no scientific, producer or protection authority.

Native recovery supports server-owned DiscoverPersonPolicy when an explicit person policy/report is absent. Selection occurs inside the original parent lock after anchor recovery; every subsequent original source/target person proof, physical coverage and transactional admission gate remains mandatory. Existing explicit deployment policy/report paths remain available. No report is fabricated or selected by newest timestamp.

Tests cover cancellation before IO, invalid metadata budgets and rejection of non-person operations. They do not prove positive physical qualification discovery or policy/report mutation acceptance. go test ./internal/semanticproposal passed in 2.066s; go test ./... passed across backend. No migrations or deployment were performed.

Still open: semantic.transfer qualification/calibration pin selection and complete runtime-factory/CLI assembly, automatic missing stage/protection generation, physical PostgreSQL/object-store/lease acceptance, remaining V27 authority branches and V28–V32 scope.


### 2026-10-07 — semantic.transfer qualification/calibration discovery

Forward-only migration 00807 adds API-only bounded qualification candidate discovery. Candidate tuples carry original policy/qualification IDs, report/calibration refs and profile locks, and each passes the existing locked semantic proposal qualification gate. Candidates require exact project/tenant/profile, admitted licensed policy, passed trusted import with audit provenance, ready verified versioned unexpired report/calibration objects, and policy-allowed calibration SHA. Two candidates remain ambiguity; no newest selection is performed.

Added strict Go pin decoding with exact canonical field set and server-policy binding. `selectNativeTransferQualificationPolicy` chooses the unique admission policy, discovers one candidate, charges bounded metadata, physically reads/verifies report and calibration through the unchanged qualification preparation reader, retains metrics from the verified report, repeats unique discovery, and rechecks the policy snapshot. These outputs are compiler inputs only; source/person/protection and final admission gates remain separate.

Tests cover a valid original qualification locator, unknown/changed/null pin fields, cancellation before IO and invalid metadata budgets. The physical API-only ACL fixture was extended but remains unexecuted. go test ./... passed (semanticproposal 1.936s); a subsequent targeted package run verifies final policy-size guards. Migration 00807 is not applied or physically verified. No deployment occurred.

The selection helper still needs production runtime-factory integration and per-request resource/configuration assembly. Default CLI recovery remains disabled. Full initial stage generation, missing protection generation, PostgreSQL/object-store/lease concurrency acceptance, remaining V27 branches and V28–V32 remain open.


### 2026-10-07 — runtime transfer qualification/resource assembly

Added server-owned DiscoverTransferPolicy to native manual-parent recovery. Under the original parent lock, the processor selects and physically verifies the semantic.transfer report/calibration before constructing its configuration. Explicit qualification or expected policy, when supplied, must match the selected original tuple exactly; conflicting configuration is refused.

Unspecified additional model/transport costs are derived by rounding verified report peak RSS/VRAM/scratch up to safe integer bytes. Non-finite, negative and unsafe integer-range measurements refuse. Explicit installed costs remain supported but must cover observed peaks and fit deployment capacity. Task compilers still add their operation/input/transport overhead and enforce final profile limits. Existing saved graph replay still requires exact config/profile reconstruction; costs are never rewritten in stored graphs. Legacy graphs with different explicit original costs retain the explicit-configuration path and may refuse automatic reconstruction when compiler inputs differ.

Separated capacity-only startup validation from fully qualified model policy validation for the explicitly enabled discovery path. Full policy validation runs after selection, and complete protection refs and physical/source/person/admission gates remain required later. This does not enable default CLI recovery or construct missing producer outputs.

Tests cover fractional rounding, zero additional measured cost, NaN/infinity/negative/unsafe values, configured report/policy conflict, under-provisioned explicit costs, capacity overflow, detached base configuration and cancellation before runtime selection. go test ./... passed across backend. No migrations or deployment occurred in this step; physical database/GPU acceptance remains unverified.

Remaining work: production factory and CLI dependency assembly, multi-project discovery/dispatch, initial API/preparation orchestration and missing protection generation, physical migration/lease/model/quality acceptance, remaining V27 branches and V28–V32 scope.


### 2026-10-07 — native parent production dependency factory and project dispatcher

Added SQLNativeParentRecoveryFactory assembling separate validated API/scheduler role pools, the original project-scoped source reader, accepted-version document publication, scheduler-fenced transport publication, per-reference catalog construction, independent person/transfer qualification discovery and server-selected bounded resources/manual verification budgets. Pools/storage remain owned by the service. Factory capacities, reservation bounds, timeout and per-pass/aggregate manual limits refuse zero/unbounded configuration. Qualification and protection refs are selected per original tenant/request rather than fabricated at startup.

Added forward-only migration 00808 with scheduler-only project discovery and SQLNativeParentRecoveryDispatcher. Project discovery is a hint for due, explicitly enabled original manual continuations with active authenticated scope and persisted request identity. The actual project-scoped claim, full original-context reconstruction, physical authority and token-fenced recording repeat independently. A rotating cursor provides project fairness within a process; persisted due times remain authoritative across restart. No caller-supplied project mask/report refs are introduced.

Tests cover bounded factory capacity, manual limits, reservation/timeout failures, missing pools/storage and cancellation before SQL/discovery. The SQL ACL fixture now includes project discovery, but remains physically unexecuted. go test ./... passed (semanticproposal 3.265s). Migration 00808 is not applied or physically verified; final source hint filters were tightened after the Go run and do not constitute SQL execution acceptance.

Still open: service-owned API pool/configuration constructor, CLI factory activation, full initial API/preparation orchestration and missing protection generation, real PostgreSQL/migration/lease/object-store/GPU/quality acceptance, other V27 branches and V28–V32 scope. No deployment or commits occurred.


### 2026-10-07 — CLI activation of native manual-parent recovery

Scheduler now accepts optional `PHOTO_V27_NATIVE_RECOVERY_CONFIG` pointing to a bounded JSON file (16 KiB maximum). Absent configuration preserves the existing disabled path. `V27_NATIVE_SOURCE_DATABASE_URL` optionally supplies the source connection; otherwise the service database URL is used. The service creates and owns a separate four-connection pool with `photo_api` role, while the shared scheduler pool must use `photo_scheduler` and have at least two connections. Shutdown stops the recovery loop before closing the owned API pool. Installation checks now include migrations 00807 and 00808 functions. No migrations execute automatically.

Configuration requires schema_version=1, positive safe-integer ram_bytes, vram_bytes, scratch_bytes, output_bytes, metadata_bytes, reserved_bytes; metadata_bytes must fit RAM/8. timeout_seconds is 1..1740. Both manual and manual_batch require positive point_checks, edge_evaluations and payload_bytes. Unknown fields, trailing JSON, missing/zero limits and oversized content refuse startup. ICC data comes from PHOTO_ICC_ROOT. Configuration contains deployment limits only: qualifications, reports and protection refs remain selected and verified per original tenant/request. Actual hardware limits must be calibrated; this does not certify RTX3060 throughput or model quality.

Overall implementation estimate remains 50–60% of planned expert scope, not an acceptance percentage. Initial proposal/stage orchestration, missing protection generation, other V27 branches, substantial V28–V32 work and real database/object-store/GPU acceptance remain open. Migrations 00766–00808 remain unapplied and physically unverified.

Validation: `go test ./...` passed across backend, including workflow (1.470s) and CLI (0.466s). New tests exercise valid boundary limits, unsupported schema, excessive metadata/timeout/integer values, absent aggregate limits, unknown fields, malformed/trailing/oversized content and safe missing-file errors. Physical startup with real database roles and migration execution is still unverified.


### 2026-10-07 — composed initial FAN/parser child start

Added nativePreparationBootstrap.bootstrapFace for face.landmarks and face.parse. It validates server dependencies and bounded GPU/scratch/output/reservation costs, reconstitutes the exact sealed original detector plan, publishes source-derived immutable Params, prepares the independently qualified frozen child task, compiles the child graph, and rereads physical detector content after compilation. The completed original catalog and source/face/Params bytes must match the published selection. Existing blocked-draft persistence, object publication outside transactions, transport registration and commit-aware resume/seal enforce original dependency/qualification/source/quota authority. Returned receipt must match the exact child run/workflow and queued state. Cancellation or errors expose no submission; child receipt is not a semantic parent receipt.

This supplies the missing initial FAN/parser assembly boundary; it is not yet a complete API create service or a durable initial-stage progressor. Existing registered children should restore and resume their original plan. The physical bootstrap fixture now additionally exercises composed publication/admission replay for both original child families and compares exact seals/receipts. That physical fixture remains skipped without its isolated PostgreSQL/object-store environment; no inference or GPU quality acceptance is claimed.

Validation: go test ./... passed across backend (semanticproposal 2.021s, workflow 1.304s), followed by a targeted semanticproposal run after adding the physical-fixture replay assertion. Pure tests cover cancellation before infrastructure, missing dependencies, GPU capacity, transport reservation, integer limits and invalid measured costs. No migrations, production deployment or commits were performed. Remaining full-scope work includes durable initial detector→FAN/parser→skin/manual orchestration, API create wiring, missing target/person protection generation, other V27 branches and V28–V32 plus physical database/object-store/GPU/quality acceptance.


### 2026-10-07 — saved native face-pipeline advancement

Added advanceSavedNativeFacePipeline, a bounded step over original detector → landmarks → parser → skin checkpoints. Authenticated original stage observations determine waiting, terminal failure or blocked registration. Waiting/failed returns expose only phase and operation, with no producer plans or child receipts. Registered blocked stages restore exact original compiler plans and resume through existing complete admission/transport gates; missing checkpoints or transports refuse rather than fabricate authority. Successful observations independently invoke the complete physical detector, landmark, parser and skin content readers. After later IO, all original plans are reconstructed again and seals must match before producers are returned. The caller owns durable scheduling and the parent session lock; this helper holds no transaction across object IO.

Integrated this check into manual-parent continuation after successful manual stage observation, replacing direct producer reconstruction. Pending/failed upstream state contradicts a completed manual child and refuses parent publication. No child ticket becomes a parent acceptance receipt. The new helper does not yet create missing initial stages or persist initial-stage scheduling; full API create orchestration remains open.

Expanded the isolated physical bootstrap fixture to verify pending detector returns no downstream producer authority and completed four-stage reconstruction matches exact original seals. These physical assertions are unexecuted without PostgreSQL/object-store fixture. Pure tests verify cancelled and missing-infrastructure boundaries expose no stage authority. go test ./... passed across backend (semanticproposal 1.897s, workflow 1.215s); a subsequent targeted semanticproposal run checks the final waiting-fixture assertion. No migration or deployment occurred. Real DB/object-store/GPU acceptance, initial creation/scheduling, missing protection generation, other V27 branches and V28–V32 remain open.


### 2026-10-07 — authenticated stage presence and child ensure boundaries

Forward-only migration 00809 adds API-only semantic_native_input_stage_presence. Only an authenticated original detector checkpoint with the exact config SHA permits a result, and only deterministic landmark/parser/skin children are supported. No run at the scoped original child identity means absent; a run accepted by the unchanged saved-stage gate means present; a present run lacking the original gate is conflict. Unauthorized/missing/changed anchor returns SQL NULL, never absence. Fixed search_path, owner photo_fn_owner and PUBLIC revoke are retained. Presence is a read-only hint; original begin/register/seal gates still decide authority under concurrent changes. No new tenant tables or DELETE privileges were introduced.

Added bounded Go presence decoding and nativePreparationBootstrap.ensureFace/ensureSkin. A positive absent hint selects existing fully qualified physical initial assembly. Present selects complete original-plan reconstruction and commit-aware transport/admission replay, retaining original report/resource compiler inputs rather than overwriting saved graphs. Conflict, unknown/missing result, cancellation or failed restore refuses; none falls back to new creation. The durable caller must hold the original parent lock and scope transport publication to the deterministic child run. Exact child run/workflow and queued receipt are required, and never represent public parent admission.

Physical fixture now covers original absence before downstream creation, present after creation, API-only ACL inventory and exact present-child ensure replay. It remains unexecuted without isolated PostgreSQL/object-store fixtures; migration 00809 is unapplied and physically unverified. Pure tests exercise strict presence states, cancellation before IO, missing infrastructure and no exposed child submission. Full initial-stage scheduling, API create wiring and per-stage report discovery remain open, alongside missing protection generation, remaining V27/V28–V32 work and real DB/GPU/quality acceptance. No deployment or commit occurred.

Validation: final `go test ./...` passed across backend (semanticproposal 1.977s, workflow 1.304s). Physical integration fixtures remain skipped without the isolated environment.


### 2026-10-07 — initial native face pipeline with explicit creation capability

Added server-owned nativeFacePipelineStartConfiguration with bounded per-stage landmark/parser qualification report refs and compiler budgets, bounded CPU skin budgets and a transport-document publisher. Added advanceInitialFacePipeline composing original saved detector recovery, authenticated stage presence and ensureFace/ensureSkin. Each attempt creates at most one positively absent child, scopes transport publication to the deterministic original tenant/run and returns an internal waiting phase without producer plans or child receipts. It neither sleeps for inference nor treats failed presence/restore as absence. Existing stages retain exact saved qualification/resource compiler inputs. Saved recovery without a starter keeps its no-publication behavior.

Initial blocked-stage advancement now invokes the explicit creation-capable ensure boundary to repeat immutable transport publication/registration from the exact restored original plan before commit-aware resume. This supports interruptions after blocked graph persistence but before transport registration. All original physical qualification/source/dependency and transactional begin/register/seal gates remain mandatory. Completed stage observations still require independent physical content readers and repeated original-plan reconstruction. Parent session locks and persisted scheduling are owned by the durable caller, not this bounded helper.

Tests cover cancellation before publication and server configuration bounds (report identity/digest, GPU budgets, skin reservation and safe integers). The isolated physical fixture additionally invokes the initial advancement path against complete original four-stage checkpoints and checks exact seals. It remains unexecuted without isolated PostgreSQL/object storage. go test ./... passed after initial composition; a final full backend run checks the added transport repair logic. Migration 00809 remains unapplied/physically unverified.

Still open: durable initial-stage claims/enabling/recording and production runtime/CLI/API create wiring, automatic original stage qualification report selection, initial detector bootstrap/public parent receipt integration, missing target/person protection generation, remaining V27 branches and V28–V32 plus database/object-store/GPU/quality acceptance. This step does not claim a complete API create service or live initial-stage scheduler. No deployment or commit occurred.


### 2026-10-07 — automatic initial FAN/parser qualification assembly

Added selectInitialFacePipelineConfiguration selecting unique face.landmarks and face.parse qualification reports under the admission-enabled current server policy. Candidate discovery uses the existing authenticated face-qualification object gate and refuses missing/ambiguous candidates. Before object IO, aggregate policy plus actual report sizes are charged against the server metadata budget. Each report is physically read and verified through VerifyFaceQualification; its object pin is checked against the pre-read pin. After both reads, both unique report candidates/object pins and the original policy snapshot are repeated. No user-supplied report IDs or newest-report selection are introduced.

Installed model costs for new children derive from verified report RSS/VRAM/scratch peaks, rounded up to safe integer bytes. Invalid/non-finite/negative/unsafe peaks and costs exceeding deployed capacities refuse; transport reservation and GPU/CPU stage budgets remain bounded. Operation compilers still calculate their independent input/operation overhead and final admission profiles. Added advanceQualifiedInitialFacePipeline combining this server-owned selection with original-checkpoint advancement and explicit absent-child creation. Existing checkpoints retain original qualification/resource inputs; discovery confers no producer or final parent authority.

Pure tests cover conservative fractional rounding, invalid/excessive metrics, insufficient reservation and cancellation before IO/creation. The isolated physical fixture now checks discovered reports against original imported face refs and invokes the automatic qualified pipeline on saved stages, comparing exact original seals. It remains skipped without isolated PostgreSQL/object-store configuration, so physical policy/report mutation and creation acceptance are unverified. go test ./... passed across backend (semanticproposal 1.895s, workflow 1.223s), followed by targeted semanticproposal verification of final fixture assertions. No migrations, deployment or commit occurred.

Remaining full scope: persisted initial-stage scheduling and runtime/CLI/API create assembly, detector bootstrap and public parent receipt integration, defect/person missing protection generation, other V27 branches and V28–V32 plus real database/object-store/GPU/quality acceptance. Migration 00809 remains unapplied and physically unverified.


### 2026-10-07 — persisted initial producer scheduling boundary

Forward-only migration 00810 adds an independent initial producer lease queue on the original detector run: enabled/token/until/after/failures/done columns with a private owner-only write trigger. This is separate from the manual-parent recovery queue; no new tenant table or DELETE privilege is introduced. Scheduler-only seal_and_enable_v27_native_initial_pipeline calls the unchanged complete native detector seal/admission function and enables initial scheduling in the same transaction. Missing persisted original request identity causes transaction failure, never a child acceptance followed by untracked scheduling. The Go sealAndEnableInitialDetectorInTransaction wrapper reuses full physical admission evidence and exact commit-aware receipt validation. Existing default detector/person sealing remains unchanged.

Scheduler-only claim_v27_native_initial_pipeline selects due enabled original sealed detector runs using active tenant/project/editor/session scope and deterministic original parent identity, then assigns a SKIP LOCKED token-fenced bounded lease. record_v27_native_initial_pipeline rejects missing, expired or stale tokens; waiting backs off five seconds and retry uses bounded exponential backoff. Completed requires all four original detector/landmark/parser/skin saved-stage gates with sealed succeeded tasks; failure requires a terminal original deterministic producer with matching actor/project/session/parent/request and one version-1 task. SQL completion is catalog state evidence only: a production processor must still perform all physical result verification before recording completed.

Extended physical fixture ACL inventory and assertions for atomic enable, original-scope lease, stale-token rejection and all-four-stage completion recording. Fixture remains unexecuted without isolated PostgreSQL/object-store environment. Migration 00810 is unapplied and physically unverified. go test ./... passed across backend (semanticproposal 1.916s, workflow 1.225s); final targeted packages check added fixture assertions. No production migration, deployment or commit occurred.

Remaining work includes a production initial queue processor and project discovery/runtime/CLI wiring, API create/bootstrap/public parent integration, missing defect/person/target protection generation, other V27 branches and V28–V32 plus database/object-store/GPU/quality acceptance. Initial scheduling is not enabled on the default detector path yet; activation remains an explicit complete-boundary capability.


### 2026-10-07 — production initial queue processor and scheduler dispatch

Added SQLNativeInitialPipelineRecovery with project-scoped persisted claims, bounded attempt timeout and the same original-parent session lock namespace as manual continuation. Each attempt loads the complete physical original request/intent/snapshot anchor, resolves the original reference catalog, assembles trusted server policy/report/resource inputs and advances the original detector→landmarks→parser→skin pipeline. Cancelled service contexts leave leases to expire; busy parent locks record waiting; errors record retry with SQL backoff. Finish recording uses a bounded separate timeout and must accept the current lease token. No child ticket is a parent acceptance receipt.

Completion classification refuses missing/wrong parent, pending producer plans, unknown operations/phases and mismatched producer linkage, and verifies complete detector/landmark/parser/skin plan seals. These catalog/compiler checks supplement, not replace, physical result verification performed during Recover. Initial queue completion means producer readiness only; defect/manual/parent admission remains a later independent obligation.

Added InitialForProject to the validated service dependency factory. Forward-only migration 00811 adds scheduler-only due initial project discovery. Added initial project cursor dispatch and a combined SQLNativeRecoveryDispatcher alternating initial and manual-parent queues; when one has no due work it tries the other immediately. CLI native recovery configuration now creates the combined processor. Enabled startup requires stage presence plus atomic initial enable/claim/record and initial-project functions (00809–00811); absent configuration still disables native recovery. Default detector bootstrap does not automatically enable initial scheduling.

Pure tests cover cancellation before claims, missing dependencies, nil dispatcher, waiting/failed outcomes without producer authority, retry error preservation and incomplete/foreign completion refusal. Extended isolated physical fixture ACL inventory and direct locked production Recover/completion classification; fixture remains unexecuted without its PostgreSQL/object-store environment. go test ./... passed after production assembly (semanticproposal 2.085s, workflow 1.398s); a final full run checks new tests/fixture assertions. Migrations 00810/00811 remain unapplied and physically unverified. No deployment or commits occurred.

Remaining: API create/bootstrap and explicit atomic scheduling activation/public parent receipt integration; initial→defect/manual/person/target protection progression; positive concurrent DB/object-store/lease/GPU/quality acceptance; remaining V27 branches and V28–V32 scope. The goal remains full V20–V32 implementation, not initial producer readiness.


### 2026-10-07 — initial detector bootstrap with atomic scheduling activation

Added bootstrapInitialDetector using the existing complete request/source/qualification/Params/frozen-plan preparation and blocked-input publication, then resumeInitialDetectorPreparation. Resume reconstructs the exact registered original transport and invokes full detector seal plus initial scheduling enable in the same retried scheduler transaction. It emits a private detector receipt only after a known successful commit; publication/registration stays outside the admission transaction. The default bootstrapDetector remains available for the existing non-initial contour, with shared preparation logic to avoid compiler drift. No child receipt is a public semantic parent receipt.

The physical bootstrap fixture now starts and replays the initial detector path and checks persisted enabled=true, done=false and no lease before claims. Its later original producer/lease/stale-token/completion assertions remain intact. The isolated physical fixture is unexecuted without PostgreSQL/object-store configuration; migrations 00810/00811 remain unapplied and physically unverified. Pure tests cover cancelled initial preparation and replay before any SQL/publication with no exposed plan or receipt. go test ./... passed across backend (semanticproposal 1.993s, workflow 1.398s); a final targeted semanticproposal run compiles the persisted scheduling assertion.

Remaining: production API create and original detector report selection/public parent receipt integration, initial→defect/manual/person/target protection advancement, real DB/object-store/lease/GPU/quality acceptance, other V27 branches and V28–V32. No production migrations, deployment or commits occurred.


### 2026-10-07 — server-qualified initial detector start

Added selectInitialDetectorConfiguration: selects the current admission-enabled face.detect policy, requires one unique authenticated report candidate, bounds aggregate policy/report metadata before IO, physically verifies the report, derives safe rounded model resource costs and repeats the original report object pin, unique candidate and policy snapshot. No user-supplied report or newest candidate choice is used. Scientific/model quality and source/result authority remain independent gates.

Added bootstrapQualifiedInitialDetector as a new-start boundary composing owned original request identity, finite score threshold .5.. .99 and max_faces 1..100, matching ICC profile, selected report/resource inputs, deterministic original tenant/detector transport scope and atomic bootstrapInitialDetector admission/scheduling. Existing saved checkpoints must restore and resume original compiler inputs; failed restore is not permission to use this creation path. Caller retains original-parent locking and public parent response ownership; no detector child receipt is a public semantic receipt.

Tests cover control boundaries/non-finite scores, cancellation before report IO/start and no exposed plan/receipt. The isolated physical bootstrap fixture now asserts the automatically selected detector report matches the original imported report; it remains unexecuted without PostgreSQL/object-store fixtures. A full backend go test ./... run validates new code/tests; physical migrations/GPU/quality acceptance remains unverified. No migration, deployment or commit occurred.

Remaining full scope: production API create/parent receipt integration, initial→defect/manual/person/target protection progression, real database/object-store/lease/GPU/quality acceptance, other V27 branches and V28–V32.


### 2026-10-07 — automatic CPU defect qualification and manual-stage start

Added selectInitialDefectConfiguration under the unique admission-enabled defects.propose CPU policy. It discovers one authenticated current candidate, bounds aggregate policy/report bytes before IO, physically reads and verifies the pinned report, derives bounded rounded installed costs and repeats candidate/object/policy state before returning compiler inputs. initialNativeDefectBudget explicitly rejects any nonzero measured GPU/model VRAM and produces a CPU-only stage budget. Independent source/face/manual/protected-output and transactional admission evidence remains mandatory.

Added bootstrapQualifiedManualDefect composing complete original face producer plans, verified CPU report/budget selection, deterministic original tenant/defect transport scope and the existing physical manual raster/vector Params/dependency/qualification/publication/admission bootstrap. Face inputs remain server-owned locators and every manual producer is independently verified. Missing/invalid manual evidence refuses; this wrapper does not substitute a bare defect path. It exposes only a private child submission, never public parent admission. Detector, face and defect selection now validate deployment capacity/reservation before database/report IO.

Tests cover CPU-only rounded RSS/scratch costs, rejection of even fractional GPU costs, cancellation before selection/publication and no retained report/plan. go test ./... passed across backend (semanticproposal 1.987s, workflow 1.312s); final targeted packages validate early capacity guards. Positive physical defect selection and automatic manual creation are still unverified without isolated PostgreSQL/object-store fixtures. No migration, deployment or commit occurred.

Remaining: automatic original manual face selection and initial→manual durable handoff/runtime scheduling, bare/permanent/additional authority branches, production API create/public parent receipt integration, missing person/target protection generation, real DB/object-store/GPU/quality acceptance and remaining V27/V28–V32 scope.


### 2026-10-07 — verified manual-target binding to original native faces

Added bindInitialManualFaceInputs for bounded server-selected target locators and original native face declarations. Inputs are deep-owned through canonical bytes. Each target must be a distinct unprotected manual mask; global targets bind all original faces, object-bound targets only the exact original face. Duplicate face identities, existing manual declarations, multiple matching targets (including global/specific overlap), unused foreign targets and missing/oversized collections refuse. No newest mask or silent truncation is selected. This pure binding step does not attest physical mask authority.

Added selectInitialManualFaceInputs reading complete original landmark/skin results, deriving the original native face bindings, then independently verifying every selected original manual raster/vector producer and its actual target/protected arrays under a shared bounded raster budget. Later face IO repeats the complete landmark/skin contents and every retained manual proof; saved binding comparison must preserve exact original faces. Target count/body and startup capacities are bounded before expensive IO.

Added bootstrapSelectedQualifiedManualDefect composing these verified face bindings with current trusted CPU defect qualification, deterministic original transport scope and the complete manual initial bootstrap. No failure falls back to a bare protection path. Caller must supply original server-selected target locators; persisted target discovery and durable scheduler/API handoff are still open. This step does not claim automatic discovery from arbitrary user masks or a complete live manual pipeline.

Tests cover owned global bindings, no cross-face aliasing, ambiguous global/specific candidates, duplicate masks, foreign unused object bindings and cancellation. go test ./... passed across backend (semanticproposal 1.985s, workflow 1.309s); final targeted packages validate startup guards. Positive physical automatic target selection/manual creation remains unverified. No migrations, deployment or commits occurred. Remaining full scope includes persisted manual locator selection/handoff, API create/public parent integration, missing person/target protection generation, other V27 branches and V28–V32 plus real DB/object-store/GPU/quality acceptance.


### 2026-10-07 — original manual target reconstruction from pixel locators

Added review.ResolveOriginalManualTarget extracting a target declaration only from original SHA-pinned rasterized-mask manifest bytes, requiring the exact selected pixel ref and invoking the existing complete manual source/output manifest verifier. It returns an owned original target; manifest verification does not confer task producer or physical raster authority. Wrong pixels, changed root bytes and foreign source bindings refuse. No canvas/transform/object/confidence/selector fields are invented.

Added SQLTransferSourceReader.readOriginalManualTarget resolving original pre-producer catalog pins under authenticated source/version/project scope, physically reading the original root and source, reconstructing its exact target declaration, then running unchanged full original manual Params/vector/target/protected raster verification. Its retained original object closure must equal the initial catalog and is rechecked before output. Cancellation/errors return no manual locator authority. This provides the content reconstruction boundary needed by persisted locator discovery, not a newest-mask selector.

Tests use the real Python-produced manual-root JSON fixture to verify original target extraction, foreign pixel/source rejection and exact root SHA; a reader test verifies cancellation before SQL/object IO. Full backend go test ./... validates the added source and tests. Positive PostgreSQL/object-store producer/pixel acceptance remains unverified. No migration, deployment or commit occurred.

Still open: persisted manual candidate discovery and original request selection/handoff to scheduler, full API create/public parent integration, missing person/target protection generation, other V27 branches and V28–V32 plus physical DB/object-store/GPU/quality acceptance.


### 2026-10-07 — explicit original manual-pixel selection assembly

Repository inspection confirms SemanticEditIntent.reference_mask is a source-bound uploaded user MaskRef/document (semantic_proposal_catalog checks v2_draft_uploads/user_mask_binding), not an original mask.rasterize producer root. It must not be treated as a manual raster origin or used to choose unrelated historical manual tasks. Original manual selections require separate explicit pixel locators and complete producer verification.

Added validateInitialManualPixelSelection requiring 1..64 distinct nonnil artifact refs with valid SHA digests; no missing/duplicate/oversized selection is silently accepted. selectInitialManualFacesFromPixels owns the selected refs, verifies complete original native producer plans, resolves each target declaration from physically verified original source-bound manual manifests/Params/vectors/arrays, and invokes the existing original-face binding and repeated proof checks under a shared bounded raster budget. Caller supplies explicit original locators; candidate search over unrelated historical masks is not performed.

Added bootstrapPixelSelectedManualDefect composing bounded deployment budgets, matching ICC, original pixel selection, original face/manual verification, trusted CPU qualification, deterministic transport publication and full initial manual admission. No reference_mask substitution or bare fallback is introduced. Persisting explicit selection in the original request/initial checkpoint and wiring that state into scheduler/API remains open.

Tests cover missing/invalid/duplicate/oversized refs and cancellation before IO with no face output. Full backend go test ./... verifies final code/tests; positive physical original-pixel-to-manual bootstrap acceptance remains unverified. No migration, deployment or commit occurred. Remaining scope includes durable manual selection/handoff, API parent create/admission, missing person/target protection, other V27 branches and V28–V32 plus real DB/object-store/GPU/quality acceptance.


### 2026-10-07 — immutable original manual selection checkpoint

Added nativeManualSelectionCheckpoint with exact schema/parent/request hash/project/actor/session/source, 1..64 explicit distinct pixel refs and finite ordered diameter fractions. Compiler verifies the sealed original detector and its exact auth session, owns selected refs and emits canonical body/SHA. Decoder reconstructs the expected full body from the original detector, rejecting changed scope/source/request/fields or SHA. Checkpoint locators are not physical manual authority; every resumed target still needs original raster/vector/Params/content and final admission verification.

Forward-only migration 00812 stores immutable selection text plus SHA on the original detector run, with a paired size/digest constraint and owner-only mutation trigger. Scheduler-only save gate requires exact current tenant/editor/session/project/detector config/source/request scope. Re-saving exact body/hash is idempotent; replacing an existing selection refuses. API-only read delegates the unchanged original detector saved-stage gate and exact config SHA. No tenant table or DELETE privileges are introduced. SQL stores restart locators only; it does not attest manual raster science or producer authority.

Added saveNativeManualSelection under a scheduler-role transaction/tenant setting and readNativeManualSelection under authenticated API scope with complete typed decoding and repeated original body/SHA. No failed save/read exposes acceptance or manual proof. Pure tests cover unsupported schema, missing identities/refs, invalid digests/non-finite/inverted diameter and cancellation before store IO. go test ./... passed across backend (semanticproposal 2.052s, workflow 1.327s), followed by targeted package tests for the new checkpoint bounds. Migration 00812 is unapplied and physically unverified; positive persistence/immutability/ACL/concurrency acceptance remains open.

Remaining full scope: integrate explicit selection persistence with atomic detector bootstrap and initial→manual scheduler handoff, production API create/public parent admission, bare/permanent/additional paths, missing person/target protection, other V27 branches and V28–V32 plus real DB/object-store/GPU/quality acceptance. No deployment or commits occurred.


### V27: atomic initial manual selection admission (2026-10-07)

Implemented `bootstrapInitialDetectorWithManualSelection` and
`resumeInitialDetectorWithManualSelection`. Explicit immutable manual pixel
locators and diameter bounds are validated before publication. Admission
recovers the original detector transport, saves the selection through migration
00812, seals the detector and enables the initial queue through migration 00810
in the same scheduler transaction. A selection conflict rolls back admission;
a job ticket is returned only after confirmed commit. The owned selection slice
is retained across transaction retries. Manual locators remain restart inputs;
physical source/mask closure verification is still mandatory downstream.

Validation: `go test ./...` passed for the backend (semanticproposal 1.912s,
workflow 1.218s). A separate cancellation regression covers both new entry
points and verifies no receipt is exposed. Physical PostgreSQL transaction,
rollback and concurrent scheduler acceptance remain unverified; migrations
00766–00812 have not been applied as part of this work. This completes an
internal admission building block, not public SemanticPropose wiring or the
manual handoff after producer readiness. Overall 50–60% is an expert scope
estimate, not an acceptance score.


### Qualified manual initial start and checkpoint-to-defect boundary (2026-10-07)

`bootstrapQualifiedInitialManualDetector` now shares the complete current-policy,
physical qualification and repeated pin checks with the ordinary qualified
detector start; the explicit owned selection reaches atomic admission. Added
`bootstrapCheckpointSelectedManualDefect`: validates capacity before IO, verifies
face producer seals, reads and verifies the original immutable selection, then
uses the original physical manual mask/face closure and CPU qualification path.
It is a new-child boundary requiring the original parent lock and independently
proven absence of the defect child. It is not yet called automatically by the
initial queue: defect-child presence/restoration and SQL completion handoff gates
remain required. No public API wiring or physical database acceptance is claimed.

Validation: semanticproposal and workflow targeted tests passed after shared
qualified-start changes; cancellation of the qualified manual entry point is
covered. PostgreSQL migration/rollback/concurrency acceptance remains pending.


### Original defect-child presence and restoration boundary (2026-10-07)

Forward-only migration 00813 extends authenticated stage presence to
`defects.propose`, using the original `semantic.defect.prepare:reference` UUID
namespace rather than deriving it from the operation name. Existing invalid
children return conflict; an unauthorized/missing anchor returns no authority.
`ensureCheckpointSelectedManualDefect` creates only on authenticated absence;
on presence it restores the original initial plan and registered transport
admission. Restoration failures return errors without recreation. Capacity and
document dependencies are checked before IO; cancellation exposes no receipt.

Validation: semanticproposal (2.010s) and workflow (1.307s) passed; cancellation
regression added. Migration 00813 is unapplied and physical PostgreSQL presence,
ACL and concurrent-child acceptance are unverified. This boundary is not wired
into the initial queue yet: exact persisted-selection versus original defect
Params equivalence and completed handoff SQL gates remain necessary. A partially
registered existing child currently fails closed; bounded original registration
republication must be handled before automatic scheduling can claim resilience.


### Persisted manual selection versus original defect Params (2026-10-07)

Existing-child ensure now calls `verifyCheckpointManualDefectSelection` before
repeat admission. It independently reads the immutable original detector-bound
selection, resolves original physical pixel/manual closures and face inputs,
recompiles the complete frozen Params without publication, then compares Params
(including diameter, input and root inventory) and all four original face-plan
seals with the restored defect plan. A mismatch returns conflict without a child
receipt or replacement. Capacity validation still precedes IO. The original
registered-transport restore path remains required; partial-registration recovery
and automatic initial-queue handoff are still incomplete.

Validation: semanticproposal passed after the matching implementation (3.199s);
added cancellation-before-IO regression and reran semanticproposal/workflow.
No physical PostgreSQL/object-store mismatch acceptance is claimed. Migration
00813 remains unapplied; the initial queue completion gate is unchanged.


### Interrupted original manual child transport publication (2026-10-07)

Existing-child ensure now reads the authenticated original saved task/run state.
An unsealed blocked draft repeats begin/publish/register-and-seal using the
restored initial plan and a document publisher scoped to its original tenant
and defect run. Original report, costs, capacities and Params are restored from
saved evidence; current defaults do not replace them. Sealed children retain
the registered-transport recovery path. Terminal failed state refuses admission.
The returned child receipt is checked against original run/workflow and queued
state, and cancellation returns no receipt. No queue wiring was added yet.

Code inspection corrected the earlier limitation: restoring the initial manual
plan needs registered upstream face transports, but does not require the defect
child's own transport. This permits original blocked-child republication without
a replacement plan. Physical crash-after-begin/object-publication acceptance is
still pending along with unapplied migrations and automatic queue handoff.


### Initial producer queue to explicit manual child handoff (2026-10-07)

Migration 00814 adds authenticated original manual-selection presence with
paired-null absence and byte SHA verification. NULL/unauthorized observations
fail closed. After successful physical face-pipeline recovery, initial recovery
now creates/restores the explicit checkpoint-selected manual child under the
same original parent lock. No-selection retains producer-readiness behavior;
conflicting/corrupt selection never selects another branch.

Migration 00815 adds `record_v27_native_initial_pipeline_handoff`: under the
original detector row lock and fenced live lease, manual completion requires
immutable selection SHA, enabled parent recovery, authenticated original saved
defect stage, sealed child and original request SHA. It then delegates unchanged
four-producer completion/lease recording to 00810. Go now records through this
wrapper; enabled startup requires both new functions. Child complete admission
already enables parent recovery atomically in the existing seal transaction.

Migrations 00814–00815 are unapplied and physically unverified. Public create
API wiring, full native non-manual branches and real concurrency/crash acceptance
remain incomplete. The older record function still has its original scheduler
EXECUTE grant; bypass prevention and rejection of late selection insertion after
initial completion need further SQL hardening before production acceptance.


### No bypass or late manual branch attachment (2026-10-07)

Forward-only migration 00816 revokes scheduler and PUBLIC execution on the old
producer-only record function. The SECURITY DEFINER handoff wrapper retains
owner-only delegation. Enabled startup now checks that scheduler cannot execute
the old function and can execute the handoff wrapper, so an 00815-only install
fails readiness rather than silently accepting bypass privileges.

The immutable selection save function now rejects a first insertion after
initial completion, acquisition of an initial recovery token, or parent recovery
enablement. Exact same-body/SHA replays still succeed before this guard. Detector
row locking serializes this decision with claims and completion. Initial manual
selection should be committed atomically with detector admission through the
existing new-start path; a late new branch must not modify a recovered run.

Physical integration fixture now uses the handoff recorder for original/stale
lease checks and asserts scheduler cannot execute the old recorder. The fixture
has not run on a physical database; migration 00816 remains unapplied. Targeted
Go tests compile the fixture but do not prove PostgreSQL ACL/concurrency behavior.


### Physical fixture coverage for selection absence and late insertion (2026-10-07)

Extended the original native bootstrap physical fixture after fenced initial
completion: authenticated absence succeeds, a foreign auth session cannot obtain
absence authority, a valid original checkpoint round trip preserves scope and
diameter, a first selection insertion after completion returns conflict, and a
subsequent read confirms selection remains absent. The test uses a valid source
ref only as a non-authoritative locator to exercise the late-insertion guard;
it does not represent an accepted manual mask or successful manual admission.

Environment revalidated without exposing connection strings: isolated fixture
DB configuration is absent and localhost PostgreSQL 55432 is not reachable.
The physical scenario therefore remains unexecuted. Targeted Go tests compile
the fixture; they do not prove live ACL/rollback/concurrency behavior.


### Terminal original manual child ends initial retries (2026-10-07)

Migration 00817 adds an authenticated original-detector-bound terminal child
observation with exact tenant/project/actor/session/parent/request SHA, deterministic
defect ID and single version-1 task. Initial recovery checks it only after a
present immutable manual selection. A terminal original defect becomes a failed
initial outcome with no producer or receipt; SQL independently rechecks the same
terminal lineage under the live fenced lease before recording done. The old
producer recorder remains unavailable to scheduler. Startup requires the new
terminal observation function. Nonterminal errors remain retryable.

Added outcome regression for terminal manual failure and rejection of manual
waiting without evidence. Migration 00817 is unapplied; physical SQL terminal
lineage, cancellation/concurrency acceptance remain unverified.


### Terminal task identity and reader ACL coverage (2026-10-07)

Forward-only migration 00818 strengthens terminal observation and independently
fenced recording with native schema_version=1, exact native task_id and operation
matching. Detector retains its existing operation-field exception; other native
stages require exact operation declarations. A terminal single task with a
foreign task locator in config cannot finish initial recovery.

Physical fixture ACL inventory now covers manual selection presence, terminal
child observation and immutable selection reading as photo_api-only. It also
asserts absent selection and a foreign session do not grant terminal authority.
Targeted semanticproposal/workflow Go tests compile these scenarios; PostgreSQL
fixture remains unavailable, so migration 00818 and live ACL/task mutation
acceptance are unverified. Public initial create API and full product acceptance
remain incomplete.


### Initial recovery publication dependencies and aggregate raster meter (2026-10-07)

Initial recovery now validates its concrete v2draft Params publisher DB/storage
and exact reader bucket before queue claim. Production factory already supplies
these dependencies; an incomplete manually constructed job fails early.

Each locked recovery attempt creates one reader-owned manual raster scope and
passes it through the bootstrap. Nested checkpoint reconstruction, physical mask
selection, Params verification, publication and admission rechecks share the
aggregate meter instead of each starting a new per-call batch budget. A new
recovery attempt gets a fresh meter; no mutable budget is shared across jobs.
Deployment batch limits must cover actual repeated verification work for the
complete attempt; exhaustion remains an error, never a skipped physical check.

Targeted Go tests passed after correction of concrete publisher validation.
Physical full-manual attempts on realistic large masks, configured budget sizing
and PostgreSQL/object-store acceptance remain unverified.


### Complete checkpoint codec before terminal handoff (2026-10-07)

Initial recovery now reads and decodes the original immutable manual selection
before terminal child observation. Presence and raw SHA alone are insufficient
for a terminal transition: full checkpoint tenant/request/project/actor/session,
source binding, pixel bounds, diameter and canonical expected SHA must match the
original detector. Codec failure returns retry/error without a terminal outcome
or replacement child. Nonterminal ensure retains its later repeated reads.

Code inspection also confirms `loadNativePreparationAnchor` requires an existing
detector recovery row; it cannot be reused as a first-start bootstrap loader.
The public initial-create boundary still needs its independent request/intent/
snapshot preparation path. Targeted Go tests passed; physical damaged-checkpoint
and first-create acceptance remain unverified.


### New initial anchor preparation boundary (2026-10-07)

Added `prepareInitialNativeAnchor`: validates bootstrap dependencies, authenticated
identity, owned deployment capacity and reservation before publication; compiles
the original request, publishes owner-scoped intent, publishes current original
target snapshot and owns snapshot bytes under the metadata budget. It does not
read a detector checkpoint or return an admission receipt. Existing detector
restoration remains separate; caller must hold the original parent lock.

Physical bootstrap fixture compares this new path with independently published
original intent/snapshot before detector start. Cancellation-before-publication
is covered. The fixture is unexecuted without isolated PostgreSQL/object store.
Public create wiring and authenticated detector-absence selection still remain
required; this helper alone is not a production create API.


### Authenticated first-detector branch (2026-10-07)

Migration 00819 adds editor-scoped initial detector presence: deterministic
original run absence, exact saved request-SHA presence, conflict for an existing
invalid checkpoint, and no observation for unauthorized scope. It is a hint, not
source/intent/snapshot or graph admission authority.

`prepareAndStartNewInitialManualDetector` validates controls, explicit pixels,
diameter and owned capacity, checks absence, prepares original intent/snapshot
and invokes qualified atomic manual detector admission. Caller must hold the
original parent lock. Existing detector presence returns conflict for routing to
a separate original-anchor restore path; failed restoration never uses new-start.
The physical fixture now asserts absence before bootstrap and presence afterward,
and includes the new reader in its API-only ACL matrix. Migration is unapplied;
fixture remains unexecuted. Public service routing is still incomplete.


### Existing initial manual detector replay boundary (2026-10-07)

Added `resumeExistingInitialManualDetector`: under the caller's original parent
lock, loads the physical original anchor, compares the complete original request
and idempotency key, restores saved detector report/resources/config, and requires
exact immutable manual pixel order and diameter. Blocked replay republishes only
original transport; sealed replay uses registered original transport. Receipt
scope/state and cancellation are checked before returning. No new qualification
or target snapshot replaces a failed restore.

Missing immutable manual selection currently fails closed, including a crash
after detector draft persistence but before atomic selection/admission. Such a
pre-admission crash needs a separately authenticated pending selection checkpoint
or an explicit bounded recovery design; this helper does not silently attach a
new branch. Public start/resume service routing and physical replay acceptance
remain incomplete. Cancellation regression added; targeted Go tests run.


### Manual selection survives pre-admission transport failure (2026-10-07)

Initial manual bootstrap now invokes `persistInitialManualDetectorInputs`:
validates/owns explicit selection, creates the original blocked detector draft
and saves its immutable manual checkpoint in one SQL transaction before transport
publication. A checkpoint failure rolls back draft ownership. Transport IO remains
outside SQL; failed IO leaves the original draft and exact selection recoverable.
Final atomic selection/detector seal/queue enable repeats the same immutable
selection idempotently. Ordinary detector persistence is unchanged.

This closes the documented crash window for newly created manual detector drafts
using this path; historical drafts created before the change with no selection
still fail closed. No migration was required. Added cancellation-before-draft
regression. Physical crash/unknown-commit/rollback acceptance and public service
routing remain unverified/incomplete.


### Locked initial manual start/resume routing (2026-10-07)

Added `startOrResumeInitialManualDetector`: validates/owns request, capacity,
controls and explicit selection before SQL, then holds the same original-parent
session lock as durable recovery across authenticated presence, first-start or
original-anchor restoration. Absence selects new start, valid presence selects
exact saved replay, conflict refuses both. Contention, cancellation, unknown lock
release or callback failure return no child receipt. Existing replay retains
saved qualification/resource parameters; first-start controls do not replace
original detector inputs.

Cancellation-before-lock regression added. This is an internal coordinator with
a private child receipt, not public SemanticPropose wiring. Role-scoped startup
installation checks, physical concurrent first/replay and full public parent
receipt behavior still require implementation/acceptance.


### Role-scoped native recovery startup reader checks (2026-10-07)

Scheduler installation now requires immutable manual selection save/read
functions and scheduler EXECUTE on save, alongside handoff-only completion.
The configured recovery factory separately verifies photo_api EXECUTE on original
saved stage/state/anchor readers, stage/selection presence, manual checkpoint
read, terminal child observation and qualification candidates. Missing functions
or grants fail startup before dispatch; the owned source API pool is closed on
verification failure. Errors avoid connection details. Null missing signatures
are treated as unavailable through bounded regprocedure/privilege inspection.

Targeted workflow/semanticproposal tests passed. This checks a required recovery
reader subset, not every SQL authority in the product. Physical ACL rejection
and source-pool cleanup acceptance remain unverified without isolated DB.
Public initial detector create API remains incomplete.


### Physical manual preflight before first detector start (2026-10-07)

The confirmed-absent initial manual branch now independently resolves every
selected original manual raster/vector closure against the original request
source before publishing intent, snapshot or detector Params. One owned aggregate
raster meter covers the preflight. A foreign/missing/changed mask fails before
detector admission or GPU work. The complete downstream checks remain: actual
face/object binding waits for completed native landmarks/parser/skin and is not
inferred from preflight. No preflight result is retained as admission authority.

Uses existing byte/version/digest/manual Params/vector/pixel/source readers.
Targeted Go tests run; full physical new-start acceptance remains unavailable.
Public create routing and actual GPU quality/cost acceptance remain incomplete.


### Production initial manual preparation entry point (2026-10-07)

`SQLNativeInitialPipelineRecovery.StartManual` now connects project-scoped
production dependencies to the locked start/resume coordinator. It validates and
owns the original request/explicit selection, resolves the authenticated source
catalog, scopes detector transport and bounds the entire attempt by the job
timeout. It returns only worked/error; no private detector ticket can escape as
a public semantic parent receipt. Configured source startup checks now include
the authenticated first-detector presence reader from 00819.

Cancellation-before-catalog regression added. This method accepts server-owned
original rasterized pixel refs, not SemanticEditIntent.ReferenceMask uploaded
user-mask documents. Public mask rasterization/selection and durable parent
receipt routing remain required; StartManual alone is not HTTP Propose.
Targeted Go tests run; physical first/replay and object/GPU acceptance are pending.


### Factory initial preparation resolves authenticated project (2026-10-07)

`SQLNativeParentRecoveryFactory.StartManualPreparation` resolves project from
ReadCatalog's authenticated original source, target snapshot and uploaded user
mask scope, constructs production initial dependencies and invokes bounded
StartManual. Request/selection/capacity are owned and validated before reads.
No caller-supplied project replaces inventory authority; no child receipt is
returned as parent acceptance. Cancellation-before-catalog is covered.

Inspection confirms intent ReferenceMask is distinct from original mask.rasterize
manual target pixels; conversion is not implemented or inferred by this method.
Public original manual producer creation/selection and HTTP parent orchestration
still remain. Targeted Go tests run; live catalog/factory/first-start acceptance
is unverified without physical fixture services.


### Public mask producer source binding (2026-10-07)

Inspection located existing `interactive.Service.CreateMask` as the public
manual/prompt producer creator. It now requires request input_sha256 to equal
the exact authorized asset-version master/source SHA before idempotency writes,
run creation or interactive task insertion. This applies to mask.rasterize and
mask.prompt and rejects stale source bindings with VALIDATION_FAILED/422.

This fixes an actual producer entry point; it does not convert an uploaded
SemanticEditIntent.ReferenceMask into rasterized manual target provenance.
Original producer output selection and complete semantic parent orchestration
still remain. Backend Go checks run; live database rejection/no-write acceptance
remains pending.


### Mask source binding preserves idempotency conflict/replay (2026-10-07)

Moved CreateMask's exact source SHA check inside the idempotency creation
callback, before run/task creation. Idempotency.Use must decide stored hash
conflict and same-request replay first: a changed payload under an existing key
returns IDEMPOTENCY_CONFLICT/409 instead of prematurely receiving SHA validation
422. A new wrong-source request returns VALIDATION_FAILED/422 and rolls back the
entire tenant transaction, including tentative idempotency insertion. Current
source/project authorization remains outside Use as in the existing service.

This supersedes the earlier statement that SHA checking precedes idempotency
writes: it precedes committed mutation, while temporary idempotency locking is
necessary to preserve the established replay contract. Changed interactive
package compilation and targeted V27 checks run; physical replay/conflict/no-write
acceptance remains pending. Full backend linking remains limited by disk space.


### Interactive manual vectors verified before Params plan (2026-10-07)

The existing scheduler mask_manual path now canonically assembles vectors and
uses review.VerifyManualVectorsDocument against the independently read original
WorkingImage before Store.Plan. This reuses strict kind/mode/sequence, radius/
hardness/unit-coordinate, per-shape/aggregate point count, source/canvas/oriented
geometry limits instead of relying only on eventual worker rejection. The local
deterministic validation ref is not published or retained as artifact authority.

This verifies the scheduler's existing producer entry point; it does not supply
public semantic manual selection/parent receipt orchestration. Targeted checks
run. Physical malformed-template scheduling rejection remains unverified; full
backend link testing is still constrained by disk space.


### Terminal invalid interactive manual preflight (2026-10-07)

Migration 00820 adds scheduler-only `fail_interactive_manual_preflight`. It locks
the original mask run and exact single unmaterialized manual job; requires live
matching lease, unsealed active noncancelled run and no materialized tasks; accepts
only fixed template/geometry error codes. It atomically marks job/run failed and
clears lease. Stale/foreign tokens or an already materialized graph refuse writes.

Interactive scheduler calls it only for invalid manual template/source binding
or strict vector geometry rejection. Storage read errors remain retryable.
Cancellation leaves the lease untouched; recording errors retain the validation
cause. Durable failure reports work=true and preserves the diagnostic error.
Migration is unapplied and PostgreSQL lease/rollback/state-transition acceptance
is unverified. Targeted Go checks run; startup presence check for this new
interactive failure function and physical malformed-template scenarios remain.


### Interactive manual failure startup gate and fencing regression (2026-10-07)

The scheduler's common expansion installation gate now requires the 00820 manual
preflight failure function and scheduler EXECUTE before starting interactive
processing. Added coordinator-level regression: stale recording retains both
validation cause and stale marker; successful recording retains diagnostic;
cancellation performs no terminal SQL write. This tests adapter behavior, not
physical SQL lease/ACL/run transition atomicity.

Targeted workflow tests run. Migration 00820 remains unapplied and physical
malformed-template failure/foreign token/rollback acceptance is pending. Full
backend link verification remains constrained by disk availability.


### Params-bound interactive terminal failure (2026-10-07)

Migration 00821 adds a strict terminal wrapper requiring exact claimed Params SHA
and jsonb payload under run→job row locks before delegating the unchanged live
lease/single unmaterialized mask failure gate. Concurrent payload or hash changes
refuse terminal writes. Scheduler access to the old token-only overload is revoked;
startup requires strict overload access and absence of old-overload access. The
Go failure adapter transmits owned claimed payload/hash; regression checks these
are included and retains cancellation/stale behavior.

Migration 00821 is unapplied. Targeted workflow tests run; physical concurrent
payload mutation, ACL bypass and atomic status transitions remain unverified.
Full V20–V32 and public semantic creation are still incomplete.


## Automatic original person → CPU defect preparation (current implementation)

`ensureInitialPersonDefect` runs under the original parent advisory lock after
face producer verification and `readOriginalNativePersonInput`. The absent child
uses server-selected CPU qualification and resource limits, original registered
face transports, and the verified person probability mask as permanent protection.
The initial diameter range is the full supported `.002–.012` range; an existing
child always restores its saved range, Params, qualification and resource costs.
An existing child lacking the same person authority is refused rather than replaced.
Blocked transport publication/registration may resume only from the restored plan.
Sealed children replay their original receipt. Terminal children refuse recreation.

The initial recovery processor keeps the checkpoint waiting for `defects.propose`
after child admission. Neither the receipt nor face/person readiness completes the
parent. Completed defect verification, protection publication, atomic durable
parent handoff, ordinary terminal recording and positive automatic fresh-parent
end-to-end evidence remain required before this orchestration can be accepted.


### Completed automatic defect advancement

`advanceInitialPersonDefect` calls the original-child coordinator, reads its saved
state, and returns pending observations without any protection evidence. Succeeded
state triggers recovery of original registered transport followed by physical
canonical result/probability checks, immutable protection publication, original
registration and a repeated person input check. It returns private internal
protection readiness. Initial recovery invokes this function but retains its
waiting checkpoint until a durable parent handoff is implemented. No child receipt
or internal succeeded observation is public parent acceptance.


### Initial person-protected parent admission and remaining target authority

`admitInitialPersonDefectParent` consumes only succeeded internal protection
readiness and revalidates its registered canonical origin. It selects qualified
transfer runtime inputs, binds the original person qualification, reads the same
immutable source objects under transfer WorkingImage validation, and verifies
person coverage. Existing parents restore their saved target protection pins and
transport; absent parents require independent target protection selection before
any parent publication/admission. Initial scheduling remains active pending a
separate durable completion transition.

Detector source validation uses unit RGB; transfer validates WorkingImage values.
`nativeTransferSourceMatchesDetector` compares all scope, binding, ICC and immutable
object pins exactly while permitting these independently verified validation modes.
The original detector plan and pixels are never modified. Original transfer recovery
also physically reads its transfer source proof before reconstructing person authority.

Physical parent admission currently refuses the integration fixture's ambiguous
target protection inventory after successfully traversing reference person authority.
No arbitrary candidate fallback is permitted. Positive whole-parent acceptance still
requires durable per-target authority/preparation, positive restart/admission evidence
and exact checkpoint completion. This implementation subsection does not close V27.


### Pinned target policy and native protection row-lock prerequisite

For an absent parent, `admitInitialPersonDefectParent` preserves nonempty
server-selected `TargetProtection` pins rather than rediscovering candidates.
These pins remain locators: the full preparer physically verifies target source,
protection/person coverage, immutable input closure and qualification, followed by
complete SQL admission locks. Empty policy still uses unique-candidate discovery;
ambiguous candidates remain a conflict. Existing parents retain saved target pins.

Migration 00910 supplies PostgreSQL's row-lock prerequisite for
`lock_semantic_native_input_protection_evidence`: only `photo_fn_owner` receives
`UPDATE(document_id)` on `semantic_native_protections`. No application role gains
UPDATE. A BEFORE UPDATE trigger rejects every actual update with SQLSTATE 42501,
including no-op updates by the function owner. INSERT registration and the existing
allowlisted tenant purge DELETE are unchanged. The migration is forward-only and
contains no data rewrite. Full initial checkpoint completion remains a separate
pending transition; a child/protection receipt never retires parent recovery.

### Current native parent admission evidence (2026-10-08)

`audit/v27-parent-admission-current.json` records a real PostgreSQL/MinIO isolated
fixture PASS (1 test, 404.39 seconds) for fresh person-protected defect output,
registered native protection, explicit trusted target policy, parent admission
and original ticket replay. Independent verification checks graph/tasks, physical
manifest/chunk bytes and registered closure, quota and exactly-one queued
receipt/event/outbox, owner row locking and immutable UPDATE refusal. Competing
target roots remain live and ambiguous selection is refused before parent creation.

The earlier `seal_v2_graph=false` was an isolated fixture configuration error:
migration00525 intentionally leaves `semantic.transfer` disabled. This fixture
now explicitly enables it after seeding imported test qualification. Production
registry defaults and real qualification requirements are unchanged. Failed XML
logs are retained. The fixture-only timeout is ten minutes; no production SLA or
quality acceptance is inferred. Durable automatic initial completion, actual
newly inferred original-person adoption through the entire recovery chain, and
whole V27/manual/transfer/review/Apply acceptance remain open.

### Durable automatic handoff implementation (2026-10-08)

Forward-only00911 strengthens record_v27_native_initial_pipeline_handoff for
automatic completion: the private helper requires the exact sealed parent and
202receipt, original person task/result/mask, canonical defect/result registered
protection and parent input intent/protection pins. It locks producer results and
artifact rows. The existing manual branch remains; no new application EXECUTE
or DML is granted. Recording locks follow tenant/usage/parent before detector.
Recover retains the verified original face-producer success only after the exact
parent queued ticket is returned; a queued defect still returns waiting.

Current audit/v27-automatic-handoff-current.json:8PASS covers forward upgrade,
metadata/ACL preservation, missing-parent/producer and lease/scope refusal, waiting
lease release and migration markers; Go semanticproposal/v2input/review PASS.
Positive newly inferred original-person automatic Recover/handoff, later positive
SQL lock branches, concurrency/crash and whole module acceptance remain required.
These results do not turn the earlier parent-only PASS into full initial recovery
acceptance. The new recorder fails closed when the original producers are absent.

### Original newly produced person and full saved Recover (2026-10-08)

Current audit/v27-original-person-recovery-current.json records1PASS548.05s
for actual person adapter/mTLS/uploads/canonical commit on the original saved
mask.person task, exact original-result physical authority, fresh person-bearing
defect output and registered protection, explicit trusted target pins, parent
admission/replay, production Recover on the saved succeeded chain and admitted
parent, and actual scheduler lease completion recording. Independent outer
verification binds original root/pixels to the canonical task/attempt/fence,
checks done=true with lease/schedule cleared, physical input transport and one
receipt/event/outbox. No source person mask is imported in this scenario.

Inference is an explicitly deterministic spatial model double with nonempty
person coverage and disjoint selection; imported qualification remains a test
fixture. The earlier soft.37 fixture remains separate and unchanged by default.
Regressions8PASS and Go semanticproposal/v2input/review PASS. The stopped earlier
full-Recover attempt had an inherited420s nested Go budget; the successful bounded
fixture uses900s context/960s Go/990s outer, with no production SLA change.

This supersedes the earlier missing-positive original-person/late-SQL gate for
this saved-chain scenario only. Full ProcessOne, cold/crash recovery at all
publication/adoption boundaries, concurrent substitution/revocation/purge, real
GPU/corpus quality, target producer creation and whole module acceptance remain
open. No V27 accepted status is asserted.

### ProcessOne with expired prior lease (2026-10-08)

Current audit/v27-initial-process-one-current.json:1PASS677.10s. The original
new person/face/defect producer and parent chain is prepared on real native
PostgreSQL/MinIO/mTLS fixtures. An actual scheduler lease is acquired, its time
is expired through owner-only fixture metadata, and the expired token refuses
completion. Production ProcessOne obtains the replacement lease, invokes full
Recover and durably records handoff; another ProcessOne finds no work, and the
old token still refuses. Independent outer verification confirms original
canonical person dependencies, done/lease fields, parent physical closure, quota
and one queued receipt/event/outbox. Go semanticproposal/v2input/review PASS.

No process was killed/restarted in this scenario. It proves lease replacement on
a saved succeeded chain with an admitted parent, not every cold interruption
point or concurrent revocation/substitution/purge. Inference and qualification
remain explicit deterministic test fixtures. Target producer creation, real
model/corpus qualification, capacity/SLA and whole V27 acceptance remain open.

### Scheduler claim concurrency and abrupt process exit (2026-10-08)

Current audit/v27-claim-concurrency-current.json:13PASS9.66s on isolated native
PostgreSQL. Two actual scheduler connections prove SKIP LOCKED while the first
claim transaction is uncommitted, no reclaim of a committed live lease, distinct
replacement after elapsed-time simulation, and stale/consumed token refusal.
Separate Python interpreters execute actual scheduler claim then os._exit(23):
the lease survives process disappearance and only expiry permits a fresh process
to replace it. Disabled user, revoked session and archived project prevent claim
without changing lease metadata; restoring authorization makes the same candidate
eligible. Upgrade/ACL/metadata preservation and handoff refusal regression pass.
Go semanticproposal/v2input/review PASS; stale recovery type comment corrected.

These are metadata fixtures, not a whole model/transport Recover restart. The
process exits after lease commit, not during Go ProcessOne, PUT, adoption or
completion commit. Whole-chain parallel contention/revocation/purge, every cold
crash boundary, target producer creation and real model/capacity acceptance remain
required. No module accepted status is asserted.

### Target prerequisite planning foundation (2026-10-08)

Current audit/v27-target-manifest-current.json records the new canonical private
target prerequisite manifest and missing-target continuation in automatic parent
admission. Original request, intent, session/project, snapshot, full target order
and binding are retained. Explicit version1 person/face/skin/defect stage DAGs
have isolated deterministic identities; matching reference is a reuse hint only.
No Params/task/output authority is fabricated. Errors/cancellation return empty
plans; ambiguous evidence does not enter regeneration.

Final semanticproposal package PASS with saved JSON log; both manifest tests PASS,
including full5000-target coverage, no identity aliases and8MiB bound. Three Go
packages passed before final explicit operation_version field. Current diff check
PASS. No new migration was added. Durable persistence and target producer
execution/qualification/adoption plus physical missing-target integration remain
open; this foundation is not whole target orchestration or V27 acceptance.


### Target manifest restore / текущий срез 2026-10-08

Добавлен private restore codec с byte checksum, bounded metadata, полной перекомпиляцией исходного плана и независимым владением возвращённым buffer. Тесты отвергают переподписанную подмену tenant/project/actor/session/parent/request/intent/snapshot, namespace, DAG dependency, operation version, reuse и truncation; отдельно noncanonical/oversized bytes, wrong SHA, cancellation. Evidence: `audit/v27-target-restore-current.json` и JSONL. SQL persistence и исполнение target prerequisites ещё не реализованы. Инвентарь пересобран из текущих файлов: 13 пакетов, 28 экранов, 72 файла компонентов, 635 статических кандидатов состояний, 317 объявлений API, 89 contract entrypoints и 29 операций. Эти количества не подтверждают runtime acceptance.


### V27 durable target manifest / 00912 — текущая реализация

Предыдущие записи об отсутствии persistence относятся к историческим срезам. Теперь `00912_v27_target_preparation_checkpoint.sql` хранит immutable manifest/SHA на исходном detector run; SQL независимо восстанавливает exact namespaces и DAG из original snapshot, требует current scope/ready documents/recipe heads и original sealed config. Canonical bytes сравниваются полностью. Scheduler-only definer возвращает original stored bytes; Go повторяет restore verification перед commit. Нельзя заменить checkpoint даже owner; app DML/registry activation не расширяются, новых purge-таблиц нет. `Recover` сохраняет missing-target manifest и возвращает waiting semantic.transfer с пустым producer outcome; SQL00911 handoff gate остаётся обязательным.

Быстрый текущий набор: 20 PASS за 20.53s (target checkpoint SQL, forward upgrade00899→00912, native anchor, automatic handoff/concurrency/process-claim, goose markers). Проверены reference reuse и non-reference DAG, exact replay, substituted scope/config/DAG/noncanonical bytes, current revocation/cancel/expiry/recipe drift, committed reconnect и concurrent replay locks. Это metadata/role proof; не model/GPU qualification и не автоматическое исполнение targets. Production Go helper дополнительно включён в physical original person/face/defect parent fixture: отдельный wire evidence фиксирует результат текущего прогона.

Следующий обязательный участок: target-bound anchor/compiler/admission для mask.person, face.detect, face.landmarks, face.parse, mask.face_skin и defects.propose. Existing source compilers требуют `prepared.source.binding == owned.Intent.Reference`; подмена оригинального intent ради reuse не допускается. Target compiler должен сохранить original request SHA/parent/snapshot и отдельно доказать binding/ordinal из persisted manifest, затем пройти qualification/quota/physical/canonical checks. Until that execution exists, waiting recovery is intentional and V27 remains implementing.


Физический текущий прогон завершён: `v27-target-checkpoint-wire-current.xml` — 1 PASS, 21 deselected, 309.34s. На original native detector production Go checkpoint сохраняет и восстанавливает exact original manifest дважды, затем прежний explicit-server-target parent admission/replay и внешний byte/SQL probe проходят. Real adapters/mTLS/S3/canonical publication использованы с deterministic ML doubles; это не ML quality и не automatic target execution. Missing-target ветка полного `Recover` ещё не имеет отдельного физического прогона; её waiting-классификация покрыта current Go unit. Быстрые SQL20 и Go3 packages PASS. Сводный evidence: `audit/v27-target-checkpoint-current.json`. Миграция применялась только в изолированных test databases; production DB не менялась.


### V27 target-bound mask.person preparation / 00913

Добавлены owned `nativeTargetAnchor`, target-specific person compiler/factory и transactional blocked begin. Original EditIntent.Reference/RequestSHA/parent/snapshot не меняются; target version/namespace/ordinal/stage/manifest SHA закреплены отдельно. Lower source pipeline повторно читает target pixels/alpha/geometry/ICC и trusted qualification report, публикует version-scoped Params, выводит реальные run/workflow/task IDs из target namespace и требует explicit model/runtime/device SHA. Legacy reference compiler/qualification path не ослаблен. Нельзя использовать reference-reuse entry как новый target producer или ordinary_parent continuation в target plan.

00913 даёт scheduler-only lock target entry из immutable checkpoint: original scope/current authorization, snapshot inventory/recipe heads, exact saved SHA, ordinal bounds и independently reconstructed namespace/DAG. `beginNativeTargetPersonInTransaction` требует original detector seal, entry lock, target source/person/Params/policy locks и exact compiled shell; created/pending создают один unsealed draft/blocked task с reservation. Caller владеет original parent advisory lock и rollback. Automatic Recover пока не вызывает этот target factory/begin: orchestration, transport/register/seal, canonical adoption и restart recovery всё ещё обязательны.

Быстрый SQL-набор: 21 PASS за 26.75s, fresh schema +00899→00913 upgrade (14 forward migrations), current entry authority/revocation/ordinal/reference-reuse denial и прежний handoff regression. Compiler fixtures охватывают RGB/alpha/geometry catalog cases, target/reference lineage separation, deterministic replay, legacy profile refusal, parent/document/binding/report/policy/session/continuation substitutions, underreservation и fingerprints. Три Go-пакета прошли. Physical target-only fixture использует другой accepted version и собственный frozen snapshot/EditingRecipeV3 head; байты WorkingImage переиспользованы между версиями, поэтому это binding/isolation evidence, не корпус разных фотографий. Первая попытка выявила reference-only старую fixture, она не считается PASS. Новый physical probe самостоятельно проверяет stored manifest, Params SHA/object version/source, model pins, one reservation/pending record и draft/blocked/no canonical state. Итог текущего повторного прогона — в `audit/v27-target-person-plan-current.json` и XML/JSONL.


Финальный physical target-only повтор на текущих исходниках: 1 PASS, 21 deselected, 46.53s (`v27-target-person-wire-current.xml`). Новый target version имеет собственный snapshot/recipe, original reference не изменён. Production factory и transactional begin создали target draft, повторный begin вернул pending без второй reservation; независимый Python probe проверил target namespace/manifest SHA/original request lineage, физический Params object version/SHA/image, model/runtime/device pins, status draft/task blocked/no canonical. Parent semantic graph не создавался. ML target inference, transport registration и target seal не выполнялись. Текущий Go3 PASS и SQL21 PASS; миграции выполнялись только в isolated test DB.


### V27 target person transport / 00914–00915

Target-specific publication and transactional registration now exist as separate production helpers. Registration repeats current authority after object publication and permits exact replay only.00914 owns strict graph/manifest/transport metadata gates;00915 adds target registered-shell pending replay while retaining existing matcher identity and all other branches. A run remains draft/blocked and cannot execute from these helpers.

The first physical attempt exposed missing target support in pending-shell replay after transport registration. The SQL test also exposed immutable-config and required project-scope fixture assumptions; those fixtures were corrected without weakening production guards. Current test outcomes and source hashes are recorded separately in `audit/v27-target-transport-current.json` once verification finishes. Previous no-transport statements describe historical slices. Target seal/execution/canonical adoption, remaining target producers, automatic Recover wiring and the complete module cycle are still open.


Финальный текущий transport-срез: SQL17 PASS (50.60s), physical target-only1 PASS (81.17s), Go semanticproposal/v2input/review3 PASS, diff check PASS. Независимый Python probe проверяет реальные версии и SHA объектов chunks/manifest, точный inventory/profile, backend-document ownership и отсутствие queued events. Evidence: `audit/v27-target-transport-current.json`. Статический инвентарь пересобран:13 модулей,28 экранов,72 component files,635 state candidates,317 API declarations,89 planned entrypoints,29 операций; это не runtime acceptance.


### V27 target admission authority / 00916 — текущий срез

Добавлен отдельный original/target evidence compiler без изменения original intent/reference. SQL00916 independently проверяет original manifest entry, exact target config/policy/source/qualification/Params и полную registration envelope, затем singleton graph/live quota budget. Helper не seals/queues. Physical current run:1 PASS57.23s; exact repeat true, substituted binding/request/anchor/Params document/policy/registration/qualification false, target всё ещё draft/blocked. SQL18 PASS18.89s с fresh schema,00899→00916 upgrade/ACL/metadata preservation и malformed envelope refusals. Go3 packages PASS. Evidence: `audit/v27-target-admission-current.json`. ML doubles/shared WorkingImage limitations сохранены; target atomic seal/sealed replay, execution/canonical adoption, other target stages и automatic Recover всё ещё открыты.


### V27 target dedicated atomic seal / 00917 — current evidence

Dedicated target seal and independent sealed replay now exist. Positive physical current run1 PASS80.33s covers revoked-before-seal refusal, first seal, exact replay, revoked-after-seal refusal and restored replay. External SQL/S3 probe verifies target queued/sealed/task ready/no canonical, exact202 ticket, one reservation and exactly one event/outbox while validating original lineage and physical Params/transport SHA/object versions. SQL19 PASS28.03s covers fresh schema, populated00899→00917 preservation/ACL and malformed seal refusal without state/event changes. Go3 PASS, diff check PASS. Evidence: `audit/v27-target-seal-current.json`. Previous draft-only statements describe earlier slices. Target worker execution/atomic run start/canonical adoption and automatic recovery remain required; module V27 is implementing.


### V27 target person physical execution / 00918–00919

00918 starts the exact target run on successful qualified/fenced claim;00919 permits only its four ordered verified task-owned outputs at canonical commit, retaining all older guards. Current physical1 PASS43.01s executes target through real reservation/claim, mTLS downloads, person adapter, immutable uploads, canonical commit and commit replay; strict output predicate rejects wrong attempt/fence/reorder/truncation/duplication before commit. Target task succeeded with canonical result; run remains running for future orchestration finalization. Sealed replay after commit passes and external probe confirms one receipt/event/outbox. SQL19 PASS19.98s includes fresh schema and00899→00919 metadata/OID/ACL preservation. Go4 packages PASS including gateway after local-listener sandbox retry. Evidence: `audit/v27-target-execution-current.json`. Initial generic-root failure fixed by dedicated guard; probe Lease attribute error corrected. Deterministic ML/shared-image fixture does not qualify real GPU/photo quality. Target result adoption, other stages, Recover orchestration and whole V27 acceptance remain open.


### V27 exact target person result authority / 00920

Production reader discovers the exact canonical target task result and physically revalidates qualified Params/report/MaskRef/pixels twice, retaining owned fingerprints without source-wide candidate substitution or original intent mutation. Scheduler consumer lock repeats current original/target sealed authority and compares complete canonical outputs/result ID. Final physical1 PASS84.11s: repeated reader/locks accepted; foreign result and report (even with recomputed private fingerprint) refused; revoked session refuses both reader and lock; restored session accepted. SQL19 PASS40.37s includes fresh schema and00899→00920 data/function identity/ACL preservation. Go4 packages PASS, diff check PASS. Evidence: `audit/v27-target-result-current.json`. This is standalone consumer authority, not automatic adoption; producer finalization and locked adoption after finalized state remain open, along with other target stages/Recover and whole V27 acceptance. Existing ML-double/shared-image limitations remain.


### V27 target person producer finalization / 00921

Target lifecycle now uses actual terminal run status completed. Dedicated finalizer repeats exact canonical result/current scope, releases only residual reservation and commits one completion event/outbox atomically. New private output predicate permits locked canonical consumption after completion; worker commit guard remains running-only. Final physical1 PASS62.13s covers expired reservation refusal, exact tenant reserved usage delta after finish+replay, released/bytes0 quota, completed producer physical reader/consumer lock/seal replay, revoked finish replay refusal and exactly one completion event/outbox. SQL19 PASS32.29s includes fresh schema and00899→00921 tenant data, existing and newly replaced target function OID/owner/ACL preservation. Go4 packages PASS; diff check PASS. Evidence: `audit/v27-target-finish-current.json`. Previous running-only limitations are resolved for target person. Automatic Recover wiring, other target stages, cold restart and full V27 acceptance remain open; ML-double/shared-image limitations unchanged.


### V27 target person automatic advance and saved restore / 00922

The missing-target branch of Recover now checkpoints the immutable manifest and invokes target-person advancement under the original parent advisory lock. Saved-state discovery, physical Params/source/policy recompilation and exact registered-transport restoration permit queued and completed retries without replacement Params or a new reservation. Invalid/failed/partial state refuses repair. Original intent/reference remain unchanged.

Final current physical helper test:1 PASS239.14s; SQL19 PASS142.00s includes fresh schema and00899→00922 upgrade with preserved data/function identity/owner/ACL; Go4 packages PASS. Documentation checks2267 and semantic contract tests55 PASS; static inventory refreshed13 packages/28 screens/72 component files/635 state candidates/317 API declarations/89 entrypoints/29 operations. Evidence: `audit/v27-target-recovery-current.json`. The physical test covers absent creation/publication/registration/seal, waiting restore, actual fenced mTLS worker canonical commit, finish and completed restore/replay. Recover is wired and compiled, but a full fresh-process Recover/ProcessOne crash/restart is not proved. Five other target stages, failed-target/dead-letter policy and whole V27 acceptance remain open. ML doubles/shared-image limitations unchanged; production DB unchanged.


### V27 target detector compiler, physical preparation and blocked begin

Separate target face.detect compiler now retains original intent/reference and exact target manifest namespace. It requires target source/ICC/input pins, target Params ownership, current qualified CUDA/model/runtime/device identity and output+8MiB reservation. Dedicated factory physically verifies target source and Params, rechecks qualification policy after IO; transport publisher validates the frozen plan before and after publication. Blocked begin retains original target-entry locks plus exact target source/Params/report/policy locks and supports created/pending retry only.

Final physical helper test1 PASS235.81s covers target Params/qualification/source preparation, blocked begin+repeat, immutable transport publication and revoked-session replay refusal. Independent Python SQL/S3 verification checks original manifest/lineage, deterministic target run/task/stage IDs, physical Params SHA/version/image binding, explicit execution identity, one reservation, no admission receipt, no registered inputs and no canonical result. Go4 packages PASS; dedicated compiler negatives include wrong actor/stage/source/document/report/policy/session, legacy identity-free profile, insufficient reservation, mutable plan, cancelled publisher/compiler and missing original transaction authority. Evidence: `audit/v27-target-detector-plan-current.json`.

Target detector remains draft/blocked; dedicated SQL registration/seal/output guards, actual execution/result lifecycle, restoration and automatic target scheduling are open. Whole V27 remains implementing. No photo-quality or complete crash/restart proof is claimed; original ML-double/shared-image limitations persist.


### V27 target detector registration / 00923

Dedicated private graph scope and scheduler registration state now independently bind original detector, immutable target manifest/namespace/stage1, target accepted version, deterministic Params/run/task and live editor/session. Registration requires complete exact rows/direct/profile/SHA/chunks/manifest/backend-document closure. Missing allows one atomic registration; exact replay is accepted; partial state refuses repair. In-place pending matcher replacement preserves existing OID/owner/ACL and all older branches.

Current physical1 PASS58.20s covers first registration, exact repeat and revoked-session refusal without partial inputs; independent SQL/S3 probe verifies target lineage/Params, transport object versions/bytes/SHA/backend-document ownership and ready closure. SQL19 PASS31.13s includes fresh schema,00899→00923 metadata/function identity/ACL preservation, denied roles and malformed/partial/substituted scope/registration cases. Go4 packages PASS; diff check PASS. Evidence: `audit/v27-target-detector-registration-current.json`. Target remains draft/blocked with no admission receipt or result. Dedicated target admission/seal, actual execution/result lifecycle, recovery and automatic advance remain open; whole V27 implementing. Existing ML-double/shared-image and full-module evidence limitations remain.


### V27 target detector admission and atomic seal / 00924–00925

Separate original/target evidence compiler and dedicated SQL admission/seal now exist. Admission independently repeats original manifest entry, exact target config/policy/source/qualification/Params, complete registered transport and live graph/quota budget. Atomic seal retains this authority through task activation, queued revision,202 receipt and event/outbox. Exact sealed replay rechecks current authority; saved receipt alone is insufficient. Original intent/reference and source detector compiler stay distinct.

Final physical1 PASS53.28s covers positive admission repeat, substituted binding/request/anchor/Params document/policy/registration/qualification refusals, revoked-before-seal refusal, first seal, exact repeat, revoked-after-seal refusal and restored replay. Independent SQL/S3 verification requires queued/sealed run, ready task without canonical result, one reservation, exact202 receipt and exactly one queued event/outbox while checking physical Params/transport SHA/object versions/backend ownership. SQL21 PASS24.00s includes fresh schema,00899→00925 metadata/function identity/ACL preservation and malformed/denied-role cases. Go4 packages PASS; diff check PASS. Evidence: `audit/v27-target-detector-seal-current.json`.

Previous draft-only limitations are resolved for target detector admission. Worker claim/start, canonical commit/output guard/result lifecycle, completed finalization, saved restoration and automatic target advancement remain open; whole V27 implementing. Existing ML-double/shared-image and full-module evidence limitations persist.


### V27 target detector physical execution / 00926–00927

Qualified fenced claim now starts the exact target detector run atomically; dedicated canonical-output predicate accepts one verified task/attempt/fence-owned face_boxes JSON and retains previous source/person/Apply guards. Physical current1 PASS52.40s executes target through resource reservation/claim, indexed mTLS input downloads, production face.detect adapter with deterministic model, immutable verified output upload, canonical commit and exact commit replay. Strict readiness checks reject wrong attempt/fence/SHA, empty/duplicate outputs and substitution with input artifact before canonical commit. Worker probe validates nonempty FaceBoxes body and exact source/model identity; task slots/reservations released; dedicated sealed replay still passes after success. Independent final SQL/S3 probe verifies running/sealed target with succeeded canonical task, one admission receipt/event/outbox and exact target lineage/Params/transport.

SQL21 PASS24.48s includes fresh schema and00899→00927 data/function OID/owner/ACL preservation; Go4 packages PASS; diff check PASS. Evidence: `audit/v27-target-detector-execution-current.json`. Target run is still running; physical canonical result discovery/consumer locks, producer completed finalization, saved recovery/automatic advance and remaining target stages remain open. Whole V27 implementing; existing real-GPU/quality/shared-image/full-module limitations persist.


### V27 exact target detector result authority / 00928

Dedicated API discovery binds exact target task/canonical result/config and original manifest; scheduler consumer lock repeats sealed original/target authority and exact full output/result/attempt/fence readiness. Physical reader verifies target source/ICC/pixels proof, pinned FaceBoxes and detector Params consistency/geometry, CUDA/model/qualification and repeated catalog/object/source/policy after IO. Private proof fingerprint owns result refs, actual control bytes, parsed boxes and explicit object pins. Original intent/reference remain unchanged; no source-wide latest-candidate lookup.

Final physical1 PASS103.20s covers repeated physical reader and consumer lock, foreign result/output refusal even with recomputed fingerprint, mutated body/object proof refusal, revoked reader/lock refusal and restored authorized lock. SQL21 PASS59.79s covers fresh schema and00899→00928 data/function OID/owner/ACL preservation; Go4 packages PASS; diff check PASS. Evidence: `audit/v27-target-detector-result-current.json`. Target producer remains running; completed finalization/locked adoption, saved recovery/automatic advance and other target stages remain open. Whole V27 implementing; real-GPU/quality/shared-image/full-module limitations unchanged.


### V27 target detector producer finalization / 00929

Dedicated finalizer now repeats exact canonical consumer authority, releases residual run reservation and commits actual terminal completed status with one completion event/outbox. New private completed-output predicate permits current-authority reader/consumer locks after completion; worker commit remains running-only. In-place target sealed graph/registration/evidence and canonical discovery/result lock replacements preserve identity and ACL.

Final physical1 PASS95.12s covers expired reservation first-finish refusal, exact tenant reserved usage delta after finish+retry, completed physical reader/consumer lock/seal replay, revoked finish replay refusal and restored exact retry. Independent SQL/S3 probe requires completed producer, released/bytes0 quota, one completion event/outbox and existing exact admission receipt/queued event while checking actual Params/transport pins. SQL21 PASS31.56s includes fresh schema and00899→00929 metadata/function OID/owner/ACL preservation, including per-step old target function identity checks at00929. Go4 packages PASS; diff check PASS. Evidence: `audit/v27-target-detector-finish-current.json`.

Target detector standalone lifecycle is completed-capable. Automatic saved recovery/advance, four remaining target producer stages, original parent orchestration and whole V27 acceptance remain open; ML-double/shared-image/full-module limitations unchanged. Production DB unchanged.


### V27 target detector saved restoration / 00930

API saved reader and scheduler exact-transport reader now independently bind original manifest and deterministic target identities. Physical restore decodes original recovery costs/limits/report/request key and target Params score/max_faces, recompiles qualified target source task without publishing Params and requires exact config/profile/digest/Params/budget; source/Params/policy and saved base profile are rechecked after IO. Transport restore retains original entry authority and rebinds exact stored envelope to frozen task without repair.

Final physical1 PASS79.89s covers absent locator, queued plan/transport restore and dedicated sealed replay, completed physical plan/transport restore and current-authority replay while retaining existing result/finalization/revocation checks. SQL21 PASS37.33s includes fresh schema and00899→00930 metadata/function OID/owner/ACL preservation; Go4 packages PASS; diff check PASS. Evidence: `audit/v27-target-detector-recovery-current.json`. Automatic target detector Recover/advance wiring, four other target stages and full process crash/restart remain open. Whole V27 implementing; real-GPU/photo-quality/shared-image/full-module limitations persist.


### V27 automatic target detector advancement

Recover missing-target branch now retains original detector proof, advances all target persons and then invokes dedicated detector advancement only when person prerequisites finish. Absent target uses original verified detector report/costs/limits/reservation and physically pinned score/max_faces from original Params; target source/current qualification remain independently checked. Existing target uses exact saved plan/transport and current sealed authority. Queued state waits, canonical success physically verifies/finalizes, completed repeat creates no replacement Params/reservation. Failed/invalid/partial state refuses repair. Four later target stages keep original handoff waiting semantic.transfer.

Final physical1 PASS45.95s covers automatic absent begin/publication/registration/seal, waiting restore, actual mTLS detector worker canonical commit, finish and completed restore/retry. Independent SQL/S3 verification confirms completed producer, released quota, exact original/target lineage, physical Params/transport and one admission/completion event/outbox. Initial Go fixture passed but Python expected manual branch marker; corrected verifier requires automatic marker while retaining independent checks. SQL21 PASS58.60s includes current guards and00899→00930 upgrade; Go4 packages PASS; diff check PASS. Evidence: `audit/v27-target-detector-advance-current.json`.

Recover wiring is compiled, but full fresh-process Recover/ProcessOne crash/restart is not proved by the helper fixture. Four other target stages, failure/dead-letter handling, original parent orchestration and whole V27 acceptance remain open. Existing real-GPU/quality/shared-image/full-module limitations persist.
