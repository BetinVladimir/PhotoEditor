"""Live migration311 scheduler boundary; no production mutation."""
import os
from uuid import uuid4
import psycopg
import pytest


def test_lens_params_function_is_scheduler_only_and_context_bound():
    url=os.environ.get('PHOTO_TEST_DATABASE_URL')
    if not url:pytest.skip('PHOTO_TEST_DATABASE_URL is required')
    with psycopg.connect(url) as connection:
        check_lens_adoption_boundary(connection)


def check_lens_adoption_boundary(connection):
    row=connection.execute("""SELECT p.prosecdef,r.rolname,p.proconfig,
        has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),
        has_function_privilege('photo_api',p.oid,'EXECUTE')
        FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner
        WHERE p.oid='photo.v25_lens_adoption_params(uuid,uuid)'::regprocedure""").fetchone()
    assert row[0] and row[1]=='photo_fn_owner'
    assert 'search_path=pg_catalog, photo' in row[2]
    assert row[3] and not row[4]
    connection.execute('SET LOCAL ROLE photo_scheduler')
    assert connection.execute('SELECT * FROM photo.v25_lens_adoption_params(%s,%s)',(uuid4(),uuid4())).fetchall()==[]
    connection.execute('RESET ROLE')
    connection.execute('SET LOCAL ROLE photo_api')
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        connection.execute('SELECT * FROM photo.v25_lens_adoption_params(%s,%s)',(uuid4(),uuid4()))
