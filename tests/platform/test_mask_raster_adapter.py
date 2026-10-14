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
    assert len(result.outputs)==7 and not result.review_required
    manifest=a.staged[0][1]
    assert a.staged[1][0]=='mask_target_ref'
    assert a.staged[1][1]==manifest.target
    assert a.staged[2][0]=="mask_guard_ref"
    assert a.staged[2][1]==manifest.protected
    assert not any(role=="mask_protected_ref" for role,_ in a.staged)
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

@pytest.mark.parametrize('role',['target','protected'])
def test_materialized_raster_must_match_original_vectors_before_upload(tmp_path,monkeypatch,role):
    from contextlib import contextmanager
    import photo_worker.operations.mask.rasterize as adapter
    original_prepare=adapter.prepare_raster
    @contextmanager
    def corrupted(*args,**kwargs):
        with original_prepare(*args,**kwargs) as files:
            array=np.load(getattr(files,role),mmap_mode='r+',allow_pickle=False)
            array[0,0]=.5  # Valid probability, wrong original rasterized sample.
            array.flush();array._mmap.close()
            yield files
    monkeypatch.setattr(adapter,'prepare_raster',corrupted)
    a=MaskArtifacts(tmp_path);root=tmp_path/'scratch';root.mkdir();ctx=context(a)
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with pytest.raises(OperationFailure) as error:execute(ctx,a)
        assert error.value.error.code=='INPUT_INVALID'
        assert not a.uploads and not a.staged and not a.outputs


def test_raster_protection_flag_is_verified_before_upload(tmp_path,monkeypatch):
    from contextlib import contextmanager
    from dataclasses import replace
    import photo_worker.operations.mask.rasterize as adapter
    original_prepare=adapter.prepare_raster
    @contextmanager
    def corrupted(*args,**kwargs):
        with original_prepare(*args,**kwargs) as files:yield replace(files,protected_nonzero=not files.protected_nonzero)
    monkeypatch.setattr(adapter,'prepare_raster',corrupted)
    a=MaskArtifacts(tmp_path);root=tmp_path/'scratch';root.mkdir();ctx=context(a)
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with pytest.raises(OperationFailure) as error:execute(ctx,a)
        assert error.value.error.code=='INPUT_INVALID' and not a.uploads and not a.staged


def test_nonempty_guard_keeps_original_learning_protection_role(tmp_path):
    a=MaskArtifacts(tmp_path)
    vectors=ManualMask(source_sha256=a.image.canvas.source_sha256,canvas=a.image.canvas,
        shapes=[Stroke(kind='stroke',sequence=0,mode='add',points=[MaskPoint(x=.5,y=.5)],radius=.1,hardness=1.),
                Stroke(kind='stroke',sequence=1,mode='protect',points=[MaskPoint(x=.5,y=.5)],radius=.04,hardness=1.)])
    a.raw['vectors']=a.add(vectors).model_dump(mode='json')
    root=tmp_path/'scratch';root.mkdir();ctx=context(a)
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    assert len(result.outputs)==8
    staged=dict(a.staged)
    manifest=staged['rasterized_mask']
    assert staged['mask_target_ref']==manifest.target
    assert staged['mask_guard_ref']==manifest.protected
    assert staged['mask_protected_ref']==manifest.protected
    assert np.any(a.outputs['mask_protected'])


