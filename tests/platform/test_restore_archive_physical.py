from contextlib import contextmanager
from io import BytesIO
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4
import json
import zipfile
import pytest
from photo_worker.runtime import scratch as scratch_module
from photo_worker.runtime.scratch import DiskQuota
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.archive import ArchiveManifest,ArchiveManifestEntry
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_archive_physical as m
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('mode',['valid','entry','extra','internal_manifest','hash','changed','scratch','inactive'])
def test_real_zip_production_entry_verifier_and_cleanup(tmp_path,monkeypatch,mode):
    body=b'encoded-image';ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    entry=ArchiveManifestEntry(ordinal=0,entry_name='output.jpg',artifact=ref,bytes=len(body),sha256=ref.sha256)
    snapshot=uuid4();provisional={'schema_version':1,'snapshot_id':str(snapshot),'snapshot_digest':'a'*64,'volume_no':1,
        'entries':[entry.model_dump(mode='json')]}
    payload=json.dumps(provisional,ensure_ascii=False,sort_keys=True,separators=(',',':')).encode()
    output=BytesIO()
    with zipfile.ZipFile(output,'w') as archive:
        archive.writestr('output.jpg',b'wrong' if mode=='entry' else body,compress_type=zipfile.ZIP_STORED)
        archive.writestr('manifest.json',b'{}' if mode=='internal_manifest' else payload)
        if mode=='extra':archive.writestr('extra','unexpected')
    raw=output.getvalue();digest=sha256(raw).hexdigest()
    obj=RestoreObject('photos','tenants/fixture/archive','v1','f'*64 if mode=='hash' else digest,len(raw))
    volume=NS(object=obj,manifest_object=NS(sha256='b'*64))
    manifest=ArchiveManifest(snapshot_id=snapshot,snapshot_digest='a'*64,volume_no=1,entries=[entry],archive_sha256=digest,archive_bytes=len(raw))
    calls=[]
    def observe_manifest(*a,**k):calls.append('manifest');return volume,manifest
    monkeypatch.setattr(m,'verify_fixture_archive_manifest',observe_manifest)
    monkeypatch.setattr(m,'read_fixture_export_volume',lambda *a,**k:NS(changed=True) if mode=='changed' else volume)
    if mode=='scratch':
        monkeypatch.setattr(scratch_module.os,'fstatvfs',lambda _:NS(f_bavail=0,f_frsize=4096))
    class Store:
        def __init__(self,*a,**k):pass
        @contextmanager
        def open_version(self,obj):
            assert mode!='scratch'
            yield BytesIO(raw)
    monkeypatch.setattr(m,'MinioRestoreObjects',Store)
    if mode=='inactive':
        original_resolve=type(tmp_path).resolve
        def resolve(path,*args,**kwargs):
            if path==tmp_path:raise AssertionError('scratch root read before active guard')
            return original_resolve(path,*args,**kwargs)
        monkeypatch.setattr(type(tmp_path),'resolve',resolve)
    def invoke():return m.verify_fixture_archive(None,None,['photos'],NS(tenant_id=uuid4()),None,NS(object=NS(sha256=ref.sha256)),
        expected_database='restore',scratch_root=tmp_path,check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke().bytes_verified==len(raw)
    else:
        with pytest.raises((ValueError,RuntimeError,OperationFailure,DiskQuota)):invoke()
    if mode=='inactive':assert not calls
    assert not list(tmp_path.iterdir())
