import hashlib
from uuid import uuid4

import rfc8785
from psycopg.types.json import Jsonb


def test_generation_candidate_requires_fenced_display_preview(db):
    digest="a"*64
    user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f"{uuid4()}@test.example",)).fetchone()
    tenant=db.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s",(user,)).fetchone()[0]
    source=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'v1',%s,100,'application/json','working_image') RETURNING id",(tenant,f"source/{uuid4()}",digest)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,artifact_id) VALUES(%s,'input','user',%s,%s,1,%s) RETURNING id",(tenant,user,digest,source)).fetchone()[0]
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'preview','running',%s,%s,%s,%s,true) RETURNING id",(tenant,snapshot,Jsonb({'explicit_generation':True}),"b"*64,user)).fetchone()[0]
    task=uuid4()
    db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id,state) VALUES(%s,%s,%s,'generation',0,'image.generate',%s,'[]','{}',%s,'cpu',%s,'complete')",(tenant,run,task,source,"c"*64,"d"*64))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,timeout_seconds,attempt,fencing_token) VALUES(%s,%s,%s,'generation/00','image.generate','1','gpu','ready','[]',%s,%s,'{}',7200,1,1)",(task,tenant,run,source,"e"*64))
    generated=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,'photo',%s,'v1',%s,100,'application/json','ready',%s,1,1,'working_image') RETURNING id",(tenant,f"generated/{uuid4()}","f"*64,task)).fetchone()[0]
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest,review_required) VALUES(%s,%s,1,1,%s,'{}',%s,true) RETURNING id",(tenant,task,Jsonb([{'artifact_id':str(generated),'sha256':'f'*64}]),"1"*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    claim=db.execute("SELECT tenant_id,run_id,source_task_id,ordinal,artifact_id,sha256,token FROM photo.claim_generation_preview()").fetchone()
    assert claim[:6]==(tenant,run,task,0,generated,"f"*64)
    ref={'artifact_id':str(generated),'sha256':'f'*64}
    params={'image':ref,'modules':{'strength':1},'scale':'proxy'}
    sha=hashlib.sha256(rfc8785.dumps(params)).hexdigest();preview=uuid4()
    assert db.execute("SELECT photo.register_generation_preview(%s,%s,%s,%s,%s,%s)",(task,claim[6],preview,Jsonb(params),sha,Jsonb([ref]))).fetchone()[0]
    row=db.execute("SELECT kind,operation,source_artifact_id,params_sha256 FROM photo.interactive_tasks WHERE task_id=%s",(preview,)).fetchone()
    assert row==('generation_preview','editor.preview',generated,sha)
    assert db.execute("SELECT photo.finalize_interactive_run()").fetchone()[0] is False
    assert db.execute("SELECT status FROM photo.runs WHERE id=%s",(run,)).fetchone()[0]=='running'
