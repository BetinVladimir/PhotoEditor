"""Own retouch locked adoption scenarios; no authority stubs.

Actual own review/preview receipt/Apply/dispatch/claim, then synthetic canonical
success/upload metadata. No physical copy execution or storage proof is claimed.
Immutable artifacts/results are constructed once; scenarios remain unexecuted.
"""
import hashlib
import json
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_v27_retouch_preview_sql_roles import database, isolated_database_config
from retouch_copy_apply_fixture import committed_copy_fixture


@pytest.mark.parametrize('mode',['missing_result','wrong_attempt','wrong_fence','changed_params'])
def test_retouch_adoption_refuses_without_canonical_success(database,mode):
    values,task,_ = committed_copy_fixture(database)
    args=values[0][0]
    tenant,actor,session=args[0],args[2],args[3]
    task_id=task['node']['id']
    child=values[4]
    root=uuid4()
    shown=task['documents']['image']
    output=json.loads(shown)
    output['pixels']['artifact_id']=str(uuid4())
    output_body=json.dumps(output,sort_keys=True,separators=(',',':'))
    params_body=task['params_body']+(' ' if mode=='changed_params' else '')
    def snapshot():
        return (
            database.execute('SELECT state,attempt,fencing_token,canonical_result_id FROM photo.tasks WHERE id=%s',(task_id,)).fetchone(),
            database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(child,)).fetchone(),
            database.execute('SELECT id,state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s ORDER BY id',(tenant,child)).fetchall(),
            database.execute('SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=%s',(tenant,)).fetchone(),
            database.execute('SELECT count(*) FROM photo.inbox WHERE tenant_id=%s',(tenant,)).fetchone(),
            database.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s',(tenant,)).fetchone(),
            database.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(tenant,)).fetchone(),
            database.execute('SELECT count(*) FROM photo.asset_versions WHERE tenant_id=%s',(tenant,)).fetchone(),
        )
    before=snapshot()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_scheduler')
        accepted=database.execute('SELECT photo.adopt_shown_retouch('+','.join(['%s']*14)+')',
            (tenant,task_id,2 if mode=='wrong_attempt' else 1,99 if mode=='wrong_fence' else 1,
             uuid4(),Jsonb([]),'a'*64,task['params_artifact_id'],task['params_sha'],params_body,
             root,hashlib.sha256(output_body.encode()).hexdigest(),output_body,shown)).fetchone()[0]
        assert accepted is False
        database.execute('RESET ROLE')
    assert snapshot()==before
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        database.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert database.execute('SELECT photo.materialize_semantic_shown_version(%s,%s,%s,%s,%s)',
            (tenant,task['target_id'],actor,session,output_body)).fetchone()[0] is None
        database.execute('RESET ROLE')
    assert snapshot()==before


def successful_copy_metadata_fixture(db,*,with_alpha=False,gateway_allocations=False,historical_writer_head=None):
    """Real dispatch/claim; synthetic ready output metadata, no object-store proof."""
    from photo_worker.contracts.base import canonical_bytes
    from test_v27_shown_retouch_worker_claim import dispatched_copy_fixture
    tenant,task_id,child,worker,command,_,values,task=dispatched_copy_fixture(db,include_documents=True,with_alpha=with_alpha,historical_writer_head=historical_writer_head)
    if gateway_allocations:
        db.execute('UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE tenant_id=%s AND worker_id=%s',('f'*64,tenant,worker))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        claim=db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task_id,worker,Jsonb(command))).fetchone()
        assert claim[0]=='claimed'
        db.execute('RESET ROLE')
    fence=claim[1]
    shown=task['documents']['image']
    output=json.loads(shown)
    pixel=uuid4();root=uuid4()
    pixel_size=db.execute('SELECT bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s',
        (tenant,output['pixels']['artifact_id'])).fetchone()[0]
    def allocate(role,size,digest,media):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_gateway')
            ticket=db.execute('SELECT * FROM photo.begin_output(%s,1,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (task_id,fence,worker,'f'*64,uuid4(),role,size,digest,media,'fixture')).fetchone()
            assert ticket[0]=='created'
            db.execute('RESET ROLE')
        return ticket
    pixel_ticket=allocate('pixels',pixel_size,output['pixels']['sha256'],'application/x-npy') if gateway_allocations else None
    if pixel_ticket is not None:pixel=pixel_ticket[2]
    output['pixels']['artifact_id']=str(pixel)
    raw=canonical_bytes(output).decode()
    sha=hashlib.sha256(raw.encode()).hexdigest()
    pixel_sha=output['pixels']['sha256']
    for aid,digest,size,media,role in ((pixel,pixel_sha,pixel_size,'application/x-npy','pixels'),
            (root,sha,len(raw.encode()),'application/json','working_image')):
        key='metadata-only/'+str(aid)
        bucket='fixture'
        if gateway_allocations:
            ticket=pixel_ticket if role=='pixels' else allocate(role,size,digest,media)
            aid=ticket[2]
            if role=='working_image':root=aid
            bucket,key=db.execute('SELECT bucket,object_key FROM photo.output_uploads WHERE id=%s',(ticket[1],)).fetchone()
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,'metadata-only-v1',clock_timestamp(),%s,%s,1,%s)",
            (aid,tenant,bucket,key,digest,size,media,role,task_id,fence))
        if gateway_allocations:
            db.execute("UPDATE photo.output_uploads SET state='ready',object_version='metadata-only-v1',completed_at=clock_timestamp() WHERE id=%s",(ticket[1],))
        else:
            db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES(%s,%s,1,%s,%s,%s,%s,%s,%s,'fixture',%s,%s,%s,%s,'ready','metadata-only-v1',clock_timestamp())",
                (tenant,task_id,fence,worker,'a'*64,uuid4(),aid,role,key,digest,size,media))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,%s,%s,'{}',%s) RETURNING id",
        (tenant,task_id,fence,Jsonb([dict(artifact_id=str(root),sha256=sha)]),sha)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',lease_until=NULL,canonical_result_id=%s WHERE tenant_id=%s AND id=%s",(result,tenant,task_id))
    inventory=[dict(artifact_id=str(aid),sha256=digest,purpose=hashlib.sha256(('fixture/'+str(aid)).encode()).hexdigest(),ordinal=i)
        for i,(aid,digest) in enumerate(((root,sha),(pixel,pixel_sha)))]
    digest=hashlib.sha256(canonical_bytes(inventory)).hexdigest()
    commit=(tenant,task_id,1,fence,result,Jsonb(inventory),digest,task['params_artifact_id'],task['params_sha'],task['params_body'],root,sha,raw,shown)
    return values,task,commit


