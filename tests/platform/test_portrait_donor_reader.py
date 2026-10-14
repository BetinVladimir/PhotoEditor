from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence
from photo_worker.v2.portrait_donor_reader import DonorInputReader


@pytest.mark.parametrize('mode',['ok','undeclared','output','corrupt','revoked'])
def test_donor_reader_requires_membership_for_every_block_and_original_hash(mode):
    raw=b'original';ref=ArtifactRef(uuid4(),sha256(raw).hexdigest());calls=[];closed=[]
    class Artifacts:
        def require_declared_input(self,value):
            calls.append('membership')
            assert value.artifact_id==ref.artifact_id and value.sha256==ref.sha256
            if mode=='undeclared' or mode=='revoked' and len(calls)>=3:raise ValueError('revoked')
    class Reader:
        def evidence(self,value):
            return ArtifactEvidence(ref,'application/x-npy',len(raw),'pixels','output' if mode=='output' else 'input')
        def iter_bytes(self,value,maximum):
            try:
                yield b'changed!' if mode=='corrupt' else raw
            finally:closed.append(True)
    reader=DonorInputReader(Artifacts(),Reader(),check_active=lambda:None)
    if mode=='ok':assert b''.join(reader.iter_bytes(ref,len(raw)))==raw
    else:
        with pytest.raises(ValueError):list(reader.iter_bytes(ref,len(raw)))
    if mode in ('ok','corrupt'):assert closed==[True]
