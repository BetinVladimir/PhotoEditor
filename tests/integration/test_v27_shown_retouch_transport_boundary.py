"""Pure real-SQL transport boundary scenarios; no lease/approval authority claimed."""
import copy
import os
from hashlib import sha256
from uuid import uuid4, uuid5

import psycopg
import pytest
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes


def transport_fixture(count):
    child, target = uuid4(), uuid4()
    key = 'semantic.adopt_shown_retouch_region:' + str(target)
    task = uuid5(child, key)
    params_id = uuid5(task, 'Params')
    params = dict(child_run_id=str(child), target_id=str(target))
    entries = [dict(contract='Params', ref=dict(artifact_id=str(params_id), sha256='a'*64))]
    for field, contract in [('approved_edit', 'ApprovedSemanticEdit'),
                            ('presentation', 'RetouchPreviewPresentation'),
                            ('numerical_evidence', 'RetouchPreviewNumericalEvidence'),
                            ('working_image', 'WorkingImage'), ('pixels', 'NpyArray')]:
        ref = dict(artifact_id=str(uuid4()), sha256='b'*64)
        params[field] = ref
        entries.append(dict(contract=contract, ref=ref))
    while len(entries) < count:
        entries.append(dict(contract='NpyArray', ref=dict(artifact_id=str(uuid4()), sha256='c'*64)))
    entries.sort(key=lambda entry: entry['ref']['artifact_id'])
    entries_sha = sha256(canonical_bytes(entries)).hexdigest()
    chunk_body = dict(schema_version=1, entries=entries)
    chunk = dict(artifact_id=str(uuid5(task, 'v2_input_chunk_0')),
                 sha256=sha256(canonical_bytes(chunk_body)).hexdigest())
    manifest_body = dict(schema_version=1, task_id=str(task), entry_count=count,
                         entries_sha=entries_sha, chunks=[chunk])
    manifest = dict(artifact_id=str(uuid5(task, 'v2_input_manifest')),
                    sha256=sha256(canonical_bytes(manifest_body)).hexdigest())
    node = dict(id=str(task), node_key=key, operation='semantic.adopt_shown_retouch_region',
                operation_version='1', resource_class='cpu', params_artifact_id=str(params_id),
                params_sha='a'*64, execution_profile={}, input_digest=sha256(canonical_bytes(
                    dict(entries=entries, operation='semantic.adopt_shown_retouch_region',
                         task=str(task), version='1'))).hexdigest())
    roots = [entry['ref'] for entry in entries if count < 512 or entry['contract'] != 'Params']
    transport = dict(task_id=str(task), entry_count=count, entries_sha=entries_sha,
                     manifest=manifest, chunks=[chunk], direct_roots=roots)
    return child, params, node, transport, dict(task_id=str(task), entries=entries)


@pytest.mark.parametrize('count', [128, 511, 512])
@pytest.mark.parametrize('mutation', ['none', 'missing_root', 'duplicate_root', 'wrong_sha', 'wrong_count', 'old_roots'])
def test_exact_transport_boundary_against_real_sql(count, mutation):
    dsn = os.environ.get('PHOTO_TEST_DATABASE_URL')
    if not dsn:
        pytest.skip('explicit PHOTO_TEST_DATABASE_URL required')
    child, params, node, transport, inventory = transport_fixture(count)
    transport = copy.deepcopy(transport)
    if mutation == 'missing_root':
        transport['direct_roots'].pop()
    elif mutation == 'duplicate_root':
        transport['direct_roots'].append(transport['direct_roots'][0])
    elif mutation == 'wrong_sha':
        transport['direct_roots'][0]['sha256'] = 'f'*64
    elif mutation == 'wrong_count':
        transport['entry_count'] += 1
    elif mutation == 'old_roots':
        transport['direct_roots'] = [entry['ref'] for entry in inventory['entries']]
    with psycopg.connect(dsn) as db:
        try:
            db.execute('SET LOCAL ROLE photo_fn_owner')
            actual = db.execute('SELECT photo.semantic_shown_retouch_transport_ready(%s,%s,%s,%s,%s)',
                                (child, Jsonb(params), Jsonb(node), Jsonb(transport), Jsonb(inventory))).fetchone()[0]
            assert actual is (mutation == 'none' or (mutation == 'old_roots' and count < 512))
        finally:
            db.rollback()
