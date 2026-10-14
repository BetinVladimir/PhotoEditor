"""Derivative source admission and review media guards; no positive pixel qualification."""
from uuid import uuid4


def test_negative_derivative_readiness_remains_private(db):
    signature='photo.v29_negative_task_result_ready(uuid,uuid)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
            (role,signature,'EXECUTE')).fetchone()[0]
    assert not db.execute('SELECT photo.v29_negative_task_result_ready(%s,%s)',
        (uuid4(),uuid4())).fetchone()[0]


def test_negative_derivative_source_retains_scope_and_adoption_barriers(db):
    definition=db.execute("SELECT pg_get_functiondef('photo.v26_render_derivative_source(uuid,uuid)'::regprocedure)").fetchone()[0]
    for marker in ('v26_render_task_result_ready','v29_negative_task_result_ready',
                   'tenant.status','p.archived_at','v2_adoption_event_id',"i.consumer='v2-adopt'",
                   "w.workflow_type='preview'",'NOT r.cancel_requested'):
        assert marker in definition
    # Negative support must not make the render-batch gate accept optics.
    readiness=db.execute("SELECT pg_get_functiondef('photo.v26_render_task_result_ready(uuid,uuid)'::regprocedure)").fetchone()[0]
    assert 'optical.negative' not in readiness


def test_negative_before_preview_retains_pending_accepted_master_authority(db):
    definition=db.execute("SELECT pg_get_functiondef('photo.v26_review_media_source_ready(uuid)'::regprocedure)").fetchone()[0]
    for marker in ('optical.negative',"v.state='pending'","version.review_state='accepted'",
                   'parent.graph_sealed','NOT parent.cancel_requested','asset.project_id=workflow.project_id'):
        assert marker in definition
