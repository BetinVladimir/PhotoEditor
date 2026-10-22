"""Own context/draft and real SQL provenance; metadata only, tests not quality evidence."""
from semantic_target_state_fixture import reject_target_with_revision
import json
from hashlib import sha256
from uuid import uuid4,uuid5

import psycopg
import pytest
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
from retouch_apply_documents_fixture import retouch_context_producer_fixture,save_retouch_context_draft
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def landmark_ready(db,args,scope):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(args[0]),))
        value=db.execute('SELECT photo.semantic_retouch_preview_landmark_origin('+','.join(['%s']*7)+')',
            (args[0],scope['project'],args[2],args[3],Jsonb(scope['retouch_params'].source.model_dump(mode='json')),
             scope['landmark_ref'].artifact_id,scope['landmark_ref'].sha256)).fetchone()[0]
        db.execute('RESET ROLE')
        return value


@pytest.mark.parametrize('alpha',[False,True])
def test_own_context_has_canonical_landmark_source_and_replayable_draft(database,alpha):
    args,parent,scope,target=retouch_context_producer_fixture(database,with_alpha=alpha)
    context=RetouchPreviewAdapterContext.model_validate_json(scope['retouch_context_body'])
    assert canonical_bytes(context)==scope['retouch_context_body']
    assert context.source==scope['retouch_params'].source
    assert context.faces[0].landmarks==scope['landmark_ref']
    assert context.faces[0].manual_mode and context.faces[0].object_id is None
    assert context.transferred_mask==ArtifactRef.model_validate_json(canonical_bytes(scope['candidate_mask_ref']))
    assert landmark_ready(database,args,scope) is True
    first=save_retouch_context_draft(database,args,scope)
    ref=scope['retouch_context_ref']
    assert first==uuid5(parent,'semantic-retouch-preview:'+str(target)+':'+args[6]+':'+str(ref.artifact_id)+':'+ref.sha256+':'+str(args[2])+':'+str(args[3]))
    assert save_retouch_context_draft(database,args,scope)==first
    state=database.execute('SELECT status,graph_sealed,cancel_requested,config_snapshot FROM photo.runs WHERE id=%s',(first,)).fetchone()
    assert state[:3]==('draft',False,False)
    saved=state[3]['semantic_retouch_preview']
    assert saved['adapter_context']==context.model_dump(mode='json')
    assert saved['adapter_context_body']==scope['retouch_context_body'].decode()
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(first,)).fetchone()[0]==0
    # Saving content confers neither CPU qualification, preview receipt nor Apply.
    assert database.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND kind='apply'",(args[0],)).fetchone()[0]==0


@pytest.mark.parametrize('mutation',[
    'session','run_cancel','task_cancel','task_fence','attempt_backend','task_backend',
    'missing_source_input','canonical_result','upload_version','upload_sha','artifact_fence','artifact_role',
])
def test_landmark_provenance_rechecks_every_relational_binding(database,mutation):
    args,_,scope,_=retouch_context_producer_fixture(database)
    assert landmark_ready(database,args,scope) is True
    task=scope['landmark_task'];ref=scope['landmark_ref']
    if mutation=='session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    elif mutation=='run_cancel':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(scope['landmark_run'],))
    elif mutation=='task_cancel':
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,))
    elif mutation=='task_fence':
        database.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(task,))
    elif mutation=='attempt_backend':
        database.execute("UPDATE photo.task_attempts SET profile='{}' WHERE task_id=%s",(task,))
    elif mutation=='task_backend':
        database.execute("UPDATE photo.tasks SET execution_profile='{}' WHERE id=%s",(task,))
    elif mutation=='missing_source_input':
        database.execute("UPDATE photo.tasks SET inputs='[]' WHERE id=%s",(task,))
    elif mutation=='canonical_result':
        # Result content is immutable. Rotate the pointer to another existing
        # tenant result rather than bypassing that database invariant.
        foreign=database.execute('SELECT id FROM photo.task_results WHERE tenant_id=%s AND task_id<>%s LIMIT 1',
            (args[0],task)).fetchone()[0]
        database.execute('UPDATE photo.tasks SET canonical_result_id=%s WHERE id=%s',(foreign,task))
    elif mutation=='upload_version':
        database.execute("UPDATE photo.output_uploads SET object_version='foreign-version' WHERE artifact_id=%s",(ref.artifact_id,))
    elif mutation=='upload_sha':
        database.execute('UPDATE photo.output_uploads SET sha256=%s WHERE artifact_id=%s',('f'*64,ref.artifact_id))
    elif mutation=='artifact_fence':
        database.execute('UPDATE photo.artifacts SET produced_fence=produced_fence+1 WHERE id=%s',(ref.artifact_id,))
    else:
        database.execute("UPDATE photo.artifacts SET output_role='document' WHERE id=%s",(ref.artifact_id,))
    assert landmark_ready(database,args,scope) is False


@pytest.mark.parametrize('mutation',['session','target','parent'])
def test_saved_retouch_context_draft_rejects_stale_review(database,mutation):
    args,parent,scope,target=retouch_context_producer_fixture(database)
    run=save_retouch_context_draft(database,args,scope)
    if mutation=='session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    elif mutation=='target':
        reject_target_with_revision(database, target)
    else:
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(parent,))
    with pytest.raises((psycopg.errors.InsufficientPrivilege,psycopg.errors.SerializationFailure)):
        save_retouch_context_draft(database,args,scope)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0]==0


@pytest.mark.parametrize('field',['source','candidate','protection','model_lock_sha','runtime_lock_sha','device_profile_sha'])
def test_rehashed_foreign_context_cannot_save_retouch_draft(database,field):
    args,_,scope,_=retouch_context_producer_fixture(database)
    context=json.loads(scope['retouch_context_body'])
    if field=='source':
        context[field]['asset_version_id']=str(uuid4())
    elif field in ('candidate','protection'):
        context[field]['artifact_id']=str(uuid4())
    else:
        context[field]='f'*64
    scope['retouch_context_body']=canonical_bytes(context)
    scope['retouch_context_ref']=ArtifactRef(artifact_id=scope['retouch_context_ref'].artifact_id,
        sha256=sha256(scope['retouch_context_body']).hexdigest())
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        save_retouch_context_draft(database,args,scope)
    assert database.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'semantic_retouch_preview'",(args[0],)).fetchone()[0]==0
