from datetime import datetime, timedelta, timezone
from hashlib import sha256
from types import SimpleNamespace
from uuid import UUID

import pytest

from photo_worker.operations.learning.qualification import LearningQualification
from photo_worker.operations.learning import qualified_installation as module


def _inputs(tmp_path):
    report = 'a' * 64
    proof = LearningQualification(report, 'b' * 64, 'c' * 64,
        ('RTX 3060', 8, 6, 12_000_000_000), frozenset({'color'}),
        10_000_000_000, datetime.now(timezone.utc) + timedelta(days=1), 'd' * 64)
    models = [SimpleNamespace(id=model, approved_for_install=True,
        backends={'cuda': SimpleNamespace(status='passed', report_sha256='e' * 64)})
        for model in module._BASE_MODELS]
    pool = SimpleNamespace(root=tmp_path, lock=SimpleNamespace(lock_id=report,
                                                                 models=models))
    registry = SimpleNamespace(resolve=lambda _: SimpleNamespace(sha256='b' * 64))
    args = dict(report_bytes=b'report', signature=b'signature',
        trusted_issuer_id='deployment-qualification',
        trusted_public_key=b'public-key', package_root=tmp_path,
        packages_lock=tmp_path / 'packages.lock', evidence_root=tmp_path)
    return pool, registry, args, proof


def test_signed_installation_rechecks_model_and_report_before_task(tmp_path, monkeypatch):
    pool, registry, args, proof = _inputs(tmp_path)
    calls = []
    monkeypatch.setattr(module, 'installed_cuda_device', lambda: proof.device)
    monkeypatch.setattr(module, 'verify_learning_qualification',
        lambda *a, **kw: calls.append(('report', kw['device'])) or proof)
    monkeypatch.setattr(module, 'verify_model',
        lambda path, entry: calls.append(('model', entry.id)))
    monkeypatch.setattr(module, 'pinned_learning_evaluation_installation',
        lambda pool, registry, **kw: kw)
    installed = module.signed_pinned_learning_evaluation_installation(
        pool, registry, **args)
    assert installed['qualification'] == proof
    assert len([call for call in calls if call[0] == 'model']) == 4
    installed['verify_runtime']()
    assert len([call for call in calls if call[0] == 'model']) == 8
    pool.lock.models[0].approved_for_install = False
    with pytest.raises(ValueError, match='LEARNING_MODEL_UNQUALIFIED'):
        installed['verify_runtime']()


def test_signed_installation_refuses_missing_effect_model(tmp_path, monkeypatch):
    pool, registry, args, proof = _inputs(tmp_path)
    proof = LearningQualification(proof.model_lock_sha, proof.icc_lock_sha,
        proof.source_tree_sha, proof.device,
        frozenset({'color', 'background.blur'}), proof.peak_vram_bytes,
        proof.expires_at, proof.report_sha)
    monkeypatch.setattr(module, 'installed_cuda_device', lambda: proof.device)
    monkeypatch.setattr(module, 'verify_learning_qualification', lambda *a, **kw: proof)
    with pytest.raises(ValueError, match='LEARNING_MODEL_QUALIFICATION_MISSING'):
        module.signed_pinned_learning_evaluation_installation(
            pool, registry, **args)


def test_signed_installation_rechecks_deployment_files(tmp_path, monkeypatch):
    pool, registry, args, proof = _inputs(tmp_path)
    monkeypatch.setattr(module, 'installed_cuda_device', lambda: proof.device)
    monkeypatch.setattr(module, 'verify_learning_qualification', lambda *a, **kw: proof)
    monkeypatch.setattr(module, 'verify_model', lambda *a: None)
    monkeypatch.setattr(module, 'pinned_learning_evaluation_installation',
        lambda pool, registry, **kw: kw)
    state = {'current': True}
    def verify_files():
        if not state['current']:
            raise ValueError('qualification rotated')
    installed = module.signed_pinned_learning_evaluation_installation(
        pool, registry, verify_deployment_files=verify_files, **args)
    state['current'] = False
    with pytest.raises(ValueError, match='qualification rotated'):
        installed['verify_runtime']()


def test_signed_asset_decoder_reuses_report_verifier(tmp_path, monkeypatch):
    pool, registry, args, proof = _inputs(tmp_path)
    proof = LearningQualification(proof.model_lock_sha, proof.icc_lock_sha,
        proof.source_tree_sha, proof.device,
        frozenset({'color', 'background.asset'}), proof.peak_vram_bytes,
        proof.expires_at, proof.report_sha)
    calls = []
    monkeypatch.setattr(module, 'installed_cuda_device', lambda: proof.device)
    monkeypatch.setattr(module, 'verify_learning_qualification',
        lambda *a, **kw: calls.append('report') or proof)
    monkeypatch.setattr(module, 'verify_model', lambda *a: None)
    monkeypatch.setattr(module, 'pinned_learning_evaluation_installation',
        lambda pool, registry, **kw: kw)
    installed = module.signed_pinned_learning_evaluation_installation(
        pool, registry, **args)
    decoder_check = installed['background_verify_runtime']
    assert callable(decoder_check)
    decoder_check()
    assert len(calls) == 2


