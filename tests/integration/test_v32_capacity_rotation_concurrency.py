"""Real connection contention; isolated database fixture owns all committed data.

Authored acceptance coverage only until explicitly executed in the testing phase.
"""
import hashlib
import json
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from threading import Event
from time import monotonic
from uuid import uuid4

import psycopg
import pytest


ROOT = Path(__file__).resolve().parents[2]


def _install(conn, lease):
    policy = json.loads((ROOT / 'configs/capacity-metal-verification.json').read_text())
    policy['lease_seconds'] = lease
    # This fixture contains only ASCII keys and integral/ordinary decimal values.
    body = json.dumps(policy, sort_keys=True, separators=(',', ':'))
    pin = hashlib.sha256(body.encode()).hexdigest()
    conn.execute(
        'INSERT INTO photo.capacity_policy_timings '
        '(policy_sha,policy_jcs,offline_seconds,lease_seconds) VALUES(%s,%s,%s,%s)',
        (pin, body, policy['offline_seconds'], lease),
    )
    return pin


@pytest.mark.parametrize("competitor", ["rotation", "old_heartbeat"])
@pytest.mark.parametrize("commit_rotation", [True, False])
def test_rotation_serializes_and_rechecks_waiting_writer(database, competitor, commit_rotation):
    worker, node = uuid4(), 'rotation-' + uuid4().hex
    with psycopg.connect(**database) as setup:
        user = setup.execute(
            "SELECT user_id FROM photo.auth_bootstrap_user(%s,'en',true)",
            (uuid4().hex + '@capacity.test',),
        ).fetchone()[0]
        tenant = setup.execute(
            'SELECT id FROM photo.tenants WHERE owner_user_id=%s', (user,),
        ).fetchone()[0]
        principal = setup.execute('SELECT session_user').fetchone()[0]
        old, first, second = [_install(setup, lease) for lease in (60, 61, 62)]
        setup.execute(
            'INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,'
            'vram_budget_bytes,scratch_budget_bytes,doctor_report_sha256) '
            'VALUES(%s,true,1000,0,2000,%s)', (node, 'b' * 64),
        )
        setup.execute(
            'INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,'
            'resource_classes,backends,lock_id,last_seen,capacity,enabled,db_principal,'
            'qualified_operations,certificate_sha256,capacity_policy_sha,'
            'reported_capacity_policy_sha,capacity_policy_reported_at) '
            "VALUES(%s,%s,%s,'[\"archive\"]','[\"cpu\"]','',clock_timestamp(),'{}',"
            "true,%s,'{\"archive.build\":[\"cpu\"]}',%s,%s,%s,clock_timestamp())",
            (tenant, worker, node, principal, 'c' * 64, old, old),
        )
        setup.execute('SET LOCAL ROLE photo_worker')
        assert setup.execute(
            "SELECT photo.heartbeat_worker_capacity_v2(%s,'archive','cpu',"
            "ARRAY['archive.build'],900,0,1800,%s)", (worker, old),
        ).fetchone() == (True,)
        setup.execute('RESET ROLE')
        node_before = setup.execute(
            'SELECT ram_available_bytes,vram_available_bytes,scratch_available_bytes,'
            'reported_at,capacity_report_worker_id,capacity_report_policy_sha '
            'FROM photo.resource_nodes WHERE node_id=%s', (node,),
        ).fetchone()
    query = 'SELECT photo.rotate_worker_capacity_policy(%s,%s::name,%s,%s,%s)'
    started, release = Event(), Event()
    with psycopg.connect(**database) as winner, psycopg.connect(**database) as loser:
        for conn in (winner, loser):
            conn.execute("SET statement_timeout='15s'")
            conn.execute("SET lock_timeout='12s'")
            conn.commit()
        winner_pid = winner.info.backend_pid
        loser_pid = loser.info.backend_pid
        assert winner.execute(query, (worker, principal, node, old, first)).fetchone() == (1,)

        def competing_writer():
            started.set()
            try:
                if competitor == 'old_heartbeat':
                    loser.execute('SET LOCAL ROLE photo_worker')
                    result = loser.execute(
                        "SELECT photo.heartbeat_worker_capacity_v2(%s,'archive','cpu',"
                        "ARRAY['archive.build'],1,0,1,%s)", (worker, old),
                    ).fetchone()[0]
                    loser.commit()
                    return result
                loser.execute(query, (worker, principal, node, old, second))
                loser.commit()
                return 'rotation_committed'
            except psycopg.Error as exc:
                loser.rollback()
                return exc.sqlstate

        with ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(competing_writer)
            try:
                assert started.wait(3), 'competing connection did not start'
                with psycopg.connect(**database, autocommit=True) as observer:
                    observer.execute("SET statement_timeout='2s'")
                    deadline = monotonic() + 5
                    while True:
                        blocked = observer.execute(
                            'SELECT %s = ANY(pg_blocking_pids(%s))',
                            (winner_pid, loser_pid),
                        ).fetchone()[0]
                        if blocked:
                            break
                        assert not future.done(), 'rotation failed before reaching node lock'
                        assert monotonic() < deadline, 'actual lock contention not observed'
                        release.wait(0.01)
                if commit_rotation:
                    winner.commit()
                else:
                    winner.rollback()
                result = future.result(timeout=10)
                if competitor == 'old_heartbeat':
                    assert result is (not commit_rotation)
                else:
                    assert result == ('40001' if commit_rotation else 'rotation_committed')
            finally:
                # Release locks even if observation/assertion fails, before pool shutdown.
                winner.rollback()
    with psycopg.connect(**database) as check:
        rows = check.execute(
            'SELECT capacity_policy_sha,reported_capacity_policy_sha,'
            'capacity_policy_reported_at FROM photo.worker_registrations '
            'WHERE tenant_id=%s AND worker_id=%s', (tenant, worker),
        ).fetchall()
        final_pin = first if commit_rotation else (second if competitor == 'rotation' else old)
        if not commit_rotation and competitor == 'old_heartbeat':
            assert len(rows) == 1
            assert rows[0][:2] == (old, old)
            assert rows[0][2] is not None
        else:
            assert rows == [(final_pin, None, None)]
        node_after = check.execute(
            'SELECT ram_available_bytes,vram_available_bytes,scratch_available_bytes,'
            'reported_at,capacity_report_worker_id,capacity_report_policy_sha '
            'FROM photo.resource_nodes WHERE node_id=%s', (node,),
        ).fetchone()
        heartbeat_accepted = not commit_rotation and competitor == 'old_heartbeat'
        if heartbeat_accepted:
            assert node_after[:3] == (1, 0, 1)
            assert node_after[3] >= node_before[3]
            assert node_after[4:] == (worker, old)
        else:
            assert node_after == node_before, 'rejected writer changed node capacity'
        assert check.execute(
            'SELECT photo.capacity_policy_node_fresh(n) FROM photo.resource_nodes n '
            'WHERE node_id=%s', (node,),
        ).fetchone() == (heartbeat_accepted,)
        check.execute('SET LOCAL ROLE photo_worker')
        assert check.execute(
            "SELECT photo.heartbeat_worker_capacity_v2(%s,'archive','cpu',"
            "ARRAY['archive.build'],800,0,1600,%s)", (worker, final_pin),
        ).fetchone() == (True,)
        check.execute('RESET ROLE')
        assert check.execute(
            'SELECT photo.capacity_policy_node_fresh(n),capacity_report_policy_sha,'
            'ram_available_bytes,scratch_available_bytes FROM photo.resource_nodes n '
            'WHERE node_id=%s', (node,),
        ).fetchone() == (True, final_pin, 800, 1600)
