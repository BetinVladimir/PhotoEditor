from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactEvidence,ArtifactRef as EvidenceRef
from photo_worker.v2.shown_background_cleanup_pixels import verify_shown_background_cleanup_pixels
from test_v27_background_cleanup_preview_adopt_execute import copy_documents_fixture


@pytest.mark.parametrize('mode',[None,'hdr','corrupt_output','negative','nan','wrong_shape','scratch','revoked','storage','evidence_revoked','eof_revoked'])
def test_cleanup_physical_copy_proof_reads_both_pinned_npy(tmp_path,mode):
    params,documents,path=copy_documents_fixture(tmp_path)
    shown=WorkingImage.model_validate_json(documents[str(params.working_image.artifact_id)])
    shape=(shown.canvas.height,shown.canvas.width,3)
    values=np.full(shape,2.5 if mode=='hdr' else .5,dtype=np.float32)
    if mode=='negative':values[0,0,0]=-.01
    if mode=='nan':values[0,0,0]=np.nan
    if mode=='wrong_shape':values=values[:-1]
    np.save(path,values,allow_pickle=False)
    raw=path.read_bytes();digest=sha256(raw).hexdigest()
    shown=shown.model_copy(update={'pixels':ArtifactRef(artifact_id=uuid4(),sha256=digest)})
    output=shown.model_copy(update={'pixels':ArtifactRef(artifact_id=uuid4(),sha256=digest)})
    original=EvidenceRef(shown.pixels.artifact_id,digest)
    copied=EvidenceRef(output.pixels.artifact_id,digest)
    active=True;reads=[];closed=[]
    class Reader:
        def evidence(self,ref):
            nonlocal active
            assert active
            if mode=='evidence_revoked':active=False
            return ArtifactEvidence(ref,'application/x-npy',len(raw),'pixels' if ref==copied else None,
                'output' if ref==copied else 'input')
        def iter_bytes(self,ref,max_bytes):
            nonlocal active
            assert active and max_bytes==len(raw)
            reads.append(ref)
            try:
                if mode=='storage':raise OSError('temporary object-store read failure')
                data=raw[:-1]+bytes([raw[-1]^1]) if mode=='corrupt_output' and ref==copied else raw
                for start in range(0,len(data),65536):
                    if mode=='revoked':active=False
                    yield data[start:start+65536]
            finally:
                closed.append(ref)
                if mode=='eof_revoked':active=False
    def run():
        return verify_shown_background_cleanup_pixels(Reader(),shown,output,scratch_root=tmp_path,
            max_bytes=1 if mode=='scratch' else 2*len(raw),check_active=lambda:active)
    if mode in (None,'hdr'):
        assert run() is None
        assert reads==closed==[original,copied]
    elif mode=='storage':
        with pytest.raises(OSError,match='temporary object-store'):run()
        assert reads==closed==[original]
    else:
        with pytest.raises(AdoptionInvalid):run()
        assert reads==closed
        if mode in ('negative','nan','wrong_shape','revoked','eof_revoked'):assert reads==[original]
        if mode in ('scratch','evidence_revoked'):assert reads==[]
    assert not list(tmp_path.glob('adoption-*'))
