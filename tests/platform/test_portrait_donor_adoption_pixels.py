import numpy as np
import pytest
from photo_worker.v2.portrait_donor_adoption import verify_donor_pixel_tiles


@pytest.mark.parametrize('mode',['ok','outside','protected','changed','zero','signed_zero','nan'])
def test_donor_physical_pixel_invariants_across_tile_boundary(mode):
    source=np.zeros((513,2,3),np.float32);output=source.copy()
    mask=np.zeros((513,2),np.float32);guard=mask.copy();changed=mask.copy()
    mask[512,1]=1;output[512,1]=0.25;changed[512,1]=1
    strength=1
    if mode=='outside':mask[512,1]=0
    elif mode=='protected':guard[512,1]=np.nextafter(np.float32(0),np.float32(1))
    elif mode=='changed':changed[512,1]=0.5
    elif mode=='zero':strength=0
    elif mode=='signed_zero':output[0,0,0]=-0.0;changed[0,0]=1
    elif mode=='nan':output[512,1,0]=np.nan
    calls=[]
    def run():verify_donor_pixel_tiles(source,output,mask,guard,changed,strength=strength,check_active=lambda:calls.append(1))
    if mode=='ok':run();assert len(calls)==3
    else:
        with pytest.raises(ValueError):run()


def test_donor_pixel_verification_stops_when_authority_is_revoked():
    source=np.zeros((513,2,3),np.float32);mask=np.zeros((513,2),np.float32);calls=[]
    def check():
        calls.append(1)
        if len(calls)==2:raise RuntimeError('revoked')
    with pytest.raises(RuntimeError,match='revoked'):
        verify_donor_pixel_tiles(source,source,mask,mask,mask,strength=0,check_active=check)


def test_donor_invariants_on_hash_verified_disk_mappings(tmp_path):
    from hashlib import sha256
    from io import BytesIO
    from uuid import uuid4
    from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence
    from photo_worker.v2.array_evidence import ArrayEvidenceSession
    source=np.zeros((3,4,3),np.float32);output=source.copy();output[1,2]=1
    target=np.zeros((3,4),np.float32);target[1,2]=1
    guard=np.zeros_like(target);changed=target.copy();bodies={};metadata={}
    refs=[]
    for value,origin,role in [(source,'input',None),(output,'output','pixels'),(target,'input',None),(guard,'input',None),(changed,'output','mask_pixels')]:
        stream=BytesIO();np.save(stream,value);raw=stream.getvalue()
        ref=ArtifactRef(uuid4(),sha256(raw).hexdigest());refs.append(ref)
        bodies[ref]=raw;metadata[ref]=ArtifactEvidence(ref,'application/x-npy',len(raw),role,origin)
    class Reader:
        def evidence(self,ref):return metadata[ref]
        def iter_bytes(self,ref,maximum):yield bodies[ref]
    with ArrayEvidenceSession(Reader(),tmp_path,10000,lambda:None) as arrays:
        values=[arrays.array(ref,value.shape,source=metadata[ref].source,role=metadata[ref].role,unit=value.ndim==2)
                for ref,value in zip(refs,(source,output,target,guard,changed))]
        verify_donor_pixel_tiles(*values,strength=1,check_active=lambda:None)
    assert not list(tmp_path.iterdir())
