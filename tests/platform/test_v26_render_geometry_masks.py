from uuid import uuid4
import cv2
import numpy as np
import pytest
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.restoration import UpscaleParams
from photo_worker.operations.image.prepare_upscale import upscale_geometry
from photo_worker.operations.mask.transform_prepare import prepare_transform
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.render_geometry_masks import prepare_resize_masks
from test_image_denoise import Artifacts,LOCK


def profile():
    return {'backend':'cpu','resource_estimate':dict(pixel_count=40*48,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,
        vram_bytes=0,output_bytes=8*1024**3)}


def fixture(root):
    a=Artifacts(root);params=UpscaleParams(image=a.image_ref,scale=2,model_id='realesrgan_x2',model_lock_id=LOCK)
    canvas,chain=upscale_geometry(a.image,params)
    reference=MaskRef(artifact=a.pixel_ref,canvas=a.image.canvas,transform_chain=a.image.transform_chain,
        selector='manual',confidence=1.,model_lock_id=None,protected=True)
    values=np.zeros((20,24),np.float32);values[4:16,5:19]=1
    guide=np.full((40,48,3),.4,np.float32)
    return a,reference,values,guide,chain


def test_geometry_masks_use_native_guidance_and_exact_lineage_geometry(tmp_path):
    a,ref,values,guide,chain=fixture(tmp_path)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_transform(values,chain.steps[-1],scratch,guide=guide,tile=256) as expected:
            native=np.load(expected.array).copy()
        with prepare_resize_masks(((ref,values),),values,chain.steps[-1],a.image.transform_chain,
                guide,scratch,execution_profile=profile(),tile=256) as result:
            assert result.transform_chain==chain and result.masks[0][0]==ref
            np.testing.assert_array_equal(result.masks[0][1],native)
            np.testing.assert_allclose(result.generated,cv2.resize(values,(48,40),interpolation=cv2.INTER_LINEAR),atol=1e-7)
            assert not result.masks[0][1].flags.writeable and not result.generated.flags.writeable
        assert not list(scratch.directory.iterdir())


def test_geometry_mask_binding_fails_before_owner_allocation_and_cancel_closes(tmp_path):
    a,ref,values,guide,chain=fixture(tmp_path)
    wrong=ref.model_copy(update={'canvas':chain.steps[-1].to_canvas})
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='BINDING_CHANGED'):
            with prepare_resize_masks(((wrong,values),),None,chain.steps[-1],a.image.transform_chain,guide,scratch,execution_profile=profile(),tile=256):pass
        assert not list(scratch.directory.iterdir())
        cancelled=False
        def check():
            if cancelled:raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_resize_masks(((ref,values),),values,chain.steps[-1],a.image.transform_chain,
                    guide,scratch,execution_profile=profile(),tile=256,check_active=check):cancelled=True
        assert not list(scratch.directory.iterdir())


def test_resize_publication_binds_new_pixels_to_target_canvas(tmp_path):
    import hashlib
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.v2.render_geometry_masks import publish_resize_masks
    a,source,values,guide,chain=fixture(tmp_path)
    target=a.image.model_copy(update={'canvas':chain.steps[-1].to_canvas,'transform_chain':chain})
    target_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(target)).hexdigest())
    stages=[];documents=[]
    class Publisher:
        def stage(self,path,reservation,role,media):
            reservation.validate_file(path)
            reference=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
            stages.append((role,reference));return reference
        def stage_contract(self,value,role):
            documents.append(value)
            return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_resize_masks(((source,values),),None,chain.steps[-1],a.image.transform_chain,guide,scratch,execution_profile=profile(),tile=256) as prepared:
            wrong=a.image
            with pytest.raises(ValueError,match='BINDING_CHANGED'):
                publish_resize_masks(Publisher(),prepared,target_ref,wrong,chain.steps[-1])
            assert stages==[]
            published=publish_resize_masks(Publisher(),prepared,target_ref,target,chain.steps[-1])
            result=published[0].result
            assert result.params.target==target_ref and result.mask.artifact==stages[0][1]
            assert result.mask.canvas==target.canvas and result.mask.transform_chain==chain
            assert result.mask.protected and result.mask.selector==source.selector
            assert result.mask.confidence==source.confidence and result.mask.object_id==source.object_id
            assert result.png==stages[1][1] and result.guided_radius==prepared.masks[0][2]
            assert len(published[0].outputs)==3 and published[0].bytes_written>0 and documents==[result]
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('budget',['ram_bytes','scratch_bytes','output_bytes'])
def test_resize_mask_admission_rejects_before_allocations(tmp_path,budget):
    from photo_worker.runtime.executor import OperationFailure
    a,ref,values,guide,chain=fixture(tmp_path);sealed=profile();sealed['resource_estimate'][budget]=0
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(OperationFailure,match='ADMISSION_EXCEEDED'):
            with prepare_resize_masks(((ref,values),),values,chain.steps[-1],a.image.transform_chain,guide,scratch,
                    execution_profile=sealed,tile=256):pass
        assert not list(scratch.directory.iterdir())


def test_resize_mask_peak_counts_retained_inventory_and_halo(tmp_path):
    from photo_worker.v2.render_geometry_masks import resize_masks_peak
    from photo_worker.algorithms.mask_png import png16_bound
    a,ref,values,guide,chain=fixture(tmp_path);step=chain.steps[-1]
    one=resize_masks_peak(step,1,has_generated=False,tile=256)
    two=resize_masks_peak(step,2,has_generated=False,tile=256)
    assert two['scratch_bytes']-one['scratch_bytes']==8*40*48+8192+png16_bound(48,40)
    generated=resize_masks_peak(step,2,has_generated=True,tile=256)
    assert generated['scratch_bytes']-two['scratch_bytes']==4*40*48+8192
    for count in (-1,True,258):
        with pytest.raises(ValueError):resize_masks_peak(step,count,has_generated=False)
    with pytest.raises(ValueError,match='GUIDED_WORK_BUDGET'):
        resize_masks_peak(step,1,has_generated=False,tile=256,max_work_pixels=1)


def test_resized_generated_publication_uses_target_geometry_and_existing_roles(tmp_path):
    import hashlib
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.v2.render_geometry_masks import publish_resize_generated
    a,source,values,guide,chain=fixture(tmp_path)
    target=a.image.model_copy(update={'canvas':chain.steps[-1].to_canvas,'transform_chain':chain})
    stages=[];documents=[]
    class Publisher:
        def stage(self,path,reservation,role,media):
            reservation.validate_file(path);stages.append((role,media,np.load(path).copy()))
            return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        def stage_contract(self,value,role):
            documents.append((role,value))
            return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_resize_masks((),values,chain.steps[-1],a.image.transform_chain,guide,scratch,
                execution_profile=profile(),tile=256) as prepared:
            with pytest.raises(ValueError,match='BINDING_CHANGED'):
                publish_resize_generated(Publisher(),prepared,a.image,chain.steps[-1])
            assert stages==[]
            result=publish_resize_generated(Publisher(),prepared,target,chain.steps[-1])
            assert result.mask.canvas==target.canvas and result.mask.transform_chain==chain
            assert result.mask.selector=='generated' and not result.mask.protected
            assert stages[0][:2]==('mask_pixels','application/x-npy') and documents==[('mask',result.mask)]
            np.testing.assert_array_equal(stages[0][2],prepared.generated)
            assert len(result.outputs)==2 and result.bytes_written>0
        with prepare_resize_masks((),None,chain.steps[-1],a.image.transform_chain,guide,scratch,
                execution_profile=profile(),tile=256) as prepared:
            assert publish_resize_generated(Publisher(),prepared,target,chain.steps[-1]) is None
        assert len(stages)==1 and not list(scratch.directory.iterdir())


