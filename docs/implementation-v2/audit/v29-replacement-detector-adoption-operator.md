# V29 replacement detector adoption scheduler installation

Implementation authored; no test/build/migration execution evidence is claimed.

RunScheduler installs a dedicated independent adoption loop when
`PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_ADOPTION_ENABLED=true`. Default is disabled.
Invalid enabled configuration prevents scheduler startup. Disabled mode does not
route replacement results through ordinary perspective detector adoption.

Operator must supply absolute paths:

- `PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PYTHON`: Python interpreter with worker dependencies.
- `PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_WORKER_ROOT`: directory containing photo_worker.
- `PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_BRIDGE_ROOT`: existing private, non-symlink directory, not writable by group/others. Keep Unix socket path below platform limit.

Optional `PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_TIMEOUT_SECONDS`: integer 1–600,
default 600, bounds loading plus replay and final commit.

Explicit child environment allowlist:
`DATABASE_URL`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`,
optional `APP_ENV`, `PHOTO_ICC_ROOT`,
`PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED=true`,
`PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG`,
`PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA`,
`PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_ROOT`,
`PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_MAX_BYTES`,
`PHOTO_ADOPTER_PERSPECTIVE_LINES_RAM_MAX_BYTES`.
The config digest pins existing qualified detector installation; no model or
quality fallback is generated. Positive RAM/scratch budgets are required; this
CPU detector does not allocate GPU VRAM. Child does not inherit other secrets.
DATABASE_URL must use scheduler authority; S3_BUCKET must match scheduler store.
Actual deployment secrets are not saved in this document.

SQL migrations through 01515 must be applied by the deployment operator. Startup
checks discovery, decision recovery, evidence, commit and durable acknowledgement
functions for SECURITY DEFINER/photo_fn_owner ownership and scheduler EXECUTE.
Physical source checks remain in loader and final commit, not discovery.

Loop ticks every five seconds, processes at most one bounded job per invocation,
advances cursor on job failure, wraps on empty page, and records readiness via
ExpansionLoopState. Shutdown cancels loop and waits for Python/bridge cleanup.
A durable adoption receipt acknowledges retries after ambiguous responses.

Still open: deployment qualification and cross-process tests; upstream automatic
replacement detector preparation scheduling; downstream fresh-line consumption
and automatic perspective publication/review/Apply. This installation alone does
not complete V29.

## Automatic preparation integration

`PHOTO_SCHEDULER_REPLACEMENT_DETECTOR_PREPARATION_ENABLED=true` wraps the
configured negative/manual-perspective replacement dispatcher with automatic
detector preparation. Requires `PHOTO_V29_NEGATIVE_CONFIG` and its existing digest
pin, scheduler working ICC, and `PHOTO_SCHEDULER_PERSPECTIVE_LINES_RUNTIME_SHA`.
The runtime pin must match the qualified detector installation; no fallback.
The adoption switch above is independent and must also be provisioned for a
complete detector lifecycle. Preparation shares the replacement publication
budget (currently one new admission per scheduler pass). Historical admissions
do not consume this budget. Detector admissions/pending/unconfirmed remain
separate from perspective receipts. Original Deferred entries are retained:
detector publication cannot acknowledge perspective replacement completion.
Downstream automatic perspective publication remains open.

## Automatic perspective lifecycle wiring

When both negative and perspective project deployments are configured, the
preparation wave now advances adopted detector results through segment
publication/versioned verification, fresh project qualification/resource sizing,
automatic draft/Params/transport, exact typed input registration and graph
admission. Unsupported detection remains explicit and does not complete a branch.
Only durable perspective receipts remove the corresponding deferred operation.

Preparation-enabled startup requires all dedicated scheduler SQL signatures
through 01529 with SECURITY DEFINER/photo_fn_owner ownership and EXECUTE, plus
01530 execution-plan resolver integration. Startup does not apply migrations.
01530 routes automatic replacement source/adoption/preview observations through
its dedicated historical receipt and current segment authority. Deployment,
positive lifecycle and review/Apply completion remain unverified.


### Replacement execution prerequisite

Manual and automatic perspective replacement startup now requires migration01531 in addition to the admission inventory. This migration installs private session/source authority and routes worker source, qualification and completed-task adoption through it. Startup rejects missing routing, incorrect ownership/security mode or exposed private helper EXECUTE privileges. It does not apply migrations. All migrations through01531 and authored tests remain unapplied/unrun in this implementation phase; successful startup is not evidence of positive pixel replay, review or Apply.
