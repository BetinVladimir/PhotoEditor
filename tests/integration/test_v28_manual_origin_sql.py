"""Positive V28 relational manual pair origin; metadata only, no raster IO."""
from uuid import uuid4
import psycopg
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition
import json


def test_manual_pair_origin_requires_one_canonical_finished_producer(database):
    with psycopg.connect(**database) as db:
        case=seed_original_composition(db)
        tenant,project,snapshot,actor,session=(case[k] for k in ('Tenant','Project','Snapshot','Actor','Session'))
        base=json.loads(case['Params'])['base']
        run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'mask','running',%s,%s,%s,%s,true) RETURNING id",(tenant,snapshot,Jsonb(dict(asset_version_id=base['asset_version_id'],source_sha256=base['source']['sha256'])),'a'*64,actor)).fetchone()[0]
        task=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,'manual:0','mask.rasterize','1','cpu','blocked',%s,%s,%s,'{}',1,1) RETURNING id",(tenant,run,Jsonb([base['source']]),case['Artifact']['artifact_id'],'b'*64)).fetchone()[0]
        pins={}
        for index,role in enumerate(('mask_target_ref','mask_guard_ref','mask_target','mask_protected','rasterized_mask')):
            digest=str(index+1)*64
            aid=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,100,%s,%s,clock_timestamp(),%s,1,1) RETURNING id",(tenant,str(uuid4()),digest,'application/octet-stream' if role in ('mask_target','mask_protected') else 'application/json',role,task)).fetchone()[0]
            pins[role]=dict(artifact_id=str(aid),sha256=digest)
        db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,operation,source_artifact_id,declared_inputs,params,params_sha256,backend) VALUES(%s,%s,%s,'mask_manual','mask.rasterize',%s,%s,'{}',%s,'cpu')",(tenant,run,task,base['source']['artifact_id'],Jsonb([base['source']]),'c'*64))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tenant,task,uuid4()))
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb(list(pins.values())),'d'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    def origin(conn,tenant_id=tenant,session_id=session):
        conn.execute('SET LOCAL ROLE photo_scheduler')
        args=[tenant_id,project,snapshot,actor,session_id,Jsonb(base)]
        for role in ('mask_target_ref','mask_guard_ref','mask_target','mask_protected'):
            args.extend((pins[role]['artifact_id'],pins[role]['sha256']))
        return conn.execute('SELECT * FROM photo.v28_manual_pair_origin('+','.join(['%s']*14)+')',args).fetchall()
    with psycopg.connect(**database) as db:
        ready=origin(db)
        assert len(ready)==1 and str(ready[0][0])==pins['rasterized_mask']['artifact_id'] and str(ready[0][2])==case['Artifact']['artifact_id']
        assert origin(db,tenant_id=uuid4())==[]
        assert origin(db,session_id=uuid4())==[]
    with psycopg.connect(**database) as db:
        db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(pins['mask_target_ref']['artifact_id'],))
    with psycopg.connect(**database) as db:
        assert origin(db)==[]
