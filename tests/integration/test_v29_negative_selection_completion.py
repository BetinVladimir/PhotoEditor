"""Selected-result completion gates; positive lifecycle remains separate."""
from uuid import uuid4


def test_negative_completion_helper_is_private_and_missing_selection_refused(db):
    signature='photo.v29_negative_review_completion_ready(uuid,uuid)'
    for role in ('photo_scheduler','photo_api','photo_worker','photo_gateway','public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
            (role,signature,'EXECUTE')).fetchone()[0]
    assert not db.execute('SELECT photo.v29_negative_review_completion_ready(%s,%s)',
        (uuid4(),uuid4())).fetchone()[0]


def test_rejected_negative_cannot_complete_with_downstream_consumers(db):
    definition=db.execute("SELECT pg_get_functiondef('photo.v29_negative_review_completion_ready(uuid,uuid)'::regprocedure)").fetchone()[0]
    for marker in ('v29_negative_review_selection',"selected.route IN('base','skip')",
                   'photo.task_edges','edge.prerequisite_id=p_task','consumer.inputs','photo.v2_task_inputs',
                   'consumer_run.v26_derivative_task_id','source_result_id','origin.canonical_result_id'):
        assert marker in definition


def test_finalizer_and_polling_use_selected_result_gate(db):
    for signature in ('photo.finalize_v2_run(uuid,uuid)','photo.v2_pending_finalizations()'):
        definition=db.execute('SELECT pg_get_functiondef(%s::regprocedure)',(signature,)).fetchone()[0]
        assert 'v29_negative_review_completion_ready' in definition
        assert "'render.compose'" in definition and "'candidate'" in definition
        assert 'decided_by IS NOT NULL' in definition
