import pytest
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.render_resources import validate_render_execution_profile
from test_v26_render_authored import fixture
from photo_worker.v2.render_execute import execute_profiled_authored_render


@pytest.mark.parametrize('field,value',[(None,None),('backend','mps'),('renderer_version','v2.3'),
    ('render_runtime_sha','b'*64),('model_lock_sha','e'*64),('icc_lock_sha','e'*64),
    ('tile_size',True),('tile_size',256),('alpha_mode','normalize_uint16')])
def test_profile_pins_match_exact_frozen_runtime(tmp_path,field,value):
    artifacts,inputs,_,_,_,_=fixture(tmp_path)
    profile=dict(backend='cuda',renderer_version='v2.2',render_runtime_sha='a'*64,
        model_lock_sha=inputs.recipe.scalars.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,
        tile_size=512,alpha_mode='require_float32')
    try:
        if field is None:
            validate_render_execution_profile(profile,inputs.recipe,backend='cuda',runtime_sha='a'*64)
        else:
            profile[field]=value
            with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
                validate_render_execution_profile(profile,inputs.recipe,backend='cuda',runtime_sha='a'*64)
            with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
                execute_profiled_authored_render(artifacts,inputs,(),(),None,None,None,None,
                    backend='cuda',runtime_lock_path=tmp_path/'missing-lock',runtime_lock_sha='a'*64,
                    execution_profile=profile)
    finally:artifacts.scratch.cleanup()
