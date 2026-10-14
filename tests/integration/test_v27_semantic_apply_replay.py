from uuid import uuid4
import pytest
import psycopg
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_replay_requires_current_scope_and_committed_batch(db):
    tid,task,_,_,_,_,scope=fixture(db,snapshot_authority=True)
    workflow,project,snapshot=db.execute('SELECT w.id,w.project_id,w.input_snapshot_id FROM photo.v2_workflows w JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id WHERE t.id=%s',(task,)).fetchone()
    intent=db.execute('INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id',(tid,project,workflow,scope['version'],snapshot,scope['source'],'5'*64)).fetchone()[0]
    def replay(key='owned-key',sha='a'*64,actor=scope['actor'],session=scope['session'],owner=intent):
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE photo_api')
            return db.execute('SELECT outcome,response_body FROM photo.semantic_apply_replay(%s,%s,%s,%s,%s,%s)',(tid,owner,actor,session,key,sha)).fetchone()
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_api')
        identity=db.execute('SELECT run_id,workflow_id,project_id FROM photo.semantic_review_identity(%s,%s,%s,%s)',(tid,intent,scope['actor'],scope['session'])).fetchone()
        assert identity[1:]==(workflow,project)
    assert replay()==('absent',None)
    receipt=db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES(%s,'user',%s,'SemanticIntent.Apply','owned-key',%s,clock_timestamp()+interval '1 day') RETURNING id",(tid,scope['actor'],'a'*64)).fetchone()[0]
    assert replay()==('pending',None)
    assert replay(sha='b'*64)==('conflict',None)
    batch=db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action) VALUES(%s,%s,%s,%s,%s,'decide') RETURNING id",(tid,project,workflow,scope['source'],scope['actor'])).fetchone()[0]
    body={'workflow_id':str(workflow),'revision':'9','remaining':0,'child':None,'batch_id':str(batch)}
    db.execute('UPDATE photo.idempotency_records SET response_status=200,response_body=%s,resource_id=%s WHERE id=%s',(Jsonb(body),batch,receipt))
    assert replay()==('replayed',body)
    # Replay does not reread expired object bytes or require a still-open parent.
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval \'1 second\' WHERE id=%s',(scope['source'],))
        assert replay()==('replayed',body)
    for column,value in [('resource_id',uuid4()),('response_status',202),('response_body',Jsonb(dict(body,workflow_id=str(uuid4()))))]:
        with db.transaction(force_rollback=True):
            db.execute(f'UPDATE photo.idempotency_records SET {column}=%s WHERE id=%s',(value,receipt))
            assert replay()==('conflict',None)
    for arguments in ({'actor':uuid4()},{'session':uuid4()},{'owner':uuid4()}):
        with pytest.raises(psycopg.errors.InsufficientPrivilege):
            replay(**arguments)
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],))
        with pytest.raises(psycopg.errors.InsufficientPrivilege):
            replay()
