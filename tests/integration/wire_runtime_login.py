"""Private wire-fixture runtime identity; administrative DSN never reaches worker.

The login has one runtime membership only, no object ownership or direct grants.
It is scoped to the child probe and removed even if the probe raises.
"""
from contextlib import contextmanager
from uuid import UUID, uuid4
import psycopg
from psycopg import sql
from psycopg.conninfo import conninfo_to_dict, make_conninfo


@contextmanager
def worker_login(admin_dsn, task_id=None, *, pending_dispatch=False):
    name = 'wire_worker_' + uuid4().hex
    password = uuid4().hex + uuid4().hex
    with psycopg.connect(admin_dsn, autocommit=True) as admin:
        admin.execute(sql.SQL('CREATE ROLE {} LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION PASSWORD {}').format(sql.Identifier(name), sql.Literal(password)))
        binding = None
        try:
            admin.execute(sql.SQL('GRANT photo_worker TO {}').format(sql.Identifier(name)))
            if task_id is not None:
                task_id = UUID(str(task_id))
                if pending_dispatch:
                    binding = admin.execute("SELECT w.tenant_id,w.worker_id,w.db_principal FROM photo.tasks t JOIN photo.worker_registrations w ON w.tenant_id=t.tenant_id AND w.worker_id=t.dispatch_worker_id WHERE t.id=%s AND t.state='queued' AND t.dispatch_command IS NOT NULL", (task_id,)).fetchone()
                else:
                    binding = admin.execute('SELECT w.tenant_id,w.worker_id,w.db_principal FROM photo.tasks t JOIN photo.worker_registrations w ON w.tenant_id=t.tenant_id AND w.worker_id=t.worker_id WHERE t.id=%s', (task_id,)).fetchone()
                if binding is None:
                    raise RuntimeError('WIRE_WORKER_REGISTRATION_MISSING')
                admin.execute('UPDATE photo.worker_registrations SET db_principal=%s WHERE tenant_id=%s AND worker_id=%s', (name,binding[0],binding[1]))
            options = conninfo_to_dict(admin_dsn)
            options.update(user=name, password=password)
            # Remove inherited administrator role options; TaskDatabase selects
            # photo_worker itself and attests both session_user/current_user.
            options.pop('options', None)
            yield make_conninfo(**options)
        finally:
            if binding is not None:
                admin.execute('UPDATE photo.worker_registrations SET db_principal=%s WHERE tenant_id=%s AND worker_id=%s AND db_principal=%s', (binding[2],binding[0],binding[1],name))
            # TaskDatabase uses bounded context-managed physical connections.
            admin.execute(sql.SQL('DROP ROLE {}').format(sql.Identifier(name)))


def run_worker_probe(callback, admin_dsn):
    import os
    with worker_login(admin_dsn, os.environ['PHOTO_TEST_V27_GATEWAY_TASK']) as runtime_dsn:
        callback(runtime_dsn)
