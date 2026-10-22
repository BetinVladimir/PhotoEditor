"""Authored backend selection scenarios; no device, model load or SQL IO."""
from types import SimpleNamespace as NS
import pytest
from photo_worker.runtime.service import (
    WorkerConfigurationError, _select_removal_preview_installation,
)


def pool(status='passed', approved=True, report='a'*64, factory=True):
    entry = NS(id='big_lama', approved_for_install=approved,
               backends={backend: NS(status=status, report_sha256=report)
                         for backend in ('cpu', 'cuda', 'mps')})
    return NS(lock=NS(models=[entry]), factories={'big_lama': object()} if factory else {})


@pytest.mark.parametrize('backend', ['cpu', 'cuda', 'mps'])
def test_qualified_removal_keeps_exact_factory(backend):
    install = lambda *_: None
    assert _select_removal_preview_installation(install, pool(), backend) is install


def test_disabled_removal_needs_no_pool():
    assert _select_removal_preview_installation(None, None, 'mps') is None


def test_cuda_only_removal_is_automatically_omitted_on_metal():
    assert _select_removal_preview_installation(lambda *_: None, pool('unsupported'), 'mps') is None


@pytest.mark.parametrize('backend', ['cpu', 'cuda', 'mps'])
@pytest.mark.parametrize('defect', ['untested', 'unapproved', 'missing_report', 'missing_factory', 'missing_backend', 'missing_model', 'duplicate_model'])
def test_removal_refuses_incomplete_backend_installation(backend, defect):
    installed = pool()
    entry = installed.lock.models[0]
    if defect == 'untested': entry.backends[backend].status = 'untested'
    elif defect == 'unapproved': entry.approved_for_install = False
    elif defect == 'missing_report': entry.backends[backend].report_sha256 = None
    elif defect == 'missing_factory': installed.factories = {}
    elif defect == 'missing_backend': del entry.backends[backend]
    elif defect == 'missing_model': installed.lock.models = []
    else: installed.lock.models.append(entry)
    with pytest.raises(WorkerConfigurationError):
        _select_removal_preview_installation(lambda *_: None, installed, backend)


@pytest.mark.parametrize('backend', ['cpu', 'cuda'])
def test_unsupported_non_metal_installation_fails_explicitly(backend):
    with pytest.raises(WorkerConfigurationError):
        _select_removal_preview_installation(lambda *_: None, pool('unsupported'), backend)


def test_removal_startup_verifies_actual_weight_bytes(tmp_path):
    from hashlib import sha256
    from photo_worker.runtime.service import _verify_removal_preview_model_files
    installed = pool()
    installed.root = tmp_path
    installed.lock.lock_id = 'b'*64
    folder = tmp_path / installed.lock.lock_id / 'big_lama'
    folder.mkdir(parents=True)
    weight = folder / 'weights.bin'
    weight.write_bytes(b'pinned weights')
    installed.lock.models[0].files = [NS(path='weights.bin', bytes=14,
                                         sha256=sha256(b'pinned weights').hexdigest())]
    _verify_removal_preview_model_files(lambda *_: None, installed)
    weight.write_bytes(b'changed weight')
    with pytest.raises(WorkerConfigurationError, match='pinned model files'):
        _verify_removal_preview_model_files(lambda *_: None, installed)
    weight.unlink()
    with pytest.raises(WorkerConfigurationError, match='pinned model files'):
        _verify_removal_preview_model_files(lambda *_: None, installed)


def test_disabled_removal_does_not_read_weights():
    from photo_worker.runtime.service import _verify_removal_preview_model_files
    _verify_removal_preview_model_files(None, None)
