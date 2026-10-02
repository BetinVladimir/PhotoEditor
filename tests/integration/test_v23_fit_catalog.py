"""The executable CPU fit adapter has a deny-by-default database contract."""


def test_fit_operation_is_scoped_and_not_enabled_before_graph_admission(db):
    registry = db.execute(
        "SELECT resource_class,timeout_seconds,backends,v2_enabled "
        "FROM photo.operation_registry WHERE operation='learning.fit' "
        "AND version='1'").fetchone()
    assert registry == ('cpu', 600, ['cpu'], False)
    policy = db.execute(
        "SELECT max_bytes,max_count,media_types FROM photo.operation_output_policies "
        "WHERE operation='learning.fit' AND version='1' AND role='document'"
    ).fetchone()
    assert policy == (8388608, 1, ['application/json'])


def test_evaluate_operation_is_gpu_scoped_and_disabled_until_worker_ready(db):
    registry = db.execute(
        "SELECT resource_class,timeout_seconds,backends,v2_enabled "
        "FROM photo.operation_registry WHERE operation='learning.evaluate' "
        "AND version='1'").fetchone()
    assert registry == ('gpu', 1800, ['cuda'], False)
    policy = db.execute(
        "SELECT max_bytes,max_count,media_types FROM photo.operation_output_policies "
        "WHERE operation='learning.evaluate' AND version='1' AND role='document'"
    ).fetchone()
    assert policy == (8388608, 1, ['application/json'])


def test_identity_operation_roles_are_scoped_and_dispatch_disabled(db):
    registry = db.execute(
        "SELECT resource_class,timeout_seconds,backends,v2_enabled "
        "FROM photo.operation_registry WHERE operation='identity.compare' "
        "AND version='1'").fetchone()
    assert registry == ('gpu', 1800, ['cuda'], False)
    policies = db.execute(
        "SELECT role,max_bytes,max_count,media_types "
        "FROM photo.operation_output_policies "
        "WHERE operation='identity.compare' AND version='1' ORDER BY role"
    ).fetchall()
    assert policies == [
        ('document', 8388608, 1, ['application/json']),
        ('mask', 8388608, 1, ['application/json']),
        ('mask_pixels', 800004096, 1, ['application/x-npy'])]
