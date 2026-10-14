"""Donor glare output stays review-only and declares no generated content."""

from uuid import uuid4

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.operations.portrait.donor_files import DonorPortraitFiles
from photo_worker.operations.portrait.glare_result import stage_glare_donor_result


def ref():
    return {'artifact_id': uuid4(), 'sha256': 'a' * 64}


def test_glare_result_is_reviewed_donor_lineage(tmp_path):
    canvas = Canvas(width=2, height=2, orientation=1, space_id='source',
                    source_sha256='b' * 64)
    source = WorkingImage(pixels=ref(), canvas=canvas,
        color=ColorSpace(profile_id='linear_srgb_v1', icc_sha256='c' * 64,
                         encoding='linear'), dtype='float32', alpha=None,
        transform_chain=TransformChain.create([]))
    source_ref = ref()
    source_ref['sha256'] = canonical_hash(source)
    params = PortraitGlareParams(schema_version=1,
        base={'asset_version_id': uuid4(), 'source': source_ref,
              'canvas_hash': canonical_hash(canvas)}, strength=.5,
        donor={'asset_version_id': uuid4(), 'source': ref(),
               'canvas_hash': canonical_hash(canvas)},
        glasses_mask=ref(), protected=ref(), mode='donor',
        confirmation=ref(), model_lock_sha='d' * 64,
        calibration=ref(), source_faces=ref(), donor_faces=ref())

    class Reservation:
        def validate(self):
            pass

    class Artifacts:
        def __init__(self):
            self.values = []

        def stage(self, path, reservation, role, media_type):
            assert reservation is owned and path.is_file()
            return ref()

        def stage_contract(self, value, role):
            self.values.append(value)
            return ref()

    owned = Reservation()
    rgb, changed = tmp_path / 'rgb.npy', tmp_path / 'changed.npy'
    rgb.write_bytes(b'fixture')
    changed.write_bytes(b'fixture')
    artifacts = Artifacts()
    staged = stage_glare_donor_result(artifacts, params, canonical_bytes(source),
        DonorPortraitFiles(rgb, changed, owned), color_warning=None,
        check_active=lambda: None)
    report = next(value for value in artifacts.values if isinstance(value, ImageReport))
    result = next(value for value in artifacts.values if isinstance(value, ImageResult))
    assert staged.review_required is True and staged.bytes_written > 0
    assert report.status == 'review' and report.operation == 'portrait.glare'
    assert report.sources == [params.base, params.donor]
    assert result.generated_mask is None
