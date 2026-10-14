from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2.lens_bypass import _ref
from photo_worker.v2.portrait_donor_adoption import verify_donor_result_metadata
from test_portrait_open_eyes_stage import stage_case


@pytest.mark.parametrize('mode',['ok','pass','alpha','source','generated','role'])
def test_donor_adoption_requires_review_and_exact_source_metadata(mode):
    params,raw,*_=stage_case();source=WorkingImage.model_validate_json(raw);docs={};roles={}
    def put(value,role,origin='output',ref=None):
        body=canonical_bytes(value)
        ref=ref or ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
        docs[ref.artifact_id]=body;roles[ref.artifact_id]=(role,origin)
        return ref
    put(source,None,'input',params.base.source)
    pixels=ArtifactRef(artifact_id=uuid4(),sha256='a'*64);roles[pixels.artifact_id]=('pixels','output')
    maskpixels=ArtifactRef(artifact_id=uuid4(),sha256='b'*64);roles[maskpixels.artifact_id]=('mask_pixels','output')
    image=source.model_copy(update={'pixels':pixels},deep=True)
    if mode=='alpha':image.alpha=pixels
    image_ref=put(image,'working_image')
    mask=put(MaskRef(artifact=maskpixels,canvas=source.canvas,transform_chain=source.transform_chain,
        object_id=None,selector='changed',confidence=1,model_lock_id=None,protected=False),'mask')
    report=put(ImageReport(schema_version=1,status='pass' if mode=='pass' else 'review',operation='portrait.open_eyes',
        skipped_count=0,reason='donor_review',outside_mask_max_error=0,clipping_fraction=0,
        identity=None,transform=None,sources=[params.base] if mode=='source' else [params.base,params.donor],registrations=[]),'report')
    root=put(ImageResult(schema_version=1,working_image=image_ref,report=report,changed_mask=mask,
        generated_mask=mask if mode=='generated' else None),'document')
    pin=put(params,None,'input')
    if mode=='role':roles[pixels.artifact_id]=('alpha','output')
    class Reader:
        def evidence(self,ref):
            role,origin=roles[ref.artifact_id]
            return ArtifactEvidence(ref,'application/json',len(docs.get(ref.artifact_id,b'x')),role,origin)
        def read_json(self,ref,maximum):return docs[ref.artifact_id]
    if mode=='ok':assert verify_donor_result_metadata(Reader(),_ref(pin),_ref(root),check_active=lambda:None)[0]==params
    else:
        with pytest.raises(ValueError):verify_donor_result_metadata(Reader(),_ref(pin),_ref(root),check_active=lambda:None)
