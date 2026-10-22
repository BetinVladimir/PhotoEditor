"""Own retouch -> accepted version -> media source authority; no rendered QA.

Uses explicit synthetic worker-result metadata from the adoption fixture, then
real adoption/finalization/materialization and scheduler functions. Requires an
isolated database queue; never consumes another fixture's queued media work.
"""
import copy
import json
import re
from uuid import uuid4

import pytest
from psycopg.types.json import Jsonb
from test_v27_retouch_preview_sql_roles import database, isolated_database_config
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture, adopt
from test_v27_retouch_child_finalization import finalize
from test_v27_retouch_accepted_version import materialize


def scheduler(db, statement, args=()):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        result = db.execute(statement, args).fetchone()
        db.execute('RESET ROLE')
        return result


def accepted_media_fixture(db, *, with_alpha):
    values, task, commit = successful_copy_metadata_fixture(db, with_alpha=with_alpha)
    tenant = commit[0]
    assert adopt(db, commit) is True
    assert finalize(db, tenant, values[4]) == 'completed'
    version = materialize(db, values, task, commit[12])
    assert version is not None
    jobs = db.execute('SELECT event_id,origin,origin_run_id,status,variants FROM photo.media_retry_jobs '
        'WHERE tenant_id=%s AND asset_version_id=%s', (tenant, version)).fetchall()
    assert len(jobs) == 1
    event, origin, source, status, variants = jobs[0]
    assert (origin, source, status, variants) == ('apply', values[4], 'queued', ['thumbnail', 'preview'])
    assert materialize(db, values, task, commit[12]) == version
    assert db.execute('SELECT count(*) FROM photo.media_retry_jobs WHERE tenant_id=%s AND asset_version_id=%s',
        (tenant, version)).fetchone()[0] == 1
    image = json.loads(commit[12])
    inputs = [dict(artifact_id=str(commit[10]), sha256=commit[11]), image['pixels']]
    if image.get('alpha') is not None:
        inputs.append(image['alpha'])
    return values, task, commit, version, event, inputs


def source_live(db, event, inputs):
    # Private helper is inspected as its owner, not exposed to scheduler clients.
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result = db.execute('SELECT photo.semantic_shown_media_source_live(%s,%s)',
            (event, Jsonb(inputs))).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('function,arity,guard', [
    ('claim_task', 4, 'semantic_shown_media_source_live'),
    ('gateway_input', 6, 'semantic_shown_media_task_source_live'),
    ('heartbeat_task', 5, 'semantic_shown_media_task_source_live'),
    ('commit_task_success', 5, 'semantic_shown_media_task_source_live'),
    ('seal_media_retry_plan', 3, 'semantic_shown_media_source_live'),
    ('finalize_media_retry_job', 0, 'semantic_shown_media_task_source_live'),
])
def test_installed_media_lifecycle_keeps_source_guard(database, function, arity, guard):
    # Follow actual same-signature renamed delegates from the public entrypoint;
    # unrelated helper definitions cannot satisfy reachability by themselves.
    pending = [function]
    visited = set()
    guarded = False
    while pending:
        current = pending.pop()
        if current in visited:
            continue
        visited.add(current)
        assert len(visited) <= 128
        definitions = database.execute('SELECT pg_get_functiondef(p.oid) FROM pg_proc p '
            'JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=%s AND p.proname=%s '
            'AND p.pronargs=%s', ('photo', current, arity)).fetchall()
        assert len(definitions) == 1, current
        calls = set(re.findall(r'photo\.([a-z0-9_]+)\(', definitions[0][0]))
        guarded = guarded or guard in calls
        pending.extend(name for name in calls if name.startswith(function + '_') and name not in visited)
    assert guarded, (function, visited)


