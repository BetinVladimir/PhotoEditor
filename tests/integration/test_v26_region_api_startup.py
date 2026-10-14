import os
from pathlib import Path
import subprocess
from psycopg.conninfo import make_conninfo


def test_region_api_startup_installs_separate_roles_and_rejects_partial_migrations(database):
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./cmd/photo','-run','^TestIntegration(RegionAdmissionBuilder|RunPresentationSchema)$','-count=1'],
        cwd=Path(__file__).resolve().parents[2]/'backend',env=env,capture_output=True,text=True,timeout=45)
    assert result.returncode==0,result.stdout+result.stderr
