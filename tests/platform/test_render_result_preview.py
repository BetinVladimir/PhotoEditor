"""Real existing ICC/media encoder consumption of the Render result input plan."""
from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from PIL import Image
from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.contracts.media import MediaParams,DerivativeManifest
from photo_worker.operations.optical.contracts import ImageResult
from photo_worker.operations.media.derivative import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.render_derivative import compile_render_derivative,verify_render_derivative_encoded


def reference(body):
    return ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())

@pytest.mark.parametrize('transparent',[False,True])
def test_render_image_result_through_real_media_encoder(tmp_path,transparent,monkeypatch,client_cases=None,adoption_case=None):
    registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc')
    working=registry.resolve('linear_srgb_v1');srgb=registry.resolve('srgb_v1')
    rgb=np.full((32,64,3),.25,dtype=np.float32)
    pixels=tmp_path/'pixels.npy';np.save(pixels,rgb);pixel_ref=reference(pixels.read_bytes())
    paths={pixel_ref.artifact_id:pixels};alpha_ref=None
    if transparent:
        alpha_path=tmp_path/'alpha.npy';np.save(alpha_path,np.tile(np.linspace(0,65535,64,dtype=np.uint16),(32,1)))
        alpha_ref=reference(alpha_path.read_bytes());paths[alpha_ref.artifact_id]=alpha_path
    image=WorkingImage(pixels=pixel_ref,alpha=alpha_ref,dtype='float32',
        canvas=Canvas(width=64,height=32,orientation=1,space_id='result',source_sha256='a'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256=working.sha256),
        transform_chain=TransformChain.create([]))
    image_bytes=canonical_bytes(image);image_ref=reference(image_bytes)
    root=ImageResult(schema_version=1,working_image=image_ref,report=reference(b'report'),changed_mask=reference(b'mask'),generated_mask=None)
    root_bytes=canonical_bytes(root)
    plan=compile_render_derivative(reference(root_bytes),root_bytes,image_bytes,srgb.sha256)
    params=MediaParams.model_validate_json(plan.params_bytes)
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(inputs=plan.inputs),execution_profile={'backend':'cpu'}),check_active=lambda:None,progress=lambda *args:None)
    staged={};manifest=[]
    @contextmanager
    def read(ref,**kwargs):
        path=paths[ref.artifact_id];assert sha256(path.read_bytes()).hexdigest()==ref.sha256
        yield SimpleNamespace(path=path)
    def stage(path,reservation,role,media_type):
        reservation.validate_file(path);body=path.read_bytes();ref=reference(body);staged[ref.artifact_id]=(body,media_type);return ref
    def stage_contract(value,role):
        assert role=='derivative_manifest';manifest.append(value);return reference(canonical_bytes(value))
    scratch_root=tmp_path/'scratch';scratch_root.mkdir()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,4<<20,min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(params=lambda model:model.model_validate_json(plan.params_bytes),contract=lambda ref,model:model.model_validate_json(image_bytes),input=read,scratch=scratch,stage=stage,stage_contract=stage_contract)
        result=execute(ctx,artifacts,registry,'media.preview')
    assert len(result.outputs)==2 and len(manifest)==1
    document=DerivativeManifest.model_validate(manifest[0])
    body,mime=staged[document.derivative.artifact_id]
    assert document.source==image_ref and document.alpha==transparent
    assert document.profile_hash==params.profile_hash and document.width==64 and document.height==32
    assert mime==('image/webp' if transparent else 'image/jpeg')
    assert sha256(body).hexdigest()==document.derivative.sha256
    verify_render_derivative_encoded(document,params,image,body,srgb_sha256=srgb.sha256)
    from photo_worker.v2.render_derivative_adoption import verify_render_derivative_result
    from photo_worker.v2.adoption import ArtifactRef as EvidenceRef,AdoptionInvalid
    def evidence_ref(ref):return EvidenceRef(ref.artifact_id,ref.sha256)
    params_evidence=evidence_ref(reference(plan.params_bytes));manifest_evidence=evidence_ref(result.outputs[1])
    derivative_evidence=evidence_ref(document.derivative)
    documents={params_evidence:plan.params_bytes,manifest_evidence:canonical_bytes(document),evidence_ref(image_ref):image_bytes}
    metadata={params_evidence:('input','task_params','application/json'),manifest_evidence:('output','derivative_manifest','application/json'),evidence_ref(image_ref):('input','working_image','application/json'),derivative_evidence:('output','derivative',mime)}
    def evidence(ref):
        source,role,media_type=metadata[ref]
        return SimpleNamespace(ref=ref,source=source,role=role,media_type=media_type,bytes=len(documents[ref]) if ref in documents else len(body))
    reader=SimpleNamespace(evidence=evidence,read_json=lambda ref,limit:documents[ref],staged_outputs=lambda:{manifest_evidence,derivative_evidence},iter_bytes=lambda ref,limit:iter([body]))
    assert verify_render_derivative_result(reader,params_evidence,manifest_evidence,target_icc_sha256=srgb.sha256,working_icc_sha256=working.sha256)==document
    if adoption_case is not None:
        adoption_case(dict(bodies={**documents,derivative_evidence:body},metadata=metadata,
            params=params_evidence,manifest=manifest_evidence,derivative=derivative_evidence,
            document=document,target_sha=srgb.sha256,working_sha=working.sha256))
    from photo_worker.v2.render_derivative_store import PostgresRenderDerivativeReader
    tenant_scope,run_scope,task_scope,result_scope=uuid4(),uuid4(),uuid4(),uuid4()
    bodies={**documents,derivative_evidence:body}
    by_id={ref.artifact_id:ref for ref in bodies}
    def query(sql,args):
        assert args[:3]==(tenant_scope,run_scope,'preview1600')
        if 'v26_render_derivative_context(' in sql:
            rows=[(task_scope,result_scope,1,1,params_evidence.artifact_id,params_evidence.sha256,manifest_evidence.artifact_id,manifest_evidence.sha256)]
        elif 'v26_render_derivative_result(' in sql:
            rows=[(task_scope,result_scope,1,1,ref.artifact_id,ref.sha256) for ref in (manifest_evidence,derivative_evidence)]
        else:
            assert args[3:6]==(result_scope,1,1)
            ref=by_id[args[6]];source,role,media_type=metadata[ref]
            rows=[('test',str(ref.artifact_id),'immutable-v1',ref.sha256,len(bodies[ref]),media_type,role,source)]
        return SimpleNamespace(fetchall=lambda:rows)
    in_transaction=[False]
    def get_object(bucket,key,*,version_id):
        assert not in_transaction[0]
        assert bucket=='test' and version_id=='immutable-v1'
        return BytesIO(bodies[by_id[next(identity for identity in by_id if str(identity)==key)]])
    sql_reader=PostgresRenderDerivativeReader(SimpleNamespace(autocommit=True,execute=query),SimpleNamespace(get_object=get_object),tenant_id=tenant_scope,run_id=run_scope,task_id=task_scope,result_id=result_scope,variant='preview1600',attempt=1,fence=1,allowed_bucket='test')
    assert verify_render_derivative_result(sql_reader,params_evidence,manifest_evidence,target_icc_sha256=srgb.sha256,working_icc_sha256=working.sha256)==document
    from photo_worker.v2.render_derivative_service import RenderDerivativeAdoptionCoordinator
    receipts=[]
    rejections=[]
    accept_receipt=[True]
    @contextmanager
    def transaction():
        assert not in_transaction[0];in_transaction[0]=True
        try:yield
        finally:in_transaction[0]=False
    def write_receipt(sql,args):
        assert in_transaction[0]
        assert args[:6]==(tenant_scope,run_scope,'preview1600',result_scope,1,1)
        if 'reject_v26_render_derivative' in sql:
            assert args[6]=='RENDER_DERIVATIVE_ENCODING_INVALID';rejections.append(args)
            return SimpleNamespace(fetchone=lambda:(True,))
        assert 'record_v26_render_derivative_receipt' in sql
        assert args[6]==canonical_bytes(document).decode()
        receipts.append(args)
        return SimpleNamespace(fetchone=lambda:(accept_receipt[0],))
    coordinator=RenderDerivativeAdoptionCoordinator(SimpleNamespace(autocommit=True,execute=query),SimpleNamespace(autocommit=True,execute=write_receipt,transaction=transaction),SimpleNamespace(get_object=get_object), 'test',target_icc_sha256=srgb.sha256,working_icc_sha256=working.sha256)
    assert coordinator.adopt(tenant_scope,run_scope,'preview1600') is True and len(receipts)==1 and not in_transaction[0]
    assert coordinator.adopt(tenant_scope,run_scope,'preview1600') is True and receipts[0]==receipts[1]
    accept_receipt[0]=False
    with pytest.raises(AdoptionInvalid,match='COMMIT_STALE'):
        coordinator.adopt(tenant_scope,run_scope,'preview1600')
    assert not in_transaction[0]
    from photo_worker.v2 import render_derivative_service
    def invalid_encoded(*args,**kwargs):raise AdoptionInvalid('RENDER_DERIVATIVE_ENCODING_INVALID')
    monkeypatch.setattr(render_derivative_service,'verify_render_derivative_result',invalid_encoded)
    assert coordinator.adopt(tenant_scope,run_scope,'preview1600') is False
    assert len(rejections)==1 and not in_transaction[0]
    def unavailable(*args,**kwargs):raise OSError('object store unavailable')
    monkeypatch.setattr(render_derivative_service,'verify_render_derivative_result',unavailable)
    with pytest.raises(OSError):coordinator.adopt(tenant_scope,run_scope,'preview1600')
    assert len(rejections)==1
    reader.staged_outputs=lambda:{manifest_evidence}
    with pytest.raises(AdoptionInvalid):
        verify_render_derivative_result(reader,params_evidence,manifest_evidence,target_icc_sha256=srgb.sha256,working_icc_sha256=working.sha256)
    with pytest.raises(ValueError):
        verify_render_derivative_encoded(document,params,image,body[:-1],srgb_sha256=srgb.sha256)
    with pytest.raises(ValueError):
        verify_render_derivative_encoded(document.model_copy(update={'width':63}),params,image,body,srgb_sha256=srgb.sha256)
    with pytest.raises(ValueError):
        verify_render_derivative_encoded(document,params,image,body,srgb_sha256='0'*64)
    # Rehash a syntactically valid encoded image with the wrong ICC: byte binding
    # alone must not admit a color profile substitution.
    with Image.open(BytesIO(body)) as encoded:
        changed=BytesIO();encoded.save(changed,format=encoded.format,icc_profile=working.data)
    replaced=changed.getvalue()
    changed_manifest=document.model_copy(update={'derivative':reference(replaced)})
    with pytest.raises(ValueError):
        verify_render_derivative_encoded(changed_manifest,params,image,replaced,srgb_sha256=srgb.sha256)
    with Image.open(BytesIO(body)) as encoded:
        private=Image.Exif();private[270]='private description'
        changed=BytesIO();encoded.save(changed,format=encoded.format,icc_profile=srgb.data,exif=private.tobytes())
    replaced=changed.getvalue()
    changed_manifest=document.model_copy(update={'derivative':reference(replaced)})
    with pytest.raises(ValueError):
        verify_render_derivative_encoded(changed_manifest,params,image,replaced,srgb_sha256=srgb.sha256)
    appended=body+b'private trailing data'
    changed_manifest=document.model_copy(update={'derivative':reference(appended)})
    with pytest.raises(ValueError):
        verify_render_derivative_encoded(changed_manifest,params,image,appended,srgb_sha256=srgb.sha256)
    with Image.open(BytesIO(body)) as encoded:
        assert encoded.size==(64,32) and encoded.mode==('RGBA' if transparent else 'RGB')
        assert encoded.info['icc_profile']==srgb.data
        decoded=np.array(encoded)
        # Linear .25 must be encoded at roughly .537 sRGB, not displayed as .25.
        assert abs(float(decoded[16,32,0])-137)<=5
        if transparent:assert decoded[16,0,3]==0 and decoded[16,-1,3]==255
    np.testing.assert_array_equal(np.load(pixels),rgb)
    assert result.bytes_written==len(body)+len(canonical_bytes(document))

    before=dict(staged)
    ctx.lease.command.inputs=tuple(reversed(plan.inputs))
    with pytest.raises(OperationFailure) as denied:
        execute(ctx,artifacts,registry,'media.preview')
    assert str(denied.value)=='INPUT_INVALID' and staged==before

    if client_cases is not None:
        import base64
        def output(ref,role,media_type,payload):
            return dict(artifact_id=str(ref.artifact_id),role=role,sha256=ref.sha256,
                bytes=len(payload),media_type=media_type,url='https://photo.test/'+str(ref.artifact_id)+'?versionId=fixture-object-version',expires_at='2099-01-01T00:00:00Z')
        client_cases.append(dict(alpha=transparent,encoded=base64.b64encode(body).decode(),
            image=output(document.derivative,'derivative',mime,body),
            manifest=output(result.outputs[1],'derivative_manifest','application/json',canonical_bytes(document))))
