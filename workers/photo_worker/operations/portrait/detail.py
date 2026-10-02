from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.effective import resolve_effective_params
from photo_worker.algorithms.portrait_detail import apply_region
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.portrait_detail import DetailEvidence,PortraitDetailReport,PortraitParams
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(PortraitParams);image=artifacts.contract(p.image,WorkingImage);resolved=artifacts.contract(p.resolved,ResolvedStyle);effective=resolve_effective_params(resolved);detail=effective.modules.extensions.portrait_detail
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.canvas.width*image.canvas.height>200000000:raise ValueError('Invalid portrait image')
        required=[p.image,image.pixels,*([image.alpha] if image.alpha else []),p.resolved,*[r.mask.artifact for r in p.regions],*[m.artifact for m in p.protected]]
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError('Undeclared portrait detail input')
        for entry in [r.mask for r in p.regions]+p.protected:
            if entry.canvas!=image.canvas or entry.transform_chain!=image.transform_chain:raise ValueError('Portrait mask geometry mismatch')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width;reports=[];warnings=[];outputs=[];written=0;output=image
    enabled=detail is not None and detail.enabled
    if enabled:
        with ExitStack() as stack:
            def mapped(ref):return stack.enter_context(read_array(stack.enter_context(artifacts.input(ref,suffix='.npy')).path,(h,w),'float32',ctx.check_active,unit_interval=True))
            rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));current=np.asarray(rgb).copy();protection=np.zeros((h,w),np.float32)
            for mask in p.protected:np.maximum(protection,mapped(mask.artifact),out=protection)
            for region in sorted(p.regions,key=lambda x:x.region_id):
                base={'teeth':detail.teeth_strength,'sclera':detail.teeth_strength,'shine':detail.shine_strength,'under_eye':detail.under_eye_strength,'cloth':region.strength,'glasses':region.strength}[region.kind];strength=base*region.strength if region.kind not in ('cloth','glasses') else base
                current,changed,max_ev,chroma,reason=apply_region(current,np.asarray(mapped(region.mask.artifact)),region.kind,strength,protection,ctx.check_active);reports.append(DetailEvidence(region_id=region.region_id,kind=region.kind,applied=reason is None,changed_pixels=changed,max_ev=max_ev,chroma_reduction=chroma,reason=reason))
            reserve=h*w*12+8192
            with artifacts.scratch.reserve(reserve) as reservation:
                path=reservation.path('portrait-detail-rgb','.npy');mm=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w,3));mm[:]=current;mm.flush();mm._mmap.close();reservation.validate();pixels=artifacts.stage(path,reservation,'working_rgb','application/octet-stream');written=path.stat().st_size
            output=image.model_copy(update={'pixels':pixels});outputs.append(pixels)
        if not p.regions:warnings.append('no_regions')
        if any(r.reason for r in reports):warnings.append('region_skipped')
        if any(r.kind=='glasses' and r.reason for r in reports):warnings.append('glasses_generation_required')
    else:warnings.append('module_disabled')
    image_ref=artifacts.stage_contract(output,'working_image');outputs.append(image_ref);report=PortraitDetailReport(image=p.image,resolved=p.resolved,regions=reports,alpha_preserved=image.alpha is not None,warnings=warnings);report_ref=artifacts.stage_contract(report,'portrait_detail_report');outputs.append(report_ref)
    return OperationResult(outputs=outputs,review_required=resolved.needs_review or any(w not in ('module_disabled','no_regions') for w in warnings),bytes_written=written+len(canonical_bytes(output))+len(canonical_bytes(report)))


def detail_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_image','portrait_detail_report'})) as a:return execute(ctx,a)
    return [Operation('portrait.detail','cpu',run)]
