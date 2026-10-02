"""Execute actual Go catalog SQL against committed isolated authority metadata."""
import json
import os
from pathlib import Path
import subprocess
import psycopg
import pytest
import test_v22_scene_materialization as scene
from psycopg.conninfo import make_conninfo
import render_descriptor_scope_fixture as fixture
from test_v22_scene_materialization import test_adopted_scene_plan_materializes_once_and_rejects_rebinding as materialize


def test_go_render_catalog_authority(database, monkeypatch, tmp_path):
    original = fixture.assert_render_descriptor_authority
    root = Path(__file__).resolve().parents[2]
    invoked = []
    class Verified(Exception):
        pass
    original_tenant = scene.tenant
    def create_tenant(db, email):
        with db.transaction():
            return original_tenant(db,email)
    monkeypatch.setattr(scene,'tenant',create_tenant)
    def verify(db, *args):
        case = original(db, *args, persist=True)
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) SELECT tenant_id,id,attempt,fencing_token,gen_random_uuid(),clock_timestamp(),'cuda','{}'::jsonb FROM photo.tasks WHERE tenant_id=%s AND attempt=1 ON CONFLICT DO NOTHING",(case['Tenant'],))
        db.commit()
        case['Epoch'] = db.execute('SELECT auth_epoch FROM photo.users WHERE id=%s',(case['Actor'],)).fetchone()[0]
        path = tmp_path/'render-catalog.json'
        path.write_text(json.dumps(case))
        env = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
                   PHOTO_RENDER_CATALOG_FIXTURE=str(path),GOCACHE=str(root/'work/go-cache'))
        result = subprocess.run(['go','test','./internal/v2draft','-run','TestIntegrationRenderCatalogFrameAuthority','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=90)
        assert result.returncode==0,result.stdout+result.stderr
        invoked.append(True)
        raise Verified()
    monkeypatch.setattr(fixture,'assert_render_descriptor_authority',verify)
    with psycopg.connect(**database,autocommit=False) as db:
        with pytest.raises(Verified):
            materialize(db)
    assert invoked==[True]
