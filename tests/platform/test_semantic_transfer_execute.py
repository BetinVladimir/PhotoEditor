from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_hash,ArtifactRef
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.semantic.transfer import execute_transfer,SemanticTransferArrays
from photo_worker.operations.semantic.resources import semantic_transfer_resources
from test_semantic_transfer_contracts import transfer_documents
from test_image_denoise import Artifacts


@pytest.mark.parametrize('variant',['unsupported','qualified','qualified_revoked_before_stage','qualified_revoked_after_stage','budget','runtime','output_budget','scratch_budget'])
def test_transfer_executor_runtime_admission_and_unsupported_result(tmp_path,variant):
    a=Artifacts(tmp_path);params,intent,calibration=transfer_documents()
    params=params.model_copy(update={'calibration':ArtifactRef(artifact_id=uuid4(),sha256='f'*64)})
    reference=params.reference.model_copy(update={'canvas_hash':canonical_hash(a.image.canvas)})
    target=params.target.model_copy(update={'canvas_hash':canonical_hash(a.image.canvas)})
    params=params.model_copy(update={'reference':reference,'target':target});intent=intent.model_copy(update={'reference':reference})
    documents={params.intent.artifact_id:intent,params.calibration.artifact_id:calibration,
        reference.source.artifact_id:a.image,target.source.artifact_id:a.image}
    staged=[];calls=[]
    def document(value,role):
        staged.append(value);return ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value))
    shape=(20,24);costs=semantic_transfer_resources(shape,shape)
    if variant=='budget':costs['ram_bytes']-=1
    if variant=='output_budget':costs['output_bytes']-=1
    if variant=='scratch_budget':costs['scratch_bytes']-=1
    certificate=[None]
    def runtime():
        calls.append('runtime')
        if variant=='runtime':raise ValueError('RUNTIME_CHANGED')
        if variant.startswith('qualified'):
            if (variant=='qualified_revoked_before_stage' and calls.count('runtime')==2
                    or variant=='qualified_revoked_after_stage' and calls.count('runtime')==3):
                raise ValueError('QUALIFICATION_REVOKED')
            if certificate[0] is not None:return certificate[0]
            from test_semantic_qualification_report import fixture
            from dataclasses import replace
            from photo_worker.v2.adoption import ArtifactRef as FrozenRef
            _,_,_,pin=fixture()
            certificate[0]=replace(pin,model_lock_sha=params.model_lock_sha,dataset_sha=calibration.dataset_sha,
                calibration=FrozenRef(params.calibration.artifact_id,params.calibration.sha256))
            return certificate[0]

    @contextmanager
    def load(*args):
        calls.append('load')
        rgb=np.full((*shape,3),.5,np.float32);zero=np.zeros(shape,np.float32)
        yield SemanticTransferArrays(rgb,rgb,np.ones(shape,np.float32),zero,zero)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(params=lambda model:params,contract=lambda ref,model:documents[ref.artifact_id],
            scratch=scratch,stage_contract=document)
        ctx=SimpleNamespace(check_active=lambda:None,progress=lambda *args:None,
            lease=SimpleNamespace(execution_profile={'resource_estimate':costs}))
        def run():return execute_transfer(ctx,artifacts,load_arrays=load,verify_runtime=runtime,verify_scene=lambda *args:None)
        if variant in ('qualified_revoked_before_stage','qualified_revoked_after_stage'):
            with pytest.raises(ValueError,match='QUALIFICATION_REVOKED'):run()
            assert len(staged)==(0 if variant=='qualified_revoked_before_stage' else 1)
        elif variant=='runtime':
            with pytest.raises(ValueError,match='RUNTIME_CHANGED'):run()
        elif variant=='output_budget':
            with pytest.raises(OperationFailure,match='OUTPUT_ADMISSION_EXCEEDED'):run()
        elif variant=='scratch_budget':
            with pytest.raises(OperationFailure,match='SCRATCH_ADMISSION_EXCEEDED'):run()
        elif variant=='budget':
            with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):run()
        else:
            result=run()
            assert not result.review_required and len(result.outputs)==1
            assert staged[0].state=='unsupported' and result.bytes_written>0
        if variant not in ('unsupported','qualified','qualified_revoked_before_stage','qualified_revoked_after_stage'):assert calls==['runtime'] and staged==[]
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('variant',['proposed','protected','source_protected','cancel','source_changed','stage_error'])
def test_positive_executor_native_closure_review_and_cancellation(tmp_path,variant):
    import cv2
    from photo_worker.contracts.base import canonical_bytes
    a=Artifacts(tmp_path);params,intent,calibration=transfer_documents()
    a.image.canvas.width=256;a.image.canvas.height=256
    params=params.model_copy(update={'calibration':ArtifactRef(artifact_id=uuid4(),sha256='f'*64),
        'reference':params.reference.model_copy(update={'canvas_hash':canonical_hash(a.image.canvas)}),
        'target':params.target.model_copy(update={'canvas_hash':canonical_hash(a.image.canvas)})})
    intent=intent.model_copy(update={'reference':params.reference})
    documents={params.intent.artifact_id:intent,params.calibration.artifact_id:calibration,
        params.reference.source.artifact_id:a.image,params.target.source.artifact_id:a.image}
    rng=np.random.default_rng(106);luma=rng.random((256,256),dtype=np.float32)
    source=np.repeat(luma[:,:,None],3,axis=2)
    target=cv2.warpAffine(source,np.array([[1,0,4],[0,1,6]],float),(256,256))
    mask=np.zeros((256,256),np.float32);mask[50:100,60:110]=1
    protected=np.zeros_like(mask)
    if variant=='protected':protected[75,85]=.001
    source_protected=np.zeros_like(mask)
    if variant=='source_protected':source_protected[75,85]=.001
    cancelled=False;events=[];staged=[];sizes=[]
    def active():
        if cancelled:raise InterruptedError('cancelled')
    @contextmanager
    def load(*args):
        events.append('load')
        try:yield SemanticTransferArrays(source[:16] if variant=='source_changed' else source,target,
            mask,source_protected,protected)
        finally:events.append('release')
    def pixels(path,owner,role,media):
        nonlocal cancelled
        if variant=="stage_error":raise ValueError("SEMANTIC_PROTECTED_OVERLAP")
        owner.validate_file(path);array=np.load(path,allow_pickle=False)
        assert array.shape==(256,256) and array[75,85]>.9
        ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
        staged.append((role,None,ref));sizes.append(path.stat().st_size)
        cancelled=variant=='cancel';return ref
    def document(value,role):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value))
        staged.append((role,value,ref));sizes.append(len(canonical_bytes(value)));return ref
    costs=semantic_transfer_resources(mask.shape,mask.shape)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(params=lambda model:params,contract=lambda ref,model:documents[ref.artifact_id],
            scratch=scratch,stage=pixels,stage_contract=document)
        ctx=SimpleNamespace(check_active=active,progress=lambda *args:None,
            lease=SimpleNamespace(execution_profile={'resource_estimate':costs}))
        def run():return execute_transfer(ctx,artifacts,load_arrays=load,
            verify_runtime=lambda:events.append('runtime'),verify_scene=lambda *args:events.append('scene'))
        if variant in ('protected','source_protected'):
            result=run()
            assert not result.review_required and len(result.outputs)==1
            assert [role for role,_,_ in staged]==['document']
            assert staged[0][1].state=='unsupported'
            assert staged[0][1].reason=='SEMANTIC_PROTECTED_OVERLAP'
            assert staged[0][1].mask is None and staged[0][1].transform is None
            assert result.bytes_written==sum(sizes)
        elif variant=='stage_error':
            with pytest.raises(ValueError,match='SEMANTIC_PROTECTED_OVERLAP'):run()
            assert staged==[]
        elif variant=='source_changed':
            with pytest.raises(ValueError,match='SOURCE_CHANGED'):run()
            assert staged==[]
        elif variant=='cancel':
            with pytest.raises(InterruptedError):run()
            assert [role for role,_,_ in staged]==['mask_pixels']
        else:
            result=run()
            assert result.review_required and result.bytes_written==sum(sizes)
            assert [role for role,_,_ in staged]==['mask_pixels','mask','transform','document']
            assert result.outputs==[staged[-1][2]]
            candidate=staged[-1][1]
            assert candidate.state=='proposed' and candidate.target==params.target
            assert candidate.mask==staged[1][2] and candidate.transform==staged[2][2]
        assert events[0]=='runtime' and events[-1]=='release'
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('variant',['calibration_identity','model','dataset','certificate'])
def test_qualified_execution_denies_changed_documents_before_images(variant):
    from test_semantic_qualification_report import fixture
    from dataclasses import replace
    from photo_worker.v2.adoption import ArtifactRef as FrozenRef,AdoptionInvalid
    _,_,_,pin=fixture()
    params,intent,calibration=transfer_documents()
    pin=replace(pin,model_lock_sha=params.model_lock_sha,dataset_sha=calibration.dataset_sha,
        calibration=FrozenRef(params.calibration.artifact_id,params.calibration.sha256))
    if variant=='calibration_identity':pin=replace(pin,calibration=FrozenRef(uuid4(),params.calibration.sha256))
    elif variant=='model':pin=replace(pin,model_lock_sha='0'*64)
    elif variant=='dataset':pin=replace(pin,dataset_sha='0'*64)
    else:pin=True
    calls=[]
    def contract(ref,model):
        calls.append(model.__name__)
        if model.__name__=='EditIntent':return intent
        if model.__name__=='Calibration':return calibration
        raise AssertionError('No image IO allowed after denied qualification')
    artifacts=SimpleNamespace(params=lambda model:params,contract=contract)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={}))
    error,code={'calibration_identity':(AdoptionInvalid,'BINDING_INVALID'),'model':(AdoptionInvalid,'BINDING_INVALID'),'dataset':(ValueError,'DATASET_CHANGED'),'certificate':(OperationFailure,'CONFIG_INVALID')}[variant]
    with pytest.raises(error,match=code):
        execute_transfer(ctx,artifacts,load_arrays=lambda *args:None,verify_runtime=lambda:pin,verify_scene=lambda *args:None)
    assert calls==['EditIntent','Calibration']
