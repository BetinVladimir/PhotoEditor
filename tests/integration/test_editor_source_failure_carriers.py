"""Actual source-loss lifecycle, relational authority only; no photographic quality claim."""
from pathlib import Path
from uuid import uuid4
import pytest,psycopg
from editor_source_failure_fixture import admitted_final
ROOT=Path(__file__).resolve().parents[2]
MIGRATION=ROOT/'backend/db/migrations/02644_editor_source_failure_carriers.sql'


def scheduler(db,sql,args=()):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        out=db.execute(sql,args).fetchone()[0]
    db.execute('RESET ROLE')
    return out


def public_codes(db,tenant,run,scope):
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(scope),))
        db.execute('SET LOCAL ROLE photo_api')
        result=db.execute('SELECT photo.editor_effect_source_failure_codes(%s,%s)',(tenant,run)).fetchone()[0]
    db.execute('RESET ROLE')
    return result


@pytest.mark.parametrize('slot',['seal_receipt','result','processed_source','executed_style','quality'])
def test_real_intermediate_loss_records_reason_then_drains_exactly_once(db,slot):
    tid,build,final,pending,descriptor,version,source,source_sha=admitted_final(db)
    assert public_codes(db,tid,build,tid)==[]
    assert scheduler(db,'SELECT photo.withdraw_editor_effect_build()') is False
    artifact=db.execute('SELECT '+slot+'_artifact_id FROM photo.editor_effect_builds WHERE run_id=%s',(build,)).fetchone()[0]
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s",(tid,artifact))
    assert db.execute('SELECT photo.editor_effect_intermediate_catalog_lost(%s,%s)',(tid,build)).fetchone()==(True,)
    reserved=db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()[0]
    assert scheduler(db,'SELECT photo.withdraw_editor_effect_build()') is True
    witness=db.execute('SELECT intermediate_source_failure FROM photo.editor_effect_builds WHERE run_id=%s',(build,)).fetchone()[0]
    assert witness=={'code':'EDITOR_INTERMEDIATE_UNAVAILABLE'}
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('UPDATE photo.editor_effect_builds SET intermediate_source_failure=NULL WHERE run_id=%s',(build,))
    assert db.execute('SELECT intermediate_source_failure FROM photo.editor_effect_builds WHERE run_id=%s',(build,)).fetchone()==(witness,)
    for run in [build,final]:
        assert public_codes(db,tid,run,tid)==['EDITOR_INTERMEDIATE_UNAVAILABLE']
        assert public_codes(db,tid,run,uuid4())==[]
    assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id=%s',(final,)).fetchone()==('cancelling',True)
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(pending,)).fetchone()==('ready',)
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(reserved,)
    assert scheduler(db,'SELECT photo.cancel_idle_task()')==1
    assert scheduler(db,'SELECT photo.finalize_run_cancellation()')==1
    assert scheduler(db,'SELECT photo.withdraw_editor_effect_build()') is True
    assert scheduler(db,'SELECT photo.withdraw_editor_effect_build()') is False
    assert db.execute('SELECT state,intermediate_source_failure FROM photo.editor_effect_builds WHERE run_id=%s',(build,)).fetchone()==('failed',witness)
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(reserved-896,)
    assert db.execute('SELECT master_artifact_id,content_hash FROM photo.asset_versions WHERE id=%s',(version,)).fetchone()==(source,source_sha)
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')


