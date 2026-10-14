"""Actual SQL freeze plus versioned S3 IO; qualification remains controlled."""
from pathlib import Path
from io import BytesIO
from hashlib import sha256
import json
import os
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
from render_full_freeze_fixture import seed_full_render_freeze
from test_v25_lens_objectstore import lens_objects


def test_freeze_reads_pinned_documents_and_recovers_s3_publication(database,lens_objects):
    client,bucket=lens_objects
    def publish(key,payload):
        version=client.put_object(bucket,key,BytesIO(payload),len(payload),content_type='application/json',metadata={'sha256':sha256(payload).hexdigest()}).version_id
        assert version and version!='null'
        latest=client.put_object(bucket,key,BytesIO(b'{}'),2,content_type='application/json').version_id
        assert latest and latest!=version
        return version
    with psycopg.connect(**database) as db:
        fixture=seed_full_render_freeze(db,bucket=bucket,publish_document=publish)
    root=Path(__file__).resolve().parents[2]
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_RENDER_FREEZE_FRESH=json.dumps(fixture),PHOTO_RENDER_FREEZE_S3_CONFIG=json.dumps(client._photo_test_config),GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationRenderFreezeS3Recovery$','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=90)
    assert result.returncode==0,result.stdout+result.stderr
