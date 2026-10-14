from contextlib import contextmanager
from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_export_memory as m
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.export.memory import presentation_ram_bytes


@pytest.mark.parametrize('mode',['valid','authority','missing','backend','budget','boolean','compatibility','alpha','inactive'])
def test_export_memory_uses_canonical_task_profile_and_production_estimate(mode):
    tenant=uuid4();result=NS(registration=NS(run_id=uuid4()),artifact_id=uuid4(),object=NS(sha256='a'*64))
    source=NS(width=4000,height=3000,alpha=object() if mode=='alpha' else None)
    required=presentation_ram_bytes(4000,3000,4000,3000,alpha=source.alpha is not None)
    profile={'backend':'metal' if mode=='backend' else 'cpu','renderer_version':'v2.2',
        'resource_estimate':{'ram_bytes':True if mode=='boolean' else required-1 if mode=='budget' else required}}
    if mode=='compatibility':profile={'backend':'cpu'}
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT t.execution_profile'):
                assert args==(tenant,result.registration.run_id,result.artifact_id,result.object.sha256)
                assert "t.resource_class='cpu'" in sql and 't.fencing_token=z.fencing_token' in sql
                assert 'a.produced_attempt=z.attempt' in sql and 'z.outputs @>' in sql
                row=None if mode=='missing' else (profile,)
            else:row=None
            return NS(fetchone=lambda:row)
    def invoke():return m.verify_fixture_export_memory(Connection(),tenant,result,source,4000,3000,
        expected_database='restore',check_active=lambda:False if mode=='inactive' else None)
    if mode in ('valid','alpha'):assert invoke()==sha256(canonical_bytes(profile)).hexdigest()
    else:
        with pytest.raises((ValueError,RuntimeError,OperationFailure)):invoke()
    if mode=='inactive':assert not calls
