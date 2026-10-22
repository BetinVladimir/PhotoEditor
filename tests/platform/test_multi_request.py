from contextlib import contextmanager
from uuid import uuid4
import pytest
from test_relight_qualification import evidence
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.optical_multi import OpticalPanoramaParams
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.multi_request import decode_multi_request,prepare_frozen_multi_request


def fixture():
    _,pin,_,calibration,_=evidence()
    params=OpticalPanoramaParams(schema_version=1,frames=[VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64) for _ in range(2)],
        reference_ordinal=1,projection='cylindrical',max_pixels=10000,
        calibration=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(calibration)))
    return params,calibration.model_dump(mode='json'),pin


def test_calibration_thresholds_and_source_order_forwarded_exactly(monkeypatch):
    import photo_worker.operations.optical.multi_request as module
    params,calibration,pin=fixture();sentinel=object()
    @contextmanager
    def prepare(images,bindings,rgbs,alphas,reference,scratch,**kwargs):
        assert bindings==params.frames and reference==1
        assert kwargs['projection']=='cylindrical'
        assert kwargs['registration_error_px']==calibration['registration_error_px']
        assert kwargs['match_inlier_min']==calibration['match_inlier_min']
        assert kwargs['motion_threshold']==calibration['motion_threshold']
        yield sentinel
    monkeypatch.setattr(module,'prepare_multi_image',prepare)
    request=decode_multi_request('optical.panorama',canonical_bytes(params),canonical_bytes(calibration),
        model_lock_sha=calibration['model_lock_sha'],dataset_sha=calibration['dataset_sha'],max_pixels=10000)
    with prepare_frozen_multi_request(request,[None,None],[],[],None,verify_icc=lambda _:True,ram_max_bytes=2**40) as prepared:
        assert prepared is sentinel


def test_changed_or_unqualified_calibration_rejected():
    params,calibration,_=fixture()
    for change in [{'qualified':False},{'motion_threshold':.9}]:
        altered={**calibration,**change}
        with pytest.raises(ValueError,match='MULTI_CALIBRATION_MISMATCH'):
            decode_multi_request('optical.panorama',canonical_bytes(params),canonical_bytes(altered),
                model_lock_sha=calibration['model_lock_sha'],dataset_sha=calibration['dataset_sha'],max_pixels=10000)


def test_params_cannot_exceed_installed_pixel_limit():
    params,calibration,_=fixture()
    with pytest.raises(ValueError,match='MULTI_PIXEL_LIMIT'):
        decode_multi_request('optical.panorama',canonical_bytes(params),canonical_bytes(calibration),
            model_lock_sha=calibration['model_lock_sha'],dataset_sha=calibration['dataset_sha'],max_pixels=9999)
