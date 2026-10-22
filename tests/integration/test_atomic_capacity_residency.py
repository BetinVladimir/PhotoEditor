"""Real committed fixture and separate worker transaction; authored, not run."""
from pathlib import Path
import psycopg
import pytest
from photo_worker.models.pool import ResidentModel
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.capacity_policy import load_capacity_policy
from photo_worker.runtime.capacity_policy_installation import install_capacity_policy_timing
from test_task_leases import queued_task


@pytest.mark.parametrize('mode',['accepted','residency_error','residency_false'])
def test_capacity_and_residency_commit_or_rollback_together(database, monkeypatch, mode):
    # The isolated database fixture owns cleanup of committed test rows.
    with psycopg.connect(**database) as setup:
        item = queued_task(setup)
        policy = load_capacity_policy(Path(__file__).resolve().parents[2]/'configs/capacity-rtx3060.json')
        pin = install_capacity_policy_timing(setup,policy)
        setup.execute('UPDATE photo.worker_registrations SET capacity_policy_sha=%s,certificate_sha256=%s '
            'WHERE worker_id=%s',(pin,'b'*64,item['worker']))
        setup.execute('UPDATE photo.resource_nodes SET enabled=true,doctor_report_sha256=%s WHERE node_id=%s',
            ('a'*64,item['node']))
        if mode == 'residency_false':
            # Controlled refusal isolates the boolean rejection branch. Capacity
            # still uses the actual migrated heartbeat and actual node rows.
            setup.execute("""CREATE OR REPLACE FUNCTION photo.report_worker_model_residency(
                p_worker uuid,p_policy text,p_models jsonb) RETURNS boolean
                LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ SELECT false $$""")
    worker = TaskDatabase('fixture connection supplied below')
    monkeypatch.setattr(worker,'_connect',lambda:psycopg.connect(**database,
        options='-c role=photo_worker -c statement_timeout=5000 -c lock_timeout=3000'))
    residents = () if mode != 'residency_error' else (
        ResidentModel('a'*64,'sfd','cpu','b'*64,-1),)
    if mode == 'residency_error':
        with pytest.raises(psycopg.errors.InvalidParameterValue):
            worker.heartbeat_policy_worker_with_residency(item['worker'],'media','cpu',
                {'asset.verify'},1000,0,2000,pin,residents)
    else:
        assert worker.heartbeat_policy_worker_with_residency(item['worker'],'media','cpu',
            {'asset.verify'},1000,0,2000,pin,residents) is (mode == 'accepted')
    with psycopg.connect(**database) as observer:
        node = observer.execute('SELECT ram_available_bytes,scratch_available_bytes,reported_at '
            'FROM photo.resource_nodes WHERE node_id=%s',(item['node'],)).fetchone()
        registration = observer.execute('SELECT reported_capacity_policy_sha,capacity_policy_reported_at,'
            'model_residency_reported_at FROM photo.worker_registrations WHERE worker_id=%s',
            (item['worker'],)).fetchone()
    if mode == 'accepted':
        assert node[:2] == (1000,2000) and node[2] is not None
        assert registration[0] == pin and all(value is not None for value in registration[1:])
    else:
        assert node == (0,0,None)
        assert registration == (None,None,None)
