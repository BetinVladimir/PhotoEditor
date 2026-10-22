from types import SimpleNamespace
import pytest
from photo_worker.runtime.cuda_budget import install_cuda_budget, remaining_cuda_budget, CUDA_MVP_BYTES


def runtime(total=12_000_000_000,used=0,count=1):
    calls=[]
    cuda=SimpleNamespace(is_available=lambda:True,device_count=lambda:count,
        get_device_properties=lambda device:SimpleNamespace(total_memory=total),
        memory_reserved=lambda device:used,
        set_per_process_memory_fraction=lambda fraction,device:calls.append((fraction,device)))
    return SimpleNamespace(cuda=cuda),calls


def test_installs_decimal_cuda_ceiling():
    torch,calls=runtime()
    effective=install_cuda_budget(torch_module=torch)
    assert 9_999_999_990<=effective<=CUDA_MVP_BYTES
    assert len(calls)==1 and calls[0][1]==0 and calls[0][0]<1


def test_respects_smaller_operator_budget():
    torch,_=runtime()
    assert 0<install_cuda_budget(6_000_000_000,torch_module=torch)<=6_000_000_000


def test_remaining_includes_reserved_memory_and_guard():
    torch,_=runtime(used=8_000_000_000)
    assert 0<remaining_cuda_budget(torch_module=torch)<2_000_000_000


@pytest.mark.parametrize('limit',[True,0,-1,10_000_000_001,10_000_000_000.0])
def test_rejects_invalid_limit_before_runtime(limit):
    with pytest.raises(ValueError,match='CUDA_BUDGET_INVALID'):
        install_cuda_budget(limit,torch_module=object())


@pytest.mark.parametrize('total,used,count,code',[(12_000_000_000,11_000_000_000,1,'CUDA_BUDGET_ALREADY_EXCEEDED'),(12_000_000_000,0,2,'CUDA_BUDGET_UNAVAILABLE'),(12_000_000_000.0,0,1,'CUDA_BUDGET_MEASUREMENT_INVALID')])
def test_refuses_invalid_device_or_existing_overrun(total,used,count,code):
    torch,calls=runtime(total,used,count)
    with pytest.raises(ValueError,match=code):install_cuda_budget(torch_module=torch)
    assert calls==[]


def test_service_skips_cuda_allocator_on_metal_and_cpu(monkeypatch):
    from photo_worker.runtime.service import _install_cuda_budget
    monkeypatch.setattr('photo_worker.runtime.cuda_budget.install_cuda_budget',lambda limit:pytest.fail('CUDA touched on non-CUDA worker'))
    for resource,backend in [('gpu','mps'),('cpu','cpu')]:
        assert _install_cuda_budget(SimpleNamespace(resource_class=resource,backend=backend)) is None


def test_cuda_budget_precedes_provider_loading(monkeypatch):
    import photo_worker.runtime.service as service
    events=[]
    config=SimpleNamespace(resource_class='gpu',backend='cuda',capacity_policy=None)
    monkeypatch.setattr(service,'_install_cuda_budget',lambda config:events.append('budget'))
    monkeypatch.setattr(service,'_install_metal_budget',lambda config:None)
    def build(*args,**kwargs):
        events.append('providers')
        raise RuntimeError('stop before IO')
    monkeypatch.setattr(service,'build_worker',build)
    with pytest.raises(RuntimeError,match='stop before IO'):service.run_worker(config)
    assert events==['budget','providers']


def test_cuda_budget_failure_prevents_provider_loading(monkeypatch):
    import photo_worker.runtime.service as service
    config=SimpleNamespace(resource_class='gpu',backend='cuda',capacity_policy=None)
    def fail(limit):raise ValueError('CUDA_BUDGET_ALREADY_EXCEEDED')
    monkeypatch.setattr('photo_worker.runtime.cuda_budget.install_cuda_budget',fail)
    monkeypatch.setattr(service,'build_worker',lambda *a,**kw:pytest.fail('providers loaded after budget refusal'))
    with pytest.raises(service.WorkerConfigurationError,match='CUDA_BUDGET_ALREADY_EXCEEDED'):
        service.run_worker(config)
