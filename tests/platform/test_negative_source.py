from types import SimpleNamespace
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.negative_source import negative_source_authority
from test_negative_result import source_case


def test_negative_source_owns_lease_and_rejects_stale_or_changed_original():
    p,image,source=source_case();body=canonical_bytes(p);calls=[];ready=[True]
    lease=SimpleNamespace(command=SimpleNamespace(operation='optical.negative',operation_version='1',
        params=SimpleNamespace(sha256=sha256(body).hexdigest())))
    def live(owned,raw):
        assert owned.command.operation=='optical.negative' and raw==body
        owned.command.operation='mutated';calls.append('sql');return ready[0]
    def color(value):
        assert value==image;value.color.icc_sha256='a'*64;calls.append('color');return True
    authority=negative_source_authority(SimpleNamespace(negative_source_ready=live),lease,body,
        verify_color=color,check_active=lambda:None)
    lease.command.operation='mutated'
    assert authority(p,source) and authority(p,source)
    assert calls==['sql','color']*2
    with pytest.raises(ValueError,match='NEGATIVE_SOURCE_CHANGED'):authority(p,source+b' ')
    ready[0]=False
    with pytest.raises(ValueError,match='NEGATIVE_SOURCE_STALE'):authority(p,source)
    assert calls[-1]=='sql'


@pytest.mark.parametrize('mode',['ok','corrupt','alpha','revoked','ram'])
def test_negative_source_original_arrays_and_cleanup(tmp_path,mode):
    from io import BytesIO
    import numpy as np
    from photo_worker.v2.adoption import ArtifactEvidence
    from photo_worker.v2.negative_source import load_negative_source_arrays
    p,image,_=source_case();bodies={};reads=[];ready=[True]
    scan=np.ones((image.canvas.height,image.canvas.width,3),np.float32)
    for ref,value in [(image.pixels,scan),(image.alpha,np.full(scan.shape[:2],2 if mode=='alpha' else 1,np.float32))]:
        stream=BytesIO();np.save(stream,value);body=stream.getvalue()
        ref.sha256=sha256(body).hexdigest();bodies[ref.artifact_id]=body
    source=canonical_bytes(image);p['base']['source']['sha256']=sha256(source).hexdigest();body=canonical_bytes(p)
    lease=SimpleNamespace(command=SimpleNamespace(operation='optical.negative',operation_version='1',
        params=SimpleNamespace(sha256=sha256(body).hexdigest())))
    class Reader:
        def evidence(self,ref):return ArtifactEvidence(ref,'application/x-npy',len(bodies[ref.artifact_id]),None,'input')
        def iter_bytes(self,ref,maximum):
            reads.append(ref.artifact_id);raw=bodies[ref.artifact_id]
            if mode=='corrupt':raw=raw[:-1]+bytes([raw[-1]^1])
            for offset in range(0,len(raw),65536):yield raw[offset:offset+65536]
    def run():
        with load_negative_source_arrays(Reader(),SimpleNamespace(negative_source_ready=lambda *args:ready[0]),
                lease,body,source,scratch_root=tmp_path,max_bytes=1<<20,
                ram_max_bytes=1 if mode=='ram' else 1<<30,verify_color=lambda image:True,
                check_active=lambda:None) as mapped:
            np.testing.assert_array_equal(mapped,scan)
            assert not mapped.flags.writeable
            if mode=='revoked':ready[0]=False
    if mode=='ok':run()
    else:
        errors={'corrupt':'ARRAY_BYTES_INVALID','alpha':'ARRAY_RANGE_INVALID|ARRAY.*INVALID','revoked':'NEGATIVE_SOURCE_STALE','ram':'NEGATIVE_RAM_EXCEEDED'}
        with pytest.raises(ValueError,match=errors[mode]):run()
    assert not list(tmp_path.iterdir())
    if mode=='ram':assert not reads
    if mode=='corrupt':assert reads==[image.pixels.artifact_id]
