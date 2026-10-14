from types import SimpleNamespace
from uuid import uuid4
import cv2
import numpy as np
import pytest
from photo_worker.algorithms.color import to_encoded, to_linear
from photo_worker.operations.geometry.prepare import prepare_crop
from photo_worker.runtime.scratch import ScratchSpace


@pytest.mark.parametrize('with_alpha', [False, True])
def test_crop_matches_original_linear_light_math_and_releases_files(tmp_path, with_alpha):
    rng = np.random.default_rng(81)
    rgb = rng.random((13, 17, 3), dtype=np.float32)
    alpha = rng.integers(0, 65536, (13, 17), dtype=np.uint16) if with_alpha else None
    matrix = np.array([[.83, .01, -.5], [.02, .91, -.7], [0, 0, 1]], np.float64)
    plan = SimpleNamespace(source_height=13, source_width=17, output_height=11,
        output_width=14, disposition='apply', forward_matrix=matrix.ravel().tolist())
    expected = to_encoded(cv2.warpPerspective(to_linear(rgb), matrix, (14, 11),
        flags=cv2.INTER_LANCZOS4, borderMode=cv2.BORDER_REFLECT_101)).clip(0, 1)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1000000, min_free_bytes=0) as scratch:
        with prepare_crop(rgb, alpha, plan, scratch) as crop:
            path = crop.rgb
            np.testing.assert_array_equal(np.load(path), expected)
            if with_alpha:
                expected_alpha = np.rint(np.clip(cv2.warpPerspective(alpha.astype(np.float32),
                    matrix, (14, 11), flags=cv2.INTER_LANCZOS4,
                    borderMode=cv2.BORDER_REFLECT_101), 0, 65535)).astype(np.uint16)
                np.testing.assert_array_equal(np.load(crop.alpha), expected_alpha)
            else:
                assert crop.alpha is None
        assert not path.exists()


def test_crop_stage_preserves_geometry_and_review(tmp_path):
    from test_composition_crop import Artifacts, image, ref
    from photo_worker.contracts.composition import ImageStructure
    from photo_worker.operations.composition.plan import execute
    from test_asset_adapters import context_for
    from photo_worker.operations.geometry.prepare import prepare_crop_stage
    source = image(17, 13)
    artifacts = Artifacts(source, ImageStructure(image=ref()))
    execute(context_for(artifacts.structure_ref, 'composition.plan'), artifacts)
    plan = artifacts.staged[0][1]
    plan_ref = ref(plan)
    rgb = np.full((13, 17, 3), .4, np.float32)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1000000, min_free_bytes=0) as scratch:
        with prepare_crop_stage(artifacts.image_ref, source, plan_ref, plan, rgb,
                None, scratch) as stage:
            assert stage.canvas.source_sha256 == source.canvas.source_sha256
            assert stage.transform_chain.steps[-1].from_canvas == source.canvas
            assert stage.transform_chain.steps[-1].to_canvas == stage.canvas
            assert stage.rgb.shape == (plan.output_height, plan.output_width, 3)
            assert not stage.rgb.flags.writeable
            assert stage.review_required == (plan.disposition == 'review')
            assert stage.review_reasons == tuple(plan.reasons)
        with pytest.raises(ValueError, match='CROP_PLAN_SOURCE_INVALID'):
            with prepare_crop_stage(ref(), source, plan_ref, plan, rgb, None, scratch):
                pytest.fail('foreign source accepted')
