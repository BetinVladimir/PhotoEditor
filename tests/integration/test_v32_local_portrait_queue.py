"""Authored local queue authority boundary; NOT RUN."""
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize("request", [None, {}, [], {"tenant_id": "invalid"},
                                     {"tenant_id": None, "run_id": None}])
def test_local_queue_refuses_malformed_or_missing_authority(db, request):
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert db.execute("SELECT photo.v32_local_portrait_queue(%s)",
                      (None if request is None else Jsonb(request),)).fetchone() == (None,)


@pytest.mark.parametrize("role,allowed", [("photo_scheduler", True), ("photo_api", False),
    ("photo_worker", False), ("photo_gateway", False), ("photo_portal", False)])
def test_local_queue_acl(db, role, allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v32_local_portrait_queue(jsonb)','EXECUTE')",
                      (role,)).fetchone() == (allowed,)
