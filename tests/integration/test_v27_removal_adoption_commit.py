"""Own removal SQL lifecycle, synthetic canonical output metadata, unexecuted.

Real dispatch/claim/adoption/version SQL. Does not prove physical object bytes,
worker execution, photographic quality or measured qualification.
"""
import hashlib
import json
from uuid import uuid4
import pytest
import psycopg
from psycopg.types.json import Jsonb
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from removal_apply_documents_fixture import committed_copy_fixture
from removal_transaction_state import tenant_transaction_state


@pytest.mark.parametrize('mode',['missing_result','wrong_attempt','wrong_fence','changed_params'])
def test_removal_adoption_refuses_without_exact_canonical_success(database, mode):
    values, task, commit = successful_copy_metadata_fixture(database)
    tenant = commit[0]; changed = list(commit)
    if mode == 'missing_result':changed[4] = uuid4()
    elif mode == 'wrong_attempt':changed[2] += 1
    elif mode == 'wrong_fence':changed[3] += 1
    else:changed[9] += ' '
    before = tenant_transaction_state(database, tenant)
    assert adopt(database, tuple(changed)) is False
    assert tenant_transaction_state(database, tenant) == before
    # The fixture itself remains a valid positive control after the refusal.
    assert adopt(database, commit) is True


def successful_copy_metadata_fixture(db, *, _initial_root_version="metadata-only-v1"):
    """Real dispatch/claim; synthetic ready output metadata, no object-store proof."""
    if _initial_root_version not in ('metadata-only-v1','','null'):
        raise ValueError('closed metadata-only root version')
    from photo_worker.contracts.base import canonical_bytes
    from removal_copy_dispatch_fixture import dispatched_copy_fixture
    tenant,task_id,child,worker,command,_,values,task=dispatched_copy_fixture(db,include_documents=True)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        claim=db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task_id,worker,Jsonb(command))).fetchone()
        assert claim[0]=='claimed'
        db.execute('RESET ROLE')
    fence=claim[1]
    shown=task['documents']['image']
    output=json.loads(shown)
    pixel=uuid4();root=uuid4()
    output['pixels']['artifact_id']=str(pixel)
    raw=canonical_bytes(output).decode()
    sha=hashlib.sha256(raw.encode()).hexdigest()
    pixel_sha=output['pixels']['sha256']
    pixel_size=db.execute('SELECT bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s',
        (tenant,json.loads(shown)['pixels']['artifact_id'])).fetchone()[0]
    for aid,digest,size,media,role in ((pixel,pixel_sha,pixel_size,'application/x-npy','pixels'),
            (root,sha,len(raw.encode()),'application/json','working_image')):
        key='metadata-only/'+str(aid)
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,1,%s)",
            (aid,tenant,key,digest,size,media,_initial_root_version if aid==root else 'metadata-only-v1',role,task_id,fence))
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
        result=db.execute('SELECT photo.adopt_shown_removal('+','.join(['%s']*14)+')',commit).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def test_removal_sql_adoption_retains_output_once(database):
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


def test_removal_adopt_finalize_materialize_exact_version(database):
    from uuid import UUID,uuid5
    values,task,commit=successful_copy_metadata_fixture(database)
    tenant,task_id=commit[:2]
    child=values[4]
    args=values[0][0]
    assert adopt(database,commit) is True
    assert finalize(database,tenant,child)=='completed'
    assert database.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',
        (tenant,task['target_id'])).fetchone()==('applied',)
    expected=uuid5(UUID(str(task_id)),'V27.ShownAppliedVersion')
    def materialize(body):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            result=database.execute('SELECT photo.materialize_semantic_shown_version(%s,%s,%s,%s,%s)',
                (tenant,task['target_id'],args[2],args[3],body)).fetchone()[0]
            database.execute('RESET ROLE')
            return result
    assert materialize(commit[12])==expected
    assert materialize(commit[12])==expected
    assert materialize(commit[12]+' ') is None
    source=database.execute('SELECT asset_version_id FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',
        (tenant,task['target_id'])).fetchone()[0]
    assert database.execute('SELECT parent_asset_version_id,master_artifact_id,created_by_run_id,content_hash,metadata,review_state FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',
        (tenant,expected)).fetchone()==(source,commit[10],child,commit[11],json.loads(commit[12]),'accepted')
    assert database.execute('SELECT source_version_id,role,ordinal FROM photo.asset_version_sources WHERE tenant_id=%s AND version_id=%s',
        (tenant,expected)).fetchall()==[(source,'semantic_source',0)]
    assert database.execute('SELECT count(*) FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',(tenant,expected)).fetchone()==(1,)


