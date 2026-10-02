from uuid import uuid4
from hashlib import sha256
import pytest
from test_v26_render_reports import fixture
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.v2.render_reports import RenderReportEvidence
from photo_worker.v2.render_stage import StagedRenderImage
from photo_worker.v2.render_result import stage_render_result


def test_result_retains_report_roots_and_quality_gate_and_counts_bytes():
    _,parent,binding,report=fixture()
    report.parent_reports=[parent.report]
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    image=StagedRenderImage(ref(),ref(),None,(),0);staged=[]
    class Artifacts:
        def stage_contract(self,value,role):
            body=canonical_bytes(value);reference=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
            staged.append((reference,value,role,body));return reference
    result=stage_render_result(Artifacts(),image,report,binding,RenderReportEvidence((parent.report,),True))
    assert result.review_required and result.document==staged[1][0]
    assert result.outputs==tuple(item[0] for item in staged)
    assert result.bytes_written==sum(len(item[3]) for item in staged)
    assert staged[0][1].parent_reports==[parent.report]
    assert staged[1][1].report==staged[0][0]


@pytest.mark.parametrize('failure',['dropped_parent','pass','fail','binding'])
def test_invalid_quality_rejects_before_any_staging(failure):
    _,parent,binding,report=fixture();report.parent_reports=[parent.report]
    if failure=='dropped_parent':report.parent_reports=[]
    if failure=='pass':report.status='pass'
    if failure=='fail':report.status='fail'
    if failure=='binding':report.sources=[binding.model_copy(update={'canvas_hash':'c'*64})]
    image=StagedRenderImage(parent.report,parent.report,None,(),0)
    class Artifacts:
        def stage_contract(self,*args):pytest.fail('invalid quality was staged')
    with pytest.raises(ValueError,match='QUALITY_INVALID'):
        stage_render_result(Artifacts(),image,report,binding,RenderReportEvidence((parent.report,),True))