@pytest.mark.parametrize('code',['EDITOR_SOURCE_OBJECT_MISSING','EDITOR_SOURCE_OBJECT_INTEGRITY'])
def test_real_final_loss_public_codes_are_closed_and_scoped(db,code):
    tid,build,final,pending,descriptor,version,source,source_sha=admitted_final(db)
    assert scheduler(db,'SELECT photo.fail_editor_effect_final_source(%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tid,build,*descriptor,code)) is True
    assert public_codes(db,tid,build,tid)==[code]
    assert public_codes(db,tid,final,tid)==[code]
    assert public_codes(db,tid,build,uuid4())==[]
    assert public_codes(db,uuid4(),build,tid)==[]
    assert scheduler(db,'SELECT photo.fail_editor_effect_final_source(%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tid,build,*descriptor,code)) is False


def test_intermediate_carrier_is_append_once_and_not_client_writable(db):
    tid,build,*_=admitted_final(db)
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute("UPDATE photo.editor_effect_builds SET intermediate_source_failure='{\"code\":\"EDITOR_INTERMEDIATE_UNAVAILABLE\"}' WHERE run_id=%s",(build,))
    with pytest.raises(psycopg.errors.CheckViolation):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute("UPDATE photo.editor_effect_builds SET intermediate_source_failure='{\"code\":\"arbitrary-private-text\"}' WHERE run_id=%s",(build,))
    db.execute('RESET ROLE')


@pytest.mark.parametrize('role',['photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_maintenance'])
def test_accessor_private_to_api_role(db,role):
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE '+role)
            db.execute('SELECT photo.editor_effect_source_failure_codes(%s,%s)',(uuid4(),uuid4()))


@pytest.mark.migration_head(2643)
@pytest.mark.parametrize('signature',[
 'photo.withdraw_editor_effect_build()',
 'photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text)',
])
def test_installer_null_config_refuses_without_partial_schema_or_function_write(db,signature):
    db.execute('ALTER FUNCTION '+signature+' RESET ALL')
    assert db.execute('SELECT proconfig FROM pg_proc WHERE oid=%s::regprocedure',(signature,)).fetchone()==(None,)
    before=db.execute("SELECT p.oid,to_jsonb(p) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' ORDER BY p.oid").fetchall()
    with pytest.raises(psycopg.errors.RaiseException,match='EDITOR_FAILURE_CARRIER_PREIMAGE_DRIFT'):
        with db.transaction():db.execute(MIGRATION.read_text().split('-- +goose Down',1)[0])
    assert db.execute("SELECT p.oid,to_jsonb(p) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' ORDER BY p.oid").fetchall()==before
    assert db.execute("SELECT count(*) FROM pg_attribute WHERE attrelid='photo.editor_effect_builds'::regclass AND attname='intermediate_source_failure' AND NOT attisdropped").fetchone()==(0,)


@pytest.mark.migration_head(2643)
def test_exact_upgrade_preserves_all_existing_metadata_and_other_bodies(db):
    def inventory():return {r[0]:(r[1],r[2],r[3]) for r in db.execute("SELECT p.oid,p.oid::regprocedure::text,to_jsonb(p)-'prosrc',p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo'")}
    before=inventory()
    table_before=db.execute("SELECT relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.editor_effect_builds'::regclass").fetchone()
    db.execute(MIGRATION.read_text().split('-- +goose Down',1)[0])
    after=inventory();assert before.keys()<=after.keys()
    changed={before[k][0] for k in before if before[k]!=after[k]}
    assert changed=={'photo.withdraw_editor_effect_build()','photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text)'}
    assert all(before[k][:2]==after[k][:2] for k in before)
    assert {after[k][0] for k in after.keys()-before.keys()}=={'photo.guard_editor_intermediate_source_failure()','photo.editor_effect_source_failure_codes(uuid,uuid)'}
    assert db.execute("SELECT relowner,relacl,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.editor_effect_builds'::regclass").fetchone()==table_before


@pytest.mark.parametrize('changed',['tenant','build','key','version','sha','size','media','code'])
def test_current_admitted_origin_rejects_foreign_or_mismatched_physical_witness(db,changed):
    tid,build,final,pending,descriptor,version,source,source_sha=admitted_final(db)
    assert db.execute('SELECT photo.editor_effect_final_origin_ready(%s,%s)',(tid,build)).fetchone()==(True,)
    args=[tid,build,*descriptor,'EDITOR_SOURCE_OBJECT_MISSING']
    index,value={'tenant':(0,uuid4()),'build':(1,uuid4()),'key':(3,'foreign/key'),'version':(4,'foreign-v2'),'sha':(5,'f'*64),'size':(6,descriptor[4]+1),'media':(7,'application/x-npy'),'code':(8,'NETWORK_TIMEOUT')}[changed]
    args[index]=value
    assert scheduler(db,'SELECT photo.fail_editor_effect_final_source(%s,%s,%s,%s,%s,%s,%s,%s,%s)',args) is False
    assert db.execute('SELECT final_source_failure FROM photo.editor_effect_builds WHERE run_id=%s',(build,)).fetchone()==(None,)
    assert db.execute('SELECT photo.editor_effect_final_origin_ready(%s,%s)',(tid,build)).fetchone()==(True,)
    assert db.execute('SELECT master_artifact_id,content_hash FROM photo.asset_versions WHERE id=%s',(version,)).fetchone()==(source,source_sha)
