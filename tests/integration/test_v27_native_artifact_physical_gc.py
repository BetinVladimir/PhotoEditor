"""Actual exact-version artifact GC with owner-seeded native registry metadata.

This does not execute/admit native inference. Retention state/expiry is fixture
setup; lease expiry is waited in real time. The API attach probe is serialized
at the actual GC remove boundary, not an independent reader-process race.
"""
import json
import os
import subprocess
import time
from datetime import datetime,timezone
from pathlib import Path
from io import BytesIO
from hashlib import sha256
from uuid import uuid4
import psycopg
from psycopg.conninfo import make_conninfo
from test_v27_native_protection_gc import seed_native_registry_metadata,lens_objects


def test_native_protection_physical_gc_retention_lease_and_exact_version(database,lens_objects):
    client,bucket=lens_objects
    with psycopg.connect(**database) as db:
        case,root,pixels,report,document=seed_native_registry_metadata(db,objects=lens_objects)
        tenant=case['Tenant']
        def stored(artifact):
            key,version,digest,size,media=db.execute('SELECT object_key,object_version,sha256,bytes,media_type FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()
            return dict(Key=key,VersionID=version,SHA256=digest,Bytes=size,ContentType=media)
        target,retained_report=stored(pixels),stored(report)
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=%s",(pixels,))
    newer=b'newer untracked object version must survive exact-version GC'
    sibling=dict(target,SHA256=sha256(newer).hexdigest(),Bytes=len(newer))
    sibling['VersionID']=client.put_object(bucket,target['Key'],BytesIO(newer),len(newer),content_type=target['ContentType'],metadata={'sha256':sibling['SHA256']}).version_id
    cfg=client._photo_test_config;root_dir=Path(__file__).resolve().parents[2]
    fixture=dict(Tenant=tenant,Artifact=str(pixels),Target=target,Sibling=sibling,Report=retained_report)
    def clean(remove):
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_NATIVE_ARTIFACT_GC=json.dumps(dict(fixture,Remove=remove)),PHOTO_TEST_MINIO_URL=cfg['Endpoint'],PHOTO_TEST_MINIO_BUCKET=bucket,PHOTO_TEST_MINIO_ACCESS=cfg['AccessKey'],PHOTO_TEST_MINIO_SECRET=cfg['SecretKey'],GOCACHE='/private/tmp/photoeditor-go-cache')
        result=subprocess.run(['go','test','-tags','integration','./internal/maintenance','-run','^TestIntegrationV27NativeArtifactPhysicalGC$','-count=1','-timeout=60s','-v'],cwd=root_dir/'backend',env=env,capture_output=True,text=True,timeout=90)
        assert result.returncode==0,result.stdout+result.stderr
        assert ('V27_NATIVE_ARTIFACT_PHYSICAL_GC_OK' if remove else 'V27_NATIVE_ARTIFACT_PHYSICAL_RETAINED_OK') in result.stdout
    clean(False) # Live native registry root prevents collection.
    with psycopg.connect(**database) as db:
        db.execute("UPDATE photo.v2_draft_uploads SET state='retired' WHERE tenant_id=%s AND document_id=%s",(tenant,document))
        db.execute('UPDATE photo.artifacts SET expires_at=NULL WHERE id=%s',(pixels,))
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
        lease=db.execute("INSERT INTO photo.artifact_leases(tenant_id,artifact_id,holder_type,holder_id,expires_at) VALUES(%s,%s,'physical-gc-test',%s,clock_timestamp()+interval '10 seconds') RETURNING expires_at",(tenant,pixels,uuid4())).fetchone()[0]
        db.execute('SET LOCAL ROLE NONE')
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=%s",(pixels,))
    clean(False) # Real API lease still prevents physical deletion after retirement.
    delay=(lease-datetime.now(timezone.utc)).total_seconds()+.05
    assert delay<15
    if delay>0:time.sleep(delay)
    clean(True)
    versions=list(client.list_objects(bucket,prefix=target['Key'],recursive=True,include_version=True))
    assert [(v.object_name,v.version_id) for v in versions]==[(target['Key'],sibling['VersionID'])]
    response=client.get_object(bucket,sibling['Key'],version_id=sibling['VersionID'])
    try:assert response.read()==newer
    finally:response.close();response.release_conn()
    with psycopg.connect(**database) as db:
        assert db.execute('SELECT state,object_version FROM photo.artifacts WHERE id=%s',(pixels,)).fetchone()==('deleted',target['VersionID'])
        assert db.execute('SELECT count(*) FROM photo.semantic_native_protections WHERE tenant_id=%s',(tenant,)).fetchone()[0]==1
