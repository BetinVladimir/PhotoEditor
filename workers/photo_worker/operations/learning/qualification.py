"""Deployment-signed V23 Evaluate qualification; never self-issues a pass."""
from __future__ import annotations

from dataclasses import dataclass
from contextlib import ExitStack, contextmanager
from datetime import datetime, timezone
from hashlib import sha256
from math import isfinite
import os
from pathlib import Path
import re
import stat
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, parse_json


_SHA = re.compile(r'[0-9a-f]{64}\Z')
_EFFECTS = frozenset({'color', 'local', 'region', 'beauty', 'background.solid',
    'background.transparent', 'background.blur', 'background.asset',
    'restoration.denoise', 'restoration.sensor_dust', 'defects.repair',
    'portrait.detail'})
_CHECKS = frozenset({'detector', 'identity', 'rendered_color', 'clipping',
    'protected_pixels', 'consent', 'resources', 'real_images'})
_EFFECT_CHECKS = {
    'defects.repair': 'reviewed_defects',
    'restoration.sensor_dust': 'reviewed_sensor_dust',
    'restoration.denoise': 'denoise_quality',
    'background.blur': 'depth_quality',
    'background.asset': 'asset_decoder',
    'portrait.detail': 'portrait_detail',
}
_EFFECT_MODELS = frozenset({'restoration.denoise', 'background.blur'})
_FIELDS = frozenset({'schema_version', 'operation', 'operation_version',
    'renderer_version', 'backend', 'model_lock_sha', 'icc_lock_sha',
    'source_tree_sha', 'packages_lock_sha', 'device', 'effects', 'checks',
    'metrics', 'effect_peaks', 'dataset_sha', 'issuer_id', 'expires_at', 'status',
    'peak_vram_bytes'})
_METRICS = frozenset({'baseline_loss', 'candidate_loss_ratio',
    'color_p95_delta', 'clipping_delta_fraction', 'protected_max_error',
    'identity_failures', 'holdout_sessions', 'holdout_examples'})


@dataclass(frozen=True)
class LearningQualification:
    model_lock_sha: str
    icc_lock_sha: str
    source_tree_sha: str
    device: tuple[str, int, int, int]
    effects: frozenset[str]
    peak_vram_bytes: int
    expires_at: datetime
    report_sha: str
    effect_peaks: tuple = ()

    def peak_for(self, effect: str) -> dict[str, int]:
        matches = [dict(values) for name, values in self.effect_peaks if name == effect]
        if len(matches) != 1:
            raise ValueError('LEARNING_EFFECT_PEAK_MISSING')
        return matches[0]


def required_learning_effects(styles) -> frozenset[str]:
    """Derive the exact currently executable effects from all three arms."""
    required = {'color'}
    for style in (styles.baseline, styles.candidate, styles.approved):
        if style.local.enabled:
            required.add('local')
        if style.extensions.region_adjustments:
            required.add('region')
        if style.beauty.enabled:
            required.add('beauty')
        if style.defects.enabled:
            required.add('defects.repair')
        detail = style.extensions.portrait_detail
        if detail is not None and detail.enabled:
            required.add('portrait.detail')
        background = style.extensions.background
        if background is not None and background.mode != 'keep':
            required.add('background.' + background.mode)
        restoration = style.extensions.restoration
        if restoration is not None:
            if restoration.upscale != 'off':
                raise ValueError('LEARNING_EFFECT_UNQUALIFIED')
            if restoration.denoise != 'off':
                required.add('restoration.denoise')
            if restoration.sensor_dust == 'review':
                required.add('restoration.sensor_dust')
            elif restoration.sensor_dust != 'off':
                raise ValueError('LEARNING_EFFECT_UNQUALIFIED')
    if not required <= _EFFECTS:
        raise ValueError('LEARNING_EFFECT_UNQUALIFIED')
    return frozenset(required)


def source_tree_sha(root: Path) -> str:
    """Hash every Python source file in the installed photo_worker package."""
    root = Path(root)
    if not root.is_absolute() or not root.is_dir() or root.is_symlink():
        raise ValueError('LEARNING_SOURCE_TREE_INVALID')
    with _pinned_directory(root) as root_directory:
        pinned_root = os.fstat(root_directory)
        return _hash_source_tree(root, root_directory, pinned_root)


@contextmanager
def _directory_descriptor(descriptor):
    # Directory FDs are not byte streams (fdopen rejects them on macOS).
    try:
        yield descriptor
    finally:
        os.close(descriptor)


@contextmanager
def _pinned_directory(path: Path):
    path = Path(path)
    if not path.is_absolute() or '..' in path.parts:
        raise ValueError('LEARNING_PINNED_DIRECTORY_INVALID')
    with ExitStack() as descriptors:
        directory = descriptors.enter_context(_directory_descriptor(os.open(
            path.anchor, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW |
            os.O_CLOEXEC)))
        for component in path.parts[1:]:
            if component in ('', '.', '..'):
                raise ValueError('LEARNING_PINNED_DIRECTORY_INVALID')
            directory = descriptors.enter_context(_directory_descriptor(os.open(
                component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW |
                os.O_CLOEXEC, dir_fd=directory)))
        yield directory


def _file_identity(value: os.stat_result) -> tuple[int, ...]:
    return (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns,
            value.st_ctime_ns, value.st_mode, value.st_nlink)


def _hash_source_tree(root: Path, root_directory: int,
                      pinned_root: os.stat_result) -> str:
    digest = sha256()
    paths: list[tuple[str, ...]] = []
    visited = 0

    def collect(directory: int, prefix: tuple[str, ...]) -> None:
        nonlocal visited
        if len(prefix) > 32:
            raise ValueError('LEARNING_SOURCE_TREE_INVALID')
        names = []
        with os.scandir(directory) as entries:
            for entry in entries:
                visited += 1
                if visited > 100000:
                    raise ValueError('LEARNING_SOURCE_TREE_INVALID')
                names.append(entry.name)
        names.sort()
        for name in names:
            if (name in ('', '.', '..') or
                    len('/'.join((*prefix, name)).encode('utf-8')) > 4096):
                raise ValueError('LEARNING_SOURCE_TREE_INVALID')
            metadata = os.stat(name, dir_fd=directory, follow_symlinks=False)
            if stat.S_ISLNK(metadata.st_mode):
                raise ValueError('LEARNING_SOURCE_TREE_INVALID')
            if stat.S_ISDIR(metadata.st_mode):
                with _directory_descriptor(os.open(name, os.O_RDONLY | os.O_DIRECTORY |
                        os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)) as child:
                    opened = os.fstat(child)
                    if (opened.st_dev, opened.st_ino) != (metadata.st_dev, metadata.st_ino):
                        raise ValueError('LEARNING_SOURCE_TREE_INVALID')
                    collect(child, (*prefix, name))
                    current = os.stat(name, dir_fd=directory, follow_symlinks=False)
                    if (not stat.S_ISDIR(current.st_mode) or
                            (current.st_dev, current.st_ino) !=
                            (metadata.st_dev, metadata.st_ino)):
                        raise ValueError('LEARNING_SOURCE_TREE_INVALID')
            elif name.endswith('.py'):
                if not stat.S_ISREG(metadata.st_mode):
                    raise ValueError('LEARNING_SOURCE_TREE_INVALID')
                paths.append((*prefix, name))
                if len(paths) > 10000:
                    raise ValueError('LEARNING_SOURCE_TREE_INVALID')

    collect(root_directory, ())
    paths.sort(key=lambda parts: '/'.join(parts))
    if not 1 <= len(paths) <= 10000:
        raise ValueError('LEARNING_SOURCE_TREE_INVALID')
    total_bytes = 0
    for relative in paths:
        name = '/'.join(relative).encode('utf-8')
        if len(name) > 4096:
            raise ValueError('LEARNING_SOURCE_TREE_INVALID')
        with ExitStack() as descriptors:
            directory = descriptors.enter_context(_directory_descriptor(os.dup(
                root_directory)))
            for component in relative[:-1]:
                directory = descriptors.enter_context(_directory_descriptor(os.open(
                    component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW |
                    os.O_CLOEXEC, dir_fd=directory)))
            descriptor = os.open(relative[-1],
                os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                dir_fd=directory)
            stream = descriptors.enter_context(os.fdopen(descriptor, 'rb'))
            metadata = os.fstat(stream.fileno())
            if (not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1 or
                    not 0 <= metadata.st_size <= 8 * 1024 * 1024):
                raise ValueError('LEARNING_SOURCE_TREE_INVALID')
            content = stream.read(metadata.st_size + 1)
            after = os.fstat(stream.fileno())
            current = os.stat(relative[-1], dir_fd=directory,
                              follow_symlinks=False)
            if (len(content) != metadata.st_size or
                    not stat.S_ISREG(current.st_mode) or current.st_nlink != 1 or
                    _file_identity(metadata) != _file_identity(after) or
                    _file_identity(metadata) != _file_identity(current)):
                raise ValueError('LEARNING_SOURCE_TREE_INVALID')
        total_bytes += len(content)
        if total_bytes > 128 * 1024 * 1024:
            raise ValueError('LEARNING_SOURCE_TREE_INVALID')
        digest.update(len(name).to_bytes(4, 'big'))
        digest.update(name)
        digest.update(len(content).to_bytes(8, 'big'))
        digest.update(content)
    current_root = os.stat(root, follow_symlinks=False)
    if (not stat.S_ISDIR(current_root.st_mode) or
            (pinned_root.st_dev, pinned_root.st_ino) !=
            (current_root.st_dev, current_root.st_ino)):
        raise ValueError('LEARNING_SOURCE_TREE_INVALID')
    return digest.hexdigest()


