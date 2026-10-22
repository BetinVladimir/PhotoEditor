"""Scheduler evidence scope: actual dispatch/claim, synthetic output metadata.

Physical object verification is exercised separately by the full shown scenario.
"""
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest
from conftest import database
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture

ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02586_semantic_shown_adoption_evidence.sql').read_text().split('-- +goose Down')[0]
OP='semantic.adopt_shown_retouch_region'

def test_installed_startup_guard_accepts_current_and_rejects_public_private_execute(database):
    from photo_worker.v2.shown_adoption_schema import require_shown_adoption_evidence_schema
    helper='photo.semantic_shown_adoption_evidence_scope(uuid,uuid,integer,bigint,text)'
    with psycopg.connect(**database,autocommit=True) as db:
        db.execute('SET ROLE photo_scheduler')
        require_shown_adoption_evidence_schema(db)
        db.execute('RESET ROLE')
        try:
            db.execute('GRANT EXECUTE ON FUNCTION '+helper+' TO PUBLIC')
            db.execute('SET ROLE photo_scheduler')
            with pytest.raises(ValueError,match='schema unavailable'):
                require_shown_adoption_evidence_schema(db)
        finally:
            db.execute('RESET ROLE')
            db.execute('REVOKE EXECUTE ON FUNCTION '+helper+' FROM PUBLIC')
        db.execute('SET ROLE photo_scheduler')
        require_shown_adoption_evidence_schema(db)
        db.execute('RESET ROLE')

@pytest.mark.migration_head(2585)
def test_upgrade_preserves_generic_reader_and_closes_helper_privileges(database):
    with psycopg.connect(**database) as db:
        def generic():
            return db.execute("SELECT oid,proowner,proacl,proconfig,prosecdef,pg_get_functiondef(oid) FROM pg_proc WHERE oid IN(to_regprocedure('photo.v2_adoption_artifact(uuid,uuid,integer,bigint,uuid)'),to_regprocedure('photo.v2_adoption_staged_outputs(uuid,uuid,integer,bigint)')) ORDER BY oid").fetchall()
        before=generic();assert len(before)==2
        db.execute(PATCH);assert generic()==before
        private='photo.semantic_shown_adoption_evidence_scope(uuid,uuid,integer,bigint,text)'
        exposed=('photo.v27_shown_adoption_artifact(uuid,uuid,integer,bigint,uuid,text)','photo.v27_shown_adoption_staged_outputs(uuid,uuid,integer,bigint,text)')
        for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'):
            assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,private)).fetchone()==(False,)
            for signature in exposed:
                assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(role=='photo_scheduler',)

def test_scheduler_current_result_input_output_and_revoked_authority(database):
    with psycopg.connect(**database) as db:
        values,task,c=successful_copy_metadata_fixture(db)
        tenant,tid,attempt,fence=c[:4]
        def artifact(aid,*,t=tenant,task_id=tid,a=attempt,f=fence,op=OP):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                rows=db.execute('SELECT * FROM photo.v27_shown_adoption_artifact(%s,%s,%s,%s,%s,%s)',(t,task_id,a,f,aid,op)).fetchall()
                db.execute('RESET ROLE');return rows
        def staged():
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                rows=db.execute('SELECT * FROM photo.v27_shown_adoption_staged_outputs(%s,%s,%s,%s,%s)',(tenant,tid,attempt,fence,OP)).fetchall()
                db.execute('RESET ROLE');return rows
        assert artifact(c[7])[0][-1]=='input'
        assert artifact(c[10])[0][-2:]==('working_image','output')
        outputs=staged();assert len(outputs)==2 and all(row[2]=='ready' for row in outputs)
        assert artifact(uuid4())==[]
        unrelated=db.execute("SELECT id FROM photo.artifacts WHERE tenant_id=%s AND state='ready' AND verified_at IS NOT NULL AND object_version IS NOT NULL AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=%s AND i.task_id=%s AND i.artifact_id=artifacts.id) AND id NOT IN(SELECT artifact_id FROM photo.output_uploads WHERE tenant_id=%s AND task_id=%s) LIMIT 1",(tenant,tenant,tid,tenant,tid)).fetchone()
        assert unrelated is not None and artifact(unrelated[0])==[]
        from test_database import tenant as create_tenant
        foreign_tenant,_=create_tenant(db,'reader-foreign-'+str(uuid4())+'@example.test')
        foreign=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,'fixture',%s,%s,1,'application/json','foreign-v1',clock_timestamp()) RETURNING id",(foreign_tenant,str(uuid4()),'e'*64)).fetchone()[0]
        assert artifact(foreign)==[]
        for override in ({'t':uuid4()},{'task_id':uuid4()},{'a':attempt+1},{'f':fence+1},{'op':'semantic.adopt_shown_exposure'},{'op':'unknown'}):
            assert artifact(c[7],**override)==[]
        class ProbeRollback(Exception):pass
        session=values[0][0][3]
        for sql,args in [
            ("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",(session,)),
            ("UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=%s AND id=(SELECT (config_snapshot->>'project_id')::uuid FROM photo.runs WHERE id=%s)",(tenant,values[4])),
            ("UPDATE photo.tasks SET canonical_result_id=NULL,state='failed' WHERE tenant_id=%s AND id=%s",(tenant,tid)),
        ]:
            with pytest.raises(ProbeRollback):
                with db.transaction():
                    changed=db.execute(sql,args).rowcount;assert changed>0
                    assert artifact(c[7])==[] and staged()==[]
                    raise ProbeRollback()
            assert artifact(c[7])
        with pytest.raises(ProbeRollback):
            with db.transaction():
                db.execute("UPDATE photo.output_uploads SET state='cleaned' WHERE tenant_id=%s AND task_id=%s AND artifact_id=%s",(tenant,tid,c[10]))
                assert artifact(c[10])==[]
                assert any(row[2]=='cleaned' for row in staged())
                raise ProbeRollback()
