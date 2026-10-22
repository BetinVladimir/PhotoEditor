# Collection publication

Current coding contract, 2026-10-17. Tests are authored, not executed.

`POST /api/v1/tenants/{t}/collections/{id}/publication` requires the current Editor/project authority and a locked writable (`draft` or `published`) collection. The JSON request has `expected_revision` (positive numeric int64), `item_ids` (unique nonzero UUIDs), `publish` and `publish_collection` (booleans). Revision mismatch refuses the entire transaction.

For collection-only publication, send `item_ids: []` and `publish_collection: true`. `publish: true` changes collection state to `published`; false changes it to `draft`. Empty item selection is valid only with `publish_collection: true`. This supports empty galleries without inventing a photo selection. Item publications and visibility remain unchanged. Existing nonempty selections remain limited to 500 and retain readiness/review validation and transactional photo publication behavior.

The response is the current Collection projection with revision incremented exactly once. The client validates tenant/project/collection, requested state and exact revision increment. Because the existing request uses a numeric revision, the client refuses integers beyond JavaScript's safe range rather than sending a rounded CAS value.

The mounted Collection settings delegate provides Publish/Unpublish actions, guarded by current scope, current editing authority, writable state and the shared mutation lock. Requests have a bounded wait. A stopped, failed or ambiguous request blocks further mutations until an explicit GET reconciliation; no automatic POST repetition occurs. Publishing the collection does not create a share link or make hidden photographs visible.

Source: `backend/internal/collection/service.go`, `backend/internal/httpapi/catalog.go`, `apps/photographer/src/collection-metadata-api.ts`, `apps/photographer/src/collection-metadata.tsx`.

Authored scenarios: collection-only publish/unpublish, empty item-only refusal, stale CAS, no photo publication changes, exact client payload, wrong response state and unsafe revision refusal. Whole Collection closure and runtime behavior remain unproven until the planned review and testing phase.

## Current guest authority after unpublication

Unpublication withdraws guest photo viewing and downloading, including already prepared archive volumes. The02554 source candidate requires a currently published, nondeleted collection when issuing or replaying a guest volume intent, consuming it and validating its exact object after the external signer returns. The private check takes a collection SHARE lock so it serializes with the publication row update. Existing portal media resolution also requires publication and repeats resolution after signing.

Draft upload-only access remains possible through current share permissions and the upload admission rules; collection unpublication does not itself revoke every share session. Authenticated owner downloads retain current tenant/project authority independently of guest publication. A storage URL already returned cannot be revoked by changing SQL state, and remains limited by the established frozen URL lifetime of at most 60 seconds. No new URL is returned after the final authorization check observes unpublication.

The production API startup requires the installed02554 private helper and all three guest-volume consumers. The producer-backed `guest_volume_publication_integration_test.go` scenarios include positive publication baselines, unpublication denial, draft owner volume access and draft guest upload permission. They are authored only.
