"""Actual Go/API/SQL binary lifecycle with injected object transport."""
from pathlib import Path
import json
import os
import subprocess
import psycopg
import pytest
from psycopg.conninfo import make_conninfo
from layer_original_fixture import seed_original_composition
from test_v25_lens_objectstore import lens_objects
from hashlib import sha256
from io import BytesIO
import numpy as np

ROOT=Path(__file__).resolve().parents[2]


def test_mask_pixels_go_publication_and_recovery(database):
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            case=seed_original_composition(db)
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(case['Tenant'],))
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_MASK_PIXELS_CASE=json.dumps(case),GOCACHE='/private/tmp/photoeditor-go-cache')
        result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationUserMaskPixelsPublisher$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=45)
        assert result.returncode==0,result.stdout+result.stderr
        rows=db.execute("SELECT u.bytes,u.state,a.media_type,a.output_role,u.mask_width,u.mask_height FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.upload_contract='MaskPixels'",(case['Tenant'],)).fetchall()
        assert len(rows)==3
        assert all(row[1:]==('ready','application/x-npy','mask_pixels',3,2) for row in rows)
        assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(sum(row[0] for row in rows),0)


def test_mask_pixels_real_s3_publication(database,lens_objects):
    client,bucket=lens_objects
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            case=seed_original_composition(db)
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(case['Tenant'],))
        case['Objects']=client._photo_test_config
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_MASK_PIXELS_S3_CASE=json.dumps(case),GOCACHE='/private/tmp/photoeditor-go-cache')
        result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationUserMaskPixelsS3$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=50)
        assert result.returncode==0,result.stdout+result.stderr
        rows=db.execute("SELECT a.object_key,a.object_version,a.sha256,a.bytes FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.upload_contract='MaskPixels' AND u.state='ready'",(case['Tenant'],)).fetchall()
        assert len(rows)==1
        key,version,digest,size=rows[0]
        assert version and version!='null'
        reader=client.get_object(bucket,key,version_id=version)
        try:body=reader.read()
        finally:reader.close();reader.release_conn()
        assert len(body)==size and sha256(body).hexdigest()==digest
        assert np.array_equal(np.load(BytesIO(body),allow_pickle=False),np.array([[0,.5,1],[1,.25,0]],dtype='<f4'))
        assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(size,0)

        # A completed upload remains stored, but a revoked session loses access.
        artifact=db.execute("SELECT artifact_id FROM photo.v2_draft_uploads WHERE tenant_id=%s AND upload_contract='MaskPixels'",(case['Tenant'],)).fetchone()[0]
        asset_version=json.loads(case['Params'])['base']['asset_version_id']
        args=(case['Tenant'],asset_version,case['Actor'],case['Session'],artifact,digest,3,2)
        probe='SELECT * FROM photo.read_v25_user_mask_pixels(%s,%s,%s,%s,%s,%s,%s,%s)'
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
            assert len(db.execute(probe,args).fetchall())==1
            db.execute("SELECT set_config('app.tenant_id','',true)")
            assert db.execute(probe,args).fetchall()==[]
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
            assert db.execute(probe,args).fetchall()==[]
        denied=dict(Tenant=case['Tenant'],Actor=case['Actor'],Session=case['Session'],Version=asset_version,Bucket=bucket,Ref=dict(artifact_id=str(artifact),sha256=digest))
        env['PHOTO_MASK_PIXELS_REVOKED_CASE']=json.dumps(denied)
        revoked=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationUserMaskPixelsRevokedScope$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=30)
        assert revoked.returncode==0,revoked.stdout+revoked.stderr
        from psycopg import errors
        with pytest.raises(errors.InsufficientPrivilege):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                db.execute(probe,args)


