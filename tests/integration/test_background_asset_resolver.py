from pathlib import Path as _PhotoRepositoryPath
import os
from pathlib import Path
import subprocess
from psycopg.conninfo import make_conninfo


def test_portable_background_resolver_enforces_live_sql_scope(database):
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
    result=subprocess.run(['go','test','./internal/v2draft','-run','^TestIntegrationPortableBackgroundAsset$','-count=1'],cwd=Path(__file__).resolve().parents[2]/'backend',env=env,capture_output=True,text=True,timeout=45)
    assert result.returncode==0,result.stdout+result.stderr
