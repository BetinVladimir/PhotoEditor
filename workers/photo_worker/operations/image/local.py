"""Finish semantic/region color and blend once with the aligned base."""
from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.color import apply_local, apply_series, apply_strength, ColorDiagnostics
from photo_worker.algorithms.regions import apply_regions
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.image_local import LocalParams, LocalReport, RegionSkip, LocalClipping
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.series import SeriesCorrections
from photo_worker.contracts.style import Disabled
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts, ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationResult, OperationFailure


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    profile=ctx.lease.execution_profile; estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    tile=profile.get('tile_size',512)
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<536870912 or type(tile) is not int or tile not in (256,512,1024):
        raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(LocalParams)
        image=artifacts.contract(params.image,WorkingImage)
        base=artifacts.contract(params.base_aligned,WorkingImage)
        resolved=artifacts.contract(params.resolved,ResolvedStyle)
        if resolved.mode=='skip':raise OperationFailure('STYLE_OOD_SKIP')
        params.validate_geometry(image,base)
        source=base if params.base_aligned==resolved.params.image else artifacts.contract(resolved.params.image,WorkingImage)
        if image.color!=source.color or image.canvas.source_sha256!=source.canvas.source_sha256:
            raise ValueError('Style source mismatch')
        if image.alpha is not None and image.alpha not in ctx.lease.command.inputs:
            raise ValueError('Undeclared alpha')
        h,w=image.canvas.height,image.canvas.width
        if h*w>200000000:raise ValueError('Pixel limit')
        rules=resolved.modules.extensions.region_adjustments
        if len({r.region_id for r in rules})!=len(rules):raise ValueError('Duplicate regional rule')
        bindings={r.region_id:r for r in params.regions}
        if set(bindings)-{r.region_id for r in rules}:raise ValueError('Unused regional binding')
        correction=None
        if params.series is not None:
            if params.item_id is None:raise ValueError('Series correction requires item_id')
            series=artifacts.contract(params.series,SeriesCorrections)
            matches=[value for value in series.items if value.item_id==params.item_id]
            if len(matches)!=1:raise ValueError('Series item correction missing or ambiguous')
            correction=matches[0]
        elif params.item_id is not None:raise ValueError('item_id is only valid with series correction')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    local=resolved.local; local_skip=None
    if not local.enabled:local_skip='disabled'
    elif params.person is None:local_skip='missing_mask'
    elif params.person.confidence<.75:local_skip='mask_unreliable'
    if local_skip is not None:local=Disabled()
    skips=[]; usable=[]
    for rule in rules:
        binding=bindings.get(rule.region_id)
        mask=None if binding is None else binding.mask
        if binding is not None and binding.invert and rule.selector!='background':
            raise OperationFailure('INPUT_INVALID')
        expected='person' if binding is not None and binding.invert else rule.selector
        reason=('missing_mask' if mask is None else 'selector_mismatch' if mask.selector!=expected
                else 'mask_unreliable' if mask.confidence<.75 else None)
        if reason:skips.append(RegionSkip(region_id=rule.region_id,reason=reason))
        else:usable.append((rule,mask,binding.invert))
    sums={}
    total=((h+tile-1)//tile)*((w+tile-1)//tile)+2;ctx.progress(0,total)
    with ExitStack() as stack:
        def mapped(ref,shape):
            item=stack.enter_context(artifacts.input(ref,suffix='.npy'))
            return stack.enter_context(read_array(item.path,shape,'float32',ctx.check_active,unit_interval=True))
        rgb=mapped(image.pixels,(h,w,3)); original=mapped(base.pixels,(h,w,3))
        person=mapped(params.person.artifact,(h,w)) if local.enabled else None
        # Mask mappings are files, not full RAM arrays; all artifacts reserve scratch.
        masks=[(rule,mapped(mask.artifact,(h,w)),invert) for rule,mask,invert in usable]
        with artifacts.scratch.reserve(h*w*12+8192) as reservation:
            path=reservation.path('local-rgb','.npy')
            target=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w,3))
            try:
                done=0
                for y in range(0,h,tile):
                    for x in range(0,w,tile):
                        ctx.check_active();sl=(slice(y,y+tile),slice(x,x+tile))
                        diagnostic=ColorDiagnostics()
                        source=rgb[sl]
                        if correction is not None:
                            source=apply_series(source,correction.applied_ev,correction.applied_gains,diagnostic)
                        output=apply_local(source,None if person is None else person[sl],local,diagnostic)
                        if masks:output=apply_regions(output,[(r,1-m[sl] if invert else m[sl]) for r,m,invert in masks],diagnostic=diagnostic,check_active=ctx.check_active).rgb
                        target[sl]=apply_strength(original[sl],output,resolved.strength)
                        for stage,values in diagnostic.clipping.items():
                            counts=sums.setdefault(stage,[0,0]);size=rgb[sl].size
                            counts[0]+=round(values["below_zero"]*size);counts[1]+=round(values["above_one"]*size)
                        done+=1;ctx.progress(done,total)
                target.flush();reservation.validate()
            finally:target._mmap.close()
            pixel_ref=artifacts.stage(path,reservation,'working_rgb','application/octet-stream')
            written=path.stat().st_size
    output_image=WorkingImage(pixels=pixel_ref,canvas=image.canvas,color=image.color,dtype='float32',alpha=image.alpha,transform_chain=image.transform_chain)
    report=LocalReport(image=params.image,base_aligned=params.base_aligned,resolved=params.resolved,series=params.series,
        series_applied=bool(correction and correction.accepted and correction.reason!='reference'),
        series_reason=None if correction is None else correction.reason,pixel_count=h*w,
        strength=resolved.strength,local_applied=local.enabled,local_skip=local_skip,region_skips=skips,
        clipping=[LocalClipping(stage=s,below_zero=c[0]/(h*w*3),above_one=c[1]/(h*w*3)) for s,c in sums.items()])
    image_ref=artifacts.stage_contract(output_image,'working_image');ctx.progress(total-1,total)
    report_ref=artifacts.stage_contract(report,'local_report');ctx.progress(total,total)
    return OperationResult(outputs=[pixel_ref,image_ref,report_ref],review_required=resolved.needs_review or bool(skips) or local_skip in ('missing_mask','mask_unreliable'),
        bytes_written=written+len(canonical_bytes(output_image))+len(canonical_bytes(report)))


def image_local_operations(config: ArtifactConfig) -> list[Operation]:
    def localized(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_image','local_report'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('image.local','cpu',localized)]
