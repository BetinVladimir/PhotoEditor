import pytest
import psycopg
from uuid import uuid4
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


def test_approved_child_source_is_exact_and_parent_independent(db):
    tid,assembly,_,_,_,candidate,scope=fixture(db,snapshot_authority=True)
    parent=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(assembly,)).fetchone()[0]
    workflow=db.execute('SELECT id FROM photo.v2_workflows WHERE tenant_id=%s AND run_id=%s',(tid,parent)).fetchone()[0]
    intent=db.execute('INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id',(tid,scope['project'],workflow,scope['version'],scope['snapshot'],scope['source'],'5'*64)).fetchone()[0]
    target=db.execute("INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state) VALUES(%s,%s,%s,%s,%s,'proposed') RETURNING id",(tid,scope['project'],intent,scope['version'],candidate)).fetchone()[0]
    child=uuid4()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','running',%s,%s,%s,%s,'fail_fast')",(child,tid,scope['snapshot'],Jsonb({'project_id':str(scope['project'])}),'b'*64,scope['actor']))
    batch=db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action,child_run_id) VALUES(%s,%s,%s,%s,%s,'apply',%s) RETURNING id",(tid,scope['project'],workflow,scope['source'],scope['actor'],child)).fetchone()[0]
    candidate_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(candidate,)).fetchone()[0]
    source_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(scope['source'],)).fetchone()[0]
    args=(tid,child,target,scope['version'],candidate,candidate_sha,scope['source'],source_sha)
    def allowed(values=args):return db.execute('SELECT photo.semantic_approved_child_source(%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()[0]
    assert not allowed()
    db.execute("UPDATE photo.semantic_targets SET state='queued',batch_id=%s,child_run_id=%s WHERE id=%s",(batch,child,target))
    assert allowed()
    intent_sha=db.execute('SELECT intent_sha FROM photo.semantic_intents WHERE id=%s',(intent,)).fetchone()[0]
    def edit_allowed(artifact=scope['source'],sha=intent_sha):
        return db.execute('SELECT photo.semantic_approved_child_edit(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(*args,artifact,sha)).fetchone()[0]
    assert edit_allowed()
    assert not edit_allowed(artifact=uuid4())
    assert not edit_allowed(sha='0'*64)
    with db.transaction(force_rollback=True):
        with pytest.raises(psycopg.errors.CheckViolation):
            db.execute("UPDATE photo.artifacts SET media_type='application/octet-stream' WHERE id=%s",(scope['source'],))
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(scope['source'],))
        assert not edit_allowed()
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.semantic_approved_child_edit(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text)','EXECUTE')",(role,)).fetchone()[0]
    for n in range(8):
        changed=list(args);changed[n]='0'*64 if n in (5,7) else uuid4()
        assert not allowed(tuple(changed))
    for state in ('completed','cancelled'):
        with db.transaction(force_rollback=True):
            db.execute('UPDATE photo.runs SET status=%s WHERE id=%s',(state,parent))
            assert allowed()
    for query,values in (
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(child,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(candidate,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['source'],)),
        ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert not allowed()
    function='photo.semantic_approved_child_source(uuid,uuid,uuid,uuid,uuid,text,uuid,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,function)).fetchone()[0]
