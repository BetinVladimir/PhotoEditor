"""Common direct/indexed exposure-preview gate installation and accounting."""
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest
ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02584_semantic_exposure_preview_live_io.sql').read_text().split('-- +goose Down')[0]
BASE='photo.execution_source_live(uuid)'
HELPER='photo.semantic_preapply_preview_task_live(uuid)'

@pytest.mark.migration_head(2583)
@pytest.mark.parametrize('drift',[False,True])
def test_exposure_live_upgrade_and_atomic_rollback(database,drift):
    with psycopg.connect(**database) as db:
        def snapshot():
            return db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,provolatile,pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(BASE,)).fetchone()
        if drift:
            row=snapshot();needle=' AND photo.semantic_shown_exposure_task_live(p_task) IS TRUE'
            assert row[-1].count(needle)==1
            db.execute(row[-2].replace(row[-1],row[-1].replace(needle,needle.replace('(p_task)','( p_task)'))));db.commit()
        before=snapshot()
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='EXPOSURE_PREVIEW_LIVE_SOURCE_SHAPE'):
                with db.transaction():db.execute(PATCH)
            assert snapshot()==before
            assert db.execute('SELECT to_regprocedure(%s)',(HELPER,)).fetchone()==(None,)
        else:
            db.execute(PATCH)
            after=snapshot();assert after[:6]==before[:6]
            assert after[-1].count('photo.semantic_preapply_preview_task_live(p_task)')==1
            for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'):
                assert db.execute('SELECT has_function_privilege(%s,to_regprocedure(%s),%s)',(role,HELPER,'EXECUTE')).fetchone()==(False,)
            assert db.execute('SELECT photo.semantic_preapply_preview_task_live(%s)',(uuid4(),)).fetchone()==(False,)

def test_refund_accounting_excludes_returned_allocations(database):
    # Execute the exact aggregate grammar installed by both new predicates,
    # with nonreturned allocations and an already returned/refunded allocation.
    with psycopg.connect(**database) as db:
        for name in ('photo.semantic_preapply_preview_task_live(uuid)','photo.semantic_preview_indexed_input_ready(uuid,uuid,uuid,uuid,text)'):
            body=db.execute('SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(name,)).fetchone()[0]
            assert body.count('AND NOT u.returned_to_run_reservation')==1
        assert db.execute('''WITH u(bytes,returned_to_run_reservation) AS (VALUES (20::bigint,false),(30::bigint,false),(70::bigint,true))
         SELECT 50+coalesce(sum(u.bytes) FILTER(WHERE NOT u.returned_to_run_reservation),0),50+sum(u.bytes) FROM u''').fetchone()==(100,170)
