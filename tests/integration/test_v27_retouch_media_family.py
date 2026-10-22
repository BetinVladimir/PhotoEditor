"""Own media scheduler lifecycle with synthetic terminal output metadata.

Exercises relational publication, not image encoding, object-store integrity or
worker qualification. Artifacts are first inserted with their final metadata.
"""
from hashlib import sha256
from uuid import uuid4

import pytest
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from test_v27_retouch_preview_sql_roles import database, isolated_database_config
from test_v27_retouch_media_sources import accepted_media_fixture, begin_media_fixture, scheduler


def terminal_media_family(db, *, with_alpha, oversized_last_image=False):
    accepted = accepted_media_fixture(db, with_alpha=with_alpha)
    values, task, commit, version, event, inputs = accepted
    tenant = commit[0]
    token, run = begin_media_fixture(db, accepted)
    nodes = []
    for variant in sorted(('thumb320', 'thumb640', 'preview1600', 'preview2560')):
        node_id = uuid4()
        operation = 'media.thumbnail' if variant.startswith('thumb') else 'media.preview'
        params = canonical_bytes(dict(image=inputs[0], variant=variant,
            profile_hash='9'*64, source_kind='working'))
        digest = sha256(params).hexdigest()
        node = dict(id=str(node_id), asset_version_id=str(version), variant=variant,
            operation=operation, profile_hash='9'*64, inputs=inputs, params_sha256=digest)
        assert scheduler(db, 'SELECT photo.register_media_retry_node(%s,%s,%s,%s,%s)',
            (event, token, run, Jsonb(node), event)) == (True,)
        reservation = scheduler(db, 'SELECT outcome,artifact_id,token,ready '
            'FROM photo.reserve_media_retry_document(%s,%s,%s,%s,%s,%s)',
            (tenant, run, node_id, digest, len(params), 'fixture'))
        assert reservation[0] == 'created' and reservation[3] is False
        assert scheduler(db, 'SELECT photo.complete_media_retry_document(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (tenant, run, node_id, reservation[1], reservation[2], digest, len(params),
             'fixture', 'metadata-only-v1')) == (True,)
        command = dict(id=str(node_id), node_key=f'media.retry/{event}/{variant}',
            operation=operation, operation_version='1', resource_class='media',
            timeout_seconds=180 if operation == 'media.thumbnail' else 300,
            params=dict(artifact_id=str(reservation[1]), sha256=digest), inputs=inputs,
            input_digest='f'*64, execution_profile=dict(backend='cpu', precision='float32',
                resource_estimate=dict(ram_bytes=1, vram_bytes=0, scratch_bytes=1,
                    output_bytes=1, pixel_count=1)))
        assert scheduler(db, 'SELECT photo.materialize_media_retry_task(%s,%s,%s,%s)',
            (event, token, run, Jsonb(command))) == (True,)
        nodes.append((node_id, variant))
    assert scheduler(db, 'SELECT photo.seal_media_retry_plan(%s,%s,%s)', (event, token, run)) == (True,)
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=%s AND id=%s',
        (tenant, run)).fetchone() == ('running', True)
    for node_id, variant in nodes:
        outputs = []
        # Width is immutable: construct the adversarial output before INSERT.
        width = 4097 if oversized_last_image and node_id == nodes[-1][0] else 1
        for role, media_type, width, height in (
            ('derivative', 'image/webp' if with_alpha else 'image/jpeg', width, 1),
            ('derivative_manifest', 'application/json', None, None)):
            artifact = uuid4()
            digest = sha256(f'metadata-only/{artifact}'.encode()).hexdigest()
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,"
                'media_type,state,verified_at,width,height,created_by_task_id,produced_attempt,produced_fence,output_role) '
                "VALUES(%s,%s,'fixture',%s,'metadata-only-v1',%s,100,%s,'ready',clock_timestamp(),%s,%s,%s,1,1,%s)",
                (artifact, tenant, f'metadata-only/{artifact}', digest, media_type, width, height, node_id, role))
            outputs.append(dict(artifact_id=str(artifact), sha256=digest))
        result = db.execute('INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) '
            "VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",
            (tenant, node_id, Jsonb(outputs), sha256(canonical_bytes(outputs)).hexdigest())).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s "
            'WHERE tenant_id=%s AND id=%s', (result, tenant, node_id))
    other = db.execute("SELECT event_id FROM photo.media_retry_jobs WHERE status='running' AND event_id<>%s", (event,)).fetchall()
    if other:
        pytest.skip('isolated finalization queue required')
    return accepted, run, nodes


@pytest.mark.parametrize('with_alpha', [False, True])
def test_retouch_media_family_publishes_all_variants_once(database, with_alpha):
    accepted, run, nodes = terminal_media_family(database, with_alpha=with_alpha)
    _, _, commit, version, event, _ = accepted
    tenant = commit[0]
    assert scheduler(database, 'SELECT photo.finalize_media_retry_job()') == (True,)
    assert database.execute('SELECT status,error_code FROM photo.media_retry_jobs WHERE event_id=%s',
        (event,)).fetchone() == ('completed', None)
    expected = sorted((variant, node_id, 'ready') for node_id, variant in nodes)
    actual = database.execute('SELECT variant,task_id,status FROM photo.derivatives '
        'WHERE tenant_id=%s AND asset_version_id=%s ORDER BY variant', (tenant, version)).fetchall()
    assert actual == expected
    revision = database.execute('SELECT revision FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, run)).fetchone()
    assert scheduler(database, 'SELECT photo.finalize_media_retry_job()') == (False,)
    assert database.execute('SELECT variant,task_id,status FROM photo.derivatives '
        'WHERE tenant_id=%s AND asset_version_id=%s ORDER BY variant', (tenant, version)).fetchall() == actual
    assert database.execute('SELECT revision FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, run)).fetchone() == revision


@pytest.mark.parametrize('mutation', ['expired_last_image', 'unverified_last_manifest',
    'oversized_last_image', 'cancelled_media_run', 'unverified_source_rgb', 'cancelled_source'])
def test_retouch_media_family_refuses_partial_publication(database, mutation):
    accepted, run, nodes = terminal_media_family(database, with_alpha=True,
        oversized_last_image=mutation == 'oversized_last_image')
    values, _, commit, version, event, inputs = accepted
    tenant = commit[0]
    last = nodes[-1][0]  # finalizer orders by variant, so earlier inserts must roll back
    if mutation == 'expired_last_image':
        database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
            "WHERE tenant_id=%s AND created_by_task_id=%s AND output_role='derivative'", (tenant, last))
    elif mutation == 'unverified_last_manifest':
        database.execute("UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND created_by_task_id=%s "
            "AND output_role='derivative_manifest'", (tenant, last))
    elif mutation == 'oversized_last_image':
        pass  # Immutable oversized metadata was inserted by the fixture.
    elif mutation == 'cancelled_media_run':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, run))
    elif mutation == 'unverified_source_rgb':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
            (tenant, inputs[1]['artifact_id']))
    else:
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, values[4]))
    assert scheduler(database, 'SELECT photo.finalize_media_retry_job()') == (True,)
    assert database.execute('SELECT status,error_code FROM photo.media_retry_jobs WHERE event_id=%s',
        (event,)).fetchone() == ('failed', 'DERIVATIVE_FAILED')
    assert database.execute('SELECT count(*) FROM photo.derivatives WHERE tenant_id=%s AND asset_version_id=%s',
        (tenant, version)).fetchone() == (0,)
