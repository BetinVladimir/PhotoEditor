from types import SimpleNamespace
import pytest
from photo_worker.runtime.metal_budget import install_metal_budget


def runtime(recommended=24_000_000_000, used=0, after=None):
    calls=[]
    def allocated():
        return after if calls and after is not None else used
    torch=SimpleNamespace(backends=SimpleNamespace(mps=SimpleNamespace(is_available=lambda:True)),
                          mps=SimpleNamespace(recommended_max_memory=lambda:recommended,
                                              driver_allocated_memory=allocated,
                                              set_per_process_memory_fraction=calls.append))
    return torch,calls


def test_metal_budget_installs_strict_ten_decimal_gb_ceiling():
    torch,calls=runtime()
    evidence=install_metal_budget(10_000_000_000,torch_module=torch)
    assert len(calls)==1 and 0<calls[0]<=1
    assert evidence.configured_bytes==10_000_000_000
    assert 9_999_999_990<=evidence.allocator_bytes<=10_000_000_000


def test_metal_budget_never_exceeds_recommended_working_set():
    torch,calls=runtime(recommended=4_000_000_000)
    result=install_metal_budget(10_000_000_000,torch_module=torch)
    assert 0<result.allocator_bytes<=4_000_000_000 and calls[0]<1


@pytest.mark.parametrize('limit',[True,0,-1,1.5,2**53])
def test_metal_budget_rejects_invalid_limit_before_runtime(limit):
    with pytest.raises(ValueError,match='METAL_BUDGET_INVALID'):
        install_metal_budget(limit,torch_module=object())


@pytest.mark.parametrize('recommended,used',[(True,0),(0,0),(2**53,0),(100,-1),(100,True),(100,2**53)])
def test_metal_budget_refuses_invalid_driver_measurements(recommended,used):
    torch,calls=runtime(recommended,used)
    with pytest.raises(ValueError,match='MEASUREMENT'):
        install_metal_budget(100,torch_module=torch)
    assert not calls


def test_metal_budget_refuses_already_loaded_over_budget_runtime():
    torch,calls=runtime(24_000_000_000,11_000_000_000)
    with pytest.raises(ValueError,match='ALREADY_EXCEEDED'):
        install_metal_budget(10_000_000_000,torch_module=torch)
    assert not calls


def test_metal_budget_rechecks_driver_after_installation():
    torch,calls=runtime(after=11_000_000_000)
    with pytest.raises(ValueError,match='ALREADY_EXCEEDED'):
        install_metal_budget(10_000_000_000,torch_module=torch)
    assert len(calls)==1


def test_worker_installs_only_configured_mps_gpu_budget(monkeypatch):
    import photo_worker.runtime.metal_budget as budget
    from photo_worker.runtime.service import _install_metal_budget
    calls=[]
    monkeypatch.setattr(budget,'install_metal_budget',lambda value:calls.append(value))
    policy=SimpleNamespace(gpu_budget_bytes=10_000_000_000)
    _install_metal_budget(SimpleNamespace(resource_class='gpu',backend='mps',capacity_policy=policy))
    for resource,backend,configured in [('cpu','cpu',policy),('gpu','cuda',policy)]:
        _install_metal_budget(SimpleNamespace(resource_class=resource,backend=backend,capacity_policy=configured))
    assert calls==[10_000_000_000]


@pytest.mark.parametrize('used', [0, 8_000_000_000, 10_000_000_000])
def test_remaining_metal_budget_subtracts_driver_memory_and_guard(used):
    from photo_worker.runtime.metal_budget import remaining_metal_budget
    torch, calls = runtime(used=used)
    assert remaining_metal_budget(10_000_000_000, torch_module=torch) == max(0, 10_000_000_000-used-64*1024*1024)
    assert not calls


@pytest.mark.parametrize('used', [True, -1, 2**53, 10_000_000_001])
def test_remaining_metal_budget_refuses_invalid_or_exceeded_measurements(used):
    from photo_worker.runtime.metal_budget import remaining_metal_budget
    torch, _ = runtime(used=used)
    with pytest.raises(ValueError):
        remaining_metal_budget(10_000_000_000, torch_module=torch)


