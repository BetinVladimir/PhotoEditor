from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.learning.contracts import RenderCase
from photo_worker.operations.learning.source import open_holdout_source

from test_image_color_adapter import Artifacts


def _fixture(tmp_path):
    artifacts = Artifacts(tmp_path, False)
    image = artifacts.image
    guard = np.zeros(image.canvas.height * image.canvas.width,
                     dtype=np.float32).reshape(image.canvas.height,
                                               image.canvas.width)
    mask = MaskRef(artifact=artifacts.array(tmp_path, 'protected', guard),
                   canvas=image.canvas, transform_chain=image.transform_chain,
                   selector='protected', confidence=1,
                   model_lock_id=None, protected=True)
    mask_ref = artifacts.add(mask)
    case = RenderCase(binding=dict(asset_version_id=uuid4(),
                                   source=artifacts.image_ref,
                                   canvas_hash=canonical_hash(image.canvas)),
                      working_image=artifacts.image_ref,
                      base_recipe=artifacts.image_ref,
                      approved_recipe=artifacts.image_ref,
                      base_document=artifacts.image_ref,
                      approved_document=artifacts.image_ref,
                      descriptor_shard=artifacts.image_ref,
                      descriptor_analysis_id=uuid4(), descriptor_ordinal=0,
                      protected=mask_ref, faces=artifacts.image_ref,
                      approval_id=uuid4(), source_lineage_sha='a' * 64)
    return artifacts, case


def test_pinned_holdout_source_maps_exact_children(tmp_path):
    artifacts, case = _fixture(tmp_path)
    with open_holdout_source(artifacts, case,
                             artifacts.image.color.icc_sha256,
                             lambda: None) as opened:
        np.testing.assert_array_equal(opened.rgb, artifacts.rgb)
        assert opened.alpha.shape == artifacts.rgb.shape[:2]
        assert not np.any(opened.protected)
        assert isinstance(opened.rgb, np.memmap)


def test_holdout_source_rejects_profile_and_canvas_mismatch(tmp_path):
    artifacts, case = _fixture(tmp_path)
    with pytest.raises(ValueError, match='HOLDOUT_SOURCE_GEOMETRY_INVALID'):
        with open_holdout_source(artifacts, case, '0' * 64, lambda: None):
            pass
    case.binding.canvas_hash = 'f' * 64
    with pytest.raises(ValueError, match='HOLDOUT_SOURCE_GEOMETRY_INVALID'):
        with open_holdout_source(artifacts, case,
                                 artifacts.image.color.icc_sha256,
                                 lambda: None):
            pass


def test_holdout_source_maps_effective_mask_and_rejects_protected_alias(tmp_path):
    artifacts, case = _fixture(tmp_path)
    pixels = np.ones(artifacts.rgb.shape[:2], np.float32)
    mask = MaskRef(artifact=artifacts.array(tmp_path, 'effective', pixels),
                   canvas=artifacts.image.canvas,
                   transform_chain=artifacts.image.transform_chain,
                   selector='person', confidence=1, model_lock_id=None,
                   protected=False)
    case = RenderCase.model_validate({**case.model_dump(mode='python'),
        'effective_mask': artifacts.add(mask),
        'consent_snapshot': artifacts.image_ref})
    with open_holdout_source(artifacts, case,
                             artifacts.image.color.icc_sha256,
                             lambda: None) as opened:
        np.testing.assert_array_equal(opened.effective_mask, pixels)
    case = RenderCase.model_validate({**case.model_dump(mode='python'),
        'effective_mask': case.protected})
    with pytest.raises(ValueError, match='HOLDOUT_EFFECTIVE_MASK_GEOMETRY_INVALID'):
        with open_holdout_source(artifacts, case,
                                 artifacts.image.color.icc_sha256,
                                 lambda: None):
            pass


def test_holdout_source_maps_frozen_region_mask(tmp_path):
    artifacts, case = _fixture(tmp_path)
    pixels = np.full(artifacts.rgb.shape[:2], .3, np.float32)
    mask = MaskRef(artifact=artifacts.array(tmp_path, 'sky-region', pixels),
                   canvas=artifacts.image.canvas,
                   transform_chain=artifacts.image.transform_chain,
                   selector='sky', confidence=.9, model_lock_id=None,
                   protected=False)
    case = RenderCase.model_validate({**case.model_dump(mode='python'),
        'region_masks': [{'region_id': 'sky', 'mask': artifacts.add(mask)}]})
    with open_holdout_source(artifacts, case,
                             artifacts.image.color.icc_sha256,
                             lambda: None) as opened:
        found, mapped = opened.region_masks['sky']
        assert found.selector == 'sky'
        np.testing.assert_array_equal(mapped, pixels)
