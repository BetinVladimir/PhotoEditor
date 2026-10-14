"""V28 donorless glare task branch requiring a trusted installed inpaint model."""

from contextlib import ExitStack

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.operations.session.contracts import Calibration, FaceObservations
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure, OperationResult

from .glare_generative_inputs import verify_generative_glare_inputs
from .glare_generative_stage import prepare_and_stage_generative_glare
from .local_result import StagedLocalPortrait


def _handoff(staged, declared, budget, check_active):
    check_active()
    if (not isinstance(staged, StagedLocalPortrait)
            or staged.review_required is not True
            or not isinstance(staged.outputs, tuple) or len(staged.outputs) != 8
            or type(staged.bytes_written) is not int or staged.bytes_written < 1
            or type(budget) is not int or staged.bytes_written > budget
            or not isinstance(declared, (tuple, list)) or not 1 <= len(declared) <= 512):
        raise OperationFailure('GLARE_GENERATIVE_RESULT_INVALID')
    try:
        outputs = tuple(ArtifactRef.model_validate_json(canonical_bytes(ref))
                        for ref in staged.outputs)
        inputs = tuple(ArtifactRef.model_validate_json(canonical_bytes(ref))
                       for ref in declared)
        root = ArtifactRef.model_validate_json(canonical_bytes(staged.document))
    except ValueError as error:
        raise OperationFailure('GLARE_GENERATIVE_RESULT_INVALID') from error
    ids = {ref.artifact_id for ref in outputs}
    if (len(ids) != 8 or any(ref.artifact_id.int == 0 for ref in outputs)
            or root != outputs[-1]
            or ids & {ref.artifact_id for ref in inputs}):
        raise OperationFailure('GLARE_GENERATIVE_RESULT_INVALID')
    check_active()
    return OperationResult(outputs=[root], review_required=True,
                           bytes_written=staged.bytes_written)


def execute_generative(ctx, artifacts, admission, params):
    """Run only under a live consent/qualification callback and model lease."""
    if (not callable(getattr(admission, 'admit_generative', None))
            or not callable(getattr(admission, 'acquire_inpaint', None))
            or not isinstance(getattr(admission, 'model_lock_sha', None), str)):
        raise OperationFailure('GLARE_GENERATIVE_MODEL_UNAVAILABLE')
    profile = ctx.lease.execution_profile
    estimate = profile.get('resource_estimate')
    ram = estimate.get('ram_bytes') if isinstance(estimate, dict) else None
    if (profile.get('backend') not in ('cpu', 'cuda')
            or type(ram) is not int or ram < 2 * 1024**3):
        raise OperationFailure('CONFIG_INVALID')
    try:
        def document(ref, model):
            raw = artifacts.contract_bytes(ref, model)
            value = model.model_validate_json(raw)
            if canonical_bytes(value) != raw:
                raise ValueError('GLARE_DOCUMENT_INVALID')
            return raw, value

        source_raw, source = document(params.base.source, WorkingImage)
        proof_raw, proof = document(params.confirmation, DonorConfirmation)
        faces_raw, faces = document(params.source_faces, FaceObservations)
        _, target = document(params.glasses_mask, MaskRef)
        _, protected = document(params.protected, MaskRef)
        _, calibration = document(params.calibration, Calibration)
        declared = artifacts.declared_input_refs(512)
        verify_generative_glare_inputs(params, source, target, protected,
            proof, faces, calibration, declared, admission.model_lock_sha)
        height, width = source.canvas.height, source.canvas.width
        if ram < 2 * 1024**3 + height * width * 128:
            raise OperationFailure('RESOURCE_OOM')

        def authorize():
            ctx.check_active()
            if admission.admit_generative(ctx, params.model_copy(deep=True),
                    bytes(proof_raw), source.model_copy(deep=True),
                    calibration.model_copy(deep=True)) is not True:
                raise OperationFailure('GLARE_GENERATIVE_MODEL_UNAVAILABLE')
            ctx.check_active()

        authorize()
        ctx.progress(0, 3)
        with admission.acquire_inpaint(ctx, profile['backend']) as engine, ExitStack() as stack:
            def mapped(ref, shape):
                path = stack.enter_context(artifacts.input(ref, suffix='.npy')).path
                return stack.enter_context(read_array(path, shape, 'float32',
                    authorize, unit_interval=True))

            rgb = mapped(source.pixels, (height, width, 3))
            target_pixels = mapped(target.artifact, (height, width))
            protected_pixels = mapped(protected.artifact, (height, width))
            ctx.progress(1, 3)
            staged = prepare_and_stage_generative_glare(
                params, source_raw, proof_raw, faces_raw, rgb,
                target_pixels, protected_pixels, engine, artifacts,
                artifacts.scratch, min_face_edge_px=calibration.min_face_edge_px,
                check_active=authorize, authority=lambda *_: (authorize() or True))
        authorize()
        budget = estimate.get('output_bytes')
        result = _handoff(staged, declared, budget, authorize)
        ctx.progress(3, 3)
        return result
    except ValueError as error:
        code = str(error)
        allowed = {'MODEL_OUTPUT_INVALID', 'MODEL_INFERENCE_ERROR',
                   'GLARE_GENERATIVE_OUTSIDE_MASK_CHANGED',
                   'GLARE_GENERATIVE_FACE_TOO_SMALL',
                   'DONOR_MASK_PATCH_BUDGET'}
        raise OperationFailure(code if code in allowed else 'INPUT_INVALID') from error
