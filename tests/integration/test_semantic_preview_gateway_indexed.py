"""Private indexed selector installation preserves the complete outer chain."""
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest
ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02583_semantic_preview_gateway_indexed_inputs.sql').read_text().split('-- +goose Down')[0]
BASE='photo.gateway_input_before_render_cache(uuid,integer,bigint,uuid,text,uuid)'
HELPER='photo.semantic_preview_indexed_input_ready(uuid,uuid,uuid,uuid,text)'

@pytest.mark.migration_head(2582)
@pytest.mark.parametrize('drift',[False,True])
def test_index_upgrade_identity_and_drift_rollback(database,drift):
    with psycopg.connect(**database) as db:
        def snapshot():
            return db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,provolatile,proallargtypes,proargmodes,proargnames,pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(BASE,)).fetchone()
        if drift:
            row=snapshot();needle="  OR (\n   t.execution_profile->>'renderer_version'='v2.2'"
            assert row[-1].count(needle)==1
            db.execute(row[-2].replace(row[-1],row[-1].replace(needle,needle.replace('  OR (',' OR ('))));db.commit()
        outer_before=db.execute("SELECT oid,proowner,proacl,proconfig,prosecdef,pg_get_functiondef(oid) FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname LIKE 'gateway_input%%' AND oid<>to_regprocedure(%s) ORDER BY oid",(BASE,)).fetchall()
        before=snapshot()
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='SEMANTIC_GATEWAY_INDEX_SOURCE_SHAPE'):
                with db.transaction():db.execute(PATCH)
            assert snapshot()==before
            assert db.execute('SELECT to_regprocedure(%s)',(HELPER,)).fetchone()==(None,)
        else:
            db.execute(PATCH)
            after=snapshot();assert after[:9]==before[:9]
            assert after[-1].count('photo.semantic_preview_indexed_input_ready(')==1
            for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
                assert db.execute('SELECT has_function_privilege(%s,to_regprocedure(%s),%s)',(role,HELPER,'EXECUTE')).fetchone()==(False,)
            assert db.execute('SELECT photo.semantic_preview_indexed_input_ready(%s,%s,%s,%s,%s)',(uuid4(),uuid4(),uuid4(),uuid4(),'0'*64)).fetchone()==(False,)

        assert db.execute("SELECT oid,proowner,proacl,proconfig,prosecdef,pg_get_functiondef(oid) FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname LIKE 'gateway_input%%' AND oid<>to_regprocedure(%s) ORDER BY oid",(BASE,)).fetchall()==outer_before
