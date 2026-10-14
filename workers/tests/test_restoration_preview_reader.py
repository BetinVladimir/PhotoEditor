from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.restoration_preview_reader import PostgresRestorationPreviewReader


class _IdleConnection:
    autocommit = True


def _reader():
    return PostgresRestorationPreviewReader(_IdleConnection(), object(),
        tenant_id=uuid4(), run_id=uuid4(), task_id=uuid4(), result_id=uuid4(),
        side='after', variant='preview2560', attempt=2, fence=3,
        allowed_bucket='photos')


def test_restoration_reader_refuses_unknown_side():
    with pytest.raises(ValueError):
        PostgresRestorationPreviewReader(_IdleConnection(), object(),
            tenant_id=uuid4(), run_id=uuid4(), task_id=uuid4(), result_id=uuid4(),
            side='candidate', variant='preview2560', attempt=2, fence=3,
            allowed_bucket='photos')


def test_restoration_reader_binds_result_side_and_fence():
    reader = _reader()
    first, second = uuid4(), uuid4()
    digest_a, digest_b = 'a' * 64, 'b' * 64
    calls = []

    def read(sql, args):
        calls.append((sql, args))
        return [(reader.task_id, reader.result_id, 2, 3, first, digest_a),
                (reader.task_id, reader.result_id, 2, 3, second, digest_b)]

    reader._read = read
    assert reader.staged_outputs() == {ArtifactRef(first, digest_a),
                                       ArtifactRef(second, digest_b)}
    assert len(calls) == 1
    assert calls[0][1][2:4] == ('after', 'preview2560')

    def stale(sql, args):
        return [(reader.task_id, reader.result_id, 2, 4, first, digest_a),
                (reader.task_id, reader.result_id, 2, 4, second, digest_b)]

    reader._read = stale
    with pytest.raises(AdoptionInvalid, match='RESTORATION_PREVIEW_RESULT_STALE'):
        reader.staged_outputs()
