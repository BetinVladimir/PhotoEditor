"""Advertise only a running, fully installed import result adopter."""

from threading import Event, Thread
from uuid import uuid4


REQUIRED_FUNCTIONS = (
    'photo.announce_v30_import_adopter_runtime_bound(uuid,text)',
    'photo.withdraw_v30_import_adopter_runtime(uuid)',
)


def require_import_adopter_runtime_schema(connection):
    if connection.autocommit is not True:
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_SCHEMA')
    row = connection.execute("SELECT current_user").fetchone()
    if row != ('photo_scheduler',):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_ROLE')
    for signature in REQUIRED_FUNCTIONS:
        row = connection.execute('''
            SELECT p.prokind='f' AND p.prosecdef
                   AND pg_get_userbyid(p.proowner)='photo_fn_owner'
                   AND ('search_path=pg_catalog,photo'=ANY(p.proconfig)
                     OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
                   AND has_function_privilege(current_user,p.oid,'EXECUTE')
                   AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
                   AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
                   AND NOT has_function_privilege('photo_gateway',p.oid,'EXECUTE')
                   AND NOT has_function_privilege('photo_portal',p.oid,'EXECUTE')
                   AND NOT has_function_privilege('photo_maintenance',p.oid,'EXECUTE')
                   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(
                       p.proacl,acldefault('f',p.proowner))) acl
                       WHERE acl.privilege_type='EXECUTE'
                         AND acl.grantee NOT IN
                             (p.proowner,'photo_scheduler'::regrole::oid))
            FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
        ''', (signature,)).fetchone()
        if row != (True,):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_SCHEMA')
    row = connection.execute('''
        SELECT p.prokind='f' AND p.prosecdef
               AND pg_get_userbyid(p.proowner)='photo_fn_owner'
               AND ('search_path=pg_catalog,photo'=ANY(p.proconfig)
                 OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
               AND position('v30_import_adopter_runtime_leases' in
                    pg_get_functiondef(p.oid))>0
               AND position('v30_import_runtime_leases' in
                    pg_get_functiondef(p.oid))>0
               AND position('p_bucket' in pg_get_functiondef(p.oid))>0
               AND has_function_privilege('photo_api',p.oid,'EXECUTE')
               AND NOT has_function_privilege(current_user,p.oid,'EXECUTE')
        FROM pg_catalog.pg_proc p
        WHERE p.oid=to_regprocedure('photo.v30_import_runtime_ready(text)')
    ''').fetchone()
    if row != (True,):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_READY_GATE')


class ImportAdopterRuntimeLease:
    """Use a dedicated SQL connection so long object IO cannot starve renewals."""

    def __init__(self, connection, stop, bucket, *, interval_seconds=10):
        if (connection.autocommit is not True
                or not callable(getattr(stop, 'is_set', None))
                or not callable(getattr(stop, 'set', None))
                or type(interval_seconds) is not int
                or not 1 <= interval_seconds <= 10
                or not isinstance(bucket, str) or not 1 <= len(bucket) <= 255):
            raise ValueError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_INSTALLATION')
        self.connection, self.stop = connection, stop
        self.bucket = bucket
        self.interval = interval_seconds
        self.instance = uuid4()
        self._halt = Event()
        self._thread = None
        self._failure = None

    def _announce(self):
        row = self.connection.execute(
            'SELECT photo.announce_v30_import_adopter_runtime_bound(%s,%s)',
            (self.instance, self.bucket)).fetchone()
        if row != (True,):
            raise RuntimeError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_ANNOUNCE')

    def _renew(self):
        while not self._halt.wait(self.interval) and not self.stop.is_set():
            try:
                self._announce()
            except Exception:
                self._failure = RuntimeError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_LOST')
                self.stop.set()
                break

    def __enter__(self):
        self._announce()
        self._thread = Thread(target=self._renew, name='v30-import-adopter-lease',
                              daemon=True)
        try:
            self._thread.start()
        except Exception:
            try:
                self.connection.execute(
                    'SELECT photo.withdraw_v30_import_adopter_runtime(%s)',
                    (self.instance,))
            except Exception:
                pass
            raise RuntimeError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_THREAD') from None
        return self

    def __exit__(self, exc_type, exc, traceback):
        self._halt.set()
        self._thread.join(5)
        if self._thread.is_alive():
            raise RuntimeError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_THREAD')
        try:
            self.connection.execute(
                'SELECT photo.withdraw_v30_import_adopter_runtime(%s)',
                (self.instance,))
        except Exception:
            if exc_type is None:
                raise RuntimeError('INTERCHANGE_IMPORT_ADOPTER_RUNTIME_WITHDRAW') from None
        if exc_type is None and self._failure is not None:
            raise self._failure
        return False
