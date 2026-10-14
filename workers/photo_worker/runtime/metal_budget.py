"""Install the operator's byte ceiling in the process-local PyTorch MPS allocator.

This does not bound external Metal libraries or replace node-wide DB admission.
https://docs.pytorch.org/docs/stable/generated/torch.mps.set_per_process_memory_fraction.html
"""
from dataclasses import dataclass
from math import floor, nextafter


@dataclass(frozen=True)
class MetalBudget:
    configured_bytes: int
    allocator_bytes: int
    recommended_bytes: int
    fraction: float


def valid_metal_budget_receipt(receipt: MetalBudget, limit_bytes: int) -> bool:
    """Check a handed-off receipt against the exact allocator setting."""
    if (not isinstance(receipt, MetalBudget) or type(limit_bytes) is not int
            or type(receipt.configured_bytes) is not int
            or type(receipt.allocator_bytes) is not int
            or type(receipt.recommended_bytes) is not int
            or receipt.configured_bytes != limit_bytes
            or not 1 <= limit_bytes <= 2**53 - 1
            or not 1 <= receipt.recommended_bytes <= 2**53 - 1
            or type(receipt.fraction) is not float):
        return False
    fraction = nextafter(min(1.0, limit_bytes / receipt.recommended_bytes), 0.0)
    effective = floor(receipt.recommended_bytes * fraction)
    return (receipt.fraction == fraction and
            receipt.allocator_bytes == effective and
            1 <= effective <= limit_bytes)


def install_metal_budget(limit_bytes: int, *, torch_module=None) -> MetalBudget:
    if type(limit_bytes) is not int or not 1 <= limit_bytes <= 2**53 - 1:
        raise ValueError('METAL_BUDGET_INVALID')
    if torch_module is None:
        import torch as torch_module
    if not torch_module.backends.mps.is_available():
        raise ValueError('METAL_BUDGET_UNAVAILABLE')
    recommended = torch_module.mps.recommended_max_memory()
    used = torch_module.mps.driver_allocated_memory()
    if (type(recommended) is not int or not 1 <= recommended <= 2**53 - 1
            or type(used) is not int or not 0 <= used <= 2**53 - 1):
        raise ValueError('METAL_BUDGET_MEASUREMENT_INVALID')
    # Never exceed Metal's recommendation. Round down so floating point
    # conversion cannot silently advertise a byte above the configured ceiling.
    fraction = nextafter(min(1.0, limit_bytes / recommended), 0.0)
    effective = floor(recommended * fraction)
    if effective < 1 or effective > limit_bytes:
        raise ValueError('METAL_BUDGET_UNREPRESENTABLE')
    if used > effective:
        raise ValueError('METAL_BUDGET_ALREADY_EXCEEDED')
    torch_module.mps.set_per_process_memory_fraction(fraction)
    after = torch_module.mps.driver_allocated_memory()
    if type(after) is not int or not 0 <= after <= effective:
        raise ValueError('METAL_BUDGET_ALREADY_EXCEEDED')
    return MetalBudget(limit_bytes, effective, recommended, fraction)


def remaining_metal_budget(limit_bytes: int, *, torch_module=None) -> int:
    """Free process budget after driver allocations and a 64 MiB guard band."""
    if type(limit_bytes) is not int or not 1 <= limit_bytes <= 2**53 - 1:
        raise ValueError('METAL_BUDGET_INVALID')
    if torch_module is None:
        import torch as torch_module
    if not torch_module.backends.mps.is_available():
        raise ValueError('METAL_BUDGET_UNAVAILABLE')
    used = torch_module.mps.driver_allocated_memory()
    if type(used) is not int or not 0 <= used <= 2**53 - 1:
        raise ValueError('METAL_BUDGET_MEASUREMENT_INVALID')
    if used > limit_bytes:
        raise ValueError('METAL_BUDGET_ALREADY_EXCEEDED')
    return max(0, limit_bytes - used - 64 * 1024 * 1024)
