"""Own current retouch review documents under real SQL session/target authority."""
from semantic_target_state_fixture import reject_target_with_revision
import json
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.semantic.preview_binding import bind_retouch_preview_documents,bind_retouch_preview_source
from retouch_apply_documents_fixture import prepared_retouch_review_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('alpha',[False,True])
def test_retouch_current_review_has_exact_own_documents_and_no_synthetic_preview(database,alpha):
    args,parent,scope,target=prepared_retouch_review_fixture(database,with_alpha=alpha)
    params=scope['retouch_params']
    owned,candidate,intent=bind_retouch_preview_documents(params,scope['candidate_body'],scope['intent_body'])
    image=bind_retouch_preview_source(owned,scope['source_body'])
    assert intent.kind=='retouch_region' and candidate.target==owned.source
    assert (image.alpha is not None) is alpha
    assert canonical_bytes(owned).decode()==args[5]
    current=(args[0],args[1],target,args[2],args[3],int(params.workflow_revision),int(params.target_revision),params.plan.sha256,params.candidate.sha256)
    assert database.execute('SELECT photo.semantic_preapply_preview_current('+','.join(['%s']*9)+')',current).fetchone()[0] is True
    assert database.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'semantic_retouch_preview'",(args[0],)).fetchone()[0]==0
    assert database.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND kind='apply'",(args[0],)).fetchone()[0]==0


@pytest.mark.parametrize('mutation',['session','target','parent'])
def test_retouch_current_review_rechecks_actual_lifecycle(database,mutation):
    args,parent,scope,target=prepared_retouch_review_fixture(database)
    params=scope['retouch_params']
    if mutation=='session':database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    if mutation=='target':reject_target_with_revision(database, target)
    if mutation=='parent':database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(parent,))
    current=(args[0],args[1],target,args[2],args[3],int(params.workflow_revision),int(params.target_revision),params.plan.sha256,params.candidate.sha256)
    assert database.execute('SELECT photo.semantic_preapply_preview_current('+','.join(['%s']*9)+')',current).fetchone()[0] is False


def test_retouch_current_review_rejects_stale_expected_target_revision_without_state_change(database):
    args,parent,scope,target=prepared_retouch_review_fixture(database)
    params=scope['retouch_params']
    before=database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s', (target,)).fetchone()
    assert before == ('proposed',int(params.target_revision),None,None)
    current=(args[0],args[1],target,args[2],args[3],int(params.workflow_revision),int(params.target_revision),params.plan.sha256,params.candidate.sha256)
    sql='SELECT photo.semantic_preapply_preview_current('+','.join(['%s']*9)+')'
    assert database.execute(sql,current).fetchone()[0] is True
    stale=(*current[:6],current[6]+1,*current[7:])
    assert database.execute(sql,stale).fetchone()[0] is False
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s', (target,)).fetchone() == before
    assert database.execute(sql,current).fetchone()[0] is True
