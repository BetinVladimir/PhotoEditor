"""Actual local processing; authority and external staging are test doubles."""
from hashlib import sha256
import io
import json
import pytest
from uuid import uuid4
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef,ColorSpace
from photo_worker.contracts.manual_mask import ManualMask,MaskRasterParams,RasterizedMask,Polygon,MaskPoint
from photo_worker.v2.adoption import ArtifactRef as EvidenceRef,ArtifactEvidence
from photo_worker.v2.portrait_open_eyes_manual_stage import prepare_and_stage_manual_open_eyes
from photo_worker.runtime.scratch import ScratchSpace
from test_portrait_open_eyes_stage import stage_case
from test_portrait_local_result import Artifacts
from photo_worker.operations.portrait.donor_resources import donor_portrait_resources
from test_portrait_donor_runtime_lock import current_runtime


@pytest.mark.parametrize('donor_value',[1.,2.])
@pytest.mark.parametrize('protected_center',[False,True])
@pytest.mark.parametrize('encoding',['linear','gamma24'])
@pytest.mark.parametrize('has_alpha',[False,True])
def test_physical_manual_masks_rgb_to_typed_result_without_processing_stub(tmp_path,donor_value,protected_center,encoding,has_alpha):
    params,raw,rgb_reader,_,proof,faces=stage_case()
    source=WorkingImage.model_validate_json(raw);bodies={};masks=[];mask_raw=[]
    ref=lambda body:ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    if encoding=='gamma24':
        source.color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',
                                icc_sha256=source.color.icc_sha256)
    if has_alpha:
        stream=io.BytesIO();np.save(stream,np.full((100,100),.5,np.float32))
        body=stream.getvalue();source.alpha=ref(body)
        bodies[EvidenceRef(source.alpha.artifact_id,source.alpha.sha256)]=body
    raw=canonical_bytes(source);params.base.source.sha256=sha256(raw).hexdigest()
    stream=io.BytesIO();np.save(stream,np.full((100,100,3),donor_value,np.float32))
    donor_body=stream.getvalue();donor_pixels=ref(donor_body)
    bodies[EvidenceRef(donor_pixels.artifact_id,donor_pixels.sha256)]=donor_body
    donor_raw=canonical_bytes(source.model_copy(update={'pixels':donor_pixels}))
    params.donor.source.sha256=sha256(donor_raw).hexdigest()
    confirmation=json.loads(proof);confirmation['source']=params.base.model_dump(mode='json')
    confirmation['donor']=params.donor.model_dump(mode='json')
    proof=canonical_bytes(confirmation);params.confirmation.sha256=sha256(proof).hexdigest()
    donor_faces=json.loads(faces[1]);donor_faces['binding']=params.donor.model_dump(mode='json')
    faces[1]=canonical_bytes(donor_faces);params.donor_faces.sha256=sha256(faces[1]).hexdigest()
    source_faces=json.loads(faces[0]);source_faces['binding']=params.base.model_dump(mode='json')
    faces[0]=canonical_bytes(source_faces);params.source_faces.sha256=sha256(faces[0]).hexdigest()
    for protected,pin in ((False,params.eyes_mask),(True,params.protected)):
        array=np.full((100,100),0 if protected else 1,np.float32)
        if protected and protected_center:array[40:60,40:60]=1
        stream=io.BytesIO();np.save(stream,array)
        body=stream.getvalue();pixels=ref(body)
        bodies[EvidenceRef(pixels.artifact_id,pixels.sha256)]=body
        mask=MaskRef(artifact=pixels,canvas=source.canvas,transform_chain=source.transform_chain,
            selector='manual',confidence=1.,object_id=None,model_lock_id=None,protected=protected)
        doc=canonical_bytes(mask);pin.sha256=sha256(doc).hexdigest()
        masks.append(mask);mask_raw.append(doc)
    vectors=ManualMask(canvas=source.canvas,source_sha256=source.canvas.source_sha256,
        shapes=[Polygon(kind='polygon',sequence=0,mode='add',points=[MaskPoint(x=x,y=y)
            for x,y in [(0.,0.),(1.,0.),(1.,1.),(0.,1.)]])])
    if protected_center:
        vectors.shapes.append(Polygon(kind='polygon',sequence=1,mode='protect',
            points=[MaskPoint(x=x,y=y) for x,y in [(.4,.4),(.6,.4),(.6,.6),(.4,.6)]]))
    producer=canonical_bytes(MaskRasterParams(image=params.base.source,inline_vectors=vectors))
    producer_ref=ref(producer)
    root=canonical_bytes(RasterizedMask(image=params.base.source,vectors=producer_ref,
        target=masks[0],protected=masks[1],target_png=ref(b't'),protected_png=ref(b'p')))
    class Reader:
        def evidence(self,pin):
            if pin in bodies:return ArtifactEvidence(pin,'application/x-npy',len(bodies[pin]),None,'input')
            return rgb_reader.evidence(pin)
        def iter_bytes(self,pin,max_bytes):
            if pin not in bodies:
                yield from rgb_reader.iter_bytes(pin,max_bytes);return
            body=bodies[pin]
            for i in range(0,len(body),65536):yield body[i:i+65536]
    class Stager(Artifacts):
        def stage(self,path,reservation,role,mime):
            if role=='pixels':
                expected=np.full((100,100,3),donor_value,np.float32)
                if protected_center:expected[40:60,40:60]=1
                np.testing.assert_allclose(np.load(path,allow_pickle=False),expected,rtol=1e-6)
            if role=='mask_pixels':
                expected=np.full((100,100),float(donor_value!=1),np.float32)
                if protected_center:expected[40:60,40:60]=0
                np.testing.assert_array_equal(np.load(path,allow_pickle=False),expected)
            return super().stage(path,reservation,role,mime)
    output=tmp_path/'output';output.mkdir();mask_dir=tmp_path/'masks';mask_dir.mkdir()
    material=tmp_path/'material';material.mkdir();artifacts=Stager()
    lock=current_runtime();runtime_sha=canonical_hash(lock)
    with ScratchSpace(output,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        result=prepare_and_stage_manual_open_eyes(params,raw,donor_raw,proof,*faces,*mask_raw,
            ref(root),root,producer_ref,producer,Reader(),artifacts,scratch,
            source_origin=(0,0),patch_size=(100,100),min_face_edge_px=40,
            mask_scratch_root=mask_dir,mask_max_bytes=1_000_000,
            material_scratch_root=material,material_max_bytes=1_000_000,
            check_active=lambda:None,authority=lambda p,r:True,
            producer_authority=lambda p,r,q:True,alpha_mode='require_float32',
            profile={'backend':'cpu','renderer_version':'v2.2',
                'resource_estimate':donor_portrait_resources(100,100,100,100,100,100),
                'portrait_runtime_sha':runtime_sha},runtime_lock=lock,expected_runtime_sha=runtime_sha)
    assert len(result.outputs)==6 and result.review_required
    assert artifacts.roles['report'][1].sources==[params.base,params.donor]
    image=artifacts.roles['working_image'][1]
    assert image.alpha==source.alpha and image.color==source.color
    assert not list(mask_dir.iterdir()) and not list(material.iterdir())
