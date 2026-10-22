"""Actual Go transport serialization and atomic forward-upgrade boundaries."""
import copy
import json
import os
from pathlib import Path
import subprocess
import psycopg
from psycopg import sql
import pytest

ROOT=Path(__file__).resolve().parents[2]
NAMES=('semantic_preapply_preview_transport_ready','semantic_shown_exposure_transport_ready','semantic_removal_preview_transport_ready','semantic_shown_removal_transport_ready','semantic_background_cleanup_preview_transport_ready','semantic_shown_background_cleanup_transport_ready','semantic_retouch_preview_transport_ready','semantic_shown_retouch_transport_ready')
SIGNATURES=tuple('photo.'+n+'(uuid,jsonb,jsonb,jsonb,jsonb)' for n in NAMES)
PATCH=(ROOT/'backend/db/migrations/02582_semantic_transport_manifest_canonical.sql').read_text().split('-- +goose Down')[0]
OLD=PATCH.split('$old$')[1]
NEW=PATCH.split('$new$')[1]

@pytest.fixture(scope='module')
def go_transports(tmp_path_factory):
    output=tmp_path_factory.mktemp('semantic-go-transport')/'inputs.json'
    env=dict(os.environ,PHOTO_TEST_MANIFEST_FIXTURES=str(output),GOPROXY='off',GOSUMDB='off',GOTOOLCHAIN='local')
    subprocess.run(['go','test','-p','2','./internal/semanticreview','-run','^TestWriteSemanticTransportManifestSQLFixtures$','-count=1'],cwd=ROOT/'backend',env=env,check=True,timeout=120)
    return json.loads(output.read_text())

@pytest.mark.parametrize('index',range(8))
def test_go_transport_and_tamper_boundaries(database,go_transports,index):
    value=go_transports[index]
    with psycopg.connect(**database) as db:
        def ready(row):
            return db.execute(sql.SQL('SELECT photo.{}(%s,%s::jsonb,%s::jsonb,%s::jsonb,%s::jsonb)').format(sql.Identifier(NAMES[index])),(row['parent'],*(json.dumps(row[k]) for k in ('params','node','transport','inventory')))).fetchone()[0]
        assert ready(value) is True
        for mutate in ('manifest_sha','chunk_sha','task_uuid','cardinality','body'):
            bad=copy.deepcopy(value)
            if mutate=='manifest_sha':bad['transport']['manifest']['sha256']='0'*64
            elif mutate=='chunk_sha':bad['transport']['chunks'][0]['sha256']='0'*64
            elif mutate=='task_uuid':bad['transport']['task_id']='00000000-0000-0000-0000-000000000000'
            elif mutate=='cardinality':bad['transport']['entry_count']+=1
            else:bad['inventory']['entries'][0]['ref']['sha256']='0'*64
            assert ready(bad) is False,mutate
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
            assert db.execute('SELECT has_function_privilege(%s,to_regprocedure(%s),%s)',(role,SIGNATURES[index],'EXECUTE')).fetchone()==(False,)

@pytest.mark.migration_head(2581)
@pytest.mark.parametrize('drift',[False,True])
def test_manifest_upgrade_identity_and_last_patch_rollback(database,drift):
    with psycopg.connect(**database) as db:
        def snapshot():
            return [db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef,provolatile,pg_get_functiondef(oid),prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(name,)).fetchone() for name in SIGNATURES]
        if drift:
            last=snapshot()[-1]
            assert last[-1].count(OLD)==1
            # Failure on ordinal8 must roll back all seven preceding replacements.
            db.execute(last[-2].replace(last[-1],last[-1].replace(OLD,NEW)))
            db.commit()
        before=snapshot()
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='SEMANTIC_MANIFEST_SOURCE_SHAPE'):
                with db.transaction():db.execute(PATCH)
            assert snapshot()==before
        else:
            db.execute(PATCH)
            after=snapshot()
            for original,current in zip(before,after):
                assert original[:6]==current[:6]
                assert current[-1]==original[-1].replace(OLD,NEW)
                assert current[-2]==original[-2].replace(OLD,NEW)
