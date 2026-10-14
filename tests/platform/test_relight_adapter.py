from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_relight_inputs import inputs
from photo_worker.contracts.base import ArtifactRef, canonical_hash, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.portrait.local_resources import local_portrait_resources
from photo_worker.operations.portrait.relight_adapter import execute, relight_operations


@pytest.mark.parametrize('mode',['success','no_invalid','revoked','nonbinary','alpha','params','undeclared'])
def test_adapter_real_arrays_preparation_and_authority(tmp_path,mode):
    params,source,depth,subject,protected,calibration,declared=inputs()
    if mode=='no_invalid':
        depth=depth.model_copy(update={'invalid_mask':None})
        old=params.depth
        params=params.model_copy(update={'depth':old.model_copy(update={'sha256':canonical_hash(depth)})})
        declared=[params.depth if ref==old else ref for ref in declared]
    h,w=source.canvas.height,source.canvas.width
    profile={'backend':'cpu','renderer_version':'v2.2','resource_estimate':local_portrait_resources(w,h)}
    command=SimpleNamespace(operation='image.relight',operation_version='1',resource_class='cpu',
        params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params)),inputs=declared)
    if mode=='params':command.params=command.params.model_copy(update={'sha256':'f'*64})
    if mode=='undeclared':command.inputs=[]
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,execution_profile=profile),check_active=lambda:None)
    docs={str(ref.artifact_id):doc for ref,doc in [(params.base.source,source),(params.depth,depth),
        (params.subject,subject),(params.protected,protected),(params.calibration,calibration)]}
    arrays=[(source.pixels,np.full((h,w,3),.4,np.float32)),
        (depth.artifact,np.full((h,w),.5,np.float32)),(subject.artifact,np.ones((h,w),np.float32)),
        (protected.artifact,np.zeros((h,w),np.float32)),(source.alpha,np.ones((h,w),np.float32))]
    arrays[3][1][:,:5]=1
    if mode=='alpha':arrays[-1][1][0,0]=2
    if depth.invalid_mask:
        invalid=np.zeros((h,w),np.float32);invalid[:5]=1
        if mode=='nonbinary':invalid[0,0]=.5
        arrays.append((depth.invalid_mask,invalid))
    paths={}
    for i,(ref,array) in enumerate(arrays):
        path=tmp_path/f'input-{i}.npy';np.save(path,array);paths[str(ref.artifact_id)]=path
    class Admission:
        calls=0
        def admit(self,*args):
            self.calls+=1
            assert args[-1].depth_sigma==calibration.depth_sigma
            if mode=='revoked' and self.calls==2:raise ValueError('REVOKED')
    admission=Admission()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,profile['resource_estimate']['scratch_bytes'],min_free_bytes=0) as scratch:
        class Artifacts:
            roles={};reads=[];written=0
            def params(self,model):return params
            def contract(self,ref,model):
                value=docs[str(ref.artifact_id)];assert isinstance(value,model);return value
            @contextmanager
            def input(self,ref,**kwargs):
                self.reads.append(ref);yield SimpleNamespace(path=paths[str(ref.artifact_id)])
            def stage(self,path,reservation,role,mime):
                reservation.validate();value=np.load(path).copy();self.roles[role]=value
                self.written+=path.stat().st_size
                return ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
            def stage_contract(self,value,role):
                self.roles[role]=value;self.written+=len(canonical_bytes(value))
                return ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value))
        artifacts=Artifacts();artifacts.scratch=scratch
        if mode in ('success','no_invalid'):
            result=execute(ctx,artifacts,admission)
            assert admission.calls==2 and result.review_required and result.bytes_written==artifacts.written
            assert source.alpha in artifacts.reads
            output=artifacts.roles['pixels'];changed=artifacts.roles['mask_pixels']
            np.testing.assert_array_equal(output[:,:5],arrays[0][1][:,:5])
            if mode=='success':np.testing.assert_array_equal(output[:5],arrays[0][1][:5])
            assert np.any(output[10:,10:]>arrays[0][1][10:,10:])
            np.testing.assert_array_equal(changed,np.any(output!=arrays[0][1],axis=-1).astype(np.float32))
        else:
            with pytest.raises(OperationFailure,match='INPUT_INVALID'):execute(ctx,artifacts,admission)
            assert not artifacts.roles
            if mode in ('params','undeclared'):assert not artifacts.reads and admission.calls==0
        assert not list(scratch.directory.iterdir())


def test_no_authority_no_operation():
    assert relight_operations(None)==[]
    with pytest.raises(ValueError,match='RELIGHT_ADMISSION_CONFIG_INVALID'):relight_operations(None,42)
    with pytest.raises(OperationFailure,match='RELIGHT_INPUT_AUTHORITY_UNAVAILABLE'):execute(None,None,None)
