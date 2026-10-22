"""Own accepted-version SQL over retained retouch metadata; no physical QA."""
import json
from uuid import UUID,uuid4,uuid5
import pytest
from test_v27_retouch_preview_sql_roles import database, isolated_database_config
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture,adopt,adoption_state
from test_v27_retouch_child_finalization import finalize


def materialize(db,values,task,body,*,actor=None,session=None,tenant_context=None):
    args=values[0][0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",
            (str(args[0] if tenant_context is None else tenant_context),))
        result=db.execute('SELECT photo.materialize_semantic_shown_version(%s,%s,%s,%s,%s)',
            (args[0],task['target_id'],args[2] if actor is None else actor,
             args[3] if session is None else session,body)).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('with_alpha',[False,True])
def test_retouch_materializes_exact_deterministic_version_once(database,with_alpha):
    values,task,commit=successful_copy_metadata_fixture(database,with_alpha=with_alpha)
    tenant,task_id=commit[:2];child=values[4]
    before=adoption_state(database,tenant,child)
    assert materialize(database,values,task,commit[12]) is None
    assert adoption_state(database,tenant,child)==before
    assert adopt(database,commit) is True
    before=adoption_state(database,tenant,child)
    assert materialize(database,values,task,commit[12]) is None
    assert adoption_state(database,tenant,child)==before
    assert finalize(database,tenant,child)=='completed'
    expected=uuid5(UUID(str(task_id)),'V27.ShownAppliedVersion')
    assert materialize(database,values,task,commit[12])==expected
    source=database.execute('SELECT asset_version_id FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',
        (tenant,task['target_id'])).fetchone()[0]
    assert database.execute('SELECT parent_asset_version_id,master_artifact_id,created_by_run_id,content_hash,metadata,review_state FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',
        (tenant,expected)).fetchone()==(source,commit[10],child,commit[11],json.loads(commit[12]),'accepted')
    assert database.execute('SELECT source_version_id,role,ordinal FROM photo.asset_version_sources WHERE tenant_id=%s AND version_id=%s',
        (tenant,expected)).fetchall()==[(source,'semantic_source',0)]
    metadata=json.loads(commit[12]);shown=json.loads(commit[13])
    assert metadata.get('alpha')==shown.get('alpha')
    assert (metadata.get('alpha') is not None) is with_alpha
    assert metadata['pixels']['artifact_id']!=shown['pixels']['artifact_id']
    assert metadata['pixels']['sha256']==shown['pixels']['sha256']
    after=adoption_state(database,tenant,child)
    for _ in range(2):
        assert materialize(database,values,task,commit[12])==expected
        assert adoption_state(database,tenant,child)==after


@pytest.mark.parametrize('mutation',['body','foreign_actor','foreign_session','foreign_tenant',
    'session','project','root','child_cancel','task_cancel'])
def test_retouch_version_refuses_stale_authority_without_insertion(database,mutation):
    values,task,commit=successful_copy_metadata_fixture(database,with_alpha=True)
    tenant,task_id=commit[:2];child=values[4]
    assert adopt(database,commit) is True
    assert finalize(database,tenant,child)=='completed'
    with database.transaction(force_rollback=True):
        options={};body=commit[12]
        if mutation=='body':body+=' '
        elif mutation=='foreign_actor':options['actor']=uuid4()
        elif mutation=='foreign_session':options['session']=uuid4()
        elif mutation=='foreign_tenant':options['tenant_context']=uuid4()
        elif mutation=='session':
            database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(values[0][0][3],))
        elif mutation=='project':
            database.execute('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=%s AND id=%s',
                (tenant,values[0][2]['project']))
        elif mutation=='root':
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',(tenant,commit[10]))
        elif mutation=='child_cancel':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s',(tenant,child))
        elif mutation=='task_cancel':
            database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=%s AND id=%s',(tenant,task_id))
        before=adoption_state(database,tenant,child)
        assert materialize(database,values,task,body,**options) is None
        assert adoption_state(database,tenant,child)==before
    assert materialize(database,values,task,commit[12])==uuid5(UUID(str(task_id)),'V27.ShownAppliedVersion')
