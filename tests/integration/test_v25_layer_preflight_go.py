"""Actual Go composition preflight through isolated PostgreSQL service roles."""
from pathlib import Path
import os
import subprocess
from psycopg.conninfo import make_conninfo

ROOT=Path(__file__).resolve().parents[2]


def test_composition_preflight_go_role_isolation(database):
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationCompositionPreflightRoleIsolation$','-count=1'],
        cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=45)
    assert result.returncode==0,result.stdout+result.stderr
