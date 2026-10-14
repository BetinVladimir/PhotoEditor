from datetime import datetime, timedelta, timezone
from hashlib import sha256
from uuid import UUID

import pytest

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.learning.qualification import (
    packages_lock_sha, source_tree_sha, verify_learning_evidence,
    verify_learning_qualification,
    required_learning_effects,
)


def _proof(tmp_path):
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

    package = tmp_path / 'photo_worker'
    package.mkdir()
    (package / 'renderer.py').write_text('VERSION = 1\n')
    lock = tmp_path / 'packages.lock'
    lock.write_text('locked dependencies\n')
    key = Ed25519PrivateKey.generate()
    public = key.public_key().public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw)
    device = ('RTX 3060', 8, 6, 12_000_000_000)
    digest = 'a' * 64
    evidence_root = tmp_path / 'evidence'
    evidence_root.mkdir()
    metrics = dict(baseline_loss=1.0, candidate_loss_ratio=.8,
        color_p95_delta=-.1, clipping_delta_fraction=0.0,
        protected_max_error=0.0, identity_failures=0,
        holdout_sessions=2, holdout_examples=10)
    samples = [dict(sha256=f'{index:064x}',
        session_id=str(UUID(int=1 if index <= 5 else 2)))
        for index in range(1, 11)]
    checks = []
    for name in ('detector', 'identity', 'rendered_color', 'clipping',
                 'protected_pixels', 'consent', 'resources', 'real_images',
                 'reviewed_defects'):
        content = (canonical_bytes(dict(schema_version=1, samples=samples,
                                        metrics=metrics)) if name == 'real_images'
                   else canonical_bytes({'check': name, 'result': 'passed'}))
        evidence_sha = sha256(content).hexdigest()
        (evidence_root / (evidence_sha + '.json')).write_bytes(content)
        checks.append(dict(id=name, passed=True, evidence_sha=evidence_sha))
    report = dict(schema_version=1, operation='learning.evaluate',
        operation_version='1', renderer_version='v2.2', backend='cuda',
        model_lock_sha=digest, icc_lock_sha='b' * 64,
        source_tree_sha=source_tree_sha(package),
        packages_lock_sha=sha256(lock.read_bytes()).hexdigest(),
        device=dict(name=device[0], major=device[1], minor=device[2],
                    total_memory=device[3]), effects=['color', 'defects.repair'],
        checks=checks,
        effect_peaks={},
        metrics=metrics,
        dataset_sha=sha256(canonical_bytes(samples)).hexdigest(),
        issuer_id='deployment-qualification',
        expires_at=(datetime.now(timezone.utc) + timedelta(days=1))
            .isoformat().replace('+00:00', 'Z'), status='passed',
        peak_vram_bytes=10_000_000_000)
    raw = canonical_bytes(report)
    args = dict(trusted_issuer_id='deployment-qualification',
        model_lock_sha=digest, icc_lock_sha='b' * 64,
        package_root=package, packages_lock=lock,
        evidence_root=evidence_root, device=device)
    return raw, key.sign(raw), public, args, key, package, lock


def test_signed_learning_qualification_binds_installed_runtime(tmp_path):
    raw, signature, public, args, _, package, lock = _proof(tmp_path)
    proof = verify_learning_qualification(raw, signature, public, **args)
    assert proof.effects == frozenset({'color', 'defects.repair'})
    assert proof.peak_vram_bytes == 10_000_000_000
    (package / 'renderer.py').write_text('VERSION = 2\n')
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public, **args)
    (package / 'renderer.py').write_text('VERSION = 1\n')
    lock.write_text('changed dependencies\n')
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public, **args)


def test_reviewed_dust_needs_separately_signed_quality_evidence(tmp_path):
    from types import SimpleNamespace
    raw, _, public, args, key, _, _ = _proof(tmp_path)
    report = parse_json(raw)
    report['effects'].append('restoration.sensor_dust')
    unsigned_check = canonical_bytes(report)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(unsigned_check, key.sign(unsigned_check),
                                      public, **args)
    content = canonical_bytes({'check': 'reviewed_sensor_dust',
                               'result': 'passed'})
    digest = sha256(content).hexdigest()
    (args['evidence_root'] / (digest + '.json')).write_bytes(content)
    report['checks'].append(dict(id='reviewed_sensor_dust', passed=True,
                                 evidence_sha=digest))
    signed = canonical_bytes(report)
    proof = verify_learning_qualification(signed, key.sign(signed), public,
                                          **args)
    assert 'restoration.sensor_dust' in proof.effects

    def arm(mode):
        return SimpleNamespace(local=SimpleNamespace(enabled=False),
            beauty=SimpleNamespace(enabled=False),
            defects=SimpleNamespace(enabled=False),
            extensions=SimpleNamespace(region_adjustments=[], background=None,
                restoration=SimpleNamespace(denoise='off', upscale='off',
                                            sensor_dust=mode)))
    styles = SimpleNamespace(baseline=arm('review'), candidate=arm('review'),
                             approved=arm('review'))
    assert 'restoration.sensor_dust' in required_learning_effects(styles)
    styles.candidate.extensions.restoration.sensor_dust = 'conservative_auto'
    with pytest.raises(ValueError, match='LEARNING_EFFECT_UNQUALIFIED'):
        required_learning_effects(styles)