def test_capacity_sample_caps_mps_by_remaining_allocator_budget(monkeypatch, tmp_path):
    import photo_worker.runtime.metal_budget as budget
    from photo_worker.runtime.service import _capacity_sample
    from photo_worker.runtime.capacity import Capacity
    policy=SimpleNamespace(gpu_budget_bytes=10_000_000_000,
        cap_measurement=lambda measured, **kwargs: measured)
    config=SimpleNamespace(capacity_policy=policy, resource_class='gpu', backend='mps', scratch_root=tmp_path)
    monkeypatch.setattr(budget, 'remaining_metal_budget', lambda limit: 100)
    result=_capacity_sample(config, Capacity(1000, 2000, 3000))
    assert result==Capacity(1000, 2000, 100)


def test_mps_budget_failure_stops_built_worker_without_publishing_capacity(monkeypatch, tmp_path):
    import photo_worker.runtime.service as service
    import photo_worker.runtime.metal_budget as budget
    from photo_worker.runtime.capacity import Capacity
    from threading import Event
    calls=[]
    aborted=Event()
    policy=SimpleNamespace(gpu_budget_bytes=10_000_000_000, heartbeat_seconds=.001,
        cap_measurement=lambda measured, **kwargs: measured)
    config=SimpleNamespace(capacity_policy=policy, resource_class='gpu', backend='mps',
        scratch_root=tmp_path, worker_id='worker')
    samples=[]
    def remaining(limit):
        samples.append(limit)
        if len(samples)>1:
            raise ValueError('METAL_BUDGET_ALREADY_EXCEEDED')
        return 100
    monkeypatch.setattr(budget, 'remaining_metal_budget', remaining)
    monkeypatch.setattr(service, '_install_metal_budget', lambda config: None)
    monkeypatch.setattr(service, 'measure_accelerator', lambda root, backend: Capacity(1000, 2000, 3000))
    monkeypatch.setattr(service.signal, 'getsignal', lambda sig: None)
    monkeypatch.setattr(service.signal, 'signal', lambda sig, handler: None)
    database=SimpleNamespace(ping=lambda:None, accelerator_operations=lambda *args:{'operation'},
        heartbeat_accelerator_worker=lambda *args:calls.append(args) or True)
    def consume(stop):
        assert stop.wait(2), 'capacity failure did not stop consumer'
    transport=SimpleNamespace(executor=SimpleNamespace(operations={'operation'}, abort_active=aborted.set), run=consume)
    with pytest.raises(RuntimeError, match='capacity reporting stopped'):
        service.run_built_worker(config, database, transport)
    assert aborted.is_set()
    assert len(calls)==1 and calls[0][4]==100
    assert len(samples)==2


def test_mps_startup_measurement_failure_never_starts_consumer(monkeypatch, tmp_path):
    import photo_worker.runtime.service as service
    import photo_worker.runtime.metal_budget as budget
    from photo_worker.runtime.capacity import Capacity
    events=[]
    config=SimpleNamespace(resource_class='gpu',backend='mps',worker_id='worker',scratch_root=tmp_path,
        capacity_policy=SimpleNamespace(gpu_budget_bytes=10_000_000_000,heartbeat_seconds=1,
            cap_measurement=lambda measured, **kwargs:measured))
    def failed(limit):
        raise ValueError('METAL_BUDGET_MEASUREMENT_INVALID')
    monkeypatch.setattr(budget,'remaining_metal_budget',failed)
    monkeypatch.setattr(service,'_install_metal_budget',lambda config:None)
    monkeypatch.setattr(service,'measure_accelerator',lambda *args:Capacity(100,100,100))
    database=SimpleNamespace(ping=lambda:None,accelerator_operations=lambda *args:{'operation'},
        heartbeat_accelerator_worker=lambda *args:events.append('heartbeat'))
    transport=SimpleNamespace(executor=SimpleNamespace(operations={'operation'},abort_active=lambda:events.append('abort')),
        run=lambda stop:events.append('consume'))
    with pytest.raises(ValueError,match='MEASUREMENT_INVALID'):
        service.run_built_worker(config,database,transport)
    assert events==[]


