from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_legacy_preview_catalogue as m


@pytest.mark.parametrize('mode',['valid','authority','receipt','registration','config','params','inputs','request','version','fixture_false','activity_false'])
def test_preview_registration_binds_saved_request_source_and_exact_interactive_task(mode):
    version=uuid4()
    target=m.RestoreLegacyPreviewTarget('https://restore.test',uuid4(),version,3,
        canonical_bytes({'strength':1}),'fresh','Bearer private')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4() if mode=='version' else version,uuid4(),'a'*64)
    run,snapshot,task=uuid4(),uuid4(),uuid4()
    request=m.legacy_preview_request_body(target)
    response=canonical_bytes({'run_id':str(run),'status':'queued'})
    digest=lambda v:sha256(canonical_bytes(v)).hexdigest()
    ticket=m.RestoreLegacyPreviewTicket(run,'f'*64 if mode=='request' else sha256(request).hexdigest(),sha256(response).hexdigest())
    config={'schema_version':1,'explicit_generation':False,'asset_version_id':str(version),'draft_revision':3,
        'style_version_id':None,'scale':'proxy','roi':None}
    if mode=='config':config['draft_revision']=4
    source={'artifact_id':str(binding.source_artifact_id),'sha256':binding.source_sha256}
    params={'image':source,'style':None,'modules':{'strength':2 if mode=='params' else 1},'scale':'proxy','roi':None}
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT request_hash'):row=(sha256(request).hexdigest(),200 if mode=='receipt' else 202,response,run)
            elif sql.startswith('SELECT r.input_snapshot_id'):
                assert "t.operation='editor.preview'" in sql and 'v.content_hash=ar.sha256' in sql
                assert args[0]==version and args[-1]==binding.source_sha256
                row=None if mode=='registration' else (snapshot,config,digest(config),task,params,digest(params),[] if mode=='inputs' else [source])
            else:row=None
            return NS(fetchone=lambda:row)
    def invoke():return m.observe_fixture_legacy_preview_registration(Connection(),target,binding,ticket,
        expected_database='restore',
        assert_fixture_scope=lambda *a:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert invoke().task_id==task
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode in ('fixture_false','activity_false'):assert queries==[]
