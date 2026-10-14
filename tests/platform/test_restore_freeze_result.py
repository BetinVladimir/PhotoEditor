from contextlib import contextmanager
from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
from datetime import datetime, timezone
import psycopg
import pytest
from photo_worker.runtime import restore_freeze_result as module
from photo_worker.runtime.restore_revoke_freeze import (
    RestoreFreezeDenialTarget, RestoreFreezeBaselineBinding, RestoreFreezeBaselineEvidence, _freeze_document_id)
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture


@pytest.mark.parametrize('mode', ['valid','foreign_database','unregistered','changed_catalogue',
    'wrong_bytes','wrong_version','wrong_learning','wrong_source','scope_false','activity_false'])
def test_registered_frozen_result_requires_exact_physical_bytes_and_stable_catalogue(monkeypatch, mode):
    f = RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    target = RestoreFreezeDenialTarget('https://restore.test',f.tenant_id,uuid4(),uuid4(),7,'baseline','Bearer secret')
    binding = RestoreFreezeBaselineBinding(f.actor_id,uuid4(),uuid4(),'a'*64,'b'*64)
    raw=b'{}'; pin=sha256(raw).hexdigest(); reads=[]; closed=[]; queries=[]
    result = RestoreFreezeBaselineEvidence('restore-freeze-baseline/v1',_freeze_document_id(target,f.actor_id),
        uuid4(),pin,'c'*64,'d'*64,datetime.now(timezone.utc))
    row=('photos','tenants/'+str(f.tenant_id)+'/frozen.json','v1',pin,len(raw))
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,params=None):
            queries.append(q); value=None
            if q.startswith('SELECT current_database'):value=('foreign' if mode=='foreign_database' else 'isolated',True)
            if q.startswith('SELECT a.bucket'):
                assert params[2]==result.frozen_document_id and params[-1]==str(f.learning_version_id)
                reads.append(True);value=None if mode=='unregistered' else row
                if mode=='changed_catalogue' and len(reads)==2:value=(*row[:2],'v2',*row[3:])
            return NS(fetchone=lambda:value)
    class Client:
        def get_object(self,bucket,key,version_id):
            assert (bucket,key,version_id)==row[:3]
            data=[b'xx' if mode=='wrong_bytes' else raw,b'']
            return NS(status=200,headers={'x-amz-version-id':'v2' if mode=='wrong_version' else 'v1','content-length':'2'},
                read=lambda size,decode_content=False:data.pop(0),close=lambda:closed.append(True),release_conn=lambda:None)
    # This matrix exercises catalogue/physical transport; full decoder has its own contract suite.
    source=NS(asset_version_id=binding.asset_version_id,source=NS(artifact_id=binding.source_artifact_id,
        sha256='e'*64 if mode=='wrong_source' else binding.source_sha256),canvas_hash=binding.canvas_hash)
    monkeypatch.setattr(module,'decode_frozen_render_recipe',lambda body:NS(recipe_sha256='f'*64,
        recipe=NS(source=source,learning_version_id=f.learning_version_id),
        learning=NS(version_id=uuid4() if mode=='wrong_learning' else f.learning_version_id)))
    def run():return module.verify_fixture_frozen_result(Connection(),Client(),('photos',),target,binding,f,result,'f'*64,
        expected_database='isolated',assert_fixture_scope=lambda *args:False if mode=='scope_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().object.version=='v1' and len(reads)==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert not any(q.startswith(('UPDATE','DELETE','INSERT')) for q in queries)
    if mode in ('scope_false','activity_false'):
        assert queries==[] and reads==[] and closed==[]
    elif mode not in ('foreign_database','unregistered'):assert closed==[True]
