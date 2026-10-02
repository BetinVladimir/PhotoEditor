"""Lens worker execution; authoritative profile admission is injected by runtime."""
from contextlib import ExitStack
from typing import Protocol
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array,read_alpha_array
from photo_worker.runtime.executor import OperationFailure,OperationResult
from photo_worker.algorithms.lens import match_lens_metadata
from .contracts import OpticalLensParams,LensProfile
from .prepare import prepare_source_lens
from .result import stage_lens_result


class LensAdmission(Protocol):
    def admit(self,ctx,params:OpticalLensParams,profile:LensProfile)->tuple[str,str,float,float]:
        """Verify declared qualification bytes and return authoritative EXIF values.

        Implementations must bind metadata to params.base.asset_version_id/source,
        profile qualification SHA, operation/version/runtime locks, and approved
        photographic evidence. User-provided strings or profile fields are not
        authoritative metadata. Missing qualification raises OperationFailure.
        """
        ...


def execute(ctx,artifacts,admission:LensAdmission|None)->OperationResult:
    execution=ctx.lease.execution_profile
    if execution.get('renderer_version')!='v2.2' or execution.get('backend')!='cpu':
        raise OperationFailure('CONFIG_INVALID')
    if admission is None:raise OperationFailure('LENS_PROFILE_UNAVAILABLE')
    try:
        params=artifacts.params(OpticalLensParams)
        source=artifacts.contract(params.base.source,WorkingImage)
        profile=artifacts.contract(params.profile,LensProfile)
        if canonical_hash(source.canvas)!=params.base.canvas_hash:
            raise ValueError('LENS_SOURCE_MISMATCH')
        if source.dtype!='float32':raise ValueError('LENS_SOURCE_INVALID')
        from .resources import admit_lens
        admit_lens(execution,source.canvas.width,source.canvas.height,strength=params.strength)
        ctx.check_active()
        camera,lens,focal,aperture=admission.admit(ctx,params,profile)
        match_lens_metadata(profile,camera,lens,focal,aperture)
        if abs(params.focal_mm-focal)>1e-6 or abs(params.aperture-aperture)>1e-6:
            raise ValueError('LENS_PROFILE_MISMATCH')
        height,width=source.canvas.height,source.canvas.width
        ctx.progress(0,3)
        with ExitStack() as inputs:
            path=inputs.enter_context(artifacts.input(source.pixels,suffix='.npy')).path
            rgb=inputs.enter_context(read_array(path,(height,width,3),'float32',ctx.check_active))
            alpha=None
            if source.alpha is not None:
                path=inputs.enter_context(artifacts.input(source.alpha,suffix='.npy')).path
                alpha=inputs.enter_context(read_alpha_array(path,(height,width),ctx.check_active))
            with prepare_source_lens(source,profile,rgb,alpha,params.strength,artifacts.scratch,
                                     check_active=ctx.check_active) as files:
                ctx.progress(1,3)
                staged=stage_lens_result(artifacts,params.base.source,source,params.base,files,strength=params.strength)
        ctx.progress(3,3)
        return OperationResult(outputs=[staged.document],review_required=staged.review_required,bytes_written=staged.bytes_written)
    except ValueError as error:
        code=str(error)
        raise OperationFailure(code if code in {'LENS_PROFILE_MISMATCH','LENS_NON_INVERTIBLE','LENS_SOURCE_MISMATCH'} else 'INPUT_INVALID') from error


def lens_operations(config,registry):
    """Install only with explicit operator pins; an empty registry admits nothing."""
    if registry is None or not registry.profiles:return []
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.executor import Operation
    from .admission import PinnedLensAdmission
    from .metadata_source import ArtifactLensMetadataResolver
    def run(ctx):
        roles=frozenset({'document','working_image','pixels','alpha','warp_field','mask_pixels','mask','report'})
        with TaskArtifacts.create(ctx,config,roles) as artifacts:
            resolver=ArtifactLensMetadataResolver.from_declared_inputs(artifacts,ctx.check_active)
            admission=PinnedLensAdmission(artifacts,registry.admission_pins(),resolver,
                canonical_hash(registry.runtime),runtime_lock=registry.runtime)
            return execute(ctx,artifacts,admission)
    return [Operation('optical.lens','cpu',run)]
