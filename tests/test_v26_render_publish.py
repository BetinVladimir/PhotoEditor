from types import SimpleNamespace
import pytest
from test_v26_render_reports import fixture
from photo_worker.v2.render_reports import RenderReportEvidence
from photo_worker.v2.render_publish import publish_prepared_render


def test_publication_rejects_lost_source_quality_before_any_staging():
    artifacts,parent,binding,report=fixture('pass')
    inputs=SimpleNamespace(recipe=SimpleNamespace(recipe=SimpleNamespace(source=binding)))
    prepared=SimpleNamespace(source_reports=RenderReportEvidence((parent.report,),False))
    with pytest.raises(ValueError,match='SOURCE_REPORTS_DROPPED'):
        publish_prepared_render(artifacts,inputs,prepared,report,())


def test_publication_rejects_measured_failure_before_materialization():
    artifacts,parent,binding,report=fixture('fail')
    inputs=SimpleNamespace(recipe=SimpleNamespace(recipe=SimpleNamespace(source=binding)))
    with pytest.raises(ValueError,match='MEASURED_INVALID'):
        publish_prepared_render(artifacts,inputs,None,report,(parent,))
    assert artifacts.reads==0


def test_chain_review_preserves_measured_reason_metrics_and_all_parents(monkeypatch):
    from contextlib import contextmanager
    from uuid import uuid4
    import numpy as np
    import photo_worker.v2.render_publish as publication
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    from photo_worker.v2.render_beauty import RenderBeautyStage
    from photo_worker.v2.render_portrait import RenderPortraitBase
    from photo_worker.v2.render_effects import RenderEffectChain
    from photo_worker.operations.portrait.prepare import FaceEvidence
    artifacts,parent,binding,report=fixture('pass')
    report=report.model_copy(update={'reason':'MEASURED_TEXTURE_WARNING','clipping_fraction':.12,'outside_mask_max_error':.03,'skipped_count':2})
    before=canonical_bytes(report)
    pixels=AdjustmentPixels(np.zeros((1,1,3),np.float32),None)
    skipped=RenderBeautyStage(pixels,(FaceEvidence(uuid4(),False,'low_confidence'),),False)
    clean=RenderPortraitBase(pixels,(),False)
    prepared=SimpleNamespace(source_reports=RenderReportEvidence((parent.report,),False),
        effect_evidence=RenderEffectChain(pixels,(skipped,clean)),alpha_requires_new_reference=False)
    inputs=SimpleNamespace(recipe=SimpleNamespace(recipe=SimpleNamespace(source=binding)),source=object())
    artifacts.scratch=object();events=[]
    @contextmanager
    def files(*args,**kwargs):events.append('files');yield object()
    def result(_artifacts,image,actual,_binding,parents,**kwargs):
        assert actual.status=='review' and actual.reason==report.reason
        assert actual.clipping_fraction==.12 and actual.outside_mask_max_error==.03 and actual.skipped_count==2
        assert tuple(actual.parent_reports)==parents.reports==(parent.report,)
        events.append('result');return actual
    monkeypatch.setattr(publication,'prepare_render_output_files',files)
    monkeypatch.setattr(publication,'stage_render_image',lambda *args,**kwargs:object())
    monkeypatch.setattr(publication,'stage_render_result',result)
    actual=publish_prepared_render(artifacts,inputs,prepared,report,(parent,))
    assert actual.status=='review' and canonical_bytes(report)==before and events==['files','result']
