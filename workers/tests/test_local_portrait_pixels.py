"""Physical tile invariants; authored only, not photo-quality evidence."""
import numpy as np
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.local_portrait_pixels import verify_local_portrait_tile


def arrays():
    before=np.ones((2,2,3),dtype=np.float32)
    return before,before.copy(),np.ones((2,2),dtype=np.float32),np.zeros((2,2),dtype=np.float32),np.zeros((2,2),dtype=np.float32)


def test_selected_unprotected_pixel_change_is_allowed():
    before,after,target,guard,changed=arrays()
    after[0,0]=2;changed[0,0]=1
    verify_local_portrait_tile(before,after,target,guard,changed,.5)


@pytest.mark.parametrize('mode',['outside','protected','zero_strength'])
def test_disabled_pixel_changes_are_refused(mode):
    before,after,target,guard,changed=arrays()
    after[0,0]=2;changed[0,0]=1
    if mode=='outside': target[0,0]=0
    if mode=='protected': guard[0,0]=np.float32(.001)
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_PROTECTED_PIXELS_CHANGED'):
        verify_local_portrait_tile(before,after,target,guard,changed,0 if mode=='zero_strength' else .5)


def test_changed_mask_cannot_hide_or_invent_changes():
    before,after,target,guard,changed=arrays()
    changed[0,0]=1
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_CHANGED_MASK_INVALID'):
        verify_local_portrait_tile(before,after,target,guard,changed,.5)
    changed[:]=0;after[0,0]=2
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_CHANGED_MASK_INVALID'):
        verify_local_portrait_tile(before,after,target,guard,changed,.5)


def test_protected_signed_zero_must_remain_bit_exact():
    before,after,target,guard,changed=arrays()
    before[0,0,0]=np.float32(-0.0)
    after[0,0,0]=np.float32(0.0)
    guard[0,0]=1
    # Numeric comparison alone considers these equal. The original bit pattern
    # still belongs to the photographer and cannot change in a protected pixel.
    assert before[0,0,0] == after[0,0,0]
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_PROTECTED_PIXELS_CHANGED'):
        verify_local_portrait_tile(before,after,target,guard,changed,.5)


def test_zero_strength_keeps_original_bits_and_empty_change_mask():
    before,after,target,guard,changed=arrays()
    verify_local_portrait_tile(before,after,target,guard,changed,0)


def physical_fixture(*, corrupt=False, protected_change=False):
    from io import BytesIO
    from hashlib import sha256
    from types import SimpleNamespace as NS
    from uuid import uuid4
    from photo_worker.contracts.base import canonical_bytes, canonical_hash
    from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence
    documents, binaries, evidence = {}, {}, {}
    def publish_array(value, source, role=None):
        buffer=BytesIO();np.save(buffer,value,allow_pickle=False);raw=buffer.getvalue()
        ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
        binaries[ref]=raw;evidence[ref]=ArtifactEvidence(ref,'application/x-npy',len(raw),role,source)
        return ref
    canvas=dict(width=2,height=2,orientation=1,space_id='fixture',source_sha256='b'*64)
    chain=dict(steps=[],hash=canonical_hash({'steps':[]}))
    def mask(value, selector, protected, source, role=None):
        pixels=publish_array(value,source,'mask_pixels' if source=='output' else None)
        raw=canonical_bytes(dict(artifact={'artifact_id':str(pixels.artifact_id),'sha256':pixels.sha256},canvas=canvas,transform_chain=chain,object_id=None,selector=selector,confidence=1,model_lock_id=None,protected=protected))
        ref=ArtifactRef(uuid4(),sha256(raw).hexdigest());documents[ref]=raw
        evidence[ref]=ArtifactEvidence(ref,'application/json',len(raw),role,source)
        return ref
    before,after,target,guard,changed=arrays()
    after[0,0]=2;changed[0,0]=1
    if protected_change:guard[0,0]=1
    before_ref=publish_array(before,'input');after_ref=publish_array(after,'output','pixels')
    params=NS(mask=mask(target,'target',False,'input'),protected=mask(guard,'protected',True,'input'),strength=.5)
    root=NS(changed_mask=mask(changed,'changed',False,'output','mask'))
    source=NS(pixels=before_ref,alpha=publish_array(np.ones((2,2),dtype=np.float32),'input'),canvas=NS(height=2,width=2),transform_chain=NS(steps=[]))
    image=NS(pixels=after_ref)
    if corrupt:binaries[after_ref]=binaries[after_ref][:-1]+bytes([binaries[after_ref][-1]^1])
    class Reader:
        def evidence(self,ref):return evidence[ref]
        def read_json(self,ref,maximum):return documents[ref]
        def iter_bytes(self,ref,maximum):
            raw=binaries[ref]
            for offset in range(0,len(raw),17):yield raw[offset:offset+17]
    return Reader(),params,source,root,image


def test_physical_session_reads_hashed_arrays_and_cleans_scratch(tmp_path):
    from photo_worker.v2.local_portrait_pixels import verify_local_portrait_pixels
    verify_local_portrait_pixels(*physical_fixture(),scratch_root=tmp_path,max_bytes=100000,check_active=lambda:True)
    assert list(tmp_path.iterdir())==[]


@pytest.mark.parametrize('mode,error',[('corrupt','ARRAY_BYTES_INVALID'),('protected','LOCAL_PORTRAIT_PROTECTED_PIXELS_CHANGED'),('budget','ADOPTION_SCRATCH_EXCEEDED')])
def test_physical_session_rejects_corruption_protected_edits_and_budget_overrun(tmp_path,mode,error):
    from photo_worker.v2.local_portrait_pixels import verify_local_portrait_pixels
    fixture=physical_fixture(corrupt=mode=='corrupt',protected_change=mode=='protected')
    with pytest.raises(AdoptionInvalid,match=error):
        verify_local_portrait_pixels(*fixture,scratch_root=tmp_path,max_bytes=1 if mode=='budget' else 100000,check_active=lambda:True)
    assert list(tmp_path.iterdir())==[]


def test_physical_session_stops_and_cleans_scratch_when_authority_is_revoked_mid_stream(tmp_path):
    from photo_worker.v2.local_portrait_pixels import verify_local_portrait_pixels
    reader,params,source,root,image=physical_fixture()
    original=reader.iter_bytes
    active=True
    def revoked_stream(ref,maximum):
        nonlocal active
        for block in original(ref,maximum):
            yield block
            active=False
    reader.iter_bytes=revoked_stream
    with pytest.raises(AdoptionInvalid,match='LOCAL_PORTRAIT_INACTIVE'):
        verify_local_portrait_pixels(reader,params,source,root,image,scratch_root=tmp_path,max_bytes=100000,check_active=lambda:active)
    assert list(tmp_path.iterdir())==[]
