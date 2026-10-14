"""V28 donor glare task handler; not installed without dedicated live SQL authority."""

from contextlib import ExitStack
from typing import Protocol

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.session.contracts import Calibration, FaceObservations
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.portrait_donor_reader import DonorInputReader

from .donor_handoff import donor_operation_result
from .glare_inputs import verify_glare_donor_inputs
from .glare_stage import prepare_and_stage_glare_full_mask


class GlareAdmission(Protocol):
    material_scratch_root: object
    material_max_bytes: int
    alpha_mode: str

    def admit(self, ctx, params, proof, source, donor, calibration) -> bool:
        """Verify live tenant, attempt, session, consent, mask origin and qualification."""

    def reader(self, ctx, params, proof):
        """Return current versioned input-evidence reader for this exact attempt."""


def execute(ctx, artifacts, admission: GlareAdmission | None):
    """Process donor mode; generative mode awaits independent model qualification."""
    if admission is None:
        raise OperationFailure('GLARE_AUTHORITY_UNAVAILABLE')
    command = ctx.lease.command
    profile = ctx.lease.execution_profile
    if (command.operation != 'portrait.glare' or command.operation_version != '1'
            or command.resource_class != 'gpu' or profile.get('backend') not in ('cpu','cuda')
            or profile.get('renderer_version') != 'v2.2'):
        raise OperationFailure('CONFIG_INVALID')
    ctx.check_active()
    try:
        params = artifacts.params(PortraitGlareParams)
        if (len(canonical_bytes(params)) > 65536
                or canonical_hash(params) != command.params.sha256):
            raise ValueError('GLARE_PARAMS_BINDING_MISMATCH')
        params = PortraitGlareParams.model_validate_json(canonical_bytes(params))
        if params.mode == 'generative':
            from .glare_generative_worker import execute_generative
            return execute_generative(ctx, artifacts, admission, params)
        if profile.get('backend') != 'cpu':
            raise OperationFailure('CONFIG_INVALID')
        if (not callable(getattr(admission, 'admit', None))
                or not callable(getattr(admission, 'reader', None))):
            raise OperationFailure('GLARE_AUTHORITY_UNAVAILABLE')

        def document(ref, model):
            raw = artifacts.contract_bytes(ref, model)
            value = model.model_validate_json(raw)
            if canonical_bytes(value) != raw:
                raise ValueError('GLARE_DOCUMENT_INVALID')
            return raw, value

        source_raw, source = document(params.base.source, WorkingImage)
        donor_raw, donor = document(params.donor.source, WorkingImage)
        proof_raw, proof = document(params.confirmation, DonorConfirmation)
        source_faces_raw, source_faces = document(params.source_faces, FaceObservations)
        donor_faces_raw, donor_faces = document(params.donor_faces, FaceObservations)
        _, target = document(params.glasses_mask, MaskRef)
        _, protected = document(params.protected, MaskRef)
        _, calibration = document(params.calibration, Calibration)
        declared = artifacts.declared_input_refs(512)
        verify_glare_donor_inputs(params, source, donor, target, protected,
            proof, source_faces, donor_faces, calibration, declared)

        def authorize():
            ctx.check_active()
            if admission.admit(ctx, params.model_copy(deep=True), bytes(proof_raw),
                    source.model_copy(deep=True), donor.model_copy(deep=True),
                    calibration.model_copy(deep=True)) is not True:
                raise OperationFailure('GLARE_AUTHORITY_UNAVAILABLE')
            ctx.check_active()

        authorize()
        ctx.progress(0, 3)
        underlying = admission.reader(ctx, params.model_copy(deep=True), bytes(proof_raw))
        reader = DonorInputReader(artifacts, underlying, check_active=authorize)
        height, width = source.canvas.height, source.canvas.width
        with ExitStack() as stack:
            def mask_pixels(ref):
                path = stack.enter_context(artifacts.input(ref, suffix='.npy')).path
                return stack.enter_context(read_array(path, (height, width),
                    'float32', authorize, unit_interval=True))

            target_pixels = mask_pixels(target.artifact)
            guard_pixels = mask_pixels(protected.artifact)
            ctx.progress(1, 3)
            staged = prepare_and_stage_glare_full_mask(
                params, source_raw, donor_raw, proof_raw,
                source_faces_raw, donor_faces_raw, reader,
                target_pixels, guard_pixels, artifacts, artifacts.scratch,
                profile=profile, min_face_edge_px=calibration.min_face_edge_px,
                material_scratch_root=admission.material_scratch_root,
                material_max_bytes=admission.material_max_bytes,
                check_active=authorize, authority=lambda *_: (authorize() or True),
                alpha_mode=admission.alpha_mode)
        authorize()
        estimate = profile.get('resource_estimate')
        budget = estimate.get('output_bytes') if isinstance(estimate, dict) else None
        result = donor_operation_result(staged, declared_inputs=declared,
            output_budget=budget, check_active=authorize)
        ctx.progress(3, 3)
        return result
    except ValueError as error:
        code = str(error)
        allowed = {'GLARE_AUTHORITY_UNAVAILABLE', 'GLARE_DONOR_AUTHORITY_UNAVAILABLE',
                   'DONOR_COVERAGE', 'DONOR_ALPHA_COVERAGE',
                   'DONOR_MASK_PATCH_BUDGET'}
        raise OperationFailure(code if code in allowed else 'INPUT_INVALID') from error
