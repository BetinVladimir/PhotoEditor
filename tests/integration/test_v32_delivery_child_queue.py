"""Delivery child queue boundary. Authored only; no execution evidence."""
import json
from uuid import uuid4

import pytest


def test_fresh_delivery_child_config_freezes_batch(db):
    tenant, project, child, export = (uuid4() for _ in range(4))
    request = {"child_run_id": str(child), "config_jcs": json.dumps({"metadata_policy": "strip"})}
    actual = db.execute(
        "SELECT photo.v32_delivery_child_config(%s,%s,%s::jsonb,%s)",
        (tenant, project, json.dumps(request), export),
    ).fetchone()[0]
    assert actual == {"project_id": str(project), "v30_delivery_export": request,
                      "export_id": str(export), "queue_class": "batch"}
    assert db.execute(
        "SELECT photo.v32_delivery_child_config(%s,%s,%s::jsonb,%s)",
        (tenant, project, json.dumps({"child_run_id": "invalid"}), export),
    ).fetchone() == (None,)


@pytest.mark.parametrize("role", ["photo_api", "photo_worker", "photo_scheduler",
                                  "photo_gateway", "photo_portal"])
def test_delivery_child_queue_helper_private(db, role):
    assert db.execute(
        "SELECT has_function_privilege(%s,'photo.v32_delivery_child_config(uuid,uuid,jsonb,uuid)','EXECUTE')",
        (role,),
    ).fetchone() == (False,)
