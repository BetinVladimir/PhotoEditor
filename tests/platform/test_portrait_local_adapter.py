from types import SimpleNamespace

import pytest

from photo_worker.operations.portrait.local_adapter import execute
from photo_worker.runtime.executor import OperationFailure


class NoIO:
    def __getattr__(self,name):
        raise AssertionError('unexpected artifact IO: '+name)


def context(**changes):
    command=SimpleNamespace(operation='portrait.hair',operation_version='1',resource_class='cpu')
    profile={'renderer_version':'v2.2','backend':'cpu'}
    for key,value in changes.items():
        if key in profile: profile[key]=value
        else: setattr(command,key,value)
    return SimpleNamespace(lease=SimpleNamespace(command=command,execution_profile=profile),check_active=lambda:None)


def test_local_adapter_requires_authority_before_any_io():
    with pytest.raises(OperationFailure,match='PORTRAIT_INPUT_AUTHORITY_UNAVAILABLE'):
        execute(context(),NoIO(),None)


@pytest.mark.parametrize('changes',[
    {'operation':'portrait.open_eyes'}, {'operation_version':'2'},
    {'resource_class':'gpu'}, {'backend':'cuda'}, {'renderer_version':'v2.1'},
])
def test_local_adapter_refuses_wrong_command_before_io(changes):
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        execute(context(**changes),NoIO(),object())


def test_cancelled_local_adapter_performs_no_io():
    ctx=context()
    def cancelled(): raise RuntimeError('cancelled')
    ctx.check_active=cancelled
    with pytest.raises(RuntimeError,match='cancelled'):
        execute(ctx,NoIO(),object())


@pytest.mark.parametrize("invalid_luminance", [False, True])
@pytest.mark.parametrize("has_alpha", [False, True])
@pytest.mark.parametrize("operation", ["portrait.hair", "portrait.clothing"])
@pytest.mark.parametrize("late_refusal", [False, True])
def test_successful_adapter_reads_npy_and_stages_result(tmp_path,late_refusal,operation,has_alpha,invalid_luminance,cancel_role=None):
    import json
    from pathlib import Path
    from uuid import uuid4
    from contextlib import contextmanager
    import numpy as np
    from photo_worker.contracts.base import ArtifactRef, canonical_hash
    from photo_worker.contracts.images import WorkingImage,MaskRef
    from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.portrait.local_resources import local_portrait_resources
    data=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_bytes())
    source=WorkingImage.model_validate_json(json.dumps(data[0]))
    source=source.model_copy(update={'canvas':source.canvas.model_copy(update={'width':5,'height':4}),'alpha':None})
    # Rebuild an empty transform chain for the deliberately small test canvas.
    from photo_worker.contracts.images import TransformChain
    source=source.model_copy(update={'transform_chain':TransformChain.create([])})
    new_ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source_ref,target_ref,guard_ref=new_ref(),new_ref(),new_ref()
    if has_alpha: source=source.model_copy(update={'alpha':new_ref()})
    target=MaskRef(artifact=new_ref(),canvas=source.canvas,transform_chain=source.transform_chain,object_id=None,selector='hair',confidence=1.,model_lock_id=None,protected=False)
    guard=target.model_copy(update={'artifact':new_ref(),'protected':True})
    model=PortraitHairParams if operation=='portrait.hair' else PortraitClothingParams
    body={'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':source_ref.model_dump(mode='json'),'canvas_hash':canonical_hash(source.canvas)},'strength':1,'mask':target_ref.model_dump(mode='json'),'protected':guard_ref.model_dump(mode='json'),'hue_delta_degrees':25,'saturation_factor':1.2,'detail':.5}
    if operation=='portrait.clothing':
        body['wrinkle_strength']=body.pop('detail')
    params=model.model_validate_json(json.dumps(body))
    rgb=np.random.default_rng(8).uniform(.1,.9,(4,5,3)).astype(np.float32)
    if invalid_luminance: rgb[:]=-1
    mask=np.ones((4,5),np.float32);mask[0]=0
    protection=np.zeros((4,5),np.float32);protection[:,0]=1
    paths={}
    for index,(ref,array) in enumerate([(source.pixels,rgb),(target.artifact,mask),(guard.artifact,protection)]):
        path=tmp_path/f'input{index}.npy';np.save(path,array);paths[ref.artifact_id]=path
    if has_alpha:
        alpha_path=tmp_path/'alpha.npy';np.save(alpha_path,np.full((4,5),.5,np.float32));paths[source.alpha.artifact_id]=alpha_path
    ctx=context(operation=operation);ctx.lease.command.inputs=[source_ref,source.pixels,target_ref,guard_ref,target.artifact,guard.artifact]
    ctx.lease.command.params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params))
    if has_alpha: ctx.lease.command.inputs.append(source.alpha)
    ctx.lease.execution_profile['resource_estimate']=local_portrait_resources(5,4)
    documents={source_ref.artifact_id:source,target_ref.artifact_id:target,guard_ref.artifact_id:guard}
    class ArtifactFiles:
        def __init__(self,scratch):self.scratch=scratch;self.staged={}
        def params(self,model):assert isinstance(params,model);return params
        def contract(self,ref,model):
            value=documents[ref.artifact_id];assert isinstance(value,model);return value
        @contextmanager
        def input(self,ref,suffix):yield SimpleNamespace(path=paths[ref.artifact_id])
        def stage(self,path,reservation,role,mime):
            reservation.validate_file(path)
            ref=new_ref();self.staged[role]=(ref,np.load(path).copy())
            if role==cancel_role: raise RuntimeError('cancelled_after_upload')
            return ref
        def stage_contract(self,value,role):
            ref=new_ref();self.staged[role]=(ref,value)
            if role==cancel_role: raise RuntimeError('cancelled_after_upload')
            return ref
    class Authority:
        calls=0
        def admit(self,*args):
            self.calls+=1
            if late_refusal and self.calls==2: raise OperationFailure("AUTHORITY_REVOKED")
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        artifacts=ArtifactFiles(scratch);authority=Authority()
        if cancel_role is not None:
            with pytest.raises(RuntimeError,match='cancelled_after_upload'):
                execute(ctx,artifacts,authority)
            order=['pixels','working_image','mask_pixels','mask','report','document']
            assert list(artifacts.staged)==order[:order.index(cancel_role)+1]
            assert not list(scratch.directory.iterdir())
            return
        if invalid_luminance:
            with pytest.raises(OperationFailure,match='INVALID_LUMINANCE'):
                execute(ctx,artifacts,authority)
            assert not artifacts.staged
            assert not list(scratch.directory.iterdir())
            return
        if late_refusal:
            with pytest.raises(OperationFailure,match="AUTHORITY_REVOKED"):
                execute(ctx,artifacts,authority)
            assert not artifacts.staged
            assert not list(scratch.directory.iterdir())
            return
        result=execute(ctx,artifacts,authority)
        assert authority.calls==2
        assert result.outputs==[artifacts.staged['document'][0]]
        assert artifacts.staged['working_image'][1].alpha==source.alpha
        output=artifacts.staged['pixels'][1]
        np.testing.assert_array_equal(output[0],rgb[0])
        np.testing.assert_array_equal(output[:,0],rgb[:,0])
        assert np.any(output[1:,1:]!=rgb[1:,1:])
        assert artifacts.staged['report'][1].status==('pass' if operation=='portrait.hair' else 'review')
        assert result.review_required==(operation=='portrait.clothing')
        np.testing.assert_array_equal(artifacts.staged['mask_pixels'][1],np.any(output!=rgb,axis=-1).astype(np.float32))