def test_learning_source_tree_refuses_symlinked_and_oversized_code(tmp_path):
    package = tmp_path / 'photo_worker'
    package.mkdir()
    external = tmp_path / 'external.py'
    external.write_text('VERSION = 1\n')
    (package / 'renderer.py').symlink_to(external)
    with pytest.raises(ValueError, match='LEARNING_SOURCE_TREE_INVALID'):
        source_tree_sha(package)
    (package / 'renderer.py').unlink()
    external_dir = tmp_path / 'external'
    external_dir.mkdir()
    (external_dir / 'renderer.py').write_text('VERSION = 1\n')
    (package / 'nested').symlink_to(external_dir, target_is_directory=True)
    (package / 'valid.py').write_text('VERSION = 1\n')
    with pytest.raises(ValueError, match='LEARNING_SOURCE_TREE_INVALID'):
        source_tree_sha(package)
    (package / 'nested').unlink()
    (package / 'valid.py').write_bytes(b'x' * (8 * 1024 * 1024 + 1))
    with pytest.raises(ValueError, match='LEARNING_SOURCE_TREE_INVALID'):
        source_tree_sha(package)


def test_learning_source_tree_refuses_hardlink_and_symlinked_parent(tmp_path):
    package = tmp_path / 'photo_worker'
    package.mkdir()
    source = package / 'renderer.py'
    source.write_text('VERSION = 1\n')
    (tmp_path / 'other.py').hardlink_to(source)
    with pytest.raises(ValueError, match='LEARNING_SOURCE_TREE_INVALID'):
        source_tree_sha(package)
    (tmp_path / 'other.py').unlink()
    parent = tmp_path / 'alias'
    parent.symlink_to(tmp_path, target_is_directory=True)
    with pytest.raises((ValueError, OSError)):
        source_tree_sha(parent / 'photo_worker')


def test_learning_source_tree_enumerates_pinned_directories_in_path_order(tmp_path, monkeypatch):
    from pathlib import Path
    package = tmp_path / 'photo_worker'
    (package / 'a').mkdir(parents=True)
    files = {'a.py': b'A\n', 'a/z.py': b'Z\n', 'b.py': b'B\n'}
    for name, body in files.items():
        (package / name).write_bytes(body)
    digest = sha256()
    for name in sorted(files):
        encoded = name.encode('utf-8')
        digest.update(len(encoded).to_bytes(4, 'big'))
        digest.update(encoded)
        digest.update(len(files[name]).to_bytes(8, 'big'))
        digest.update(files[name])
    monkeypatch.setattr(Path, 'rglob', lambda *_args, **_kwargs:
                        pytest.fail('path-based source enumeration'))
    assert source_tree_sha(package) == digest.hexdigest()


def test_learning_source_tree_bounds_directory_enumeration_before_sort(tmp_path, monkeypatch):
    from contextlib import nullcontext
    from types import SimpleNamespace
    from photo_worker.operations.learning import qualification
    package = tmp_path / 'photo_worker'
    package.mkdir()
    (package / 'renderer.py').write_text('VERSION = 1\n')
    monkeypatch.setattr(qualification.os, 'scandir', lambda _directory:
        nullcontext((SimpleNamespace(name='candidate.py') for _ in range(100001))))
    with pytest.raises(ValueError, match='LEARNING_SOURCE_TREE_INVALID'):
        source_tree_sha(package)


def test_learning_source_tree_refuses_entry_replaced_during_read(tmp_path, monkeypatch):
    from photo_worker.operations.learning import qualification
    package = tmp_path / 'photo_worker'
    package.mkdir()
    source = package / 'renderer.py'
    source.write_text('VERSION = 1\n')
    original = qualification.os.open
    changed = False

    def replace_after_open(path, flags, *args, **kwargs):
        nonlocal changed
        descriptor = original(path, flags, *args, **kwargs)
        if path == 'renderer.py' and not changed:
            changed = True
            source.unlink()
            source.write_text('VERSION = 2\n')
        return descriptor

    monkeypatch.setattr(qualification.os, 'open', replace_after_open)
    with pytest.raises(ValueError, match='LEARNING_SOURCE_TREE_INVALID'):
        source_tree_sha(package)


