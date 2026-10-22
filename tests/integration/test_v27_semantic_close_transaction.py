"""Real close SQL transaction scenarios; authored, not executed evidence."""
import json
import psycopg
import pytest
from semantic_close_fixture import close_fixture, commit_close, replay_close
from removal_transaction_state import tenant_transaction_state


@pytest.mark.parametrize('revision', [None, 9007199254740990])
def test_close_completes_parent_once_without_execution_or_target_changes(db, revision):
    f = close_fixture(db, parent_revision=revision)
    tenant = f['args'][0]
    targets = db.execute('SELECT to_jsonb(t) FROM photo.semantic_targets t WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()
    tasks = db.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()
    runs = db.execute('SELECT id FROM photo.runs WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()
    quota = db.execute('SELECT to_jsonb(q) FROM photo.quota_reservations q WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()
    assert replay_close(db, f)==('absent', None)
    result = commit_close(db, f)
    assert result['state']=='completed' and result['revision']==str(f['revision']+1)
    assert result['run_id']==str(f['parent']) and result['outputs']==[] and result['error_code'] is None
    assert db.execute('SELECT status,revision FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant,f['parent'])).fetchone()==('completed',f['revision']+1)
    assert db.execute('SELECT to_jsonb(t) FROM photo.semantic_targets t WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()==targets
    assert db.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()==tasks
    assert db.execute('SELECT id FROM photo.runs WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()==runs
    assert db.execute('SELECT to_jsonb(q) FROM photo.quota_reservations q WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()==quota
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND type='v27.review.closed'", (tenant,)).fetchone()==(1,)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='v27.review.closed'", (tenant,)).fetchone()==(1,)
    before=tenant_transaction_state(db,tenant)
    assert replay_close(db,f)==('replayed',result)
    assert replay_close(db,f,digest='0'*64)==('conflict',None)
    with pytest.raises(psycopg.Error): commit_close(db,f,reserve_receipt=False)
    assert tenant_transaction_state(db,tenant)==before


@pytest.mark.parametrize('revision', [0, 9007199254740991, 9007199254740992, 9223372036854775806])
def test_close_refuses_nonincrementable_revision_before_mutation(db, revision):
    f=close_fixture(db,parent_revision=revision if revision else None)
    f['revision']=revision
    before=tenant_transaction_state(db,f['args'][0])
    with pytest.raises(psycopg.errors.InvalidParameterValue,match='SEMANTIC_CLOSE_INVALID'):
        commit_close(db,f)
    assert tenant_transaction_state(db,f['args'][0])==before


def test_close_refuses_remaining_proposed_targets(db):
    f=close_fixture(db,selected_count=1)
    before=tenant_transaction_state(db,f['args'][0])
    with pytest.raises(psycopg.errors.SerializationFailure,match='SEMANTIC_REVIEW_INCOMPLETE'):
        commit_close(db,f)
    assert tenant_transaction_state(db,f['args'][0])==before


@pytest.mark.parametrize('mutation', ['session','parent_revision','parent_cancel','publication','source','plan','body','missing_document'])
def test_close_authority_and_binding_refusal_rolls_back_receipt_and_parent(db,mutation):
    f=close_fixture(db)
    tenant=f['args'][0]
    with db.transaction(force_rollback=True):
        if mutation=='session': db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(f['args'][3],))
        elif mutation=='parent_revision': db.execute('UPDATE photo.runs SET revision=revision+1 WHERE id=%s',(f['parent'],))
        elif mutation=='parent_cancel': db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(f['parent'],))
        elif mutation=='publication': db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(f['artifact'],))
        elif mutation=='source': db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(f['documents'][0]['artifact_id'],))
        elif mutation=='plan':
            request=json.loads(f['request']); request['request']['plan_sha']='0'*64
            f['request']=json.dumps(request,sort_keys=True,separators=(',',':'))
        elif mutation=='body': f['body']=f['body']+' '
        else: f['documents']=f['documents'][:-1]
        before=tenant_transaction_state(db,tenant)
        with pytest.raises(psycopg.Error): commit_close(db,f)
        assert tenant_transaction_state(db,tenant)==before


def test_close_late_event_failure_is_atomic_and_retryable(db):
    f=close_fixture(db)
    tenant=f['args'][0]
    with db.transaction(force_rollback=True):
        db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991) '
            'ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor',(tenant,))
        before=tenant_transaction_state(db,tenant)
        with pytest.raises(psycopg.errors.CheckViolation,match='EVENT_CURSOR_EXHAUSTED'): commit_close(db,f)
        assert tenant_transaction_state(db,tenant)==before
    assert commit_close(db,f)['state']=='completed'


