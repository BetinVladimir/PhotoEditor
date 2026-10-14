from contextlib import contextmanager
from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.runtime import restore_render_input_array as m


@pytest.mark.parametrize('mode',['valid','wrong_ref','wrong_source_sha','foreign_database','missing_lineage','late_version','fixture_false','activity_false'])
def test_reused_array_requires_exact_physical_source_document_and_registered_inputs(monkeypatch,mode):
    tenant=uuid4();ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    image=WorkingImage(pixels=ref,alpha=None,dtype='float32',canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    raw=canonical_bytes(image)
    binding=m.VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='0'*64 if mode=='wrong_source_sha' else sha256(raw).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    result=m.RestoreBatchResultEvidence('restore-batch-result/v1','isolated',uuid4(),uuid4(),uuid4(),1,7,m.RestoreObject('photos','root','v1','a'*64,128))
    if mode=='wrong_ref':ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    reads=[];verified=[];queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,params=None):
            queries.append(q)
            row=None
            if q.startswith('SELECT current_database'):row=('other' if mode=='foreign_database' else 'isolated',True)
            if q.startswith('SELECT a.bucket'):
                reads.append(True)
                assert params[-3:]==(binding.source.artifact_id,binding.source.sha256,binding.asset_version_id)
                row=None if mode=='missing_lineage' else ('photos','tenants/'+str(tenant)+'/pixels','v2' if mode=='late_version' and len(reads)>1 else 'v1',ref.sha256,152)
            return NS(fetchone=lambda:row)
    monkeypatch.setattr(m,'verify_restore_render_array',lambda *a,**k:verified.append(True) or 'verified')
    def run():return m.verify_fixture_source_array(Connection(),object(),('photos',),tenant,result,binding,raw,ref,'pixels',
        assert_fixture_scope=lambda *a:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run()=='verified' and len(reads)==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('wrong_ref','wrong_source_sha','foreign_database','missing_lineage'):assert not verified
    if mode in ('fixture_false','activity_false'):assert queries==[] and verified==[]
