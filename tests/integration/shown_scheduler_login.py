"""Test-owned scheduler LOGIN; administrative connection never runs adopter."""
from contextlib import contextmanager
from uuid import uuid4
import psycopg
from psycopg import sql
from psycopg.conninfo import conninfo_to_dict, make_conninfo

@contextmanager
def scheduler_login(admin_dsn):
    name='wire_scheduler_'+uuid4().hex
    password=uuid4().hex+uuid4().hex
    with psycopg.connect(admin_dsn,autocommit=True) as admin:
        admin.execute(sql.SQL('CREATE ROLE {} LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION PASSWORD {}').format(sql.Identifier(name),sql.Literal(password)))
        try:
            admin.execute(sql.SQL('GRANT photo_scheduler TO {}').format(sql.Identifier(name)))
            options=conninfo_to_dict(admin_dsn)
            options.update(user=name,password=password)
            options.pop('options',None)
            dsn=make_conninfo(**options)
            with psycopg.connect(dsn) as check:
                check.execute('SET ROLE photo_scheduler')
                assert check.execute('SELECT session_user,current_user').fetchone()==(name,'photo_scheduler')
            yield dsn
        finally:
            admin.execute(sql.SQL('DROP ROLE {}').format(sql.Identifier(name)))
