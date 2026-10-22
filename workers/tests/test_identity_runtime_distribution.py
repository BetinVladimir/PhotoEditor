"""Identity receipts name the installed CPU/CUDA ONNX distribution exactly."""
import pytest
from photo_worker.operations.identity import measurement as runtime
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('backend,selected,other', [
    ('cpu', 'onnxruntime', 'onnxruntime-gpu'),
    ('cuda', 'onnxruntime-gpu', 'onnxruntime')])
def test_identity_receipt_selects_exact_distribution(monkeypatch, backend, selected, other):
    def version(name):
        if name == other:
            raise runtime.metadata.PackageNotFoundError(name)
        return '1.30.0' if name == selected else '1.0'
    monkeypatch.setattr(runtime.metadata, 'version', version)
    packages = runtime.runtime_packages(backend)
    assert packages[selected] == '1.30.0' and other not in packages
    assert len(packages) == 7


@pytest.mark.parametrize('backend', ['cpu', 'cuda'])
def test_identity_receipt_rejects_shared_namespace_collision(monkeypatch, backend):
    monkeypatch.setattr(runtime.metadata, 'version', lambda name: '1.30.0')
    with pytest.raises(OperationFailure, match='DISTRIBUTION_AMBIGUOUS'):
        runtime.runtime_packages(backend)


def test_identity_receipt_refuses_missing_gpu_package(monkeypatch):
    def missing(name):
        raise runtime.metadata.PackageNotFoundError(name)
    monkeypatch.setattr(runtime.metadata, 'version', missing)
    with pytest.raises(runtime.metadata.PackageNotFoundError):
        runtime.runtime_packages('cuda')


def test_identity_receipt_refuses_metal_before_metadata(monkeypatch):
    def unexpected(name):
        raise AssertionError('metadata must not be read')
    monkeypatch.setattr(runtime.metadata, 'version', unexpected)
    with pytest.raises(OperationFailure, match='BACKEND_UNAVAILABLE'):
        runtime.runtime_packages('mps')
