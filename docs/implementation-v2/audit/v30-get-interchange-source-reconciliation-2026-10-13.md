# V30 GetInterchange source reconciliation — 2026-10-13

The authenticated tenant route `GET /api/v1/tenants/{t}/interchange-jobs/{id}` delegates to `interchange.Service.Get`, which uses `tenant.WithTenant` with viewer authority and calls `photo.get_v30_interchange` with the current actor, session, tenant and workflow ID. Migration 01326 defines the import projection. Migration 01370 renames that function and creates the export-aware wrapper; its export branch requires a sealed run, export task, ready Params document, project ACL and a pinned format from immutable result evidence or the private pending record.

The adjacent inspection route calls `photo.inspect_v30_interchange`. Migration 01370's wrapper calls the renamed inspection function for a base result and adjusts export stages. The renamed function calls `photo.get_v30_interchange` by its current SQL name, so export jobs can obtain the base result after 01370. An apparent export-inspection NULL gap from reading only the old import migration is therefore not an absent code path.

This is source evidence, not acceptance evidence. Current migrated PostgreSQL, project ACL matrix, import/export job status transitions, adopted artifact readiness, HTTP response shape and client rendering remain unverified. No tests, builds, validators or migrations were run during this reconciliation.

The import `GetInterchange` projection currently leaves `mapping_artifact_id`, `report_artifact_id` and `bundle_artifact_id` null. The resource schema permits null for all three, while the import inspection projection exposes the bounded review report reference and target summary. The current contracts do not establish that these resource fields must become non-null after review, so this source finding alone does not justify a new artifact-access path. Export projection separately gates report and bundle IDs on adopted, currently ready outputs.

The Go `ReadJob` boundary now validates the closed public resource shape and exact tenant/workflow IDs, canonical revision, format/direction, timestamps and nullable artifact IDs before returning SQL JSON. Unit cases for a valid owned resource, scalar/incomplete JSON and foreign tenant were authored, NOT RUN. This closes a source-level response-integrity gap; it does not establish SQL projection or client acceptance.

The adjacent `/inspection` boundary now also validates the SQL projection before returning it: closed top-level shape, tenant/workflow identity, nonempty run/project IDs, canonical revision, status/stage pairing, nullable child identifiers, and bounded review summary/report pins. Authored unit cases cover a running inspection, review inspection, foreign tenant, malformed shape and inconsistent lifecycle state; NOT RUN. Current SQL/client acceptance remains unverified.

The review page is also checked entry by entry: closed target/field shapes, unique nonzero entry IDs, proposed state, external/asset identifiers, field names/status/reason rules, and page/count bounds. A positive populated page and negative wrong-state case are authored, NOT RUN.

Required review counts, booleans and target/field arrays now reject JSON `null` instead of accepting Go zero values; corresponding refusal cases are authored, NOT RUN.
# Client inspection projection follow-up

The photographer client now requires the public `/inspection` projection to carry an explicit `review` field, including `null` outside review, matching the Go response gate's closed eleven-field document. Primitive or array review values are refused with a domain error before nested field access. The client fixture and missing/malformed review unit cases were updated. Tests remain NOT RUN under the implementation-first plan; this does not establish end-to-end V30 acceptance.

The client resource reader now also enforces the four-field envelope and thirteen-field `interchange_jobs` data projection, mandatory ordered timestamps, and a closed nullable document artifact/SHA reference. The photographer job and mounted-screen fixtures were updated to the SQL/Go projection; missing, malformed and extra-field refusal cases were authored. These checks are source-alignment work only; tests/builds remain NOT RUN.

The inspection reader now applies the server's closed eleven-field projection and closed review/report/target/field shapes. Added client refusal cases for extra fields at each level. Runtime behavior and build status remain unverified until the deferred testing phase.

The review reader now requires `can_decide` to be a real boolean and `targets` to be an array even if both keys are present with `undefined`; the public Go response gate already requires those types. Negative client cases were authored, NOT RUN.

The client timestamp reader now rejects impossible calendar dates and out-of-range clock/offset components before `Date.parse` can normalize them. Added invalid-date and `24:00` refusal cases, NOT RUN. Static route review also confirmed the selected import format is carried through upload handoff into the closed create request and its durable attempt journal; no format-authority change was needed there.
