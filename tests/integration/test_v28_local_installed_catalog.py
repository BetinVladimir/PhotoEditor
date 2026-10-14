"""Run V28 installed SQL contracts on fresh full-head PostgreSQL, no skips.

Denial/resource parity evidence, not a positive portrait producer/owner cycle.
"""
import json
import os
from pathlib import Path
import subprocess
import pytest
from psycopg.conninfo import make_conninfo

ROOT=Path(__file__).resolve().parents[2]


@pytest.mark.parametrize('package,name',[
    ('v2portrait','TestLocalPortraitSQLResourceParity'),
    ('v2portrait','TestLocalPortraitPostgresCatalogDenials'),
    ('v2portrait','TestLocalPortraitPostgresWorkerGatewayACLs'),
    ('v2draft','TestLocalPortraitSQLInventoryDigestParity'),
])
def test_v28_local_installed_postgres_contract(database,package,name):
    env={**os.environ,'PHOTO_TEST_DATABASE_URL':make_conninfo(**database),'GOCACHE':'/private/tmp/photoeditor-go-cache'}
    result=subprocess.run(['go','test','./internal/'+package,'-run','^'+name+'$','-count=1','-json'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr
    events=[json.loads(line) for line in result.stdout.splitlines() if line.startswith('{')]
    assert any(e.get('Test')==name and e.get('Action')=='pass' for e in events)
    assert not any(e.get('Action') in ('skip','fail') for e in events)
