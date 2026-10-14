from types import SimpleNamespace
from unittest.mock import Mock
from math import floor, nextafter

import pytest

from photo_worker.runtime import service, metal_budget
from photo_worker.runtime.capacity import Capacity

LIMIT = 10_000_000_000


def valid_receipt():
    recommended = 12_000_000_000
    fraction = nextafter(LIMIT / recommended, 0.0)
    return metal_budget.MetalBudget(LIMIT, floor(recommended * fraction), recommended, fraction)


def config_and_policy(monkeypatch):
    policy = SimpleNamespace(gpu_budget_bytes=LIMIT,
        cap_measurement=Mock(return_value=Capacity(100, 200, LIMIT)))
    config = SimpleNamespace(capacity_policy=policy, resource_class='gpu', backend='mps', scratch_root='/unused')
    monkeypatch.setattr(service.shutil, 'disk_usage', lambda root: SimpleNamespace(total=20_000_000_000))
    return config, policy


@pytest.mark.parametrize('receipt', [None, object(),
    metal_budget.MetalBudget(LIMIT + 1, LIMIT, LIMIT, 1.0),
    metal_budget.MetalBudget(LIMIT, True, LIMIT, 1.0),
    metal_budget.MetalBudget(LIMIT, 0, LIMIT, 1.0),
    metal_budget.MetalBudget(LIMIT, LIMIT + 1, LIMIT, 1.0),
    metal_budget.MetalBudget(LIMIT, LIMIT - 1, 12_000_000_000, 0.8),
    metal_budget.MetalBudget(LIMIT, LIMIT - 1, 12_000_000_000, float('nan'))])
def test_mps_heartbeat_requires_matching_installed_allocator_receipt(monkeypatch, receipt):
    config, policy = config_and_policy(monkeypatch)
    remaining = Mock(return_value=LIMIT)
    monkeypatch.setattr(metal_budget, 'remaining_metal_budget', remaining)
    with pytest.raises(service.WorkerConfigurationError, match='receipt mismatch'):
        service._capacity_sample(config, Capacity(100, 200, LIMIT), metal_budget=receipt)
    remaining.assert_not_called()


@pytest.mark.parametrize('available', [0, 128, LIMIT - 1])
def test_mps_heartbeat_uses_effective_allocator_ceiling(monkeypatch, available):
    config, policy = config_and_policy(monkeypatch)
    receipt = valid_receipt()
    remaining = Mock(return_value=available)
    monkeypatch.setattr(metal_budget, 'remaining_metal_budget', remaining)
    sample = service._capacity_sample(config, Capacity(100, 200, LIMIT), metal_budget=receipt)
    remaining.assert_called_once_with(receipt.allocator_bytes)
    assert sample == Capacity(100, 200, available)


def test_mps_measurement_failure_does_not_return_advertised_capacity(monkeypatch):
    config, policy = config_and_policy(monkeypatch)
    receipt = valid_receipt()
    monkeypatch.setattr(metal_budget, 'remaining_metal_budget', Mock(side_effect=ValueError('METAL_BUDGET_ALREADY_EXCEEDED')))
    with pytest.raises(ValueError, match='ALREADY_EXCEEDED'):
        service._capacity_sample(config, Capacity(100, 200, LIMIT), metal_budget=receipt)


def test_mps_capacity_without_policy_refuses_before_measurement(monkeypatch):
    config = SimpleNamespace(capacity_policy=None, resource_class='gpu', backend='mps')
    remaining = Mock()
    disk = Mock()
    monkeypatch.setattr(metal_budget, 'remaining_metal_budget', remaining)
    monkeypatch.setattr(service.shutil, 'disk_usage', disk)
    with pytest.raises(service.WorkerConfigurationError, match='PHOTO_CAPACITY_POLICY required'):
        service._capacity_sample(config, Capacity(100, 200, LIMIT))
    remaining.assert_not_called()
    disk.assert_not_called()


@pytest.mark.parametrize('resource_class,backend', [('gpu', 'cuda'), ('cpu', 'cpu'), ('archive', 'cpu'), ('media', 'cpu')])
def test_non_mps_legacy_capacity_path_preserves_sample_without_policy(monkeypatch, resource_class, backend):
    config = SimpleNamespace(capacity_policy=None, resource_class=resource_class, backend=backend)
    sample = Capacity(100, 200, 300)
    disk = Mock()
    monkeypatch.setattr(service.shutil, 'disk_usage', disk)
    assert service._capacity_sample(config, sample) is sample
    disk.assert_not_called()


def test_mps_start_reuses_prebuild_receipt_without_resetting_allocator(monkeypatch):
    config, _ = config_and_policy(monkeypatch)
    receipt = valid_receipt()
    install = Mock(side_effect=AssertionError('allocator installed twice'))
    remaining = Mock(return_value=LIMIT - 1)
    monkeypatch.setattr(service, '_install_metal_budget', install)
    monkeypatch.setattr(metal_budget, 'remaining_metal_budget', remaining)
    assert service._ready_metal_budget(config, receipt) is receipt
    install.assert_not_called()
    remaining.assert_called_once_with(receipt.allocator_bytes)


def test_mps_prebuilt_worker_installs_once_and_mismatched_receipt_refuses(monkeypatch):
    config, _ = config_and_policy(monkeypatch)
    receipt = valid_receipt()
    install = Mock(return_value=receipt)
    remaining = Mock()
    monkeypatch.setattr(service, '_install_metal_budget', install)
    monkeypatch.setattr(metal_budget, 'remaining_metal_budget', remaining)
    assert service._ready_metal_budget(config, None) is receipt
    install.assert_called_once_with(config)
    for wrong in (object(), metal_budget.MetalBudget(LIMIT + 1, LIMIT - 1, 12_000_000_000, 0.8)):
        with pytest.raises(service.WorkerConfigurationError, match='receipt mismatch'):
            service._ready_metal_budget(config, wrong)
    remaining.assert_not_called()


def test_run_worker_carries_prebuild_metal_receipt_to_lifecycle(monkeypatch):
    config, _ = config_and_policy(monkeypatch)
    receipt = valid_receipt()
    installed = Mock(return_value=receipt)
    build = Mock(return_value=('database', 'transport'))
    run = Mock()
    monkeypatch.setattr(service, '_install_metal_budget', installed)
    monkeypatch.setattr(service, 'build_worker', build)
    monkeypatch.setattr(service, 'run_built_worker', run)
    service.run_worker(config)
    installed.assert_called_once_with(config)
    run.assert_called_once_with(config, 'database', 'transport', metal_budget=receipt)
