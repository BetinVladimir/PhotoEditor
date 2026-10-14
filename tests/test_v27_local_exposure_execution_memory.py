import pytest
from photo_worker.operations.semantic.local_exposure import local_exposure_execution_memory


def test_loader_and_kernel_peaks_are_sequential_but_retained_costs_overlap():
    assert local_exposure_execution_memory((4000,6000),loader_peak_bytes=3_000_000_000,retained_loader_bytes=500_000_000)==3_000_000_000
    assert local_exposure_execution_memory((4000,6000),loader_peak_bytes=1_000_000_000,retained_loader_bytes=500_000_000)==2_804_000_000


@pytest.mark.parametrize('peak,retained',[(True,0),(0,False),(-1,0),(1,-1),(0,1),(2**53,0),(2**53-1,2**53-1)])
def test_invalid_or_overflowing_loader_costs_are_denied(peak,retained):
    with pytest.raises(ValueError,match='RESOURCE_PROFILE'):
        local_exposure_execution_memory((1,1),loader_peak_bytes=peak,retained_loader_bytes=retained)
