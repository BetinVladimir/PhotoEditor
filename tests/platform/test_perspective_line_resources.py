import pytest
from photo_worker.operations.optical.perspective_line_resources import (
    line_detection_resources, admit_line_detection)
from photo_worker.runtime.executor import OperationFailure


def profile(alpha=False):
    return dict(backend='cpu', precision='float32', renderer_version='v2.2',
                resource_estimate=line_detection_resources(100, 80, alpha))


def test_cpu_budget_counts_float_alpha_and_downloads():
    plain = line_detection_resources(100, 80, False)
    alpha = line_detection_resources(100, 80, True)
    assert alpha['ram_bytes']-plain['ram_bytes'] == 8000*4
    assert alpha['scratch_bytes']-plain['scratch_bytes'] == 8000*4
    assert alpha['vram_bytes'] == 0
    assert admit_line_detection(profile(True), 100, 80, True) == alpha


@pytest.mark.parametrize('field,code', [
    ('ram_bytes','RAM_ADMISSION_EXCEEDED'),
    ('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
    ('output_bytes','OUTPUT_ADMISSION_EXCEEDED')])
def test_underreservation_refused(field, code):
    value = profile(); value['resource_estimate'][field] -= 1
    with pytest.raises(OperationFailure) as error:
        admit_line_detection(value, 100, 80, False)
    assert error.value.error.code == code


@pytest.mark.parametrize('field,value', [('pixel_count',True),('pixel_count',7999),
    ('vram_bytes',1),('ram_bytes',2**53),('scratch_bytes',None)])
def test_malformed_estimate_refused(field, value):
    request = profile(); request['resource_estimate'][field] = value
    with pytest.raises(OperationFailure):
        admit_line_detection(request, 100, 80, False)


@pytest.mark.parametrize('dimensions', [(True,80,False),(1,80,False),
    (20001,10000,False),(100,80,1)])
def test_invalid_dimensions(dimensions):
    with pytest.raises(ValueError): line_detection_resources(*dimensions)
