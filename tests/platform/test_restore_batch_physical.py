from contextlib import contextmanager
from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_batch_physical as m
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.contracts.base import ArtifactRef


@pytest.mark.parametrize('mode',['valid','wrong_root','alias','missing_child','foreign_child','changed_result','semantic_failure','late_bytes','scope_false','activity_false'])
def test_physical_render_root_and_direct_children_are_pinned_and_rechecked(monkeypatch,mode):
    tenant=uuid4();root=uuid4();ids=[uuid4() for _ in range(3)];opens={};semantic_calls=[]
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)
    if mode=='alias':ids[1]=ids[0]
    from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
    childraw=canonical_bytes(WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='c'*64),
        canvas=Canvas(width=2,height=3,orientation=1,space_id='source',source_sha256='a'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='d'*64),
        dtype='float32',alpha=None,transform_chain=TransformChain.create([])))
    pin=sha256(childraw).hexdigest()
    body=dict(schema_version=1,working_image=dict(artifact_id=str(ids[0]),sha256=pin),
        report=dict(artifact_id=str(ids[1]),sha256=pin),changed_mask=dict(artifact_id=str(ids[2]),sha256=pin),generated_mask=None)
    raw=canonical_bytes(body);key='tenants/'+str(tenant)+'/root';calls=[]
    evidence=NS(object=RestoreObject('photos',key,'v1',sha256(raw).hexdigest(),len(raw)),artifact_id=root,
        task_id=uuid4(),attempt=1,fence=7)
    def observe(*args,**kwargs):
        calls.append(True)
        return object() if mode=='changed_result' and len(calls)>1 else evidence
    monkeypatch.setattr(m,'observe_fixture_batch_result',observe)
    def semantics(*args,**kwargs):
        assert args==(childraw,childraw,childraw,None) and kwargs['binding'] is binding
        semantic_calls.append(True)
        if mode=='semantic_failure':raise ValueError('invalid report')
        return 'semantics'
    monkeypatch.setattr(m,'validate_restore_render_documents',semantics)
    monkeypatch.setattr(m,'verify_fixture_render_arrays',lambda *a,**k:('arrays',))
    monkeypatch.setattr(m,'verify_fixture_parent_reports',lambda *a,**k:'parents')
    class Client:
        def get_object(self,bucket,k,version_id):
            opens[k]=opens.get(k,0)+1
            data=raw if k==key else childraw
            if mode=='wrong_root' and k==key:data=b'x'*len(raw)
            if mode=='late_bytes' and k!=key and opens[k]>1:data=b'xx'
            chunks=[data,b'']
            return NS(status=200,headers={'x-amz-version-id':'v1','content-length':str(len(data))},
                read=lambda size,decode_content=False:chunks.pop(0),close=lambda:None,release_conn=lambda:None)
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,params=None):
            row=None
            if q.startswith('SELECT current_database'):row=('isolated',True)
            if q.startswith('SELECT a.bucket'):
                assert params[-2]==(params[1]==ids[0])
                row=None if mode=='missing_child' else ('photos','tenants/'+str(uuid4() if mode=='foreign_child' else tenant)+'/'+str(params[1]),'v1',pin,len(childraw))
            return NS(fetchone=lambda:row)
    def run():return m.verify_fixture_batch_physical(Connection(),Client(),('photos',),tenant,NS(database='isolated'),
        binding=binding,source_raw=b'{}',
        assert_fixture_scope=lambda *args:False if mode=='scope_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        result=run();assert result.inventory.objects==3 and len(calls)==2 and result.semantics=='semantics' and semantic_calls==[True]
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('scope_false','activity_false'):assert opens=={} and semantic_calls==[]