def begin_media_fixture(db, accepted):
    values, task, commit, version, event, inputs = accepted
    available = db.execute("SELECT event_id FROM photo.media_retry_jobs WHERE status='queued' "
        "OR (status='materializing' AND materialize_lease_until<=clock_timestamp())").fetchall()
    if available != [(event,)]:
        pytest.skip('isolated media queue required; other work must not be claimed')
    claimed = scheduler(db, 'SELECT event_id,materialize_token,tenant_id,asset_version_id '
        'FROM photo.claim_media_retry_job()')
    assert claimed is not None
    assert (claimed[0], claimed[2], claimed[3]) == (event, commit[0], version)
    token = claimed[1]
    run = uuid4()
    config = dict(event_id=str(event), asset_version_id=str(version),
        variants=['thumbnail', 'preview'], srgb_sha256='b'*64)
    assert scheduler(db, 'SELECT photo.begin_media_retry_plan(%s,%s,%s,%s,%s)',
        (event, token, run, 'c'*64, Jsonb(config))) == (True,)
    return token, run


@pytest.mark.parametrize('with_alpha', [False, True])
def test_retouch_media_enqueues_once_and_registers_exact_inputs(database, with_alpha):
    values, task, commit, version, event, inputs = accepted_media_fixture(database, with_alpha=with_alpha)
    assert source_live(database, event, inputs) is True
    token, run = begin_media_fixture(database, (values, task, commit, version, event, inputs))
    shown = json.loads(commit[13])
    for variant in ('thumb320', 'thumb640', 'preview1600', 'preview2560'):
        node = dict(id=str(uuid4()), asset_version_id=str(version), variant=variant,
            operation='media.thumbnail' if variant.startswith('thumb') else 'media.preview',
            profile_hash='9'*64, inputs=inputs, params_sha256='d'*64)
        wrong = [inputs[:1], list(reversed(inputs)), inputs + [inputs[0]]]
        original = copy.deepcopy(inputs)
        original[1] = shown['pixels']
        wrong.append(original)
        if with_alpha:
            wrong.append(inputs[:2])
        for replacement in wrong:
            refused = dict(node, inputs=replacement)
            assert scheduler(database, 'SELECT photo.register_media_retry_node(%s,%s,%s,%s,%s)',
                (event, token, run, Jsonb(refused), event)) == (False,)
            assert database.execute('SELECT count(*) FROM photo.media_retry_nodes WHERE id=%s',
                (node['id'],)).fetchone()[0] == 0
        for _ in range(2):
            assert scheduler(database, 'SELECT photo.register_media_retry_node(%s,%s,%s,%s,%s)',
                (event, token, run, Jsonb(node), event)) == (True,)
        assert database.execute('SELECT inputs FROM photo.media_retry_nodes WHERE id=%s',
            (node['id'],)).fetchone()[0] == inputs
        for ref in inputs:
            for mutation in ('unverified', 'expired'):
                with database.transaction(force_rollback=True):
                    if mutation == 'unverified':
                        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                            (commit[0], ref['artifact_id']))
                    else:
                        database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
                            'WHERE tenant_id=%s AND id=%s', (commit[0], ref['artifact_id']))
                    assert scheduler(database, 'SELECT photo.register_media_retry_node(%s,%s,%s,%s,%s)',
                        (event, token, run, Jsonb(node), event)) == (False,)
                    assert database.execute('SELECT inputs FROM photo.media_retry_nodes WHERE id=%s',
                        (node['id'],)).fetchone()[0] == inputs
        assert scheduler(database, 'SELECT photo.register_media_retry_node(%s,%s,%s,%s,%s)',
            (event, token, run, Jsonb(node), event)) == (True,)


@pytest.mark.parametrize('mutation', ['child_cancel', 'task_cancel', 'project_archive',
    'asset_delete', 'target_failed', 'lineage_removed', 'adoption_removed',
    'root_unverified', 'rgb_unverified', 'alpha_unverified',
    'root_expired', 'rgb_expired', 'alpha_expired'])