def test_verified_new_user_mask_real_s3(database,lens_objects):
    from photo_worker.contracts.base import canonical_bytes
    from uuid import uuid4
    client,bucket=lens_objects
    source=json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
    body=canonical_bytes(source)
    key='source-'+uuid4().hex+'.json'
    version=client.put_object(bucket,key,BytesIO(body),len(body),content_type='application/json',metadata={'sha256':sha256(body).hexdigest()}).version_id
    metadata=dict(bucket=bucket,key=key,bytes=len(body),version=version,sha256=sha256(body).hexdigest())
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            case=seed_original_composition(db,source_object=metadata,canvas_hash=sha256(canonical_bytes(source['canvas'])).hexdigest())
            db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','100000000') WHERE id=%s",(case['Tenant'],))
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(case['Tenant'],))
        case.update(Objects=client._photo_test_config,SourceBody=body.decode())
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_VERIFIED_MASK_S3_CASE=json.dumps(case),GOCACHE='/private/tmp/photoeditor-go-cache')
        result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationVerifiedUserMaskS3$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=50)
        assert result.returncode==0,result.stdout+result.stderr
        rows=db.execute("SELECT u.upload_contract,u.bytes,a.object_key,a.object_version FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.bucket=%s AND u.state='ready' ORDER BY u.upload_contract",(case['Tenant'],bucket)).fetchall()
        assert len(rows)==3 and [r[0] for r in rows]==['Document','Document','MaskPixels']
        # Identify the bound MaskRef separately from its additional composition params.
        root_key=db.execute("SELECT a.object_key FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.user_mask_binding IS NOT NULL",(case['Tenant'],)).fetchone()[0]
        rows.sort(key=lambda r:0 if r[2]==root_key else 1 if r[0]=='MaskPixels' else 2)
        usage=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()
        output_reserved=db.execute("SELECT sum(bytes) FROM photo.quota_reservations WHERE tenant_id=%s AND state='reserved'",(case['Tenant'],)).fetchone()[0]
        assert usage==(sum(r[1] for r in rows),output_reserved)
        reader=client.get_object(bucket,rows[0][2],version_id=rows[0][3])
        try:mask=json.loads(reader.read())
        finally:reader.close();reader.release_conn()
        artifact=db.execute('SELECT id,sha256 FROM photo.artifacts WHERE tenant_id=%s AND object_key=%s',(case['Tenant'],rows[1][2])).fetchone()
        assert mask['artifact']==dict(artifact_id=str(artifact[0]),sha256=artifact[1])
        assert mask['canvas']==source['canvas'] and mask['transform_chain']==source['transform_chain']

        binding=db.execute("SELECT user_mask_binding,artifact_id FROM photo.v2_draft_uploads WHERE tenant_id=%s AND bucket=%s AND upload_contract='Document' AND user_mask_binding IS NOT NULL",(case['Tenant'],bucket)).fetchone()
        assert binding[0]['mask']==mask
        base=json.loads(case['Params'])['base']
        assert binding[0]['source']==base['source'] and binding[0]['canvas_hash']==base['canvas_hash']
        # Binary upload retirement must not collect a dependency of a live root.
        binary_id=artifact[0]
        db.execute("UPDATE photo.v2_draft_uploads SET state='retired' WHERE tenant_id=%s AND artifact_id=%s",(case['Tenant'],binary_id))
        assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(case['Tenant'],binary_id)).fetchone()[0] is True
        db.execute("UPDATE photo.v2_draft_uploads SET state='retired' WHERE tenant_id=%s AND artifact_id=%s",(case['Tenant'],binding[1]))
        assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(case['Tenant'],binary_id)).fetchone()[0] is False
        # A persisted document closure protects the root after both uploads retire.
        # The document row is synthetic metadata, not a recipe save acceptance test.
        with db.transaction():
            stream=db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",(case['Tenant'],case['Project'],base['asset_version_id'])).fetchone()[0]
            document=db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'LayerStack',%s,%s) RETURNING id",(case['Tenant'],case['Project'],stream,case['Artifact']['artifact_id'],case['Artifact']['sha256'])).fetchone()[0]
            root_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(case['Tenant'],binding[1])).fetchone()[0]
            edge=db.execute("INSERT INTO photo.v2_document_refs(tenant_id,project_id,document_id,artifact_id,sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,'gc-mask-fixture',0) RETURNING id",(case['Tenant'],case['Project'],document,binding[1],root_sha)).fetchone()[0]
        assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(case['Tenant'],binary_id)).fetchone()[0] is True
        assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(str(uuid4()),binary_id)).fetchone()[0] is False
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 hour' WHERE tenant_id=%s AND id=%s",(case['Tenant'],binary_id))
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_maintenance')
            assert db.execute('SELECT artifact_id FROM photo.claim_artifact_gc()').fetchall()==[]
        db.execute('DELETE FROM photo.v2_document_refs WHERE id=%s',(edge,))
        assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(case['Tenant'],binary_id)).fetchone()[0] is False
        # Claim eligibility is tested in a rollback transaction; no live bytes removed.
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE photo_maintenance')
            claimed=db.execute('SELECT artifact_id,gc_token FROM photo.claim_artifact_gc()').fetchone()
            assert claimed and claimed[0]==binary_id and claimed[1]
        db.execute('UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=%s AND id=%s',(case['Tenant'],binary_id))
        db.execute("UPDATE photo.v2_draft_uploads SET state='ready' WHERE tenant_id=%s AND artifact_id IN(%s,%s)",(case['Tenant'],binding[1],binary_id))
        from psycopg import errors
        with pytest.raises(errors.CheckViolation,match='immutable user_mask_binding'):
            with db.transaction():
                db.execute('UPDATE photo.v2_draft_uploads SET user_mask_binding=NULL WHERE tenant_id=%s AND artifact_id=%s',(case['Tenant'],binding[1]))
        args=(case['Tenant'],base['asset_version_id'],case['Actor'],case['Session'],binding[1],canonical_bytes(mask).decode(),base['source']['artifact_id'],base['source']['sha256'],base['canvas_hash'])
        sql='SELECT photo.bind_v25_user_mask(%s,%s,%s,%s,%s,%s,%s,%s,%s)'
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
            assert db.execute(sql,args).fetchone()[0] is True
            changed=list(args);changed[-1]='f'*64
            assert db.execute(sql,changed).fetchone()[0] is False
            changed=list(args);changed[5]='{}'
            assert db.execute(sql,changed).fetchone()[0] is False
        tasks=db.execute("SELECT t.run_id,t.id,t.params_artifact_id,a.sha256,t.input_digest,t.execution_profile,t.state,t.attempt FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id WHERE t.tenant_id=%s AND t.operation='layer.compose'",(case['Tenant'],)).fetchall()
        assert len(tasks)==1 and tasks[0][-2:]==('blocked',0)
        staged=tasks[0]
        from psycopg.types.json import Jsonb
        stage_args=(case['Tenant'],staged[0],staged[1],0,staged[2],staged[3],staged[4],Jsonb(staged[5]))
        stage_sql='SELECT photo.stage_v25_user_masked_composition_task(%s,%s,%s,%s,%s,%s,%s,%s)'
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute(stage_sql,stage_args).fetchone()[0] is True
        db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(staged[1],))
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute(stage_sql,stage_args).fetchone()[0] is False
        db.execute('UPDATE photo.tasks SET cancel_requested=false WHERE id=%s',(staged[1],))
        from psycopg.types.json import Jsonb
        composition=json.loads(case['Params'])
        composition['mask']=dict(op='ref',mask=dict(artifact_id=str(binding[1]),sha256=root_sha),canvas_hash=base['canvas_hash'])
        input_args=(case['Tenant'],case['Project'],case['Snapshot'],case['Actor'],case['Session'],Jsonb(composition),binding[1],root_sha,binary_id,artifact[1],400,300)
        input_sql='SELECT photo.v25_layer_original_user_mask_pixels_scope(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute(input_sql,input_args).fetchone()[0] is True
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
            assert db.execute(sql,args).fetchone()[0] is False

        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute(input_sql,input_args).fetchone()[0] is False
            assert db.execute(stage_sql,stage_args).fetchone()[0] is False

        # Retire both uploads and physically collect only the pinned expired array.
        db.execute("UPDATE photo.v2_draft_uploads SET state='retired' WHERE tenant_id=%s AND artifact_id IN(%s,%s)",(case['Tenant'],binding[1],binary_id))
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 hour' WHERE tenant_id=%s AND id=%s",(case['Tenant'],binary_id))
        replacement=b'newer-version-test-object'
        later=client.put_object(bucket,rows[1][2],BytesIO(replacement),len(replacement),content_type='application/octet-stream',metadata={'sha256':sha256(replacement).hexdigest()}).version_id
        env['PHOTO_MASK_GC_CASE']=json.dumps(dict(Objects=client._photo_test_config))
        collected=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationUserMaskGC$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=50)
        assert collected.returncode==0,collected.stdout+collected.stderr
        assert db.execute('SELECT state FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(case['Tenant'],binary_id)).fetchone()[0]=='deleted'
        from minio.error import S3Error
        with pytest.raises(S3Error):client.stat_object(bucket,rows[1][2],version_id=rows[1][3])
        assert client.stat_object(bucket,rows[1][2],version_id=later).size==len(replacement)
        assert client.stat_object(bucket,rows[0][2],version_id=rows[0][3]).size==rows[0][1]