def verify_learning_evidence(root: Path, digests: set[str], *,
                             capture: str | None = None) -> bytes | None:
    """Require exact local, bounded evidence bytes for every signed check."""
    root = Path(root)
    if (not root.is_dir() or root.is_symlink() or not 1 <= len(digests) <= 100):
        raise ValueError('LEARNING_EVIDENCE_UNAVAILABLE')
    captured = None
    with _pinned_directory(root) as evidence_dir:
        opened_root = os.fstat(evidence_dir)
        for expected in sorted(digests):
            if not isinstance(expected, str) or _SHA.fullmatch(expected) is None:
                raise ValueError('LEARNING_EVIDENCE_UNAVAILABLE')
            digest = sha256()
            chunks = [] if expected == capture else None
            descriptor = os.open(expected + '.json',
                os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                dir_fd=evidence_dir)
            with os.fdopen(descriptor, 'rb') as stream:
                metadata = os.fstat(stream.fileno())
                if (not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1 or
                        not 2 <= metadata.st_size <= 8 * 1024 * 1024):
                    raise ValueError('LEARNING_EVIDENCE_UNAVAILABLE')
                total = 0
                for block in iter(lambda: stream.read(1024 * 1024), b''):
                    total += len(block)
                    if total > 8 * 1024 * 1024:
                        raise ValueError('LEARNING_EVIDENCE_UNAVAILABLE')
                    digest.update(block)
                    if chunks is not None:
                        chunks.append(block)
                after = os.fstat(stream.fileno())
                current = os.stat(expected + '.json', dir_fd=evidence_dir,
                                  follow_symlinks=False)
                if (not stat.S_ISREG(current.st_mode) or current.st_nlink != 1 or
                        _file_identity(metadata) != _file_identity(after) or
                        _file_identity(metadata) != _file_identity(current)):
                    raise ValueError('LEARNING_EVIDENCE_CHANGED')
            if total != metadata.st_size or digest.hexdigest() != expected:
                raise ValueError('LEARNING_EVIDENCE_CHANGED')
            if chunks is not None:
                captured = b''.join(chunks)
        current_root = os.stat(root, follow_symlinks=False)
        if (not stat.S_ISDIR(current_root.st_mode) or
                (opened_root.st_dev, opened_root.st_ino) !=
                (current_root.st_dev, current_root.st_ino)):
            raise ValueError('LEARNING_EVIDENCE_ROOT_CHANGED')
    return captured


