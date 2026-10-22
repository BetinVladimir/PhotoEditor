"""Actual installed roles cannot call the internal scene predicate directly."""
from uuid import uuid4
import psycopg
from psycopg import sql
import pytest


def test_private_scene_acl_and_public_seal_context(database):
    tenant,run=uuid4(),uuid4()
    with psycopg.connect(**database) as db:
        for role in ('photo_api','photo_worker','photo_scheduler'):
            with pytest.raises(psycopg.errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute(sql.SQL('SET LOCAL ROLE {}').format(sql.Identifier(role)))
                    db.execute('SELECT photo.v27_native_scene_graph_ready(%s,%s)',(tenant,run))
        for context in ('',str(uuid4()),'malformed-uuid'):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                db.execute("SELECT set_config('app.tenant_id',%s,true)",(context,))
                assert db.execute('SELECT photo.seal_v2_graph(%s,%s)',(tenant,run)).fetchone()==(False,)


@pytest.mark.migration_head(2579)
@pytest.mark.parametrize('drift',['none','private','public'])
def test_context_upgrade_preserves_identity_and_refuses_drift(database,drift):
    from pathlib import Path
    signatures=('photo.v27_native_scene_graph_ready(uuid,uuid)','photo.seal_v2_graph(uuid,uuid)')
    patch=(Path(__file__).resolve().parents[2]/'backend/db/migrations/02580_semantic_scene_private_context.sql').read_text().split('-- +goose Down')[0]
    with psycopg.connect(**database) as db:
        def identities():
            return [db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(name,)).fetchone() for name in signatures]
        if drift=='private':
            definition,body=db.execute('SELECT pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signatures[0],)).fetchone()
            guard=" IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;"
            assert body.count(guard)==1
            db.execute(definition.replace(body,body.replace(guard,guard+'\n'+guard)))
            db.commit()
        if drift=='public':
            definition,body=db.execute('SELECT pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signatures[1],)).fetchone()
            db.execute(definition.replace(body,body+'\n-- unexpected public source drift\n'))
            db.commit()
        before=identities()
        definitions_before=[db.execute('SELECT pg_get_functiondef(to_regprocedure(%s))',(name,)).fetchone()[0] for name in signatures]
        public_before=db.execute('SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signatures[1],)).fetchone()[0]
        if drift!='none':
            expected='SEMANTIC_PRIVATE_CONTEXT_SOURCE_SHAPE' if drift=='private' else 'SEMANTIC_PUBLIC_SEAL_SOURCE_SHAPE'
            with pytest.raises(psycopg.errors.RaiseException,match=expected):
                with db.transaction():db.execute(patch)
        else:
            db.execute(patch)
        assert identities()==before
        if drift!='none':
            assert [db.execute('SELECT pg_get_functiondef(to_regprocedure(%s))',(name,)).fetchone()[0] for name in signatures]==definitions_before
        if drift!='none':assert db.execute('SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signatures[1],)).fetchone()[0]==public_before
        body=db.execute('SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signatures[0],)).fetchone()[0]
        assert body.count("current_setting('app.tenant_id',true)")==(2 if drift=='private' else 1 if drift=='public' else 0)
