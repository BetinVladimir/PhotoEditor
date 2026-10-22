"""Single-image preview preserves a live frozen collection selection."""
from typing import Literal
from uuid import UUID
from photo_worker.contracts.base import Contract,SHA256,canonical_bytes


class RegionCullingSelection(Contract):
    version: Literal['region_culling_selection_v1']
    run_id: UUID
    review_id: UUID
    decision_hash: SHA256
    snapshot_id: UUID
    snapshot_digest: SHA256
    item_id: UUID
    producer_task_id: UUID
    never_delete: Literal[True]


def validate_collection_selection(effective,execution):
    requested=effective.extensions.culling
    enabled=requested is not None and requested.enabled
    authority=None if execution is None else execution.culling_authority
    if enabled!=(authority is not None):raise ValueError('REGION_CULLING_SELECTION_AUTHORITY_REQUIRED')
    if authority is None:return None
    selected=RegionCullingSelection.model_validate_json(canonical_bytes(authority))
    if any(value.int==0 for value in (selected.run_id,selected.review_id,selected.snapshot_id,selected.item_id,selected.producer_task_id)):
        raise ValueError('REGION_CULLING_SELECTION_INVALID')
    # The live SQL scope binds item to the original version/source and current
    # decided review. A preview never replaces this with a new ranking result.
    return selected
