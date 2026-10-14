"""Semantic success cannot bypass deterministic plan assembly."""
from uuid import uuid4
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_finalizer_waits_for_assembly_and_keeps_private_core(db):
    tid, assembly, result, _, _, _ = fixture(db)
    run = db.execute('SELECT run_id FROM photo.tasks WHERE id=%s', (assembly,)).fetchone()[0]
    # Simulate the initial graph containing adopted transfers only. Keep the
    # unused assembly row in this fixture under a different node identity.
    db.execute("UPDATE photo.tasks SET node_key='fixture.other:0' WHERE id=%s", (assembly,))
    # Metadata fixture only: seed the marker to isolate finalization from typed
    # adoption. The predecessor demonstrably completes this incomplete graph.
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())", (tid, assembly, result))
    with db.transaction(force_rollback=True):
        assert db.execute('SELECT photo.finalize_v2_run_pre_semantic(%s,%s)', (tid, run)).fetchone()[0] == 'completed'
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tid, run)).fetchone()[0] == 'pending'
        assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (uuid4(), run)).fetchone()[0] == 'stale'
    finally:
        db.execute('RESET ROLE')
    assert db.execute('SELECT status FROM photo.runs WHERE id=%s', (run,)).fetchone()[0] == 'running'
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.tasks SET node_key='v2.semantic.assemble:0' WHERE id=%s", (assembly,))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tid, run)).fetchone()[0] == 'completed'
        db.execute('RESET ROLE')
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (run,))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tid, run)).fetchone()[0] == 'cancelled'
        db.execute('RESET ROLE')
    for role in ('photo_scheduler', 'photo_api', 'photo_worker', 'public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.finalize_v2_run_pre_semantic(uuid,uuid)','EXECUTE')", (role,)).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.finalize_v2_run(uuid,uuid)','EXECUTE')", (role,)).fetchone()[0]
