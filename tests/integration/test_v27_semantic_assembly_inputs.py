"""All-or-nothing dynamic CPU assembly inputs under current producer authority."""
from uuid import uuid4
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_expansion_inputs_require_complete_adopted_live_set(db):
    tid,assembly,result,producer,producer_result,candidate,scope=fixture(db,snapshot_authority=True)
    run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(assembly,)).fetchone()[0]
    params=db.execute('SELECT params_artifact_id FROM photo.tasks WHERE id=%s',(producer,)).fetchone()[0]
    def read(tenant=tid):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT * FROM photo.v27_semantic_assembly_inputs(%s,%s)',(tenant,run)).fetchall()
        finally:db.execute('RESET ROLE')
    expected=[(0,producer,candidate,'9'*64)]
    assert read()==expected
    assert read(uuid4())==[]
    for query,args in [
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(producer,)),
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(producer,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(params,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(candidate,)),
        ("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(tid,producer,producer_result)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(query,args)
            assert read()==[]
    assert read()==expected
    signature='photo.v27_semantic_assembly_inputs(uuid,uuid)'
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]
