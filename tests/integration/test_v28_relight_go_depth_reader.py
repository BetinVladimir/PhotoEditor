"""Real Go scheduler transactions and versioned MinIO original Params reads."""
from physical_go_child_evidence import run_evidenced_go
from pathlib import Path
from io import BytesIO
import json
import os
import subprocess
import pytest
import psycopg
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import canonical_bytes
from test_v25_lens_objectstore import lens_objects
from test_v27_semantic_final_producer_gate import fixture
from test_v28_relight_draft_intent import check_relight_intent

ROOT=Path(__file__).resolve().parents[2]


@pytest.mark.parametrize("producer",["depth","person","manual","manual_external"])
def test_go_relight_depth_reader_real_sql_and_original_object(database,lens_objects,tmp_path,producer):
    client,bucket=lens_objects
    def source_builder(artifact,original_sha):
        image=json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
        image['canvas'].update(width=5,height=5,source_sha256=original_sha,space_id='oriented')
        pixel=artifact('a'*64,role='working_rgb',media='application/octet-stream')
        image['pixels']=dict(artifact_id=str(pixel),sha256='a'*64)
        return canonical_bytes(image)
    with psycopg.connect(**database) as setup:
        seed=fixture(setup,snapshot_authority=True,source_builder=source_builder,qualification_objects=(client,bucket))
        saved=check_relight_intent(setup,seed,verify_depth=producer=="depth",verify_person=producer=="person",verify_manual=producer.startswith("manual"),external_vectors=producer=="manual_external",objects=(client,bucket))
    retained=saved['retained']
    # A mutable latest object cannot replace the original SQL-pinned version.
    newer=b'newer invalid original Params'
    client.put_object(bucket,retained['key'],BytesIO(newer),len(newer),content_type='application/json')
    seed_file=tmp_path/'go-relight-depth-reader.json'
    seed_file.write_text(json.dumps(dict(tenant=str(saved['tenant']),run=str(saved['run']),intent=saved['request'],
        source=saved['scope']['source_body'].decode(),depth=retained.get('body',b'').decode(),
        original=retained['original'].decode(),original_id=str(retained['params']),producer="manual" if producer.startswith("manual") else producer,
        subject=retained['body'].decode() if producer=='person' else retained.get('target_raw',b'').decode(),
        protected=retained.get('protected_body',retained.get('guard_raw',b'')).decode())))
    credentials=client._provider.retrieve()
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_RELIGHT_DEPTH_SEED=str(seed_file),
        PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,PHOTO_TEST_MINIO_BUCKET=bucket,
        PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,
        GOCACHE=str(ROOT/'work/go-cache'))
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightDepthReader$',
        '-count=1','-timeout=60s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=90)
    assert result.returncode==0,result.stdout+result.stderr