def test_learning_qualification_refuses_replaced_package_lock(tmp_path):
    raw, signature, public, args, _, _, lock = _proof(tmp_path)
    original = lock.read_bytes()
    lock.unlink()
    external = tmp_path / 'replacement.lock'
    external.write_bytes(original)
    lock.symlink_to(external)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public, **args)
    lock.unlink()
    lock.write_bytes(b'x' * (8 * 1024 * 1024 + 1))
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public, **args)


def test_learning_qualification_refuses_tamper_and_expiry(tmp_path):
    raw, signature, public, args, key, _, _ = _proof(tmp_path)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw + b' ', signature, public, **args)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, key.sign(b'other report'), public, **args)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public,
            now=datetime.now(timezone.utc) + timedelta(days=2), **args)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public,
            **{**args, 'trusted_issuer_id': 'another-issuer'})


def test_learning_qualification_refuses_missing_or_changed_evidence(tmp_path):
    from photo_worker.contracts.base import parse_json
    raw, signature, public, args, _, _, _ = _proof(tmp_path)
    report = parse_json(raw, 262144)
    path = args['evidence_root'] / (report['checks'][0]['evidence_sha'] + '.json')
    path.write_bytes(b'changed')
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public, **args)
    path.unlink()
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public, **args)
    other = tmp_path / 'other-evidence'
    other.write_bytes(b'changed')
    path.symlink_to(other)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(raw, signature, public, **args)


def test_learning_evidence_refuses_replaced_root(tmp_path, monkeypatch):
    import os
    from photo_worker.operations.learning import qualification

    root = tmp_path / 'evidence'
    root.mkdir()
    data = b'{"passed":true}'
    digest = sha256(data).hexdigest()
    (root / (digest + '.json')).write_bytes(data)
    replacement = tmp_path / 'replacement'
    replacement.mkdir()
    (replacement / (digest + '.json')).write_bytes(data)
    real_open = os.open
    def replace_before_open(path, flags, *args, **kwargs):
        if path == root.name and kwargs.get('dir_fd') is not None:
            root.rename(tmp_path / 'old-evidence')
            root.symlink_to(replacement, target_is_directory=True)
        return real_open(path, flags, *args, **kwargs)
    monkeypatch.setattr(qualification.os, 'open', replace_before_open)
    with pytest.raises(OSError):
        verify_learning_evidence(root, {digest})


def test_learning_evidence_refuses_root_replaced_during_read(tmp_path, monkeypatch):
    import os
    from photo_worker.operations.learning import qualification

    root = tmp_path / 'evidence'
    root.mkdir()
    data = b'{"passed":true}'
    digest = sha256(data).hexdigest()
    name = digest + '.json'
    (root / name).write_bytes(data)
    replacement = tmp_path / 'replacement'
    replacement.mkdir()
    (replacement / name).write_bytes(data)
    real_open = os.open
    def replace_after_root_open(path, flags, *args, **kwargs):
        if path == name:
            root.rename(tmp_path / 'old-evidence')
            root.symlink_to(replacement, target_is_directory=True)
        return real_open(path, flags, *args, **kwargs)
    monkeypatch.setattr(qualification.os, 'open', replace_after_root_open)
    with pytest.raises(ValueError, match='LEARNING_EVIDENCE_ROOT_CHANGED'):
        verify_learning_evidence(root, {digest})


def test_learning_evidence_refuses_hardlink_and_replaced_entry(tmp_path, monkeypatch):
    from photo_worker.operations.learning import qualification
    root = tmp_path / 'evidence'
    root.mkdir()
    data = b'{"passed":true}'
    digest = sha256(data).hexdigest()
    source = root / (digest + '.json')
    source.write_bytes(data)
    alias = tmp_path / 'alias.json'
    alias.hardlink_to(source)
    with pytest.raises(ValueError, match='LEARNING_EVIDENCE_UNAVAILABLE'):
        verify_learning_evidence(root, {digest})
    alias.unlink()
    original = qualification.os.open
    changed = False

    def replace_after_open(path, flags, *args, **kwargs):
        nonlocal changed
        descriptor = original(path, flags, *args, **kwargs)
        if path == source.name and not changed:
            changed = True
            source.unlink()
            source.write_bytes(data)
        return descriptor

    monkeypatch.setattr(qualification.os, 'open', replace_after_open)
    with pytest.raises(ValueError, match='LEARNING_EVIDENCE_CHANGED'):
        verify_learning_evidence(root, {digest})


