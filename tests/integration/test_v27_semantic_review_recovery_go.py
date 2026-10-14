"""Run scheduler recovery against a private database with all review migrations."""
import os
from pathlib import Path
import subprocess
from psycopg.conninfo import make_conninfo


def test_recovery_with_semantic_child_settlement_installed(database):
    root=Path(__file__).resolve().parents[2]
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/workflow','-run','^TestIntegrationRecovery$','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr
