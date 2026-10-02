import pytest
from test_v26_render_reports import fixture
from photo_worker.v2.render_quality import build_render_quality


def test_measured_metrics_are_preserved_and_parent_review_is_inherited():
    artifacts,parent,binding,ancestor=fixture('review')
    measured=ancestor.model_copy(deep=True);measured.status='pass';measured.clipping_fraction=.125
    measured.reason='MEASURED_DETAIL';measured.outside_mask_max_error=.01
    result=build_render_quality(artifacts,measured,(parent,),binding)
    assert result.report.status=='review' and result.report.reason=='MEASURED_DETAIL'
    assert result.report.clipping_fraction==.125 and result.report.outside_mask_max_error==.01
    assert result.report.parent_reports==[parent.report] and result.parents.review_required
    assert measured.status=='pass' and measured.parent_reports is None


@pytest.mark.parametrize('failure',['fail','parents','source'])
def test_invalid_measurement_or_parent_inventory_cannot_be_repaired(failure):
    artifacts,parent,binding,ancestor=fixture('pass');measured=ancestor.model_copy(deep=True)
    if failure=='fail':measured.status='fail'
    if failure=='parents':measured.parent_reports=[]
    if failure=='source':measured.sources=[binding.model_copy(update={'canvas_hash':'c'*64})]
    with pytest.raises(ValueError):build_render_quality(artifacts,measured,(parent,),binding)