def adoption_state(db,tenant,child):
    return tenant_transaction_state(db, tenant)


@pytest.mark.parametrize('mode',['session','cancel','unverified_root','expired_quota','changed_output','fence','missing_upload','empty_root_version','null_root_version','expired_root','unverified_pixels'])
def test_removal_canonical_result_does_not_bypass_live_adoption_gates(database,mode):
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
        elif mode in ('empty_root_version','null_root_version'):
            before=adoption_state(database,tenant,child)
            with pytest.raises(psycopg.errors.CheckViolation) as exact_failure:
                with database.transaction():
                    database.execute('UPDATE photo.artifacts SET object_version=%s WHERE tenant_id=%s AND id=%s',
                        ('' if mode=='empty_root_version' else 'null',tenant,commit[10]))
            assert exact_failure.value.diag.message_primary=='immutable field artifacts.object_version'
            assert adoption_state(database,tenant,child)==before
        elif mode=='expired_root':
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s",(tenant,commit[10]))
        elif mode=='unverified_pixels':
            pixel=json.loads(commit[12])['pixels']['artifact_id']
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',(tenant,pixel))
        elif mode=='expired_quota':
            database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,child))
        elif mode=='changed_output':changed[12]+=' '
        elif mode=='fence':changed[3]+=1
        else:
            database.execute('DELETE FROM photo.output_uploads WHERE tenant_id=%s AND task_id=%s AND artifact_id=%s',(tenant,task_id,commit[10]))
        if mode not in ('empty_root_version','null_root_version'):
            before=adoption_state(database,tenant,child)
            assert adopt(database,tuple(changed)) is False
            assert adoption_state(database,tenant,child)==before
    assert adopt(database,commit) is True


def test_removal_adoption_event_exhaustion_rolls_back_links_and_marker(database):
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


def test_removal_adoption_replay_rejects_changed_inventory(database):
    from psycopg import errors
    from photo_worker.contracts.base import canonical_bytes
    values,task,commit=successful_copy_metadata_fixture(database)
    assert adopt(database,commit) is True
    before=adoption_state(database,commit[0],values[4])
    inventory=[dict(entry) for entry in commit[5].obj]
    inventory[1]['purpose']='b'*64
    changed=list(commit)
    changed[5]=Jsonb(inventory)
    changed[6]=hashlib.sha256(canonical_bytes(inventory)).hexdigest()
    with pytest.raises(errors.CheckViolation,match='V2_ADOPTION_RESULT_CONFLICT'):
        adopt(database,tuple(changed))
    assert adoption_state(database,commit[0],values[4])==before
    assert adopt(database,commit) is False


def finalize(db, tenant, run):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        result = db.execute('SELECT photo.finalize_shown_semantic_apply_child(%s,%s)',
            (tenant, run)).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('version',['','null'])
def test_removal_adoption_denies_initial_missing_root_version_without_writes(database,version):
    values,task,commit=successful_copy_metadata_fixture(database,_initial_root_version=version)
    tenant=commit[0];child=values[4]
    assert database.execute('SELECT object_version FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,commit[10])).fetchone()==(version,)
    before=adoption_state(database,tenant,child)
    assert adopt(database,commit) is False
    assert adoption_state(database,tenant,child)==before
