"""Load independently pinned recovery evidence without importing JSON type names."""
from dataclasses import fields, is_dataclass, asdict
from datetime import datetime
from hashlib import sha256
from pathlib import Path
from typing import get_type_hints, get_origin, get_args
from uuid import UUID

from photo_worker.contracts.base import parse_json, canonical_bytes
from .restore_key_installation import _read_private
from .restore_recovery import RestoreComputeRecoveryEvidence
from .restore_report import _json
from .restore_replay_receipt import verify_restore_replay_files


def _decode(kind, value):
    # The reachable type graph is fixed by the source dataclass, never the file.
    if is_dataclass(kind):
        names = {field.name for field in fields(kind)}
        if not isinstance(value, dict) or set(value) != names:
            raise ValueError('RESTORE_RECOVERY_FIELDS')
        hints = get_type_hints(kind)
        return kind(**{name: _decode(hints[name], item) for name, item in value.items()})
    args = get_args(kind)
    if get_origin(kind) is tuple:
        if not isinstance(value, list) or len(value) > 10_000_000:
            raise ValueError('RESTORE_RECOVERY_TUPLE')
        if len(args) == 2 and args[1] is Ellipsis:
            return tuple(_decode(args[0], item) for item in value)
        if len(args) != len(value):
            raise ValueError('RESTORE_RECOVERY_TUPLE')
        return tuple(_decode(part, item) for part, item in zip(args, value))
    if kind in (str, int, bool):
        if type(value) is not kind:
            raise ValueError('RESTORE_RECOVERY_TYPE')
        if kind is int and not 0 <= value <= 2**53-1:
            raise ValueError('RESTORE_RECOVERY_INTEGER')
        return value
    if kind is UUID:
        result = UUID(value)
        if result.int == 0 or str(result) != value:
            raise ValueError('RESTORE_RECOVERY_UUID')
        return result
    if kind is Path:
        result = Path(value)
        if not result.is_absolute():
            raise ValueError('RESTORE_RECOVERY_PATH')
        return result
    if kind is datetime:
        result = datetime.fromisoformat(value.replace('Z', '+00:00'))
        if result.tzinfo is None or result.utcoffset().total_seconds() != 0:
            raise ValueError('RESTORE_RECOVERY_TIME')
        return result
    raise ValueError('RESTORE_RECOVERY_TYPE')


def load_pinned_restore_recovery(recovery_path, recovery_sha256, replay_path,
                                 replay_sha256, *, build_sha256, check_active):
    """Return typed evidence and verified replay; never derive operator pins."""
    raw = _read_private(recovery_path, 8 << 20, check_active)
    if sha256(raw).hexdigest() != recovery_sha256:
        raise ValueError('RESTORE_REPLAY_PIN')
    body = parse_json(raw, max_bytes=8 << 20)
    if canonical_bytes(body) != raw or not isinstance(body, dict) or 'recovery' not in body:
        raise ValueError('RESTORE_REPLAY_CANONICAL')
    evidence = _decode(RestoreComputeRecoveryEvidence, body['recovery'])
    if _json(asdict(evidence)) != body['recovery']:
        raise ValueError('RESTORE_RECOVERY_ROUNDTRIP')
    receipt = verify_restore_replay_files(recovery_path, recovery_sha256, replay_path,
        replay_sha256, evidence=evidence, build_sha256=build_sha256, check_active=check_active)
    return evidence, receipt
