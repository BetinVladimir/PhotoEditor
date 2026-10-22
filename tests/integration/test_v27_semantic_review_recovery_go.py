"""Run scheduler recovery against a private database with all review migrations."""
from pathlib import Path as _PhotoRepositoryPath
import os
from pathlib import Path
import subprocess
from psycopg.conninfo import make_conninfo


def test_recovery_with_semantic_child_settlement_installed(database):
    root=Path(__file__).resolve().parents[2]
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
    result=subprocess.run(['go','test','./internal/workflow','-run','^TestIntegrationRecovery$','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr
