"""Physical external-LUT replay boundary; no model/hardware substitution."""
from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.contracts.resolved import AbsoluteLUTRef
from photo_worker.operations.region.geometry_replay import verify_region_luts
from test_image_color_adapter import Artifacts


def stage(lut):return (None,None,None,None,{'resolved':SimpleNamespace(absolute_lut=lut)})


def test_real_float_grid_and_mutable_ref_repeat_are_verified_once(tmp_path):
    artifacts=Artifacts(tmp_path,True);lut=artifacts.resolved.absolute_lut
    reads=[];artifacts.require_declared_input=lambda ref:reads.append((ref.artifact_id,ref.sha256))
    verify_region_luts([stage(lut),stage(lut.model_copy(deep=True))],artifacts)
    assert reads==[(lut.artifact.artifact_id,lut.artifact.sha256)]
    # Same bytes under another exact occurrence must independently be declared.
    second=artifacts.array(tmp_path,'other-lut',artifacts.grid)
    verify_region_luts([stage(lut),stage(AbsoluteLUTRef(artifact=second,size=33))],artifacts)
    assert reads[-2:]==[(lut.artifact.artifact_id,lut.artifact.sha256),(second.artifact_id,second.sha256)]


def test_repeated_ref_cannot_replace_grid_size(tmp_path):
    artifacts=Artifacts(tmp_path,True);lut=artifacts.resolved.absolute_lut
    artifacts.require_declared_input=lambda ref:None
    with pytest.raises(ValueError,match='SHAPE_BINDING'):
        verify_region_luts([stage(lut),stage(AbsoluteLUTRef(artifact=lut.artifact.model_copy(deep=True),size=65))],artifacts)


@pytest.mark.parametrize('kind',['nan','outside','dtype','shape'])
def test_replay_rejects_physically_invalid_grid_even_with_typed_resolved_doc(tmp_path,kind):
    artifacts=Artifacts(tmp_path,True);lut=artifacts.resolved.absolute_lut
    artifacts.require_declared_input=lambda ref:None
    grid=artifacts.grid.copy()
    if kind=='nan':grid[1,2,3,0]=np.nan
    elif kind=='outside':grid[1,2,3,0]=1.01
    elif kind=='dtype':grid=grid.astype(np.float64)
    else:grid=grid[:-1]
    np.save(artifacts.paths[lut.artifact.artifact_id],grid)
    with pytest.raises(ValueError):verify_region_luts([stage(lut)],artifacts)


def test_lut_duplicate_still_observes_lost_lease(tmp_path):
    artifacts=Artifacts(tmp_path,True);lut=artifacts.resolved.absolute_lut
    artifacts.require_declared_input=lambda ref:None
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls>1:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        verify_region_luts([stage(lut),stage(lut.model_copy(deep=True))],artifacts,check_active=active)
