"""Pinned operator configuration installs four concrete restore probe families.

No imports, callable names, arbitrary SQL or claimed boolean observations are
accepted from JSON. Runtime authority and a live in-process renderer barrier are
separate resources, never serialized evidence. Construction performs no API
mutations; each installed runner retains its own physical/catalogue checks.
"""
from dataclasses import fields, MISSING, is_dataclass
from datetime import datetime
from hashlib import sha256
from pathlib import Path
from types import UnionType
from typing import get_type_hints, get_origin, get_args, Union
from uuid import UUID
import re
import ssl
import stat
import os

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.export import EncodeParams
from photo_worker.color.icc import ICCRegistry, validate_icc
from .restore_key_installation import _read_private
from .restore_authority import require_restore_authority, require_restore_active
from .restore_probe_dispatch import (
    RestoreProbeDispatch, RestoreInstalledLegacyProbe, RestoreInstalledV2Probe,
    RestoreInstalledPurgeProbe, RestoreInstalledRevokeProbe,
    )
from .restore_legacy_lifecycle import RestoreLegacyInstallation
from .restore_legacy_full_probe import RestoreLegacyExportPlan
from .restore_legacy_preview import RestoreLegacyPreviewTarget
from .restore_legacy_apply import RestoreLegacyApplyTarget
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_preview_download import RestorePreviewDownloadTarget
from .restore_revoke_batch import RestoreRenderBatchTarget
from .restore_revoke_freeze import RestoreFreezeBaselineBinding, RestoreFreezeBaselineEvidence, RestoreFreezeDenialTarget
from .restore_revoke_probe import RestoreRevokeFixture
from .restore_revoke_lifecycle import RestoreConsentRevokeTarget
from .restore_revoke_access import RestoreRevokeDocumentTarget
from .restore_revoke_runner import RestoreRevokeBatchInstallation, validate_fixture_revoke_installation
from .restore_revoke_report_runner import RestoreAdmissionRevokeInstallation
from .restore_purge_lifecycle import RestoreTenantDeletionTarget
from .restore_access_probe import RestoreAccessTarget
from .restore_s3_probe import RestoreS3AccessTarget
from .restore_inventory import RestoreObject
from .restore_render_barrier import RestoreBarrierRenderer
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_probe_report import validate_probe_report_root

_SHA = re.compile('[0-9a-f]{64}')
_TYPES = frozenset((RestoreLegacyPreviewTarget, RestoreLegacyApplyTarget,
    RestoreLegacyApplyBinding, RestorePreviewDownloadTarget, RestoreRenderBatchTarget,
    RestoreFreezeBaselineBinding, RestoreFreezeBaselineEvidence, RestoreFreezeDenialTarget,
    RestoreRevokeFixture, RestoreConsentRevokeTarget, RestoreRevokeDocumentTarget,
    RestoreRevokeBatchInstallation, RestoreTenantDeletionTarget, RestoreAccessTarget,
    RestoreS3AccessTarget, RestoreObject, RestoreLegacyInstallation, RestoreLegacyExportPlan,
    RestoreAdmissionRevokeInstallation))


def _record(value, keys):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ValueError('RESTORE_PROBE_CONFIG_FIELDS')
    return value


def _pin(value):
    if not isinstance(value, str) or _SHA.fullmatch(value) is None:
        raise ValueError('RESTORE_PROBE_CONFIG_PIN')
    return value


def _read_pin(value, maximum, active):
    _record(value, ('path', 'sha256'))
    require_restore_active(active)
    raw = _read_private(value['path'], maximum, active)
    if len(raw) > maximum or sha256(raw).hexdigest() != _pin(value['sha256']):
        raise ValueError('RESTORE_PROBE_CONFIG_FILE_PIN')
    require_restore_active(active)
    return raw


def _value(kind, value, active):
    origin, args = get_origin(kind), get_args(kind)
    if origin in (UnionType, Union):
        if value is None and type(None) in args:
            return None
        choices = tuple(item for item in args if item is not type(None))
        if len(choices) != 1:
            raise ValueError('RESTORE_PROBE_CONFIG_TYPE')
        return _value(choices[0], value, active)
    if origin is tuple:
        if not isinstance(value, list) or len(value) > 5000:
            raise ValueError('RESTORE_PROBE_CONFIG_TUPLE')
        if len(args) == 2 and args[1] is Ellipsis:
            return tuple(_value(args[0], item, active) for item in value)
        if len(value) != len(args):
            raise ValueError('RESTORE_PROBE_CONFIG_TUPLE')
        return tuple(_value(part, item, active) for part, item in zip(args, value))
    if kind is UUID:
        if not isinstance(value, str):
            raise ValueError('RESTORE_PROBE_CONFIG_UUID')
        result = UUID(value)
        if result.int == 0 or str(result) != value:
            raise ValueError('RESTORE_PROBE_CONFIG_UUID')
        return result
    if kind is bytes:
        return _read_pin(value, 8 << 20, active)
    if kind is Path:
        if not isinstance(value, str):
            raise ValueError('RESTORE_PROBE_CONFIG_PATH')
        result = Path(value)
        if not result.is_absolute() or result.resolve(strict=True) != result:
            raise ValueError('RESTORE_PROBE_CONFIG_PATH')
        return result
    if kind is datetime:
        if not isinstance(value, str):
            raise ValueError('RESTORE_PROBE_CONFIG_TIME')
        result = datetime.fromisoformat(value.replace('Z', '+00:00'))
        if result.tzinfo is None or result.utcoffset().total_seconds() != 0:
            raise ValueError('RESTORE_PROBE_CONFIG_TIME')
        return result
    if kind is int:
        if type(value) is not int or not 0 <= value <= 2**53-1:
            raise ValueError('RESTORE_PROBE_CONFIG_INTEGER')
        return value
    if kind is str:
        if not isinstance(value, str) or not 1 <= len(value.encode()) <= 16384 or '\r' in value or '\n' in value or '\0' in value:
            raise ValueError('RESTORE_PROBE_CONFIG_STRING')
        return value
    if kind in _TYPES:
        return _build(kind, value, active)
    raise ValueError('RESTORE_PROBE_CONFIG_TYPE')


