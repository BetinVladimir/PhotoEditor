"""Actual API-role Mask.Create service and immutable idempotency receipt."""
from pathlib import Path as _PhotoRepositoryPath
import json
import os
import subprocess
from pathlib import Path
import psycopg
from psycopg.conninfo import make_conninfo
from layer_original_fixture import seed_original_composition


def test_mask_create_service_persists_once(database):
    with psycopg.connect(**database) as db:
        case=seed_original_composition(db)
    base=json.loads(case['Params'])['base'];root=Path(__file__).resolve().parents[2]
    fixture={k:case[k] for k in ('Tenant','Actor','Session')};fixture.update(Version=base['asset_version_id'],SourceSHA=base['source']['sha256'])
    env={**os.environ,'PHOTO_TEST_DATABASE_URL':make_conninfo(**database),'PHOTO_TEST_MASK_CREATE':json.dumps(fixture),'GOCACHE':str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache')}
    result=subprocess.run(['go','test','./internal/interactive','-run','^TestIntegrationMaskCreateReplay$','-count=1','-timeout=30s','-v'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=45)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'MASK_CREATE_REPLAY_OK' in result.stdout
    with psycopg.connect(**database) as db:
        assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND kind='mask'",(case['Tenant'],)).fetchone()==(1,)
        assert db.execute("SELECT count(*) FROM photo.interactive_tasks WHERE tenant_id=%s AND kind='mask_manual'",(case['Tenant'],)).fetchone()==(1,)
