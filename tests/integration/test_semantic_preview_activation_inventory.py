"""Actual capability projection and isolated final fences; no whole lifecycle PASS.

The predecessor doubles expose rollback at the final wrapper only. Production
preview source/Params/quality authority remains required and is not simulated
as qualified by these scenarios.
"""
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_v2_claim_activation_guard import controlled_claim
from test_v2_dispatch_activation_guard import controlled_dispatch,dispatch
from test_task_leases import queued_task

OP='semantic.preview_local_exposure'

@pytest.mark.parametrize('enabled',[False,True])
def test_preview_alias_capability_requires_its_own_activation(db,enabled):
    item=queued_task(db)
    db.execute("UPDATE photo.operation_registry SET v2_enabled=%s WHERE operation=%s AND version='1'",(enabled,OP))
    db.execute('UPDATE photo.worker_registrations SET resource_classes=%s,backends=%s,qualified_operations=%s WHERE tenant_id=%s AND worker_id=%s',(Jsonb(['cpu']),Jsonb(['cpu']),Jsonb({OP:['cpu']}),item['tenant'],item['worker']))
    db.execute("UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),doctor_report_sha256=%s WHERE node_id=%s",('a'*64,item['node']))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(item['tenant']),))
        assert db.execute('SELECT available_backends FROM photo.tenant_capabilities() WHERE operation=%s',(OP,)).fetchone()==(['cpu'] if enabled else [],)
        db.execute('RESET ROLE')

@pytest.mark.parametrize('operation',[OP,'editor.effects_seal','editor.layers_precompose','capture.prepare'])
@pytest.mark.parametrize('stage',['claim','dispatch'])
@pytest.mark.parametrize('enabled',[False,True])
def test_preview_alias_final_fences_preserve_delegate_or_rollback(db,stage,enabled,operation):
    if stage=='claim':item,command=controlled_claim(db)
    else:item=controlled_dispatch(db)
    db.execute('UPDATE photo.tasks SET operation=%s,resource_class=%s WHERE id=%s',(operation,'gpu' if operation=='capture.prepare' else 'cpu',item['task']))
    db.execute("UPDATE photo.operation_registry SET v2_enabled=%s WHERE operation=%s AND version='1'",(enabled,operation))
    before=db.execute('SELECT attempt FROM photo.tasks WHERE id=%s',(item['task'],)).fetchone()
    def invoke():
        if stage=='claim':return db.execute('SELECT outcome,fence FROM photo.claim_task(%s,1,%s,%s)',(item['task'],item['worker'],Jsonb(command))).fetchone()
        return dispatch(db,item)
    if enabled:
        assert invoke()==(('claimed',7) if stage=='claim' else ('queued',{'test_delegate':True}))
    else:
        with pytest.raises(errors.SerializationFailure,match='ACTIVATION_STALE'):
            with db.transaction():invoke()
        assert db.execute('SELECT attempt FROM photo.tasks WHERE id=%s',(item['task'],)).fetchone()==before
