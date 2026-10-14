# V23/V30 source reconciliation — 2026-10-14

This is a source inventory for the code-first phase, not runtime acceptance. No tests, builds, migrations, model inference, SDK calls or object-store flows were executed for this review.

## V23 Evaluate installation

The old note in `v23-holdout-effects-source-gap-2026-10-14.md` that says the CLI installation remains open is superseded as a source-presence claim. `workers/photo_worker/cli.py` now calls `learning_evaluation_from_environment`; `runtime/service.py` accepts `SignedLearningRequest` and resolves it through `install_signed_learning_evaluation`; `runtime/learning_config.py` reads the pinned report, signature, public key, package lock and evidence root; `operations/learning/qualified_installation.py` verifies the signed qualification, installed CUDA device, ICC, source tree, packages and required model bytes before task execution. The deployment verifier and worker wiring therefore exist in source.

This does **not** enable V23 in a running installation. The `learning.evaluate` operation remains disabled in the database registry pending a real deployment-signed qualification report, target-device/model/effect checks, migrated SQL and end-to-end Fit→Evaluate→review/Apply acceptance. Do not turn on `v2_enabled` based on the existence of the installer or synthetic tests.

## V30 required provenance create gate

The earlier statement that public required-mode creation has no code path is superseded. `backend/internal/delivery/create_signing_policy_test.go` exercises the installed tenant policy checker and actual frozen preset MIME set; current creation code calls those checks. The API and scheduler have separate pinned signing deployment installers (`backend/cmd/photo/signing_installation.go`), and the Delivery signing stage and signed assembly processor are wired through the scheduler installer. The source path for a structurally valid required choice is present.

This does **not** establish that a signed Delivery package can complete. Migrations 01875–01887 and later gates have not been applied or exercised in this phase, and no current physical evidence proves signer dispatch, native signed output, assembly, published package, download/revocation or browser retrieval. Those are the next V30/V31 module-cycle checks after code completion; any specific missing implementation must be identified from current source rather than copied from older chronological audit entries.

## Next code decision

Audit the current `learning.evaluate` registry activation contract and the complete V30 signed-package path against `07-execution-acceptance.md` before changing enablement. Keep feature availability closed until all required runtime qualification and migrated-database evidence exists. The whole V20–V32 implementation percentage remains unknown.

## V23 qualification file boundary (source update)

`runtime/learning_config.py` now opens every component of the deployment path through `O_NOFOLLOW` directory descriptors, requires a single-link regular final file, bounds reads to the recorded size, and compares the opened file with the same directory entry after the read. This closes parent-symlink escape and changed-file acceptance in the signed report/key/package-lock reader. Unit refusal cases for a symlinked parent and hardlinked key were authored. Python syntax compilation and diff whitespace checks passed; the unit cases and a live qualified installation were not run under the code-first agreement. This does not change the disabled Evaluate registry state.

The qualification source-tree hash reader now opens every package-root ancestor with `O_NOFOLLOW`, retains a root descriptor during hashing, refuses hardlinked Python files and compares each opened file's device/inode/size/mtime with the same directory entry after reading. It also rejects a changed root path before returning the digest. Symlinked-parent, hardlink and replaced-entry refusal cases were authored; Python AST parsing passed, while unit tests and a signed deployment were not run. This strengthens the proof of the installed renderer bytes but does not enable Evaluate or expand its supported effects.

The same source-tree reader now enumerates directories through the pinned root descriptor with bounded depth, entry count, path length and Python-file count. It rejects symlink entries and changed directory identities, sorts relative names before hashing, and opens the listed files under that same descriptor. The earlier path-based `rglob` could enumerate a different tree from the pinned file reads after a root-path swap. A nested lexical-order case that forbids `Path.rglob` was authored and parsed, **NOT RUN**. Real deployment qualification and Evaluate acceptance remain open.

The qualification verifier's evidence root and packages lock now use no-follow traversal for every parent path component. Evidence files and the lock must be single-link regular files; their opened identity is compared with the same directory entry after a bounded read before any SHA is accepted. Existing root-replacement tests were adapted to descriptor-relative opening, and hardlink/entry-replacement/parent-symlink cases were authored. AST parsing passed; tests and signed deployment verification remain **NOT RUN**. This does not create or sign qualification evidence.

