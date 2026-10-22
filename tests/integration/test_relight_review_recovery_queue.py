"""Real SQL queue discovery for legacy adopted relight tasks; not quality proof."""
from uuid import uuid4
import pytest
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_adoption_source import complete_relight_task


@pytest.mark.parametrize('case', ['legacy', 'cancelled_task', 'cancelled_run', 'completed', 'existing_review'])
def test_legacy_relight_review_recovery_discovery(db, case):
    args, scope, _ = relight_lease_fixture(db)
    tenant, run, task = args[:3]
    result, _ = complete_relight_task(db, args, review_required=True)
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())", (tenant, task, result))
    if case == 'cancelled_task':
        db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, task))
    elif case == 'cancelled_run':
        db.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, run))
    elif case == 'completed':
        db.execute("UPDATE photo.runs SET status='completed' WHERE tenant_id=%s AND id=%s", (tenant, run))
    elif case == 'existing_review':
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute("INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons,relight_params_jcs) VALUES(%s,%s,%s,%s,%s,%s,'image.relight','[]','{}')",
                       (tenant, run, task, scope['source'], scope['source'], scope['source_sha']))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        rows = db.execute('SELECT * FROM photo.v2_pending_adoptions()').fetchall()
    assert ((tenant, run, task) in rows) is (case == 'legacy')
    assert len(rows) <= 64


@pytest.mark.parametrize('role', ['public', 'photo_api', 'photo_worker', 'photo_scheduler'])
def test_legacy_queue_wrapper_privileges(db, role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v2_pending_adoptions_pre_relight_review_recovery()','EXECUTE')", (role,)).fetchone()[0] is False
    assert db.execute("SELECT has_function_privilege(%s,'photo.v2_pending_adoptions()','EXECUTE')", (role,)).fetchone()[0] is (role == 'photo_scheduler')
