from uuid import uuid4

import numpy as np

from photo_worker.algorithms.color import apply_global, apply_strength
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.learning.recipe import FrozenHoldoutStyles
from photo_worker.operations.learning.workspace import (
    open_holdout_workspace, render_holdout_triplet,
)
from photo_worker.runtime.scratch import ScratchSpace

from test_v23_learning_source import _fixture
from test_style_resolve import profile


def test_holdout_workspace_keeps_four_buffers_mapped_and_releases_them(tmp_path):
    artifacts, case = _fixture(tmp_path)
    root = tmp_path / 'scratch'
    root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000,
                      min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with open_holdout_workspace(artifacts, case,
                                    artifacts.image.color.icc_sha256,
                                    lambda: None) as workspace:
            workspace.baseline[:] = workspace.source.rgb
            workspace.candidate[:] = workspace.source.rgb
            workspace.approved[:] = workspace.source.rgb
            np.testing.assert_array_equal(workspace.baseline,
                                          artifacts.rgb)
            assert np.allclose(workspace.alpha,
                               np.float32(10000 / 65535))
            assert len(list(scratch.directory.glob('holdout-*.npy'))) == 4
        assert list(scratch.directory.iterdir()) == []


def test_holdout_triplet_renders_three_styles_from_one_source(tmp_path):
    artifacts, case = _fixture(tmp_path)
    base = profile()
    changed = base.model_dump(mode='json', by_alias=True)
    changed['global']['gains_rgb'] = [1.1, 1, 1]
    candidate = StyleV2.model_validate(changed)
    changed['global']['gains_rgb'] = [1.2, 1, 1]
    approved = StyleV2.model_validate(changed)
    styles = FrozenHoldoutStyles(base, candidate, approved)
    root = tmp_path / 'scratch'
    root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000,
                      min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with open_holdout_workspace(artifacts, case,
                                    artifacts.image.color.icc_sha256,
                                    lambda: None) as workspace:
            render_holdout_triplet(workspace, styles, person_reliable=False)
            for style, output in zip(
                    (base, candidate, approved),
                    (workspace.baseline, workspace.candidate, workspace.approved),
                    strict=True):
                expected = apply_strength(artifacts.rgb,
                                          apply_global(artifacts.rgb, style.global_),
                                          style.strength)
                np.testing.assert_array_equal(output, expected)


def test_holdout_float_alpha_is_not_divided_twice(tmp_path):
    artifacts,case=_fixture(tmp_path)
    opacity=np.full(artifacts.rgb.shape[:2],.375,np.float32)
    alpha_ref=artifacts.array(tmp_path,'alpha-float',opacity)
    artifacts.image=artifacts.image.model_copy(update={'alpha':alpha_ref})
    image_ref=artifacts.add(artifacts.image)
    artifacts.image_ref=image_ref
    case=case.model_copy(update={'working_image':image_ref,
        'binding':case.binding.model_copy(update={'source':image_ref})})
    root=tmp_path/'scratch-float';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,2_000_000,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        with open_holdout_workspace(artifacts,case,artifacts.image.color.icc_sha256,lambda:None) as workspace:
            np.testing.assert_array_equal(workspace.alpha,opacity)
