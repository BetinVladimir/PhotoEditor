"""Glare donor material uses a dedicated attempt-scoped input catalogue."""

from types import SimpleNamespace
from unittest.mock import Mock
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.glare_donor_object_reader import GlareWorkerObjectReader


def test_glare_donor_reader_requires_exact_worker_catalogue():
    ref = ArtifactRef(uuid4(), 'a' * 64)
    location = {'bucket': 'photos', 'object_key': 'exact/input',
        'object_version': 'version-1', 'sha256': ref.sha256,
        'bytes': 12, 'media_type': 'application/x-npy',
        'role': 'pixels', 'source': 'input'}
    lookup = Mock(return_value=location)
    db = SimpleNamespace(glare_donor_input_evidence=lookup)
    lease = SimpleNamespace(command=SimpleNamespace(task_id=uuid4()))
    reader = GlareWorkerObjectReader(db, lease, b'{}', b'{}', object(),
        bucket='photos', check_active=lambda: None)
    evidence = reader.evidence(ref)
    assert evidence.ref == ref and evidence.source == 'input'
    lookup.assert_called_once()
    lookup.return_value = {**location, 'source': 'output'}
    with pytest.raises(AdoptionInvalid, match='DONOR_INPUT_EVIDENCE_INVALID'):
        reader.evidence(ref)
