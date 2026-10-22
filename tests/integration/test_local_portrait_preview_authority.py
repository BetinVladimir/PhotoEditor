"""Current production preview gate structure; authored only, not executed.

These guards supplement positive relational preview fixtures. They do not
substitute for applying migrations or exercising actual media adoption.
"""
import pytest
from local_portrait_preview_fixture import local_preview_inventory


@pytest.mark.parametrize('index',[0,1])
@pytest.mark.parametrize('role',['manifest','image'])
@pytest.mark.parametrize('loss',['expired','cleaning','unverified','upload_cleaning','stale_task_fence'])
def test_clothing_review_media_loss_revokes_decision_readiness(db,index,role,loss):
    _assert_local_media_loss(db,'portrait.clothing',index,role,loss)


@pytest.mark.parametrize('index',[0,1])
@pytest.mark.parametrize('role',['manifest','image'])
@pytest.mark.parametrize('loss',['expired','cleaning','unverified','upload_cleaning','stale_task_fence'])
def test_hair_automatic_result_media_loss_invariance_and_no_review(db,index,role,loss):
    _assert_local_media_loss(db,'portrait.hair',index,role,loss)


def _assert_local_media_loss(db,operation,index,role,loss):
    item,review,refs=local_preview_inventory(db,operation)
    tid=item['tenant'];manifest,image,task=refs[index]
    artifact=(manifest if role=='manifest' else image)['artifact_id']
    reviewed=operation=='portrait.clothing'
    assert (review is not None)==reviewed
    ready=lambda:db.execute('SELECT photo.v28_local_portrait_review_preview_ready(%s,%s)',(tid,review)).fetchone()[0] if reviewed else db.execute('SELECT photo.v28_local_portrait_result_ready(%s,%s,%s,%s)',(tid,item['run'],item['actor'],item['session'])).fetchone()[0]
    decision=lambda:db.execute('SELECT photo.v28_local_portrait_review_decision_ready(%s,%s,%s,%s)',(tid,review if reviewed else item['task'],item['actor'],item['session'])).fetchone()[0]
    assert ready() is True
    assert decision() is reviewed
    with db.transaction(force_rollback=True):
        if loss=='expired':
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(artifact,))
        elif loss=='cleaning':
            db.execute("UPDATE photo.artifacts SET state='cleaning' WHERE id=%s",(artifact,))
        elif loss=='unverified':
            db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(artifact,))
        elif loss=='upload_cleaning':
            db.execute("UPDATE photo.output_uploads SET state='cleaning' WHERE artifact_id=%s",(artifact,))
        else:
            db.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(task,))
        # Clothing approval depends on displayed media. Hair automatic result
        # authority does not acquire a photographic-review dependency.
        assert ready() is (not reviewed)
        assert decision() is False
    assert ready() is True


def test_local_portrait_after_preview_requires_live_exact_output_tuple(db):
    wrapper=db.execute("SELECT pg_get_functiondef('photo.v28_local_portrait_review_preview_ready(uuid,uuid)'::regprocedure)").fetchone()[0]
    assert 'coalesce(photo.v28_local_portrait_review_preview_ready_media_v2(p_tenant,p_review),false)' in wrapper
    assert 'coalesce(photo.v28_local_portrait_review_preview_ready_media_v1(p_tenant,p_review),false)' in wrapper
    definitions=[db.execute('SELECT pg_get_functiondef(%s::regprocedure)',
        ('photo.v28_local_portrait_review_preview_ready_'+branch+'(uuid,uuid)',)).fetchone()[0]
        for branch in ('media_v1','media_v2')]
    for predicate in [
        'photo.v28_local_portrait_derivative_source(p_tenant,origin.id)',
        "t.node_key IN('render.derivative:preview1600','render.derivative:preview2560')",
        'photo.v26_derivative_receipt_document(d.v26_derivative_receipt)',
        'result.id=t.canonical_result_id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token',
        "manifest.state='ready' AND image.state='ready'",
        'manifest.verified_at IS NOT NULL AND image.verified_at IS NOT NULL',
        'manifest.created_by_task_id=t.id AND image.created_by_task_id=t.id',
        'manifest.produced_attempt=t.attempt AND image.produced_attempt=t.attempt',
        'manifest.produced_fence=t.fencing_token AND image.produced_fence=t.fencing_token',
        'manifest.object_version=mo', 'image.object_version=io',
        "mu.state='ready' AND iu.state='ready'",
        'mu.fence=t.fencing_token AND iu.fence=t.fencing_token',
        'mu.object_version=manifest.object_version', 'iu.object_version=image.object_version',
        'mu.sha256=manifest.sha256 AND mu.bytes=manifest.bytes',
        'iu.sha256=image.sha256 AND iu.bytes=image.bytes',
        'manifest.expires_at>clock_timestamp()', 'image.expires_at>clock_timestamp()',
        "receipt.body->'manifest'->>'profile_hash'", 'result.outputs @> jsonb_build_array',
    ]:
        for definition in definitions:
            assert predicate in definition, predicate
    for consumer in ['photo.v28_local_portrait_review_decision_ready(uuid,uuid,uuid,uuid)',
                     'photo.get_v28_local_portrait_review_selection(uuid,uuid,uuid,uuid)']:
        body=db.execute('SELECT pg_get_functiondef(%s::regprocedure)',(consumer,)).fetchone()[0]
        assert 'photo.v28_local_portrait_review_preview_ready(p_tenant,p_review)' in body
