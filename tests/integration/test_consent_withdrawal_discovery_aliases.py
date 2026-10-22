"""Real scheduler calls and upgrade metadata; no photographic consent fixture claim."""
from pathlib import Path
from uuid import uuid4
import pytest

ROOT=Path(__file__).resolve().parents[2]
SIGNATURES=('photo.withdraw_completed_glare_consent()',
            'photo.withdraw_completed_open_eyes_consent()')


def _catalog(db):
    return {name:db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,pg_get_functiondef(oid) FROM pg_proc WHERE oid=%s::regprocedure',(name,)).fetchone() for name in SIGNATURES}


def _scheduler_empty(db):
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        for name in SIGNATURES:
            # Execute the discovery SELECT even with no candidate. This was
            # SQL42702 before the fix; inspecting pg_proc alone missed it.
            assert db.execute('SELECT '+name).fetchone()==(False,)
    finally:
        db.execute('RESET ROLE')


def test_current_empty_scheduler_consent_maintenance_is_callable(db):
    _scheduler_empty(db)


@pytest.mark.migration_head(2576)
def test_upgrade_preserves_function_identity_and_only_disambiguates_discovery(db):
    before=_catalog(db)
    source=(ROOT/'backend/db/migrations/02577_consent_withdrawal_discovery_aliases.sql').read_text().split('-- +goose Down',1)[0]
    db.execute(source)
    after=_catalog(db)
    for name in SIGNATURES:
        assert after[name][:5]==before[name][:5]
        assert 'photo.tasks candidate_task JOIN photo.runs candidate_run' in after[name][5]
        assert 'FROM photo.tasks t JOIN photo.runs r' not in after[name][5]
        old,new=before[name][5],after[name][5]
        start=old.index(' SELECT t.tenant_id,t.run_id,t.id,t.canonical_result_id INTO candidate')
        end=old.index('\n IF NOT FOUND THEN RETURN false;END IF;',start)
        old_fragment=old[start:end]
        new_fragment=old_fragment.replace('photo.tasks t JOIN photo.runs r','photo.tasks candidate_task JOIN photo.runs candidate_run').replace('t.','candidate_task.').replace('r.','candidate_run.')
        assert new==old.replace(old_fragment,new_fragment)
    _scheduler_empty(db)


def test_positive_cancellation_drain_survives_empty_optional_consent_families(db):
    user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(str(uuid4())+'@test.example',)).fetchone()
    tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'input','user',%s,%s,0) RETURNING id",(tenant,user,'a'*64)).fetchone()[0]
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,cancel_requested,failure_policy) VALUES(%s,'v2','cancelling',%s,'{}',%s,%s,true,'fail_fast') RETURNING id",(tenant,snapshot,'a'*64,user)).fetchone()[0]
    _scheduler_empty(db)
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(1,)
        assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(0,)
    finally:
        db.execute('RESET ROLE')
    assert db.execute('SELECT status,cancel_requested,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()==('cancelled',True,True)
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='run.cancelled'",(tenant,run)).fetchone()==(1,)
