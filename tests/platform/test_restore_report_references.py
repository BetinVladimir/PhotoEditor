from dataclasses import dataclass,asdict
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.runtime.restore_report import _json


@dataclass(frozen=True)
class ParentEvidence:
    reports: tuple
    review_required: bool


def test_parent_report_artifact_pins_survive_nested_dataclass_serialization():
    refs=tuple(ArtifactRef(artifact_id=uuid4(),sha256=ch*64) for ch in ('a','b'))
    observations={'admission':{'positive_batch':{'physical':{
        'parents':asdict(ParentEvidence(refs,True))}}}}
    decoded=parse_json(canonical_bytes(_json(observations)))
    parents=decoded['admission']['positive_batch']['physical']['parents']
    assert parents=={'reports':[{'artifact_id':str(ref.artifact_id),'sha256':ref.sha256} for ref in refs],
        'review_required':True}


@pytest.mark.parametrize('value',[object(),b'private source bytes',float('nan'),float('inf'),float('-inf')])
def test_reference_support_does_not_silently_stringify_unknown_evidence(value):
    with pytest.raises(ValueError,match='RESTORE_REPORT_VALUE'):_json({'nested':[value]})


def test_finite_numeric_metrics_remain_json_numbers():
    assert parse_json(canonical_bytes(_json({'metric':1.5})))=={'metric':1.5}
