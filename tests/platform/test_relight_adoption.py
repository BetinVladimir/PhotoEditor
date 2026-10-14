from hashlib import sha256
from uuid import uuid4
import pytest
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2.lens_bypass import _ref
from photo_worker.v2.relight_adoption import verify_relight_result_metadata,verify_relight_pixel_tiles
from test_relight_inputs import inputs


@pytest.mark.parametrize('mode',['ok','pass','alpha','source','reason','generated','role'])
def test_relight_adoption_exact_metadata_and_review(mode):
    params,source,*_=inputs();docs={};roles={}
    def put(value,role,origin='output',ref=None):
        raw=canonical_bytes(value);ref=ref or ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        docs[ref.artifact_id]=raw;roles[ref.artifact_id]=(role,origin);return ref
    put(source,None,'input',params.base.source)
    pixels=ArtifactRef(artifact_id=uuid4(),sha256='a'*64);roles[pixels.artifact_id]=('pixels','output')
    maskpixels=ArtifactRef(artifact_id=uuid4(),sha256='b'*64);roles[maskpixels.artifact_id]=('mask_pixels','output')
    image=source.model_copy(update={'pixels':pixels},deep=True)
    if mode=='alpha':image.alpha=pixels
    image_ref=put(image,'working_image')
    mask=put(MaskRef(artifact=maskpixels,canvas=source.canvas,transform_chain=source.transform_chain,object_id=None,selector='changed',confidence=1,model_lock_id=None,protected=False),'mask')
    report=put(ImageReport(schema_version=1,status='pass' if mode=='pass' else 'review',operation='image.relight',skipped_count=0,reason='other' if mode=='reason' else 'relative_depth_exposure',outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[params.base.model_copy(update={'asset_version_id':uuid4()})] if mode=='source' else [params.base],registrations=[]),'report')
    root=put(ImageResult(schema_version=1,working_image=image_ref,report=report,changed_mask=mask,generated_mask=mask if mode=='generated' else None),'document')
    pin=put(params,None,'input')
    if mode=='role':roles[pixels.artifact_id]=('alpha','output')
    class Reader:
        def evidence(self,ref):
            role,origin=roles[ref.artifact_id];return ArtifactEvidence(ref,'application/json',len(docs.get(ref.artifact_id,b'x')),role,origin)
        def read_json(self,ref,maximum):return docs[ref.artifact_id]
    if mode=='ok':assert verify_relight_result_metadata(Reader(),_ref(pin),_ref(root),check_active=lambda:None)[0]==params
    else:
        with pytest.raises(ValueError):verify_relight_result_metadata(Reader(),_ref(pin),_ref(root),check_active=lambda:None)


@pytest.mark.parametrize('mode',['ok','guard','invalid','subject','zero','changed','nonbinary','nan'])
def test_relight_adoption_pixel_invariants(mode):
    before=np.full((270,520,3),.4,np.float32);after=before.copy()
    subject=np.ones((270,520),np.float32);guard=np.zeros_like(subject);invalid=np.zeros_like(subject)
    guard[:,0]=np.nextafter(np.float32(0),np.float32(1));subject[:,1]=0;invalid[:,2]=1
    after[:,3:]*=1.1;strength=1.
    if mode in ('guard','invalid','subject'):after[0,{'guard':0,'invalid':2,'subject':1}[mode]]=0
    if mode=='zero':strength=0.
    if mode=='nonbinary':invalid[0,0]=.5
    if mode=='nan':after[0,3]=np.nan
    changed=np.any(before!=after,axis=-1).astype(np.float32)
    if mode=='changed':changed[-1,-1]=0
    if mode=='ok':verify_relight_pixel_tiles(before,after,subject,guard,changed,invalid,strength=strength,check_active=lambda:None)
    else:
        with pytest.raises(ValueError):verify_relight_pixel_tiles(before,after,subject,guard,changed,invalid,strength=strength,check_active=lambda:None)
