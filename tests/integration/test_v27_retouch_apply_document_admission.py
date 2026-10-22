"""Real private admission predicate over own published retouch documents."""
from copy import deepcopy
import json
from uuid import uuid4

import pytest
from psycopg.types.json import Jsonb
from retouch_copy_apply_fixture import admission_task_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def inputs(db, *, with_alpha=False):
    values, task = admission_task_fixture(db, with_alpha=with_alpha)
    args = values[0][0]
    preview = json.loads(args[5])
    parent = db.execute('SELECT w.run_id FROM photo.semantic_intents i JOIN photo.v2_workflows w '
        'ON w.tenant_id=i.tenant_id AND w.id=i.workflow_id WHERE i.tenant_id=%s AND i.id=%s',
        (args[0], args[1])).fetchone()[0]
    fixed = (*args[:4], values[4], args[4], parent,
        int(preview['workflow_revision']), int(preview['target_revision']), task['params_body'], task['params_sha'])
    return values, task, fixed


def ready(db, task, fixed):
    with db.transaction():
        # Use its actual admission role; API/worker must not call it directly.
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        row = db.execute('SELECT photo.semantic_shown_retouch_admission_documents_ready('
            + ','.join(['%s']*15) + ')',
            (*fixed, Jsonb(task['documents']), Jsonb(task['node']), Jsonb(task['transport']), Jsonb(task['inventory']))).fetchone()
        db.execute('RESET ROLE')
        return row[0]


def test_retouch_document_predicate_acl_and_reserved_draft_connection(database):
    signature = 'photo.semantic_shown_retouch_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb)'
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_scheduler', 'photo_portal', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
            (role, signature)).fetchone()[0] is (role == 'photo_semantic_admission')
    definitions = database.execute('SELECT pg_get_functiondef(p.oid) FROM pg_proc p '
        'JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=%s '
        'AND p.proname=%s', ('photo', 'create_semantic_shown_retouch_apply_draft_shell')).fetchall()
    assert len(definitions) == 1
    assert 'photo.semantic_shown_retouch_admission_documents_ready(' in definitions[0][0]


def state(db, tenant):
    return tuple(db.execute('SELECT to_jsonb(snapshot_row) FROM photo.'+table+
        ' AS snapshot_row WHERE tenant_id=%s ORDER BY to_jsonb(snapshot_row)::text', (tenant,)).fetchall()
        for table in ('tasks', 'runs', 'backend_documents', 'quota_reservations', 'tenant_usage', 'semantic_targets'))


@pytest.mark.parametrize('with_alpha', [False, True])
def test_own_published_retouch_documents_admit_without_activation(database, with_alpha):
    values, task, fixed = inputs(database, with_alpha=with_alpha)
    before = state(database, fixed[0])
    assert ready(database, task, fixed) is True
    assert ready(database, task, fixed) is True
    assert state(database, fixed[0]) == before
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s', (values[4],)).fetchone() == (0,)


@pytest.mark.parametrize('document', ['approved', 'presentation', 'numerical', 'image', 'candidate',
    'intent', 'source', 'reference', 'mask', 'protection'])
def test_retouch_document_admission_binds_raw_bytes(database, document):
    _, task, fixed = inputs(database, with_alpha=True)
    assert ready(database, task, fixed) is True
    changed = deepcopy(task)
    changed['documents'][document] += ' '
    before = state(database, fixed[0])
    assert ready(database, changed, fixed) is False
    assert state(database, fixed[0]) == before


@pytest.mark.parametrize('mutation', ['extra_document', 'missing_document', 'missing_input', 'duplicate_input',
    'input_order', 'manifest_sha', 'input_digest', 'qualification_pin', 'cuda', 'vram'])
def test_retouch_document_admission_refuses_changed_closure_and_profile(database, mutation):
    _, task, fixed = inputs(database, with_alpha=True)
    assert ready(database, task, fixed) is True
    changed = deepcopy(task)
    if mutation == 'extra_document': changed['documents']['unexpected'] = '{}'
    elif mutation == 'missing_document': del changed['documents']['approved']
    elif mutation == 'missing_input': changed['inventory']['entries'].pop()
    elif mutation == 'duplicate_input': changed['inventory']['entries'].append(changed['inventory']['entries'][0])
    elif mutation == 'input_order': changed['inventory']['entries'].reverse()
    elif mutation == 'manifest_sha': changed['transport']['manifest']['sha256'] = 'e'*64
    elif mutation == 'input_digest': changed['node']['input_digest'] = 'e'*64
    elif mutation == 'qualification_pin': changed['node']['execution_profile']['qualification']['policy_id'] = str(uuid4())
    elif mutation == 'cuda': changed['node']['execution_profile']['backend'] = 'cuda'
    else: changed['node']['execution_profile']['resource_estimate']['vram_bytes'] = 1
    before = state(database, fixed[0])
    assert ready(database, changed, fixed) is False
    assert state(database, fixed[0]) == before


@pytest.mark.parametrize('mutation', ['params_unverified', 'manifest_expired', 'policy_disabled'])
def test_retouch_document_admission_rechecks_current_metadata(database, mutation):
    _, task, fixed = inputs(database, with_alpha=True)
    assert ready(database, task, fixed) is True
    with database.transaction(force_rollback=True):
        if mutation == 'params_unverified':
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                (fixed[0], task['params_artifact_id']))
        elif mutation == 'manifest_expired':
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
                'WHERE tenant_id=%s AND id=%s', (fixed[0], task['transport']['manifest']['artifact_id']))
        else:
            database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',
                (task['node']['execution_profile']['qualification']['policy_id'],))
        before = state(database, fixed[0])
        assert ready(database, task, fixed) is False
        assert state(database, fixed[0]) == before
    assert ready(database, task, fixed) is True
