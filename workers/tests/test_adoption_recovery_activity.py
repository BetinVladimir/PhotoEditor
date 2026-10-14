"""Direct recovery calls must not acknowledge a revoked adoption."""
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.delivery_assembly_recovery import delivery_assembly_adoption_committed
from photo_worker.v2.interchange_import_adoption_recovery import interchange_import_adoption_committed
from photo_worker.v2.interchange_parse_adoption_recovery import interchange_parse_adoption_committed
from photo_worker.v2.perspective_lines_adoption_recovery import perspective_lines_adoption_committed


@pytest.mark.parametrize('recover', [
    delivery_assembly_adoption_committed,
    interchange_import_adoption_committed,
    interchange_parse_adoption_committed,
    perspective_lines_adoption_committed,
])
@pytest.mark.parametrize('late', [False, True])
def test_false_activity_refuses_recovery_before_or_after_sql(recover, late):
    queries, checks = [], []

    def execute(statement, values):
        queries.append((statement, values))
        return SimpleNamespace(fetchone=lambda: (True,))

    def active():
        checks.append(True)
        return late and len(checks) == 1

    connection = SimpleNamespace(autocommit=True, execute=execute)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        recover(connection, uuid4(), uuid4(), check_active=active)
    assert len(queries) == (1 if late else 0)
