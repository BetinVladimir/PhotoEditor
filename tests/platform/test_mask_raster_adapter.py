import hashlib
from uuid import uuid4
import numpy as np
import pytest
from PIL import Image
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.manual_mask import ManualMask,Stroke,MaskPoint
from photo_worker.operations.mask.rasterize import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from test_image_color_adapter import Artifacts,context

class MaskArtifacts(Artifacts):
    def __init__(self,root):
        super().__init__(root,False)
        vectors=ManualMask(source_sha256=self.image.canvas.source_sha256,canvas=self.image.canvas,
            shapes=[Stroke(kind='stroke',sequence=0,mode='add',points=[MaskPoint(x=.5,y=.5)],radius=.05,hardness=.5)])
        self.raw=dict(image=self.image_ref.model_dump(mode='json'),vectors=self.add(vectors).model_dump(mode='json'))
        self.outputs={}
    def stage(self,path,reservation,role,media_type):
        reservation.validate_file(path)
        if media_type=='image/png':
            with Image.open(path) as image:self.outputs[role]=np.asarray(image).copy()
        else:self.outputs[role]=np.load(path,allow_pickle=False)
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.uploads.append(ref);return ref


def test_raster_task_publishes_complete_typed_artifacts(tmp_path):
    a=MaskArtifacts(tmp_path);root=tmp_path/'scratch';root.mkdir();ctx=context(a)
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    assert len(result.outputs)==5 and not result.review_required
    manifest=a.staged[0][1]
    assert manifest.target.canvas==a.image.canvas and manifest.target.selector=='manual'
    assert manifest.protected.protected and not manifest.target.protected
    assert manifest.target.model_lock_id is None and manifest.target.confidence==1.
    assert np.any(a.outputs['mask_target']>0) and not np.any(a.outputs['mask_protected'])
    assert np.array_equal(a.outputs['mask_target_png'],np.rint(a.outputs['mask_target'].astype(np.float64)*65535).astype(np.uint16))
    assert ctx.snapshot_progress()==dict(done=2,total=2)


def test_stale_source_prevents_all_uploads(tmp_path):
    a=MaskArtifacts(tmp_path)
    vectors=next(v for v in a.contracts.values() if isinstance(v,ManualMask))
    other=vectors.model_copy(update={'source_sha256':'f'*64,'canvas':vectors.canvas.model_copy(update={'source_sha256':'f'*64})})
    a.raw['vectors']=a.add(other).model_dump(mode='json')
    with pytest.raises(OperationFailure) as e:execute(context(a),a)
    assert e.value.error.code=='OVERRIDE_SOURCE_MISMATCH' and not a.uploads

def test_inline_vectors_are_bound_to_exact_params_artifact(tmp_path):
    a=MaskArtifacts(tmp_path);vectors=next(v for v in a.contracts.values() if isinstance(v,ManualMask));a.raw={'image':a.image_ref.model_dump(mode='json'),'inline_vectors':vectors.model_dump(mode='json')}
    root=tmp_path/'scratch';root.mkdir();ctx=context(a);params=ArtifactRef(artifact_id=uuid4(),sha256='d'*64);ctx.lease.command.params=params
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:a.scratch=scratch;execute(ctx,a)
    assert a.staged[0][1].vectors==params