def test_retouch_media_source_loses_authority_after_lifecycle_change(database, mutation):
    values, task, commit, version, event, inputs = accepted_media_fixture(database, with_alpha=True)
    tenant = commit[0]
    assert source_live(database, event, inputs) is True
    with database.transaction(force_rollback=True):
        if mutation == 'child_cancel':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, values[4]))
        elif mutation == 'task_cancel':
            database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, commit[1]))
        elif mutation == 'project_archive':
            database.execute('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=%s AND id=%s',
                (tenant, values[0][2]['project']))
        elif mutation == 'asset_delete':
            database.execute('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE tenant_id=%s AND id='
                '(SELECT asset_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s)', (tenant, tenant, version))
        elif mutation == 'target_failed':
            # Applied is terminal: this forbidden write cannot withdraw a
            # healthy accepted media source. A legal pre-adoption failed child
            # is covered separately below, not relabelled as post-applied loss.
            from psycopg import errors
            from removal_transaction_state import tenant_transaction_state
            assert database.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',
                (tenant,task['target_id'])).fetchone() == ('applied',)
            before=tenant_transaction_state(database,tenant)
            with pytest.raises(errors.ObjectNotInPrerequisiteState) as rejected:
                with database.transaction():
                    database.execute("UPDATE photo.semantic_targets SET state='failed' WHERE tenant_id=%s AND id=%s",
                        (tenant,task['target_id']))
            assert rejected.value.sqlstate == '55000'
            assert rejected.value.diag.message_primary == 'SEMANTIC_TARGET_STATE_CONFLICT'
            assert tenant_transaction_state(database,tenant) == before
            assert source_live(database,event,inputs) is True
            return
        elif mutation == 'lineage_removed':
            database.execute('DELETE FROM photo.asset_version_sources WHERE tenant_id=%s AND version_id=%s', (tenant, version))
        elif mutation == 'adoption_removed':
            database.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id="
                'photo.v2_adoption_event_id(%s,%s)', (tenant, commit[1], commit[4]))
        else:
            index = {'root': 0, 'rgb': 1, 'alpha': 2}[mutation.split('_')[0]]
            artifact = inputs[index]['artifact_id']
            if mutation.endswith('_unverified'):
                database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                    (tenant, artifact))
            else:
                database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
                    'WHERE tenant_id=%s AND id=%s', (tenant, artifact))
        assert source_live(database, event, inputs) is False
    assert source_live(database, event, inputs) is True


@pytest.mark.parametrize('with_alpha',[False,True])
def test_retouch_failed_child_before_adoption_cannot_publish_media(database,with_alpha):
    """Legal queued->failed settlement; explicit child terminal metadata only.

    Canonical output metadata is the same fixture used by positive adoption.
    This tests failed child/target refusal, not physical worker failure and not
    a post-applied target transition that production deliberately prohibits.
    """
    from test_v27_retouch_adoption_commit import adoption_state
    values,task,commit=successful_copy_metadata_fixture(database,with_alpha=with_alpha)
    tenant=commit[0];child=values[4]
    target_before=database.execute('SELECT state,revision,child_run_id,batch_id,review_id,override_id '
        'FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',(tenant,task['target_id'])).fetchone()
    assert target_before[0] == 'queued'
    assert target_before[2] == child
    assert database.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' "
        'AND event_id=photo.v2_adoption_event_id(%s,%s)',(tenant,commit[1],commit[4])).fetchone() == (0,)
    # Model child termination, then use the real scheduler settlement entrypoint.
    database.execute("UPDATE photo.runs SET status='failed' WHERE tenant_id=%s AND id=%s",(tenant,child))
    assert scheduler(database,'SELECT photo.settle_semantic_child(%s,%s)',(tenant,child)) == (1,)
    assert database.execute('SELECT state,revision,child_run_id,batch_id,review_id,override_id '
        'FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',(tenant,task['target_id'])).fetchone() == (
            'failed',target_before[1]+1,*target_before[2:])
    before=adoption_state(database,tenant,child)
    media_before=database.execute('SELECT event_id,asset_version_id,status,origin,origin_run_id '
        'FROM photo.media_retry_jobs WHERE tenant_id=%s ORDER BY event_id',(tenant,)).fetchall()
    assert scheduler(database,'SELECT photo.settle_semantic_child(%s,%s)',(tenant,child)) == (0,)
    assert adopt(database,commit) is False
    assert materialize(database,values,task,commit[12]) is None
    assert adoption_state(database,tenant,child) == before
    assert database.execute('SELECT event_id,asset_version_id,status,origin,origin_run_id '
        'FROM photo.media_retry_jobs WHERE tenant_id=%s ORDER BY event_id',(tenant,)).fetchall() == media_before
    assert database.execute('SELECT count(*) FROM photo.media_retry_jobs WHERE tenant_id=%s AND origin_run_id=%s',
        (tenant,child)).fetchone() == (0,)
