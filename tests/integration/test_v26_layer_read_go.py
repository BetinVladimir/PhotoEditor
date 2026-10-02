"""Run actual API-role layer-document resolution against isolated full migrations."""
import os
import subprocess
from psycopg.conninfo import make_conninfo
from conftest import ROOT

def test_v26_layer_read_api_role(database):
    env=os.environ.copy()
    env['PHOTO_TEST_DATABASE_URL']=make_conninfo(**database)
    env['GOCACHE']=str(ROOT/'.cache'/'go')
    result=subprocess.run(['go','test','./internal/v2draft','-run','^TestIntegrationReadApprovalDraftPinsCurrentDocument$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
