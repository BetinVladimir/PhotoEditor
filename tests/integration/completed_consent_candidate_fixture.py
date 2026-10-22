"""Completed consent relational fixture, not photographic quality or worker execution."""
import json
from copy import deepcopy
from hashlib import sha256
from uuid import uuid4, uuid5
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from background_cleanup_preview_documents_fixture import working_image_builder
from test_v27_semantic_final_producer_gate import fixture
from semantic_qualification_fixture import frozen_qualification

def completed_consent_candidate(db, family, *, bound_confirmation=True):
    assert family in ("glare", "open_eyes")
    operation="portrait."+family
    tid, template, *tail = fixture(db, snapshot_authority=True, qualification=False,
        metadata_bodies=True, source_builder=working_image_builder(with_alpha=True))
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(tid,))
    scope=tail[-1]
    actor,session,project=scope['actor'],scope['session'],scope['project']
    old_run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(template,)).fetchone()[0]
    run,task,review=uuid4(),uuid4(),uuid4()
    def artifact(raw,role=None,producer=None,media='application/json'):
        aid=uuid4();digest=sha256(raw).hexdigest()
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,%s,%s,%s,clock_timestamp(),%s,%s,%s)",
            (aid,tid,str(aid),digest,len(raw),media,role,producer,1 if producer else None,1 if producer else None))
        return dict(artifact_id=str(aid),sha256=digest)
    base=json.loads(scope['source_body']);candidate=deepcopy(base)
    candidate['pixels']=artifact(b'candidate RGB fixture',producer=task,media='application/x-npy')
    candidate_raw=canonical_bytes(candidate)
    # A separate accepted donor version is mandatory even for the same person.
    donor_image=artifact(scope['source_body'],'working_image')
    donor_asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,scope['original'])).fetchone()[0]
    donor_version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tid,donor_asset,donor_image['artifact_id'],donor_image['sha256'])).fetchone()[0]
    collection=db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) VALUES(%s,%s,'Donor source') RETURNING id",(tid,project)).fetchone()[0]
    for ordinal,asset,version in [(0,scope['asset'],scope['version']),(1,donor_asset,donor_version)]:
        db.execute('INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES(%s,%s,%s,%s,%s)',(tid,collection,asset,version,ordinal))
    shoot=db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,name,state,policy,consent_epoch) VALUES(%s,%s,%s,'Donor','open','{}',1) RETURNING id",(tid,project,collection)).fetchone()[0]
    person=db.execute("INSERT INTO photo.project_persons(tenant_id,project_id,label,state,consent_epoch) VALUES(%s,%s,'Confirmed person','active',1) RETURNING id",(tid,project)).fetchone()[0]
    consent=db.execute("INSERT INTO photo.consent_grants(tenant_id,project_id,purpose,person_id,status,epoch,policy_version,actor_id) VALUES(%s,%s,'identity_matching',%s,'granted',1,'fixture-v1',%s) RETURNING id",(tid,project,person,actor)).fetchone()[0]
    faces=[artifact(b'{}'),artifact(b'{}')];face_ids=[uuid4(),uuid4()];observations=[]
    for version,face,evidence in zip([scope['version'],donor_version],face_ids,faces):
        observations.append(db.execute('INSERT INTO photo.person_observations(tenant_id,project_id,person_id,asset_version_id,face_id,evidence_artifact_id,confirmed_by) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id',(tid,project,person,version,face,evidence['artifact_id'],actor)).fetchone()[0])
    source=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']))
    donor=dict(asset_version_id=str(donor_version),source=donor_image)
    proof=dict(schema_version=1,actor_id=str(actor),person_id=str(person),consent_id=str(consent),epoch='1',source=source,donor=donor,
        source_observation_id=str(observations[0]),donor_observation_id=str(observations[1]),source_face_id=str(face_ids[0]),donor_face_id=str(face_ids[1]))
    proof_raw=canonical_bytes(proof);confirmation=artifact(proof_raw)
    body=dict(schema_version=1,base=source,donor=donor,source_faces=faces[0],donor_faces=faces[1],confirmation=confirmation)
    if family=='glare': body['mode']='donor'
    params=artifact(canonical_bytes(body))
    pin=frozen_qualification(db,tid,project,actor,operation=operation,backend='cpu')
    pin['policy_sha']=db.execute('SELECT photo.v28_donor_policy_sha(%s)',(pin['policy_id'],)).fetchone()[0]
    objects=[]
    for contract,key in [('Report','report'),('Calibration','calibration')]:
        row=db.execute('SELECT bucket,object_key,object_version,bytes FROM photo.artifacts WHERE id=%s',(pin[key]['artifact_id'],)).fetchone()
        objects.append(dict(contract=contract,**pin[key],bucket=row[0],object_key=row[1],object_version=row[2],bytes=row[3]))
    objects.sort(key=lambda item:item['contract'])
    descriptor=canonical_bytes(dict(params=body,task=dict(id=str(task),params_artifact_id=params['artifact_id'],input_digest='b'*64,execution_profile=dict(donor_qualification=pin)),qualification_objects=objects)).decode()
    receipt=dict(descriptor_sha=sha256(descriptor.encode()).hexdigest(),task_id=str(task),actor_id=str(actor),auth_session_id=str(session))
    column='v28_donor_descriptor_jcs' if family=='open_eyes' else 'v28_restoration_descriptor_jcs'
    # Column comes only from the closed family discriminator, never caller SQL.
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed,"+column+",v28_donor_queued_receipt,v28_restoration_session_id) SELECT %s,tenant_id,'v2','waiting_review',input_snapshot_id,config_snapshot,config_hash,created_by,'fail_fast',true,%s,%s,%s FROM photo.runs WHERE id=%s",(run,descriptor,Jsonb(receipt) if family=='open_eyes' else None,session if family=='glare' else None,old_run))
    workflow=db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) SELECT tenant_id,project_id,%s,input_snapshot_id,'preview','v2.2',policy_sha FROM photo.v2_workflows WHERE run_id=%s RETURNING id",(run,old_run)).fetchone()[0]
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,deadline) VALUES(%s,%s,%s,'open-eyes:0',%s,'1','cpu','queued','[]',%s,%s,'{}',1,1,clock_timestamp()+interval '1 hour')",(task,tid,run,operation,params['artifact_id'],'b'*64))
    db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,\'WorkingImage\',0)',(tid,project,task,scope['source'],scope['source_sha']))
    db.execute('UPDATE photo.tasks SET execution_profile=%s WHERE id=%s',(Jsonb({'renderer_version':'v2.2'}),task))
    image=artifact(candidate_raw,'working_image',task);root=artifact(b'{}','document',task)
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tid,task,uuid4()))
    db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,(SELECT worker_id FROM photo.task_attempts WHERE task_id=%s AND attempt=1),%s,%s,id,output_role,bucket,object_key,sha256,bytes,media_type,'ready',object_version,clock_timestamp() FROM photo.artifacts WHERE id=%s",(task,task,'a'*64,uuid4(),root['artifact_id']))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest,review_required) VALUES(%s,%s,1,1,%s,'{}',%s,true) RETURNING id",(tid,task,Jsonb([root]),'e'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        for ref,path in [(root,'$'),(image,'$/working_image'),(candidate['pixels'],'$/working_image/pixels'),(candidate['alpha'],'$/working_image/alpha')]:
            db.execute("INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,encode(sha256(convert_to(%s,'UTF8')),'hex'),0)",(tid,project,workflow,ref['artifact_id'],ref['sha256'],'tasks/'+str(task)+'/'+path))
        db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
        context=dict(schema_version=1,tenant_id=str(tid),run_id=str(run),actor_id=str(actor),auth_session_id=str(session),project_id=str(project),shoot_session_id=str(shoot),params=dict(body,confirmation=dict(artifact_id=str(task if bound_confirmation else uuid4()),sha256=confirmation['sha256'])),proof_jcs=proof_raw.decode())
        db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,v28_donor_issuer_context) VALUES(%s,%s,%s,'v28_donor_confirmation',%s,%s,%s,%s,'fixture','ready','v1',clock_timestamp(),%s)",(tid,run,task,confirmation['artifact_id'],uuid4(),confirmation['sha256'],len(proof_raw),Jsonb(context)))
    db.execute("RESET ROLE")
    return dict(tenant=tid,run=run,task=task,result=result,consent=consent,person=person,observations=observations)
