"""Actual Go API service cancellation on all migrations; isolated committed DB."""
import os
import subprocess
from psycopg.conninfo import make_conninfo
from conftest import ROOT


def test_go_run_cancel_and_batch_propagation(database):
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/run','-run','^TestIntegrationCancelRunIsAuthorizedAuditedAndIdempotent$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr
