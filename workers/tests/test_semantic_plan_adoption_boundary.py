import pytest

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_plan_adoption import _load_validated_snapshot


@pytest.mark.parametrize('snapshot', [None, False, {}, b'{}'])
def test_semantic_plan_snapshot_authority_must_return_typed_snapshot(snapshot):
    with pytest.raises(AdoptionInvalid, match='SEMANTIC_PLAN_SNAPSHOT_INVALID'):
        _load_validated_snapshot(lambda reader, intent: snapshot, object(), object())
