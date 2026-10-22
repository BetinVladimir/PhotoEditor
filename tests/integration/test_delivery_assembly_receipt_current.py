"""Exact canonical assembly receipt authority; authored, not executed.

Persist privileged receipt state, independently of assembly execution. Positive
control holds artifact producer pins current while negative cases change only
result pins or consumer acknowledgement, so stale-result rejection is isolated.
"""
from uuid import uuid4

import pytest
from psycopg.types.json import Jsonb

from test_delivery_manifest_download_lifecycle import published_manifest, persisted_document_output


def assembly_receipt(db, result_attempt=2, result_fence=3, missing_consumer=None):
    x = published_manifest(db)
    workflow = db.execute(
        'SELECT id FROM photo.v2_workflows WHERE tenant_id=%s AND run_id=%s',
        (x['tenant'], x['run']),
    ).fetchone()[0]
    task = db.execute("SELECT photo.v30_uuid5(%s,'v2.delivery.assemble:0')", (workflow,)).fetchone()[0]
    params = db.execute(
        "SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND operation='v2.delivery.prepare'",
        (x['tenant'], x['run']),
    ).fetchone()[0]
    result = uuid4()
    db.execute(
        "INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,canonical_result_id) VALUES(%s,%s,%s,'assemble','v2.delivery.assemble','1','cpu','succeeded','[]',%s,%s,'{}',%s,%s,%s)",
        (task, x['tenant'], x['run'], params, 'e'*64, result_attempt, result_fence, result),
    )
    persisted_document_output(db,x['tenant'],task,result_attempt,result_fence,x['manifest'])
    db.execute(
        "INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,%s,%s,%s,'{}',%s)",
        (result, x['tenant'], task, result_attempt, result_fence,
         Jsonb([{'artifact_id': str(x['manifest']), 'sha256': 'd'*64}]), 'f'*64),
    )
    # Retire the valid old result while keeping the current artifact/task pins
    # together; never bypass the result-root guard to insert invalid metadata.
    db.execute('UPDATE photo.tasks SET attempt=2,fencing_token=3 WHERE id=%s',(task,))
    db.execute('UPDATE photo.artifacts SET produced_attempt=2,produced_fence=3 WHERE id=%s',(x['manifest'],))
    for consumer in ('v2-adopt', 'v30-delivery-assembled'):
        if consumer != missing_consumer:
            db.execute(
                'INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,%s,photo.v2_adoption_event_id(%s,%s),clock_timestamp())',
                (x['tenant'], consumer, task, result),
            )
    return x['tenant'], task


def committed(db, tenant, task):
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        return db.execute('SELECT photo.v30_delivery_assembly_adoption_committed(%s,%s)', (tenant, task)).fetchone()[0]
    finally:
        db.execute('RESET ROLE')


def test_current_assembly_receipt_is_acknowledged(db):
    assert committed(db, *assembly_receipt(db)) is True


@pytest.mark.parametrize('attempt,fence', [(1, 3), (2, 2), (1, 2)])
def test_stale_result_rejected_even_when_manifest_producer_is_current(db, attempt, fence):
    assert committed(db, *assembly_receipt(db, attempt, fence)) is False


@pytest.mark.parametrize('consumer', ['v2-adopt', 'v30-delivery-assembled'])
def test_both_adoption_acknowledgements_are_required(db, consumer):
    assert committed(db, *assembly_receipt(db, missing_consumer=consumer)) is False


def test_receipt_is_tenant_scoped(db):
    tenant, task = assembly_receipt(db)
    assert committed(db, tenant, task) is True
    assert committed(db, uuid4(), task) is False
