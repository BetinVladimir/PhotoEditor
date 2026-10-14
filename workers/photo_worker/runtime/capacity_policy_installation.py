"""Operator-only installation of exact policy timing; never called by workers.

The caller supplies an operator connection inside an explicit transaction and
commits only after all node/registration policy pins have been installed.
"""
from .capacity_policy import CapacityPolicy
from photo_worker.contracts.base import canonical_bytes


def install_capacity_policy_timing(connection, policy: CapacityPolicy) -> str:
    if not isinstance(policy, CapacityPolicy) or getattr(connection, 'autocommit', None) is not False:
        raise ValueError('CAPACITY_POLICY_INSTALLATION_INVALID')
    # Revalidate even a model_copy/model_construct supplied by an operator.
    raw = canonical_bytes(policy)
    owned = CapacityPolicy.model_validate_json(raw)
    pin = owned.sha256
    body = raw.decode('utf-8')
    connection.execute(
        'INSERT INTO photo.capacity_policy_timings(policy_sha,policy_jcs,offline_seconds,lease_seconds) '
        'VALUES(%s,%s,%s,%s) ON CONFLICT(policy_sha) DO NOTHING',
        (pin, body, owned.offline_seconds, owned.lease_seconds))
    row = connection.execute(
        'SELECT policy_jcs,offline_seconds,lease_seconds FROM photo.capacity_policy_timings WHERE policy_sha=%s',
        (pin,)).fetchone()
    if row != (body, owned.offline_seconds, owned.lease_seconds):
        raise ValueError('CAPACITY_POLICY_INSTALLATION_CONFLICT')
    return pin


def rotate_worker_capacity_policy(connection, policy: CapacityPolicy, *, worker_id,
                                  principal: str, node: str, expected_sha: str | None) -> int:
    """CAS all enabled registrations of one operator-selected worker principal.

    The caller owns commit/rollback and restarts the worker with the same policy.
    No credentials, enabled flags, resource budgets or qualifications are changed.
    """
    validate_capacity_policy_rotation_scope(worker_id, principal, node, expected_sha)
    pin = install_capacity_policy_timing(connection, policy)
    row = connection.execute('SELECT photo.rotate_worker_capacity_policy(%s,%s,%s,%s,%s)',
                             (worker_id, principal, node, expected_sha, pin)).fetchone()
    if row is None or len(row) != 1 or type(row[0]) is not int or row[0] < 1:
        raise ValueError('CAPACITY_POLICY_ROTATION_CONFLICT')
    return row[0]


def validate_capacity_policy_rotation_scope(worker_id, principal, node, expected_sha):
    import re
    from uuid import UUID
    if (not isinstance(worker_id, UUID) or worker_id.int == 0
            or not isinstance(principal, str) or not principal or len(principal.encode('utf-8')) > 63 or '\x00' in principal
            or not isinstance(node, str) or not node or '\x00' in node
            or expected_sha is not None and
               (not isinstance(expected_sha, str) or re.fullmatch(r'[0-9a-f]{64}', expected_sha) is None)):
        raise ValueError('CAPACITY_POLICY_ROTATION_INVALID')