@pytest.mark.parametrize('budget_ok',[True,False])
def test_upscale_assembly_admits_and_retains_geometry_protection_and_lineage(tmp_path,budget_ok):
    from types import SimpleNamespace
    from photo_worker.contracts.restoration import UpscaleParams
    from photo_worker.v2.render_upscale import prepare_upscale_assembly
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    from photo_worker.runtime.executor import OperationFailure
    a,ref,values,guide,chain=fixture(tmp_path)
    params=UpscaleParams(image=a.image_ref,scale=2,model_id='realesrgan_x2',model_lock_id=LOCK)
    class Engine:
        calls=0
        def infer(self,value):
            self.calls+=1
            return np.clip(cv2.resize(value,(value.shape[1]*2,value.shape[0]*2),interpolation=cv2.INTER_CUBIC)+.03,0,1)
    engine=Engine();sealed=profile();verified=[]
    if not budget_ok:sealed['resource_estimate']['ram_bytes']=0
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:
        def prepare():
            return prepare_upscale_assembly(SimpleNamespace(scratch=scratch),a.image_ref,a.image,
                AdjustmentPixels(a.source,None,values),params,engine,((ref,values),),backend='cpu',execution_profile=sealed,
                provider_peak=dict(ram_bytes=0,scratch_bytes=0,vram_bytes=0,output_bytes=0),
                verify_runtime=lambda:verified.append('runtime'),tile_size=256)
        if not budget_ok:
            with pytest.raises(OperationFailure):
                with prepare():pass
            assert engine.calls==0
        else:
            with prepare() as result:
                assert result.canvas==chain.steps[-1].to_canvas and result.transform_chain==chain
                assert result.report.image==a.image_ref and result.report.output_pixel_count==40*48
                assert result.report.model_lock_id==LOCK and result.report.backend=='cpu'
                baseline=np.load(result.native.baseline_path)
                protected=result.masks.masks[0][1]>0
                np.testing.assert_array_equal(result.pixels.rgb[protected].view(np.uint32),baseline[protected].view(np.uint32))
                assert result.pixels.rgb[~protected].mean()>baseline[~protected].mean()
                assert result.pixels.generated is result.masks.generated
                assert not result.pixels.rgb.flags.writeable and result.native.baseline_path.exists()
                import hashlib
                from photo_worker.contracts.base import ArtifactRef,canonical_bytes
                from photo_worker.v2.render_upscale import stage_upscale_assembly
                staged_values=[];writes=[]
                class Publisher:
                    def __init__(self):self.scratch=scratch
                    def stage(self,path,owner,role,media):
                        owner.validate_file(path);writes.append(role)
                        return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
                    def stage_contract(self,value,role):
                        staged_values.append((role,value));writes.append(role)
                        return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())
                from dataclasses import replace
                for altered in (
                        replace(result,report=result.report.model_copy(update={'model_lock_id':'c'*64})),
                        replace(result,report=result.report.model_copy(update={'disabled_delta_fraction':.5})),
                        replace(result,pixels=AdjustmentPixels(np.full((40,48,3),np.nan,np.float32),None,result.pixels.generated)),
                        replace(result,pixels=AdjustmentPixels(result.pixels.rgb[:1],None,result.pixels.generated))):
                    with pytest.raises(ValueError,match='STAGE_INVALID'):
                        stage_upscale_assembly(Publisher(),altered,tile_size=256)
                    assert writes==[]
                staged=stage_upscale_assembly(Publisher(),result,tile_size=256)
                assert staged.image.canvas==result.canvas and staged.image.transform_chain==chain
                assert staged.masks[0].result.params.target==staged.image_ref
                assert staged.generated.mask.canvas==staged.image.canvas and len(staged.outputs)==8
                assert staged.bytes_written>0 and staged_values[1][0]=='upscale_report'

            assert engine.calls==1
        assert verified==['runtime'] and not list(scratch.directory.iterdir())


@pytest.mark.parametrize('variant',['pass','changed','lock','budget'])
def test_pinned_upscale_loads_declared_masks_and_denies_before_model(tmp_path,variant):
    import hashlib
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.v2.render_upscale import prepare_authored_upscale
    from photo_worker.contracts.style import Restoration
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    from photo_worker.runtime.executor import OperationFailure
    from test_image_denoise import Pool
    a,mask,values,guide,chain=fixture(tmp_path)
    path=tmp_path/'declared-mask.npy';np.save(path,values)
    pixels=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    mask=mask.model_copy(update={'artifact':pixels});a.paths[pixels.artifact_id]=path
    mask_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(mask)).hexdigest())
    a.manifests[mask_ref.artifact_id]=mask
    params=UpscaleParams(image=a.image_ref,scale=2,model_id='realesrgan_x2',model_lock_id=LOCK)
    params_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(params)).hexdigest())
    a.manifests[params_ref.artifact_id]=params
    pool=Pool(model='realesrgan_x2');calls=[]
    def infer(value):
        calls.append('infer')
        return np.clip(cv2.resize(value,(value.shape[1]*2,value.shape[0]*2),interpolation=cv2.INTER_CUBIC)+.03,0,1)
    pool.engine.infer=infer
    current=a.source.copy()
    if variant=='changed':current[0,0,0]=.1
    if variant=='lock':pool.lock.lock_id='c'*64
    sealed=profile();sealed['backend']='cuda'
    if variant=='budget':sealed['resource_estimate']['ram_bytes']=0
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch
        def prepare():
            return prepare_authored_upscale(a,a.image_ref,Restoration(denoise='off',denoise_strength=0,
                upscale='2x',sensor_dust='off'),params_ref,AdjustmentPixels(current,None),pool,mask_refs=(mask_ref,),
                backend='cuda',execution_profile=sealed,provider_peak=dict(ram_bytes=0,scratch_bytes=0,vram_bytes=0,output_bytes=0),
                verify_runtime=lambda:None,tile_size=256)
        if variant=='pass':
            with prepare() as result:
                assert result.report.image==a.image_ref and result.masks.masks[0][0]==mask
                assert result.pixels.rgb.shape==(40,48,3)
                protected=result.masks.masks[0][1]>0
                np.testing.assert_array_equal(result.pixels.rgb[protected],np.load(result.native.baseline_path)[protected])
            assert calls==['infer']
        else:
            with pytest.raises((ValueError,OperationFailure)):
                with prepare():pass
            assert calls==[]
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('kind',['dtype','shape','nan','range','generated'])
def test_upscale_masks_are_preflighted_before_inference(tmp_path,kind):
    from types import SimpleNamespace
    from photo_worker.v2.render_upscale import prepare_upscale_assembly
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    a,ref,values,guide,chain=fixture(tmp_path)
    params=UpscaleParams(image=a.image_ref,scale=2,model_id='realesrgan_x2',model_lock_id=LOCK)
    invalid=values.copy();generated=None
    if kind=='dtype':invalid=invalid.astype(np.float64)
    elif kind=='shape':invalid=invalid[:1]
    elif kind=='nan':invalid[0,0]=np.nan
    elif kind=='range':invalid[0,0]=1.01
    else:generated=invalid.copy();generated[0,0]=np.inf
    calls=[]
    engine=SimpleNamespace(infer=lambda value:calls.append('infer'))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='MASK_PIXELS_INVALID'):
            with prepare_upscale_assembly(SimpleNamespace(scratch=scratch),a.image_ref,a.image,
                    AdjustmentPixels(a.source,None,generated),params,engine,((ref,invalid),),backend='cpu',
                    execution_profile=profile(),provider_peak=dict(ram_bytes=0,scratch_bytes=0,vram_bytes=0,output_bytes=0),
                    verify_runtime=lambda:None,tile_size=256):pass
        assert calls==[] and not list(scratch.directory.iterdir())


