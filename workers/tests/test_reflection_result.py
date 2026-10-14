"""V28 reflection result carries review report and both mask references."""

from pathlib import Path
import os
from uuid import uuid4
import pytest

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.contracts.portrait_restoration import ImageReflectionParams
from photo_worker.operations.image.reflection_result import ReflectionFiles, stage_reflection_result
from photo_worker.operations.optical.contracts import ImageResult, ImageReport


def ref():
    return {'artifact_id': uuid4(), 'sha256': 'a' * 64}


def test_reflection_result_declares_review_and_generated_mask(tmp_path):
    canvas = Canvas(width=2, height=2, orientation=1, space_id='original',
                    source_sha256='b' * 64)
    source = WorkingImage(pixels=ref(), canvas=canvas,
        color=ColorSpace(profile_id='linear_srgb_v1', icc_sha256='c' * 64,
                         encoding='linear'), dtype='float32', alpha=None,
        transform_chain=TransformChain.create([]))
    params = ImageReflectionParams(schema_version=1,
        base={'asset_version_id': uuid4(), 'source': ref(),
              'canvas_hash': canonical_hash(canvas)}, strength=.5,
        mask=ref(), protected=ref(), model_lock_sha='d' * 64,
        calibration=ref())

    class Reservation:
        def validate(self):
            pass

    class Artifacts:
        def __init__(self):
            self.values = []
            self.roles = []

        def stage(self, path, reservation, role, media_type):
            assert reservation is owned and path.is_file()
            self.roles.append(role)
            return ref()

        def stage_contract(self, value, role):
            self.values.append(value)
            self.roles.append(role)
            return ref()

    owned = Reservation()
    paths = [tmp_path / name for name in ('rgb.npy', 'changed.npy', 'generated.npy')]
    for path in paths:
        path.write_bytes(b'fixture')
    artifacts = Artifacts()
    result_ref, written = stage_reflection_result(
        artifacts, params, source,
        ReflectionFiles(*paths, owned, 0.0))
    assert result_ref.artifact_id.int != 0 and written > 0
    assert artifacts.roles == ['pixels', 'working_image', 'mask_pixels',
                               'mask_pixels', 'mask', 'mask', 'report', 'document']
    report = next(value for value in artifacts.values if isinstance(value, ImageReport))
    result = next(value for value in artifacts.values if isinstance(value, ImageResult))
    assert report.status == 'review' and report.operation == 'image.reflection'
    assert result.generated_mask is not None and result.generated_mask != result.changed_mask
    roles = list(artifacts.roles)
    for same in ((paths[0], paths[0], paths[2]),
                 (paths[0], paths[1], paths[1]),
                 (paths[0], paths[1], paths[0])):
        with pytest.raises(ValueError, match='REFLECTION_RESULT_INVALID'):
            stage_reflection_result(artifacts, params, source,
                                    ReflectionFiles(*same, owned, 0.0))
        assert artifacts.roles == roles
    alias = tmp_path / 'generated-hardlink.npy'
    os.link(paths[1], alias)
    with pytest.raises(ValueError, match='REFLECTION_RESULT_INVALID'):
        stage_reflection_result(artifacts, params, source,
                                ReflectionFiles(paths[0], paths[1], alias,
                                                owned, 0.0))
    assert artifacts.roles == roles
