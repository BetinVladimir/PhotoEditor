"""Final dispatch fence with a controlled predecessor; not full dispatch evidence."""
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_task_leases import queued_task


def controlled_dispatch(db, *, enabled=True, backend='cpu'):
    item = queued_task(db)
    db.execute("UPDATE photo.tasks SET operation='image.relight',operation_version='1',resource_class='cpu',execution_profile=%s WHERE id=%s",
        (Jsonb({'backend':backend}),item['task']))
    db.execute("UPDATE photo.operation_registry SET v2_enabled=%s WHERE operation='image.relight' AND version='1'",(enabled,))
    db.execute("""CREATE OR REPLACE FUNCTION photo.reserve_dispatch_before_v2_activation_guard(
    p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,p_event uuid,p_correlation uuid,p_trace text)
    RETURNS TABLE(outcome text,command jsonb)
    LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
    BEGIN
     UPDATE photo.tasks SET attempt=attempt+1 WHERE id=p_task;
     RETURN QUERY SELECT p_trace,'{"test_delegate":true}'::jsonb;
    END $$""")
    return item


def dispatch(db,item,outcome='queued'):
    return db.execute('SELECT * FROM photo.reserve_dispatch(%s,%s,1,0,1,%s,%s,%s)',
        (item['task'],item['node'],uuid4(),uuid4(),outcome)).fetchone()


def test_disabled_v2_dispatch_rolls_back_predecessor_mutations(db):
    item = controlled_dispatch(db,enabled=False)
    before = db.execute('SELECT attempt FROM photo.tasks WHERE id=%s',(item['task'],)).fetchone()
    with pytest.raises(errors.SerializationFailure,match='DISPATCH_ACTIVATION_STALE'):
        with db.transaction(): dispatch(db,item)
    assert db.execute('SELECT attempt FROM photo.tasks WHERE id=%s',(item['task'],)).fetchone() == before


def test_enabled_dispatch_preserves_command(db):
    item = controlled_dispatch(db)
    assert dispatch(db,item) == ('queued',{'test_delegate':True})


def test_backend_mismatch_rolls_back_dispatch(db):
    item = controlled_dispatch(db,backend='cuda')
    with pytest.raises(errors.SerializationFailure,match='DISPATCH_ACTIVATION_STALE'):
        with db.transaction(): dispatch(db,item)


@pytest.mark.parametrize('outcome',['stale','waiting_resource','waiting_dependency'])
def test_non_queued_outcomes_remain_unchanged(db,outcome):
    item = controlled_dispatch(db,enabled=False)
    assert dispatch(db,item,outcome) == (outcome,{'test_delegate':True})


def test_scheduler_cannot_call_private_predecessor(db):
    assert db.execute("SELECT has_function_privilege('photo_scheduler',"
        "'photo.reserve_dispatch_before_v2_activation_guard(uuid,text,bigint,bigint,bigint,uuid,uuid,text)','EXECUTE')").fetchone() == (False,)
