# Semantic transfer: frozen scene evidence and ordinary installation

Status: implementation in progress; no execution or physical qualification asserted. This addendum preserves execution-contract §136: planar background only, no face/object fallback, positive protected pixels excluded, proposal requires photographer review.

## Immutable authority

A fresh transfer freezes `execution_profile.semantic_scene` as an embedded immutable scene-evidence document bound to the exact reference/target VersionBinding, original EditIntent/ref, calibration/ref and actual runtime/model/device/dataset pins. It is not an artifact reference: its canonical bytes/hash are bound by the immutable task/config SHA. Existing accepted Params bytes and idempotency keys are never rewritten. Every child artifact remains explicitly typed in the input manifest. Worker and independent adopter compare the same canonical embedded document, exact source pair and projection. Its typed children are the two source WorkingImages, source mask, independently produced SceneLabels and RelativeDepth for each source, plus reproducible correspondence evidence. Scene/depth outputs require canonical adopted producer task/result/attempt/fence and exact source refs. A document or a client-supplied `planar=true` flag is never authority. All child arrays must be declared typed inputs and physically SHA/version verified; scheduler adoption rechecks live authority after IO.

The scene verifier uses existing pinned scene/depth providers and a deterministic classical correspondence algorithm. It never invents dense-flow model weights. CPU OpenCV computation is compatible with either CUDA or Metal model installations, but complete Metal transfer remains unavailable until its own measured runtime/report/SQL pins and registration exist. Existing MPS scene.parse/depth providers alone do not establish full transfer support.

## Function decomposition

1. `SemanticSceneEvidence`: strict versioned contract, exact source pair, input intent/calibration refs, scene/depth refs, runtime pins and algorithm ID. No mutable thresholds in user input.
2. Producer admission resolves canonical SceneLabels/RelativeDepth occurrences for each exact source and freezes their refs. Missing evidence schedules actual existing scene/depth producers; it cannot fabricate empty masks or depth.
3. `verify_planar_scene_arrays`: bounded physical arrays; source support excludes every positive protected value. On a bounded proxy, classical Farneback forward/backward flow must be finite, mutually consistent and consistent with the adopted planar homography. Positive source support must map in bounds to background labels and the target mask domain. Relative-depth least-squares planes must satisfy the policy residual threshold on both domains. Nonbackground labels, insufficient support, missing depth, occlusion, parallax or ambiguity produce unsupported, never a successful empty proposal.
4. Worker preparation loads hash-verified scene/depth evidence under resource admission and runs the verifier before staging. Exact qualification report supplies algorithm/policy thresholds; thresholds cannot come from UI.
5. Independent adopter rereads the same evidence, reproduces correspondence and scene checks, then retains existing exact mask warp reconstruction, qualification, task/fence and live-source checks.
6. Ordinary CLI constructor installs this concrete verifier and existing SQL artifact factory for worker and adopter; it verifies immutable descriptor/runtime files and actual backend pins. No callback module paths or restore-role authority are accepted.

## Required authored scenarios

Textured planar positive; inconsistent forward/backward flow; moving/occluded patch; nonplanar depth; person/foreground label in positive support; fractional protected mask; foreign source or stale producer; altered array SHA; missing scene/depth; cancellation during bounded work; exact worker/adopter agreement; ordinary CLI mounting; explicit CUDA and separately qualified Metal availability. Tests are written first/alongside implementation and executed only in the later testing phase.

## Remaining implementation

The existing accepted transfer Params remains unchanged; fresh immutable execution profiles must carry the embedded scene evidence and its typed children. Canonical prerequisite scheduling and trusted preadmission scene-policy resolution remain required before complete transfer can be credited. Constructor availability does not enable capability without actual installed qualified policy/report and producer evidence.

## Current implementation checkpoint (source only)

`algorithms/semantic_scene.py` implements the bounded classical proxy gate, using actual OpenCV Farneback (single-thread section under the shared ORB lock), deterministic sampled plane fitting and every-support depth residual. Both actual homography and forward-flow footprints are checked, including finite homogeneous coordinates, target bounds, contributor labels/protection and the depth domain. Existing `scene.parse` uint16 output is accepted without narrowing; class IDs outside ADE150 are refused before conversion. `contracts/semantic_scene.py` and `backend/internal/v2input/semantic_scene_contract.go` mirror the strict frozen evidence/policy representation. Native positive/unsafe/cancellation and cross-language shape scenarios are authored in `test_semantic_scene_evidence.py`, `test_semantic_scene_contract.py` and `semantic_scene_contract_test.go`; none executed.

`semantic_scene_projection.py` now derives exact pixel-center source→proxy transforms, conservatively projects every original positive selection/protection/foreground contributor, conjugates the actual downstream homography, and repeats full-resolution depth-plane checks. Target depth checks every original sample inside every bin touched by either actual homography or forward-flow bilinear contributors; it does not require unrelated target foreground outside this conservative footprint to be planar.

