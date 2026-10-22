"""Automatic repair providers use the complete Render admission contract."""
import pytest
from photo_worker.v2.render_conservative import conservative_render_provider_peak
from photo_worker.v2.render_resources import render_resources, admit_render
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('width,height', [(1, 1), (6000, 4000), (4000, 6000)])
def test_repair_budget_is_accepted_and_all_resource_dimensions_are_enforced(width, height):
    peak = conservative_render_provider_peak(width, height)
    assert peak['ram_bytes'] >= max(1 << 30, 128 * width * height + (256 << 20))
    assert peak['scratch_bytes'] >= 12 * width * height + 8192
    assert peak['vram_bytes'] == 0
    budget = render_resources(width, height, mask_refs=0, mask_nodes=0, provider_peak=peak)
    profile = {'resource_estimate': budget}
    assert admit_render(profile, width, height, mask_refs=0, mask_nodes=0, provider_peak=peak) == budget
    for field, code in [('ram_bytes', 'RAM_ADMISSION_EXCEEDED'),
                        ('scratch_bytes', 'SCRATCH_ADMISSION_EXCEEDED'),
                        ('output_bytes', 'OUTPUT_ADMISSION_EXCEEDED')]:
        insufficient = dict(budget)
        insufficient[field] -= 1
        with pytest.raises(OperationFailure) as error:
            admit_render({'resource_estimate': insufficient}, width, height,
                         mask_refs=0, mask_nodes=0, provider_peak=peak)
        assert code in str(error.value)


@pytest.mark.parametrize('width,height', [(0, 1), (1, -1), (True, 2), (2.0, 2), (200000001, 1)])
def test_repair_budget_rejects_invalid_canvas_before_allocation(width, height):
    with pytest.raises(ValueError, match='DUST_REPAIR_INPUT_INVALID'):
        conservative_render_provider_peak(width, height)
