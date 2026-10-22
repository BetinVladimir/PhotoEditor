"""Synthetic canonical-result SQL finalization scenarios; no physical GPU proof."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from generation_replay_fixture import generation_replay_fixtures


def node(db, tenant, run, source, kind, ordinal, state, role=None):
    task = uuid4()
    operation = 'image.generate' if kind == 'generation' else 'editor.preview'
    db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id,state) "
        "VALUES(%s,%s,%s,%s,%s,%s,%s,'[]','{}',%s,'cpu',%s,'complete')",
        (tenant, run, task, kind, ordinal, operation, source, 'c'*64, 'd'*64))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,timeout_seconds,attempt,fencing_token) "
        "VALUES(%s,%s,%s,%s,%s,'1',%s,%s,'[]',%s,%s,'{}',7200,1,1)",
        (task, tenant, run, f'{kind}/{ordinal:02}', operation, 'gpu' if kind == 'generation' else 'cpu', state, source, 'e'*64))
    if state == 'succeeded':
        artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,created_by_task_id,produced_attempt,produced_fence,output_role) "
            "VALUES(%s,'photo',%s,'fixture-v1',%s,100,%s,'ready',clock_timestamp(),%s,1,1,%s) RETURNING id",
            (tenant, f'finalization/{uuid4()}', 'f'*64, 'image/jpeg' if role == 'derivative' else 'application/json', task, role)).fetchone()[0]
        result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest,review_required) "
            "VALUES(%s,%s,1,1,%s,'{}',%s,%s) RETURNING id",
            (tenant, task, Jsonb([dict(artifact_id=str(artifact), sha256='f'*64)]), '1'*64, kind == 'generation')).fetchone()[0]
        db.execute("UPDATE photo.tasks SET canonical_result_id=%s WHERE id=%s", (result, task))


@pytest.mark.parametrize('mode,expected', [
    ('failed','failed'), ('cancelled','failed'), ('skipped','failed'),
    ('partial','completed_with_errors'), ('preview_failed','failed'),
    ('preview_missing','running'), ('success','waiting_review'),
])
def test_generation_terminal_variants_do_not_wait_for_impossible_previews(db, mode, expected):
    f = generation_replay_fixtures(db)[0]
    tenant, run = f['Tenant'], f['Expected']['run_id']
    source = db.execute("SELECT master_artifact_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s",
        (tenant, f['Request']['asset_version_id'])).fetchone()[0]
    db.execute("UPDATE photo.runs SET status='running' WHERE tenant_id=%s AND id=%s", (tenant, run))
    if mode in ('failed','cancelled','skipped'):
        node(db, tenant, run, source, 'generation', 0, mode)
    else:
        node(db, tenant, run, source, 'generation', 0, 'succeeded', 'working_image')
        if mode != 'preview_missing':
            node(db, tenant, run, source, 'generation_preview', 0,
                'failed' if mode == 'preview_failed' else 'succeeded', 'derivative')
        if mode == 'partial':
            node(db, tenant, run, source, 'generation', 1, 'failed')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        changed = db.execute('SELECT photo.finalize_interactive_run()').fetchone()[0]
        db.execute('RESET ROLE')
    assert changed is (expected != 'running')
    assert db.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, run)).fetchone()[0] == expected
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.finalize_interactive_run()').fetchone()[0] is False
        db.execute('RESET ROLE')
