from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain,MaskRef
from photo_worker.operations.optical.contracts import OpticalLensParams,ImageResult,ImageReport
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence,AdoptionInvalid
from photo_worker.v2.lens_bypass import verify_lens_identity


def fixture(strength=0,nonzero=False):
    class Reader:
        def __init__(self):self.rows={};self.payloads={}
        def add(self,value,source='output',role=None):
            binary=isinstance(value,bytes);raw=value if binary else canonical_bytes(value)
            if not binary and source=='output':role={'ImageResult':'document','ImageReport':'report','MaskRef':'mask'}[type(value).__name__]
            ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
            self.rows[ref]=ArtifactEvidence(ref,'application/x-npy' if binary else 'application/json',len(raw),role,source)
            self.payloads[ref]=raw
            return ref
        def evidence(self,ref):return self.rows[ref]
        def staged_outputs(self):return {ref for ref,evidence in self.rows.items() if evidence.source=='output'}
        def read_json(self,ref,limit):return self.payloads[ref]
        def iter_bytes(self,ref,limit):
            payload=self.payloads[ref]
            for offset in range(0,len(payload),65536):
                yield payload[offset:offset+65536]
    r=Reader();fake={'artifact_id':uuid4(),'sha256':'a'*64}
    image=WorkingImage(pixels=fake,alpha=None,dtype='float32',canvas=Canvas(width=3,height=2,orientation=1,space_id='source',source_sha256='b'*64),color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    source=r.add(image,'input');binding=VersionBinding(asset_version_id=uuid4(),source=source.__dict__,canvas_hash=canonical_hash(image.canvas))
    params=r.add(OpticalLensParams(schema_version=1,base=binding,strength=strength,profile=fake,focal_mm=50,aperture=2),'input')
    values=np.zeros((2,3),np.float32)
    if nonzero:values[0,0]=1
    output=BytesIO();np.save(output,values,allow_pickle=False)
    pixels=r.add(output.getvalue(),role='mask_pixels')
    mask=r.add(MaskRef(artifact=pixels.__dict__,canvas=image.canvas,transform_chain=image.transform_chain,object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False))
    report=r.add(ImageReport(schema_version=1,status='pass',operation='optical.lens',skipped_count=0,reason=None,outside_mask_max_error=0.,clipping_fraction=0.,identity=None,transform=None,sources=[binding],registrations=[]))
    root=r.add(ImageResult(schema_version=1,working_image=source.__dict__,report=report.__dict__,changed_mask=mask.__dict__,generated_mask=None))
    return r,params,root,source


def test_identity_uses_exact_zero_mask_bytes_and_bound_params():
    r,params,root,source=fixture()
    assert verify_lens_identity(r,params,root)==source
    r,params,root,source=fixture(strength=.5)
    with pytest.raises(AdoptionInvalid,match='NOT_IDENTITY'):verify_lens_identity(r,params,root)
    r,params,root,source=fixture(nonzero=True)
    with pytest.raises(AdoptionInvalid,match='MASK_NONZERO'):verify_lens_identity(r,params,root)
    r,params,root,source=fixture()
    result=ImageResult.model_validate_json(r.payloads[root])
    mask=MaskRef.model_validate_json(r.payloads[ArtifactRef(result.changed_mask.artifact_id,result.changed_mask.sha256)])
    pixels=ArtifactRef(mask.artifact.artifact_id,mask.artifact.sha256)
    tampered=bytearray(r.payloads[pixels]);tampered[-1]=1;r.payloads[pixels]=bytes(tampered)
    with pytest.raises(AdoptionInvalid,match='MASK_NONZERO'):verify_lens_identity(r,params,root)
    r,params,root,source=fixture();r.payloads[params]=b'{}'
    with pytest.raises(AdoptionInvalid,match='BYTES_INVALID'):verify_lens_identity(r,params,root)


def test_identity_adoption_requires_authoritative_params():
    from photo_worker.v2.adoption import prepare_v2_adoption
    r,params,root,source=fixture()
    kwargs=dict(task_id=uuid4(),operation='optical.lens',version='1',root=root,reader=r)
    with pytest.raises(AdoptionInvalid,match='LENS_ADOPTION_PARAMS_REQUIRED'):
        prepare_v2_adoption(**kwargs)
    result=prepare_v2_adoption(**kwargs,params_ref=params)
    assert source.artifact_id in {__import__('uuid').UUID(row['artifact_id']) for row in result.inventory}
    r,params,root,source=fixture(nonzero=True)
    with pytest.raises(AdoptionInvalid,match='MASK_NONZERO'):
        prepare_v2_adoption(task_id=uuid4(),operation='optical.lens',version='1',root=root,reader=r,params_ref=params)


def replace_document(reader,ref,model,change,source='output'):
    import json
    value=json.loads(reader.payloads[ref])
    change(value)
    replacement=reader.add(model.model_validate_json(json.dumps(value,default=str)),source)
    return replacement


@pytest.mark.parametrize('field,value',[
    ('status','review'),('reason','uncovered_area'),('operation','optical.panorama'),
    ('skipped_count',1),('outside_mask_max_error',.01),('clipping_fraction',.01),
])
def test_rehashed_nonidentity_report_is_rejected(field,value):
    import json
    r,params,root,source=fixture()
    result=json.loads(r.payloads[root]);report=ArtifactRef.parse(result['report'])
    changed=replace_document(r,report,ImageReport,lambda d:d.update({field:value}))
    root=replace_document(r,root,ImageResult,lambda d:d.update(report=changed.__dict__))
    with pytest.raises(AdoptionInvalid,match='REPORT_INVALID'):
        verify_lens_identity(r,params,root)


def test_rehashed_mask_geometry_and_substituted_source_are_rejected():
    import json
    r,params,root,source=fixture()
    result=json.loads(r.payloads[root]);mask=ArtifactRef.parse(result['changed_mask'])
    def other_canvas(value):value['canvas']['space_id']='other'
    changed=replace_document(r,mask,MaskRef,other_canvas)
    root=replace_document(r,root,ImageResult,lambda d:d.update(changed_mask=changed.__dict__))
    with pytest.raises(AdoptionInvalid,match='MASK_INVALID'):verify_lens_identity(r,params,root)
    r,params,root,source=fixture()
    other=replace_document(r,source,WorkingImage,lambda d:d['canvas'].update(space_id='other'),'input')
    root=replace_document(r,root,ImageResult,lambda d:d.update(working_image=other.__dict__))
    with pytest.raises(AdoptionInvalid,match='NOT_IDENTITY'):verify_lens_identity(r,params,root)


def test_large_canvas_is_rejected_before_zero_hash_loop():
    import json
    r,params,root,source=fixture()
    changed=replace_document(r,source,WorkingImage,lambda d:d['canvas'].update(width=100000,height=100000),'input')
    new_image=WorkingImage.model_validate_json(r.payloads[changed])
    params=replace_document(r,params,OpticalLensParams,
        lambda d:d['base'].update(source=changed.__dict__,canvas_hash=canonical_hash(new_image.canvas)),'input')
    root=replace_document(r,root,ImageResult,lambda d:d.update(working_image=changed.__dict__))
    with pytest.raises(AdoptionInvalid,match='PIXEL_LIMIT'):verify_lens_identity(r,params,root)


def test_hash_cancellation_is_observed():
    r,params,root,source=fixture()
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        verify_lens_identity(r,params,root,check_active=cancelled)
