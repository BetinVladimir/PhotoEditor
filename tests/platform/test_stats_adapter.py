import hashlib,json
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import MaskRef,Stats20
from photo_worker.operations.color.stats import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from test_color_fit_adapter import Artifacts
from test_asset_adapters import context_for


def setup(root):
    artifacts=Artifacts(root,values=(.25,.3))
    original=artifacts.contracts[artifacts.before.artifact_id]
    def array(name,values):
        path=root/(name+'.npy');np.save(path,values)
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        artifacts.paths[ref.artifact_id]=path;return ref
    alpha=np.zeros((16,24),np.uint16);alpha[:,:12]=65535
    person=(alpha>0).astype(np.float32)
    image=original.model_copy(update={'alpha':array('alpha',alpha)})
    image_ref=artifacts.add(image)
    mask=MaskRef(artifact=array('person',person),canvas=image.canvas,transform_chain=image.transform_chain,
        object_id=None,selector='person',confidence=.5,model_lock_id=None,protected=False)
    artifacts.raw={'image':image_ref.model_dump(mode='json'),'person':mask.model_dump(mode='json')}
    ctx=context_for(image_ref,'color.stats')
    ctx.lease.execution_profile['resource_estimate']={'ram_bytes':268435456}
    return artifacts,ctx


def test_stats_adapter_excludes_transparent_pixels_and_releases_workspace(tmp_path):
    artifacts,ctx=setup(tmp_path);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,100000,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        result=execute(ctx,artifacts)
        assert list(scratch.directory.iterdir())==[]
    role,stats=artifacts.staged[0]
    assert role=='stats' and stats.values[15]==1. # opaque region is entirely person
    assert sum(stats.coverage_bins)==1 and stats.mask_confidence==.5
    assert result.review_required and stats.warnings==['person_mask_low_confidence']
    assert ctx.snapshot_progress()==dict(done=3,total=3)
    Stats20.model_validate_json(canonical_bytes(stats))


def test_stats_rejects_wrong_mask_canvas_before_pixels(tmp_path):
    artifacts,ctx=setup(tmp_path)
    artifacts.raw['person']['canvas']['space_id']='other'
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='INPUT_INVALID' and artifacts.opens==0 and not artifacts.staged
