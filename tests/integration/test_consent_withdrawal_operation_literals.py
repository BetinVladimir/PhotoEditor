"""Nonempty scheduler consent withdrawal: real relational gates, no pixel quality claim."""
from pathlib import Path
import pytest
from completed_consent_candidate_fixture import completed_consent_candidate

ROOT=Path(__file__).resolve().parents[2]
MIGRATION=ROOT/'backend/db/migrations/02643_consent_withdrawal_operation_literals.sql'


def _call(db, family):
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        with db.transaction():
            return db.execute('SELECT photo.withdraw_completed_'+family+'_consent()').fetchone()[0]
    finally:
        db.execute('RESET ROLE')


def _loss(db, f, family):
    db.execute('SET LOCAL ROLE photo_fn_owner')
    try:
        return db.execute('SELECT photo.completed_'+family+'_consent_loss(%s,%s,%s)',
                          (f['tenant'],f['task'],f['result'])).fetchone()[0]
    finally:
        db.execute('RESET ROLE')


@pytest.mark.parametrize('family',['glare','open_eyes'])
@pytest.mark.parametrize('withdrawal',['consent','confirmation'])
def test_nonempty_withdrawal_discovers_real_operation_and_settles_once(db,family,withdrawal):
    f=completed_consent_candidate(db,family)
    assert _loss(db,f,family) is None
    assert _call(db,family) is False  # Valid consent must not be cancelled.
    if withdrawal=='consent':
        db.execute("UPDATE photo.consent_grants SET status='revoked' WHERE tenant_id=%s AND id=%s",
                   (f['tenant'],f['consent']))
        code=('GLARE' if family=='glare' else 'OPEN_EYES')+'_CONSENT_REVOKED'
    else:
        db.execute('UPDATE photo.person_observations SET confirmed_by=NULL WHERE tenant_id=%s AND id=%s',
                   (f['tenant'],f['observations'][0]))
        code=('GLARE' if family=='glare' else 'OPEN_EYES')+'_SOURCE_CONFIRMATION_WITHDRAWN'
    # Prove the genuine typed predicate admits this nonempty relational candidate.
    assert _loss(db,f,family)==code
    assert db.execute('SELECT status FROM photo.tenants WHERE id=%s',(f['tenant'],)).fetchone()==('active',)
    assert db.execute('SELECT count(*) FROM photo.tenant_usage WHERE tenant_id=%s',(f['tenant'],)).fetchone()==(1,)
    before=db.execute('SELECT revision FROM photo.runs WHERE id=%s',(f['run'],)).fetchone()[0]
    assert _call(db,family) is True
    column='v28_glare_authority_failure' if family=='glare' else 'v28_donor_authority_failure'
    row=db.execute('SELECT status,cancel_requested,revision,'+column+' FROM photo.runs WHERE id=%s',(f['run'],)).fetchone()
    assert row[:3]==('cancelling',True,before+1)
    assert row[3]==dict(code=code,task_id=str(f['task']),result_id=str(f['result']),attempt=1,fence=1)
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='run.cancelling'",(f['tenant'],f['run'])).fetchone()==(1,)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='run.cancelling' AND payload->>'run_id'=%s",(f['tenant'],str(f['run']))).fetchone()==(1,)
    assert _call(db,family) is False
    assert db.execute('SELECT revision FROM photo.runs WHERE id=%s',(f['run'],)).fetchone()==(before+1,)


@pytest.mark.migration_head(2642)
def test_upgrade_changes_only_discovery_and_invalid_error_assignment_and_preserves_authority_metadata(db):
    signatures=['photo.withdraw_completed_'+f+'_consent()' for f in ('glare','open_eyes')]
    def capture():
        return {s:db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,prolang,prorettype,pronargs,prosrc FROM pg_proc WHERE oid=%s::regprocedure',(s,)).fetchone() for s in signatures}
    before=capture()
    db.execute(MIGRATION.read_text().split('-- +goose Down',1)[0])
    after=capture()
    for signature in signatures:
        assert after[signature][:-1]==before[signature][:-1]
        invalid=",errors=errors||jsonb_build_array(jsonb_build_object('code',code,\n  'message','The original consent or confirmed identity was withdrawn. Start a new request after authorization is restored.'))"
        assert before[signature][-1].count(invalid)==1
        assert after[signature][-1]==before[signature][-1].replace("'portraicandidate_task.","'portrait.").replace(invalid,'')
        assert 'portraicandidate_task.' not in after[signature][-1]
    # Public/API/worker must still be unable to invoke scheduler maintenance.
    for signature in signatures:
        for role in ('photo_api','photo_worker','photo_portal','photo_gateway'):
            assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(False,)
        assert db.execute('SELECT has_function_privilege(\'photo_scheduler\',%s,\'EXECUTE\')',(signature,)).fetchone()==(True,)


@pytest.mark.parametrize('family',['glare','open_eyes'])
def test_revoked_but_unbound_issuer_confirmation_cannot_cancel(db,family):
    f=completed_consent_candidate(db,family,bound_confirmation=False)
    db.execute("UPDATE photo.consent_grants SET status='revoked' WHERE tenant_id=%s AND id=%s",(f['tenant'],f['consent']))
    assert _loss(db,f,family) is None
    before=db.execute('SELECT status,cancel_requested,revision FROM photo.runs WHERE id=%s',(f['run'],)).fetchone()
    assert _call(db,family) is False
    assert db.execute('SELECT status,cancel_requested,revision FROM photo.runs WHERE id=%s',(f['run'],)).fetchone()==before


@pytest.mark.parametrize('family',['glare','open_eyes'])
@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_portal','photo_gateway','photo_maintenance'])
def test_untrusted_roles_cannot_execute_withdrawal(db,family,role):
    import psycopg
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE '+role)  # Closed parametrized test roles only.
            db.execute('SELECT photo.withdraw_completed_'+family+'_consent()')


@pytest.mark.migration_head(2642)
@pytest.mark.parametrize('family',['glare','open_eyes'])
def test_null_proconfig_installer_drift_refuses_before_any_function_change(db,family):
    import psycopg
    # Actual catalog mutation in a rollback-only test; prosrc remains the exact preimage.
    db.execute('ALTER FUNCTION photo.withdraw_completed_'+family+'_consent() RESET ALL')
    assert db.execute('SELECT proconfig FROM pg_proc WHERE oid=%s::regprocedure',('photo.withdraw_completed_'+family+'_consent()',)).fetchone()==(None,)
    before=db.execute("SELECT oid,prosrc,proconfig FROM pg_proc WHERE oid IN('photo.withdraw_completed_glare_consent()'::regprocedure,'photo.withdraw_completed_open_eyes_consent()'::regprocedure) ORDER BY oid").fetchall()
    with pytest.raises(psycopg.errors.RaiseException,match='CONSENT_OPERATION_PREIMAGE_DRIFT'):
        with db.transaction():
            db.execute(MIGRATION.read_text().split('-- +goose Down',1)[0])
    assert db.execute("SELECT oid,prosrc,proconfig FROM pg_proc WHERE oid IN('photo.withdraw_completed_glare_consent()'::regprocedure,'photo.withdraw_completed_open_eyes_consent()'::regprocedure) ORDER BY oid").fetchall()==before
