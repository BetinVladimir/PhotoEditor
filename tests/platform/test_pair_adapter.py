from contextlib import contextmanager
import hashlib
import json
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.contracts.pair import RegistrationParams,PairAlignment
from photo_worker.operations.pair.register import execute
from photo_worker.runtime.executor import OperationFailure
from test_asset_adapters import context_for as base_context

def context_for(source,operation):
    ctx=base_context(source,operation)
    ctx.lease.execution_profile['resource_estimate']={'ram_bytes':268435456}
    return ctx
from test_registration import scene

class Artifacts:
    def __init__(self,root):
        self.paths={};self.manifests={};self.staged=[]
        refs=[]
        for name in ('before','after'):
            path=root/(name+'.npy');np.save(path,scene())
            pixel=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
            manifest=WorkingImage(pixels=pixel,canvas=Canvas(width=384,height=256,orientation=1,space_id=name,source_sha256=pixel.sha256),
                color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='a'*64,encoding='gamma24'),dtype='float32',alpha=None,transform_chain=TransformChain.create([]))
            ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(manifest)).hexdigest())
            self.paths[pixel.artifact_id]=path;self.manifests[ref.artifact_id]=manifest;refs.append(ref)
        self.raw=dict(before=refs[0].model_dump(mode='json'),after=refs[1].model_dump(mode='json'))
        self.before=refs[0]
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    def contract(self,ref,model):return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self,ref,*,suffix):yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage_contract(self,manifest,role):
        self.staged.append((role,manifest))
        return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(manifest)).hexdigest())


def test_executable_registration_evidence(tmp_path):
    artifacts=Artifacts(tmp_path);ctx=context_for(artifacts.before,'pair.register')
    result=execute(ctx,artifacts)
    role,alignment=artifacts.staged[0]
    assert role=='alignment' and alignment.confidence=='measured'
    assert alignment.inlier_count>=12 and alignment.overlap>=.9
    assert np.allclose(np.array(alignment.before_to_after).reshape(3,3),np.eye(3),atol=.01)
    assert 'background_mask_not_provided' in alignment.warnings and result.review_required
    assert ctx.snapshot_progress()==dict(done=4,total=4)
    PairAlignment.model_validate_json(canonical_bytes(alignment))


def test_manual_requires_confirmation_and_does_not_invent_evidence(tmp_path):
    artifacts=Artifacts(tmp_path);ctx=context_for(artifacts.before,'pair.register')
    artifacts.raw['manual_transform']=np.eye(3).ravel().tolist()
    with pytest.raises(OperationFailure):execute(ctx,artifacts)
    assert not artifacts.staged
    artifacts.raw['manual_confirmed']=True
    result=execute(context_for(artifacts.before,'pair.register'),artifacts)
    alignment=artifacts.staged[0][1]
    assert alignment.confidence=='user_confirmed' and alignment.median_reprojection is None and alignment.inlier_count==0
    assert not result.review_required


def test_reject_layout_mismatch_and_alpha_before_staging(tmp_path):
    artifacts=Artifacts(tmp_path)
    path=next(iter(artifacts.paths.values()));np.save(path,np.zeros((4,5,3),np.float32))
    with pytest.raises(OperationFailure):execute(context_for(artifacts.before,'pair.register'),artifacts)
    assert not artifacts.staged
    image=artifacts.manifests[artifacts.before.artifact_id];image.alpha=image.pixels
    with pytest.raises(OperationFailure):execute(context_for(artifacts.before,'pair.register'),artifacts)
    assert not artifacts.staged


def test_registration_requires_admitted_memory_before_artifact_reads(tmp_path):
    artifacts=Artifacts(tmp_path);ctx=context_for(artifacts.before,'pair.register')
    ctx.lease.execution_profile['resource_estimate']['ram_bytes']=1024
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='CONFIG_INVALID' and not artifacts.staged
