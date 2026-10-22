"""Resolve an immutable portable style for one explicitly bound working image."""
from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.style import resolve_style
from photo_worker.algorithms.statistics import coverage_counts_bounded
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage,Stats20
from photo_worker.contracts.override import OverrideModules
from photo_worker.contracts.style import Disabled
from photo_worker.contracts.style import Extensions
from photo_worker.algorithms.fitting import identity_color
from photo_worker.contracts.resolved import ResolveParams,ResolvedStyle,ResolvedModules,AbsoluteLUTRef,PrototypeWeight
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure


def validate_override_source(override, image_ref, image):
    """Support exact legacy envelope and encoded-source V2 bindings only."""
    if hasattr(override, 'input_sha256') and override.input_sha256 not in (
            image_ref.sha256, image.canvas.source_sha256):
        raise ValueError('OVERRIDE_SOURCE_MISMATCH')


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    estimate=ctx.lease.execution_profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if ctx.lease.execution_profile.get('backend')!='cpu' or type(ram) is not int or ram<3*1024**3:
        raise OperationFailure('CONFIG_INVALID')
    ctx.progress(0,4)
    try:
        params=artifacts.params(ResolveParams)
        style=artifacts.style(params.style) if params.style is not None else None
        if style is not None and style.quality.status=='example':raise ValueError('EXAMPLE_STYLE_FORBIDDEN')
        image=artifacts.contract(params.image,WorkingImage)
        # Global job overrides are applied first; the exact per-image override wins.
        patch={}
        for override in (params.job_override, params.override):
            if override is None:
                continue
            validate_override_source(override, params.image, image)
            modules=override.modules
            for field in ("strength", "global_", "local", "beauty", "defects", "geometry", "extensions"):
                value = getattr(modules, field)
                if value is not None:
                    if field == 'extensions':
                        # Match EffectiveApplyV4Style: replace named extension
                        # modules, preserving modules absent from this override.
                        base=patch.get('extensions')
                        if base is None:
                            base=style.extensions if style is not None else Extensions()
                        merged=base.model_dump(mode='json')
                        merged.update(value.model_dump(mode='json',exclude_unset=True))
                        patch[field]=Extensions.model_validate(merged)
                    else:
                        patch[field] = value
        if style is not None and patch:
            from photo_worker.contracts.style import StyleV2
            style=StyleV2.model_validate(style.model_copy(update=patch).model_dump(mode='json',by_alias=True))
        if image.color.profile_id!='work_rec2020_gamma24_v1' or image.dtype!='float32':raise ValueError('RESOLVE_IMAGE_INVALID')
        stats=artifacts.contract(params.stats,Stats20) if params.stats is not None else None
        if stats is not None and stats.source_image!=params.image:raise ValueError('RESOLVE_STATS_SOURCE_MISMATCH')
        if style is not None and style.mode=='adaptive' and stats is None:raise ValueError('ADAPTIVE_FEATURES_REQUIRED')
        counts=None
        ctx.progress(1,4)
        if style is not None and style.mode=='adaptive':
            with ExitStack() as inputs:
                def load(ref,shape,dtype):
                    local=inputs.enter_context(artifacts.input(ref,suffix='.npy'))
                    return inputs.enter_context(read_array(local.path,shape,dtype,ctx.check_active,unit_interval=dtype=='float32'))
                shape=(image.canvas.height,image.canvas.width)
                rgb=load(image.pixels,(*shape,3),'float32')
                alpha=load(image.alpha,shape,'uint16') if image.alpha is not None else None
                counts=coverage_counts_bounded(rgb,alpha=alpha,encoding='gamma24',check_active=ctx.check_active)
        ctx.check_active()
        if style is None:
            # Keep inherited job modules when an item changes only one field.
            identity_patch={('global' if name=='global_' else name):value for name,value in patch.items()}
            modules=OverrideModules.model_validate(identity_patch)
            fixed=modules.global_ or identity_color()
            local=modules.local or Disabled()
            strength=modules.strength if modules.strength is not None else 1.
            result=None
        else:
            result=resolve_style(style,features=np.array(stats.values) if stats is not None else None,coverage_counts=counts,
                person_reliable=stats is not None and stats.mask_confidence>=.75 and stats.values[15]>=.01,
                strict_ood=False,group_prototype_id=params.group_prototype_id)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(2,4)
    evidence={'mode':'fixed','neighbors':[],'distance':None,'uncovered_fraction':None,'reasons':[]} if result is None else result.evidence
    skip=result is not None and params.strict_ood and evidence['mode']=='fixed_fallback'
    outputs=[];lut=None;written=0
    if result is not None and result.absolute_lut is not None and not skip:
        grid=np.asarray(result.absolute_lut,dtype='<f4')
        if grid.shape not in ((33,33,33,3),(65,65,65,3)) or not np.isfinite(grid).all() or np.any(grid<0) or np.any(grid>1):
            raise OperationFailure('INTERNAL_ERROR')
        with artifacts.scratch.reserve(grid.nbytes+8192) as reservation:
            path=reservation.path('resolved-lut','.npy')
            with artifacts.scratch.open_writer(path,reservation) as stream:np.save(stream,grid,allow_pickle=False)
            ctx.check_active()
            ref=artifacts.stage(path,reservation,'absolute_lut','application/octet-stream')
            outputs.append(ref);written=path.stat().st_size
            lut=AbsoluteLUTRef(artifact=ref,size=grid.shape[0])
    manifest=ResolvedStyle.create(params=params,mode='skip' if skip else evidence['mode'],
        fixed=None if skip else (fixed if result is None else result.fixed),absolute_lut=lut,local=Disabled(enabled=False) if skip else (local if result is None else result.local),
        strength=strength if result is None else result.strength,modules=ResolvedModules(
            beauty=(modules.beauty or Disabled()) if result is None else style.beauty,
            defects=(modules.defects or Disabled()) if result is None else style.defects,
            geometry=(modules.geometry or Disabled()) if result is None else style.geometry,
            extensions=(modules.extensions or Extensions()) if result is None else style.extensions,
            models=[] if result is None else style.models),
        neighbors=[PrototypeWeight(**item) for item in evidence['neighbors']],distance=evidence.get('distance'),
        uncovered_fraction=evidence.get('uncovered_fraction'),reasons=evidence['reasons'],needs_review=False if result is None else result.needs_review)
    ctx.progress(3,4)
    outputs.append(artifacts.stage_contract(manifest,'resolved_style'))
    ctx.progress(4,4)
    return OperationResult(outputs=outputs,review_required=False if result is None else result.needs_review,bytes_written=written+len(canonical_bytes(manifest)))


def resolve_operations(config:ArtifactConfig)->list[Operation]:
    def resolved(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'resolved_style','absolute_lut'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('style.resolve','cpu',resolved)]
