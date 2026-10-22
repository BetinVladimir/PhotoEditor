"""Session-private migrated baselines; opt-in only, never a persistent PASS cache."""
from contextlib import contextmanager
from hashlib import sha256
from pathlib import Path
from uuid import uuid4
import json
import os

import psycopg
from psycopg import sql


class TemplateIsolationError(RuntimeError):
    pass


def migration_inventory(root, head, scaffold):
    cutoff = min(head, 196) if head is not None and scaffold else (196 if scaffold else head)
    files = []
    for path in sorted((root / 'backend/db/migrations').glob('*.sql')):
        version = int(path.name.split('_', 1)[0])
        if cutoff is None or version <= cutoff:
            files.append((path.name, path.read_bytes().split(b'-- +goose Down')[0]))
    digest = sha256()
    for part in (json.dumps({'head': head, 'scaffold': scaffold}, sort_keys=True).encode(),
                 *(item for name, body in files for item in (name.encode(), body))):
        digest.update(len(part).to_bytes(8, 'big'))
        digest.update(part)
    return digest.hexdigest(), tuple(files)


def apply_inventory(connection, inventory):
    for name, body in inventory:
        up = body.decode('utf-8')
        try:
            if '-- +goose NO TRANSACTION' in up:
                connection.execute(up)
            else:
                with connection.transaction():
                    connection.execute(up)
        except Exception as exc:
            exc.add_note(f'Migration: {name}')
            raise


def role_fingerprint(admin):
    # Never select rolpassword or log credentials. Names, attributes and membership
    # semantics are cluster-global and therefore not isolated by database cloning.
    roles = admin.execute("""SELECT rolname,rolsuper,rolinherit,rolcreaterole,
        rolcreatedb,rolcanlogin,rolreplication,rolbypassrls,rolconnlimit,rolconfig,rolvaliduntil
        FROM pg_roles ORDER BY rolname""").fetchall()
    memberships = admin.execute("""SELECT r.rolname,u.rolname,g.rolname,
        to_jsonb(m)-'oid'-'roleid'-'member'-'grantor'
        FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.roleid
        JOIN pg_roles u ON u.oid=m.member JOIN pg_roles g ON g.oid=m.grantor
        ORDER BY r.rolname,u.rolname,g.rolname""").fetchall()
    return sha256(json.dumps([roles, memberships], sort_keys=True, default=str).encode()).hexdigest()


class SessionDatabaseTemplates:
    LOCK = 0x50484F544F544553

    def __init__(self, root):
        if os.environ.get('PYTEST_XDIST_WORKER'):
            raise TemplateIsolationError('Database templates require serial pytest; xdist is unsupported')
        self.root = root
        self.source_identity = migration_inventory(root, None, False)[0]
        self.entries = {}
        self.owned = {}  # name -> redacted-at-call-site connection kwargs
        self.roles = None
        self.invalid = False

    def admin(self, kwargs):
        # The only accepted fixtures are the repository's dedicated local servers.
        host = kwargs.get('host')
        native = host == str(self.root / 'work') and kwargs.get('port') == 55433
        docker = host == '127.0.0.1' and kwargs.get('port') == 55432
        if not (native or docker):
            raise TemplateIsolationError('Templates require the dedicated local test PostgreSQL server')
        return psycopg.connect(**kwargs, dbname='postgres', autocommit=True,
                               connect_timeout=5, options='-c statement_timeout=30000 -c lock_timeout=5000')

    def drop(self, admin, name):
        admin.execute(sql.SQL('DROP DATABASE IF EXISTS {} WITH (FORCE)').format(sql.Identifier(name)))
        self.owned.pop(name, None)

    def assert_source(self, head, scaffold, expected):
        if (migration_inventory(self.root, None, False)[0] != self.source_identity
                or migration_inventory(self.root, head, scaffold)[0] != expected):
            self.invalid = True
            raise TemplateIsolationError('Migration source changed during pytest; restart this session')

    @contextmanager
    def clone(self, kwargs, head, scaffold):
        if self.invalid:
            raise TemplateIsolationError('Database template session invalidated; restart pytest')
        fingerprint, inventory = migration_inventory(self.root, head, scaffold)
        self.assert_source(head, scaffold, fingerprint)
        with self.admin(kwargs) as admin:
            if not admin.execute('SELECT pg_try_advisory_lock(%s)', (self.LOCK,)).fetchone()[0]:
                raise TemplateIsolationError('Dedicated PostgreSQL test lock occupied; run serially')
            try:
                current_roles = role_fingerprint(admin)
                if self.roles is not None and current_roles != self.roles:
                    self.invalid = True
                    raise TemplateIsolationError('Shared PostgreSQL role state changed; restart isolated session')
                server = admin.info.server_version
                identity = sha256(json.dumps(kwargs, sort_keys=True).encode()).hexdigest()
                key = (fingerprint, server, identity)
                template = self.entries.get(key)
                if template is None:
                    template = 'photo_template_' + uuid4().hex
                    self.owned[template] = dict(kwargs)
                    admin.execute(sql.SQL('CREATE DATABASE {}').format(sql.Identifier(template)))
                    try:
                        with psycopg.connect(**kwargs, dbname=template, autocommit=True,
                                               connect_timeout=5) as connection:
                            apply_inventory(connection, inventory)
                        self.assert_source(head, scaffold, fingerprint)
                        admin.execute(sql.SQL('ALTER DATABASE {} ALLOW_CONNECTIONS false').format(sql.Identifier(template)))
                        if admin.execute('SELECT count(*) FROM pg_stat_activity WHERE datname=%s', (template,)).fetchone()[0]:
                            raise TemplateIsolationError('Baseline still has active connections')
                        new_roles = role_fingerprint(admin)
                        if self.roles is not None and new_roles != self.roles:
                            self.invalid = True
                            raise TemplateIsolationError('Head variant changed shared roles; use a private cluster for this variant')
                        self.roles = new_roles
                        self.entries[key] = template
                    except BaseException:
                        self.invalid = True
                        self.drop(admin, template)
                        raise
                self.assert_source(head, scaffold, fingerprint)
                name = 'photo_test_' + uuid4().hex
                self.owned[name] = dict(kwargs)
                try:
                    admin.execute(sql.SQL('CREATE DATABASE {} TEMPLATE {}').format(sql.Identifier(name), sql.Identifier(template)))
                    yield name
                finally:
                    try:
                        self.drop(admin, name)
                    finally:
                        try:
                            self.assert_source(head, scaffold, fingerprint)
                            if role_fingerprint(admin) != self.roles:
                                self.invalid = True
                                raise TemplateIsolationError('Test left shared PostgreSQL role drift; templates invalidated')
                        except BaseException:
                            self.invalid = True
                            raise
            finally:
                admin.execute('SELECT pg_advisory_unlock(%s)', (self.LOCK,))

    def close(self):
        failures = []
        for name, kwargs in list(self.owned.items())[::-1]:
            try:
                with self.admin(kwargs) as admin:
                    self.drop(admin, name)
            except Exception:
                failures.append(name)
        if failures:
            raise TemplateIsolationError('Could not remove session-owned databases: ' + ', '.join(failures))
