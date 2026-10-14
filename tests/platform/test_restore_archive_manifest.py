from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_archive_manifest as m


@pytest.mark.parametrize('mode',['valid','snapshot','digest','archive','entry','ordinal','noncanonical','inactive'])
def test_real_archive_manifest_binds_one_encoded_entry_and_volume(mode):
    snapshot,artifact=uuid4(),uuid4();target=NS(selection_snapshot_id=snapshot);selection=NS(snapshot_digest='a'*64)
    encoded=NS(artifact_id=artifact,object=NS(sha256='b'*64,size=1024),entry_name='output.jpg',volume_no=1)
    value={'schema_version':1,'snapshot_id':str(uuid4() if mode=='snapshot' else snapshot),
        'snapshot_digest':'f'*64 if mode=='digest' else 'a'*64,'volume_no':1,
        'archive_sha256':'f'*64 if mode=='archive' else 'c'*64,'archive_bytes':2048,
        'entries':[{'ordinal':1 if mode=='ordinal' else 0,'entry_name':'other.jpg' if mode=='entry' else 'output.jpg',
            'artifact':{'artifact_id':str(artifact),'sha256':'b'*64},'bytes':1024,'sha256':'b'*64}]}
    raw=canonical_bytes(value)
    if mode=='noncanonical':raw=b' '+raw
    volume=NS(delivery_kind='zip',object=NS(sha256='c'*64,size=2048),manifest_object=NS(sha256=sha256(raw).hexdigest(),size=len(raw)))
    def invoke():return m.validate_fixture_archive_manifest(raw,target,selection,encoded,volume,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke().entries[0].artifact.artifact_id==artifact
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()


def test_archive_manifest_physical_refuses_inactive_before_catalogue(monkeypatch):
    def unexpected(*args,**kwargs):raise AssertionError('catalogue must not be read')
    monkeypatch.setattr(m,'read_fixture_export_volume',unexpected)
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        m.verify_fixture_archive_manifest(None,None,[],NS(tenant_id=uuid4()),None,None,
            expected_database='restore',check_active=lambda:False)
