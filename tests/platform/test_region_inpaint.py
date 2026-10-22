from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_image_inpaint import Artifacts,Pool,LOCK,context
from photo_worker.operations.image.inpaint import execute
from photo_worker.operations.region.inpaint import prepare_region_inpaint,inpaint_entry
from photo_worker.runtime.scratch import ScratchSpace


def fixture(tmp_path,protected_value):
    a=Artifacts(tmp_path,protected_value=protected_value)
    a.require_declared_input=lambda ref:None
    image=a.contract(a.image_ref,None)
    effective=SimpleNamespace(extensions=SimpleNamespace(inpainting=SimpleNamespace(enabled=True,model='big_lama')))
    verified=[(SimpleNamespace(operation='image.inpaint'),a.raw,image,image,{})]
    return a,effective,verified


@pytest.mark.parametrize('protected_value',[1,.25,np.nextafter(np.float32(0),np.float32(1))])
@pytest.mark.parametrize('backend',['cuda','mps'])
def test_native_inpaint_matches_production_math_and_preserves_positive_protection(tmp_path,protected_value,backend):
    a,effective,verified=fixture(tmp_path,protected_value)
    root=tmp_path/'scratch';root.mkdir()
    class NativePool(Pool):
        @contextmanager
        def acquire(self,model,selected_backend):
            assert model=='big_lama' and selected_backend==backend
            yield self.engine
    verified_runtime=[]
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        a.scratch=scratch;execute(context(a),a,Pool(),'cuda')
        expected=a.output.copy()
        with prepare_region_inpaint(a,effective,verified,a.source,None,pool=NativePool(),backend=backend,
                model_lock_sha=LOCK,profile={'backend':backend,'resource_estimate':{'ram_bytes':2<<30}},
                verify_runtime=lambda:verified_runtime.append(True),check_active=lambda:None) as output:
            np.testing.assert_array_equal(output.view(np.uint32),expected.view(np.uint32))
            np.testing.assert_array_equal(output[a.protected].view(np.uint32),a.source[a.protected].view(np.uint32))
            assert not output.flags.writeable
        assert not list(scratch.directory.iterdir())
    assert len(verified_runtime)>=3


@pytest.mark.parametrize('failure',['missing','duplicate','model_lock','geometry'])
def test_native_inpaint_rejects_unbound_stage(failure,tmp_path):
    _,effective,verified=fixture(tmp_path,.25)
    if failure=='missing':verified=[]
    elif failure=='duplicate':verified*=2
    elif failure=='model_lock':verified[0][1]['model_lock_id']='f'*64
    else:
        old=verified[0];image=old[3].model_copy(update={'alpha':old[3].pixels})
        verified=[(*old[:3],image,old[4])]
    with pytest.raises(ValueError):inpaint_entry(effective,verified,LOCK)
