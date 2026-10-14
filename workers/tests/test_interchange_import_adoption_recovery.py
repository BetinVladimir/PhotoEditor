from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_adoption_recovery import interchange_import_adoption_committed


@pytest.mark.parametrize('row', [(True,), (False,), None, (1,), ('true',)])
def test_import_recovery_only_acknowledges_strict_boolean_receipt(row):
    calls = []
    def execute(query, values):
        calls.append((query, values))
        return SimpleNamespace(fetchone=lambda: row)
    connection = SimpleNamespace(autocommit=True, execute=execute)
    tenant, task = uuid4(), uuid4()
    if row in ((True,), (False,)) and type(row[0]) is bool:
        assert interchange_import_adoption_committed(connection, tenant, task, check_active=lambda: None) is row[0]
    else:
        with pytest.raises(AdoptionInvalid):
            interchange_import_adoption_committed(connection, tenant, task, check_active=lambda: None)
    assert calls[0][1] == (tenant, task)
