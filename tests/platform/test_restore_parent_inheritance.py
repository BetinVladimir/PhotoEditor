from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.contracts import ImageReport
from photo_worker.runtime import restore_parent_reports as m
from photo_worker.v2 import render_adoption_inputs as inputs
from photo_worker.v2 import render_reports as reports


@pytest.mark.parametrize('mode',['empty','review','dropped_parent','lost_review','changed_roots'])
def test_canonical_parent_roots_and_review_are_required_in_output_report(monkeypatch,mode):
    tenant=uuid4();ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    binding=VersionBinding(asset_version_id=uuid4(),source=ref,canvas_hash='b'*64)
    parents=() if mode=='empty' else (ref,)
    report=ImageReport(schema_version=1,status='pass' if mode in ('empty','lost_review') else 'review',operation='render.compose',
        skipped_count=0,reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[binding],registrations=[],
        **({} if mode=='empty' else {'parent_reports':[] if mode=='dropped_parent' else [ref]}))
    obj=m.RestoreObject('photos','root','v1','a'*64,128)
    result=m.RestoreBatchResultEvidence('restore-batch-result/v1','isolated',uuid4(),uuid4(),uuid4(),1,7,obj);reads=[]
    monkeypatch.setattr(inputs,'load_render_adoption_inputs',lambda *a:reads.append(True) or NS(
        parent_roots=() if mode=='changed_roots' and len(reads)>1 else parents))
    monkeypatch.setattr(m.RestoreParentReportReader,'contract',lambda *a:NS(report=ref))
    monkeypatch.setattr(reports,'collect_render_parent_reports',lambda *a,**k:NS(reports=parents,review_required=bool(parents)))
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,*a):return NS(fetchone=lambda:('isolated',True))
    def run():return m.verify_fixture_parent_reports(Connection(),NS(get_object=lambda *a:None),('photos',),tenant,result,binding,
        canonical_bytes(report),assert_fixture_scope=lambda *a:None,check_active=lambda:None)
    if mode in ('empty','review'):assert run().review_required==bool(parents) and len(reads)==2
    else:
        with pytest.raises(ValueError):run()
