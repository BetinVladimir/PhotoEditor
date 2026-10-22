"""Authored source scenarios; no restore or runtime qualification is implied."""
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from uuid import uuid4
import pytest

from photo_worker.runtime.restore_recovery_load import _decode, load_pinned_restore_recovery


@dataclass(frozen=True)
class EvidenceFixture:
    run_id: object


def test_recovery_decoder_never_accepts_file_selected_type():
    with pytest.raises(ValueError, match='RESTORE_RECOVERY_TYPE'):
        _decode(object, {'module': 'os', 'callable': 'system'})


def test_recovery_decoder_rejects_bool_as_counter_and_negative_counts():
    for value in (True, -1, 2**53):
        with pytest.raises(ValueError):
            _decode(int, value)


def test_recovery_decoder_preserves_fixed_nested_types():
    from photo_worker.runtime.restore_cleanup import RestoreCleanupEvidence
    value = _decode(RestoreCleanupEvidence, {
        'schema_version': 'restore-cleanup/v1', 'database': 'isolated',
        'expired_claims': [['uploads', 3]],
    })
    assert value.expired_claims == (('uploads', 3),)
    with pytest.raises(ValueError, match='RESTORE_RECOVERY_FIELDS'):
        _decode(RestoreCleanupEvidence, {
            'schema_version': 'restore-cleanup/v1', 'database': 'isolated',
            'expired_claims': [], 'writers_authorized': True})


def test_recovery_loader_rejects_external_pin_before_constructing_evidence(tmp_path):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    path = tmp_path / (str(uuid4()) + '.json')
    path.write_bytes(canonical_bytes({'recovery': {}}))
    path.chmod(0o600)
    with pytest.raises(ValueError, match='RESTORE_REPLAY_PIN'):
        load_pinned_restore_recovery(path, '0' * 64, tmp_path / 'missing', '1' * 64,
            build_sha256='2' * 64, check_active=lambda: True)


def test_recovery_decoder_requires_absolute_paths_and_utc():
    with pytest.raises(ValueError, match='RESTORE_RECOVERY_PATH'):
        _decode(Path, 'relative')
    with pytest.raises(ValueError, match='RESTORE_RECOVERY_TIME'):
        _decode(datetime, '2026-10-17T12:00:00+02:00')
    assert _decode(datetime, '2026-10-17T10:00:00Z').tzinfo == timezone.utc
