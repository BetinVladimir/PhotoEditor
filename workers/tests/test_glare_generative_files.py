"""V28 full-frame synthetic output never edits outside the glasses mask."""

from uuid import uuid4

import numpy as np
import pytest

from photo_worker.operations.portrait.glare_generative_files import write_generative_glare_files
from photo_worker.operations.portrait.glare_generative_patch import GenerativeGlarePatch
from photo_worker.runtime.scratch import ScratchSpace


def test_generative_glare_files_keep_outside_exact_and_mark_generation(tmp_path):
    source = np.full((8, 8, 3), .2, np.float32)
    pixels = source[2:6, 2:6].copy()
    pixels[1, 1] = .8
    mask = np.zeros((4, 4), np.float32)
    mask[1, 1] = 1
    patch = GenerativeGlarePatch(pixels, mask, mask.copy(), 0)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1_000_000,
                      min_free_bytes=0) as scratch:
        with write_generative_glare_files(source, patch, (2, 2), scratch,
                target=mask, protected=np.zeros_like(mask),
                check_active=lambda: None) as files:
            output = np.load(files.rgb, allow_pickle=False)
            changed = np.load(files.changed, allow_pickle=False)
            generated = np.load(files.generated, allow_pickle=False)
            assert output[3, 3, 0] == pytest.approx(.8)
            assert changed[3, 3] == 1 and generated[3, 3] == 1
            outside = np.ones((8, 8), bool)
            outside[3, 3] = False
            assert np.array_equal(output[outside].view(np.uint32),
                                  source[outside].view(np.uint32))
            assert not generated[outside].any()
