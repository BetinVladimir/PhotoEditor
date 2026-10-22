"""Own exposure batch/transport SQL binding; not transactional admission evidence.

Uses conftest's isolated migrated DB when eventually executed. No canonical
producer, physical object or qualification authority is fabricated by this gate.
"""
from copy import deepcopy
from hashlib import sha256
from uuid import uuid4, uuid5

import pytest
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes


def shown_exposure_binding_task(child, target):
    task = uuid5(child, 'semantic.adopt_shown_exposure:' + str(target))
    params_id = uuid5(task, 'Params')
    ref = lambda: dict(artifact_id=str(uuid4()), sha256='a' * 64)
    params = dict(child_run_id=str(child), target_id=str(target), approved_edit=ref(),
                  presentation=ref(), numerical_evidence=ref(), working_image=ref(), pixels=ref())
    body = canonical_bytes(params).decode()
    digest = sha256(body.encode()).hexdigest()
    entries = [dict(contract='Params', ref=dict(artifact_id=str(params_id), sha256=digest))]
    for key, contract in (('approved_edit', 'ApprovedSemanticEdit'),
                          ('presentation', 'ExposurePreviewPresentation'),
                          ('numerical_evidence', 'ExposurePreviewNumericalEvidence'),
                          ('working_image', 'WorkingImage'), ('pixels', 'NpyArray')):
        entries.append(dict(contract=contract, ref=params[key]))
    entries.sort(key=lambda entry: entry['ref']['artifact_id'])
    entries_sha = sha256(canonical_bytes(entries)).hexdigest()
    input_digest = sha256(canonical_bytes(dict(entries=entries,
        operation='semantic.adopt_shown_exposure', task=str(task), version='1'))).hexdigest()
    chunk = dict(artifact_id=str(uuid5(task, 'v2_input_chunk_0')),
                 sha256=sha256(canonical_bytes(dict(entries=entries, schema_version=1))).hexdigest())
    manifest_body = dict(chunks=[chunk], entry_count=len(entries), entries_sha=entries_sha,
                         schema_version=1, task_id=str(task))
    manifest = dict(artifact_id=str(uuid5(task, 'v2_input_manifest')),
                    sha256=sha256(canonical_bytes(manifest_body)).hexdigest())
    node = dict(id=str(task), node_key='semantic.adopt_shown_exposure:' + str(target),
                operation='semantic.adopt_shown_exposure', operation_version='1', resource_class='cpu',
                params_artifact_id=str(params_id), params_sha=digest, input_digest=input_digest,
                execution_profile={})  # Pure binding never qualifies execution.
    transport = dict(task_id=str(task), entry_count=len(entries), entries_sha=entries_sha,
                     manifest=manifest, chunks=[chunk], direct_roots=[entry['ref'] for entry in entries])
    row = dict(target_id=str(target), params_artifact_id=str(params_id), params_sha=digest,
               params_body=body, node=node, transport=transport,
               inventory=dict(task_id=str(task), entries=entries), documents={})
    choice = dict(target_id=str(target), decision='approve',
                  preview=dict(render=params['working_image'], evidence=params['presentation']))
    return choice, row


@pytest.mark.parametrize('mode', ['mixed', 'reverse', 'omit', 'two_approvals',
    'missing_approval', 'duplicate_task', 'task_for_rejected', 'task_for_omitted',
    'changed_render', 'changed_evidence', 'changed_child', 'legacy_operation',
    'changed_manifest', 'changed_inventory', 'duplicate_decision', 'reject_only'])
def test_shown_exposure_mixed_batch_binds_only_explicit_approvals(db, mode):
    child, approved, rejected, omitted = uuid4(), uuid4(), uuid4(), uuid4()
    choice, row = shown_exposure_binding_task(child, approved)
    decisions = [choice, dict(target_id=str(rejected), decision='reject')]
    tasks = [row]
    if mode == 'reverse': decisions.reverse()
    elif mode == 'omit': decisions = [choice]
    elif mode == 'two_approvals':
        other_choice, other_row = shown_exposure_binding_task(child, omitted)
        decisions.append(other_choice); tasks.append(other_row)
    elif mode == 'missing_approval': tasks = []
    elif mode == 'duplicate_task': tasks.append(deepcopy(row))
    elif mode == 'task_for_rejected': tasks = [shown_exposure_binding_task(child, rejected)[1]]
    elif mode == 'task_for_omitted': tasks = [shown_exposure_binding_task(child, omitted)[1]]
    elif mode == 'changed_render': choice['preview'] = {**choice['preview'], 'render': dict(artifact_id=str(uuid4()), sha256='a'*64)}
    elif mode == 'changed_evidence': choice['preview'] = {**choice['preview'], 'evidence': dict(artifact_id=str(uuid4()), sha256='a'*64)}
    elif mode == 'changed_child': child = uuid4()
    elif mode == 'legacy_operation': row['node']['operation'] = 'semantic.local_exposure'
    elif mode == 'changed_manifest': row['transport']['manifest']['sha256'] = 'f'*64
    elif mode == 'changed_inventory': row['inventory']['entries'][0]['ref']['sha256'] = 'f'*64
    elif mode == 'duplicate_decision': decisions.append(deepcopy(choice))
    elif mode == 'reject_only': decisions = [dict(target_id=str(rejected), decision='reject')]; tasks = []
    result = db.execute('SELECT photo.semantic_shown_batch_nodes_bound(%s,%s,%s)',
                        (child, Jsonb(decisions), Jsonb(tasks))).fetchone()[0]
    assert result is (mode in ('mixed', 'reverse', 'omit', 'two_approvals'))


def test_shown_exposure_binding_is_private(db):
    signature = 'photo.semantic_shown_batch_nodes_bound(uuid,jsonb,jsonb)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_gateway', 'photo_portal'):
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',
                          (role, signature)).fetchone()[0] is False
