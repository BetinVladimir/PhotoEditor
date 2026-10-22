"""Region adopter startup policy scenarios; authored, not hardware qualification."""
from types import SimpleNamespace
import pytest
from photo_worker.v2.region_adopter_config import RegionAdopterBudget


def fake_torch(backend,total):
    calls=[]
    runtime=SimpleNamespace(
        cuda=SimpleNamespace(is_available=lambda:backend=='cuda',device_count=lambda:1,
            get_device_properties=lambda _:SimpleNamespace(total_memory=total),
            memory_reserved=lambda _:0,set_per_process_memory_fraction=lambda fraction,device:calls.append((fraction,device))),
        backends=SimpleNamespace(mps=SimpleNamespace(is_available=lambda:backend=='mps')),
        mps=SimpleNamespace(recommended_max_memory=lambda:total,driver_allocated_memory=lambda:0,
            set_per_process_memory_fraction=lambda fraction:calls.append((fraction,None))))
    return runtime,calls


def test_cuda_adopter_installs_ten_decimal_gigabytes():
    runtime,calls=fake_torch('cuda',12_000_000_000)
    budget=RegionAdopterBudget.install('cuda',torch_module=runtime)
    assert budget.limit_bytes==10_000_000_000
    assert len(calls)==1 and 0<calls[0][0]<10_000_000_000/12_000_000_000
    assert 0<budget.remaining(torch_module=runtime)<10_000_000_000


def test_metal_adopter_uses_actual_device_recommendation():
    runtime,calls=fake_torch('mps',24_000_000_000)
    budget=RegionAdopterBudget.install('mps',torch_module=runtime)
    assert 10_000_000_000<budget.limit_bytes<=24_000_000_000
    assert len(calls)==1 and 0<calls[0][0]<1
    assert budget.remaining(torch_module=runtime)>10_000_000_000


def test_missing_device_refuses_before_factories():
    runtime,_=fake_torch('mps',24_000_000_000)
    with pytest.raises(ValueError,match='CUDA_BUDGET_UNAVAILABLE'):
        RegionAdopterBudget.install('cuda',torch_module=runtime)


def test_startup_budget_failure_precedes_model_lock_io_and_factory_construction(monkeypatch,tmp_path):
    from photo_worker.v2.region_adopter_config import RegionAdopterConfig,configured_region_adopter
    config=RegionAdopterConfig(object(),tmp_path,tmp_path/'absent-lock.json',tmp_path,'cuda')
    def unavailable(cls,backend,**kwargs):
        assert backend=='cuda'
        raise ValueError('CUDA_BUDGET_ALREADY_EXCEEDED')
    monkeypatch.setattr(RegionAdopterBudget,'install',classmethod(unavailable))
    with pytest.raises(ValueError,match='CUDA_BUDGET_ALREADY_EXCEEDED'):
        configured_region_adopter(config,tmp_path,1024)


def test_consumed_capacity_is_measured_again_before_replay():
    runtime,_=fake_torch('cuda',12_000_000_000)
    budget=RegionAdopterBudget.install('cuda',torch_module=runtime)
    initial=budget.remaining(torch_module=runtime)
    runtime.cuda.memory_reserved=lambda _:4_000_000_000
    assert budget.remaining(torch_module=runtime)==initial-4_000_000_000
    runtime.cuda.memory_reserved=lambda _:10_000_000_001
    with pytest.raises(ValueError,match='CUDA_BUDGET_ALREADY_EXCEEDED'):
        budget.remaining(torch_module=runtime)