The packages-lock hash now also compares its pinned parent directory with the current path after reading. Without that check, a parent replacement could leave the verifier hashing a lock from a directory that is no longer installed. A rename-and-symlink replacement case was authored and AST parsing passed; the test itself is **NOT RUN**.

The descriptor-relative source-tree walker now enforces its 100,000-entry ceiling while iterating `scandir`, before accumulating and sorting directory names. Previously a single oversized directory could allocate its entire name list before the refusal. An oversized enumerator refusal case was authored and AST-parsed, **NOT RUN**.

# V30 RAW metadata parser (source-only, 2026-10-14)

`workers/photo_worker/v2/delivery_metadata_raw_extract.py` now decodes one bounded
ExifTool `-j -G1 -n` record into the existing allowlisted `MetadataDocument`.
It rejects duplicate JSON keys, multiple records, malformed/partial GPS and
non-numeric or non-finite coordinates. GPS is absent unless explicitly opted in;
maker notes and other unlisted tags are ignored. Unit cases are authored in
`workers/tests/test_delivery_metadata_raw_extract.py`, NOT RUN under the
accepted code-first sequence. This parser is not yet invoked by the source
job; no RAW MIME is enabled by this change. Remaining code work includes a
bounded, qualified ExifTool process over the SHA-verified immutable original,
post-process file-identity recheck, cross-language MIME/SQL admission updates,
and positive/negative physical RAW fixtures. Do not count RAW Delivery as
available or the V30 module as complete.

The next source change adds `delivery_metadata_raw_tool.py`: it invokes a
configured absolute ExifTool executable with an explicit EXIF tag list, adds GPS
tags only on opt-in, drains stdout with a 64 KiB ceiling and 120-second deadline,
suppresses stderr, checks cancellation, and kills/reaps on failure. Tool output,
nonzero exit and successful JSON cases are authored in unit tests, **NOT RUN**;
AST parsing and whitespace checks passed. This is not yet wired to the
SHA-pinned source preparation job, and file-type qualification plus current
original-identity checks still have to surround it before any RAW MIME is
admitted by Go, Python job schemas or SQL.

The RAW tool is now called by the low-level SHA-pinned metadata source
preparer for the seven MIME types already recognized by upload admission
(DNG, CR2, NEF, ARW, RAF, ORF, RW2). It requires an explicitly installed
ExifTool executable and checks the physical `File:FileType` against that MIME.
The existing source descriptor/path identity check runs after the subprocess;
SHA verification runs before it. Direct DNG source, changed SHA, absent tool
and format-substitution cases were authored, **NOT RUN**. This is still an
internal path: the authenticated original reader, job schema, publication
compiler, Go and SQL still refuse RAW, so public extraction remains unavailable.

The authenticated Python original-reader and source job now carry those seven
RAW MIME values through current original evidence, download, SHA-pinned source
preparation, frozen metadata projection and canonical publication. RAW jobs
require an explicit `PHOTO_METADATA_SOURCE_EXIFTOOL` absolute executable path;
without it extraction fails closed. A DNG job scenario with SQL reads outside
object IO and absent-tool refusal was authored, **NOT RUN**. The earlier
sentence saying the Python job/reader/compiler refuse RAW is superseded. SQL
`v30_delivery_metadata_original`, Go publication/creation, and source
publication SQL validators still need the matching allowlist and current
authority/physical tests before public enablement.

Go's authenticated original resolver and canonical metadata-source compiler
now accept the same seven RAW MIME values and still reject unknown MIME.
The API subprocess allowlist carries optional
`PHOTO_METADATA_SOURCE_EXIFTOOL` only as an absolute clean path. Forward-only
migration `01979_v30_raw_metadata_source_publication.sql` replaces the private
publication value validator with the matching closed MIME set; its existing
owner and role ACL remain. Go MIME and environment cases are authored and
gofmt applied; migration/test execution is **NOT RUN / NOT APPLIED**. Runtime
deployment must install a qualified ExifTool binary and physical camera RAW
fixtures before enabling RAW metadata extraction. Whole V30 completion is
still unproven.

The RAW ExifTool entry executable now requires a paired lowercase SHA-256
configuration. The worker hashes a no-follow, regular, single-link executable
before and after the bounded extraction, checks the same path identity, and
refuses missing/changed pins. Go subprocess configuration accepts the two keys
only together and requires a clean absolute path. Authored wrong-digest and
missing-pair tests are **NOT RUN**. This does not pin ExifTool's Perl module
tree; deployment must qualify those dependencies and real camera fixtures.

