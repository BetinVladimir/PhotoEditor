"""Live, policy-bound readiness for the installed V31 result adopter."""

from threading import Event, Lock, Thread
from uuid import UUID, uuid4

from psycopg.types.json import Jsonb


def require_signing_adopter_runtime_schema(connection):
    if connection.autocommit is not True or connection.execute(
            'SELECT current_user').fetchone() != ('photo_scheduler',):
        raise ValueError('SIGNING_ADOPTER_RUNTIME_ROLE')
    for signature in (
            'photo.announce_v31_signing_adopter_runtime(uuid,text,jsonb)',
            'photo.withdraw_v31_signing_adopter_runtime(uuid)'):
        row = connection.execute('''
            SELECT p.prokind='f' AND p.prosecdef
              AND pg_get_userbyid(p.proowner)='photo_fn_owner'
              AND ('search_path=pg_catalog,photo'=ANY(p.proconfig)
                OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
              AND has_function_privilege(current_user,p.oid,'EXECUTE')
              AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
              AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
              AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(
                p.proacl,acldefault('f',p.proowner))) acl
                WHERE acl.privilege_type='EXECUTE' AND acl.grantee NOT IN
                  (p.proowner,'photo_scheduler'::regrole::oid))
            FROM pg_catalog.pg_proc p WHERE p.oid=to_regprocedure(%s)
        ''', (signature,)).fetchone()
        if row != (True,):
            raise ValueError('SIGNING_ADOPTER_RUNTIME_SCHEMA')
    row = connection.execute('''
        SELECT p.prokind='f' AND p.prosecdef
          AND pg_get_userbyid(p.proowner)='photo_fn_owner'
          AND position('v31_signing_adopter_runtime_leases' in
            pg_get_functiondef(p.oid))>0
          AND has_function_privilege('photo_api',p.oid,'EXECUTE')
          AND NOT has_function_privilege(current_user,p.oid,'EXECUTE')
        FROM pg_catalog.pg_proc p
        WHERE p.oid=to_regprocedure(
          'photo.v31_signing_adopter_runtime_ready(text,text)')
    ''').fetchone()
    if row != (True,):
        raise ValueError('SIGNING_ADOPTER_RUNTIME_READY_GATE')
    row = connection.execute('''
        SELECT c.relkind='r' AND c.relrowsecurity AND c.relforcerowsecurity
          AND pg_get_userbyid(c.relowner)='photo_fn_owner'
          AND has_table_privilege('photo_fn_owner',c.oid,'SELECT')
          AND has_table_privilege('photo_fn_owner',c.oid,'INSERT')
          AND has_table_privilege('photo_fn_owner',c.oid,'UPDATE')
          AND NOT has_table_privilege('photo_fn_owner',c.oid,'DELETE')
          AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['photo_api','photo_worker',
            'photo_scheduler','photo_gateway','photo_portal','photo_maintenance']) role(name)
            WHERE has_table_privilege(role.name,c.oid,'SELECT')
              OR has_table_privilege(role.name,c.oid,'INSERT')
              OR has_table_privilege(role.name,c.oid,'UPDATE')
              OR has_table_privilege(role.name,c.oid,'DELETE'))
          AND EXISTS(SELECT 1 FROM pg_policy policy WHERE policy.polrelid=c.oid
            AND policy.polname='trusted_functions' AND policy.polcmd='*'
            AND policy.polroles=ARRAY['photo_fn_owner'::regrole::oid]
            AND pg_get_expr(policy.polqual,policy.polrelid)='true'
            AND pg_get_expr(policy.polwithcheck,policy.polrelid)='true')
        FROM pg_class c WHERE c.oid=to_regclass(
          'photo.v31_signing_adopter_runtime_leases')
    ''').fetchone()
    if row != (True,):
        raise ValueError('SIGNING_ADOPTER_RUNTIME_TABLE_AUTHORITY')
    for signature in (
            'photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text)',
            'photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb)'):
        row = connection.execute('''
            SELECT position('v31_signing_adopter_runtime_ready' in
              pg_get_functiondef(to_regprocedure(%s)))>0
        ''', (signature,)).fetchone()
        if row != (True,):
            raise ValueError('SIGNING_ADOPTER_RUNTIME_ADMISSION_GATE')


