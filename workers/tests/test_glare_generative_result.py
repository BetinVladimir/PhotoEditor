"""V28 donorless glare result must expose generated pixels and review status."""

from uuid import uuid4

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.operations.portrait.glare_generative_files import GenerativeGlareFiles
from photo_worker.operations.portrait.glare_generative_result import stage_generative_glare_result


def ref():
    return {'artifact_id': uuid4(), 'sha256': 'a' * 64}


def test_generative_glare_result_requires_synthetic_mask_and_review(tmp_path):
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
        donor=None, glasses_mask=ref(), protected=ref(),
        mode='generative', confirmation=ref(), model_lock_sha='d' * 64,
        calibration=ref(), source_faces=ref(), donor_faces=None)

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
    paths = [tmp_path / name for name in ('rgb.npy', 'changed.npy', 'generated.npy')]
    for path in paths:
        path.write_bytes(b'fixture')
    artifacts = Artifacts()
    staged = stage_generative_glare_result(artifacts, params,
        canonical_bytes(source), GenerativeGlareFiles(*paths, owned, 0.),
        check_active=lambda: None)
    report = next(value for value in artifacts.values if isinstance(value, ImageReport))
    result = next(value for value in artifacts.values if isinstance(value, ImageResult))
    assert staged.review_required and len(staged.outputs) == 8
    assert report.status == 'review' and report.sources == [params.base]
    assert report.reason == 'synthetic_glare_proposal'
    assert result.generated_mask is not None
