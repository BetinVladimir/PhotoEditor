"""Production lens factory against actual isolated PostgreSQL service roles."""
from pathlib import Path
import os
import subprocess
from psycopg.conninfo import make_conninfo

ROOT=Path(__file__).resolve().parents[2]


def test_lens_factory_role_isolation(database):
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE=str(ROOT/'.runtime/go-cache'))
    result=subprocess.run(['go','test','./internal/v2input','-run','^TestIntegrationLensFactoryRoleIsolation$',
        '-count=1','-timeout=60s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=70)
    assert result.returncode==0,result.stdout+result.stderr
