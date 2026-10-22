"""Own operation-specific SQL child completion matrix, synthetic outputs only."""
import importlib
import pytest
from test_v27_semantic_child_adoption_rejection import reject
from test_v27_semantic_child_canonical_completion import finalize
from removal_transaction_state import tenant_transaction_state


@pytest.mark.parametrize('operation,module', [
    ('semantic.adopt_shown_exposure', 'exposure_copy_completion_fixture'),
    ('semantic.adopt_shown_removal', 'test_v27_removal_adoption_commit'),
    ('semantic.adopt_shown_background_cleanup', 'test_v27_background_cleanup_adoption_commit'),
    ('semantic.adopt_shown_retouch_region', 'test_v27_retouch_adoption_commit'),
])
@pytest.mark.parametrize('outcome', ['completed', 'failed', 'cancelled'])
def test_own_shown_operation_reaches_exact_child_and_target_terminal_state(db, operation, module, outcome):
    # Each operation uses its own real Apply/dispatch/claim fixture and adopter.
    # No operation name substitution or borrowed approval/qualification context.
    source = importlib.import_module(module)
    values, task, commit = source.successful_copy_metadata_fixture(db)
    tenant, task_id, _, _, result = commit[:5]
    child = values[4]
    assert db.execute('SELECT operation FROM photo.tasks WHERE tenant_id=%s AND id=%s',
                      (tenant, task_id)).fetchone() == (operation,)
    if outcome == 'completed': assert source.adopt(db, commit) is True
    elif outcome == 'failed': assert reject(db, tenant, task_id, result) is True
    else: db.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, child))
    assert finalize(db, tenant, child) == outcome
    target_state = 'applied' if outcome == 'completed' else outcome
    assert db.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',
                      (tenant, child)).fetchall() == [(target_state,)]
    after = tenant_transaction_state(db, tenant)
    assert finalize(db, tenant, child) == outcome
    assert tenant_transaction_state(db, tenant) == after
