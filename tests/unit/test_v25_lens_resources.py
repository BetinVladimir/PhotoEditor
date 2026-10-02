import math
import pytest
from photo_worker.operations.optical.resources import lens_resources,admit_lens
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('strength',[0,1])
def test_exact_lens_admission_and_each_underbudget(strength):
    budget=lens_resources(6000,4000,strength=strength)
    assert admit_lens({'resource_estimate':budget},6000,4000,strength=strength)==budget
    for name,code in [('ram_bytes','RAM'),('scratch_bytes','SCRATCH'),('output_bytes','OUTPUT')]:
        with pytest.raises(OperationFailure,match=code):
            admit_lens({'resource_estimate':{**budget,name:budget[name]-1}},6000,4000,strength=strength)
    for bad in [{}, {**budget,'ram_bytes':True},{**budget,'pixel_count':1},{**budget,'vram_bytes':1}]:
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
            admit_lens({'resource_estimate':bad},6000,4000,strength=strength)


def test_lens_canvas_limits_and_identity_output_budget():
    assert lens_resources(100,100,strength=1)['output_bytes']-lens_resources(100,100,strength=0)['output_bytes']==240000
    for width,height in [(0,1),(True,1),(200000001,1)]:
        with pytest.raises(ValueError):lens_resources(width,height,strength=1)
    with pytest.raises(ValueError):lens_resources(2,2,strength=math.nan)
