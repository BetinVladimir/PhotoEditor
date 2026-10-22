"""Real auth/HTTP boundary with controlled queue, not real renderer enqueue."""
from pathlib import Path as _PhotoRepositoryPath
from pathlib import Path
import os
import subprocess
from psycopg.conninfo import make_conninfo


def test_region_http_authenticated_contract(database):
    root=Path(__file__).resolve().parents[2]
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
    result=subprocess.run(['go','test','./internal/httpapi','-run','^TestIntegrationRegionHTTP','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr
