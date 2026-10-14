"""V28 reflection input closure requires exact hashes and declared bytes."""

from uuid import uuid4
from types import SimpleNamespace

import pytest

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, MaskRef, TransformChain, WorkingImage
from photo_worker.contracts.portrait_restoration import ImageReflectionParams
from photo_worker.operations.image.reflection_inputs import verify_reflection_inputs
from photo_worker.operations.image.reflection import execute as execute_reflection
from photo_worker.operations.session.contracts import Calibration
from photo_worker.runtime.executor import OperationFailure


def ref():
    return {'artifact_id': uuid4(), 'sha256': 'a' * 64}


def fixture():
    lock = 'b' * 64
    canvas = Canvas(width=12, height=10, orientation=1, space_id='source', source_sha256='c' * 64)
    chain = TransformChain.create([])
    source = WorkingImage(pixels=ref(), canvas=canvas,
        color=ColorSpace(profile_id='linear_srgb_v1', icc_sha256='d' * 64, encoding='linear'),
        dtype='float32', alpha=None, transform_chain=chain)
    target = MaskRef(artifact=ref(), canvas=canvas, transform_chain=chain,
        selector='reflection', confidence=1, model_lock_id=None, protected=False)
    protected = MaskRef(artifact=ref(), canvas=canvas, transform_chain=chain,
        selector='protected', confidence=1, model_lock_id=None, protected=True)
    calibration = Calibration(schema_version=1, model_lock_sha=lock, dataset_sha='e' * 64,
        qualified=True, scene_cosine_min=0, scene_log_luma_max=1,
        cluster_diameter_max=1, sharpness_scale=1, clipping_penalty=1,
        match_inlier_min=.5, match_error_px_max=2, identity_similarity_min=.5,
        landmark_drift_max=.2, normalization_scales=[1] * 6,
        loss_weights=[1/6] * 6, focus_warning=.3, focus_critical=.1,
        eye_open_min=.5, glare_warning_fraction=.1, clipping_warning_fraction=.1,
        clipping_critical_fraction=.2, min_face_edge_px=32, depth_sigma=.1,
        registration_error_px=1, motion_threshold=.1)
    source_ref, target_ref, protected_ref, calibration_ref = ref(), ref(), ref(), ref()
    source_ref['sha256'] = canonical_hash(source)
    target_ref['sha256'] = canonical_hash(target)
    protected_ref['sha256'] = canonical_hash(protected)
    calibration_ref['sha256'] = canonical_hash(calibration)
    params = ImageReflectionParams(schema_version=1,
        base={'asset_version_id': uuid4(), 'source': source_ref,
              'canvas_hash': canonical_hash(canvas)},
        strength=.5, mask=target_ref, protected=protected_ref,
        model_lock_sha=lock, calibration=calibration_ref)
    declared = [params.base.source, params.mask, params.protected,
                params.calibration, source.pixels, target.artifact, protected.artifact]
    return params, source, target, protected, calibration, declared, lock


def test_reflection_inputs_accept_exact_declared_documents():
    verify_reflection_inputs(*fixture())


def test_reflection_inputs_reject_missing_binary_and_wrong_lock():
    params, source, target, protected, calibration, declared, lock = fixture()
    with pytest.raises(ValueError, match='REFLECTION_INPUT_UNDECLARED'):
        verify_reflection_inputs(params, source, target, protected, calibration,
                                 declared[:-1], lock)
    with pytest.raises(ValueError, match='REFLECTION_MODEL_UNAVAILABLE'):
        verify_reflection_inputs(params, source, target, protected, calibration,
                                 declared, 'f' * 64)


@pytest.mark.parametrize('decision', [None, False, 1, 'yes'])
def test_reflection_worker_denies_non_true_admission_before_pixel_or_model_io(decision):
    params, source, target, protected, calibration, declared, lock = fixture()
    documents = {
        params.base.source.artifact_id: source,
        params.mask.artifact_id: target,
        params.protected.artifact_id: protected,
        params.calibration.artifact_id: calibration,
    }
    calls = []
    artifacts = SimpleNamespace(
        params=lambda model: params,
        contract=lambda ref, model: documents[ref.artifact_id],
        declared_input_refs=lambda: declared,
        input=lambda *_args, **_kwargs: calls.append('pixel'),
    )
    command = SimpleNamespace(operation='image.reflection', operation_version='1',
        resource_class='gpu', params=SimpleNamespace(sha256=canonical_hash(params)))
    context = SimpleNamespace(lease=SimpleNamespace(command=command,
        execution_profile={'backend': 'cpu', 'renderer_version': 'v2.2',
                           'resource_estimate': {'ram_bytes': 4 * 1024**3}}),
        check_active=lambda: None, progress=lambda *_: None)
    admission = SimpleNamespace(lock_sha=lock,
        admit=lambda *_: decision,
        acquire=lambda *_: calls.append('model'))
    with pytest.raises(OperationFailure, match='REFLECTION_MODEL_UNAVAILABLE'):
        execute_reflection(context, artifacts, admission)
    assert calls == []


