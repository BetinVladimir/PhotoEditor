"""Real repeated worker admission across gateway allocation; no model quality claim."""
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest
from conftest import database as isolated_database
from test_v27_shown_retouch_live_io import claimed_fixture

ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02585_semantic_shown_admission_allocated_quota.sql').read_text().split('-- +goose Down')[0]
NAMES=('exposure','removal','background_cleanup','retouch')
SIGNATURES=tuple('photo.admit_semantic_shown_'+n+'(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)' for n in NAMES)
NEEDLE="q.bytes>= (t.execution_profile->'resource_estimate'->>'output_bytes')::bigint"

@pytest.mark.migration_head(2584)
@pytest.mark.parametrize('drift',[False,True])
def test_upgrade_identity_and_last_function_atomic_rollback(database,drift):
    with psycopg.connect(**database) as db:
        def snapshot():
            return [db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,provolatile,prorettype,proargtypes,proargnames,proargmodes,proallargtypes,pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(s,)).fetchone() for s in SIGNATURES]
        if drift:
            last=snapshot()[-1];assert last[-1].count(NEEDLE)==1
            db.execute(last[-2].replace(last[-1],last[-1].replace(NEEDLE,NEEDLE.replace('q.bytes>=','q.bytes >= '))));db.commit()
        before=snapshot()
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='SHOWN_ADMISSION_QUOTA_SOURCE_SHAPE'):
                with db.transaction():db.execute(PATCH)
            assert snapshot()==before
        else:
            db.execute(PATCH);after=snapshot()
            for old,new in zip(before,after):
                assert new[:11]==old[:11]
                assert new[-1].count('AND NOT u.returned_to_run_reservation')==1
            for signature in SIGNATURES:
                assert db.execute("SELECT has_function_privilege('photo_worker',to_regprocedure(%s),'EXECUTE'),has_function_privilege('photo_api',to_regprocedure(%s),'EXECUTE')",(signature,signature)).fetchone()==(True,False)

def test_actual_worker_admission_survives_gateway_allocation_and_refuses_tamper(database):
    with psycopg.connect(**database) as db:
        x=claimed_fixture(db);plan=x['plan']
        args=(x['tenant'],x['run'],x['task'],1,x['fence'],x['worker'],plan['params_artifact_id'],plan['params_sha'],plan['params_body'],plan['node']['input_digest'])
        def admitted():
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_worker')
                row=db.execute('SELECT photo.admit_semantic_shown_retouch('+','.join(['%s']*10)+')',args).fetchone()[0]
                db.execute('RESET ROLE')
                return row
        assert admitted() is True
        q=db.execute("SELECT id,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(x['tenant'],x['run'])).fetchone()
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_gateway')
            ticket=db.execute('SELECT * FROM photo.begin_output(%s,1,%s,%s,%s,%s,%s,2,%s,%s,%s)',(x['task'],x['fence'],x['worker'],'f'*64,uuid4(),'pixels','a'*64,'application/x-npy','test')).fetchone()
            assert ticket[0]=='created'
            db.execute('RESET ROLE')
        assert db.execute('SELECT bytes FROM photo.quota_reservations WHERE id=%s',(q[0],)).fetchone()==(q[1]-2,)
        assert admitted() is True
        class RollbackProbe(Exception):pass
        mutations=[('UPDATE photo.output_uploads SET run_reservation_id=NULL WHERE id=%s',(ticket[1],)),
            ("UPDATE photo.output_uploads SET state='cleaned',returned_to_run_reservation=true WHERE id=%s",(ticket[1],)),
            ("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(q[0],)),
            ('UPDATE photo.quota_reservations SET principal_id=%s WHERE id=%s',(uuid4(),q[0]))]
        foreign=db.execute('SELECT id FROM photo.quota_reservations WHERE tenant_id=%s AND id<>%s ORDER BY id LIMIT 1',(x['tenant'],q[0])).fetchone()
        assert foreign is not None
        mutations.append(('UPDATE photo.output_uploads SET run_reservation_id=%s WHERE id=%s',(foreign[0],ticket[1])))
        for sql,values in mutations:
            with pytest.raises(RollbackProbe):
                with db.transaction():
                    db.execute(sql,values)
                    assert admitted() is False,sql
                    raise RollbackProbe()
            assert admitted() is True
