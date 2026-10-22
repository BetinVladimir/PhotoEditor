"""Real review-source authority over own removal documents, metadata fixture only."""
from semantic_target_state_fixture import reject_target_with_revision
import json
import pytest
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from removal_apply_documents_fixture import prepared_review_fixture


@pytest.mark.parametrize('with_alpha', [False, True])
def test_removal_review_fixture_has_own_intent_and_complete_authority(database, with_alpha):
    args, parent, scope, target = prepared_review_fixture(database, with_alpha=with_alpha)
    documents = {row['contract']: json.loads(row['body']) for row in scope['review_documents']}
    assert documents['EditIntent']['kind'] == 'remove_object'
    assert documents['EditIntent']['ev'] is None
    assert documents['TransferCandidate']['target'] == json.loads(args[5])['source']
    source = json.loads(scope['source_body'])
    assert (source['alpha'] is not None) == with_alpha
    assert database.execute('SELECT state FROM photo.semantic_targets WHERE id=%s', (target,)).fetchone()[0] == 'proposed'


@pytest.mark.parametrize('with_alpha', [False, True])
def test_removal_saved_preview_replays_without_dispatch_or_approval(database, with_alpha):
    from uuid import uuid5
    from removal_apply_documents_fixture import saved_preview_fixture, save_preview
    args, parent, scope, target, preview = saved_preview_fixture(database, with_alpha=with_alpha)
    assert preview == uuid5(parent, 'semantic-removal-preview:'+str(target)+':'+args[6]+':'+str(args[2])+':'+str(args[3]))
    assert save_preview(database, args) == preview
    assert database.execute('SELECT status,graph_sealed,cancel_requested FROM photo.runs WHERE id=%s',
        (preview,)).fetchone() == ('draft', False, False)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (preview,)).fetchone()[0] == 0
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (target,)).fetchone() == ('proposed', None, None)


@pytest.mark.parametrize('mutation', ['session', 'target_revision', 'cancel_parent'])
def test_removal_saved_preview_replay_requires_current_authority(database, mutation):
    import psycopg
    from removal_apply_documents_fixture import saved_preview_fixture, save_preview
    args, parent, scope, target, preview = saved_preview_fixture(database)
    before = database.execute('SELECT status,graph_sealed,config_snapshot,config_hash FROM photo.runs WHERE id=%s',
        (preview,)).fetchone()
    if mutation == 'session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (args[3],))
    elif mutation == 'target_revision':
        reject_target_with_revision(database, target)
    else:
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (parent,))
    with pytest.raises((psycopg.errors.InsufficientPrivilege, psycopg.errors.SerializationFailure)):
        save_preview(database, args)
    assert database.execute('SELECT status,graph_sealed,config_snapshot,config_hash FROM photo.runs WHERE id=%s',
        (preview,)).fetchone() == before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (preview,)).fetchone()[0] == 0
