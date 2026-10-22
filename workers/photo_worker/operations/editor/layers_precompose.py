"""Execute authored layers once on their original pixels, before full Style.

This explicit prestage never claims to be the complete frozen recipe render.
Its frozen original recipe remains an admitted provenance root. No scalar or
Style base effect is applied here; subsequent Apply stages execute them once.
"""
from contextlib import contextmanager
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.editor_precompose import EditorLayersPrecomposeParams
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from photo_worker.v2.render_inputs import load_frozen_render_inputs
from photo_worker.v2.render_layer_params import load_render_layer_params
from photo_worker.v2.render_prepare import prepare_render_pixels
from photo_worker.v2.render_publish import publish_prepared_render

class _RoleArtifacts:
    """Use canonical Apply-compatible roles without modifying render staging."""
    def __init__(self, artifacts, source, source_ref):
        self._artifacts=artifacts;self.source=source;self.source_ref=source_ref;self.extra=[];self.extra_bytes=0
    def __getattr__(self,name):return getattr(self._artifacts,name)
    def stage_contract(self,value,role):
        if role=='document' and isinstance(value,ImageResult) and value.working_image==self.source_ref:
            # Apply stages require a canonical task-owned WorkingImage even
            # when the authored inventory produced an exact identity result.
            reference=self._artifacts.stage_contract(self.source,'working_image')
            self.extra.append(reference);self.extra_bytes+=len(canonical_bytes(self.source))
            value=value.model_copy(update={'working_image':reference})
        return self._artifacts.stage_contract(value,{'document':'image_result','report':'image_report'}.get(role,role))

def validate_precompose_compute_binding(inputs,core,profile):
    """Bind actual recipe scalars to the existing nested six-pin profile ABI."""
    if (inputs.recipe.scalars.model_lock_sha!=core.pin.model_lock_sha
            or inputs.recipe.scalars.icc_lock_sha!=core.icc
            or profile.get('backend')!=core.backend
            or profile.get('native_qualification_pin')!=core.pin.__dict__
            or profile.get('icc_lock_sha')!=core.icc):
        raise ValueError('EDITOR_PRECOMPOSE_ACTUAL_DEPLOYMENT_CHANGED')

@contextmanager
def prepare_layers_precompose(ctx,artifacts,core,params):
    """The same owned pixel preparation, before any result publication.

    Operator callers use a verified NativeRenderComputeCore, never a fabricated
    NativeRenderInstallation or SQL admission callback. Worker callers retain
    both original admission/qualification checks around this context.
    """
    from photo_worker.v2.render_native_compute import NativeRenderComputeCore
    if not isinstance(core,NativeRenderComputeCore):raise ValueError('NATIVE_COMPUTE_CORE_REQUIRED')
    core.verify();ctx.check_active()
    inputs=load_frozen_render_inputs(artifacts,params.original_recipe,check_active=ctx.check_active)
    if inputs.recipe.recipe.source!=params.source or inputs.recipe.recipe.executed_base_receipt is not None:
        raise ValueError('EDITOR_PRECOMPOSE_SOURCE_CHANGED')
    validate_precompose_compute_binding(inputs,core,ctx.lease.execution_profile)
    # Load the real complete native layer inventory and strict pinned Params.
    # The assembly's Style base callback is deliberately never invoked.
    providers=core.assembly().worker(ctx,artifacts,inputs)
    layers=load_render_layer_params(artifacts,inputs,providers['operators'],
        max_document_bytes=core.document_bytes,check_active=ctx.check_active)
    @contextmanager
    def original_base(state,original):
        ctx.check_active();yield original;ctx.check_active()
    source_parent=artifacts.contract(params.source_result,ImageResult) if params.source_result is not None else None
    if source_parent is not None and source_parent.working_image!=params.source.source:
        raise ValueError("EDITOR_PRECOMPOSE_PARENT_SOURCE_CHANGED")
    tile=ctx.lease.execution_profile['tile_size']
    with prepare_render_pixels(artifacts,inputs,layers,original_base,providers['render_proposal'],
            max_input_bytes=core.input_bytes,max_mask_bytes=core.mask_bytes,
            max_tile_bytes=core.tile_bytes,tile_size=tile,
            execution_profile=ctx.lease.execution_profile,source_result=source_parent,provider_peak=core.provider_peak_for(inputs,ctx.lease.execution_profile),
            check_active=ctx.check_active) as prepared:
        h,w=inputs.source.canvas.height,inputs.source.canvas.width
        clipping=0;outside=0.
        with artifacts.input(inputs.source.pixels,suffix='.npy') as source:
            with read_array(source.path,(h,w,3),'float32',ctx.check_active) as original:
                with read_array(prepared.composition.changes.path,(h,w),'float32',ctx.check_active,unit_interval=True) as changed:
                    for y in range(0,h,tile):
                        for x in range(0,w,tile):
                            ctx.check_active();sl=np.s_[y:y+tile,x:x+tile];part=prepared.composition.pixels.rgb[sl]
                            if not np.isfinite(part).all():raise ValueError('EDITOR_PRECOMPOSE_NONFINITE')
                            clipping+=int(np.count_nonzero(np.any((part<0)|(part>1),axis=2)))
                            unchanged=changed[sl]==0
                            if np.any(unchanged):outside=max(outside,float(np.max(np.abs(part[unchanged].astype(np.float64)-original[sl][unchanged]))))
        fraction=clipping/(h*w)
        report=ImageReport(schema_version=1,status='review' if fraction>0 else 'pass',operation='editor.layers_precompose',
            skipped_count=0,reason='EDITOR_PRECOMPOSE_CLIPPING' if fraction>0 else None,
            outside_mask_max_error=outside,clipping_fraction=fraction,identity=None,transform=None,
            sources=[params.source],registrations=[])
        yield inputs,prepared,report,tile
        ctx.check_active()

def publish_layers_precompose(artifacts,params,inputs,prepared,report,tile,check_active):
    output=_RoleArtifacts(artifacts,inputs.source,params.source.source)
    result=publish_prepared_render(output,inputs,prepared,report,(),tile_size=tile,check_active=check_active)
    return OperationResult(outputs=[*result.outputs,*output.extra],bytes_written=result.bytes_written+output.extra_bytes,review_required=result.review_required)

def execute_layers_precompose(ctx,artifacts,installation,admission):
    params=artifacts.params(EditorLayersPrecomposeParams)
    admission.admit(ctx,params);ctx.check_active();installation.verify()
    if (ctx.lease.execution_profile.get('native_qualification_pin')!=installation.pin.__dict__
            or ctx.lease.execution_profile.get('icc_lock_sha')!=installation.icc):
        raise ValueError('EDITOR_PRECOMPOSE_INSTALLATION_CHANGED')
    with prepare_layers_precompose(ctx,artifacts,installation,params) as (inputs,prepared,report,tile):
        admission.admit(ctx,params);installation.verify();ctx.check_active()
        return publish_layers_precompose(artifacts,params,inputs,prepared,report,tile,ctx.check_active)

def layers_precompose_operations(config,installation,admission):
    if not callable(getattr(admission,'admit',None)):raise ValueError('EDITOR_PRECOMPOSE_ADMISSION_REQUIRED')
    def execute(ctx):
        try:
            with TaskArtifacts.create(ctx,config,frozenset({'pixels','alpha','mask_pixels','mask','working_image','image_report','image_result'})) as artifacts:
                return execute_layers_precompose(ctx,artifacts,installation,admission)
        except ValueError as error:raise OperationFailure('INPUT_INVALID') from error
    return [Operation('editor.layers_precompose','cpu',execute)]
