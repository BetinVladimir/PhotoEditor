"""Narrow cancellation upgrade preserves existing authority and unrelated ledgers."""
from pathlib import Path
from uuid import uuid4
import pytest
import psycopg

ROOT=Path(__file__).resolve().parents[2]

@pytest.mark.migration_head(2586)
def test_training_cancel_upgrade_preserves_function_authority_and_ledgers(db):
    signature='photo.finalize_run_cancellation()'
    def catalog():
        return db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,pg_get_functiondef(oid) FROM pg_proc WHERE oid=%s::regprocedure',(signature,)).fetchone()
    before=catalog()
    authority=db.execute("SELECT relname,relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid IN ('photo.quota_reservations'::regclass,'photo.tenant_usage'::regclass,'photo.output_uploads'::regclass,'photo.backend_documents'::regclass) ORDER BY relname").fetchall()
    db.execute((ROOT/'backend/db/migrations/02587_v23_cancelled_training_quota_settlement.sql').read_text().split('-- +goose Down',1)[0])
    after=catalog()
    assert after[:5]==before[:5]
    assert '-- 02587:' in after[5]
    assert 'typed_outcome:=photo.finalize_v2_run(r.tenant_id,r.id)' in after[5]
    assert db.execute("SELECT relname,relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid IN ('photo.quota_reservations'::regclass,'photo.tenant_usage'::regclass,'photo.output_uploads'::regclass,'photo.backend_documents'::regclass) ORDER BY relname").fetchall()==authority
    assert db.execute('SELECT photo.v23_training_cancellation_scope(%s,%s)',(uuid4(),uuid4())).fetchone()==(False,)
    helper=db.execute("SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid='photo.v23_training_cancellation_scope(uuid,uuid)'::regprocedure").fetchone()
    assert helper[0:2]==('photo_fn_owner',True)
    assert any(value.replace(' ','')=='search_path=pg_catalog,photo' for value in helper[2])
    for role in ('photo_api','photo_worker','photo_scheduler','photo_maintenance','photo_portal','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.v23_training_cancellation_scope(uuid,uuid)','EXECUTE')",(role,)).fetchone()==(False,)
    assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(0,)

@pytest.mark.migration_head(2586)
@pytest.mark.parametrize('anchor',[
 'DECLARE candidate uuid; r photo.runs%ROWTYPE; next_cursor bigint;',
 ' SELECT * INTO r FROM photo.runs WHERE id=candidate FOR UPDATE SKIP LOCKED;',
 " IF NOT FOUND OR NOT r.cancel_requested OR r.status<>'cancelling' THEN RETURN 0;END IF;",
 ' INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(r.tenant_id,0)',
])
def test_training_cancel_upgrade_refuses_each_anchor_drift_atomically(db,anchor):
    definition=db.execute("SELECT pg_get_functiondef('photo.finalize_run_cancellation()'::regprocedure)").fetchone()[0]
    assert definition.count(anchor)==1
    db.execute(definition.replace(anchor,anchor+' /* '+anchor+' */'))
    before=db.execute("SELECT pg_get_functiondef('photo.finalize_run_cancellation()'::regprocedure)").fetchone()[0]
    with pytest.raises(psycopg.errors.RaiseException,match='V23_TRAINING_CANCEL_FINALIZER_SOURCE_SHAPE'):
        with db.transaction():
            db.execute((ROOT/'backend/db/migrations/02587_v23_cancelled_training_quota_settlement.sql').read_text().split('-- +goose Down',1)[0])
    assert db.execute("SELECT to_regprocedure('photo.v23_training_cancellation_scope(uuid,uuid)')").fetchone()==(None,)
    assert db.execute("SELECT pg_get_functiondef('photo.finalize_run_cancellation()'::regprocedure)").fetchone()[0]==before
