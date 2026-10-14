"""Scoped geometry-aware native upscale assembly; graph authority is external."""
from contextlib import contextmanager,ExitStack
from copy import deepcopy
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.restoration import UpscaleParams,UpscaleReport
from photo_worker.operations.image.prepare_upscale import prepare_upscale,upscale_geometry
from photo_worker.runtime.arrays import read_array
from .render_adjustments import AdjustmentPixels
from .render_geometry_masks import prepare_resize_masks,resize_masks_peak
from .render_protection import preserve_render_protection
from .render_resources import admit_render,render_resources


@dataclass(frozen=True)
class RenderUpscaleAssembly:
    pixels: AdjustmentPixels
    canvas: object
    transform_chain: object
    masks: object
    native: object
    report: UpscaleReport
    source: WorkingImage
    source_ref: object
    params: UpscaleParams
    backend: str


@contextmanager
def prepare_upscale_assembly(artifacts,image_ref,image,current,params,engine,entries,*,backend,execution_profile,
                             provider_peak,verify_runtime,tile_size=512,check_active=lambda:None):
    """Qualified installer owns source/model pins and engine for the full scope.

    This context neither stages a root nor grants source/mask/model authority.
    profile must admit the target canvas and all simultaneous retained owners.
    """
    image_ref=deepcopy(image_ref);profile=deepcopy(execution_profile);peak=deepcopy(provider_peak)
    if backend not in ('cpu','cuda','mps') or profile.get('backend')!=backend:
        raise ValueError('RENDER_UPSCALE_BACKEND_INVALID')
    image=WorkingImage.model_validate_json(canonical_bytes(image))
    params=UpscaleParams.model_validate_json(canonical_bytes(params))
    effects=deepcopy(current.effects)
    if (params.image!=image_ref or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
            or image.color.encoding!='gamma24' or current.rgb.shape!=(image.canvas.height,image.canvas.width,3)
            or (current.alpha is None)!=(image.alpha is None) or not callable(verify_runtime)):
        raise ValueError('RENDER_UPSCALE_SOURCE_INVALID')
    if current.alpha is not None and current.alpha.dtype!=np.float32:raise ValueError('RENDER_UPSCALE_ALPHA_INVALID')
    if not isinstance(entries,tuple):raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')
    owned_entries=[]
    for entry in entries:
        if not isinstance(entry,tuple) or len(entry)!=2 or not isinstance(entry[0],MaskRef):
            raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')
        reference=MaskRef.model_validate_json(canonical_bytes(entry[0]))
        if reference.canvas!=image.canvas or reference.transform_chain!=image.transform_chain:
            raise ValueError('RENDER_GEOMETRY_MASK_BINDING_CHANGED')
        owned_entries.append((reference,entry[1]))
    entries=tuple(owned_entries)
    if len({(ref.artifact.artifact_id,ref.artifact.sha256) for ref,_ in entries})!=len(entries):
        raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')

    canvas,chain=upscale_geometry(image,params);step=chain.steps[-1]
    check_active();verify_runtime();check_active()
    peak=upscale_assembly_peak(image,params,len(entries),has_generated=current.generated is not None,
        provider_peak=peak,tile_size=tile_size)
    pixels=canvas.width*canvas.height;source_pixels=image.canvas.width*image.canvas.height
    admit_render(profile,canvas.width,canvas.height,mask_refs=0,mask_nodes=0,provider_peak=peak,tile_size=tile_size)
    for array in (*[values for _,values in entries],*((current.generated,) if current.generated is not None else ())):
        if (not isinstance(array,np.ndarray) or array.dtype!=np.float32
                or array.shape!=(image.canvas.height,image.canvas.width)):
            raise ValueError('RENDER_GEOMETRY_MASK_PIXELS_INVALID')
        for y in range(0,array.shape[0],tile_size):
            for x in range(0,array.shape[1],tile_size):
                check_active();part=array[y:y+tile_size,x:x+tile_size]
                if not np.isfinite(part).all() or np.any(part<0) or np.any(part>1):
                    raise ValueError('RENDER_GEOMETRY_MASK_PIXELS_INVALID')
    with prepare_upscale(current.rgb,current.alpha,params,engine,artifacts.scratch,
            retain_baseline=True,check_active=check_active) as native,ExitStack() as stack:
        shape=(canvas.height,canvas.width)
        rgb=stack.enter_context(read_array(native.rgb_path,(*shape,3),'float32',check_active,unit_interval=True))
        baseline=stack.enter_context(read_array(native.baseline_path,(*shape,3),'float32',check_active,unit_interval=True))
        alpha=stack.enter_context(read_array(native.alpha_path,shape,'float32',check_active,unit_interval=True)) if native.alpha_path else None
        with prepare_resize_masks(entries,current.generated,step,image.transform_chain,rgb,artifacts.scratch,
                execution_profile=profile,tile=tile_size,check_active=check_active) as masks:
            protected=tuple(values for reference,values,_ in masks.masks if reference.protected)
            def protection(x,y,w,h):
                result=np.zeros((h,w),np.float32)
                for values in protected:np.maximum(result,values[y:y+h,x:x+w],out=result)
                return result
            candidate=AdjustmentPixels(rgb,alpha,masks.generated,effects)
            with preserve_render_protection(artifacts,AdjustmentPixels(baseline,alpha),candidate,
                    protection if protected else None,tile_size=tile_size,check_active=check_active) as restored:
                warnings=[]
                if native.source_clip:warnings.append('source_srgb_clipped')
                if native.gamut_excess:warnings.append('delta_gamut_excess')
                report=UpscaleReport(image=image_ref,model_id=params.model_id,model_lock_id=params.model_lock_id,
                    backend=backend,scale=params.scale,color_adapter_id=params.color_adapter_id,tile=params.tile,
                    overlap=params.overlap,input_pixel_count=source_pixels,output_pixel_count=pixels,
                    disabled_delta_fraction=native.disabled/pixels,source_clip_fraction=native.source_clip/pixels,
                    gamut_excess_fraction=native.gamut_excess/pixels,alpha_resampled=alpha is not None,warnings=warnings)
                check_active();yield RenderUpscaleAssembly(restored,canvas,chain,masks,native,report,image,image_ref,params,backend);check_active()


