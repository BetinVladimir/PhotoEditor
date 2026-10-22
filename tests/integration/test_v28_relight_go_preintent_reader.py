"""Original documents read before any relight intent, using actual Go/SQL/S3."""
from physical_go_child_evidence import run_evidenced_go
from pathlib import Path
from io import BytesIO
from uuid import uuid4
from hashlib import sha256
import json
import os
import subprocess
import pytest
import psycopg
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from test_v25_lens_objectstore import lens_objects
from test_v27_semantic_final_producer_gate import fixture
from test_v28_relight_depth import seed_depth_inputs
from test_v28_relight_person import seed_person_inputs
from test_v28_relight_manual import seed_manual_inputs

ROOT=Path(__file__).resolve().parents[2]


@pytest.mark.parametrize('kind',['depth','person','manual_target','manual_guard','manual_external','manual_target_full','manual_guard_full','manual_external_full'])
def test_go_preintent_original_documents(database,lens_objects,tmp_path,kind):
    client,bucket=lens_objects
    def source_builder(artifact,original_sha):
        image=json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
        image['canvas'].update(width=5,height=5,source_sha256=original_sha,space_id='oriented')
        pixel=artifact('a'*64,role='working_rgb',media='application/octet-stream')
        image['pixels']=dict(artifact_id=str(pixel),sha256='a'*64)
        return canonical_bytes(image)
    def publish(aid,raw,role):
        media='application/octet-stream' if role in ('relative_depth','depth_invalid','mask_target','mask_protected') else 'image/png' if role.endswith('_png') else 'application/json'
        version=client.put_object(bucket,str(aid),BytesIO(raw),len(raw),content_type=media,metadata={'sha256':sha256(raw).hexdigest()}).version_id
        return bucket,str(aid),version
    retained={}
    with psycopg.connect(**database) as setup:
        tenant,*_,scope=fixture(setup,snapshot_authority=True,source_builder=source_builder,qualification_objects=(client,bucket))
        if kind=='depth':seed_depth_inputs(setup,tenant,scope,retained,objects=(client,bucket),physical=True,publish=publish)
        elif kind=='person':seed_person_inputs(setup,tenant,scope,retained,objects=(client,bucket),publish=publish)
        else:seed_manual_inputs(setup,tenant,scope,retained,physical=True,publish=publish,external='external' in kind)
    newref=lambda:dict(artifact_id=str(uuid4()),sha256='a'*64)
    source=scope['source_body'].decode()
    params=dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash=canonical_hash(json.loads(source)['canvas'])),depth=scope.get('depth_ref',newref()),subject=scope.get('subject_ref',newref()),protected=scope.get('protected_ref',newref()),calibration=newref(),lights=[dict(x=.5,y=.5,radius=.2,ev=1)],strength=.7)
    original=retained.get('original',retained.get('raw_objects',{}).get(retained['params']))
    key=retained.get('key',str(retained['params']))
    newer=b'newer invalid Params'
    client.put_object(bucket,key,BytesIO(newer),len(newer),content_type='application/json')
    seed_file=tmp_path/'preintent.json'
    seed_file.write_text(json.dumps(dict(tenant=str(tenant),project=str(scope['project']),snapshot=str(scope['snapshot']),actor=str(scope['actor']),session=str(scope['session']),kind='manual_guard' if 'external' in kind else kind.removesuffix('_full'),params=params,source=source,original=original.decode(),original_id=str(retained['params']),external='external' in kind,full=kind.endswith('_full'))))
    credentials=client._provider.retrieve()
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_RELIGHT_PREINTENT_SEED=str(seed_file),PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,PHOTO_TEST_MINIO_BUCKET=bucket,PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,GOCACHE=str(ROOT/'work/go-cache'))
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightPreintentReader$','-count=1','-timeout=60s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=90)
    assert result.returncode==0,result.stdout+result.stderr
