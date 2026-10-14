from hashlib import sha256
import json
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.interchange_mapping import MappingEntry, MappingField, publish_mapping_report


def entry(status='exact'):
    return MappingEntry(entry_id=uuid4(),external_id=str(uuid4()),asset_version_id=uuid4(),proposed_document=None,
        fields=[MappingField(name='recipe',status=status,reason=None if status=='exact' else 'unsupported dependency')])


def test_mapping_shards_preserve_complete_order_and_entry_counts():
    entries = [entry('unsupported' if index==0 else 'rejected' if index==100 else 'exact') for index in range(201)]
    calls = []
    def publish(body,contract,ordinal):
        calls.append((body,contract,ordinal))
        return ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    result = publish_mapping_report(entries,[(e.entry_id,e.external_id) for e in entries],publish)
    assert [len(json.loads(body)['entries']) for body in result.shard_bodies] == [100,100,1]
    assert [item['entry_id'] for body in result.shard_bodies for item in json.loads(body)['entries']] == [str(e.entry_id) for e in entries]
    report = json.loads(result.report_jcs)
    assert (report['entry_count'],report['unsupported_count'],report['rejected_count']) == (201,1,1)
    assert [call[1:] for call in calls] == [('MappingShard',0),('MappingShard',1),('MappingShard',2),('MappingReport',0)]


def test_mapping_source_mismatch_is_rejected_before_publication():
    e = entry()
    with pytest.raises(ValueError,match='source identity'):
        publish_mapping_report([e],[(uuid4(),e.external_id)],lambda *args: pytest.fail('unexpected publication'))


def test_mapping_wrong_published_hash_cannot_enter_report():
    e = entry()
    with pytest.raises(ValueError,match='publication mismatch'):
        publish_mapping_report([e],[(e.entry_id,e.external_id)],lambda *args: ArtifactRef(artifact_id=uuid4(),sha256='a'*64))


def test_mapping_field_failure_requires_reason():
    with pytest.raises(ValueError):
        MappingField(name='crop',status='unsupported',reason=None)
