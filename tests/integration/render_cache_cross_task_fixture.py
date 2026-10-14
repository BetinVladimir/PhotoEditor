"""Rollback-only cross-task SQL metadata proof; not typed/GPU/S3 acceptance."""
from uuid import uuid4
from copy import deepcopy
from hashlib import sha256
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes


def assert_cross_task_render_cache(db,tenant,run,task,worker,cache_key,cache_body,root,result,policy):
    with db.transaction(force_rollback=True):
        target,newrun,workflow,manifest=uuid4(),uuid4(),uuid4(),uuid4()
        config=deepcopy(db.execute('SELECT config_snapshot FROM photo.runs WHERE id=%s',(run,)).fetchone()[0])
        node=next(n for n in config['v26_render']['nodes'] if n['task_id']==str(task))
        node=deepcopy(node);node['task_id']=str(target);config['v26_render']['nodes']=[node]
        profile=deepcopy(db.execute('SELECT execution_profile FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0])
        chunks=db.execute("SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE task_id=%s AND contract='InputClosureChunk' ORDER BY ordinal",(task,)).fetchall()
        body=canonical_bytes(dict(schema_version=1,task_id=str(target),entry_count=profile['v2_entry_count'],
            entries_sha=profile['v2_entries_sha'],chunks=[{'artifact_id':str(i),'sha256':s} for i,s in chunks]))
        checksum=sha256(body).hexdigest();profile['v2_input_manifest']={'artifact_id':str(manifest),'sha256':checksum}
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed,v26_qualification_pin) SELECT %s,tenant_id,kind,'running',input_snapshot_id,%s,%s,created_by,failure_policy,true,v26_qualification_pin FROM photo.runs WHERE id=%s",
            (newrun,Jsonb(config),sha256(canonical_bytes(config)).hexdigest(),run))
        db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) SELECT %s,tenant_id,project_id,%s,input_snapshot_id,workflow_type,renderer_version,policy_sha FROM photo.v2_workflows WHERE run_id=%s",(workflow,newrun,run))
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,'cross-task-transport',%s,'transport-version-1',%s,%s,'application/json',clock_timestamp())",(manifest,tenant,str(manifest),checksum,len(body)))
        inputs=[profile['v2_input_manifest'],node['params']]
        initial={k:v for k,v in profile.items() if k!='v2_input_manifest'}
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) SELECT %s,tenant_id,%s,'render.compose:cache-target',operation,operation_version,resource_class,'blocked',%s,params_artifact_id,input_digest,%s FROM photo.tasks WHERE id=%s",(target,newrun,Jsonb(inputs),Jsonb(initial),task))
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) SELECT tenant_id,project_id,%s,artifact_id,sha256,ordinal,contract FROM photo.v2_task_inputs WHERE task_id=%s AND contract<>'InputClosureManifest'",(target,task))
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) SELECT tenant_id,project_id,%s,%s,%s,ordinal,contract FROM photo.v2_task_inputs WHERE task_id=%s AND contract='InputClosureManifest'",(target,manifest,checksum,task))
        db.execute("UPDATE photo.tasks SET execution_profile=%s,state='running',attempt=1,fencing_token=1,worker_id=%s,lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '120 seconds' WHERE id=%s",(Jsonb(profile),worker,target))
        db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) SELECT node_id,'cache-target','gpu',capacity,tenant_id,%s,1,clock_timestamp()+interval '60 seconds' FROM photo.resource_slots WHERE task_id=%s",(target,task))
        db.execute('RESET ROLE')
        db.execute("UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s",('a'*64,worker))
        args=(tenant,newrun,target,1,1,worker,node['params']['artifact_id'],node['params']['sha256'],node['params_jcs'],node['input_digest'],cache_key,cache_body)
        db.execute('SET LOCAL ROLE photo_worker')
        hit=db.execute('SELECT * FROM photo.lookup_v26_render_worker_cache('+','.join(['%s']*12)+')',args).fetchone()
        assert hit is not None and hit[0]==result and hit[1]['artifact_id']==str(root)
        manifest_rows=db.execute('SELECT * FROM photo.v26_render_worker_cache_artifacts('+','.join(['%s']*12)+')',args).fetchall()
        assert len(manifest_rows)==1 and manifest_rows[0][0]==root and manifest_rows[0][7]=='fixture-version-1'
        db.execute('RESET ROLE');db.execute('SET LOCAL ROLE photo_gateway')
        location=db.execute('SELECT * FROM photo.gateway_input(%s,1,1,%s,%s,%s)',(target,worker,'a'*64,root)).fetchone()
        assert location is not None and location[0]==root and location[3]=='fixture-version-1'
        assert db.execute('SELECT * FROM photo.gateway_input(%s,1,2,%s,%s,%s)',(target,worker,'a'*64,root)).fetchone() is None
        db.execute('RESET ROLE')
        for sql,values in (
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(root,)),
            ("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",(config['v26_render']['auth_session_id'],)),
            ("UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s",(policy,)),
        ):
            with db.transaction(force_rollback=True):
                db.execute(sql,values)
                db.execute('SET LOCAL ROLE photo_worker')
                assert db.execute('SELECT * FROM photo.lookup_v26_render_worker_cache('+','.join(['%s']*12)+')',args).fetchone() is None
                db.execute('RESET ROLE');db.execute('SET LOCAL ROLE photo_gateway')
                assert db.execute('SELECT * FROM photo.gateway_input(%s,1,1,%s,%s,%s)',(target,worker,'a'*64,root)).fetchone() is None
                db.execute('RESET ROLE')
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE photo_worker')
            query='SELECT photo.quarantine_v26_render_worker_cache('+','.join(['%s']*13)+')'
            assert db.execute(query,(*args,uuid4())).fetchone()==(False,)
            assert db.execute(query,(*args,result)).fetchone()==(True,)
            assert db.execute('SELECT * FROM photo.lookup_v26_render_worker_cache('+','.join(['%s']*12)+')',args).fetchone() is None
            db.execute('RESET ROLE');db.execute('SET LOCAL ROLE photo_gateway')
            assert db.execute('SELECT * FROM photo.gateway_input(%s,1,1,%s,%s,%s)',(target,worker,'a'*64,root)).fetchone() is None
            db.execute('RESET ROLE')
            # A replay of the old accepted producer must not clear quarantine.
            db.execute('SET LOCAL ROLE photo_scheduler')
            old_inventory=hit[2]
            assert db.execute('SELECT photo.publish_v26_render_cache(%s,%s,1,1,%s,%s,%s,%s)',(tenant,task,result,cache_key,cache_body,Jsonb(old_inventory))).fetchone()==(False,)
            db.execute('RESET ROLE')
            newroot,newresult=uuid4(),uuid4();checksum=sha256(b'{}').hexdigest()
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'cache-replacement',%s,%s,2,'application/json','replacement-v1',clock_timestamp(),'document',%s,1,1)",(newroot,tenant,str(newroot),checksum,target))
            db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'document','cache-replacement',%s,%s,2,'application/json','ready','replacement-v1')",(tenant,target,worker,'a'*64,uuid4(),newroot,str(newroot),checksum))
            db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)",(newresult,tenant,target,Jsonb([{'artifact_id':str(newroot),'sha256':checksum}]),'a'*64))
            db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s,lease_until=NULL,worker_id=NULL WHERE id=%s",(newresult,target))
            inventory=[{'artifact_id':str(newroot),'sha256':checksum,'purpose':sha256(('tasks/'+str(target)+'/$').encode()).hexdigest(),'ordinal':0}]
            from photo_worker.contracts.base import canonical_hash
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',(tenant,target,newresult,Jsonb(inventory),canonical_hash(inventory))).fetchone()==(True,)
            assert db.execute('SELECT photo.publish_v26_render_cache(%s,%s,1,1,%s,%s,%s,%s)',(tenant,target,newresult,cache_key,cache_body,Jsonb(inventory))).fetchone()==(True,)
            db.execute('RESET ROLE')
            assert db.execute('SELECT result_id,quarantined_at FROM photo.result_cache WHERE tenant_id=%s AND cache_key=%s',(tenant,cache_key)).fetchone()==(newresult,None)
