"""Real wire mTLS, SQL output repository and versioned S3 upload/replay."""
from pathlib import Path
import os
import subprocess
import pytest
import psycopg
from psycopg.types.json import Jsonb
from psycopg.conninfo import make_conninfo
from test_gateway_outputs import setup
from test_v25_lens_objectstore import lens_objects

ROOT=Path(__file__).resolve().parents[2]


def run_wire(database,lens_objects,large=False):
    with psycopg.connect(**database) as connection:
        with connection.transaction():
            item=setup(connection)
            if large:
                size=2**32+1024*1024+17
                connection.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',%s::jsonb) WHERE id=%s",(str(size+1024),item['tenant']))
                connection.execute('UPDATE photo.tasks SET execution_profile=execution_profile||%s WHERE id=%s',(Jsonb({'resource_estimate':{'output_bytes':size+1}}),item['task']))
                connection.execute("UPDATE photo.operation_output_policies SET max_bytes=%s WHERE operation='asset.verify' AND version='1' AND role='verified'",(size+1,))
    client,_=lens_objects
    credentials=client._provider.retrieve()
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_GATEWAY_TASK=str(item['task']),
        PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,PHOTO_TEST_MINIO_ACCESS=credentials.access_key,
        PHOTO_TEST_MINIO_SECRET=credentials.secret_key,GOCACHE=str(ROOT/'.runtime/go-cache'))
    env['PHOTO_RUN_GATEWAY_WIRE_LARGE_IO']='1' if large else '0'
    result=subprocess.run(['go','test','./internal/gateway','-run','^TestIntegrationWireSQLUpload$',
        '-count=1','-timeout=300s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=310)
    assert result.returncode==0,result.stdout+result.stderr


def test_wire_sql_s3_upload(database,lens_objects):
    run_wire(database,lens_objects)


@pytest.mark.skipif(os.environ.get('PHOTO_RUN_GATEWAY_WIRE_LARGE_IO')!='1',reason='opt-in >4GiB real mTLS/SQL/S3')
def test_large_wire_sql_s3_upload(database,lens_objects):
    run_wire(database,lens_objects,large=True)
