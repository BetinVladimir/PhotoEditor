"""Internal CPU layer execution; relational graph admission is mandatory."""
from typing import Protocol
from photo_worker.contracts.images import WorkingImage
from photo_worker.algorithms.layer_masks import compile_mask
from photo_worker.runtime.executor import OperationFailure,OperationResult
from .contracts import LayerComposeParams
from .inputs import prepare_layer_inputs
from .resources import layer_tile_bytes
from .result import stage_layer_result


class LayerAdmission(Protocol):
    def admit(self,ctx,params:LayerComposeParams)->None:
        """Verify frozen graph, same-version parent closure and original scope.

        Must reject null source_result unless base.source is the authorized
        original snapshot WorkingImage. Tenant/session/task fence and immutable
        operation/runtime pins are authoritative, never inferred from JSON alone.
        """
        ...


def execute(ctx,artifacts,admission:LayerAdmission|None)->OperationResult:
    profile=ctx.lease.execution_profile
    if profile.get('layer_alpha_mode','require_float32') not in ('require_float32','normalize_uint16'):
        raise OperationFailure('CONFIG_INVALID')
    if profile.get('renderer_version')!='v2.2' or profile.get('backend')!='cpu':
        raise OperationFailure('CONFIG_INVALID')
    if admission is None:raise OperationFailure('LAYER_ADMISSION_UNAVAILABLE')
    try:
        params=artifacts.params(LayerComposeParams)
        ctx.check_active();admission.admit(ctx,params);ctx.check_active()
        source=artifacts.contract(params.base.source,WorkingImage)
        plan=compile_mask(params.mask,params.base.canvas_hash)
        budget=layer_tile_bytes(source.canvas.width,source.canvas.height,mask_nodes=len(plan.nodes),tile_size=512)
        ctx.progress(0,3)
        with prepare_layer_inputs(params,artifacts,max_tile_bytes=budget,tile_size=512,
                check_active=ctx.check_active,execution_profile=profile) as (parents,files):
            ctx.progress(1,3);ctx.check_active()
            staged=stage_layer_result(artifacts,params.base.source,parents.source,params.base,files,
                source_result=parents.source_result,proposal_result=parents.proposal_result)
            ctx.check_active()
        ctx.progress(3,3)
        return OperationResult(outputs=[staged.document],review_required=staged.review_required,bytes_written=staged.bytes_written)
    except ValueError as error:
        raise OperationFailure('INPUT_INVALID') from error


def layer_operations(config, database, runtime_lock=None, runtime_sha=None):
    """Install the CPU handler; SQL qualification and lease admission stay mandatory."""
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.executor import Operation
    from .admission import LensUserLayerAdmission
    if runtime_lock is None and runtime_sha is None:return []
    if runtime_lock is None or runtime_sha is None:raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    from .runtime_lock import verify_installed_runtime
    from copy import deepcopy
    runtime_lock=deepcopy(runtime_lock)
    digest=verify_installed_runtime(runtime_lock,runtime_sha)
    admission = LensUserLayerAdmission(database)
    roles = frozenset({'document', 'working_image', 'pixels', 'alpha',
                       'mask_pixels', 'mask', 'report'})
    def run(ctx):
        ctx.check_active()
        if ctx.lease.execution_profile.get('layer_runtime_sha256')!=digest:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        verify_installed_runtime(runtime_lock,digest)
        ctx.check_active()
        with TaskArtifacts.create(ctx, config, roles) as artifacts:
            return execute(ctx, artifacts, admission)
    return [Operation('layer.compose', 'cpu', run)]
