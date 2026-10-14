"""Explicit operator provisioning entry point; never uses worker credentials."""
import os
from pathlib import Path
import psycopg
from .capacity_policy import load_capacity_policy
from .capacity_policy_installation import (
    install_capacity_policy_timing, rotate_worker_capacity_policy,
    validate_capacity_policy_rotation_scope,
)


def provision_capacity_policy(path: Path, *, rotate=False, worker_id=None,
                              principal=None, node=None, expected_sha=None,
                              environ=None, connect=None):
    if type(rotate) is not bool:
        raise ValueError('CAPACITY_POLICY_OPERATOR_MODE')
    if rotate:
        validate_capacity_policy_rotation_scope(worker_id, principal, node, expected_sha)
    elif any(value is not None for value in (worker_id, principal, node, expected_sha)):
        raise ValueError('CAPACITY_POLICY_OPERATOR_SCOPE')
    policy = load_capacity_policy(path)
    env = os.environ if environ is None else environ
    dsn = env.get('PHOTO_OPERATOR_DATABASE_URL')
    if not isinstance(dsn, str) or not dsn.strip():
        raise ValueError('CAPACITY_POLICY_OPERATOR_DATABASE_REQUIRED')
    factory = psycopg.connect if connect is None else connect
    with factory(dsn, autocommit=False, connect_timeout=5,
                 options='-c statement_timeout=30000 -c lock_timeout=5000') as connection:
        with connection.transaction():
            if rotate:
                changed = rotate_worker_capacity_policy(connection, policy, worker_id=worker_id,
                    principal=principal, node=node, expected_sha=expected_sha)
            else:
                install_capacity_policy_timing(connection, policy)
                changed = 0
    # Only acknowledge after successful transaction exit/commit.
    return {'schema_version': 1, 'policy_sha': policy.sha256,
            'offline_seconds': policy.offline_seconds, 'lease_seconds': policy.lease_seconds,
            'rotated_registrations': changed}


def reconcile_capacity_policy(path: Path, *, worker_id, principal, node, expected_sha,
                              environ=None, connect=None):
    """Observe exact current pins in one snapshot; never infer commit ownership.

    Matching next pin is a state observation, not permission to resume workers.
    """
    from photo_worker.contracts.base import canonical_bytes
    validate_capacity_policy_rotation_scope(worker_id, principal, node, expected_sha)
    policy = load_capacity_policy(path)
    env = os.environ if environ is None else environ
    dsn = env.get('PHOTO_OPERATOR_DATABASE_URL')
    if not isinstance(dsn, str) or not dsn.strip():
        raise ValueError('CAPACITY_POLICY_OPERATOR_DATABASE_REQUIRED')
    factory = psycopg.connect if connect is None else connect
    with factory(dsn, autocommit=False, connect_timeout=5,
                 options='-c statement_timeout=30000 -c lock_timeout=5000') as connection:
        with connection.transaction():
            connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            timing = connection.execute(
                'SELECT policy_jcs,offline_seconds,lease_seconds FROM photo.capacity_policy_timings WHERE policy_sha=%s',
                (policy.sha256,)).fetchone()
            if timing is not None and timing != (canonical_bytes(policy).decode(), policy.offline_seconds, policy.lease_seconds):
                raise ValueError('CAPACITY_POLICY_INSTALLATION_CONFLICT')
            rows = connection.execute(
                'SELECT node_id,capacity_policy_sha FROM photo.worker_registrations '
                'WHERE worker_id=%s AND db_principal=%s::name AND enabled ORDER BY id LIMIT 10001',
                (worker_id, principal)).fetchall()
            if len(rows) > 10000:
                raise ValueError('CAPACITY_POLICY_RECONCILIATION_LIMIT')
            if not rows:
                state = 'missing_registration'
            elif all(row == (node, policy.sha256) for row in rows):
                state = 'matches_next'
            elif all(row == (node, expected_sha) for row in rows):
                state = 'matches_expected'
            else:
                state = 'conflict'
    return {'schema_version': 1, 'policy_sha': policy.sha256, 'state': state,
            'policy_installed': timing is not None, 'registrations': len(rows)}
