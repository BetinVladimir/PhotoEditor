"""Own exposure preview SQL prerequisites; synthetic output metadata only."""
import json
import pytest
from exposure_review_documents_fixture import prepared_exposure_reviews
from exposure_preview_receipt_fixture import receipt_fixture, seal
from semantic_qualification_fixture import frozen_qualification


@pytest.mark.parametrize('alpha,dense', [(False,0),(True,0),(True,1)])
def test_exposure_preview_receipt_only_for_selected_approved_candidate(db,alpha,dense):
    reviews = prepared_exposure_reviews(db,target_count=3,with_alpha=alpha,dense_warps=dense)
    args,_,scope,_ = reviews[0]
    pin = frozen_qualification(db,args[0],scope['project'],scope['actor'],operation='semantic.local_exposure',backend='cpu')
    scope['exposure_preview_qualification'] = pin
    data = receipt_fixture(db,review=reviews[0])
    receipt = seal(db,data)
    assert seal(db,data) == receipt
    args,run,scope = data[:3]
    params = json.loads(args[5])
    assert scope['preview_node']['operation'] == 'semantic.preview_local_exposure'
    assert scope['preview_node']['resource_class'] == 'cpu'
    assert json.loads(data[5])['algorithm'] == 'local_exposure_exact_replay_v1'
    assert receipt['pixels'] == json.loads(data[6])['pixels']
    # Original targets remain proposed. The other two have no preview runs.
    for original,_,_,target in reviews:
        assert db.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',
                          (args[0],target)).fetchone() == ('proposed',None,None)
    for original,_,_,target in reviews[1:]:
        assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot->'semantic_preapply_preview'->'params'->>'target_id'=%s",
                          (args[0],str(target))).fetchone()[0] == 0
    selected = (args[0],args[1],args[2],args[3],int(params['workflow_revision']),args[4],int(params['target_revision']),
                params['plan']['sha256'],params['candidate']['sha256'],receipt['working_image']['artifact_id'],receipt['working_image']['sha256'],receipt['presentation']['artifact_id'],receipt['presentation']['sha256'])
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        rows = db.execute('SELECT * FROM photo.lock_semantic_preapply_preview_selection('+','.join(['%s']*13)+')',selected).fetchall()
        db.execute('RESET ROLE')
    assert len(rows) == 1
