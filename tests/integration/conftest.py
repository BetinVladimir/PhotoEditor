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

@pytest.fixture
def database(request):
    path=ROOT/'work/postgres.env'
    if not path.exists():pytest.skip('Start dedicated photoeditor development PostgreSQL first')
    values=dict(line.split('=',1) for line in path.read_text().splitlines() if '=' in line)
    kwargs=dict(host='127.0.0.1',port=55432,user=values['POSTGRES_USER'],password=values['POSTGRES_PASSWORD'])
    name='photo_test_'+uuid4().hex
    with psycopg.connect(**kwargs,dbname='postgres',autocommit=True) as admin:
        admin.execute(sql.SQL('CREATE DATABASE {}').format(sql.Identifier(name)))
    try:
        with psycopg.connect(**kwargs,dbname=name) as conn:
            for p in sorted((ROOT/'backend/db/migrations').glob('*.sql')):
                # Legacy scaffolds stop before V21; later migrations depend on V21.
                if Path(request.module.__file__).name in SCAFFOLD_MODULES and int(p.name.split('_',1)[0]) >= 197:
                    continue
                up=p.read_text().split('-- +goose Down')[0]
                conn.execute(up)
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