def test_learning_packages_lock_refuses_symlinked_parent_and_replacement(tmp_path, monkeypatch):
    from photo_worker.operations.learning import qualification
    directory = tmp_path / 'dependencies'
    directory.mkdir()
    lock = directory / 'packages.lock'
    lock.write_bytes(b'locked dependencies\n')
    alias = tmp_path / 'alias'
    alias.symlink_to(directory, target_is_directory=True)
    with pytest.raises((ValueError, OSError)):
        packages_lock_sha(alias / lock.name)
    original = qualification.os.open
    changed = False

    def replace_after_open(path, flags, *args, **kwargs):
        nonlocal changed
        descriptor = original(path, flags, *args, **kwargs)
        if path == lock.name and not changed:
            changed = True
            lock.unlink()
            lock.write_bytes(b'locked dependencies\n')
        return descriptor

    monkeypatch.setattr(qualification.os, 'open', replace_after_open)
    with pytest.raises(ValueError, match='LEARNING_PACKAGES_LOCK_INVALID'):
        packages_lock_sha(lock)


def test_learning_packages_lock_refuses_parent_replaced_during_read(tmp_path, monkeypatch):
    from photo_worker.operations.learning import qualification
    directory = tmp_path / 'dependencies'
    directory.mkdir()
    lock = directory / 'packages.lock'
    lock.write_bytes(b'locked dependencies\n')
    replacement = tmp_path / 'replacement'
    replacement.mkdir()
    (replacement / lock.name).write_bytes(lock.read_bytes())
    original = qualification.os.open
    changed = False

    def replace_parent_after_open(path, flags, *args, **kwargs):
        nonlocal changed
        descriptor = original(path, flags, *args, **kwargs)
        if path == lock.name and not changed:
            changed = True
            directory.rename(tmp_path / 'old-dependencies')
            directory.symlink_to(replacement, target_is_directory=True)
        return descriptor

    monkeypatch.setattr(qualification.os, 'open', replace_parent_after_open)
    with pytest.raises(ValueError, match='LEARNING_PACKAGES_LOCK_INVALID'):
        packages_lock_sha(lock)


@pytest.mark.parametrize('metric,value', [
    ('candidate_loss_ratio', .91), ('color_p95_delta', .01),
    ('clipping_delta_fraction', .01), ('protected_max_error', .0001),
    ('identity_failures', 1), ('holdout_sessions', 1),
    ('holdout_examples', 9),
])
def test_signed_but_regressing_qualification_is_refused(tmp_path, metric, value):
    from photo_worker.contracts.base import parse_json
    raw, _, public, args, key, _, _ = _proof(tmp_path)
    report = parse_json(raw, 262144)
    report['metrics'][metric] = value
    changed = canonical_bytes(report)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(changed, key.sign(changed), public, **args)


def test_signed_denoise_peak_is_required_and_pinned(tmp_path):
    from photo_worker.contracts.base import parse_json
    raw, _, public, args, key, _, _ = _proof(tmp_path)
    report = parse_json(raw, 262144)
    report['effects'].append('restoration.denoise')
    content = canonical_bytes({'check': 'denoise_quality', 'result': 'passed'})
    digest = sha256(content).hexdigest()
    (args['evidence_root'] / (digest + '.json')).write_bytes(content)
    report['checks'].append(dict(id='denoise_quality', passed=True,
                                 evidence_sha=digest))
    unsigned_peak = canonical_bytes(report)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(unsigned_peak, key.sign(unsigned_peak),
                                      public, **args)
    peak = dict(ram_bytes=100, scratch_bytes=100, vram_bytes=100,
                output_bytes=0)
    report['effect_peaks'] = {'restoration.denoise': peak}
    qualified = canonical_bytes(report)
    proof = verify_learning_qualification(qualified, key.sign(qualified),
                                           public, **args)
    assert proof.peak_for('restoration.denoise') == peak
    report['effect_peaks']['restoration.denoise']['scratch_bytes'] = 1073741825
    over_budget = canonical_bytes(report)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(over_budget, key.sign(over_budget),
                                      public, **args)


def test_signed_real_image_evidence_must_match_holdout_inventory(tmp_path):
    from photo_worker.contracts.base import parse_json
    raw, _, public, args, key, _, _ = _proof(tmp_path)
    report = parse_json(raw, 262144)
    check = next(item for item in report['checks'] if item['id'] == 'real_images')
    document = parse_json((args['evidence_root'] /
        (check['evidence_sha'] + '.json')).read_bytes(), 8 * 1024 * 1024)
    document['samples'].pop()
    changed_evidence = canonical_bytes(document)
    changed_sha = sha256(changed_evidence).hexdigest()
    (args['evidence_root'] / (changed_sha + '.json')).write_bytes(changed_evidence)
    check['evidence_sha'] = changed_sha
    changed_report = canonical_bytes(report)
    with pytest.raises(ValueError, match='LEARNING_QUALIFICATION_INVALID'):
        verify_learning_qualification(changed_report, key.sign(changed_report),
                                      public, **args)
