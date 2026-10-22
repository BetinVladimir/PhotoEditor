"""Own decision -> CPU shown-copy child SQL; synthetic object/qualification metadata."""
import json
import pytest
import psycopg
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from removal_apply_documents_fixture import committed_copy_fixture


def test_removal_apply_creates_exact_shown_copy_child(database):
    values, task, response = committed_copy_fixture(database)
    data, _, _, _, child = values[:5]
    tenant = data[0][0]
    assert response['child'] == dict(run_id=str(child), state='queued')
    assert database.execute('SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=%s AND id=%s',
        (tenant, child)).fetchone() == ('queued', True)
    row = database.execute('SELECT operation,operation_version,resource_class,state,inputs FROM photo.tasks WHERE id=%s',
        (task['node']['id'],)).fetchone()
    assert row[:4] == ('semantic.adopt_shown_removal', '1', 'cpu', 'ready')
    assert row[4] == [task['transport']['manifest'], *task['transport']['direct_roots']]
    params = json.loads(task['params_body'])
    receipt = database.execute('SELECT v27_removal_preview_receipt FROM photo.runs WHERE id=%s',
        (data[1],)).fetchone()[0]
    assert all(params[key] == receipt[key] for key in ('presentation', 'working_image', 'pixels', 'numerical_evidence'))
    assert database.execute('SELECT state,child_run_id FROM photo.semantic_targets WHERE id=%s',
        (task['target_id'],)).fetchone() == ('queued', child)


@pytest.mark.parametrize('mutation', ['session', 'preview_fence'])
def test_removal_apply_lost_authority_creates_no_partial_child(database, mutation):
    captured = {}
    def before_commit(db, values, task):
        data = values[0]; captured.update(child=values[4], target=task['target_id'])
        if mutation == 'session':
            db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (data[0][3],))
        else:
            db.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s', (data[7],))
    with pytest.raises(psycopg.Error):
        committed_copy_fixture(database, before_commit=before_commit)
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s', (captured['child'],)).fetchone()[0] == 0
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (captured['child'],)).fetchone()[0] == 0
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (captured['target'],)).fetchone() == ('proposed', None, None)