def test_close_caller_rollback_preserves_all_rows(db):
    f=close_fixture(db)
    before=tenant_transaction_state(db,f['args'][0])
    with db.transaction(force_rollback=True): assert commit_close(db,f)['state']=='completed'
    assert tenant_transaction_state(db,f['args'][0])==before
    assert commit_close(db,f)['state']=='completed'


@pytest.mark.migration_head(2413)
@pytest.mark.parametrize('phase',['pending','completed'])
def test_close_forward_upgrade_preserves_rows_function_identity_and_replay(db,phase):
    from pathlib import Path
    f=close_fixture(db)
    result=None
    if phase=='completed': result=commit_close(db,f)
    else:
        db.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) '
            "VALUES(%s,'user',%s,'SemanticIntent.CloseReview',%s,%s,clock_timestamp()+interval '1 day')",
            (f['args'][0],f['args'][2],f['key'],f['request_sha']))
        assert replay_close(db,f)==('pending',None)
    signatures=['photo.commit_semantic_review_close(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb)',
                'photo.semantic_close_replay(uuid,uuid,uuid,uuid,text,text)']
    identity=[db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone() for signature in signatures]
    before=tenant_transaction_state(db,f['args'][0])
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/02414_semantic_close_safe_revision.sql'
    db.execute(migration.read_text().split('-- +goose Down',1)[0])
    assert [db.execute('SELECT oid,proowner,proacl,proconfig,prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone() for signature in signatures]==identity
    assert tenant_transaction_state(db,f['args'][0])==before
    if phase=='pending': result=commit_close(db,f,reserve_receipt=False)
    assert replay_close(db,f)==('replayed',result)


def test_close_does_not_wait_for_queued_retouch_child(db):
    from semantic_mixed_retouch_fixture import mixed_decision_fixture, commit_mixed_decision
    from semantic_rejection_fixture import rejection_fixture, commit_rejection
    mixed=mixed_decision_fixture(db)
    applied=commit_mixed_decision(db,mixed)
    remaining=rejection_fixture(db,reviews=mixed['reviews'][-1:],selected_count=1,
        parent_revision=int(applied['revision']))
    rejected=commit_rejection(db,remaining)
    f=close_fixture(db,decided=(remaining,rejected))
    tenant=f['args'][0]
    child=db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE tenant_id=%s AND id=%s',
                     (tenant,mixed['child'])).fetchone()
    tasks=db.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE tenant_id=%s AND run_id=%s ORDER BY id',
                     (tenant,mixed['child'])).fetchall()
    targets=db.execute('SELECT to_jsonb(t) FROM photo.semantic_targets t WHERE tenant_id=%s ORDER BY id',
                       (tenant,)).fetchall()
    assert child[0]['status']=='queued' and tasks and all(row[0]['state']=='ready' for row in tasks)
    assert commit_close(db,f)['state']=='completed'
    assert db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE tenant_id=%s AND id=%s',
                      (tenant,mixed['child'])).fetchone()==child
    assert db.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE tenant_id=%s AND run_id=%s ORDER BY id',
                      (tenant,mixed['child'])).fetchall()==tasks
    assert db.execute('SELECT to_jsonb(t) FROM photo.semantic_targets t WHERE tenant_id=%s ORDER BY id',
                      (tenant,)).fetchall()==targets
