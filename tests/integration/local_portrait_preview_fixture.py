"""Complete relational before/after media inventory, no physical image claim."""
import json
from hashlib import sha256
from uuid import uuid4
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.media import MediaParams
from test_local_portrait_result_authority import adopted_local_result


def local_preview_inventory(db,operation):
    review_required=operation=='portrait.clothing'
    item=adopted_local_result(db,operation,review_required=review_required)
    tid,task,result=item['tenant'],item['task'],item['result']
    db.execute('SET LOCAL ROLE photo_scheduler')
    review=db.execute('SELECT photo.publish_v28_local_portrait_result(%s,%s,%s)',(tid,task,result)).fetchone()[0]
    db.execute('RESET ROLE')
    receipt,snapshot,config=db.execute('SELECT v28_local_result_receipt,input_snapshot_id,config_snapshot FROM photo.runs WHERE id=%s',(item['run'],)).fetchone()
    if review_required:
        assert review is not None and str(review)==receipt['review_id']
    else:
        assert review==task and receipt['review_id'] is None
        assert db.execute('SELECT count(*) FROM photo.reviews WHERE tenant_id=%s AND task_id=%s',(tid,task)).fetchone()==(0,)
        review=None
    base_version=json.loads(config['v28_local_portrait']['intent']['params_jcs'])['base']['asset_version_id']
    refs=seed_review_media_inventory(db,item,receipt,snapshot,base_version)
    if review_required:
        assert db.execute('SELECT photo.v28_local_portrait_base_preview_ready(%s,%s,%s)',(tid,review,'a'*64)).fetchone()[0] is True
        assert db.execute('SELECT photo.v28_local_portrait_review_preview_ready(%s,%s)',(tid,review)).fetchone()[0] is True
    else:
        assert db.execute('SELECT photo.v28_local_portrait_result_ready(%s,%s,%s,%s)',(tid,item['run'],item['actor'],item['session'])).fetchone()==(True,)
        assert db.execute('SELECT photo.v28_local_portrait_review_decision_ready(%s,%s,%s,%s)',(tid,task,item['actor'],item['session'])).fetchone()==(False,)
    return item,review,refs


def seed_review_media_inventory(db,item,receipt,snapshot,base_version):
    """Reusable exact presentation inventory; caller owns original operation gates."""
    tid,task,result=item['tenant'],item['task'],item['result']
    srgb='a'*64
    preview=uuid4()
    db.execute('SET LOCAL ROLE photo_fn_owner')
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,v26_derivative_task_id) VALUES(%s,%s,'preview','completed',%s,%s,%s,%s,true,%s)",(preview,tid,snapshot,Jsonb(dict(source_result_id=str(result),source_attempt='1',source_fence='1',root=receipt['root'],srgb_sha256=srgb)),'d'*64,item['actor'],task))
    db.execute('RESET ROLE')
    refs=[]

    def artifact(role,producer=None,media='application/json',raw=None):
        aid=uuid4();digest=sha256(raw).hexdigest() if raw is not None else aid.hex*2
        size=len(raw) if raw is not None else 2
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,%s,%s,'ready',%s,clock_timestamp(),%s,%s,%s)",(aid,tid,str(aid),digest,size,media,role,producer,1 if producer else None,1 if producer else None))
        return dict(artifact_id=str(aid),sha256=digest)

    for side,variant in [('before','preview1600'),('after','preview1600'),('after','preview2560')]:
        source=receipt['base'] if side=='before' else receipt['candidate']
        edge=1600 if variant=='preview1600' else 2560
        profile=sha256(canonical_bytes(dict(icc_sha256=srgb,long_edge=edge,metadata_policy='stripped',no_upscale=True,quality=88,schema_version=1,variant=variant))).hexdigest()
        owner=preview
        if side=='before':
            owner=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'preview','completed',%s,'{}',%s,%s,true) RETURNING id",(tid,snapshot,'e'*64,item['actor'])).fetchone()[0]
        node,canonical,worker=uuid4(),uuid4(),uuid4()
        # Exact current media.preview model and canonical bytes, not a hash
        # unrelated to the authored document. Physical outputs remain synthetic.
        params_raw=canonical_bytes(MediaParams(image=ArtifactRef.model_validate_json(canonical_bytes(source)),variant=variant,profile_hash=profile))
        params=artifact('task_params',raw=params_raw)
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,canonical_result_id) VALUES(%s,%s,%s,%s,'media.preview','1','media','succeeded',%s,%s,%s,'{}',1,1,%s)",(node,tid,owner,'render.derivative:'+variant,Jsonb([source]),params['artifact_id'],'f'*64,canonical))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tid,node,worker))
        manifest,image=artifact('derivative_manifest',node),artifact('derivative',node,'image/jpeg')
        outputs=[manifest,image];objects=[]
        for ref in outputs:
            aid=ref['artifact_id']
            db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,output_role,bucket,object_key,sha256,bytes,media_type,'ready',object_version,clock_timestamp() FROM photo.artifacts WHERE id=%s",(node,worker,'a'*64,uuid4(),aid))
            objects.append(dict(ref,bucket='fixture',key=aid,version='v1',bytes=2,media_type='application/json' if ref==manifest else 'image/jpeg'))
        db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)",(canonical,tid,node,Jsonb(outputs),'e'*64))
        if side=='before':
            db.execute("INSERT INTO photo.derivatives(tenant_id,asset_version_id,variant,profile_hash,artifact_id,status,task_id) VALUES(%s,%s,%s,%s,%s,'ready',%s)",(tid,base_version,variant,profile,image['artifact_id'],node))
        else:
            body=dict(manifest=dict(source=source,variant=variant,profile_hash=profile,derivative=image),manifest_id=manifest['artifact_id'],objects=objects,result_id=str(canonical),attempt='1',fence='1')
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,v26_derivative_params,v26_derivative_receipt) VALUES(%s,%s,%s,'task_params',%s,%s,%s,%s,'fixture','ready','v1',clock_timestamp(),%s,%s)",(tid,owner,node,params['artifact_id'],uuid4(),params['sha256'],len(params_raw),params_raw.decode(),Jsonb(body)))
            db.execute('RESET ROLE')
            refs.append((manifest,image,node))
    db.execute('SET CONSTRAINTS ALL IMMEDIATE');db.execute('SET CONSTRAINTS ALL DEFERRED')
    return refs