`scene_worker.py` and `v2/semantic_scene_adoption.py` independently load exact typed Params/root/array documents, run the same physical scene verifier, enforce bounded RAM/scratch, and repeat live SQL/report/runtime authority after IO. Normal CLI constructors are mounted through `runtime/semantic_scene_config.py` and `v2/semantic_scene_config.py`. `PHOTO_SEMANTIC_TRANSFER_ENABLED=true` requires the four measured hashes and an absolute bounded runtime-lock file; adopter additionally requires explicit RAM and existing scratch/ICC configuration. Runtime verification hashes actual source/native OpenCV components and exact installed package/Python identity. It does not create or qualify a report. Scene producer frozen pins now require a real installed verifier with measured model/runtime/device identity; unpinned historical scene jobs retain their prior path.

Remaining coding: canonical durable scene/depth prerequisite orchestration for both exact sources; trusted preadmission policy resolution and physical measured producer qualification; assembly of the embedded evidence and additive resource costs; final transactional adoption/live-authority hook and SQL lifecycle/concurrency scenarios; deterministic legacy missing-evidence terminal/recovery handling. Migration02551 currently supplies task-scoped origin/policy worker/adopter readers only and remains an unexecuted implementation candidate. No capability is credited by standalone constructors, unexecuted scenarios, or absent physical qualification.

## Durable producer and frozen handoff addendum

Each exact full VersionBinding has a deterministic namespace under the original
request run. `scene.parse` and `depth.predict` have distinct child run/task/Params
identities. Reused reference bindings share these exact children. A source from
another parent or version never satisfies the dependency merely because its
pixels match. Publication uses the existing accepted-version draft quota and
object fencing protocol; no SQL transaction spans object IO.

The immutable `v27_scene_prepare` marker retains the original parent/request and
actor session, full source binding/hash, Params document/ref, complete frozen
DraftTask and task SHA/input digest, reservation bytes, independent producer
qualification Expected and exact report object pin, measured costs, physical
input/source object descriptors and working ICC SHA. Resource estimates and
qualification are reconstructed from these saved values during recovery, never
from current deployment defaults. The first blocked draft cannot execute until
typed transport registration and dedicated transactional seal repeat live
parent, accepted source, Params, qualification, quota and authority checks.

Completed result access returns only the exact canonical successful
task/result/attempt/fence and Params/root/pixels/optional-invalid object inventory.
The server physically rereads Params and result documents, verifies original
source/canvas/transform/model identity and repeats the scoped catalog after IO.
Worker and independent adopter additionally hash-read all binary arrays. Dedicated
completion settles the child's normal reservation once; it does not replace or
delete source files.

New final all-target graphs retain optional `scene_evidence` per target ordinal
inside `v27_native_targets_transfer`; each entry exactly equals its task's frozen
`execution_profile.semantic_scene`. Legacy accepted graphs omit this field and
retain their original bytes. Saved final graphs requalify the embedded policy
and reconstruct exact producer occurrences without scheduling new prerequisites.
Scene verification costs augment the existing transfer estimator exactly once.
The actual mask-warp tile iterator is shared by transfer and full-resolution
target contributor verification, including magnification and perspective; proxy
center neighbors alone are insufficient proof of the published mask footprint.

These are authored source candidates. Complete SQL lifecycle/concurrency fixtures,
ordinary server configuration and final independent source review are required
before coding closure; physical qualification and all test execution remain later.

### Ordinary installation and immutable recovery policy

The ordinary server deployment project may declare `scene` with a strict policy
document and separate scene/depth qualification report, expected pins and measured
costs. New public commands require this installation after exact accepted receipt
lookup. A sealed accepted receipt remains replayable without current installation;
an unsealed pending command is recovered by its durable prerequisite path.

The original detector recovery metadata freezes this declaration as optional
`scene_policy`. Recompilation and restart recovery read that saved declaration,
never the current process default. The prerequisite runner and final all-target
admission independently recover the same declaration from the verified original
detector plan. Historical metadata omits it and preserves its original bytes.
An ordinary person prerequisite's existing `ordinary_policy` also retains the
same scene declaration for subsequent original-parent creation. A declaration
is a locator and policy pin, not evidence: actual source, installed producer
qualification, measured report, canonical result and live authority remain
mandatory independently at each scheduling and consumption boundary.

Fresh public scene commands start the existing qualified original detector
pipeline under the same original-parent advisory lock. Default detector controls
are the actual FaceDetectParams defaults (score 0.8, maximum 64), frozen in Params.
The durable initial coordinator advances original protection, target protection,
scene/depth and final all-target transfer admission. Existing initial checkpoints
are observed, not rebuilt from current snapshots. Until the parent itself commits
its exact accepted receipt, the public create call returns a recoverable conflict;
it must never return a private child run as the accepted parent ticket.
