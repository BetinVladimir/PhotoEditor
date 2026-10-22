"""Doctor installs CUDA allocator policy before creating the model pool."""
from types import SimpleNamespace
import pytest
from photo_worker.models import doctor
from photo_worker.runtime import cuda_budget


@pytest.mark.parametrize('backend', ['cpu', 'mps'])
def test_non_cuda_doctor_does_not_install_cuda_budget(monkeypatch, backend):
    def unexpected():
        raise AssertionError('CUDA must not be touched')
    monkeypatch.setattr(cuda_budget, 'install_cuda_budget', unexpected)
    assert doctor._install_doctor_budget(backend) is None


def test_cuda_doctor_uses_default_production_budget(monkeypatch):
    calls = []
    def install():
        calls.append(True)
        return cuda_budget.CUDA_MVP_BYTES - 1
    monkeypatch.setattr(cuda_budget, 'install_cuda_budget', install)
    assert doctor._install_doctor_budget('cuda') == 9_999_999_999
    assert calls == [True]


def test_cuda_budget_failure_prevents_model_pool_creation(monkeypatch, tmp_path):
    lock = tmp_path / 'lock.json'
    lock.write_text('{}')
    monkeypatch.setattr(doctor, 'ModelLock', SimpleNamespace(model_validate_json=lambda raw: object()))
    def refuse():
        raise ValueError('CUDA_BUDGET_ALREADY_EXCEEDED')
    def unexpected(*args, **kwargs):
        raise AssertionError('models must not be constructed before budget admission')
    monkeypatch.setattr(cuda_budget, 'install_cuda_budget', refuse)
    monkeypatch.setattr(doctor, 'ModelPool', unexpected)
    with pytest.raises(ValueError, match='ALREADY_EXCEEDED'):
        doctor.run(tmp_path, lock, 'cuda')
