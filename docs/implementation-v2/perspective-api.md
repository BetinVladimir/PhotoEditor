# Perspective editor API integration (implementation in progress)

`POST /api/v1/tenants/{t}/projects/{id}/perspective-intents` is the dedicated editor submission extension. It requires tenant write authorization/CSRF and a valid `Idempotency-Key`, and accepts at most8192 bytes. JSON has exactly `schema_version:1`, `snapshot_id` and `params` (the full OpticalPerspectiveParams contract including explicit nullable corners/lines). Unknown fields, source document bytes, qualification pins and resource overrides are rejected. The API returns202 with `{run_id,workflow_id,state:"queued"}` only after validating the historical receipt actor/session/operation/task/workflow identities. It does not return current job state; use the existing workflow reader for current state.

Server.PerspectiveSubmit installs the service interface. With no service installed the route returns503 DEPENDENCY_UNAVAILABLE. PerspectiveService is scoped to exactly one tenant/project and owns independent deployment policy lists/pin/capacity. The source loader resolves the current accepted snapshot and immutable WorkingImage object version, reads bounded original bytes outside SQL and rechecks the locator. Resource pixel_count becomes the exact derived output size only after checking the operator pixel ceiling; RAM/scratch/output/zero-VRAM ceilings remain deployment-owned.

Current producer supports manual geometry. Worker auto geometry exists, but backend auto planning and LineSegments production are still incomplete. A manual API route is not acceptance of the complete perspective operation. Application bootstrap/configuration, multi-project service dispatch, editor controls and integration qualification remain required. This route extends existing V29 editor orchestration; the normative OpticalPerspectiveParams ABI is unchanged.

All new code/tests/migrations in this implementation increment are NOT RUN. No enabled policy or qualification is provisioned by this documentation.

## Backend deployment bootstrap

Set PHOTO_V29_PERSPECTIVE_CONFIG to an operator-owned regular JSON file (at most8MiB), PHOTO_V29_PERSPECTIVE_CONFIG_SHA256 to the SHA-256 of its original bytes, and V29_PERSPECTIVE_SCHEDULER_DATABASE_URL to a dedicated scheduler DSN. Absent config leaves the route unavailable. Invalid configured files, digest, role, missing SQL capabilities or policy/pin/capacity inconsistencies refuse startup. Bootstrap forces photo_scheduler and closes its pool on failure/shutdown. It does not import qualifications, enable registry policies, apply migrations or install worker runtimes.

Config exact shape: schema_version1, projects array1..2048. Each entry has tenant_id, project_id, pin (PerspectiveQualificationPin), policy (DeploymentLocks), max_pixels, capacity (PerspectiveResourceEstimate). Duplicate tenant/project entries are refused. Pin/policy operation/version/backend/model/runtime/device/dataset/issuer and canonical policy SHA must agree. Capacity uses CPU zero VRAM, pixel ceiling<=200M and safe-integer RAM/scratch/output ceilings. Source/task exact output pixel_count is derived under those ceilings. Per-project dispatch refuses unconfigured scopes without IO.

Bootstrap and project dispatch are now implemented; deployment execution and migrations remain NOT RUN. Editor controls and backend auto planning remain open.

## Photographer client increment

Editor now includes manual perspective numeric corner/strength/crop controls, server binding/snapshot preparation, dedicated scoped persistent submission journal, same-body/key retries and workflow-authorized navigation. Journals are fenced and cleared on logout. Numeric corners use normalized source pixel-edge coordinates in TL/TR/BR/BL order. This is not a visually verified complete UI: drag handles/auto controls/repeated authoring and complete result review/Apply still require implementation or integration review. Client unit tests and all new code remain NOT RUN.

## Explicit line authoring extension (unverified)

`POST /api/v1/tenants/{t}/projects/{id}/perspective-lines`, authenticated tenant-write with CSRF and Idempotency-Key, accepts `{schema_version:1,snapshot_id,base,line_segments}`. `base` is the existing accepted version/source/canvas binding; `line_segments` uses the canonical LineSegments contract with normalized endpoints and horizontal/vertical families. Unknown fields/object locators are rejected. At most1MiB. Deployment must install the scoped perspective service and apply01048–01050. Source resolution, publication and original readback precede200 `{schema_version,tenant_id,project_id,snapshot_id,base,lines}`. Same key/body retries preserve document identity; changed content conflicts. An ambiguous response must be retried with the same key/body. This authors lines explicitly; no automatic extraction model is installed. Publishing lines alone does not admit a perspective task. Auto task admission remains unfinished. Tests/migrations have not been run/applied.

## Auto intent submission wiring (unverified)

The existing `perspective-intents` service now resolves the original source-bound line ref for `mode:auto`, recomputes effective strength geometry and exact resource PixelCount, then uses the dedicated perspective publication/admission protocol. The client submits the ref returned by explicit line publication, with `corners:null`; object keys/line bytes/runtime overrides are not accepted in the intent. The line binding must match the exact input snapshot and accepted version/source/canvas. Even zero-strength auto verifies line authority. Deployments require01048–01052 in addition to the prior perspective chain. No automatic line extraction is installed. This code path remains untested; production availability must not be inferred from this appendix.