def test_local_adapter_refuses_params_hash_before_source_io():
    import json
    from uuid import uuid4
    from photo_worker.contracts.portrait_local import PortraitHairParams
    ref={'artifact_id':str(uuid4()),'sha256':'a'*64}
    params=PortraitHairParams.model_validate_json(json.dumps({'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref,'canvas_hash':'b'*64},'strength':1,'mask':ref,'protected':ref,'hue_delta_degrees':0,'saturation_factor':1,'detail':0}))
    class ParamsOnly(NoIO):
        def params(self,model): return params
    ctx=context();ctx.lease.command.params=SimpleNamespace(sha256='c'*64)
    with pytest.raises(OperationFailure,match='INPUT_INVALID'):
        execute(ctx,ParamsOnly(),object())


def test_local_operations_installation_requires_explicit_factory():
    from photo_worker.operations.portrait.local_adapter import local_portrait_operations
    assert local_portrait_operations(None)==[]
    with pytest.raises(ValueError,match='PORTRAIT_ADMISSION_CONFIG_INVALID'):
        local_portrait_operations(None,object())
    operations=local_portrait_operations(None,lambda ctx,artifacts:None)
    assert [op.name for op in operations]==['portrait.hair','portrait.clothing']
    assert all(op.resource_class=='cpu' for op in operations)


def test_local_operation_factory_missing_authority_closes_artifact_scope(monkeypatch):
    from contextlib import contextmanager
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.operations.portrait.local_adapter import local_portrait_operations
    events=[]
    @contextmanager
    def create(ctx,config,roles):
        assert roles==frozenset({'document','working_image','pixels','report','mask','mask_pixels'})
        events.append('open')
        try: yield NoIO()
        finally: events.append('close')
    monkeypatch.setattr(TaskArtifacts,'create',staticmethod(create))
    operations=local_portrait_operations(None,lambda ctx,artifacts:None)
    with pytest.raises(OperationFailure,match='PORTRAIT_INPUT_AUTHORITY_UNAVAILABLE'):
        operations[0].execute(context())
    assert events==['open','close']


@pytest.mark.parametrize('role',['pixels','working_image','mask_pixels','mask','report','document'])
@pytest.mark.parametrize('operation',['portrait.hair','portrait.clothing'])
def test_local_cancel_during_upload_closes_scratch_without_returning_result(tmp_path,role,operation):
    # Emulate TaskArtifacts.stage's post-upload check_active failure. Objects
    # already uploaded remain unadopted; local attempt reservations must close.
    test_successful_adapter_reads_npy_and_stages_result(tmp_path,False,operation,True,False,cancel_role=role)
