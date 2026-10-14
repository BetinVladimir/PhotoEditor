"""Pinned process qualification selection is canonical and unambiguous."""

from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_import_adoption_policy import (
    import_adoption_qualifications_from_environment)
from test_interchange_import_adoption_qualification_report import fixture


def _load(tmp_path, qualifications):
    raw = canonical_bytes({'schema_version': 1, 'qualifications': qualifications})
    path = tmp_path / 'qualifications.json'
    path.write_bytes(raw)
    values = {
        'PHOTO_ADOPTER_INTERCHANGE_IMPORT_QUALIFICATIONS_FILE': str(path),
        'PHOTO_ADOPTER_INTERCHANGE_IMPORT_QUALIFICATIONS_SHA': sha256(raw).hexdigest(),
    }
    return import_adoption_qualifications_from_environment(
        lambda key, default='': values.get(key, default))


def _entry(expected, scale=None):
    return {'operation': expected['Operation'], 'scale': scale,
            'expected': expected, 'report_id': str(uuid4()),
            'report_sha': 'a' * 64}


def test_import_qualification_policy_requires_current_canonical_pin(tmp_path):
    expected, _ = fixture()
    loaded = _load(tmp_path, [_entry(expected)])
    assert loaded['image.denoise'].expected == expected
    assert import_adoption_qualifications_from_environment(lambda *_: '') is None
    with pytest.raises(ValueError, match='PIN'):
        import_adoption_qualifications_from_environment(lambda key, default='':
            '/tmp/missing.json' if key.endswith('_FILE') else default)


def test_import_qualification_policy_rejects_duplicate_or_wrong_scale(tmp_path):
    expected, _ = fixture()
    with pytest.raises(ValueError, match='PIN'):
        _load(tmp_path, [_entry(expected), _entry(expected)])
    upscale = dict(expected, Operation='image.upscale')
    with pytest.raises(ValueError, match='PIN'):
        _load(tmp_path, [_entry(upscale)])
    loaded = _load(tmp_path, [_entry(upscale, 2)])
    assert ('image.upscale', 2) in loaded
    malformed = _entry(expected)
    malformed['expected'] = dict(expected)
    malformed['expected'].pop('EvidenceSHAs')
    with pytest.raises(ValueError, match='PIN'):
        _load(tmp_path, [malformed])
    malformed = _entry(expected)
    malformed['report_id'] = str(uuid4()).upper()
    with pytest.raises(ValueError, match='PIN'):
        _load(tmp_path, [malformed])
