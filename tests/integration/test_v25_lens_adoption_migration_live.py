"""Verify scheduler-only lens lookup after the complete forward migration set."""
from uuid import uuid4
import psycopg
import pytest


def test_lens_lookup_scheduler_privileges_and_empty_scope(db):
    row=db.execute("""SELECT p.prosecdef,r.rolname,p.proconfig,
        has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),
        has_function_privilege('photo_api',p.oid,'EXECUTE'),
        has_function_privilege('public',p.oid,'EXECUTE')
        FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner
        WHERE p.oid='photo.v25_lens_adoption_params(uuid,uuid)'::regprocedure""").fetchone()
    assert row is not None and row[0] and row[1]=='photo_fn_owner'
    assert 'search_path=pg_catalog, photo' in row[2]
    assert row[3] and not row[4] and not row[5]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT * FROM photo.v25_lens_adoption_params(%s,%s)',(uuid4(),uuid4())).fetchall()==[]
        db.execute('RESET ROLE')
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute('SELECT * FROM photo.v25_lens_adoption_params(%s,%s)',(uuid4(),uuid4()))
