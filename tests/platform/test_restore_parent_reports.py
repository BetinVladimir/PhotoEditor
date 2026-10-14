from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.contracts import ImageReport
from photo_worker.v2.render_reports import collect_render_parent_reports
from photo_worker.runtime.restore_parent_reports import RestoreParentReportReader
from photo_worker.runtime.restore_batch_result import RestoreBatchResultEvidence
from photo_worker.runtime.restore_inventory import RestoreObject


@pytest.mark.parametrize('mode',['valid','review','failed','wrong_bytes','changed_catalogue','cycle','fixture_false','activity_false'])
def test_real_parent_report_codec_and_dag_inherit_review_and_reject_invalid_bytes(monkeypatch,mode):
    tenant=uuid4();binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)
    report=ImageReport(schema_version=1,status='review' if mode=='review' else 'fail' if mode=='failed' else 'pass',
        operation='test.adjust',skipped_count=0,reason=None,outside_mask_max_error=0,clipping_fraction=0,
        identity=None,transform=None,sources=[binding],registrations=[])
    identity=uuid4()
    # Cycle walker is independently exercised with a pinned reader to avoid a self-referential SHA fixture.
    raw=canonical_bytes(report);ref=ArtifactRef(artifact_id=identity,sha256=sha256(raw).hexdigest())
    obj=RestoreObject('photos','tenants/'+str(tenant)+'/report','v1',ref.sha256,len(raw))
    result=RestoreBatchResultEvidence('restore-batch-result/v1','isolated',uuid4(),uuid4(),uuid4(),1,7,obj)
    requests=[]
    class Client:
        def get_object(self,*args,**kwargs):
            requests.append(True)
            parts=[b'x'*len(raw) if mode=='wrong_bytes' else raw,b'']
            return NS(status=200,headers={'x-amz-version-id':'v1','content-length':str(len(raw))},
                read=lambda *a,**k:parts.pop(0),close=lambda:None,release_conn=lambda:None)
    reader=RestoreParentReportReader(object(),Client(),('photos',),tenant,result,
        assert_fixture_scope=lambda *a:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    reads=[]
    def catalogue(actual,contract='ImageReport'):
        assert actual==ref;reads.append(True)
        return RestoreObject(obj.bucket,obj.key,'v2',obj.sha256,obj.size) if mode=='changed_catalogue' and len(reads)>1 else obj
    monkeypatch.setattr(reader,'catalogue',catalogue)
    if mode=='cycle':
        monkeypatch.setattr(reader,'contract',lambda *a:report.model_copy(update={'parent_reports':[ref]}))
    def run():return collect_render_parent_reports(reader,(NS(report=ref),),binding)
    if mode in ('valid','review'):assert run().review_required==(mode=='review')
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):assert requests==[]