def test_upscale_stage_validation_bounds_wide_canvas_temporaries(monkeypatch):
    from photo_worker.v2.render_upscale import _validate_stage_pixels
    values=np.zeros((3,2049,3),np.float32)
    original=np.isfinite
    shapes=[]
    def bounded(part):
        shapes.append(part.shape)
        assert part.shape[0]<=256 and part.shape[1]<=256
        return original(part)
    monkeypatch.setattr(np,'isfinite',bounded)
    _validate_stage_pixels(values,values.shape,256,lambda:None)
    assert len(shapes)==9 and shapes[-1]==(3,1,3)
    values[2,-1,0]=np.nan
    with pytest.raises(ValueError,match='RENDER_UPSCALE_STAGE_INVALID'):
        _validate_stage_pixels(values,values.shape,256,lambda:None)
    values[2,-1,0]=0
    calls=[]
    def cancelled():
        calls.append(1)
        if len(calls)==2:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        _validate_stage_pixels(values,values.shape,256,cancelled)


def test_upscale_source_comparison_is_bounded_and_bit_exact(monkeypatch):
    from photo_worker.v2.render_upscale import _same_upscale_pixels
    expected=np.zeros((2,1025,3),np.float32);actual=expected.copy()
    original=np.array_equal;shapes=[]
    def bounded(a,b):
        shapes.append(a.shape)
        assert a.shape[0]<=256 and a.shape[1]<=256
        return original(a,b)
    monkeypatch.setattr(np,'array_equal',bounded)
    _same_upscale_pixels(actual,expected,lambda:None)
    assert len(shapes)==5 and shapes[-1]==(2,1,3)
    actual[-1,-1,0]=-0.0
    with pytest.raises(ValueError,match='RENDER_UPSCALE_CURRENT_PIXELS_CHANGED'):
        _same_upscale_pixels(actual,expected,lambda:None)
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        _same_upscale_pixels(expected,expected,cancelled)


@pytest.mark.parametrize('change',['source','scale','off'])
def test_authored_upscale_refuses_recipe_substitution(change):
    from types import SimpleNamespace
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.style import Restoration
    from photo_worker.v2.render_upscale import prepare_authored_upscale
    source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=UpscaleParams(image=source,scale=2,model_id='realesrgan_x2',model_lock_id=LOCK)
    authored=Restoration(denoise='off',denoise_strength=0,upscale='2x',sensor_dust='off')
    if change=='source':source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    if change=='scale':authored.upscale='4x'
    if change=='off':authored.upscale='off'
    with pytest.raises(ValueError,match='AUTHORED_BINDING_INVALID'):
        with prepare_authored_upscale(SimpleNamespace(contract=lambda *args:params),source,authored,
                ArtifactRef(artifact_id=uuid4(),sha256='b'*64),object(),object(),mask_refs=(),
                backend='cuda',execution_profile={},provider_peak={},verify_runtime=lambda:None):
            pytest.fail('Unauthored upscale executed')
