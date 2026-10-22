"""Actual own preview publication/quota/admission SQL; synthetic imported pins only."""
from semantic_target_state_fixture import reject_target_with_revision
import psycopg
import pytest
from psycopg.types.json import Jsonb
from retouch_preview_admission_fixture import prepared_retouch_preview_fixture,activate_retouch_preview_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def documents_ready(db,data):
    args=data['args']
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        value=db.execute('SELECT photo.semantic_retouch_preview_documents_ready(%s,%s,%s,%s,%s,%s)',
            (args[0],data['run'],Jsonb(data['scope']['retouch_params'].model_dump(mode='json')),
             Jsonb(data['node']),Jsonb(data['transport']),Jsonb(data['inventory']))).fetchone()[0]
        db.execute('RESET ROLE')
        return value


@pytest.mark.parametrize('alpha',[False,True])
def test_real_retouch_document_publication_quota_and_atomic_activation(database,alpha):
    data=prepared_retouch_preview_fixture(database,with_alpha=alpha)
    assert documents_ready(database,data) is True
    args=data['args']
    before=database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    base=(args[0],args[1],args[2],args[3],data['target'],data['run'],args[5],args[6])
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        for purpose,ref,raw in data['published']:
            replay=database.execute('SELECT * FROM photo.reserve_semantic_retouch_preview_document('+','.join(['%s']*14)+')',
                (*base,purpose,data['task'],ref['artifact_id'],ref['sha256'],len(raw),'fixture')).fetchone()
            assert replay[0]=='replayed' and replay[3] is True
        assert database.execute('SELECT photo.reserve_semantic_retouch_preview_quota('+','.join(['%s']*10)+')',
            (*base,data['scope']['source_body'].decode(),Jsonb(data['node']['execution_profile']))).fetchone()[0] is True
        database.execute('RESET ROLE')
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()==before
    assert activate_retouch_preview_fixture(database,data)==data['task']
    assert database.execute('SELECT status,graph_sealed,cancel_requested FROM photo.runs WHERE id=%s',(data['run'],)).fetchone()==('queued',True,False)
    row=database.execute('SELECT state,attempt,inputs,execution_profile FROM photo.tasks WHERE id=%s',(data['task'],)).fetchone()
    assert row[:2]==('ready',0)
    assert row[2]==[data['transport']['manifest'],*data['transport']['direct_roots']]
    assert row[3]['v2_entry_count']==len(data['inventory']['entries'])
    assert row[3]['retouch_context']==data['scope']['retouch_context_ref'].model_dump(mode='json')
    actual=database.execute('SELECT artifact_id,sha256,contract,ordinal FROM photo.v2_task_inputs WHERE task_id=%s ORDER BY ordinal',(data['task'],)).fetchall()
    assert len(actual)==len(data['inventory']['entries'])+2
    for ordinal,entry in enumerate(data['inventory']['entries']):
        assert tuple(map(str,actual[ordinal][:3]))==(entry['ref']['artifact_id'],entry['ref']['sha256'],entry['contract'])
        assert actual[ordinal][3]==ordinal
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(data['target'],)).fetchone()==('proposed',None,None)
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_fn_owner')
        assert database.execute('SELECT photo.semantic_retouch_preview_closure_ready(%s)',(data['task'],)).fetchone()[0] is True
        database.execute('RESET ROLE')


@pytest.mark.parametrize('purpose',['task_params','retouch_adapter_context','v2_input_chunk_0','v2_input_manifest'])
def test_exact_published_document_role_required_before_admission(database,purpose):
    data=prepared_retouch_preview_fixture(database)
    assert documents_ready(database,data) is True
    ref=next(ref for name,ref,_ in data['published'] if name==purpose)
    # Role is mutable lifecycle metadata; immutable object SHA/bytes remain intact.
    database.execute("UPDATE photo.artifacts SET output_role='document' WHERE id=%s",(ref['artifact_id'],))
    assert documents_ready(database,data) is False
    with pytest.raises(psycopg.errors.CheckViolation):
        activate_retouch_preview_fixture(database,data)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(data['run'],)).fetchone()[0]==0


@pytest.mark.parametrize('mutation',['session','target','parent','policy','quota','landmark'])
def test_late_review_qualification_quota_or_landmark_loss_rolls_back_activation(database,mutation):
    data=prepared_retouch_preview_fixture(database)
    args=data['args']
    if mutation=='session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    elif mutation=='target':
        reject_target_with_revision(database, data['target'])
    elif mutation=='parent':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(data['parent'],))
    elif mutation=='policy':
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(data['pin']['policy_id'],))
    elif mutation=='quota':
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE holder_type='run' AND holder_id=%s",(data['run'],))
    else:
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(data['scope']['landmark_task'],))
    before=database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    with pytest.raises((psycopg.errors.InsufficientPrivilege,psycopg.errors.SerializationFailure,psycopg.errors.CheckViolation)):
        activate_retouch_preview_fixture(database,data)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(data['run'],)).fetchone()[0]==0
    assert database.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(data['run'],)).fetchone()==('draft',False)
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()==before