def test_capacity_uses_installed_allocator_ceiling_even_if_configured_limit_is_larger(monkeypatch, tmp_path):
    from photo_worker.runtime import metal_budget as module
    from photo_worker.runtime.service import _capacity_sample
    from photo_worker.runtime.capacity import Capacity
    torch, _ = runtime(recommended=4_000_000_000)
    receipt = install_metal_budget(10_000_000_000, torch_module=torch)
    limits = []
    monkeypatch.setattr(module, 'remaining_metal_budget', lambda limit: limits.append(limit) or 123)
    config = SimpleNamespace(resource_class='gpu',backend='mps',scratch_root=tmp_path,
        capacity_policy=SimpleNamespace(gpu_budget_bytes=10_000_000_000,
            cap_measurement=lambda measured, **kwargs: measured))
    result = _capacity_sample(config, Capacity(1000,2000,3000), metal_budget=receipt)
    assert limits == [receipt.allocator_bytes]
    assert result.vram_bytes == 123


@pytest.mark.parametrize('receipt', [object(),
    SimpleNamespace(configured_bytes=10_000_000_000, allocator_bytes=100)])
def test_capacity_refuses_untrusted_allocator_receipt_before_driver_read(monkeypatch, tmp_path, receipt):
    from photo_worker.runtime import metal_budget as module
    from photo_worker.runtime.service import _capacity_sample, WorkerConfigurationError
    from photo_worker.runtime.capacity import Capacity
    monkeypatch.setattr(module,'remaining_metal_budget',lambda limit:pytest.fail('driver read'))
    config=SimpleNamespace(resource_class='gpu',backend='mps',scratch_root=tmp_path,
        capacity_policy=SimpleNamespace(gpu_budget_bytes=10_000_000_000,
            cap_measurement=lambda measured,**kwargs:measured))
    with pytest.raises(WorkerConfigurationError,match='receipt mismatch'):
        _capacity_sample(config,Capacity(100,100,100),metal_budget=receipt)


def test_direct_mps_install_refuses_missing_policy_before_torch(monkeypatch):
    import photo_worker.runtime.metal_budget as module
    from photo_worker.runtime.service import _install_metal_budget, WorkerConfigurationError
    monkeypatch.setattr(module,'install_metal_budget',lambda *args:pytest.fail('allocator touched'))
    with pytest.raises(WorkerConfigurationError,match='PHOTO_CAPACITY_POLICY required for MPS'):
        _install_metal_budget(SimpleNamespace(resource_class='gpu',backend='mps',capacity_policy=None))


def test_mps_heartbeats_reduce_advertised_budget_as_driver_allocations_grow(monkeypatch, tmp_path):
    from photo_worker.runtime import metal_budget as module
    from photo_worker.runtime.service import _capacity_sample
    from photo_worker.runtime.capacity import Capacity
    from photo_worker.runtime.capacity_policy import load_capacity_policy
    from pathlib import Path

    policy = load_capacity_policy(Path(__file__).resolve().parents[2] / 'configs/capacity-metal-verification.json')
    assert policy.gpu_budget_bytes == 10_000_000_000
    torch, _ = runtime()
    receipt = install_metal_budget(policy.gpu_budget_bytes, torch_module=torch)
    allocated = [0]
    torch.mps.driver_allocated_memory = lambda: allocated[0]
    real_remaining = module.remaining_metal_budget
    monkeypatch.setattr(module, 'remaining_metal_budget', lambda limit: real_remaining(limit, torch_module=torch))
    config = SimpleNamespace(resource_class='gpu', backend='mps', scratch_root=tmp_path, capacity_policy=policy)
    physical = Capacity(40_000_000_000, 20_000_000_000, 24_000_000_000)
    reports = []
    for used in [0, 3_000_000_000, receipt.allocator_bytes - 1, receipt.allocator_bytes]:
        allocated[0] = used
        measured = _capacity_sample(config, physical, metal_budget=receipt)
        reports.append(measured.vram_bytes)
        assert measured.vram_bytes == max(0, receipt.allocator_bytes - used - 64 * 1024 * 1024)
        assert measured.vram_bytes <= policy.gpu_budget_bytes
    assert reports == sorted(reports, reverse=True)
    allocated[0] = receipt.allocator_bytes + 1
    with pytest.raises(ValueError, match='ALREADY_EXCEEDED'):
        _capacity_sample(config, physical, metal_budget=receipt)
