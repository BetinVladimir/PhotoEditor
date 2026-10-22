"""V2 run output project scope on an isolated fully migrated database."""
from pathlib import Path as _PhotoRepositoryPath
import os
import subprocess
from psycopg.conninfo import make_conninfo
from conftest import ROOT
from test_v25_lens_objectstore import lens_objects


def test_v2_run_output_scope_without_collection(database):
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
    result=subprocess.run(['go','test','./internal/run','-run','^TestIntegrationV2OutputsProjectWithoutCollection$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr


def test_output_service_real_signed_version(database, lens_objects):
    from io import BytesIO
    from hashlib import sha256
    import base64
    import json
    from PIL import Image
    client, bucket = lens_objects
    encoded = BytesIO()
    Image.new('RGB', (8, 8), (31, 127, 221)).save(encoded, format='JPEG')
    payload = encoded.getvalue()
    first = client.put_object(bucket, 'presentation.jpg', BytesIO(payload), len(payload),
                              content_type='image/jpeg').version_id
    replacement = b'wrong latest version'
    second = client.put_object(bucket, 'presentation.jpg', BytesIO(replacement),
                               len(replacement), content_type='image/jpeg').version_id
    assert first and first != second
    fixture = dict(Config=client._photo_test_config, Version=first,
                   SHA256=sha256(payload).hexdigest(), Bytes=len(payload),
                   Payload=base64.b64encode(payload).decode('ascii'))
    env = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
               PHOTO_OUTPUT_S3_FIXTURE=json.dumps(fixture),
               GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
    result = subprocess.run(['go', 'test', './internal/run', '-run',
        '^TestIntegrationV2OutputsProjectWithoutCollection$', '-count=1', '-v'],
        cwd=ROOT/'backend', env=env, capture_output=True, text=True, timeout=90)
    assert result.returncode == 0, result.stdout + result.stderr
    assert '--- PASS: TestIntegrationV2OutputsProjectWithoutCollection' in result.stdout