@dataclass(frozen=True)
class StagedUpscaleAssembly:
    image: object
    image_ref: object
    report_ref: object
    masks: tuple
    generated: object
    outputs: tuple
    bytes_written: int
    review_required: bool


def _same_upscale_pixels(actual,expected,check_active):
    if not isinstance(actual,np.ndarray) or actual.dtype!=np.float32 or actual.shape!=expected.shape:
        raise ValueError('RENDER_UPSCALE_CURRENT_PIXELS_CHANGED')
    for y in range(0,expected.shape[0],256):
        for x in range(0,expected.shape[1],256):
            check_active()
            if not np.array_equal(actual[y:y+256,x:x+256].view(np.uint32),expected[y:y+256,x:x+256].view(np.uint32)):
                raise ValueError('RENDER_UPSCALE_CURRENT_PIXELS_CHANGED')


def _validate_stage_pixels(value,shape,tile_size,check_active):
    if not isinstance(value,np.ndarray) or value.dtype!=np.float32 or value.shape!=shape:
        raise ValueError('RENDER_UPSCALE_STAGE_INVALID')
    for y in range(0,shape[0],tile_size):
        for x in range(0,shape[1],tile_size):
            check_active()
            part=value[y:y+tile_size,x:x+tile_size]
            if not np.isfinite(part).all() or np.any(part<0) or np.any(part>1):
                raise ValueError('RENDER_UPSCALE_STAGE_INVALID')


