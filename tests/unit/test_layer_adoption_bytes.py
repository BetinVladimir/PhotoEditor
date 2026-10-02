"""Full composition proof with real canonical JSON/NPY SHA and disk mappings."""
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage,MaskRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.operations.layers.contracts import LayerComposeParams
from photo_worker.v2.adoption import ArtifactRef as EvidenceRef,AdoptionInvalid
from photo_worker.v2.layer_adoption import verify_layer_composition


def fixture(*,legacy_alpha=False,alpha_failure=None):
    objects={}
    def put(body,media,scope,role=None):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
        objects[ref.artifact_id]=(body,media,scope,role)
        return ref
    def document(value,scope='input',role=None):return put(canonical_bytes(value),'application/json',scope,role)
    def array(value,scope='input',role=None):
        stream=BytesIO();np.save(stream,value,allow_pickle=False)
        return put(stream.getvalue(),'application/x-npy',scope,role)
    canvas=Canvas(width=2,height=2,orientation=1,space_id='source',source_sha256='a'*64)
    color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='b'*64)
    chain=TransformChain.create([])
    rgb=np.full((2,2,3),[-.2,4.,.8],np.float32)
    image=WorkingImage(pixels=array(np.zeros_like(rgb)),alpha=None,dtype='float32',canvas=canvas,color=color,transform_chain=chain)
    opacity=np.array([[0,1],[32768,65535]],np.uint16)
    normalized=opacity.astype(np.float32)/np.float32(65535)
    if legacy_alpha:image.alpha=array(opacity)
    source=document(image)
    base=VersionBinding(asset_version_id=uuid4(),source=source,canvas_hash=canonical_hash(canvas))
    ones=np.ones((2,2),np.float32)
    def mask(selector,scope='input'):
        return document(MaskRef(artifact=array(ones,scope,'mask_pixels' if scope=='output' else None),canvas=canvas,
            transform_chain=chain,selector=selector,confidence=1.,model_lock_id=None,protected=False),scope,'mask')
    def report(operation,scope):
        return document(ImageReport(schema_version=1,status='pass',operation=operation,skipped_count=0,reason=None,
            outside_mask_max_error=0.,clipping_fraction=0.,identity=None,transform=None,sources=[base],registrations=[],**({'parent_reports':[proposal_report]} if scope=='output' else {})),scope,'report')
    proposal_image=document(image.model_copy(update={'pixels':array(rgb),'alpha':array(normalized) if legacy_alpha else None}))
    proposal_report=report('optical.lens','input')
    proposal=document(ImageResult(schema_version=1,working_image=proposal_image,changed_mask=mask('changed'),
        generated_mask=mask('generated'),report=proposal_report))
    params=document(LayerComposeParams(schema_version=1,base=base,source_result=None,proposal_result=proposal,
        mask={'op':'ref','mask':mask('user').model_dump(mode='json'),'canvas_hash':base.canvas_hash},protected_mask=None,opacity=1.,blend='normal'))
    output_pixels=array(rgb,'output','pixels')
    output_alpha=None
    if legacy_alpha:
        output_alpha=image.alpha if alpha_failure=='retained' else array(
            opacity if alpha_failure=='uint16' else normalized+np.float32(.00001) if alpha_failure=='wrong' else normalized,'output','alpha')
    output_image=document(image.model_copy(update={'pixels':output_pixels,'alpha':output_alpha}),'output','working_image')
    root=document(ImageResult(schema_version=1,working_image=output_image,changed_mask=mask('changed','output'),
        generated_mask=mask('generated','output'),report=report('layer.compose','output')),'output','document')
    class Reader:
        opened=0;closed=0
        def evidence(self,ref):
            body,media,scope,role=objects[ref.artifact_id]
            return SimpleNamespace(ref=ref,media_type=media,source=scope,role=role,bytes=len(body))
        def staged_outputs(self):
            return {EvidenceRef(identifier,sha256(value[0]).hexdigest()) for identifier,value in objects.items() if value[2]=='output'}
        def read_json(self,ref,limit):return objects[ref.artifact_id][0]
        def iter_bytes(self,ref,limit):
            self.opened+=1
            try:
                body=objects[ref.artifact_id][0]
                for offset in range(0,len(body),11):yield body[offset:offset+11]
            finally:self.closed+=1
    return Reader(),EvidenceRef(params.artifact_id,params.sha256),EvidenceRef(root.artifact_id,root.sha256),objects,output_pixels


def test_complete_proof_with_actual_bytes_and_cleanup(tmp_path):
    reader,params,root,objects,pixels=fixture()
    verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000)
    assert reader.opened==reader.closed and reader.opened>0
    assert not list(tmp_path.iterdir())


def test_corrupt_stream_and_budget_fail_before_adoption(tmp_path):
    reader,params,root,objects,pixels=fixture()
    body,media,scope,role=objects[pixels.artifact_id]
    objects[pixels.artifact_id]=(body[:-1]+b'x',media,scope,role)
    with pytest.raises(AdoptionInvalid):verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000)
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())
    with pytest.raises(AdoptionInvalid,match='ADOPTION_SCRATCH_EXCEEDED'):verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1)
    assert not list(tmp_path.iterdir())


