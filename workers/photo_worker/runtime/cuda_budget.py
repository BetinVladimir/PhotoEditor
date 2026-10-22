"""MVP per-process CUDA allocator budget; external providers still need qualification."""
from math import floor, nextafter

CUDA_MVP_BYTES = 10_000_000_000
GUARD_BYTES = 64 * 1024 * 1024


def _runtime(limit_bytes, torch_module):
    if type(limit_bytes) is not int or not 1 <= limit_bytes <= CUDA_MVP_BYTES:
        raise ValueError('CUDA_BUDGET_INVALID')
    if torch_module is None:
        import torch as torch_module
    if not torch_module.cuda.is_available() or torch_module.cuda.device_count() != 1:
        raise ValueError('CUDA_BUDGET_UNAVAILABLE')
    total = torch_module.cuda.get_device_properties(0).total_memory
    used = torch_module.cuda.memory_reserved(0)
    if (type(total) is not int or not 1 <= total <= 2**53-1 or
            type(used) is not int or not 0 <= used <= total):
        raise ValueError('CUDA_BUDGET_MEASUREMENT_INVALID')
    fraction = nextafter(min(1.0, limit_bytes / total), 0.0)
    effective = floor(total * fraction)
    if not 1 <= effective <= limit_bytes:
        raise ValueError('CUDA_BUDGET_UNREPRESENTABLE')
    if used > effective:
        raise ValueError('CUDA_BUDGET_ALREADY_EXCEEDED')
    return torch_module, fraction, effective, used


def install_cuda_budget(limit_bytes=CUDA_MVP_BYTES, *, torch_module=None):
    torch_module, fraction, effective, _ = _runtime(limit_bytes, torch_module)
    torch_module.cuda.set_per_process_memory_fraction(fraction, 0)
    # Re-read after installation, before providers may load any models.
    _runtime(limit_bytes, torch_module)
    return effective


def remaining_cuda_budget(limit_bytes=CUDA_MVP_BYTES, *, torch_module=None):
    _, _, effective, used = _runtime(limit_bytes, torch_module)
    return max(0, effective - used - GUARD_BYTES)
