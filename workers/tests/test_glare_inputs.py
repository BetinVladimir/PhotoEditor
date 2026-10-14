"""V28 glare donor input closure binds both frames and all declared bytes."""

from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.images import Canvas, ColorSpace, MaskRef, TransformChain, WorkingImage
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.portrait.glare_inputs import verify_glare_donor_inputs
from photo_worker.operations.session.contracts import Calibration, FaceObservations


def ref(value=None):
    return {'artifact_id': uuid4(), 'sha256': value or 'a' * 64}


def fixture():
    lock = 'b' * 64
    canvas = Canvas(width=16, height=16, orientation=1, space_id='source',
                    source_sha256='c' * 64)
    chain = TransformChain.create([])
    color = ColorSpace(profile_id='linear_srgb_v1', icc_sha256='d' * 64,
                       encoding='linear')
    source = WorkingImage(pixels=ref(), canvas=canvas, color=color,
                          dtype='float32', alpha=None, transform_chain=chain)
    donor = WorkingImage(pixels=ref(), canvas=canvas, color=color,
                         dtype='float32', alpha=None, transform_chain=chain)
    source_binding = {'asset_version_id': uuid4(),
                      'source': ref(canonical_hash(source)),
                      'canvas_hash': canonical_hash(canvas)}
    donor_binding = {'asset_version_id': uuid4(),
                     'source': ref(canonical_hash(donor)),
                     'canvas_hash': canonical_hash(canvas)}
    target = MaskRef(artifact=ref(), canvas=canvas, transform_chain=chain,
        selector='glasses', confidence=1, model_lock_id=None, protected=False)
    protected = MaskRef(artifact=ref(), canvas=canvas, transform_chain=chain,
        selector='protected', confidence=1, model_lock_id=None, protected=True)
    confirmation = DonorConfirmation(schema_version=1, person_id=uuid4(),
        source=source_binding, donor=donor_binding,
        source_face_id=uuid4(), donor_face_id=uuid4(),
        source_observation_id=uuid4(), donor_observation_id=uuid4(),
        consent_id=uuid4(), epoch='1', actor_id=uuid4())
    source_faces = FaceObservations(schema_version=1, binding=source_binding, faces=[])
    donor_faces = FaceObservations(schema_version=1, binding=donor_binding, faces=[])
    calibration = Calibration(schema_version=1, model_lock_sha=lock,
        dataset_sha='e' * 64, qualified=True, scene_cosine_min=0,
        scene_log_luma_max=1, cluster_diameter_max=1, sharpness_scale=1,
        clipping_penalty=1, match_inlier_min=.5, match_error_px_max=2,
        identity_similarity_min=.5, landmark_drift_max=.2,
        normalization_scales=[1] * 6, loss_weights=[1/6] * 6,
        focus_warning=.3, focus_critical=.1, eye_open_min=.5,
        glare_warning_fraction=.1, clipping_warning_fraction=.1,
        clipping_critical_fraction=.2, min_face_edge_px=32,
        depth_sigma=.1, registration_error_px=1, motion_threshold=.1)
    params = PortraitGlareParams(schema_version=1, base=source_binding,
        strength=.5, donor=donor_binding, glasses_mask=ref(canonical_hash(target)),
        protected=ref(canonical_hash(protected)), mode='donor',
        confirmation=ref(canonical_hash(confirmation)), model_lock_sha=lock,
        calibration=ref(canonical_hash(calibration)),
        source_faces=ref(canonical_hash(source_faces)),
        donor_faces=ref(canonical_hash(donor_faces)))
    declared = [params.base.source, params.donor.source, params.glasses_mask,
        params.protected, params.confirmation, params.source_faces,
        params.donor_faces, params.calibration, source.pixels, donor.pixels,
        target.artifact, protected.artifact]
    return (params, source, donor, target, protected, confirmation,
            source_faces, donor_faces, calibration, declared)


def test_glare_donor_input_closure_accepts_exact_documents():
    verify_glare_donor_inputs(*fixture())


def test_glare_donor_input_closure_rejects_missing_declared_binary():
    values = fixture()
    with pytest.raises(ValueError, match='GLARE_DONOR_INPUT_UNDECLARED'):
        verify_glare_donor_inputs(*values[:-1], values[-1][:-1])
