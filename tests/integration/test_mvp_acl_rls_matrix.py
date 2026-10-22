"""Current enabled branch role boundaries and real two-tenant RLS fixtures."""
from uuid import uuid4
import pytest
import psycopg
from psycopg import errors
from test_database import tenant

API_ONLY = (
 'photo.authorize_tenant(uuid,uuid,uuid)',
 'photo.discover_v28_local_portrait_result(uuid,uuid,uuid,uuid)',
 'photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text)',
 'photo.get_v29_negative_apply_selection(uuid,uuid,uuid,uuid)',
 'photo.apply_v29_negative_version(uuid,uuid,uuid,uuid,text,text,text)',
)
WORKER_ONLY = (
 'photo.v28_glare_generative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text)',
 'photo.semantic_removal_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)',
)
ROLES = ('photo_api','photo_auth','photo_portal','photo_worker','photo_scheduler',
         'photo_publisher','photo_gateway','photo_maintenance','photo_mail','photo_semantic_admission')


def test_current_enabled_branch_execute_matrix(db):
    for expected, signatures in (('photo_api',API_ONLY),('photo_worker',WORKER_ONLY)):
        for signature in signatures:
            assert db.execute('SELECT to_regprocedure(%s)', (signature,)).fetchone()[0] is not None
            for role in ROLES:
                actual = db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0]
                assert actual is (role == expected), (signature,role,actual)


def test_tenant_context_is_local_and_foreign_rows_remain_invisible(database):
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            a,_ = tenant(db,str(uuid4())+'@acl.test')
            b,_ = tenant(db,str(uuid4())+'@acl.test')
            for value in (a,b):
                db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Private project')", (value,))
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute('SELECT count(*) FROM photo.projects').fetchone() == (0,)
            db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(a),))
            assert db.execute('SELECT DISTINCT tenant_id FROM photo.projects').fetchall() == [(a,)]
            assert db.execute('SELECT count(*) FROM photo.projects WHERE tenant_id=%s', (b,)).fetchone() == (0,)
            with pytest.raises(errors.InsufficientPrivilege), db.transaction():
                db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Forbidden')", (b,))
        # SET LOCAL must not leak into the next pooled-equivalent transaction.
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute('SELECT count(*) FROM photo.projects').fetchone() == (0,)


def test_every_current_tenant_table_forces_rls_and_roles_cannot_bypass(db):
    rows = db.execute("""SELECT c.relname,c.relrowsecurity,c.relforcerowsecurity
      FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
      JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped
      WHERE n.nspname='photo' AND c.relkind='r' AND c.relname<>'resource_slots'""").fetchall()
    assert len(rows)>50
    assert all(enabled and forced for _,enabled,forced in rows), rows
    for role in ROLES:
        assert db.execute('SELECT NOT rolsuper AND NOT rolbypassrls FROM pg_roles WHERE rolname=%s', (role,)).fetchone() == (True,)
        assert db.execute("SELECT has_schema_privilege(%s,'photo','CREATE')", (role,)).fetchone() == (False,)
