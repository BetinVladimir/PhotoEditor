"""Actual gateway allocation, scheduler readiness, and atomic forward upgrade.

Dispatch/claim/allocation are real SQL; ready output metadata is synthetic and
does not constitute physical object-store verification or photographic quality.
"""
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest
from conftest import database
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture

ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02588_semantic_shown_adoption_allocated_quota.sql').read_text().split('-- +goose Down')[0]
SIGNATURES=tuple('photo.v27_shown_'+n+'_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text)' for n in ('exposure','removal','background_cleanup','retouch'))
NEEDLE="q.bytes>= (t.execution_profile->'resource_estimate'->>'output_bytes')::bigint"

@pytest.mark.migration_head(2587)
@pytest.mark.parametrize('drift',[None,'source','authority'])
def test_upgrade_identity_and_last_function_atomic_rollback(database,drift):
    with psycopg.connect(**database) as db:
        def snapshot():
            return [db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,provolatile,prorettype,proargtypes,proargnames,proargmodes,proallargtypes,pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(s,)).fetchone() for s in SIGNATURES]
        if drift=='source':
            last=snapshot()[-1];assert last[-1].count(NEEDLE)==1
            db.execute(last[-2].replace(last[-1],last[-1].replace(NEEDLE,NEEDLE.replace('q.bytes>=','q.bytes >= '))))
        elif drift=='authority':db.execute('GRANT EXECUTE ON FUNCTION '+SIGNATURES[-1]+' TO PUBLIC')
        db.commit();before=snapshot()
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='SHOWN_ADOPTION_QUOTA_'):
                with db.transaction():db.execute(PATCH)
            assert snapshot()==before
        else:
            db.execute(PATCH);after=snapshot()
            for old,new in zip(before,after):
                assert new[:11]==old[:11]
                assert new[-1].count('AND NOT u.returned_to_run_reservation')==1
            for signature in SIGNATURES:
                for role in ('photo_scheduler','photo_worker','photo_api','photo_gateway','photo_portal'):
                    assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(role=='photo_scheduler',)

@pytest.mark.migration_head(2587)
def test_actual_allocated_outputs_old_denial_upgrade_and_quota_negatives(database):
    with psycopg.connect(**database) as db:
        _,_,c=successful_copy_metadata_fixture(db,gateway_allocations=True)
        tenant,task,attempt,fence,result=c[:5]
        args=(tenant,task,attempt,fence,result,c[7],c[8],c[9])
        def ready():
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                accepted=db.execute('SELECT photo.v27_shown_retouch_adoption_ready('+','.join(['%s']*8)+')',args).fetchone()[0]
                db.execute('RESET ROLE');return accepted
        q=db.execute("SELECT id,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=(SELECT run_id FROM photo.tasks WHERE id=%s)",(tenant,task)).fetchone()
        uploads=db.execute('SELECT id,bytes,run_reservation_id FROM photo.output_uploads WHERE tenant_id=%s AND task_id=%s ORDER BY id',(tenant,task)).fetchall()
        assert len(uploads)==2 and all(row[2]==q[0] and row[1]>0 for row in uploads)
        expected=db.execute("SELECT (execution_profile->'resource_estimate'->>'output_bytes')::bigint FROM photo.tasks WHERE id=%s",(task,)).fetchone()[0]
        assert q[1]<expected and q[1]+sum(row[1] for row in uploads)>=expected
        assert ready() is False
        db.execute(PATCH)
        assert ready() is True
        class ProbeRollback(Exception):pass
        other_principal=db.execute('INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',('shown-quota-other-'+str(uuid4())+'@example.test',)).fetchone()[0]
        mutations=[
            ('UPDATE photo.output_uploads SET run_reservation_id=NULL WHERE id=%s',(uploads[0][0],)),
            ("UPDATE photo.output_uploads SET state='cleaned',returned_to_run_reservation=true WHERE id=%s",(uploads[0][0],)),
            ("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(q[0],)),
            ('UPDATE photo.quota_reservations SET principal_id=%s WHERE id=%s',(other_principal,q[0])),
            ('UPDATE photo.quota_reservations SET bytes=0 WHERE id=%s',(q[0],)),
        ]
        foreign=db.execute('SELECT id FROM photo.quota_reservations WHERE tenant_id=%s AND id<>%s ORDER BY id LIMIT 1',(tenant,q[0])).fetchone()
        assert foreign is not None
        mutations.append(('UPDATE photo.output_uploads SET run_reservation_id=%s WHERE id=%s',(foreign[0],uploads[0][0])))
        for sql,values in mutations:
            with pytest.raises(ProbeRollback):
                with db.transaction():
                    assert db.execute(sql,values).rowcount==1
                    assert ready() is False,sql
                    raise ProbeRollback()
            assert ready() is True
