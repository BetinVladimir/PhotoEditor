from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef
from photo_worker.v2.negative_object_reader import NegativeWorkerObjectReader


@pytest.mark.parametrize('mode',['ok','revoked','output','version','size'])
def test_worker_reader_uses_current_input_metadata_and_exact_object_version(mode):
    ref=ArtifactRef(uuid4(),'a'*64);calls=[]
    row=dict(bucket='bucket',object_key='key',object_version='version',sha256=ref.sha256,
             bytes=3,media_type='application/x-npy',role='pixels',source='input')
    def evidence(lease,body,pin):
        calls.append('sql');assert body==b'{}' and pin==ref
        if mode=='revoked':return None
        return dict(row,**({'source':'output'} if mode=='output' else {'object_version':'null'} if mode=='version' else {'bytes':True} if mode=='size' else {}))
    class Objects:
        def get_object(self,bucket,key,version_id):
            calls.append('object');assert (bucket,key,version_id)==('bucket','key','version')
            return BytesIO(b'123')
    reader=NegativeWorkerObjectReader(SimpleNamespace(negative_input_evidence=evidence),SimpleNamespace(),b'{}',Objects(),bucket='bucket',check_active=lambda:None)
    if mode=='ok':
        assert b''.join(reader.iter_bytes(ref,3))==b'123'
        assert b''.join(reader.iter_bytes(ref,3))==b'123'
        assert calls==['sql','object','sql','object']
    else:
        with pytest.raises(ValueError):list(reader.iter_bytes(ref,3))
        assert calls==['sql']
    with pytest.raises(ValueError):reader.staged_outputs()
