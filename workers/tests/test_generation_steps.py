"""Generation step propagation contracts; authored during coding, not executed."""
from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.models.adapters import DiffusersInpaint

@pytest.mark.parametrize('steps',[1,12,25,50])
def test_diffusers_receives_exact_admitted_steps(steps):
    engine=DiffusersInpaint.__new__(DiffusersInpaint)
    engine.device='mps'
    class Generator:
        def __init__(self,device):assert device=='cpu'
        def manual_seed(self,seed):assert seed==7;return self
    engine.torch=SimpleNamespace(Generator=Generator)
    received=[]
    def pipe(**kwargs):
        received.append(kwargs)
        return SimpleNamespace(images=[np.zeros((512,512,3),np.uint8)])
    engine.pipe=pipe
    result=engine.generate(np.zeros((512,512,3),np.float32),np.ones((512,512),np.float32),prompt='fill',negative_prompt='',seed=7,steps=steps,guidance_scale=7.5,scheduler='ddim_v1')
    assert received[0]['num_inference_steps']==steps
    assert result.shape==(512,512,3)

@pytest.mark.parametrize('steps',[0,51,True,25.0])
def test_diffusers_rejects_invalid_steps_before_inference(steps):
    engine=DiffusersInpaint.__new__(DiffusersInpaint)
    with pytest.raises(ValueError,match='GENERATION_CONFIG_INVALID'):
        engine.generate(np.zeros((1,1,3),np.float32),np.ones((1,1),np.float32),prompt='fill',negative_prompt='',seed=7,steps=steps,guidance_scale=7.5,scheduler='ddim_v1')


def test_generation_style_bounds_and_strength_are_preserved():
    from uuid import uuid4
    from photo_worker.contracts.restoration import GenerationParams
    from photo_worker.contracts.images import MaskRef,Canvas,TransformChain
    from photo_worker.contracts.base import ArtifactRef
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    canvas=Canvas(width=8,height=8,orientation=1,space_id='oriented:test',source_sha256='b'*64)
    mask=MaskRef(artifact=ref,canvas=canvas,transform_chain=TransformChain.create([]),object_id=None,selector='manual',confidence=.9,model_lock_id=None,protected=False)
    params=GenerationParams(image=ref,mask=mask,confirmed=True,prompt='',negative_prompt='я'*2000,seed=0,steps=50,guidance_scale=0,strength=.25,model_lock_id='c'*64)
    assert params.prompt=='' and len(params.negative_prompt)==2000
    assert params.guidance_scale==0 and params.strength==.25 and params.steps==50
    with pytest.raises(ValueError):GenerationParams.model_validate({**params.model_dump(),'strength':1.01})
    with pytest.raises(ValueError):GenerationParams.model_validate({**params.model_dump(),'prompt':'я'*2001})
