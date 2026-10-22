"""Removal media writer scenarios; real PNG/scratch, upload authority doubles."""
from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from PIL import Image
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.semantic.preview_media_stage import stage_exposure_preview_media
from photo_worker.operations.semantic.preview_presentation import ExposureDisplay


@pytest.mark.parametrize('role', ['before', 'after', 'affected', 'protected'])
@pytest.mark.parametrize('failure', [None, 'upload', 'sha', 'alias', 'revoked'])
def test_removal_four_media_publication(tmp_path, role, failure):
    profile = b'unit-pinned-profile'
    digest = sha256(profile).hexdigest()
    rgb = np.full((2, 3, 3), .5, np.float32)
    alpha = np.full((2, 3), .75, np.float32)
    display = ExposureDisplay(rgb, alpha, profile, digest)
    overlay = np.zeros((2, 3, 4), np.uint8)
    forbidden = uuid4()
    calls = []
    allowed = True
    sizes = []
    paths = []

    class Artifacts:
        def stage(self, path, reservation, current, media):
            nonlocal allowed
            calls.append(current); paths.append(path)
            assert media == 'image/png'
            with Image.open(path) as png:
                assert png.size == (3, 2) and png.mode == 'RGBA'
                assert png.info == ({'icc_profile': profile} if current in ('before', 'after') else {})
            body = path.read_bytes(); sizes.append(len(body))
            if current == role and failure == 'upload':
                raise RuntimeError('object quota exhausted')
            if current == role and failure == 'revoked':
                allowed = False
            return ArtifactRef(
                artifact_id=forbidden if current == role and failure == 'alias' else uuid4(),
                sha256='0'*64 if current == role and failure == 'sha' else sha256(body).hexdigest())

    artifacts = Artifacts()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 4*1048576, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        def execute():
            return stage_exposure_preview_media(artifacts, before=display, after=display,
                affected=overlay, protected=overlay, forbidden_ids={forbidden}, srgb_sha=digest,
                max_bytes=128*1048576, max_output_bytes=4*1048576,
                verify_runtime=lambda: True, verify_admission=lambda: allowed,
                check_active=lambda: None)
        if failure:
            with pytest.raises((RuntimeError, ValueError)):
                execute()
            assert calls == ['before', 'after', 'affected', 'protected'][:['before', 'after', 'affected', 'protected'].index(role)+1]
        else:
            result = execute()
            assert result.bytes_written == sum(sizes)
            assert len({ref.artifact_id for ref in (result.before, result.after, result.affected, result.protected)}) == 4
    assert all(not path.exists() for path in paths)


@pytest.mark.parametrize('failure', ['profile', 'alpha_dtype', 'ram', 'output'])
def test_removal_media_preflight_refuses_before_scratch(failure):
    profile = b'unit-profile'; digest = sha256(profile).hexdigest()
    display = ExposureDisplay(np.zeros((2, 3, 3), np.float32),
        np.zeros((2, 3), np.float64 if failure == 'alpha_dtype' else np.float32),
        b'changed' if failure == 'profile' else profile, digest)
    class NoIO:
        @property
        def scratch(self):
            raise AssertionError('invalid media reached scratch')
    with pytest.raises(ValueError):
        stage_exposure_preview_media(NoIO(), before=display, after=display,
            affected=np.zeros((2, 3, 4), np.uint8), protected=np.zeros((2, 3, 4), np.uint8),
            forbidden_ids=set(), srgb_sha=digest,
            max_bytes=1 if failure == 'ram' else 128*1048576,
            max_output_bytes=1 if failure == 'output' else 4*1048576,
            verify_runtime=lambda: True, verify_admission=lambda: True, check_active=lambda: None)
