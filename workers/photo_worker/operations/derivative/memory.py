"""Frame-buffer RAM admission matching delivery derivative compiler policy."""
from photo_worker.runtime.executor import OperationFailure


def derivative_ram_bytes(source_width, source_height, width, height):
    dimensions = (source_width, source_height, width, height)
    if any(type(value) is not int or value < 1 for value in dimensions):
        raise ValueError('DERIVATIVE_DIMENSIONS_INVALID')
    source, target = source_width * source_height, width * height
    if max(source, target) > 200_000_000:
        raise ValueError('DERIVATIVE_PIXEL_LIMIT')
    return (256 << 20) + 192 * (source + target)


def admit_derivative(profile, source_width, source_height, width, height):
    required = derivative_ram_bytes(source_width, source_height, width, height)
    estimate = profile.get('resource_estimate')
    if estimate is None and profile.get('renderer_version') != 'v2.2':
        # Preserve bounded legacy small-image execution. V2.2 must carry admission.
        budget = 512 << 20
    elif (isinstance(estimate, dict) and type(estimate.get('ram_bytes')) is int
          and 0 < estimate['ram_bytes'] <= 2**53 - 1):
        budget = estimate['ram_bytes']
    else:
        raise OperationFailure('CONFIG_INVALID')
    if required > budget:
        raise OperationFailure('RAM_ADMISSION_EXCEEDED')
    return required
