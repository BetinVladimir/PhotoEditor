from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime import restore_preview_source as m


@pytest.mark.parametrize('mode',['valid','authority','missing','scope','busy','activity_false','post_read_false'])
def test_preview_source_readonly_catalogue_binds_version_project_sha(mode):
    tenant=uuid4();binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64)
    queries=[];checks=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.INTRANS if mode=='busy' else psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT ar.bucket'):
                assert args==(tenant,binding.asset_version_id,binding.project_id,binding.source_artifact_id,binding.source_sha256)
                assert 'ar.sha256=v.content_hash' in sql and "ar.output_role='working_image'" in sql
                row=None if mode=='missing' else ('artifacts','tenants/'+str(uuid4() if mode=='scope' else tenant)+'/source','v1','a'*64,512)
            else:row=None
            return NS(fetchone=lambda:row)
    def active():
        checks.append(True)
        return False if mode=='activity_false' or mode=='post_read_false' and len(checks)>=2 else None
    def invoke():return m.read_preview_source_object(Connection(),tenant,binding,expected_database='restore',check_active=active)
    if mode=='valid':assert invoke().sha256==binding.source_sha256
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='activity_false':assert queries==[]
    if mode=='post_read_false':assert any(q.startswith('SELECT ar.bucket') for q in queries)
