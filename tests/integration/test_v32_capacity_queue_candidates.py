"""Authored candidate SQL boundary tests; full task/pool fixtures still required."""
import psycopg
import pytest


@pytest.mark.parametrize('pool', [None, '', 'general', 'GPU'])
def test_capacity_candidates_refuse_unknown_pool(db, pool):
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():
            db.execute('SELECT * FROM photo.capacity_queue_candidates(%s)', (pool,))


@pytest.mark.parametrize('role,allowed', [
    ('photo_api', False), ('photo_worker', False), ('photo_scheduler', True),
])
def test_capacity_candidates_scheduler_only(db, role, allowed):
    assert db.execute(
        "SELECT has_function_privilege(%s,'photo.capacity_queue_candidates(text)','EXECUTE')",
        (role,),
    ).fetchone() == (allowed,)


@pytest.mark.parametrize('pool', ['cpu', 'gpu', 'signer'])
def test_empty_capacity_candidate_snapshot(db, pool):
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT * FROM photo.capacity_queue_candidates(%s)', (pool,)).fetchall() == []


def test_selected_capacity_dispatch_requires_leader_session(db):
    from uuid import uuid4
    with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState):
        with db.transaction():
            db.execute(
                'SELECT * FROM photo.capacity_pick_selected_task(%s,%s,%s,%s)',
                (uuid4(), uuid4(), 'cpu', 2),
            )


@pytest.mark.parametrize('role,allowed', [
    ('photo_api', False), ('photo_worker', False), ('photo_scheduler', True),
])
def test_selected_capacity_dispatch_scheduler_only(db, role, allowed):
    assert db.execute(
        "SELECT has_function_privilege(%s,'photo.capacity_pick_selected_task(uuid,uuid,text,bigint)','EXECUTE')",
        (role,),
    ).fetchone() == (allowed,)


@pytest.mark.parametrize('role,allowed', [('photo_api', False), ('photo_worker', False), ('photo_scheduler', True)])
def test_legacy_partition_picker_scheduler_only(db, role, allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.pick_ready_legacy_task()','EXECUTE')", (role,)).fetchone() == (allowed,)


@pytest.mark.parametrize('role,allowed', [('photo_api', False), ('photo_worker', False), ('photo_scheduler', True)])
def test_targeted_capacity_candidate_scheduler_only(db, role, allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.capacity_queue_candidate(text,uuid)','EXECUTE')", (role,)).fetchone() == (allowed,)


@pytest.mark.parametrize('pool', [None, '', 'general', 'GPU'])
def test_targeted_capacity_candidate_invalid_pool(db, pool):
    from uuid import uuid4
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():
            db.execute('SELECT * FROM photo.capacity_queue_candidate(%s,%s)', (pool, uuid4()))


def test_targeted_capacity_candidate_null_task(db):
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():
            db.execute('SELECT * FROM photo.capacity_queue_candidate(%s,%s)', ('cpu', None))


@pytest.mark.parametrize('role,allowed', [('photo_api', False), ('photo_worker', False), ('photo_scheduler', True)])
def test_heavy_admission_scheduler_only(db, role, allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.capacity_heavy_admission_available(text)','EXECUTE')", (role,)).fetchone() == (allowed,)


def test_heavy_admission_requires_capacity_leader(db):
    with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState):
        with db.transaction():
            db.execute('SELECT photo.capacity_heavy_admission_available(%s)', ('a'*64,))


def test_heavy_admission_missing_policy_refuses(db):
    db.execute('SELECT pg_advisory_xact_lock(2026,32)')
    assert db.execute('SELECT photo.capacity_heavy_admission_available(%s)', ('a'*64,)).fetchone() == (False,)
