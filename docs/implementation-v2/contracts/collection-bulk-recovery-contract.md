# Collection bulk recovery and abandonment

Normative coding contract for the restored MVP Collection bulk UI. It does not replace per-operation backend authority or qualify execution. Source candidate, 2026-10-17; no checks executed.

## Immutable command

The user selects explicit current collection item UUIDs using all/filter/range/individual controls. Before any POST the client freezes the item list, collection revision, action and normalized parameters, allocates two original keys, and writes an actor/origin/tenant/collection-scoped durable attempt. The selection POST uses `Selection.Create` and the original `snapshot_key`; the Start POST uses `Bulk.Start` and the original `key`. Numeric revision conversion is allowed only for positive canonical safe integers. Selection.Create binds the immutable current snapshot to the collection, actor and source revision. Start binds that snapshot to the action and parameters. A lost response never allocates a replacement key.

Supported actions are rating, flag, tag, hide, publish, copy, move, remove and retry_media. Removal affects collection membership, not deletion of originals. Copy/move destinations are current writable collections in the source project; server authorization and ordered collection locks remain authoritative. No receipt replay is transformed into a new command merely because a snapshot expired or a collection revision changed.

## Combined abandonment

`POST /api/v1/tenants/{t}/collections/{id}/bulk-operations/abandon` consumes the **original Bulk.Start Idempotency-Key** and `{selection_key,selection,action,params}`. `selection` is the original normalized Selection.Create request, not a newly generated selection. Both keys are independently validated; equal key strings are legal because their receipt Route identities differ.

The transaction checks current authenticated tenant/project Editor authority, then takes receipt locks in this order:

1. `Selection.Create` under the original selection key and normalized request hash.
2. `Bulk.Start` under the original bulk key and normalized BulkRequest hash.

Neither abandonment nor normal admission takes a domain row lock before its first receipt lock. Normal Selection.Create takes no Bulk receipt. Normal Bulk.Start takes no Selection receipt. Thus no reversed pair of receipt locks is introduced. Receipt waits revalidate current authority; final authority is repeated before transaction completion.

If Selection.Create already committed201, its strict canonical SnapshotRef and persisted tenant/collection/source revision/principal/digest/count/expiry tuple are checked. That exact snapshot ID determines the BulkRequest hash. If no snapshot was admitted, Selection.Create becomes a permanent200 abandonment receipt and the Bulk tombstone hash contains a nil snapshot UUID. Nil is only an internal tombstone fingerprint: normal normalizeBulk refuses it, and no fake snapshot ID is returned or inserted. Every syntactically valid late Start has a different hash and cannot create work.

An already committed Bulk.Start202 wins. Its strict canonical ticket and persisted tenant/collection/snapshot/action/params/run/created_by/resource tuple are checked, even if a durable parent binding already exists. The abandonment endpoint returns202 `{status:"accepted",snapshot:SnapshotRef,ticket:BulkAccepted}`. The original stored Start202 body remains unchanged; only this endpoint's response uses the resolution envelope. The client remembers both validated receipts together before awaiting local persistence and subsequently reads the report.

Otherwise Bulk.Start becomes200 `{status:"abandoned",key:originalBulkKey,collection_id}` with no resource ID. Both completed tombstones carry the exact collection parent binding. Their expiry is explicitly extended to year9999 under the receipt lock, including a previously existing unsealed receipt; `Use`'s INSERT expiry argument alone does not update such a row. Completed generic receipts remain governed by the existing retention-until-tenant-purge policy. No accepted operation is deleted, rewound or rolled back.

Normal Selection.Create detects its tombstone before interpreting a resource and returns409 `SELECTION_ABANDONED`. Normal Bulk.Start does the same with409 `BULK_ABANDONED`; a different snapshot hash conflicts with409 `IDEMPOTENCY_CONFLICT`. Cross-collection receipt reuse refuses instead of following a resource from another URL parent. A failed abandonment transaction clears no client attempt.

## Client lifetime and acknowledgment

A visible action owns one controller, deadline and parent mutation token. It captures the exact parent read lifetime; backgrounding, cancellation, API/origin/tenant/collection replacement or parent withdrawal cannot borrow a later lifetime or publish an old result. Stop ends waiting and retains the original attempt; it does not claim server cancellation. Server cancel stops remaining items and does not undo completed ones.

An accepted selection response is retained locally under its exact current scope even if Editor permission is withdrawn while the response arrives. Fresh Start still requires live Editor authority. A response from an old scope/flight cannot overwrite the new scope's memory. Validated accepted tickets and snapshot-ticket resolutions survive local persistence failure in scoped memory; retries do not allocate a new command. Accepted metadata never includes an object-store credential or signed URL.

After confirmed200 abandonment, the client writes a durable closure acknowledgment before removing the active journal. The same cross-context lock and persisted logout generation fence all journal transactions. Stale tabs cannot restore a closed key; a distinct new intent is allowed. After an observed terminal report, a separate explicit finish action acknowledges and clears the accepted attempt. Generic422 responses never clear an uncertain admission because normalization or current-state refusal alone is not serialized proof of child absence.

## Report and remaining qualification

The report binds operation, collection, run, snapshot and action to the saved command. Every result contains the actual immutable snapshot item_id, ordinal, status, error_code and result. Pagination rejects duplicates, invalid ordering and cursor cycles. Removing live membership does not erase snapshot result identity. No report assumes the position of a current grid row.

Authored tests cover normal validation, reordered equivalent receipt properties, immutable journal upgrades, logout/closure fencing, unknown snapshot refusal, accepted-first preservation, simultaneous independent Start/Abandon transactions, late/cancelled responses, role withdrawal and persistence failure. None were executed for this candidate. Live lock ordering, physical processing/cancellation, quality, RLS behavior on an applied schema and complete regression remain the later testing phase.