def test_signed_peaks_install_denoise_and_depth_on_shared_pool(tmp_path, monkeypatch):
    from photo_worker.v2.render_native_base import NativeDenoiseProvider
    from photo_worker.operations.learning.depth import HoldoutDepthProvider
    pool, registry, args, original = _inputs(tmp_path)
    peak = dict(ram_bytes=1024, scratch_bytes=1024,
                vram_bytes=1024, output_bytes=0)
    effects = frozenset({'color', 'restoration.denoise', 'background.blur'})
    proof = LearningQualification(original.model_lock_sha, original.icc_lock_sha,
        original.source_tree_sha, original.device, effects,
        original.peak_vram_bytes, original.expires_at, original.report_sha,
        tuple((name, tuple(sorted(peak.items()))) for name in
              sorted(('restoration.denoise', 'background.blur'))))
    pool.lock.models.extend(SimpleNamespace(id=model, approved_for_install=True,
        backends={'cuda': SimpleNamespace(status='passed', report_sha256='e' * 64)})
        for model in ('scunet_color_real_psnr', 'depth_anything_v2_small'))
    monkeypatch.setattr(module, 'installed_cuda_device', lambda: proof.device)
    monkeypatch.setattr(module, 'verify_learning_qualification', lambda *a, **kw: proof)
    monkeypatch.setattr(module, 'verify_model', lambda *a: None)
    monkeypatch.setattr(module, 'pinned_learning_evaluation_installation',
        lambda pool, registry, **kw: kw)
    installed = module.signed_pinned_learning_evaluation_installation(
        pool, registry, **args)
    assert isinstance(installed['denoise_provider'], NativeDenoiseProvider)
    assert isinstance(installed['depth_provider'], HoldoutDepthProvider)
    assert installed['denoise_provider'].pool is pool
    assert installed['depth_provider'].pool is pool
    installed['verify_runtime']()


def test_signed_factory_rejects_physical_model_drift(tmp_path, monkeypatch):
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.learning.qualification import source_tree_sha

    key = Ed25519PrivateKey.generate()
    public = key.public_key().public_bytes(serialization.Encoding.Raw,
                                           serialization.PublicFormat.Raw)
    package = tmp_path / 'photo_worker'
    package.mkdir()
    (package / 'renderer.py').write_text('qualified renderer\n')
    dependencies = tmp_path / 'packages.lock'
    dependencies.write_text('locked\n')
    evidence = tmp_path / 'evidence'
    evidence.mkdir()
    metrics = dict(baseline_loss=1.0, candidate_loss_ratio=.8,
        color_p95_delta=-.1, clipping_delta_fraction=0.0,
        protected_max_error=0.0, identity_failures=0,
        holdout_sessions=2, holdout_examples=10)
    samples = [dict(sha256=f'{index:064x}',
        session_id=str(UUID(int=1 if index <= 5 else 2)))
        for index in range(1, 11)]
    checks = []
    for name in ('detector', 'identity', 'rendered_color', 'clipping',
                 'protected_pixels', 'consent', 'resources', 'real_images'):
        content = (canonical_bytes(dict(schema_version=1, samples=samples,
                                        metrics=metrics)) if name == 'real_images'
                   else canonical_bytes({'check': name, 'result': 'passed'}))
        digest = sha256(content).hexdigest()
        (evidence / (digest + '.json')).write_bytes(content)
        checks.append(dict(id=name, passed=True, evidence_sha=digest))
    lock_sha, icc_sha = 'a' * 64, 'b' * 64
    device = ('RTX 3060', 8, 6, 12_000_000_000)
    report = dict(schema_version=1, operation='learning.evaluate',
        operation_version='1', renderer_version='v2.2', backend='cuda',
        model_lock_sha=lock_sha, icc_lock_sha=icc_sha,
        source_tree_sha=source_tree_sha(package),
        packages_lock_sha=sha256(dependencies.read_bytes()).hexdigest(),
        device=dict(name=device[0], major=device[1], minor=device[2],
                    total_memory=device[3]), effects=['color'], checks=checks,
        effect_peaks={}, metrics=metrics,
        dataset_sha=sha256(canonical_bytes(samples)).hexdigest(),
        issuer_id='qualifier',
        expires_at=(datetime.now(timezone.utc) + timedelta(days=1))
            .isoformat().replace('+00:00', 'Z'), status='passed',
        peak_vram_bytes=10_000_000_000)
    raw = canonical_bytes(report)
    model_root = tmp_path / 'models'
    entries = []
    for model_id in sorted(module._BASE_MODELS):
        root = model_root / lock_sha / model_id
        root.mkdir(parents=True)
        weight = root / 'weights.bin'
        weight.write_bytes(model_id.encode())
        entry_file = SimpleNamespace(path='weights.bin',
            sha256=sha256(weight.read_bytes()).hexdigest(),
            bytes=weight.stat().st_size)
        entries.append(SimpleNamespace(id=model_id, approved_for_install=True,
            backends={'cuda': SimpleNamespace(status='passed',
                                              report_sha256='d' * 64)},
            files=[entry_file]))
    pool = SimpleNamespace(root=model_root,
        lock=SimpleNamespace(lock_id=lock_sha, models=entries))
    registry = SimpleNamespace(resolve=lambda _: SimpleNamespace(sha256=icc_sha))
    monkeypatch.setattr(module, 'installed_cuda_device', lambda: device)
    monkeypatch.setattr(module, 'pinned_learning_evaluation_installation',
        lambda pool, registry, **kw: kw)
    installed = module.signed_pinned_learning_evaluation_installation(
        pool, registry, report_bytes=raw, signature=key.sign(raw),
        trusted_public_key=public, trusted_issuer_id='qualifier',
        package_root=package, packages_lock=dependencies, evidence_root=evidence)
    installed['verify_runtime']()
    (model_root / lock_sha / entries[0].id / 'weights.bin').write_bytes(b'tampered')
    with pytest.raises(ValueError, match='MODEL_'):
        installed['verify_runtime']()
