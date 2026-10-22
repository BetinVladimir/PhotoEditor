from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_image_inpaint import Artifacts,Pool,LOCK,context
from photo_worker.contracts.restoration import GenerationParams
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.image.generate import execute
from photo_worker.operations.region.generation import prepare_region_generation,generation_entry
from photo_worker.runtime.scratch import ScratchSpace


def fixture(tmp_path,value):
    a=Artifacts(tmp_path,protected_value=value);a.require_declared_input=lambda ref:None
    a.raw=GenerationParams.model_validate_json(canonical_bytes(dict(image=a.image_ref.model_dump(mode='json'),mask=a.raw['mask'],protected=a.raw['protected'],confirmed=True,
        prompt='fill',negative_prompt='',seed=7,steps=25,guidance_scale=7.5,strength=.7,model_lock_id=LOCK,feather_pixels=8))).model_dump(mode='json')
    image=a.contract(a.image_ref,None)
    config=SimpleNamespace(enabled=True,model='sd15_inpaint',prompt='fill',negative_prompt='',seed=7,steps=25,guidance_scale=7.5,strength=.7)
    effective=SimpleNamespace(extensions=SimpleNamespace(generation=config,background=None))
    stage=SimpleNamespace(operation='image.generate',phase='generate')
    return a,effective,[(stage,a.raw,image,image,{})]


@pytest.mark.parametrize('backend',['cuda','mps'])
@pytest.mark.parametrize('value',[.25,np.nextafter(np.float32(0),np.float32(1))])
def test_seeded_native_generation_matches_original_and_preserves_every_positive_protected_pixel(tmp_path,backend,value):
    a,effective,verified=fixture(tmp_path,value);root=tmp_path/'scratch';root.mkdir()
    class NativePool(Pool):
        @contextmanager
        def acquire(self,model,selected_backend):
            assert model=='sd15_inpaint' and selected_backend==backend;yield self.engine
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        a.scratch=scratch;execute(context(a),a,Pool(model='sd15_inpaint'),'cuda');expected=a.output.copy()
        with prepare_region_generation(a,effective,verified,'generate',a.source,None,pool=NativePool(model='sd15_inpaint'),
                backend=backend,model_lock_sha=LOCK,profile={'backend':backend,'resource_estimate':{'ram_bytes':2<<30}},
                verify_runtime=lambda:None,check_active=lambda:None) as output:
            np.testing.assert_array_equal(output.view(np.uint32),expected.view(np.uint32))
            np.testing.assert_array_equal(output[a.protected],a.source[a.protected])
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('change',['phase','seed','steps','strength','duplicate'])
def test_generation_cannot_relabel_or_reconfigure_frozen_stage(tmp_path,change):
    _,effective,verified=fixture(tmp_path,.25)
    if change=='phase':verified[0][0].phase='background'
    elif change=='duplicate':verified*=2
    else:verified[0][1][change]+=1
    with pytest.raises(ValueError):generation_entry(effective,verified,'generate',LOCK)
