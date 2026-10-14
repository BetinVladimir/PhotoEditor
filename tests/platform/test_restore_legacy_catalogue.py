from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime import restore_legacy_catalogue as m


@pytest.mark.parametrize('mode',['valid','authority','receipt','registration','config_hash','config_binding','scope_false','activity_false'])
def test_legacy_registration_requires_saved_request_and_one_item_source_lineage(mode):
    target=m.RestoreLegacyApplyTarget('https://restore.test',uuid4(),uuid4(),uuid4(),uuid4(),7,'fresh','Bearer private')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64)
    request=m.legacy_apply_request_body(target);run,output,snapshot=uuid4(),uuid4(),uuid4()
    response=canonical_bytes({'run_id':str(run),'output_collection_id':str(output),'status':'queued'})
    ticket=m.RestoreLegacyApplyTicket(run,output,sha256(request).hexdigest(),sha256(response).hexdigest())
    fingerprint=sha256(canonical_bytes({'source_collection_id':str(target.collection_id),'request':parse_json(request)})).hexdigest()
    config={'pipeline':'apply','renderer_version':'photo_renderer_v2','source_collection_id':str(target.collection_id),
        'source_revision':7,'input_snapshot_id':str(snapshot),'style_version_id':str(target.style_version_id),
        'scope':'all' if mode=='config_binding' else 'selected','identity_style':False,'strength':1}
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT request_hash'):row=(fingerprint,200 if mode=='receipt' else 202,response,run)
            elif sql.startswith('SELECT r.input_snapshot_id'):row=None if mode=='registration' else (snapshot,config,
                'f'*64 if mode=='config_hash' else sha256(canonical_bytes(config)).hexdigest())
            else:row=None
            return NS(fetchone=lambda:row)
    def run_test():return m.observe_fixture_legacy_apply_registration(Connection(),target,binding,ticket,
        expected_database='restore',
        assert_fixture_scope=lambda *a:False if mode=='scope_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run_test().snapshot_id==snapshot
    else:
        with pytest.raises((ValueError,RuntimeError)):run_test()
    if mode in ('scope_false','activity_false'):assert queries==[]
