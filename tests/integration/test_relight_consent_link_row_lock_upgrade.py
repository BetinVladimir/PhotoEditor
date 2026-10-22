"""Relational row-lock authority regression; model/output receipts are synthetic."""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from time import monotonic, sleep
from uuid import uuid4
import psycopg
from psycopg import errors
import pytest
from test_relight_recorded_subject_consents import person_fixture
from test_v28_relight_adoption_source import complete_relight_task

ROOT=Path(__file__).resolve().parents[2]
RUNTIME=('photo_api','photo_worker','photo_scheduler','photo_auth','photo_portal',
         'photo_publisher','photo_maintenance','photo_gateway','photo_semantic_admission')
SIG='photo.v28_relight_recorded_subject_consents_live(uuid,jsonb)'

def apply_repair(db):
    files=list((ROOT/'backend/db/migrations').glob('02591_*.sql'))
    assert len(files)==1
    db.execute(files[0].read_text().split('-- +goose Down',1)[0])

def catalog(db):
    return dict(
        relation=db.execute("SELECT oid,relowner,relacl::text,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.workflow_consent_links'::regclass").fetchall(),
        columns=db.execute("SELECT attnum,attname,attacl::text FROM pg_attribute WHERE attrelid='photo.workflow_consent_links'::regclass AND attnum>0 AND NOT attisdropped ORDER BY attnum").fetchall(),
        helper=db.execute('SELECT to_jsonb(p) FROM pg_proc p WHERE oid=%s::regprocedure',(SIG,)).fetchone()[0],
        triggers=db.execute("SELECT to_jsonb(t) FROM pg_trigger t WHERE tgrelid='photo.workflow_consent_links'::regclass ORDER BY oid").fetchall(),
        policies=db.execute("SELECT to_jsonb(p) FROM pg_policy p WHERE polrelid='photo.workflow_consent_links'::regclass ORDER BY oid").fetchall(),
        constraints=db.execute("SELECT to_jsonb(c) FROM pg_constraint c WHERE conrelid='photo.workflow_consent_links'::regclass ORDER BY oid").fetchall(),
        runtime=db.execute("SELECT r.rolname,a.attname,has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE'),has_table_privilege(r.oid,a.attrelid,'INSERT'),has_table_privilege(r.oid,a.attrelid,'DELETE'),has_function_privilege(r.oid,%s,'EXECUTE') FROM pg_roles r CROSS JOIN pg_attribute a WHERE r.rolname=ANY(%s) AND a.attrelid='photo.workflow_consent_links'::regclass AND a.attnum>0 AND NOT a.attisdropped ORDER BY r.rolname,a.attnum",(SIG,list(RUNTIME))).fetchall())

@pytest.mark.migration_head(2590)
def test_upgrade_preserves_authority_and_only_adds_identity_column_lock_privilege(db):
    before=catalog(db)
    assert db.execute("SELECT has_any_column_privilege('photo_fn_owner','photo.workflow_consent_links','UPDATE')").fetchone()==(False,)
    apply_repair(db)
    after=catalog(db)
    assert {k:v for k,v in before.items() if k!='columns'}=={k:v for k,v in after.items() if k!='columns'}
    assert [r for r in before['columns'] if r[1]!='id']==[r for r in after['columns'] if r[1]!='id']
    assert db.execute("SELECT attname,has_column_privilege('photo_fn_owner',attrelid,attnum,'UPDATE') FROM pg_attribute WHERE attrelid='photo.workflow_consent_links'::regclass AND attnum>0 AND NOT attisdropped ORDER BY attnum").fetchall()==[(r[1],r[1]=='id') for r in after['columns']]
    assert db.execute("SELECT has_table_privilege('photo_fn_owner','photo.workflow_consent_links','UPDATE')").fetchone()==(False,)
    apply_repair(db)
    assert catalog(db)==after

@pytest.mark.migration_head(2590)
@pytest.mark.parametrize('drift',['trigger','policy','helper_body','helper_acl','helper_config'])
def test_incompatible_authority_catalog_refuses_atomically(db,drift):
    baseline=catalog(db)
    with db.transaction(force_rollback=True):
        if drift=='trigger':db.execute('ALTER TABLE photo.workflow_consent_links DISABLE TRIGGER workflow_consent_links_immutable')
        elif drift=='policy':db.execute('ALTER POLICY trusted_functions ON photo.workflow_consent_links TO photo_api')
        elif drift=='helper_acl':db.execute('GRANT EXECUTE ON FUNCTION '+SIG+' TO photo_worker')
        elif drift=='helper_config':db.execute('ALTER FUNCTION '+SIG+' RESET search_path')
        else:
            definition=db.execute('SELECT pg_get_functiondef(%s::regprocedure)',(SIG,)).fetchone()[0]
            assert 'RETURN false;' in definition
            db.execute(definition.replace('RETURN false;','RETURN false; /* source drift */',1))
        changed=catalog(db)
        with pytest.raises(errors.RaiseException,match='RELIGHT_CONSENT_LINK_ROW_LOCK_SOURCE_SHAPE'),db.transaction():
            apply_repair(db)
        assert catalog(db)==changed
    assert catalog(db)==baseline

