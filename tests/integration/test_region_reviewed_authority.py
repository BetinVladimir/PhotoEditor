"""Relational live no-op review authority. Authored, not physical qualification."""
from copy import deepcopy
from uuid import uuid4
from psycopg.types.json import Jsonb
from test_region_execution_provenance import execution_fixture


def base_fixture(db):
    tenant,_,run,output,session,plan,scope=execution_fixture(db)
    actor=db.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    base=plan['execution']['stages'][0]['output'];parent=uuid4()
    params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test',%s,'v1',%s,64,'application/json','document',now()) RETURNING id",(tenant,str(uuid4()),'1'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,%s,'defects.propose','1','cpu','ready',%s,%s,%s,'{\"backend\":\"cpu\"}',1,1)",(parent,tenant,run,str(parent),Jsonb([base]),params,'2'*64))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cpu','{}')",(tenant,parent,uuid4()))
    refs=[]
    for role,mime,tag in [('defect_candidates','application/json','3'),('defect_label_map','application/octet-stream','4')]:
        artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,64,%s,%s,now(),%s,1,1) RETURNING id",(tenant,str(uuid4()),tag*64,mime,role,parent)).fetchone()[0]
        refs.append(dict(artifact_id=str(artifact),sha256=tag*64))
    proposal,labels=refs
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,parent,Jsonb(refs),'5'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,parent))
    review=db.execute("INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons) VALUES(%s,%s,%s,%s,%s,%s,'defects','[]') RETURNING id",(tenant,run,parent,proposal['artifact_id'],output,proposal['sha256'])).fetchone()[0]
    selection=dict(version='defect_review_v1',proposal=proposal,image=base,label_map=labels,revision=2,accepted_candidate_ids=[],rejected_candidate_ids=[])
    db.execute("UPDATE photo.reviews SET state='decided',revision=2,decision='reject',decided_by=%s,decided_at=now(),selection=%s,decision_hash=%s,decision_route='base' WHERE id=%s",(actor,Jsonb(selection),'6'*64,review))
    expansion=db.execute("INSERT INTO photo.graph_expansions(tenant_id,run_id,parent_task_id,kind,state,expected_count) VALUES(%s,%s,%s,%s,'closed',0) RETURNING id",(tenant,run,parent,'review.defects/'+str(review))).fetchone()[0]
    db.execute("INSERT INTO photo.defect_branches(expansion_id,tenant_id,run_id,review_id,completion_token,payload,base_artifact_id) VALUES(%s,%s,%s,%s,%s,%s,%s)",(expansion,tenant,run,review,uuid4(),Jsonb(dict(route='base',decision_hash='6'*64,base=base)),output))
    proof=dict(route='base',review_id=str(review),decision_hash='6'*64,selection=selection,producer_task_id=str(parent))
    plan['execution']['stages'][0]['review_authority']=proof
    plan['inputs'].extend([dict(ref=proposal,contract='DefectCandidates'),dict(ref=labels,contract='NpyArray')])
    return tenant,parent,proposal,plan,scope


def test_live_noop_human_choice_is_positive_with_complete_typed_inventory(db):
    tenant,parent,proposal,plan,scope=base_fixture(db)
    assert scope() is True
    omitted=deepcopy(plan);del omitted['execution']['stages'][0]['review_authority']
    assert scope(omitted) is False
    omitted=deepcopy(plan);omitted['inputs']=omitted['inputs'][:-1]
    assert scope(omitted) is False
    forged=deepcopy(plan);forged['execution']['stages'][0]['review_authority']['decision_hash']='7'*64
    assert scope(forged) is False
    for query,identity in [
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',parent),
        ('UPDATE photo.tasks SET attempt=2 WHERE id=%s',parent),
        ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",proposal['artifact_id']),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(query,(identity,));assert scope() is False


def test_private_review_projection_cannot_be_called_by_application_roles(db):
    signatures=['photo.v26_region_execution_repair_review(uuid,uuid,uuid)',
        'photo.v26_region_execution_defect_base(uuid,uuid,uuid,text)',
        'photo.v26_region_execution_culling(uuid,uuid,uuid,uuid,uuid,text)']
    for signature in signatures:
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]


def test_region_replay_profile_reader_has_only_scheduler_authority(db):
    signature='photo.v26_region_adoption_execution_profile(uuid,uuid,integer,bigint,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    for role in ('photo_api','photo_worker','photo_portal','photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]
    from uuid import uuid4
    assert db.execute('SELECT photo.v26_region_adoption_execution_profile(%s,%s,1,1,%s)',
        (uuid4(),uuid4(),uuid4())).fetchone()[0] is None