def stage_upscale_assembly(artifacts,prepared,*,tile_size=512,check_active=lambda:None):
    """Stage geometry intermediates in live assembly scope, not a Render root."""
    from .render_geometry_masks import publish_resize_masks,publish_resize_generated
    if not isinstance(prepared,RenderUpscaleAssembly) or type(tile_size) is not int or tile_size not in (256,512,1024):
        raise ValueError('RENDER_UPSCALE_STAGE_INVALID')
    report=UpscaleReport.model_validate_json(canonical_bytes(prepared.report))
    source=WorkingImage.model_validate_json(canonical_bytes(prepared.source))
    params=UpscaleParams.model_validate_json(canonical_bytes(prepared.params))
    canvas,chain=upscale_geometry(source,params)
    count=canvas.width*canvas.height
    expected_warnings=[]
    if prepared.native.source_clip:expected_warnings.append('source_srgb_clipped')
    if prepared.native.gamut_excess:expected_warnings.append('delta_gamut_excess')
    if (params.image!=prepared.source_ref or report.image!=prepared.source_ref
            or prepared.canvas!=canvas or prepared.transform_chain!=chain
            or report.input_pixel_count!=source.canvas.width*source.canvas.height or report.output_pixel_count!=count
            or report.backend!=prepared.backend or report.model_id!=params.model_id or report.model_lock_id!=params.model_lock_id
            or report.scale!=params.scale or report.tile!=params.tile or report.overlap!=params.overlap
            or any(type(value) is not int or not 0<=value<=count for value in
                (prepared.native.source_clip,prepared.native.gamut_excess,prepared.native.disabled))
            or report.source_clip_fraction!=prepared.native.source_clip/count
            or report.gamut_excess_fraction!=prepared.native.gamut_excess/count
            or report.disabled_delta_fraction!=prepared.native.disabled/count
            or report.warnings!=expected_warnings or report.alpha_resampled!=(prepared.pixels.alpha is not None)
            or prepared.masks.transform_chain!=chain
            or len(prepared.masks.files)!=len(prepared.masks.masks)
            or prepared.pixels.generated is not prepared.masks.generated):
        raise ValueError('RENDER_UPSCALE_STAGE_INVALID')
    arrays={'rgb':prepared.pixels.rgb,'alpha':prepared.pixels.alpha}
    for name,value in arrays.items():
        if value is None:
            if name=='rgb':raise ValueError('RENDER_UPSCALE_STAGE_INVALID')
            continue
        shape=(canvas.height,canvas.width,3) if name=='rgb' else (canvas.height,canvas.width)
        _validate_stage_pixels(value,shape,tile_size,check_active)
    for (mask,values,radius),files in zip(prepared.masks.masks,prepared.masks.files):
        if (mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain or files.radius!=radius
                or values.shape!=(canvas.height,canvas.width) or values.dtype!=np.float32):
            raise ValueError('RENDER_UPSCALE_STAGE_INVALID')
        files.arrays.validate_file(files.array);files.pngs.validate_file(files.png)
    from .render_effects import effect_review_required
    review=bool(report.warnings) or report.disabled_delta_fraction>0
    reviews=tuple(effect_review_required(item) for item in deepcopy(prepared.pixels.effects))
    review=any(reviews) or review
    outputs=[];written=0
    with artifacts.scratch.reserve(sum(value.nbytes+8192 for value in arrays.values() if value is not None)) as owner:
        refs={}
        for name,value in arrays.items():
            if value is None:refs[name]=None;continue
            path=owner.path('upscale-final-'+name,'.npy')
            output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=value.shape)
            try:
                for y in range(0,value.shape[0],tile_size):
                    check_active();output[y:y+tile_size]=value[y:y+tile_size]
                output.flush();owner.validate()
            finally:output._mmap.close()
            check_active();refs[name]=artifacts.stage(path,owner,'pixels' if name=='rgb' else 'alpha','application/x-npy')
            outputs.append(refs[name]);written+=path.stat().st_size
    image=WorkingImage(pixels=refs['rgb'],alpha=refs['alpha'],dtype='float32',canvas=prepared.canvas,
        color=source.color,transform_chain=prepared.transform_chain)
    check_active();image_ref=artifacts.stage_contract(image,'working_image')
    outputs.append(image_ref);written+=len(canonical_bytes(image))
    check_active();report_ref=artifacts.stage_contract(report,'upscale_report')
    outputs.append(report_ref);written+=len(canonical_bytes(report))
    step=prepared.transform_chain.steps[-1]
    masks=publish_resize_masks(artifacts,prepared.masks,image_ref,image,step,check_active=check_active)
    generated=publish_resize_generated(artifacts,prepared.masks,image,step,check_active=check_active)
    for item in (*masks,*((generated,) if generated is not None else ())):
        outputs.extend(item.outputs);written+=item.bytes_written
    check_active();return StagedUpscaleAssembly(image,image_ref,report_ref,masks,generated,tuple(outputs),written,review)


def upscale_assembly_peak(image,params,mask_count,*,has_generated,provider_peak,tile_size=512):
    image=WorkingImage.model_validate_json(canonical_bytes(image))
    params=UpscaleParams.model_validate_json(canonical_bytes(params))
    canvas,chain=upscale_geometry(image,params);step=chain.steps[-1];peak=deepcopy(provider_peak)
    render_resources(canvas.width,canvas.height,mask_refs=0,mask_nodes=0,provider_peak=peak,tile_size=tile_size)
    mask_peak=resize_masks_peak(step,mask_count,has_generated=has_generated,tile=tile_size)
    for key in peak:peak[key]+=mask_peak[key]
    pixels=canvas.width*canvas.height;source_pixels=image.canvas.width*image.canvas.height
    work=min(image.canvas.height,320)*min(image.canvas.width,320)*params.scale**2
    peak['ram_bytes']+=32*source_pixels+64*pixels+192*work+1073741824
    peak['scratch_bytes']+=64*pixels+1048576
    return peak