def test_valid_hashes_do_not_accept_wrong_composition_pixels(tmp_path):
    import json
    from uuid import UUID
    reader,params,root,objects,pixels=fixture()
    body,media,scope,role=objects[pixels.artifact_id]
    wrong=np.load(BytesIO(body),allow_pickle=False);wrong[1,1,0]=7.
    stream=BytesIO();np.save(stream,wrong,allow_pickle=False);body=stream.getvalue()
    pixel_sha=sha256(body).hexdigest();objects[pixels.artifact_id]=(body,media,scope,role)
    root_body,root_media,root_scope,root_role=objects[root.artifact_id]
    result=json.loads(root_body);image_id=UUID(result['working_image']['artifact_id'])
    image_body,image_media,image_scope,image_role=objects[image_id]
    image=json.loads(image_body);image['pixels']['sha256']=pixel_sha
    image_body=canonical_bytes(image);objects[image_id]=(image_body,image_media,image_scope,image_role)
    result['working_image']['sha256']=sha256(image_body).hexdigest()
    root_body=canonical_bytes(result);objects[root.artifact_id]=(root_body,root_media,root_scope,root_role)
    root=EvidenceRef(root.artifact_id,sha256(root_body).hexdigest())
    with pytest.raises(AdoptionInvalid,match='LAYER_ADOPTION_COMPOSITION_INVALID'):
        verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000)
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())



def test_generic_adoption_prepares_composition_inventory(tmp_path):
    from photo_worker.v2.adoption import prepare_v2_adoption
    reader,params,root,objects,pixels=fixture()
    prepared=prepare_v2_adoption(task_id=uuid4(),operation='layer.compose',version='1',
        root=root,reader=reader,params_ref=params,layer_scratch_root=tmp_path,layer_scratch_bytes=1000000)
    assert prepared.inventory and len(prepared.digest)==64
    import json
    parent_reports={str(identifier) for identifier,(body,media,scope,role) in objects.items()
        if scope=='input' and media=='application/json' and json.loads(body).get('operation')=='optical.lens'}
    assert parent_reports and parent_reports<={item['artifact_id'] for item in prepared.inventory}

    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('failure',[None,'strict','retained','uint16','wrong'])
def test_adoption_proves_legacy_conversion_and_new_reference(tmp_path,failure):
    reader,params,root,objects,pixels=fixture(legacy_alpha=True,alpha_failure=failure)
    def verify():
        verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000,
                                 alpha_mode='require_float32' if failure=='strict' else 'normalize_uint16')
    if failure is None:verify()
    else:
        with pytest.raises(AdoptionInvalid):verify()
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())


def test_worker_published_legacy_conversion_passes_generic_adoption(tmp_path):
    from contextlib import contextmanager
    from photo_worker.operations.layers.adapter import execute
    from photo_worker.operations.layers.resources import layer_resources
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.v2.adoption import prepare_v2_adoption
    reader,params_ref,old_root,objects,_=fixture(legacy_alpha=True)
    for identifier in [k for k,v in objects.items() if v[2]=='output']:del objects[identifier]
    scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0)
    class Artifacts:
        def params(self,model):return model.model_validate_json(objects[params_ref.artifact_id][0])
        def contract(self,ref,model):return model.model_validate_json(objects[ref.artifact_id][0])
        @contextmanager
        def input(self,ref,**kwargs):
            path=tmp_path/(str(ref.artifact_id)+'.npy')
            path.write_bytes(objects[ref.artifact_id][0])
            try:yield SimpleNamespace(path=path)
            finally:path.unlink()
        def put(self,body,media,role):
            ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
            objects[ref.artifact_id]=(body,media,'output',role)
            return ref
        def stage(self,path,reservation,role,media):
            reservation.validate_file(path)
            return self.put(path.read_bytes(),media,role)
        def stage_contract(self,value,role):return self.put(canonical_bytes(value),'application/json',role)
    artifacts=Artifacts();artifacts.scratch=scratch
    profile={'backend':'cpu','renderer_version':'v2.2','layer_alpha_mode':'normalize_uint16',
             'resource_estimate':layer_resources(2,2,mask_refs=1,mask_nodes=1,alpha_mode='normalize_uint16')}
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=profile),check_active=lambda:None,progress=lambda *args:None)
    # Relational admission is separately covered by PostgreSQL integration tests.
    admission=SimpleNamespace(admit=lambda *args:None)
    try:
        result=execute(ctx,artifacts,admission)
        ref=result.outputs[0];root=EvidenceRef(ref.artifact_id,ref.sha256)
        prepared=prepare_v2_adoption(task_id=uuid4(),operation='layer.compose',version='1',root=root,
            reader=reader,params_ref=params_ref,layer_scratch_root=tmp_path,layer_scratch_bytes=1000000,
            layer_alpha_mode='normalize_uint16')
        assert prepared.inventory and len(prepared.digest)==64
        assert reader.opened==reader.closed
        assert not list(scratch.directory.rglob('*.npy'))
    finally:scratch.cleanup()
    assert not [path for path in tmp_path.rglob("*") if path.is_file()]
    assert not list(tmp_path.glob("adoption-*"))


@pytest.mark.parametrize('mutation',['omit','empty','replace'])
def test_rehashed_report_cannot_drop_or_replace_parent_provenance(tmp_path,mutation):
    import json
    from uuid import UUID
    reader,params,root,objects,_=fixture()
    root_body,root_media,root_scope,root_role=objects[root.artifact_id]
    result=json.loads(root_body);report_id=UUID(result['report']['artifact_id'])
    body,media,scope,role=objects[report_id];report=json.loads(body)
    if mutation=='omit':del report['parent_reports']
    elif mutation=='empty':report['parent_reports']=[]
    else:report['parent_reports']=[result['working_image']]
    body=canonical_bytes(report);objects[report_id]=(body,media,scope,role)
    result['report']['sha256']=sha256(body).hexdigest()
    root_body=canonical_bytes(result);objects[root.artifact_id]=(root_body,root_media,root_scope,root_role)
    root=EvidenceRef(root.artifact_id,sha256(root_body).hexdigest())
    with pytest.raises(AdoptionInvalid,match='LAYER_ADOPTION_METADATA_INVALID'):
        verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000)
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())