@pytest.mark.parametrize("external",[False,True])
def test_v28_manual_proof_checks_original_params_and_exact_pixels(tmp_path,external):
    from photo_worker.operations.portrait.local_manual_proof import verify_manual_pair
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.manual_mask import MaskRasterParams
    a=MaskArtifacts(tmp_path);root=tmp_path/'scratch';root.mkdir();ctx=context(a)
    with ScratchSpace(root,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;execute(ctx,a)
    manifest=dict(a.staged)['rasterized_mask']
    vector_ref=ArtifactRef.model_validate_json(__import__('json').dumps(a.raw['vectors']))
    vectors=a.contract(vector_ref,ManualMask)
    vector_bytes=canonical_bytes(vectors) if external else None
    external_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(vector_bytes).hexdigest()) if external else None
    params=MaskRasterParams(image=a.image_ref,inline_vectors=None if external else vectors,vectors=external_ref,object_id=None)
    raw=canonical_bytes(params);pin=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(raw).hexdigest())
    manifest=manifest.model_copy(update={'vectors':external_ref if external else pin})
    target=a.outputs['mask_target'];guard=a.outputs['mask_protected']
    target.flags.writeable=False;guard.flags.writeable=False
    args=(a.image_ref,a.image,pin,raw,manifest,manifest.target,manifest.protected,target,guard)
    assert verify_manual_pair(*args,vectors_raw=vector_bytes) is False
    bad=target.copy();bad[0,0]=1;bad.flags.writeable=False
    with pytest.raises(ValueError,match='RASTER_VECTOR_MISMATCH'):
        verify_manual_pair(*args[:-2],bad,guard,vectors_raw=vector_bytes)
    with pytest.raises(ValueError,match='PORTRAIT_MANUAL_PARAMS_MISMATCH'):
        verify_manual_pair(a.image_ref,a.image,pin,raw+b' ',*args[4:],vectors_raw=vector_bytes)
    if external:
        with pytest.raises(ValueError,match='PORTRAIT_MANUAL_VECTORS_MISMATCH'):
            verify_manual_pair(*args,vectors_raw=vector_bytes+b' ')
    else:
        with pytest.raises(ValueError,match='PORTRAIT_MANUAL_VECTORS_MISMATCH'):
            verify_manual_pair(*args,vectors_raw=canonical_bytes(vectors))

    duplicate=b'{"image":null,'+raw[1:]
    duplicate_pin=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(duplicate).hexdigest())
    with pytest.raises(ValueError,match='Duplicate JSON key'):
        verify_manual_pair(a.image_ref,a.image,duplicate_pin,duplicate,*args[4:],vectors_raw=vector_bytes)

    from types import SimpleNamespace
    from contextlib import contextmanager
    from photo_worker.operations.portrait.local_manual_inputs import ManualPairOrigin,read_verified_manual_pair
    root_bytes=canonical_bytes(manifest)
    root_pin=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(root_bytes).hexdigest())
    original=ManualPairOrigin(root_pin,pin,external_ref)
    declared=[a.image_ref,root_pin,pin,manifest.target.artifact,manifest.protected.artifact]
    if external_ref is not None:declared.append(external_ref)
    target_path=tmp_path/'original-target.npy';guard_path=tmp_path/'original-guard.npy'
    np.save(target_path,target);np.save(guard_path,guard)
    class OriginalArtifacts:
        def contract(self,ref,model):
            assert ref==root_pin and model is type(manifest)
            return manifest
        def contract_bytes(self,ref,model):
            if ref==pin:return raw
            assert ref==external_ref
            return vector_bytes
        @contextmanager
        def input(self,ref,suffix):
            assert suffix=='.npy'
            assert ref in (manifest.target.artifact,manifest.protected.artifact)
            yield SimpleNamespace(path=target_path if ref==manifest.target.artifact else guard_path)
    read_ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=SimpleNamespace(inputs=declared)))
    assert read_verified_manual_pair(read_ctx,OriginalArtifacts(),a.image_ref,a.image,manifest.target,manifest.protected,original) is False
    # Relight can select either branch while replaying the original companion.
    from photo_worker.operations.portrait.relight_manual_authority import RelightManualMaskAuthority
    from photo_worker.contracts.relight import ImageRelightParams
    from photo_worker.contracts.base import canonical_hash
    from test_portrait_manual_admission import context as relight_context,ref as fresh_ref
    authority=RelightManualMaskAuthority(OriginalArtifacts(),SimpleNamespace(resolve=lambda *args,**kwargs:original))
    for protected,selected_mask in ((False,manifest.target),(True,manifest.protected)):
        selected_ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(selected_mask))
        relight_params=ImageRelightParams.model_validate(dict(schema_version=1,
            base=dict(asset_version_id=uuid4(),source=a.image_ref,canvas_hash='c'*64),
            depth=fresh_ref(),subject=selected_ref if not protected else fresh_ref(),
            protected=selected_ref if protected else fresh_ref(),calibration=fresh_ref(),strength=.7,
            lights=[dict(x=.5,y=.5,radius=.2,ev=1.)]))
        real_ctx=relight_context()
        real_ctx.lease.command.params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(relight_params))
        real_ctx.lease.command.inputs=declared+[selected_ref]
        assert authority.verify(real_ctx,relight_params,a.image,selected_mask,protected=protected) is True
    np.save(target_path,bad)
    with pytest.raises(ValueError,match='RASTER_VECTOR_MISMATCH'):
        authority.verify(real_ctx,relight_params,a.image,selected_mask,protected=protected)
    with pytest.raises(ValueError,match='RASTER_VECTOR_MISMATCH'):
        read_verified_manual_pair(read_ctx,OriginalArtifacts(),a.image_ref,a.image,manifest.target,manifest.protected,original)
