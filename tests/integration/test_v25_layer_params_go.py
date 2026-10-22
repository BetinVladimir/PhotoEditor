"""Positive/revoked Go SQL params binding, on synthetic verified metadata."""
from pathlib import Path as _PhotoRepositoryPath
from pathlib import Path
import json
import os
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
from layer_original_fixture import seed_original_composition

ROOT=Path(__file__).resolve().parents[2]


def test_composition_params_go_positive_and_revoked(database):
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():case=seed_original_composition(db)
        def check():
            env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_COMPOSITION_PARAMS_CASE=json.dumps(case),GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
            result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationOriginalCompositionParamsScope$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=45)
            assert result.returncode==0,result.stdout+result.stderr
        check()
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
        case['Revoked']=True
        check()
