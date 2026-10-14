import pytest
from uuid import uuid4

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_plan_adoption import _load_validated_snapshot
from photo_worker.operations.session.contracts import InputSnapshot


@pytest.mark.parametrize('snapshot', [None, False, {}, b'{}'])
def test_semantic_plan_snapshot_authority_must_return_typed_snapshot(snapshot):
    with pytest.raises(AdoptionInvalid, match='SEMANTIC_PLAN_SNAPSHOT_INVALID'):
        _load_validated_snapshot(lambda reader, intent: snapshot, object(), object())


def test_semantic_plan_snapshot_serialization_failure_is_controlled():
    snapshot = InputSnapshot.model_construct(schema_version=1, snapshot_id=uuid4(),
                                             snapshot_digest='a' * 64, items=[object()])
    with pytest.raises(AdoptionInvalid, match='SEMANTIC_PLAN_SNAPSHOT_INVALID'):
        _load_validated_snapshot(lambda reader, intent: snapshot, object(), object())
