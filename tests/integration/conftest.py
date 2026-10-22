from pathlib import Path
import os
from uuid import uuid4
import pytest
import psycopg
from psycopg import sql
ROOT=Path(__file__).resolve().parents[2]

class PrivateDatabaseConfig(dict):
    def __repr__(self):
        return '<isolated PostgreSQL configuration>'

class PrivateBrokerURL(str):
    def __repr__(self):
        return '<isolated RabbitMQ endpoint>'

SCAFFOLD_MODULES = {
    'test_v22_schema_scaffold.py',
    'test_v2_capability_scaffold.py',
    'test_v2_draft_shell.py',
    'test_v2_input_closure_gate.py',
    'test_v2_input_documents.py',
    'test_v2_task_input_registration.py',
}

@pytest.fixture(scope='session')
def session_database_templates():
    from database_templates import SessionDatabaseTemplates
    templates = SessionDatabaseTemplates(ROOT)
    try:
        yield templates
    finally:
        templates.close()


@pytest.fixture
def database(request):
    head_marker = request.node.get_closest_marker('migration_head')
    migration_head = head_marker.args[0] if head_marker else None
    if migration_head is not None and (type(migration_head) is not int or migration_head < 1):
        pytest.fail('migration_head requires one positive integer', pytrace=False)
    if os.environ.get('PHOTO_TEST_NATIVE_INFRA')=='1':
        kwargs=dict(host=str(ROOT/'work'),port=55433,user='photo_test')
    else:
        path=ROOT/'work/postgres.env'
        if not path.exists():pytest.skip('Start dedicated photoeditor development PostgreSQL first')
        values=dict(line.split('=',1) for line in path.read_text().splitlines() if '=' in line)
        kwargs=dict(host='127.0.0.1',port=55432,user=values['POSTGRES_USER'],password=values['POSTGRES_PASSWORD'])
    if os.environ.get('PHOTO_TEST_DB_TEMPLATES') == '1':
        templates = request.getfixturevalue('session_database_templates')
        scaffold = Path(request.module.__file__).name in SCAFFOLD_MODULES
        with templates.clone(kwargs, migration_head, scaffold) as name:
            yield PrivateDatabaseConfig({**kwargs, 'dbname': name})
        return
    name='photo_test_'+uuid4().hex
    try:
        admin_connection=psycopg.connect(**kwargs,dbname='postgres',autocommit=True)
    except psycopg.OperationalError:
        pytest.fail('Dedicated test PostgreSQL unavailable; no migrations executed',pytrace=False)
    with admin_connection as admin:
        admin.execute(sql.SQL('CREATE DATABASE {}').format(sql.Identifier(name)))
    try:
        with psycopg.connect(**kwargs,dbname=name,autocommit=True) as conn:
            for p in sorted((ROOT/'backend/db/migrations').glob('*.sql')):
                if migration_head is not None and int(p.name.split('_',1)[0]) > migration_head:
                    continue
                # Legacy scaffolds stop before V21; later migrations depend on V21.
                if Path(request.module.__file__).name in SCAFFOLD_MODULES and int(p.name.split('_',1)[0]) >= 197:
                    continue
                up=p.read_text().split('-- +goose Down')[0]
                try:
                    if '-- +goose NO TRANSACTION' in up:
                        conn.execute(up)
                    else:
                        with conn.transaction():
                            conn.execute(up)
                except Exception as exc:
                    # Identify the failing migration without printing credentials
                    # or changing transaction/rollback behavior.
                    exc.add_note(f'Migration: {p.name}')
                    raise
        yield PrivateDatabaseConfig({**kwargs,'dbname':name})
    finally:
        with psycopg.connect(**kwargs,dbname='postgres',autocommit=True) as admin:
            admin.execute(sql.SQL('DROP DATABASE {} WITH (FORCE)').format(sql.Identifier(name)))

@pytest.fixture
def db(database):
    with psycopg.connect(**database) as conn:
        with conn.transaction(force_rollback=True):yield conn

@pytest.fixture(scope='session')
def rabbit_vhost():
    import subprocess
    import time
    from urllib.parse import quote
    path = ROOT/'work/rabbitmq.env'
    if not path.exists():
        pytest.skip('Start dedicated development RabbitMQ first')
    values = dict(line.split('=', 1) for line in path.read_text().splitlines() if '=' in line)
    def ctl(*args):
        return subprocess.run(['docker', 'exec', 'photoeditor-dev-rabbit', 'rabbitmqctl', *args],
                              capture_output=True, text=True, timeout=20)
    label = subprocess.run(['docker', 'inspect', '--format', '{{index .Config.Labels "photoeditor.scope"}}',
                            'photoeditor-dev-rabbit'], capture_output=True, text=True, timeout=10)
    if label.returncode or label.stdout.strip() != 'development':
        pytest.fail('Dedicated RabbitMQ container unavailable')
    name = 'photo_test_' + uuid4().hex
    result = ctl('add_vhost', name)
    assert result.returncode == 0, result.stderr
    try:
        result = ctl('set_permissions', '-p', name, values['RABBITMQ_DEFAULT_USER'], '.*', '.*', '.*')
        assert result.returncode == 0, result.stderr
        user = quote(values['RABBITMQ_DEFAULT_USER'], safe='')
        password = quote(values['RABBITMQ_DEFAULT_PASS'], safe='')
        yield PrivateBrokerURL(f'amqp://{user}:{password}@127.0.0.1:55672/{name}')
    finally:
        result = ctl('delete_vhost', name)
        assert result.returncode == 0, result.stderr