def adopt(db,commit):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        result=db.execute('SELECT photo.adopt_shown_retouch('+','.join(['%s']*14)+')',commit).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def test_retouch_sql_adoption_retains_output_once(database):
    values,task,commit=successful_copy_metadata_fixture(database)
    tenant,task_id,_,_,result=commit[:5]
    assert adopt(database,commit) is True
    assert database.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(tenant,task_id,result)).fetchone()==(1,)
    assert database.execute('SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=%s AND artifact_id IN(%s,%s)',
        (tenant,commit[10],json.loads(commit[12])['pixels']['artifact_id'])).fetchone()==(2,)
    before=database.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.task.adopted'",(tenant,values[4])).fetchone()
    assert before==(1,)
    assert adopt(database,commit) is False
    assert database.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.task.adopted'",(tenant,values[4])).fetchone()==before


def adoption_state(db,tenant,child):
    # Compare contents as well as counts: a replacement/update must not hide
    # behind an unchanged row count. Identifiers are a fixed local allowlist.
    tables=('v2_artifact_links','inbox','run_events','outbox','tenant_event_cursors',
        'tenant_usage','quota_reservations','tasks','task_results','semantic_targets','asset_versions','asset_version_sources')
    state={table:db.execute(
        f'SELECT to_jsonb(snapshot_row) FROM photo.{table} AS snapshot_row WHERE tenant_id=%s ORDER BY to_jsonb(snapshot_row)::text',
        (tenant,)).fetchall() for table in tables}
    state['child']=db.execute('SELECT to_jsonb(snapshot_row) FROM photo.runs AS snapshot_row WHERE tenant_id=%s AND id=%s',
        (tenant,child)).fetchall()
    return state


@pytest.mark.parametrize('mode',['session','cancel','unverified_root','expired_quota','changed_output','fence','missing_upload','expired_root','unverified_pixels',
    'preview_cancel','producer_cancel','task_cancel','policy','project','input'])
def test_retouch_canonical_result_does_not_bypass_live_adoption_gates(database,mode):
    values,task,commit=successful_copy_metadata_fixture(database)
    tenant,task_id=commit[:2];child=values[4]
    with database.transaction(force_rollback=True):
        changed=list(commit)
        if mode=='session':
            database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(values[0][0][3],))
        elif mode=='cancel':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s',(tenant,child))
        elif mode=='unverified_root':
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',(tenant,commit[10]))
        elif mode=='expired_root':
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s",(tenant,commit[10]))
        elif mode=='unverified_pixels':
            pixel=json.loads(commit[12])['pixels']['artifact_id']
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',(tenant,pixel))
        elif mode=='expired_quota':
            database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,child))
        elif mode=='changed_output':changed[12]+=' '
        elif mode=='fence':changed[3]+=1
        elif mode=='preview_cancel':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s',
                (tenant,json.loads(task['params_body'])['preview_run_id']))
        elif mode=='producer_cancel':
            database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=%s AND id=%s',
                (tenant,json.loads(task['params_body'])['preview_task_id']))
        elif mode=='task_cancel':
            database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=%s AND id=%s',(tenant,task_id))
        elif mode=='policy':
            database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',
                (task['node']['execution_profile']['qualification']['policy_id'],))
        elif mode=='project':
            database.execute('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=%s AND id=%s',
                (tenant,values[0][2]['project']))
        elif mode=='input':
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                (tenant,task['params_artifact_id']))
        elif mode=='missing_upload':
            database.execute('DELETE FROM photo.output_uploads WHERE tenant_id=%s AND task_id=%s AND artifact_id=%s',(tenant,task_id,commit[10]))
        else:
            raise AssertionError('Unknown authority mutation')
        before=adoption_state(database,tenant,child)
        assert adopt(database,tuple(changed)) is False
        assert adoption_state(database,tenant,child)==before
    assert adopt(database,commit) is True


