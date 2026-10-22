"""Saved close receipt authority and strict replay response; authored only."""
from copy import deepcopy
from uuid import uuid4
import psycopg
import pytest
from psycopg.types.json import Jsonb
from semantic_close_fixture import close_fixture, commit_close, replay_close
from removal_transaction_state import tenant_transaction_state


@pytest.mark.parametrize('mutation',['revision_zero','revision_one','revision_unsafe','revision_long',
    'revision_number','revision_missing','extra_field','outputs','error_code','wrong_run','wrong_workflow'])
def test_close_replay_refuses_corrupted_terminal_response_without_writes(db,mutation):
    f=close_fixture(db)
    result=commit_close(db,f)
    body=deepcopy(result)
    if mutation=='revision_zero': body['revision']='0'
    elif mutation=='revision_one': body['revision']='1'
    elif mutation=='revision_unsafe': body['revision']='9007199254740992'
    elif mutation=='revision_long': body['revision']='9'*100
    elif mutation=='revision_number': body['revision']=int(result['revision'])
    elif mutation=='revision_missing': del body['revision']
    elif mutation=='extra_field': body['child']={'run_id':str(uuid4())}
    elif mutation=='outputs': body['outputs']=[{'artifact_id':str(uuid4())}]
    elif mutation=='error_code': body['error_code']='FAILED'
    elif mutation=='wrong_run': body['run_id']=str(uuid4())
    else: body['workflow_id']=str(uuid4())
    db.execute("UPDATE photo.idempotency_records SET response_body=%s WHERE tenant_id=%s AND route='SemanticIntent.CloseReview' AND key=%s",
        (Jsonb(body),f['args'][0],f['key']))
    before=tenant_transaction_state(db,f['args'][0])
    assert replay_close(db,f)==('conflict',None)
    assert tenant_transaction_state(db,f['args'][0])==before


def test_close_replay_requires_canonical_batch_for_request_fingerprint(db):
    f=close_fixture(db)
    commit_close(db,f)
    tenant=f['args'][0]
    alternate=uuid4()
    # A structurally valid close batch in the same workflow/actor is insufficient.
    db.execute('INSERT INTO photo.v2_review_batches(id,tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action) '
        'SELECT %s,b.tenant_id,b.project_id,b.workflow_id,b.decisions_artifact_id,b.actor_id,b.action '
        'FROM photo.v2_review_batches b JOIN photo.idempotency_records r ON r.tenant_id=b.tenant_id AND r.resource_id=b.id '
        "WHERE r.tenant_id=%s AND r.route='SemanticIntent.CloseReview' AND r.key=%s",
        (alternate,tenant,f['key']))
    db.execute("UPDATE photo.idempotency_records SET resource_id=%s WHERE tenant_id=%s AND route='SemanticIntent.CloseReview' AND key=%s",
        (alternate,tenant,f['key']))
    before=tenant_transaction_state(db,tenant)
    assert replay_close(db,f)==('conflict',None)
    assert tenant_transaction_state(db,tenant)==before


@pytest.mark.parametrize('mutation',['revoked_session','foreign_actor','foreign_tenant'])
def test_close_replay_reauthorizes_current_caller(db,mutation):
    f=close_fixture(db)
    commit_close(db,f)
    tenant=f['args'][0]
    kwargs={}
    if mutation=='revoked_session':
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(f['args'][3],))
    elif mutation=='foreign_actor': kwargs['actor']=uuid4()
    else:
        f=deepcopy(f);f['args']=(uuid4(),*f['args'][1:])
    before=tenant_transaction_state(db,tenant)
    with pytest.raises(psycopg.errors.InsufficientPrivilege): replay_close(db,f,**kwargs)
    assert tenant_transaction_state(db,tenant)==before


@pytest.mark.parametrize('expired',['publication','source'])
def test_close_replay_survives_expired_objects_after_success(db,expired):
    f=close_fixture(db)
    result=commit_close(db,f)
    identifier=f['artifact'] if expired=='publication' else f['documents'][0]['artifact_id']
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day',verified_at=NULL WHERE id=%s",(identifier,))
    before=tenant_transaction_state(db,f['args'][0])
    assert replay_close(db,f)==('replayed',result)
    assert tenant_transaction_state(db,f['args'][0])==before
