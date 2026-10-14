from math import floor
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from photo_worker.runtime.metal_budget import install_metal_budget, remaining_metal_budget, valid_metal_budget_receipt

LIMIT = 10_000_000_000
GUARD = 64 * 1024 * 1024


def torch_double(*, recommended=12_000_000_000, used=0, available=True):
    mps = SimpleNamespace(recommended_max_memory=Mock(return_value=recommended),
        driver_allocated_memory=Mock(return_value=used), set_per_process_memory_fraction=Mock())
    return SimpleNamespace(backends=SimpleNamespace(mps=SimpleNamespace(is_available=Mock(return_value=available))), mps=mps)


def test_ten_decimal_gigabyte_install_then_remaining_budget_lifecycle():
    torch = torch_double(used=128 * 1024 * 1024)
    receipt = install_metal_budget(LIMIT, torch_module=torch)
    fraction = torch.mps.set_per_process_memory_fraction.call_args.args[0]
    assert receipt.configured_bytes == LIMIT
    assert receipt.fraction == fraction
    assert valid_metal_budget_receipt(receipt, LIMIT)
    assert receipt.allocator_bytes == floor(receipt.recommended_bytes * fraction)
    assert 0 < receipt.allocator_bytes <= LIMIT
    assert 0 < fraction <= 1
    for used in (0, 128 * 1024 * 1024, receipt.allocator_bytes - 1, receipt.allocator_bytes):
        torch.mps.driver_allocated_memory.return_value = used
        assert remaining_metal_budget(receipt.allocator_bytes, torch_module=torch) == max(0, receipt.allocator_bytes - used - GUARD)
    torch.mps.driver_allocated_memory.return_value = receipt.allocator_bytes + 1
    with pytest.raises(ValueError, match='ALREADY_EXCEEDED'):
        remaining_metal_budget(receipt.allocator_bytes, torch_module=torch)


@pytest.mark.parametrize('recommended', [8_000_000_000, 10_000_000_000, 12_000_000_000, 2**53 - 1])
def test_metal_ceiling_never_exceeds_operator_limit_or_device_recommendation(recommended):
    torch = torch_double(recommended=recommended)
    receipt = install_metal_budget(LIMIT, torch_module=torch)
    assert 0 < receipt.allocator_bytes <= min(LIMIT, recommended)
    torch.mps.set_per_process_memory_fraction.assert_called_once_with(receipt.fraction)


@pytest.mark.parametrize('limit', [True, 0, -1, 10_000_000_000.0, 2**53])
def test_invalid_budget_refuses_before_device_access(limit):
    torch = torch_double()
    for operation in (install_metal_budget, remaining_metal_budget):
        with pytest.raises(ValueError, match='BUDGET_INVALID'):
            operation(limit, torch_module=torch)
    torch.backends.mps.is_available.assert_not_called()
    torch.mps.set_per_process_memory_fraction.assert_not_called()


def test_over_budget_existing_allocation_refuses_allocator_install():
    torch = torch_double(used=LIMIT + 1)
    with pytest.raises(ValueError, match='ALREADY_EXCEEDED'):
        install_metal_budget(LIMIT, torch_module=torch)
    torch.mps.set_per_process_memory_fraction.assert_not_called()


def test_allocation_growth_during_installation_refuses_receipt():
    torch = torch_double()
    torch.mps.driver_allocated_memory.side_effect = [0, LIMIT + 1]
    with pytest.raises(ValueError, match='ALREADY_EXCEEDED'):
        install_metal_budget(LIMIT, torch_module=torch)
    torch.mps.set_per_process_memory_fraction.assert_called_once()


def test_unavailable_mps_refuses_before_allocator_measurement():
    torch = torch_double(available=False)
    for operation in (install_metal_budget, remaining_metal_budget):
        with pytest.raises(ValueError, match='UNAVAILABLE'):
            operation(LIMIT, torch_module=torch)
    torch.mps.recommended_max_memory.assert_not_called()
    torch.mps.driver_allocated_memory.assert_not_called()
    torch.mps.set_per_process_memory_fraction.assert_not_called()
