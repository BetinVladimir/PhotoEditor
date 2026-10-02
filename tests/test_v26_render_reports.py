from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.optical.contracts import ImageReport
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.render_reports import collect_render_parent_reports


def fixture(status='review'):
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    binding=VersionBinding.model_validate_json(canonical_bytes(dict(asset_version_id=str(uuid4()),source=ref.model_dump(mode='json'),canvas_hash='b'*64)))
    report=ImageReport(schema_version=1,status=status,operation='test.adjust',skipped_count=0,reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[binding],registrations=[])
    class Artifacts:
        reads=0
        def contract(self,reference,model):self.reads+=1;assert model is ImageReport;return report
    return Artifacts(),SimpleNamespace(report=ref),binding,report


def test_retains_pinned_report_once_and_inherits_review_gate():
    artifacts,parent,binding,_=fixture()
    evidence=collect_render_parent_reports(artifacts,(None,parent,parent),binding)
    assert evidence.review_required and evidence.reports==(parent.report,) and artifacts.reads==1


@pytest.mark.parametrize('status',['fail','unavailable'])
def test_failed_report_cannot_become_passing_composition(status):
    artifacts,parent,binding,_=fixture(status)
    with pytest.raises(ValueError,match='REJECTED'):collect_render_parent_reports(artifacts,(parent,),binding)


def test_rejects_same_version_with_substituted_source_and_conflicting_pins():
    artifacts,parent,binding,report=fixture()
    report.sources=[binding.model_copy(update={'canvas_hash':'c'*64})]
    with pytest.raises(ValueError,match='SOURCE_CHANGED'):collect_render_parent_reports(artifacts,(parent,),binding)
    report.sources=[binding]
    other=SimpleNamespace(report=parent.report.model_copy(update={'sha256':'d'*64}))
    with pytest.raises(ValueError,match='PIN_CHANGED'):collect_render_parent_reports(artifacts,(parent,other),binding)


def test_optional_parent_roots_preserve_legacy_wire_and_reject_duplicates_or_null():
    import json
    _,parent,_,report=fixture()
    assert 'parent_reports' not in json.loads(canonical_bytes(report))
    body=json.loads(canonical_bytes(report));body['parent_reports']=[parent.report.model_dump(mode='json')]
    decoded=ImageReport.model_validate_json(json.dumps(body))
    assert decoded.parent_reports==[parent.report]
    for invalid in (None,[parent.report.model_dump(mode='json')]*2):
        body['parent_reports']=invalid
        with pytest.raises(ValueError):ImageReport.model_validate_json(json.dumps(body))


def test_nested_reports_inherit_review_reject_cycles_and_enforce_budgets():
    artifacts,parent,binding,root=fixture('pass')
    child_ref=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    child=root.model_copy(deep=True);child.status='review'
    root.parent_reports=[child_ref]
    documents={parent.report.artifact_id:root,child_ref.artifact_id:child};reads=[]
    artifacts.contract=lambda reference,model:(reads.append(reference),documents[reference.artifact_id])[1]
    evidence=collect_render_parent_reports(artifacts,(parent,parent),binding)
    assert evidence.review_required and evidence.reports==(parent.report,) and len(reads)==2
    child.status='fail'
    with pytest.raises(ValueError,match='REJECTED'):collect_render_parent_reports(artifacts,(parent,),binding)
    child.status='pass';child.parent_reports=[parent.report]
    with pytest.raises(ValueError,match='CYCLE'):collect_render_parent_reports(artifacts,(parent,),binding)
    child.parent_reports=[]
    with pytest.raises(ValueError,match='NODE_BUDGET'):collect_render_parent_reports(artifacts,(parent,),binding,max_reports=1)
    with pytest.raises(ValueError,match='DEPTH_BUDGET'):collect_render_parent_reports(artifacts,(parent,),binding,max_depth=1)


def test_existing_layer_compose_uses_nested_quality_and_exact_source_binding():
    from photo_worker.operations.layers.reports import parent_review
    artifacts,parent,binding,root=fixture('pass')
    child_ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    child=root.model_copy(deep=True);child.status='review';root.parent_reports=[child_ref]
    artifacts.contract=lambda ref,model:root if ref==parent.report else child
    assert parent_review(artifacts,None,parent,binding)
    child.sources=[binding.model_copy(update={'canvas_hash':'f'*64})]
    with pytest.raises(ValueError,match='LAYER_PARENT_REPORT_SOURCE_INVALID'):
        parent_review(artifacts,None,parent,binding)


def test_shared_ancestor_depth_budget_counts_longer_cached_path():
    artifacts,parent,binding,root=fixture('pass')
    refs=[ArtifactRef(artifact_id=uuid4(),sha256=str(n)*64) for n in (1,2,3)]
    first=root.model_copy(deep=True);middle=root.model_copy(deep=True);leaf=root.model_copy(deep=True)
    first.parent_reports=[refs[2]];middle.parent_reports=[refs[0]]
    root.parent_reports=[refs[0],refs[1]]
    documents={parent.report.artifact_id:root,refs[0].artifact_id:first,refs[1].artifact_id:middle,refs[2].artifact_id:leaf}
    artifacts.contract=lambda reference,model:documents[reference.artifact_id]
    with pytest.raises(ValueError,match='DEPTH_BUDGET'):
        collect_render_parent_reports(artifacts,(parent,),binding,max_depth=3)
    assert not collect_render_parent_reports(artifacts,(parent,),binding,max_depth=4).review_required