def _validate_pins(pins):
    if not isinstance(pins, tuple) or not 1 <= len(pins) <= 256:
        raise ValueError('SIGNING_ADOPTER_RUNTIME_PINS')
    seen = set()
    owned = []
    for pin in pins:
        if not isinstance(pin, dict) or set(pin) != {'id', 'sha'}:
            raise ValueError('SIGNING_ADOPTER_RUNTIME_PINS')
        try:
            policy = UUID(pin['id'])
        except (ValueError, TypeError, AttributeError):
            raise ValueError('SIGNING_ADOPTER_RUNTIME_PINS') from None
        sha = pin['sha']
        if (policy.int == 0 or str(policy) != pin['id'] or
                not isinstance(sha, str) or len(sha) != 64 or
                any(c not in '0123456789abcdef' for c in sha) or
                (pin['id'], sha) in seen):
            raise ValueError('SIGNING_ADOPTER_RUNTIME_PINS')
        seen.add((pin['id'], sha))
        owned.append({'id': pin['id'], 'sha': sha})
    return tuple(owned)


class SigningAdopterRuntimeLease:
    """Keep the lease on a dedicated connection across long object reads."""

    def __init__(self, connection, stop, bucket, pins, *, interval_seconds=10):
        if (connection.autocommit is not True
                or not callable(getattr(stop, 'is_set', None))
                or not callable(getattr(stop, 'set', None))
                or type(interval_seconds) is not int or not 1 <= interval_seconds <= 10
                or not isinstance(bucket, str) or not 3 <= len(bucket) <= 63):
            raise ValueError('SIGNING_ADOPTER_RUNTIME_INSTALLATION')
        self.connection, self.stop = connection, stop
        self.bucket, self.pins = bucket, _validate_pins(pins)
        self.instance = uuid4()
        self.interval = interval_seconds
        self._halt, self._lock = Event(), Lock()
        self._thread = None
        self._ready = False
        self._failure = None

    def _announce(self):
        row = self.connection.execute(
            'SELECT photo.announce_v31_signing_adopter_runtime(%s,%s,%s)',
            (self.instance, self.bucket, Jsonb(list(self.pins)))).fetchone()
        if row != (True,):
            raise RuntimeError('SIGNING_ADOPTER_RUNTIME_ANNOUNCE')

    def _withdraw(self):
        self.connection.execute('SELECT photo.withdraw_v31_signing_adopter_runtime(%s)',
                                (self.instance,)).fetchone()

    def _renew(self):
        while not self._halt.wait(self.interval) and not self.stop.is_set():
            try:
                with self._lock:
                    if self._ready:
                        self._announce()
            except Exception:
                self._failure = RuntimeError('SIGNING_ADOPTER_RUNTIME_LOST')
                self.stop.set()
                return

    def __enter__(self):
        with self._lock:
            self._announce()
            self._ready = True
        self._thread = Thread(target=self._renew, name='v31-signing-adopter-lease',
                              daemon=True)
        try:
            self._thread.start()
        except Exception:
            with self._lock:
                self._ready = False
                try:
                    self._withdraw()
                except Exception:
                    pass
            raise RuntimeError('SIGNING_ADOPTER_RUNTIME_THREAD') from None
        return self

    def set_ready(self, ready):
        if type(ready) is not bool or self._thread is None or self._failure is not None:
            raise RuntimeError('SIGNING_ADOPTER_RUNTIME_STATE')
        with self._lock:
            if self.stop.is_set():
                raise RuntimeError('SIGNING_ADOPTER_RUNTIME_STOPPED')
            try:
                if ready:
                    self._announce()
                else:
                    self._withdraw()
                self._ready = ready
            except Exception:
                self._ready = False
                self.stop.set()
                raise RuntimeError('SIGNING_ADOPTER_RUNTIME_LOST') from None

    def __exit__(self, exc_type, exc, traceback):
        self._halt.set()
        self._thread.join(5)
        if self._thread.is_alive():
            raise RuntimeError('SIGNING_ADOPTER_RUNTIME_THREAD')
        with self._lock:
            self._ready = False
            try:
                self._withdraw()
            except Exception:
                if exc_type is None:
                    raise RuntimeError('SIGNING_ADOPTER_RUNTIME_WITHDRAW') from None
        if exc_type is None and self._failure is not None:
            raise self._failure
        return False
