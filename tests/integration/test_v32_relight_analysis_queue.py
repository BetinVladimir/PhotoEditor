"""Authored analysis outer-config queue boundary; NOT RUN."""
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize("saved,expected_queue", [
    (None, "interactive"), ({}, None), ({"queue_class": "interactive"}, "interactive"),
])
def test_analysis_outer_queue_preserves_embedded_plan_and_historical_omission(db, saved, expected_queue):
    config = {"project_id": "project", "v28_analysis_plan": {"config": {"inputs": []}, "params": {"image": "source"}},
              "v28_analysis_policy": {"model_lock_sha": "b"*64}}
    result = db.execute("SELECT photo.v32_relight_analysis_queue_config(%s,%s)",
                        (Jsonb(config), None if saved is None else Jsonb(saved))).fetchone()[0]
    expected = dict(config)
    if expected_queue is not None:
        expected["queue_class"] = expected_queue
    assert result == expected
    assert result["v28_analysis_plan"] == config["v28_analysis_plan"]
    assert db.execute("SELECT photo.v32_relight_analysis_queue_config(%s,%s)",
                      (Jsonb(config), Jsonb(result))).fetchone()[0] == result


@pytest.mark.parametrize("foreign", ["batch", "training", "capture", "", None, 1])
def test_analysis_outer_queue_refuses_foreign_saved_class(db, foreign):
    assert db.execute("SELECT photo.v32_relight_analysis_queue_config(%s,%s)",
                      (Jsonb({"v28_analysis_plan": {}}), Jsonb({"queue_class": foreign}))).fetchone() == (None,)


@pytest.mark.parametrize("role", ["photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"])
def test_analysis_queue_helper_private(db, role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v32_relight_analysis_queue_config(jsonb,jsonb)','EXECUTE')",
                      (role,)).fetchone() == (False,)