def packages_lock_sha(path: Path) -> str:
    """Hash the installed dependency lock without following replacements."""
    path = Path(path)
    with _pinned_directory(path.parent) as directory:
        opened_directory = os.fstat(directory)
        descriptor = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW |
                             os.O_CLOEXEC, dir_fd=directory)
        with os.fdopen(descriptor, 'rb') as stream:
            metadata = os.fstat(stream.fileno())
            if (not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1 or
                    not 1 <= metadata.st_size <= 8 * 1024 * 1024):
                raise ValueError('LEARNING_PACKAGES_LOCK_INVALID')
            content = stream.read(metadata.st_size + 1)
            after = os.fstat(stream.fileno())
            current = os.stat(path.name, dir_fd=directory,
                              follow_symlinks=False)
            if (len(content) != metadata.st_size or
                    not stat.S_ISREG(current.st_mode) or current.st_nlink != 1 or
                    _file_identity(metadata) != _file_identity(after) or
                    _file_identity(metadata) != _file_identity(current)):
                raise ValueError('LEARNING_PACKAGES_LOCK_INVALID')
        current_directory = os.stat(path.parent, follow_symlinks=False)
        if (not stat.S_ISDIR(current_directory.st_mode) or
                (opened_directory.st_dev, opened_directory.st_ino) !=
                (current_directory.st_dev, current_directory.st_ino)):
            raise ValueError('LEARNING_PACKAGES_LOCK_INVALID')
    return sha256(content).hexdigest()