def linked_fixture(db):
    args,scope,retained,workflow,project=person_fixture(db,linked=True)
    return args,scope,retained,retained['consent'],retained['link']

def test_identity_update_remains_immutable_and_runtime_roles_gain_no_update(db):
    args,scope,retained,consent,link=linked_fixture(db)
    original=db.execute('SELECT to_jsonb(l) FROM photo.workflow_consent_links l WHERE id=%s',(link,)).fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT id FROM photo.workflow_consent_links WHERE id=%s FOR SHARE',(link,)).fetchone()==(link,)
        with pytest.raises(errors.ObjectNotInPrerequisiteState,match='V2_IMMUTABLE'),db.transaction():
            db.execute('UPDATE photo.workflow_consent_links SET id=id WHERE id=%s',(link,))
    db.execute('RESET ROLE')
    for role in RUNTIME:
        assert db.execute("SELECT has_any_column_privilege(%s,'photo.workflow_consent_links','UPDATE'),has_function_privilege(%s,%s,'EXECUTE')",(role,role,SIG)).fetchone()==(False,False)
        with db.transaction():
            db.execute('SET LOCAL ROLE '+role)
            with pytest.raises(errors.InsufficientPrivilege),db.transaction():
                db.execute('UPDATE photo.workflow_consent_links SET id=id WHERE id=%s',(link,))
        db.execute('RESET ROLE')
    assert db.execute('SELECT to_jsonb(l) FROM photo.workflow_consent_links l WHERE id=%s',(link,)).fetchone()[0]==original

@pytest.mark.parametrize('consumer',['worker','adopter'])
def test_recorded_dependency_lock_serializes_actual_api_consent_withdrawal(database,consumer):
    with psycopg.connect(**database) as setup:
        with setup.transaction():
            args,scope,retained,consent,link=linked_fixture(setup)
            result=complete_relight_task(setup,args)[0] if consumer=='adopter' else None
    def observe(db):
        db.execute('SET LOCAL ROLE '+('photo_worker' if consumer=='worker' else 'photo_scheduler'))
        if consumer=='worker':
            return db.execute('SELECT photo.v28_worker_relight_person_origin('+','.join(['%s']*11)+')',(*args,retained['body'].decode())).fetchone()[0]
        return db.execute('SELECT photo.v28_relight_adoption_person_origin(%s,%s,1,1,%s,%s,%s)',(args[0],args[2],result,args[8],retained['body'].decode())).fetchone()[0]
    with psycopg.connect(**database) as reader,psycopg.connect(**database,autocommit=True) as monitor:
        assert observe(reader) is not None
        reader_pid=reader.info.backend_pid
        pid=[]
        def withdraw():
            with psycopg.connect(**database) as writer:
                with writer.transaction():
                    writer.execute("SELECT set_config('app.tenant_id',%s,true)",(str(args[0]),))
                    writer.execute("SET LOCAL statement_timeout='15s'")
                    writer.execute('SET LOCAL ROLE photo_api')
                    pid.append(writer.info.backend_pid)
                    return writer.execute('SELECT photo.change_v2_consent('+','.join(['%s']*10)+')',(args[0],scope['project'],consent,1,'learning_photographer',None,'revoke','fixture-v1',scope['actor'],scope['session'])).fetchone()[0]
        with ThreadPoolExecutor(max_workers=1) as pool:
            pending=pool.submit(withdraw)
            try:
                deadline=monotonic()+10
                blocked=False
                while monotonic()<deadline and not pending.done():
                    if pid:
                        blocked=monitor.execute('SELECT %s=ANY(pg_blocking_pids(%s))',(reader_pid,pid[0])).fetchone()[0]
                        if blocked:break
                    sleep(.01)
                assert blocked and not pending.done()
            finally:
                reader.commit()
            changed=pending.result(timeout=15)
        assert changed['status']=='revoked' and changed['epoch']=='2'
        assert observe(reader) is None
        reader.rollback()
        assert monitor.execute('SELECT status,epoch,revision FROM photo.consent_grants WHERE id=%s',(consent,)).fetchone()==('revoked',2,2)
