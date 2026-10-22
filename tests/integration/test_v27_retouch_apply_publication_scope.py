"""Real review authority cannot substitute for a selected retouch receipt.

No authority function replacement; all rows are rolled back. Positive publication
requires a full own retouch preview producer fixture and is tracked separately.
"""
from semantic_target_state_fixture import reject_target_with_revision
import hashlib
import json
from pathlib import Path
from uuid import uuid4, uuid5

import psycopg
import pytest

from test_v27_retouch_preview_sql_roles import database, isolated_database_config
from test_v27_background_cleanup_preview_saved_authority import saved_authority_fixture


@pytest.mark.parametrize('mutation', ['missing_retouch_receipt', 'revoked_session', 'changed_target', 'canceled_parent'])
def test_live_review_without_exact_retouch_receipt_cannot_publish_copy(database, mutation):
    args, parent, scope, target = saved_authority_fixture(database)
    params = json.loads(args[5])
    child = uuid4()
    task = uuid5(child, 'semantic.adopt_shown_retouch_region:'+str(target))
    body = json.loads((Path(__file__).resolve().parents[2]/'backend/internal/semanticreview/testdata/retouch_preview_adoption_params.json').read_bytes())
    body.update(tenant_id=str(args[0]), target_id=str(target), child_run_id=str(child))
    raw = json.dumps(body, sort_keys=True, separators=(',', ':'))
    digest = hashlib.sha256(raw.encode()).hexdigest()
    if mutation == 'revoked_session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
    elif mutation == 'changed_target':
        reject_target_with_revision(database, target)
    elif mutation == 'canceled_parent':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (parent,))
    before = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone()
    with pytest.raises((psycopg.errors.SerializationFailure, psycopg.errors.InsufficientPrivilege, psycopg.errors.CheckViolation)):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute('SELECT * FROM photo.reserve_semantic_shown_retouch_transport_document('+','.join(['%s']*18)+')',
                (args[0],args[1],args[2],args[3],int(params['workflow_revision']),target,int(params['target_revision']),
                 params['plan']['sha256'],params['candidate']['sha256'],raw,digest,child,'task_params',task,uuid5(task,'Params'),digest,len(raw.encode()),'fixture')).fetchall()
    assert database.execute('SELECT count(*) FROM photo.backend_documents WHERE tenant_id=%s AND owner_id=%s', (args[0],task)).fetchone()[0] == 0
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s', (child,)).fetchone()[0] == 0
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone() == before
