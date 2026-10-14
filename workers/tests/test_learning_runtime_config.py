from types import SimpleNamespace

import pytest

from photo_worker.runtime.learning_config import (
    SignedLearningRequest, install_signed_learning_evaluation,
    learning_evaluation_from_environment,
)
from photo_worker.runtime.service import WorkerConfigurationError


def _config(**changes):
    values = dict(resource_class='gpu', backend='cuda', model_root=object(),
                  model_lock=object(), icc_root=object())
    values.update(changes)
    return SimpleNamespace(**values)


def test_learning_evaluate_requires_complete_signed_installation(tmp_path):
    assert learning_evaluation_from_environment(_config(),
        lambda name, default: default) is None
    files = {}
    for name, data in (
        ('PHOTO_LEARNING_QUALIFICATION_REPORT', b'canonical-report'),
        ('PHOTO_LEARNING_QUALIFICATION_SIGNATURE', b's' * 64),
        ('PHOTO_LEARNING_TRUSTED_PUBLIC_KEY', b'k' * 32),
        ('PHOTO_LEARNING_PACKAGES_LOCK', b'locked-packages')):
        path = tmp_path / name
        path.write_bytes(data)
        files[name] = str(path)
    files['PHOTO_LEARNING_TRUSTED_ISSUER_ID'] = 'deployment-qualification'
    files['PHOTO_LEARNING_EVIDENCE_ROOT'] = str(tmp_path)
    request = learning_evaluation_from_environment(_config(),
        lambda name, default: files.get(name, default))
    assert isinstance(request, SignedLearningRequest)
    assert request.signature == b's' * 64
    files.pop('PHOTO_LEARNING_TRUSTED_PUBLIC_KEY')
    with pytest.raises(WorkerConfigurationError, match='Incomplete'):
        learning_evaluation_from_environment(_config(),
            lambda name, default: files.get(name, default))


def test_learning_evaluate_rejects_wrong_worker_backend(tmp_path):
    report = tmp_path / 'report'
    report.write_bytes(b'signed-report')
    with pytest.raises(WorkerConfigurationError, match='Incomplete'):
        learning_evaluation_from_environment(_config(backend='mps'),
            lambda name, default: str(report))


def test_learning_evaluate_detects_trust_file_rotation(tmp_path, monkeypatch):
    import photo_worker.color.icc as icc
    import photo_worker.operations.learning.qualified_installation as qualified
    paths = {}
    for name, data in (
        ('PHOTO_LEARNING_QUALIFICATION_REPORT', b'canonical-report'),
        ('PHOTO_LEARNING_QUALIFICATION_SIGNATURE', b's' * 64),
        ('PHOTO_LEARNING_TRUSTED_PUBLIC_KEY', b'k' * 32),
        ('PHOTO_LEARNING_PACKAGES_LOCK', b'locked-packages')):
        path = tmp_path / name
        path.write_bytes(data)
        paths[name] = str(path)
    paths['PHOTO_LEARNING_TRUSTED_ISSUER_ID'] = 'deployment-qualification'
    paths['PHOTO_LEARNING_EVIDENCE_ROOT'] = str(tmp_path)
    request = learning_evaluation_from_environment(_config(),
        lambda name, default: paths.get(name, default))
    monkeypatch.setattr(icc, 'ICCRegistry', lambda root: object())
    monkeypatch.setattr(qualified, 'signed_pinned_learning_evaluation_installation',
        lambda pool, registry, **kwargs: kwargs)
    installed = install_signed_learning_evaluation(
        request, _config(), object())
    assert installed['verify_deployment_files']() is None
    request.public_key_path.write_bytes(b'x' * 32)
    with pytest.raises(WorkerConfigurationError, match='files changed'):
        installed['verify_deployment_files']()


def test_learning_evaluate_rejects_symlinked_trust_key(tmp_path):
    from photo_worker.runtime.learning_config import _read_file
    key = tmp_path / 'key'
    key.write_bytes(b'k' * 32)
    alias = tmp_path / 'alias'
    alias.symlink_to(key)
    with pytest.raises(WorkerConfigurationError, match='qualification file'):
        _read_file(str(alias), 32)


def test_learning_evaluate_rejects_symlinked_parent_and_hardlinked_trust_key(tmp_path):
    from photo_worker.runtime.learning_config import _read_file
    directory = tmp_path / 'keys'
    directory.mkdir()
    key = directory / 'key'
    key.write_bytes(b'k' * 32)
    alias = tmp_path / 'alias'
    alias.symlink_to(directory, target_is_directory=True)
    with pytest.raises(WorkerConfigurationError, match='qualification file'):
        _read_file(str(alias / 'key'), 32)
    hardlink = tmp_path / 'hardlink'
    hardlink.hardlink_to(key)
    with pytest.raises(WorkerConfigurationError, match='qualification file'):
        _read_file(str(key), 32)