def verify_learning_qualification(raw: bytes, signature: bytes,
    trusted_ed25519_public_key: bytes, *, trusted_issuer_id: str,
    model_lock_sha: str,
    icc_lock_sha: str, package_root: Path, packages_lock: Path,
    evidence_root: Path,
    device: tuple[str, int, int, int], now: datetime | None = None,
) -> LearningQualification:
    """Verify an externally issued report against current installed bytes.

    The issuer key must come from trusted deployment configuration, never the
    report. A passed report is still not a runtime installation by itself.
    """
    if (not isinstance(raw, bytes) or not 100 <= len(raw) <= 262144 or
            not isinstance(signature, bytes) or len(signature) != 64 or
            not isinstance(trusted_ed25519_public_key, bytes) or
            len(trusted_ed25519_public_key) != 32):
        raise ValueError('LEARNING_QUALIFICATION_INVALID')
    try:
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
        Ed25519PublicKey.from_public_bytes(trusted_ed25519_public_key).verify(signature, raw)
        report = parse_json(raw, 262144)
        if (not isinstance(report, dict) or set(report) != _FIELDS or
                canonical_bytes(report) != raw or
                type(report['schema_version']) is not int or report['schema_version'] != 1 or
                report['operation'] != 'learning.evaluate' or
                report['operation_version'] != '1' or
                report['renderer_version'] != 'v2.2' or
                report['backend'] != 'cuda' or report['status'] != 'passed' or
                report['model_lock_sha'] != model_lock_sha or
                report['icc_lock_sha'] != icc_lock_sha or
                any(not isinstance(report[key], str) or _SHA.fullmatch(report[key]) is None
                    for key in ('model_lock_sha', 'icc_lock_sha', 'source_tree_sha',
                                'packages_lock_sha', 'dataset_sha'))):
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        if (not isinstance(trusted_issuer_id, str) or
                not 1 <= len(trusted_issuer_id) <= 128 or
                report['issuer_id'] != trusted_issuer_id or
                not isinstance(report['effects'], list) or
                not report['effects'] or len(set(report['effects'])) != len(report['effects']) or
                not set(report['effects']) <= _EFFECTS or
                any(not isinstance(value, str) for value in report['effects']) or
                not isinstance(report['checks'], list) or
                not 1 <= len(report['checks']) <= 100 or
                any(not isinstance(check, dict) or
                    set(check) != {'id', 'passed', 'evidence_sha'} or
                    not isinstance(check['id'], str) or
                    not 1 <= len(check['id']) <= 100 or
                    check['passed'] is not True or
                    not isinstance(check['evidence_sha'], str) or
                    _SHA.fullmatch(check['evidence_sha']) is None
                    for check in report['checks'])):
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        check_ids = [check['id'] for check in report['checks']]
        if (len(set(check_ids)) != len(check_ids) or
                not _CHECKS <= set(check_ids) or
                any(_EFFECT_CHECKS[effect] not in check_ids
                    for effect in report['effects'] if effect in _EFFECT_CHECKS)):
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        real_image_sha = next(check['evidence_sha'] for check in
            report['checks'] if check['id'] == 'real_images')
        real_image_raw = verify_learning_evidence(evidence_root,
            {check['evidence_sha'] for check in report['checks']},
            capture=real_image_sha)
        metrics = report['metrics']
        if (not isinstance(metrics, dict) or set(metrics) != _METRICS or
                any(type(value) not in (int, float) or not isfinite(value)
                    for value in metrics.values()) or
                metrics['baseline_loss'] <= 0 or
                not 0 <= metrics['candidate_loss_ratio'] <= .9 or
                metrics['color_p95_delta'] > 0 or
                metrics['clipping_delta_fraction'] > 0 or
                metrics['protected_max_error'] != 0 or
                type(metrics['identity_failures']) is not int or
                metrics['identity_failures'] != 0 or
                type(metrics['holdout_sessions']) is not int or
                not 2 <= metrics['holdout_sessions'] <= 5000 or
                type(metrics['holdout_examples']) is not int or
                not 10 <= metrics['holdout_examples'] <= 5000):
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        measured = parse_json(real_image_raw, 8 * 1024 * 1024)
        if (not isinstance(measured, dict) or
                set(measured) != {'schema_version', 'samples', 'metrics'} or
                type(measured['schema_version']) is not int or
                measured['schema_version'] != 1 or
                canonical_bytes(measured) != real_image_raw or
                measured['metrics'] != metrics or
                not isinstance(measured['samples'], list) or
                len(measured['samples']) != metrics['holdout_examples']):
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        samples = measured['samples']
        if any(not isinstance(item, dict) or
               set(item) != {'sha256', 'session_id'} or
               not isinstance(item['sha256'], str) or
               _SHA.fullmatch(item['sha256']) is None or
               not isinstance(item['session_id'], str)
               for item in samples):
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        sessions = set()
        for item in samples:
            session = UUID(item['session_id'])
            if session.int == 0 or str(session) != item['session_id']:
                raise ValueError('LEARNING_QUALIFICATION_INVALID')
            sessions.add(item['session_id'])
        if (len(sessions) != metrics['holdout_sessions'] or
                len({item['sha256'] for item in samples}) != len(samples) or
                sha256(canonical_bytes(samples)).hexdigest() !=
                report['dataset_sha']):
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        peaks = report['effect_peaks']
        required_peaks = set(report['effects']) & set(_EFFECT_MODELS)
        if not isinstance(peaks, dict) or set(peaks) != required_peaks:
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        peak_keys = {'ram_bytes', 'scratch_bytes', 'vram_bytes', 'output_bytes'}
        for values in peaks.values():
            if (not isinstance(values, dict) or set(values) != peak_keys or
                    any(type(value) is not int or value < 0 or value > 2**53-1
                        for value in values.values()) or
                    values['vram_bytes'] < 1 or
                    values['vram_bytes'] > report['peak_vram_bytes'] or
                    values['ram_bytes'] > 1073741824 or
                    values['scratch_bytes'] > 1073741824 or
                    values['output_bytes'] != 0):
                raise ValueError('LEARNING_QUALIFICATION_INVALID')
        expected_device = dict(name=device[0], major=device[1], minor=device[2],
                               total_memory=device[3])
        if (report['device'] != expected_device or not isinstance(device[0], str) or
                not device[0] or any(type(value) is not int or value < 0
                    for value in device[1:]) or device[3] < 1 or
                type(report['peak_vram_bytes']) is not int or
                not 0 < report['peak_vram_bytes'] <= device[3]):
            raise ValueError('LEARNING_QUALIFICATION_INVALID')
        expiry = datetime.fromisoformat(report['expires_at'].replace('Z', '+00:00'))
        current = now or datetime.now(timezone.utc)
        if expiry.tzinfo is None or current.tzinfo is None or expiry <= current:
            raise ValueError('LEARNING_QUALIFICATION_EXPIRED')
        if (report['source_tree_sha'] != source_tree_sha(package_root) or
                packages_lock_sha(packages_lock) != report['packages_lock_sha']):
            raise ValueError('LEARNING_QUALIFICATION_DRIFT')
        return LearningQualification(model_lock_sha, icc_lock_sha,
            report['source_tree_sha'], device, frozenset(report['effects']),
            report['peak_vram_bytes'], expiry, sha256(raw).hexdigest(),
            tuple((name, tuple(sorted(values.items())))
                  for name, values in sorted(peaks.items())))
    except Exception as error:
        raise ValueError('LEARNING_QUALIFICATION_INVALID') from error
