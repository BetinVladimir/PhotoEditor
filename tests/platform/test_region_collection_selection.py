from types import SimpleNamespace
from uuid import uuid4,UUID
import pytest
from photo_worker.operations.region.collection_selection import validate_collection_selection


def fixture():
    proof=dict(version='region_culling_selection_v1',run_id=str(uuid4()),review_id=str(uuid4()),decision_hash='a'*64,
        snapshot_id=str(uuid4()),snapshot_digest='b'*64,item_id=str(uuid4()),producer_task_id=str(uuid4()),never_delete=True)
    effective=SimpleNamespace(extensions=SimpleNamespace(culling=SimpleNamespace(enabled=True)))
    return effective,SimpleNamespace(culling_authority=proof)


def test_preview_preserves_frozen_selected_item_without_reranking():
    effective,execution=fixture();proof=validate_collection_selection(effective,execution)
    assert str(proof.item_id)==execution.culling_authority['item_id'] and proof.never_delete


@pytest.mark.parametrize('change',['missing','disabled','delete','zero_item','hash','new_rank','version'])
def test_current_collection_selection_cannot_be_omitted_or_reinterpreted(change):
    effective,execution=fixture()
    if change=='missing':execution=None
    elif change=='disabled':effective.extensions.culling=None
    elif change=='delete':execution.culling_authority['never_delete']=False
    elif change=='zero_item':execution.culling_authority['item_id']=str(UUID(int=0))
    elif change=='hash':execution.culling_authority['decision_hash']='bad'
    elif change=='new_rank':execution.culling_authority['new_rank']=1
    else:execution.culling_authority['version']='generic_selection'
    with pytest.raises(ValueError):validate_collection_selection(effective,execution)
