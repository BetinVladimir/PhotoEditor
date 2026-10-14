from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.restoration_preview_adoption import RestorationPreviewAdoptionCoordinator


class _IdleConnection:
    autocommit = True


def test_restoration_adopter_rejects_unknown_side_before_sql():
    reader, writer = _IdleConnection(), _IdleConnection()
    coordinator = RestorationPreviewAdoptionCoordinator(
        reader, writer, object(), 'photos',
        target_icc_sha256='a' * 64, working_icc_sha256='b' * 64)
    with pytest.raises(AdoptionInvalid, match='RESTORATION_PREVIEW_SCOPE_INVALID'):
        coordinator.adopt(uuid4(), uuid4(), 'candidate', 'preview1600')
    with pytest.raises(AdoptionInvalid, match='RESTORATION_PREVIEW_SCOPE_INVALID'):
        coordinator.adopt(uuid4(), uuid4(), 'before', 'full')
