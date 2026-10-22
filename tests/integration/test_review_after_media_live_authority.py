"""Authored live media regression. Relational inventory, no photo quality claim."""
import pytest
import json
from test_open_eyes_selected_apply import adopted_open_eyes
from local_portrait_preview_fixture import seed_review_media_inventory
from relight_review_publication_fixture import relight_publication_fixture, publish_relight_review


@pytest.mark.parametrize('index',[0,1])
@pytest.mark.parametrize('role',['manifest','image'])
@pytest.mark.parametrize('loss',['expired','cleaning','unverified','upload_cleaning','stale_task_fence'])
def test_relight_current_preview_withdraws_after_catalog_loss(db,index,role,loss):
    # Exercises real publisher/derivative-source/preview gates. Full Relight
    # decision qualification and viewed acknowledgement remain separate tests.
    item=relight_publication_fixture(db)
    review=publish_relight_review(db,item);db.execute('RESET ROLE')
    assert review is not None
    assert db.execute('SELECT r.status,w.workflow_type FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id WHERE r.id=%s',(item['run'],)).fetchone()==('waiting_review','preview')
    tid=item['tenant']
    actor,snapshot=db.execute('SELECT created_by,input_snapshot_id FROM photo.runs WHERE id=%s',(item['run'],)).fetchone()
    root=db.execute('SELECT outputs->0 FROM photo.task_results WHERE id=%s',(item['result'],)).fetchone()[0]
    image_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(item['candidate'],)).fetchone()[0]
    base=json.loads(item['body'])['base']
    refs=seed_review_media_inventory(db,dict(item,actor=actor),
        dict(root=root,base=base['source'],candidate=dict(artifact_id=str(item['candidate']),sha256=image_sha)),snapshot,base['asset_version_id'])
    ready=lambda:db.execute('SELECT photo.v28_relight_review_preview_ready(%s,%s)',(tid,review)).fetchone()[0]
    assert ready() is True
    manifest,image,task=refs[index];artifact=(manifest if role=='manifest' else image)['artifact_id']
    with db.transaction(force_rollback=True):
        if loss=='expired':db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(artifact,))
        elif loss=='cleaning':db.execute("UPDATE photo.artifacts SET state='cleaning' WHERE id=%s",(artifact,))
        elif loss=='unverified':db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(artifact,))
        elif loss=='upload_cleaning':db.execute("UPDATE photo.output_uploads SET state='cleaning' WHERE artifact_id=%s",(artifact,))
        else:db.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(task,))
        assert ready() is False
    assert ready() is True


@pytest.mark.parametrize('index',[0,1])
@pytest.mark.parametrize('role',['manifest','image'])
@pytest.mark.parametrize('loss',['expired','cleaning','unverified','upload_cleaning','stale_task_fence'])
def test_open_eyes_selected_media_loss_withdraws_current_selection(db,index,role,loss):
    item=adopted_open_eyes(db);tid,review=item['tenant'],item['review']
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
    manifest,image,task=item['media_refs'][index]
    artifact=(manifest if role=='manifest' else image)['artifact_id']
    selection=lambda:db.execute('SELECT count(*) FROM photo.get_v28_open_eyes_review_selection(%s,%s,%s,%s)',(tid,item['actor'],item['session'],review)).fetchone()[0]
    assert selection()==1
    with db.transaction(force_rollback=True):
        if loss=='expired':db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(artifact,))
        elif loss=='cleaning':db.execute("UPDATE photo.artifacts SET state='cleaning' WHERE id=%s",(artifact,))
        elif loss=='unverified':db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(artifact,))
        elif loss=='upload_cleaning':db.execute("UPDATE photo.output_uploads SET state='cleaning' WHERE artifact_id=%s",(artifact,))
        else:db.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(task,))
        assert selection()==0
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT photo.apply_v28_open_eyes_version(%s,%s,%s,%s,%s,%s,%s)',(tid,review,item['actor'],item['session'],'a'*64,item['base'],item['image'])).fetchone()[0] is None
    db.execute('RESET ROLE')
    assert selection()==1


def test_live_media_is_private_and_repeated_by_open_eyes_and_relight_readers(db):
    signature='photo.review_after_media_live(uuid,uuid,uuid,text,text)'
    for role in ['photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_maintenance','public']:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False
    for signature in ['photo.v28_open_eyes_review_preview_ready(uuid,uuid)',
                      'photo.v28_relight_review_preview_ready(uuid,uuid)',
                      'photo.get_v28_open_eyes_review_selection(uuid,uuid,uuid,uuid)',
                      'photo.get_v28_relight_review_selection_pre_authority(uuid,uuid,uuid,uuid)']:
        body=db.execute('SELECT pg_get_functiondef(%s::regprocedure)',(signature,)).fetchone()[0]
        assert 'photo.review_after_media_live(p_tenant,preview.id,v.candidate_artifact_id,v.candidate_hash,preview.config_snapshot' in body