@contextmanager
def prepare_pinned_upscale(artifacts,params_ref,current,pool,*,mask_refs,backend,execution_profile,
                           provider_peak,verify_runtime,tile_size=512,expected_params=None,check_active=lambda:None):
    """Declared pinned pixels/model owner; adopted origin authority is external."""
    expected_document=None if expected_params is None else canonical_bytes(expected_params)
    from photo_worker.runtime.executor import OperationFailure
    if not isinstance(mask_refs,tuple) or len(mask_refs)>257 or not callable(verify_runtime):
        raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')
    params_ref,mask_refs=deepcopy((params_ref,mask_refs))
    profile=deepcopy(execution_profile);peak=deepcopy(provider_peak)
    if backend not in ('cpu','cuda','mps') or profile.get('backend')!=backend:raise OperationFailure('CONFIG_INVALID')
    check_active();verify_runtime();check_active()
    def owned(ref,model):
        check_active();return model.model_validate_json(canonical_bytes(artifacts.contract(ref,model)))
    params=owned(params_ref,UpscaleParams)
    if expected_document is not None and canonical_bytes(params)!=expected_document:
        raise ValueError('RENDER_UPSCALE_DOCUMENT_CHANGED')
    image=owned(params.image,WorkingImage)
    if params.model_lock_id!=pool.lock.lock_id:raise OperationFailure('MODEL_LOCK_MISMATCH')
    if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color.encoding!='gamma24':
        raise ValueError('RENDER_UPSCALE_SOURCE_INVALID')
    masks=tuple(owned(ref,MaskRef) for ref in mask_refs)
    if len({(mask.artifact.artifact_id,mask.artifact.sha256) for mask in masks})!=len(masks):
        raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')
    if any(mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain for mask in masks):
        raise ValueError('RENDER_GEOMETRY_MASK_BINDING_CHANGED')
    canvas,_=upscale_geometry(image,params)
    required=upscale_assembly_peak(image,params,len(masks),has_generated=current.generated is not None,
        provider_peak=peak,tile_size=tile_size)
    admit_render(profile,canvas.width,canvas.height,mask_refs=0,mask_nodes=0,provider_peak=required,tile_size=tile_size)
    h,w=image.canvas.height,image.canvas.width
    with ExitStack() as stack:
        def mapped(ref,shape):
            check_active();local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
            return stack.enter_context(read_array(local.path,shape,'float32',check_active,unit_interval=True))
        def same(actual,expected):
            _same_upscale_pixels(actual,expected,check_active)
        same(current.rgb,mapped(image.pixels,(h,w,3)))
        if image.alpha is None:
            if current.alpha is not None:raise ValueError('RENDER_UPSCALE_CURRENT_PIXELS_CHANGED')
        else:same(current.alpha,mapped(image.alpha,(h,w)))
        entries=tuple((mask,mapped(mask.artifact,(h,w))) for mask in masks)
        check_active()
        if params.model_lock_id!=pool.lock.lock_id:raise OperationFailure('MODEL_LOCK_MISMATCH')
        with pool.acquire(params.model_id,backend) as engine:
            with prepare_upscale_assembly(artifacts,params.image,image,current,params,engine,entries,
                    backend=backend,execution_profile=profile,provider_peak=peak,verify_runtime=verify_runtime,
                    tile_size=tile_size,check_active=check_active) as prepared:
                yield prepared;check_active()


@contextmanager
def prepare_authored_upscale(artifacts,source_ref,restoration,params_ref,current,pool,*,
        mask_refs,backend,execution_profile,provider_peak,verify_runtime,
        tile_size=512,check_active=lambda:None):
    """Bind the geometry-aware upscale stage to effective authored restoration.

    The caller proves the pinned stage source and orders all restoration stages;
    denoise/dust handling and final result publication are separate obligations.
    """
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.style import Restoration
    if (not isinstance(source_ref,ArtifactRef) or not isinstance(params_ref,ArtifactRef)
            or not isinstance(restoration,Restoration)):
        raise ValueError('RENDER_UPSCALE_AUTHORED_BINDING_INVALID')
    source_ref,params_ref,mask_refs=deepcopy((source_ref,params_ref,mask_refs))
    authored=Restoration.model_validate_json(canonical_bytes(restoration))
    profile,peak=deepcopy(execution_profile),deepcopy(provider_peak)
    check_active()
    params=UpscaleParams.model_validate_json(canonical_bytes(artifacts.contract(params_ref,UpscaleParams)))
    check_active()
    if (authored.upscale=='off' or params.image!=source_ref
            or authored.upscale!=str(params.scale)+'x'
            or params.model_id!='realesrgan_x'+str(params.scale)):
        raise ValueError('RENDER_UPSCALE_AUTHORED_BINDING_INVALID')
    with prepare_pinned_upscale(artifacts,params_ref,current,pool,mask_refs=mask_refs,
            backend=backend,execution_profile=profile,provider_peak=peak,
            verify_runtime=verify_runtime,expected_params=params,tile_size=tile_size,check_active=check_active) as result:
        yield result
        check_active()