Source review of ExifTool family-1 group names found that `-G1` emits
`IFD0:Artist` / `ExifIFD:DateTimeOriginal` rather than `EXIF:*` JSON keys.
The RAW parser and authored fixtures now use those physical groups and refuse
duplicate semantic fields across IFD0 and ExifIFD. This corrects a path that
would otherwise return empty metadata from valid cameras. The correction was
AST-parsed, **NOT RUN** against a real ExifTool or RAW file.

The RAW extraction command now uses `-fast2` alongside its explicit tag list,
so it avoids scanning MakerNotes and embedded payloads outside the Delivery
metadata allowlist. The command-argument case was updated, **NOT RUN**. The
camera matrix must verify each allowed EXIF/GPS field on every supported RAW
format before this mode can be called qualified.

The preceding `-fast2` choice was removed after checking the quality
requirement: an early-stop scan could omit an allowlisted EXIF field on a RAW
layout whose metadata lies later in the file. The current command performs a
complete scan with bounded original bytes, process time and captured output;
its explicit output tag list still prevents copying MakerNotes. The argument
case now requires no `-fast2`, **NOT RUN**.

An operator-run seven-format RAW metadata matrix is now authored as
`photo_worker.v2.delivery_metadata_raw_qualification`. It requires one
SHA-pinned original per admitted MIME and at least one GPS-bearing fixture,
compares exact normalized metadata with GPS disabled and enabled, and emits
canonical per-format evidence. Synthetic CLI/unit cases are authored,
**NOT RUN**. No genuine camera corpus, signed qualification report or active
deployment is supplied by this code; those remain for the physical phase.

The Go Python-preparer now rejects a RAW original with no configured
ExifTool path/SHA pair before launching the subprocess. An authored DNG case
asserts this early refusal; JPEG/PNG/TIFF still do not require RAW settings.
The case is **NOT RUN** and does not replace current SQL/original authority.

V23 sensor-dust holdout work has started at the immutable evidence boundary.
`operations/learning/dust_evidence.py` now defines a canonical reviewed-dust
document with exact Go `sensor_dust_decision_v1` hash, accepted repair route,
source/protection/proposal/Params refs, and external SQL-supplied identity
comparison. Valid, changed-review, alias, foreign-actor and altered-SHA unit
cases are authored, **NOT RUN**. This type is not yet in RenderCase/Go manifest,
SQL closure, task inputs or the holdout renderer. The existing refusal of
sensor dust remains mandatory until those paths and pixel/quality evidence
are complete.

Go now has the matching `CompileFrozenDustHoldoutEvidence`: it copies the
review selection, checks the existing `sensor_dust_decision_v1` hash and
accepted repair route, validates distinct pinned refs and emits canonical
document bytes/SHA. Mutation, changed decision, route, selection, alias and
revision cases are authored. `gofmt` passed; tests are **NOT RUN**. The
compiler consumes only a supplied decision value; no current SQL review read,
manifest publication, task-input registration or dust pixel replay is
installed yet, so V23 Evaluate still refuses sensor dust.

The matching Go `DecodeFrozenDustHoldoutEvidence` now checks canonical object
bytes/SHA and independently SQL-supplied tenant, event, version, review, run,
actor, decision and four input refs. Authored cases cover exact decode,
foreign actor, changed proposal, wrong object SHA and noncanonical bytes;
`gofmt` and `git diff --check` passed, unit cases remain **NOT RUN**. This is
only an immutable document boundary. No live SQL authority read, manifest
closure, task-input registration or complete dust holdout render is connected.

Go `ValidateFrozenDustHoldoutMaterial` now binds exact SHA-pinned repair Params
and candidate proposal bytes to that evidence, requires the image and reviewed
base to be present in the proposal frame series, and recomputes the selection
and decision hash with the existing `review.PlanDust` implementation. Authored
unit cases cover a valid reviewed candidate and changed Params/proposal bytes;
`gofmt` passed, unit cases are **NOT RUN**. Python has a corresponding material
binding check using `accepted_dust_candidates`. These validators still require
SQL-authorized object reads, manifest/task-input closure and full measured
holdout rendering before sensor dust can be enabled in Evaluate.