def test_retouch_adoption_event_exhaustion_rolls_back_links_and_marker(database):
    from psycopg import errors
    values,task,commit=successful_copy_metadata_fixture(database)
    tenant=commit[0];child=values[4]
    with database.transaction(force_rollback=True):
        database.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991) ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor',(tenant,))
        before=adoption_state(database,tenant,child)
        with pytest.raises(errors.CheckViolation,match='EVENT_CURSOR_EXHAUSTED'):
            adopt(database,commit)
        assert adoption_state(database,tenant,child)==before
        assert database.execute('SELECT last_cursor FROM photo.tenant_event_cursors WHERE tenant_id=%s',(tenant,)).fetchone()==(9007199254740991,)
    assert adopt(database,commit) is True


@pytest.mark.parametrize('mutation',['purpose','digest_only','purpose_same_digest','order','missing_entry'])
def test_retouch_adoption_replay_rejects_changed_inventory(database,mutation):
    from psycopg import errors
    from photo_worker.contracts.base import canonical_bytes
    values,task,commit=successful_copy_metadata_fixture(database)
    assert adopt(database,commit) is True
    before=adoption_state(database,commit[0],values[4])
    inventory=[dict(entry) for entry in commit[5].obj]
    if mutation in ('purpose','purpose_same_digest'):
        inventory[1]['purpose']='b'*64
    elif mutation=='order':
        inventory.reverse()
        for i,entry in enumerate(inventory):entry['ordinal']=i
    elif mutation=='missing_entry':
        inventory.pop()
    changed=list(commit)
    changed[5]=Jsonb(inventory)
    if mutation=='digest_only':
        changed[6]='f'*64 if commit[6]!='f'*64 else 'e'*64
    elif mutation!='purpose_same_digest':
        changed[6]=hashlib.sha256(canonical_bytes(inventory)).hexdigest()
    with pytest.raises(errors.CheckViolation,match='V2_ADOPTION_RESULT_CONFLICT'):
        adopt(database,tuple(changed))
    assert adoption_state(database,commit[0],values[4])==before
    assert adopt(database,commit) is False
    assert adoption_state(database,commit[0],values[4])==before


def test_exact_retouch_adoption_replay_preserves_all_retained_state(database):
    values,task,commit=successful_copy_metadata_fixture(database)
    assert adopt(database,commit) is True
    before=adoption_state(database,commit[0],values[4])
    for _ in range(3):
        assert adopt(database,commit) is False
        assert adoption_state(database,commit[0],values[4])==before


@pytest.mark.parametrize('failure',['unverified_tail','invalid_second_ordinal'])
def test_retouch_retention_failure_after_first_link_rolls_back(database,failure):
    from psycopg import errors
    from photo_worker.contracts.base import canonical_bytes
    values,task,commit=successful_copy_metadata_fixture(database)
    inventory=[dict(entry) for entry in commit[5].obj]
    if failure=='unverified_tail':
        inventory.append(dict(artifact_id=str(uuid4()),sha256='a'*64,purpose='b'*64,ordinal=2))
    else:
        inventory[1]['ordinal']=7
    changed=list(commit)
    changed[5]=Jsonb(inventory)
    changed[6]=hashlib.sha256(canonical_bytes(inventory)).hexdigest()
    before=adoption_state(database,commit[0],values[4])
    if failure=='unverified_tail':
        with pytest.raises(errors.CheckViolation,match='V2_ADOPTION_ARTIFACT_UNVERIFIED'):
            adopt(database,tuple(changed))
    else:
        # 22023 is caught by the dedicated SQL wrapper's EXCEPTION block,
        # whose subtransaction must also roll back the first inserted link.
        assert adopt(database,tuple(changed)) is False
    assert adoption_state(database,commit[0],values[4])==before
    assert adopt(database,commit) is True


def test_generic_adopter_cannot_bypass_dedicated_retouch_proof(database):
    values,task,commit=successful_copy_metadata_fixture(database)
    before=adoption_state(database,commit[0],values[4])
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_scheduler')
        assert database.execute('SELECT photo.adopt_v2_plan('+','.join(['%s']*7)+')',commit[:7]).fetchone()[0] is False
        database.execute('RESET ROLE')
    assert adoption_state(database,commit[0],values[4])==before
    assert adopt(database,commit) is True


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_scheduler','photo_semantic_admission'])
def test_internal_retouch_retention_helper_is_not_directly_executable(database,role):
    signature='photo.adopt_shown_retouch_inventory(uuid,uuid,integer,bigint,uuid,jsonb,text)'
    assert database.execute('SELECT has_function_privilege(%s,%s,%s)',
        (role,signature,'EXECUTE')).fetchone()[0] is False
