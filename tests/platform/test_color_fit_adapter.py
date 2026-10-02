from contextlib import contextmanager
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.color_fit import ColorFit
from photo_worker.operations.pair.fit.color import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from test_asset_adapters import context_for

class Artifacts:
    def __init__(self,root,values=(0.,0.)):
        self.paths={};self.contracts={};self.staged=[];self.opens=0
        refs=[];images=[]
        for name in ('before','after'):
            path=root/(name+'.npy');np.save(path,np.full((16,24,3),values[0 if name=='before' else 1],np.float32))
            pixel=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
            image=WorkingImage(pixels=pixel,canvas=Canvas(width=24,height=16,orientation=1,space_id=name,source_sha256=pixel.sha256),
                color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='a'*64,encoding='gamma24'),dtype='float32',alpha=None,transform_chain=TransformChain.create([]))
            ref=self.add(image);refs.append(ref);images.append(image);self.paths[pixel.artifact_id]=path
        align=PairAlignment(before=refs[0],after=refs[1],before_canvas=images[0].canvas,after_canvas=images[1].canvas,
            before_to_after=np.eye(3).ravel().tolist(),overlap=1.,median_reprojection=None,inlier_count=0,confidence='user_confirmed',crop_aware=False,warnings=[])
        self.alignment=self.add(align)
        self.raw=dict(pair_id=str(uuid4()),before=refs[0].model_dump(mode='json'),after=refs[1].model_dump(mode='json'),alignment=self.alignment.model_dump(mode='json'))
        self.before=refs[0]
    def add(self,value):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest());self.contracts[ref.artifact_id]=value;return ref
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    def contract(self,ref,model):return self.contracts[ref.artifact_id]
    @contextmanager
    def input(self,ref,*,suffix):
        self.opens+=1
        yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage_contract(self,manifest,role):
        assert list(self.scratch.directory.iterdir())==[]
        self.staged.append((role,manifest));return self.add(manifest)


def context(artifacts):
    ctx=context_for(artifacts.before,'pair.fit.color')
    ctx.lease.command.resource_class='gpu'
    ctx.lease.execution_profile['resource_estimate']={'ram_bytes':1073741824}
    return ctx


def test_identity_fitting_adapter_real_solver_and_provenance(tmp_path):
    artifacts=Artifacts(tmp_path);root=tmp_path/'scratch';root.mkdir();ctx=context(artifacts)
    with ScratchSpace(root,uuid4(),uuid4(),1,100000,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        result=execute(ctx,artifacts)
    role,manifest=artifacts.staged[0]
    assert role=='color_fit' and manifest.holdout_loss==0 and manifest.median_delta_e==0
    assert manifest.backend=='cpu' and manifest.transfer_confidence=='unvalidated'
    assert manifest.params.alignment==artifacts.alignment
    assert manifest.sampling.train_count>0 and manifest.sampling.holdout_count>0
    assert manifest.warnings==['local_mask_unavailable'] and result.review_required
    assert manifest.history[0].stage=='identity' and len(result.outputs)==1
    assert ctx.snapshot_progress()==dict(done=5,total=5)
    ColorFit.model_validate_json(canonical_bytes(manifest))


def test_alignment_binding_and_config_rejected_before_pixel_download(tmp_path):
    artifacts=Artifacts(tmp_path);ctx=context(artifacts)
    artifacts.contracts[artifacts.alignment.artifact_id].before=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='INPUT_INVALID' and artifacts.opens==0 and not artifacts.staged
    ctx=context(artifacts);ctx.lease.execution_profile['backend']='automatic'
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='CONFIG_INVALID'
    ctx=context(artifacts);ctx.lease.execution_profile['tile_size']=128
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='CONFIG_INVALID'


def test_explicit_global_only_does_not_invent_local_warning(tmp_path):
    artifacts=Artifacts(tmp_path);artifacts.raw['local_requested']=False
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,100000,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        result=execute(context(artifacts),artifacts)
    assert not result.review_required and artifacts.staged[0][1].warnings==[]


def test_nonidentity_adapter_default_optimizer_improves_holdout(tmp_path):
    import torch
    from skimage.color import deltaE_ciede2000
    from photo_worker.algorithms.color import rgb_to_lab_d50
    torch.set_num_threads(2)
    artifacts=Artifacts(tmp_path,values=(.25,.30));artifacts.raw['local_requested']=False
    root=tmp_path/'scratch';root.mkdir()
    baseline=float(deltaE_ciede2000(rgb_to_lab_d50(np.array([[.25,.25,.25]],np.float32)),
        rgb_to_lab_d50(np.array([[.30,.30,.30]],np.float32)))[0])
    with ScratchSpace(root,uuid4(),uuid4(),1,100000,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        execute(context(artifacts),artifacts)
    manifest=artifacts.staged[0][1]
    assert manifest.median_delta_e<baseline*.7
    assert manifest.p95_delta_e<baseline*.7
    assert {'parameters','residual'}<={item.stage for item in manifest.history}
    assert manifest.optimizer_version=='color_fit_adam_v1' and not manifest.local.enabled
