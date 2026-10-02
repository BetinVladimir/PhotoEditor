"""SQL scheduling bounds over controlled batch metadata; no rendering qualification."""
from uuid import uuid4
import psycopg
import pytest
from test_v26_render_finalizer_tenant_scope import seed_failed_batch


def stage_incomplete(db):
    tenant,batch=seed_failed_batch(db)
    child=db.execute("SELECT id FROM photo.runs WHERE tenant_id=%s AND kind='v2'",(tenant,)).fetchone()[0]
    db.execute("UPDATE photo.runs SET status='draft',graph_sealed=false WHERE id=%s",(child,))
    db.execute("UPDATE photo.tasks SET state='blocked',terminal_reason=NULL WHERE tenant_id=%s",(tenant,))
    return tenant,batch


def record(db,tenant,batch,failed):
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:return db.execute('SELECT photo.record_v26_render_recovery(%s,%s,%s)',(tenant,batch,failed)).fetchone()[0]
    finally:db.execute('RESET ROLE')


def set_failures(db,tenant,batch,failures):
    db.execute('SET LOCAL ROLE photo_fn_owner')
    try:db.execute('UPDATE photo.runs SET v26_input_recovery_failures=%s WHERE tenant_id=%s AND id=%s',(failures,tenant,batch))
    finally:db.execute('RESET ROLE')


@pytest.mark.parametrize('failures,seconds',[(0,5),(1,10),(2,20),(3,40),(4,80),(5,160),(6,300),(2147483647,300)])
def test_failure_delay_caps_and_counter_saturates(db,failures,seconds):
    tenant,batch=stage_incomplete(db)
    set_failures(db,tenant,batch,failures)
    before=db.execute('SELECT revision,config_hash FROM photo.runs WHERE id=%s',(batch,)).fetchone()
    assert record(db,tenant,batch,True) is True
    count,delay=db.execute('SELECT v26_input_recovery_failures,extract(epoch FROM v26_input_recovery_after-clock_timestamp()) FROM photo.runs WHERE id=%s',(batch,)).fetchone()
    assert count==min(failures+1,2147483647)
    assert seconds-2<=delay<=seconds
    assert db.execute('SELECT revision,config_hash FROM photo.runs WHERE id=%s',(batch,)).fetchone()==before
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT batch_id FROM photo.next_v26_render_recovery(NULL)').fetchall()==[]
    db.execute('RESET ROLE')


def test_false_success_and_foreign_tenant_do_not_change_schedule(db):
    tenant,batch=stage_incomplete(db)
    assert record(db,tenant,batch,True)
    before=db.execute('SELECT v26_input_recovery_after,v26_input_recovery_failures FROM photo.runs WHERE id=%s',(batch,)).fetchone()
    assert record(db,tenant,batch,False) is False
    assert record(db,str(uuid4()),batch,True) is False
    assert db.execute('SELECT v26_input_recovery_after,v26_input_recovery_failures FROM photo.runs WHERE id=%s',(batch,)).fetchone()==before


@pytest.mark.parametrize('state,cancel',[('completed',False),('failed',False),('cancelled',True),('cancelling',True),('draft',True)])
def test_terminal_or_cancelled_parent_schedule_is_unchanged(db,state,cancel):
    tenant,batch=stage_incomplete(db)
    db.execute('UPDATE photo.runs SET status=%s,cancel_requested=%s WHERE id=%s',(state,cancel,batch))
    assert record(db,tenant,batch,True) is False
    assert db.execute('SELECT v26_input_recovery_after,v26_input_recovery_failures FROM photo.runs WHERE id=%s',(batch,)).fetchone()==(None,0)


@pytest.mark.parametrize('column,value',[('v26_input_recovery_failures','1'),('v26_input_recovery_after',"clock_timestamp()+interval '1 hour'")])
def test_api_cannot_insert_nondefault_scheduling_fields(db,column,value):
    tenant,batch=stage_incomplete(db)
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
    with pytest.raises(psycopg.errors.InsufficientPrivilege,match='V26_RECOVERY_SCHEDULE_PRIVATE'),db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute('INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,'+column+') SELECT tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,'+value+' FROM photo.runs WHERE id=%s',(batch,))


def test_record_function_is_scheduler_only(db):
    for role in ('photo_api','photo_worker','photo_gateway','photo_scheduler'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.record_v26_render_recovery(uuid,uuid,boolean)','EXECUTE')",(role,)).fetchone()==(role=='photo_scheduler',)
