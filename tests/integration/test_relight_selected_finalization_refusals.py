"""Authored SQL refusals; no successful qualification or physical quality claim."""
import pytest
from relight_review_publication_fixture import relight_publication_fixture, publish_relight_review


@pytest.mark.parametrize('case', ['pending', 'cancelled_run', 'cancelled_task'])
def test_relight_unselected_run_is_never_completion_ready(db, case):
    f = relight_publication_fixture(db)
    assert publish_relight_review(db, f) is not None
    if case == 'cancelled_run':
        db.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (f['tenant'], f['run']))
    elif case == 'cancelled_task':
        db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (f['tenant'], f['task']))
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(f['tenant']),))
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT photo.v28_relight_selected_run_ready(%s,%s)', (f['tenant'], f['run'])).fetchone()[0] is False
        assert db.execute('SELECT photo.v28_relight_review_completion_ready(%s,%s)', (f['tenant'], f['task'])).fetchone()[0] is False
    assert db.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s', (f['tenant'], f['run'])).fetchone()[0] == 'waiting_review'
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='v2.run.completed' AND payload->>'run_id'=%s", (f['tenant'], str(f['run']))).fetchone()[0] == 0


@pytest.mark.parametrize('role', ['photo_api', 'photo_worker', 'photo_scheduler', 'public'])
def test_relight_completion_is_scheduler_only(db, role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.finalize_v28_relight_selected_run()','EXECUTE')", (role,)).fetchone()[0] is (role == 'photo_scheduler')
    for signature in ['photo.v28_relight_selected_run_ready(uuid,uuid)', 'photo.v28_relight_review_completion_ready(uuid,uuid)']:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False