The dust evidence now has a recovery-stable UUIDv5 identity
`ReviewedDustHoldoutRef(run,event,SHA)` with namespace `run` and name
`v23-dust/{event}`. Go/Python freeze contracts reject a review base aliased to
protection, proposal or Params, while permitting a separate valid frame base.
The Go unit cases for stable replay and alias refusal are authored, **NOT RUN**.
The SQL reserve/complete functions must reproduce this UUID name and must
authorize a unique current sensor-dust review from the correction event;
neither function exists yet. The existing reviewed-defect SQL cannot be reused
by changing only its purpose or MIME because its producer and repair joins
are defect-specific.

`PrepareReviewedDustHoldout` now assembles the canonical evidence only from a
candidate plus a separately supplied reviewed source. It checks live-candidate
flags, training recipe refs and bytes, source version/image binding, exact
WorkingImage and protection MaskRef bytes, matching canvas/transform chain,
protected=true, and the SHA-pinned Params/proposal material validator before
returning publication bytes. This code is **NOT RUN** and does not publish or
authorize anything. The SQL source must handle the actual Apply v4 dust graph:
`sensor.dust` review plus a separate `sensor.dust_repair` stage, with no
`defect_branches` analogue. Source-event uniqueness, reserve/complete,
manifest/task closure and measured worker replay remain open.

Source tracing corrected an important identity error in the first dust
contract: Apply v4 detects dust on the `image.color` WorkingImage, whereas a
V23 training case binds the original asset-version WorkingImage. They are
different artifacts. Frozen dust evidence now carries both `image` (the
training/source root) and `stage_image` (the exact detection/repair input).
Go/Python material checks require repair Params and proposal frames to use
`stage_image`, while the training binding remains on `image`. Preparation
checks both SHA-pinned WorkingImage documents and matching canvas identity.
The future SQL source must prove the stage image was produced for the same
asset version by the reviewed Apply graph; equal canvas alone is insufficient.
The corrected cases are authored and **NOT RUN**. No sensor-dust holdout
capability has been enabled.

Forward-only migration `01980_v23_reviewed_dust_event_source.sql` now authors
a scheduler-only read of the unique accepted dust source for a CorrectionEvent.
It binds current tenant/project/editor/consent and original asset version to
the same Apply v4 snapshot item, canonical `image.color` output, dust detect
task/proposal/review, and the item's completed dust repair task/Params. The
read rejects no-match and ambiguous matches, checks exact versioned artifacts
and producer results, and returns original and stage-image identities
separately. `LoadReviewedDustForCandidate` compares the SQL-selected source to
the locked training candidate and render binding, then recompiles the frozen
decision hash. Both are authored, **NOT RUN**; SQL migration has not been
applied or physically checked. Publication reserve/complete, source recheck
around object reads, manifest/fit/Evaluate closure and measured pixel replay
remain open.

Forward-only `01981_v23_reviewed_dust_artifact_locator.sql` now authors an
exact-version JSON locator for the four source roles (`image`, `stage_image`,
`proposal`, `params`) through the current unique event source. The Go
`SQLReviewedDustObjects` reads bounded SHA-pinned bytes with locator checks on
both sides of object IO. `PrepareReviewedDustForCandidate` repeats the full
SQL source selection after all four reads before constructing the canonical
holdout document. These paths are **NOT RUN** and the migrations are not
applied. Protection remains independently authorized by the training mask
reader; publication must repeat current source authority and establish a
registered root/closure. No Evaluate scheduling is enabled.

Forward-only `01982_v23_reviewed_dust_document.sql` now authors the separate
`v23_dust_holdout` document purpose and reserve/complete transitions. Both
check the recovery manifest's deterministic `dust_evidence` ref and repeat the
current event-selected review/decision authority; Complete records the exact
object version and output role. `artifactjson` routes this purpose through the
dedicated SQL functions, and `PublishReviewedDustHoldout` verifies canonical
bytes, stable ref and one matching manifest case before publication. Go and
Python manifest models now carry optional `dust_evidence` with alias/mutual
exclusion checks; Go draft and Fit closure require the deterministic UUID.
These paths are authored, **NOT RUN**, and migration 01982 is not applied.
Compilation/recovery attachment, SQL frozen closure, Evaluate claim/inputs,
worker renderer and quality measurement remain incomplete. The registry stays
disabled for sensor dust.
