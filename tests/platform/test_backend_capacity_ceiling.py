from types import SimpleNamespace
import pytest
from photo_worker.runtime.capacity import Capacity
from photo_worker.runtime.service import _capacity_sample


@pytest.fixture(autouse=True)
def fake_cuda_free_budget(monkeypatch):
    monkeypatch.setattr('photo_worker.runtime.cuda_budget.remaining_cuda_budget',lambda limit:limit)


@pytest.mark.parametrize('free,expected', [(12_000_000_000,10_000_000_000),(8_000_000_000,8_000_000_000),(0,0)])
def test_cuda_ceiling_without_operator_policy(free,expected):
    config=SimpleNamespace(resource_class='gpu',backend='cuda',capacity_policy=None)
    measured=Capacity(20_000_000_000,30_000_000_000,free)
    assert _capacity_sample(config,measured)==Capacity(measured.ram_bytes,measured.scratch_bytes,expected)


def test_cuda_operator_policy_cannot_raise_mvp_ceiling(monkeypatch):
    observed=[]
    def cap(measured,**kwargs):
        observed.append(measured)
        return measured
    policy=SimpleNamespace(cap_measurement=cap,gpu_budget_bytes=12_000_000_000)
    config=SimpleNamespace(resource_class='gpu',backend='cuda',capacity_policy=policy,scratch_root='/unused')
    monkeypatch.setattr('photo_worker.runtime.service.shutil.disk_usage',lambda _:SimpleNamespace(total=40_000_000_000))
    value=_capacity_sample(config,Capacity(20_000_000_000,30_000_000_000,12_000_000_000))
    assert observed[0].vram_bytes==value.vram_bytes==10_000_000_000


def test_non_gpu_cpu_measurements_are_not_given_cuda_ceiling():
    config=SimpleNamespace(resource_class='cpu',backend='cpu',capacity_policy=None)
    measured=Capacity(20_000_000_000,30_000_000_000,0)
    assert _capacity_sample(config,measured)==measured


@pytest.mark.parametrize('free',[True,-1,12_000_000_000.0,2**53])
def test_cuda_ceiling_does_not_normalize_invalid_measurement(free):
    config=SimpleNamespace(resource_class='gpu',backend='cuda',capacity_policy=None)
    with pytest.raises(ValueError,match='CAPACITY_MEASUREMENT_INVALID'):
        _capacity_sample(config,Capacity(20_000_000_000,30_000_000_000,free))


def test_cuda_capacity_subtracts_existing_allocator_reservations(monkeypatch):
    monkeypatch.setattr('photo_worker.runtime.cuda_budget.remaining_cuda_budget',lambda limit:2_000_000_000)
    config=SimpleNamespace(resource_class='gpu',backend='cuda',capacity_policy=None)
    assert _capacity_sample(config,Capacity(20_000_000_000,30_000_000_000,12_000_000_000)).vram_bytes==2_000_000_000