def _build(kind, raw, active, **installed):
    if kind not in _TYPES or not is_dataclass(kind) or not isinstance(raw, dict):
        raise ValueError('RESTORE_PROBE_CONFIG_TYPE')
    declared = {item.name: item for item in fields(kind)}
    required = {name for name, item in declared.items() if item.default is MISSING and item.default_factory is MISSING}
    if not required <= set(raw) or not set(raw) <= set(declared) or not set(installed) <= set(raw):
        raise ValueError('RESTORE_PROBE_CONFIG_FIELDS')
    annotations = get_type_hints(kind)
    result = {name: installed[name] if name in installed else _value(annotations[name], value, active)
              for name, value in raw.items()}
    return kind(**result)


def load_restore_probe_dispatch(config_path, config_sha256, *, connection, client, replay,
                                renderer_barrier, assert_probe_isolation, check_active):
    """Return (typed dispatch, independent pins) only after complete installation.

    Caller keeps the actual barrier mounted on the isolated worker until the
    running-revoke probe observes cancellation. No factory-created success flags.
    The connection must be dedicated/autocommit: runners own their transactions.
    """
    if (not isinstance(replay, VerifiedRestoreReplay) or not isinstance(renderer_barrier, RestoreBarrierRenderer)
            or connection is None or client is None or not callable(assert_probe_isolation) or not callable(check_active)):
        raise ValueError('RESTORE_PROBE_CONFIG_INSTALLATION')
    def isolated():
        require_restore_authority(check_active, assert_probe_isolation)
    isolated()
    raw = _read_private(config_path, 1 << 20, check_active)
    if stat.S_IMODE(os.stat(config_path, follow_symlinks=False).st_mode) & 0o077:
        raise ValueError('RESTORE_PROBE_CONFIG_PRIVATE')
    if sha256(raw).hexdigest() != _pin(config_sha256):
        raise ValueError('RESTORE_PROBE_CONFIG_PIN')
    value = parse_json(raw, max_bytes=1 << 20)
    _record(value, ('schema_version', 'run_id', 'database', 'build_sha256', 'recovery_sha256', 'replay_sha256',
        'report_root', 'buckets', 'fixture_pins', 'tls_ca', 'icc_lock', 'srgb_profile', 'pepper',
        'legacy', 'v2', 'purge', 'revoke'))
    if (value['schema_version'] != 'restore-probe-installation/v1'
            or (value['run_id'], value['database'], value['build_sha256'], value['recovery_sha256'], value['replay_sha256'])
            != (str(replay.run_id), replay.database, replay.build_sha256, replay.recovery_sha256, replay.replay_sha256)):
        raise ValueError('RESTORE_PROBE_CONFIG_REPLAY')
    _record(value['fixture_pins'], ('legacy_apply', 'v2_workflow', 'purge_negative', 'revoke_negative'))
    pins = {name: _pin(pin) for name, pin in value['fixture_pins'].items()}
    buckets = value['buckets']
    if not isinstance(buckets, list) or not 1 <= len(buckets) <= 16 or any(not isinstance(b, str) or re.fullmatch('[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]', b) is None for b in buckets) or len(set(buckets)) != len(buckets):
        raise ValueError('RESTORE_PROBE_CONFIG_BUCKETS')
    buckets = tuple(buckets)
    report_root = validate_probe_report_root(_value(Path, value['report_root'], check_active))
    ca = _read_pin(value['tls_ca'], 4 << 20, check_active)
    tls = ssl.create_default_context(cadata=ca.decode('ascii'))
    tls.minimum_version = ssl.TLSVersion.TLSv1_2
    lock_raw = _read_pin(value['icc_lock'], 1 << 20, check_active)
    registry = ICCRegistry(Path(value['icc_lock']['path']).parent)
    if canonical_bytes(registry.manifest) != canonical_bytes(parse_json(lock_raw, max_bytes=1 << 20)):
        raise ValueError('RESTORE_PROBE_CONFIG_ICC')
    srgb = _read_pin(value['srgb_profile'], 4 << 20, check_active)
    validate_icc(srgb)
    if registry.resolve('srgb_v1').data != srgb:
        raise ValueError('RESTORE_PROBE_CONFIG_ICC')
    pepper = _read_pin(value['pepper'], 4096, check_active)
    if not 32 <= len(pepper) <= 4096:
        raise ValueError('RESTORE_PROBE_CONFIG_PEPPER')

    l = _record(value['legacy'], ('installation', 'plan', 'maximum_seconds'))
    li = l['installation']
    installation = _build(RestoreLegacyInstallation, li, check_active,
        preview_target=_build(RestoreLegacyPreviewTarget, li['preview_target'], check_active),
        apply_target=_build(RestoreLegacyApplyTarget, li['apply_target'], check_active),
        binding=_build(RestoreLegacyApplyBinding, li['binding'], check_active),
        download_target=_build(RestorePreviewDownloadTarget, li['download_target'], check_active))
    plan = _build(RestoreLegacyExportPlan, l['plan'], check_active,
        params=EncodeParams.model_validate(l['plan']['params']))
    v = _record(value['v2'], ('target', 'binding', 'frozen', 'source_raw', 'download_target', 'session_id', 'maximum_seconds'))
    p = _record(value['purge'], ('references', 'deletion_target', 'api_target', 'api_sha256', 's3_target', 'maximum_seconds'))
    if not isinstance(p['references'], list) or not 1 <= len(p['references']) <= 5000:
        raise ValueError('RESTORE_PROBE_CONFIG_PURGE')
    r = _record(value['revoke'], ('admission', 'running_target', 'child_run_id', 'maximum_seconds'))
    admission_raw = r['admission']
    admission = _build(RestoreAdmissionRevokeInstallation, admission_raw, check_active,
        target=_build(RestoreConsentRevokeTarget, admission_raw['target'], check_active),
        document=_build(RestoreRevokeDocumentTarget, admission_raw['document'], check_active),
        positive_freeze=_build(RestoreFreezeDenialTarget, admission_raw['positive_freeze'], check_active),
        negative_freeze=_build(RestoreFreezeDenialTarget, admission_raw['negative_freeze'], check_active),
        binding=_build(RestoreFreezeBaselineBinding, admission_raw['binding'], check_active),
        batch=_build(RestoreRevokeBatchInstallation, admission_raw['batch'], check_active))

    def fixture_guard():
        isolated()
        if sha256(_read_private(config_path, 1 << 20, check_active)).hexdigest() != config_sha256:
            raise ValueError('RESTORE_PROBE_CONFIG_CHANGED')
        row = connection.execute("SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)").fetchone()
        if row != (replay.database, True):
            raise ValueError('RESTORE_PROBE_CONFIG_DATABASE')
        isolated()
    shared = dict(connection=connection, client=client, buckets=buckets, tls_context=tls,
                  assert_installed_fixture=fixture_guard)
    dispatch = RestoreProbeDispatch(report_root,
        RestoreInstalledLegacyProbe(**shared, installation=installation, plan=plan, srgb_profile=srgb,
            icc_registry=registry, pepper=pepper, maximum_seconds=_value(int, l['maximum_seconds'], check_active)),
        RestoreInstalledV2Probe(**shared, target=_build(RestoreRenderBatchTarget, v['target'], check_active),
            binding=_build(RestoreFreezeBaselineBinding, v['binding'], check_active),
            frozen=_build(RestoreFreezeBaselineEvidence, v['frozen'], check_active),
            source_raw=_read_pin(v['source_raw'], 8 << 20, check_active),
            download_target=_build(RestorePreviewDownloadTarget, v['download_target'], check_active),
            session_id=_value(UUID, v['session_id'], check_active), srgb_profile=srgb,
            maximum_seconds=_value(int, v['maximum_seconds'], check_active)),
        RestoreInstalledPurgeProbe(**shared,
            references=tuple(_build(RestoreObject, item, check_active) for item in p['references']),
            deletion_target=_build(RestoreTenantDeletionTarget, p['deletion_target'], check_active),
            api_target=_build(RestoreAccessTarget, p['api_target'], check_active), api_sha256=_pin(p['api_sha256']),
            s3_target=_build(RestoreS3AccessTarget, p['s3_target'], check_active),
            maximum_seconds=_value(int, p['maximum_seconds'], check_active)),
        RestoreInstalledRevokeProbe(**shared, admission=admission,
            running_target=_build(RestoreConsentRevokeTarget, r['running_target'], check_active),
            child_run_id=_value(UUID, r['child_run_id'], check_active), renderer_barrier=renderer_barrier,
            maximum_seconds=_value(int, r['maximum_seconds'], check_active)))
    fixture_guard()
    validate_fixture_revoke_installation(admission.target, admission.document, admission.positive_freeze,
        admission.negative_freeze, admission.binding, batch_installation=admission.batch,
        assert_installed_fixture=fixture_guard, check_active=check_active)
    dispatch.validate_all(replay, pins, check_active)
    fixture_guard()
    return dispatch, pins
