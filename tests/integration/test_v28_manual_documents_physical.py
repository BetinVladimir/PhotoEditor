"""Real SQL lineage and pinned manual documents; seeded producer, no raster execution."""
import json
import os
import subprocess
from hashlib import sha256
from io import BytesIO
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest
from psycopg.conninfo import make_conninfo
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from layer_original_fixture import seed_original_composition
from test_v27_native_protection_gc import lens_objects


@pytest.mark.parametrize('external', [False, True])
@pytest.mark.parametrize('failure', ['', 'corrupt-root', 'missing-params'])
def test_manual_documents_exact_bytes_and_lineage(database, lens_objects, failure, external):
    client,bucket=lens_objects
    root_path=Path(__file__).resolve().parents[2]
    source=json.loads((root_path/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
    source_raw=canonical_bytes(source)
    def publish(raw):
        key='manual-documents/'+uuid4().hex
        version=client.put_object(bucket,key,BytesIO(raw),len(raw),content_type='application/json',metadata={'sha256':sha256(raw).hexdigest()}).version_id
        return dict(bucket=bucket,key=key,version=version,sha256=sha256(raw).hexdigest(),bytes=len(raw))
    source_object=publish(source_raw)
    with psycopg.connect(**database) as db:
        case=seed_original_composition(db,source_object=source_object,canvas_hash=canonical_hash(source['canvas']))
        base=json.loads(case['Params'])['base'];tenant=case['Tenant']
        db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tenant,))
        db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1073741824'::jsonb) WHERE id=%s",(tenant,))
        assert not db.execute("SELECT has_table_privilege('photo_scheduler','photo.artifacts','SELECT')").fetchone()[0]
        root=json.loads((root_path/'backend/internal/review/testdata/face-manual-root-python.json').read_text())
        root['image']=base['source']
        # Ingest lineage for source pixels; binary content is not decoded by the compiler.
        source_task=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,'ingest','running',%s,'{}',%s,%s) RETURNING id",(tenant,case['Snapshot'],'e'*64,case['Actor'])).fetchone()[0]
        source_task=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,'ingest:0','ingest','1','cpu','blocked','[]',%s,%s,'{}',1,1) RETURNING id",(tenant,source_task,case['Artifact']['artifact_id'],'e'*64)).fetchone()[0]
        db.execute('UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE id=%s',(source_task,base['source']['artifact_id']))
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,'fixture',%s,100,'application/octet-stream','working_rgb',clock_timestamp(),%s,1,1)",(source['pixels']['artifact_id'],tenant,bucket,str(uuid4()),source['pixels']['sha256'],source_task))
        params=dict(image=base['source'],object_id=root['target']['object_id'],vectors=None,inline_vectors=dict(version='manual_mask_v1',source_sha256=source['canvas']['source_sha256'],canvas=source['canvas'],shapes=[]))
        vector_raw=b'';vector_ref=None
        if external:
            vector_raw=canonical_bytes(params['inline_vectors']);vo=publish(vector_raw)
            vid=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp()) RETURNING id",(tenant,bucket,vo['key'],vo['version'],vo['sha256'],vo['bytes'])).fetchone()[0]
            vector_ref=dict(artifact_id=str(vid),sha256=vo['sha256'])
            params['vectors']=vector_ref;params['inline_vectors']=None
        declared=[base['source']]+([vector_ref] if external else [])
        params_raw=canonical_bytes(params);params_object=publish(params_raw)
        params_id=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp()) RETURNING id",(tenant,bucket,params_object['key'],params_object['version'],params_object['sha256'],params_object['bytes'])).fetchone()[0]
        root['vectors']=vector_ref or dict(artifact_id=str(params_id),sha256=params_object['sha256'])
        run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'mask','running',%s,%s,%s,%s,true) RETURNING id",(tenant,case['Snapshot'],Jsonb(dict(asset_version_id=base['asset_version_id'],source_sha256=base['source']['sha256'])),'a'*64,case['Actor'])).fetchone()[0]
        task=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,'manual:0','mask.rasterize','1','cpu','blocked',%s,%s,%s,'{}',1,1) RETURNING id",(tenant,run,Jsonb(declared),params_id,'b'*64)).fetchone()[0]
        pins={}
        def artifact(role,raw,aid=None,media='application/json'):
            obj=publish(b'x'*len(raw) if role=='rasterized_mask' and failure=='corrupt-root' else raw)
            obj['sha256']=sha256(raw).hexdigest()
            aid=aid or uuid4()
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,1,1)",(aid,tenant,bucket,obj['key'],obj['version'],obj['sha256'],obj['bytes'],media,role,task))
            pin=dict(artifact_id=str(aid),sha256=obj['sha256']);pins[role]=pin
            return pin,obj
        # SQL lineage requires physical-pixel artifact rows; pixel decoding is outside this test.
        for member,role in [('target','mask_target'),('protected','mask_protected')]:
            pixel,_=artifact(role,b'0'*100,media='application/octet-stream')
            root[member]['artifact']=pixel
        target,_=artifact('mask_target_ref',canonical_bytes(root['target']))
        guard,_=artifact('mask_guard_ref',canonical_bytes(root['protected']))
        for member,role in [('target_png','mask_target_png'),('protected_png','mask_protected_png')]:
            png,_=artifact(role,b'png-placeholder',media='image/png')
            root[member]=png
        root_raw=canonical_bytes(root)
        _,root_object=artifact('rasterized_mask',root_raw)
        db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,operation,source_artifact_id,declared_inputs,params,params_sha256,backend) VALUES(%s,%s,%s,'mask_manual','mask.rasterize',%s,%s,%s,%s,'cpu')",(tenant,run,task,base['source']['artifact_id'],Jsonb(declared),Jsonb(params),params_object['sha256']))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tenant,task,uuid4()))
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb(list(pins.values())),'d'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
        # A newer key version must never replace the catalog-pinned original bytes.
        for obj in (root_object,params_object):
            client.put_object(bucket,obj['key'],BytesIO(b'{}'),2,content_type='application/json')
        if failure=='missing-params':
            client.remove_object(bucket,params_object['key'],version_id=params_object['version'])
    config={k:case[k] for k in ('Tenant','Project','Snapshot','Actor','Session')}
    config.update(Binding=base,Target=target,Guard=guard,TargetPixel=pins['mask_target'],GuardPixel=pins['mask_protected'],Source=source_raw.decode(),Root=root_raw.decode(),Params=params_raw.decode(),RootRef=pins['rasterized_mask'],ParamsRef=dict(artifact_id=str(params_id),sha256=params_object['sha256']),VectorRef=vector_ref,Failure=failure,Vectors=vector_raw.decode(),Objects={**client._photo_test_config,'Development':True})
    env={**os.environ,'PHOTO_TEST_DATABASE_URL':make_conninfo(**database),'PHOTO_TEST_MANUAL_DOCUMENTS':json.dumps(config),'GOCACHE':'/private/tmp/photoeditor-go-cache'}
    result=subprocess.run(['go','test','./internal/v2portrait','-run','^TestIntegrationManualDocumentsPhysical$','-count=1','-timeout=30s','-v'],cwd=root_path/'backend',env=env,capture_output=True,text=True,timeout=45)
    assert result.returncode==0,result.stdout+result.stderr
    assert ('MANUAL_DOCUMENTS_PHYSICAL_REFUSED' if failure else 'MANUAL_DOCUMENTS_PHYSICAL_OK') in result.stdout

    material=subprocess.run(['go','test','./internal/v2draft','-run','^TestIntegrationLocalPortraitMaterialPhysical$','-count=1','-timeout=30s','-v'],cwd=root_path/'backend',env=env,capture_output=True,text=True,timeout=45)
    assert material.returncode==0,material.stdout+material.stderr
    assert ('LOCAL_PORTRAIT_MATERIAL_REFUSED' if failure else 'LOCAL_PORTRAIT_MATERIAL_PHYSICAL_OK') in material.stdout
    if not failure:
        assert material.stdout.count('LOCAL_PORTRAIT_STAGED_UNQUALIFIED')==2
        with psycopg.connect(**database) as owner:
            runs=owner.execute("SELECT id,graph_sealed,status,v28_local_task_plan IS NOT NULL,v28_local_qualification_pin IS NULL FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v28_local_portrait'",(tenant,)).fetchall()
            assert len(runs)==2 and all(row[1:]==(False,'draft',True,True) for row in runs)
            for run in runs:
                assert owner.execute("SELECT state,attempt,lease_until IS NULL FROM photo.tasks WHERE tenant_id=%s AND run_id=%s",(tenant,run[0])).fetchall()==[('blocked',0,True)]
                assert owner.execute("SELECT count(*) FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s AND state='ready'",(tenant,run[0])).fetchone()[0]==3
        def scope(pin, contract, parent=None, session=None):
            with psycopg.connect(**database) as api:
                api.execute('SET LOCAL ROLE photo_api')
                api.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
                return api.execute('SELECT photo.v2_draft_artifact_scope('+','.join(['%s']*10)+')',(tenant,case['Project'],base['asset_version_id'],pin['artifact_id'],pin['sha256'],contract,case['Actor'],session or case['Session'],parent['artifact_id'] if parent else None,parent['sha256'] if parent else None)).fetchone()[0]
        assert scope(target,'MaskRef')
        assert scope(pins['mask_target'],'MaskPixels',target)
        assert scope(pins['mask_target_png'],'Artifact',pins['rasterized_mask'])
        assert not scope(target,'MaskRef',pins['rasterized_mask'])
        assert not scope(pins['mask_target'],'MaskPixels',guard)
        assert not scope(pins['mask_target_png'],'Artifact',target)
        assert not scope(target,'MaskRef',session=str(uuid4()))
        with psycopg.connect(**database) as owner:
            owner.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(target['artifact_id'],))
        assert not scope(target,'MaskRef')
        assert not scope(pins['mask_target'],'MaskPixels',target)
