"""Real own admission→immutable bundle→receipt SQL, synthetic execution metadata.

No photo/ICC/inference quality, live worker claim or physical object store proof.
"""
from semantic_target_state_fixture import reject_target_with_revision
import json
import psycopg
import pytest
from retouch_preview_bundle_fixture import retouch_bundle_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def seal(db,data,*,document=None,attempt=1,fence=1):
    args,scope=data['args'],data['scope']
    context=scope['retouch_context_ref']
    documents=[data['bodies'][role].decode() for role in ('preview_presentation','preview_evidence','working_image')]
    if document is not None:
        documents[document]+=' '
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        value=db.execute('SELECT photo.seal_semantic_retouch_preview_receipt('+','.join(['%s']*15)+')',
            (args[0],data['run'],args[2],args[3],args[5],args[6],context.artifact_id,context.sha256,
             data['task'],data['result'],attempt,fence,*documents)).fetchone()[0]
        db.execute('RESET ROLE')
        return value


@pytest.mark.parametrize('alpha',[False,True])
def test_own_retouch_receipt_binds_full_admitted_preview_and_replays(database,alpha):
    data=retouch_bundle_fixture(database,with_alpha=alpha,admitted=True)
    receipt=seal(database,data)
    refs=data['refs']
    assert receipt==dict(schema_version=1,params_sha=data['args'][6],task_id=str(data['task']),
        result_id=str(data['result']),attempt=1,fence=1,presentation=refs['preview_presentation'].model_dump(mode='json'),
        working_image=refs['working_image'].model_dump(mode='json'),pixels=refs['pixels'].model_dump(mode='json'),
        numerical_evidence=refs['preview_evidence'].model_dump(mode='json'))
    assert seal(database,data)==receipt
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',(data['run'],)).fetchone()[0]==receipt
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (data['target'],)).fetchone()==('proposed',None,None)
    assert database.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND kind='apply'",(data['args'][0],)).fetchone()[0]==0


@pytest.mark.parametrize('document',[0,1,2])
def test_own_retouch_receipt_requires_original_raw_bytes(database,document):
    data=retouch_bundle_fixture(database,admitted=True)
    with pytest.raises((psycopg.errors.InvalidParameterValue,psycopg.errors.SerializationFailure)):
        seal(database,data,document=document)
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',(data['run'],)).fetchone()[0] is None
    assert seal(database,data)['result_id']==str(data['result'])


@pytest.mark.parametrize('attempt,fence',[(2,1),(1,2),(0,1),(1,0)])
def test_own_retouch_receipt_refuses_foreign_attempt_fence(database,attempt,fence):
    data=retouch_bundle_fixture(database,admitted=True)
    with pytest.raises(psycopg.errors.SerializationFailure):
        seal(database,data,attempt=attempt,fence=fence)
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',(data['run'],)).fetchone()[0] is None


@pytest.mark.parametrize('mutation',['session','target','parent','preview','source','input'])
def test_own_retouch_receipt_rechecks_scope_and_registered_inputs(database,mutation):
    data=retouch_bundle_fixture(database,admitted=True)
    if mutation=='session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(data['args'][3],))
    elif mutation=='target':
        reject_target_with_revision(database, data['target'])
    elif mutation=='parent':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(data['parent'],))
    elif mutation=='preview':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(data['run'],))
    elif mutation=='source':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(data['scope']['source'],))
    else:
        # Mutate lifecycle metadata, not immutable artifact hashes or results.
        ref=data['scope']['retouch_context_ref']
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(ref.artifact_id,))
    with pytest.raises((psycopg.errors.InsufficientPrivilege,psycopg.errors.SerializationFailure)):
        seal(database,data)
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',(data['run'],)).fetchone()[0] is None


@pytest.mark.parametrize('field',['task_id','result_id','attempt','fence','params_sha','pixels'])
def test_own_retouch_receipt_conflict_preserves_saved_receipt_and_target(database,field):
    data=retouch_bundle_fixture(database,admitted=True)
    receipt=seal(database,data)
    conflicting=dict(receipt)
    if field in ('attempt','fence'):
        conflicting[field]+=1
    elif field=='pixels':
        conflicting[field]=receipt['working_image']
    elif field=='params_sha':
        conflicting[field]='0'*64
    else:
        conflicting[field]=str(data['run'])
    # Owner-only fixture corruption represents conflicting historical state;
    # the API must refuse replacement even with otherwise current producer input.
    database.execute('UPDATE photo.runs SET v27_retouch_preview_receipt=%s::jsonb WHERE id=%s',
        (json.dumps(conflicting),data['run']))
    before=database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (data['target'],)).fetchone()
    with pytest.raises(psycopg.errors.SerializationFailure) as failure:
        seal(database,data)
    assert 'SEMANTIC_PREVIEW_RECEIPT_CONFLICT' in str(failure.value)
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
        (data['run'],)).fetchone()[0]==conflicting
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (data['target'],)).fetchone()==before


@pytest.mark.parametrize('mutation',['session','target','parent','preview','source','input'])
def test_own_retouch_saved_receipt_does_not_bypass_late_authority(database,mutation):
    data=retouch_bundle_fixture(database,admitted=True)
    receipt=seal(database,data)
    if mutation=='session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(data['args'][3],))
    elif mutation=='target':
        reject_target_with_revision(database, data['target'])
    elif mutation in ('parent','preview'):
        run=data['parent'] if mutation=='parent' else data['run']
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
    else:
        artifact=data['scope']['source'] if mutation=='source' else data['scope']['retouch_context_ref'].artifact_id
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(artifact,))
    before=database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (data['target'],)).fetchone()
    with pytest.raises((psycopg.errors.InsufficientPrivilege,psycopg.errors.SerializationFailure)):
        seal(database,data)
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
        (data['run'],)).fetchone()[0]==receipt
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (data['target'],)).fetchone()==before
