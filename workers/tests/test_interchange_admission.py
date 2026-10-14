from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import Contract, canonical_bytes
from photo_worker.operations.interchange.admission import SQLParseAdmission
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.interchange_archive import ArchiveLimits


@pytest.mark.parametrize("response", [None, {}, True,
    {"archive_bytes": True, "expanded_bytes": 10, "files": 1},
    {"archive_bytes": 10, "expanded_bytes": 10, "files": 30001},
    {"archive_bytes": 10, "expanded_bytes": 10, "files": 1, "extra": 1}])
def test_refuses_missing_or_malformed_sql_authority(response):
    ctx = SimpleNamespace(check_active=lambda: None, lease=object())
    database = SimpleNamespace(interchange_parse_context=lambda lease, raw: response)
    class Params(Contract):
        schema_version: int
    with pytest.raises(OperationFailure):
        SQLParseAdmission(database).admit(ctx, Params(schema_version=1))


def test_forwards_exact_lease_and_params_without_io():
    class Params(Contract):
        schema_version: int
    params = Params(schema_version=1)
    ctx = SimpleNamespace(check_active=lambda: None, lease=object())
    calls = []
    def context(lease, raw):
        calls.append((lease, raw))
        return {"archive_bytes": 10, "expanded_bytes": 20, "files": 2}
    limits = SQLParseAdmission(SimpleNamespace(interchange_parse_context=context)).admit(ctx, params)
    assert limits == ArchiveLimits(10, 20, 2)
    assert calls == [(ctx.lease, canonical_bytes(params))]
