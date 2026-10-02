"""Actual scheduler pgx LoadBatch across durable phases and revoked scope."""
import os
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
from conftest import ROOT
from test_v26_render_finalizer_tenant_scope import seed_failed_batch


def test_go_manifest_recovery_across_phases(database):
    with psycopg.connect(**database) as db:
        _,batch=seed_failed_batch(db)
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_RENDER_BATCH=str(batch),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationRenderBatchManifestRecoveryAcrossPhases$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr
