"""V28 donorless glare input closure keeps model, source and masks pinned."""

from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.images import Canvas, ColorSpace, MaskRef, TransformChain, WorkingImage
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.portrait.glare_generative_inputs import verify_generative_glare_inputs
from photo_worker.operations.session.contracts import Calibration, FaceObservations


def ref(sha='a' * 64):
    return {'artifact_id': uuid4(), 'sha256': sha}


def fixture():
    lock = 'b' * 64
    canvas = Canvas(width=16, height=16, orientation=1,
                    space_id='source', source_sha256='c' * 64)
    chain = TransformChain.create([])
    source = WorkingImage(pixels=ref(), canvas=canvas,
        color=ColorSpace(profile_id='linear_srgb_v1', icc_sha256='d' * 64,
                         encoding='linear'), dtype='float32', alpha=None,
        transform_chain=chain)
    binding = {'asset_version_id': uuid4(),
               'source': ref(canonical_hash(source)),
               'canvas_hash': canonical_hash(canvas)}
    target = MaskRef(artifact=ref(), canvas=canvas, transform_chain=chain,
        selector='glasses', confidence=1, model_lock_id=None, protected=False)
    protected = MaskRef(artifact=ref(), canvas=canvas, transform_chain=chain,
        selector='protected', confidence=1, model_lock_id=None, protected=True)
    proof = DonorConfirmation(schema_version=1, person_id=uuid4(),
        source=binding, donor=None, source_face_id=uuid4(), donor_face_id=None,
        source_observation_id=uuid4(), donor_observation_id=None,
        consent_id=uuid4(), epoch='1', actor_id=uuid4())
    faces = FaceObservations(schema_version=1, binding=binding, faces=[])
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
    params = PortraitGlareParams(schema_version=1, base=binding,
        strength=.5, donor=None, glasses_mask=ref(canonical_hash(target)),
        protected=ref(canonical_hash(protected)), mode='generative',
        confirmation=ref(canonical_hash(proof)), model_lock_sha=lock,
        calibration=ref(canonical_hash(calibration)),
        source_faces=ref(canonical_hash(faces)), donor_faces=None)
    declared = [params.base.source, params.glasses_mask, params.protected,
                params.confirmation, params.source_faces, params.calibration,
                source.pixels, target.artifact, protected.artifact]
    return (params, source, target, protected, proof, faces, calibration,
            declared, lock)


def test_generative_glare_input_closure_accepts_exact_documents():
    verify_generative_glare_inputs(*fixture())


def test_generative_glare_input_closure_denies_missing_binary():
    values = fixture()
    with pytest.raises(ValueError, match='GLARE_GENERATIVE_INPUT_UNDECLARED'):
        verify_generative_glare_inputs(*values[:-2], values[-2][:-1], values[-1])
