from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.operations.optical.contracts import ImageReport,ImageResult
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2.lens_bypass import _ref
from photo_worker.v2.portrait_donor_adoption import verify_donor_output_arrays
from test_portrait_open_eyes_stage import stage_case


@pytest.mark.parametrize('mode',['ok','outside','changed','corrupt','budget','noncanonical'])
def test_full_donor_array_verifier_reads_original_npy_and_cleans_scratch(tmp_path,mode):
    params,raw,*_=stage_case();source=WorkingImage.model_validate_json(raw)
    h,w=source.canvas.height,source.canvas.width;shape=(h,w)
    bodies={};metadata={}
    def put(value,role,origin='output',media='application/json'):
        if isinstance(value,np.ndarray):
            stream=BytesIO();np.save(stream,value);body=stream.getvalue();media='application/x-npy'
        else:body=canonical_bytes(value)
        if mode=='noncanonical' and role=='document':body+=b' '
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
        bodies[ref.artifact_id]=body
        metadata[ref.artifact_id]=(role,origin,media)
        return ref
    before=np.zeros((*shape,3),np.float32);after=before.copy();after[0,0]=0.25
    mask=np.zeros(shape,np.float32);mask[0,0]=1
    guard=np.zeros(shape,np.float32);changes=mask.copy()
    if mode=='outside':mask[0,0]=0
    if mode=='changed':changes[0,0]=0
    source.pixels=put(before,None,'input')
    params.base.source=put(source,None,'input')
    target=MaskRef(artifact=put(mask,None,'input'),canvas=source.canvas,transform_chain=source.transform_chain,
        object_id=None,selector='eyes',confidence=1,model_lock_id=None,protected=False)
    protected=target.model_copy(update={'artifact':put(guard,None,'input'),'protected':True},deep=True)
    params.eyes_mask=put(target,None,'input');params.protected=put(protected,None,'input')
    output=source.model_copy(update={'pixels':put(after,'pixels')},deep=True)
    changed=target.model_copy(update={'artifact':put(changes,'mask_pixels'),'selector':'changed'},deep=True)
    result=ImageResult(schema_version=1,working_image=put(output,'working_image'),
        changed_mask=put(changed,'mask'),generated_mask=None,
        report=put(ImageReport(schema_version=1,status='review',operation='portrait.open_eyes',
            skipped_count=0,reason='donor_review',outside_mask_max_error=0,clipping_fraction=0,
            identity=None,transform=None,sources=[params.base,params.donor],registrations=[]),'report'))
    root=put(result,'document');pin=put(params,None,'input')
    class Reader:
        def evidence(self,ref):
            role,origin,media=metadata[ref.artifact_id]
            return ArtifactEvidence(ref,media,len(bodies[ref.artifact_id]),role,origin)
        def read_json(self,ref,maximum):return bodies[ref.artifact_id]
        def iter_bytes(self,ref,maximum):
            body=bodies[ref.artifact_id]
            if mode=='corrupt' and ref.artifact_id==output.pixels.artifact_id:body=body[:-1]+bytes([body[-1]^1])
            for offset in range(0,len(body),65536):yield body[offset:offset+65536]
    def run():return verify_donor_output_arrays(Reader(),_ref(pin),_ref(root),scratch_root=tmp_path,
        max_bytes=1 if mode=='budget' else 10_000_000,check_active=lambda:None)
    if mode=='ok':assert run()==(params,result)
    else:
        with pytest.raises(ValueError):run()
    assert not list(tmp_path.iterdir())
