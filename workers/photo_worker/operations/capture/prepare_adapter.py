"""Internal V24 preprocessing task; its outputs remain attempt-fenced."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.models.install import ModelError
from photo_worker.operations.session.contracts import Calibration
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from .contracts import CapturePrepareParams, CapturePrepared
from .prepare import prepare_capture_inputs


def execute(ctx, artifacts, pool, backend):
    profile = ctx.lease.execution_profile
    estimate = profile.get('resource_estimate')
    if (profile.get('renderer_version') != 'v2.2' or profile.get('backend') != backend
            or backend not in ('cpu', 'cuda') or not isinstance(estimate, dict)
            or type(estimate.get('ram_bytes')) is not int or estimate['ram_bytes'] < 2 * 1024**3):
        raise OperationFailure('CONFIG_INVALID')
    try:
        params = artifacts.params(CapturePrepareParams)
        if params.model_lock_sha != pool.lock.lock_id:
            raise ModelError('MODEL_LOCK_MISMATCH')
        calibration = artifacts.contract(params.calibration, Calibration)
        prepared = prepare_capture_inputs(ctx, artifacts, params.binding, calibration, pool, backend)
        document = CapturePrepared(schema_version=1, binding=params.binding,
                                   proxy=prepared.proxy.image_ref, faces=prepared.faces_ref)
        ctx.check_active()
        root = artifacts.stage_contract(document, 'document')
        return OperationResult(outputs=[root, *prepared.outputs],
            bytes_written=prepared.bytes_written + len(canonical_bytes(document)))
    except ModelError as error:
        raise OperationFailure(str(error)) from error
    except ValueError as error:
        raise OperationFailure('INPUT_INVALID') from error


def capture_prepare_operations(config, pool, backend):
    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'document', 'faces',
                'working_rgb', 'working_alpha', 'working_image'})) as artifacts:
            return execute(ctx, artifacts, pool, backend)
    return [Operation('capture.prepare', 'gpu', run)]
