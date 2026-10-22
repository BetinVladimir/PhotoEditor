"""Actual pending output reservation + stored versions + production cleaner.

Owner-seeded retirement/cleanup expiry; object PUT is a fixture, not HTTP fault.
"""
from pathlib import Path as _PhotoRepositoryPath
import json
import os
import subprocess
from pathlib import Path
from io import BytesIO
from uuid import uuid4
import psycopg
from psycopg.conninfo import make_conninfo
from test_gateway_outputs import setup
from test_v27_native_protection_gc import lens_objects


def test_pending_output_physical_cleanup_releases_quota_after_storage(database,lens_objects):
    client,bucket=lens_objects
    with psycopg.connect(**database) as db:
        case=setup(db)
        db.execute('SET LOCAL ROLE photo_gateway')
        ticket=db.execute('SELECT * FROM photo.begin_output(%s,1,1,%s,%s,%s,%s,2,%s,%s,%s)',(case['task'],case['worker'],'f'*64,uuid4(),'verified','a'*64,'application/json',bucket)).fetchone()
        assert ticket[0]=='created'
    key=ticket[3]
    for body in (b'{}',b'[]'):
        client.put_object(bucket,key,BytesIO(body),len(body),content_type='application/json')
    neighbor=key+'-neighbor'
    kept=client.put_object(bucket,neighbor,BytesIO(b'keep'),4,content_type='application/json').version_id
    with psycopg.connect(**database) as db:
        assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()==(2,0)
        assert db.execute('SELECT count(*) FROM photo.artifacts WHERE id=%s',(ticket[2],)).fetchone()==(0,)
        db.execute("UPDATE photo.tasks SET state='cancelled',lease_until=NULL WHERE id=%s",(case['task'],))
        db.execute("UPDATE photo.output_uploads SET cleanup_after=clock_timestamp()-interval '1 second' WHERE id=%s",(ticket[1],))
    cfg=client._photo_test_config;root=Path(__file__).resolve().parents[2]
    env={**os.environ,'PHOTO_TEST_ORPHAN_PHYSICAL':'1','PHOTO_TEST_DATABASE_URL':make_conninfo(**database),'PHOTO_TEST_MINIO_URL':cfg['Endpoint'],'PHOTO_TEST_MINIO_BUCKET':bucket,'PHOTO_TEST_MINIO_ACCESS':cfg['AccessKey'],'PHOTO_TEST_MINIO_SECRET':cfg['SecretKey'],'GOCACHE':str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache')}
    result=subprocess.run(['go','test','-tags','integration','./internal/maintenance','-run','^TestIntegrationOutputOrphanPhysicalCleanup$','-count=1','-timeout=45s','-v'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=60)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'OUTPUT_ORPHAN_PHYSICAL_CLEANED_OK' in result.stdout
    assert not [v for v in client.list_objects(bucket,prefix=key,recursive=True,include_version=True) if v.object_name==key]
    assert client.stat_object(bucket,neighbor,version_id=kept).size==4
    with psycopg.connect(**database) as db:
        assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()==(0,0)
        assert db.execute('SELECT count(*) FROM photo.artifacts WHERE id=%s',(ticket[2],)).fetchone()==(0,)
