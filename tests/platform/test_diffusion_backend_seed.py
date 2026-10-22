from types import SimpleNamespace
import numpy as np
import pytest
from PIL import Image
from photo_worker.models.adapters import DiffusersInpaint


@pytest.mark.parametrize('backend,seed_device',[('mps','cpu'),('cuda','cuda'),('cpu','cpu')])
def test_seed_generator_uses_supported_device_without_moving_pipeline(backend,seed_device):
    calls=[];generator=object();submitted=[]
    def create(*,device):
        calls.append(device)
        return SimpleNamespace(manual_seed=lambda seed:generator if seed==42 else pytest.fail('seed changed'))
    def pipeline(**kwargs):
        submitted.append(kwargs)
        return SimpleNamespace(images=[Image.new('RGB',(512,512))])
    adapter=object.__new__(DiffusersInpaint);adapter.device=backend
    adapter.torch=SimpleNamespace(Generator=create);adapter.pipe=pipeline
    result=adapter.generate(np.zeros((512,512,3),np.float32),np.zeros((512,512),np.float32),
        prompt='photo',negative_prompt='',seed=42,steps=25,guidance_scale=7.5,scheduler='ddim_v1')
    assert calls==[seed_device] and submitted[0]['generator'] is generator
    assert adapter.device==backend and result.shape==(512,512,3)
