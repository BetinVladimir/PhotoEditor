"""Conservative CPU admission for source-bound line evidence extraction."""
from photo_worker.runtime.executor import OperationFailure


def line_detection_resources(width, height, has_alpha):
    if (type(width) is not int or type(height) is not int or
            min(width, height) < 2 or width * height > 200000000 or
            type(has_alpha) is not bool):
        raise ValueError('PERSPECTIVE_LINE_INPUT_INVALID')
    pixels = width * height
    # Include all mapped source pages, verified download files and bounded
    # detector/proxy/tile workspace. Alpha may be float32, not only uint16.
    source_bytes = pixels * (16 if has_alpha else 12)
    return dict(pixel_count=pixels, ram_bytes=source_bytes + 256 * 1024 * 1024,
                scratch_bytes=source_bytes + 32 * 1024 * 1024,
                output_bytes=1024 * 1024, vram_bytes=0)


def admit_line_detection(profile, width, height, has_alpha):
    required = line_detection_resources(width, height, has_alpha)
    if (not isinstance(profile, dict) or profile.get('backend') != 'cpu' or
            profile.get('precision') != 'float32' or profile.get('renderer_version') != 'v2.2'):
        raise OperationFailure('CONFIG_INVALID')
    estimate = profile.get('resource_estimate')
    if not isinstance(estimate, dict) or any(
            type(estimate.get(key)) is not int or not 0 <= estimate[key] <= 2**53-1
            for key in required):
        raise OperationFailure('CONFIG_INVALID')
    if estimate['pixel_count'] != required['pixel_count'] or estimate['vram_bytes'] != 0:
        raise OperationFailure('CONFIG_INVALID')
    for name, code in [('ram_bytes', 'RAM_ADMISSION_EXCEEDED'),
                       ('scratch_bytes', 'SCRATCH_ADMISSION_EXCEEDED'),
                       ('output_bytes', 'OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[name] < required[name]:
            raise OperationFailure(code)
    return required
