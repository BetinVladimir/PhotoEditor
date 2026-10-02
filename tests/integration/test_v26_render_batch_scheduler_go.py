"""Run actual Go scheduler adapter with two committed, isolated metadata batches."""
import os
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
from conftest import ROOT
from test_v26_render_finalizer_tenant_scope import seed_failed_batch


def test_go_batch_pass_updates_terminal_and_intermediate(database):
    with psycopg.connect(**database) as db:
        _,terminal=seed_failed_batch(db)
        tenant,queued=seed_failed_batch(db)
        db.execute("UPDATE photo.runs SET status='queued' WHERE tenant_id=%s AND kind='v2'",(tenant,))
        db.execute("UPDATE photo.tasks SET state='blocked',terminal_reason=NULL WHERE tenant_id=%s",(tenant,))
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_BATCH_TERMINAL=str(terminal),PHOTO_BATCH_QUEUED=str(queued),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/workflow','-run','^TestIntegrationRenderBatchPassFinalizesAndSynchronizes$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr
