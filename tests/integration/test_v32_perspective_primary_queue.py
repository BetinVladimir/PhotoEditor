"""Authored V29 outer-config queue boundary; NOT RUN."""
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize("producer", ["v29_perspective_plan", "v29_perspective_lines_plan", "v29_multi_plan", "v29_perspective_line_detection", "v29_negative_draft_jcs"])
@pytest.mark.parametrize("saved,expected_queue", [
    (None, "interactive"), ({}, None), ({"queue_class": "interactive"}, "interactive"),
])
def test_preview_outer_queue_preserves_embedded_plan_and_historical_omission(db, producer, saved, expected_queue):
    config = {"project_id": "project", producer: {"config": {"inputs": []}, "params": {"image": "source"}},
              "v28_analysis_policy": {"model_lock_sha": "b"*64}}
    result = db.execute("SELECT photo.v32_frozen_preview_config(%s,%s)",
                        (Jsonb(config), None if saved is None else Jsonb(saved))).fetchone()[0]
    expected = dict(config)
    if expected_queue is not None:
        expected["queue_class"] = expected_queue
    assert result == expected
    assert result[producer] == config[producer]
    assert db.execute("SELECT photo.v32_frozen_preview_config(%s,%s)",
                      (Jsonb(config), Jsonb(result))).fetchone()[0] == result


@pytest.mark.parametrize("foreign", ["batch", "training", "capture", "", None, 1])
def test_analysis_outer_queue_refuses_foreign_saved_class(db, foreign):
    assert db.execute("SELECT photo.v32_frozen_preview_config(%s,%s)",
                      (Jsonb({"v28_analysis_plan": {}}), Jsonb({"queue_class": foreign}))).fetchone() == (None,)


@pytest.mark.parametrize("role", ["photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"])
def test_analysis_queue_helper_private(db, role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v32_frozen_preview_config(jsonb,jsonb)','EXECUTE')",
                      (role,)).fetchone() == (False,)


@pytest.mark.parametrize("operation", ["optical.negative", "optical.perspective", "optical.panorama", "optical.focus_stack"])
def test_replacement_queue_retains_exact_embedded_jcs(db, operation):
    draft = '{"schema_version":2,"identity":{"operation":"' + operation + '"},"owner":{"auth_session_id":"session"}}'
    config = {"project_id": "project", "v29_negative_replacement": {"version": "v29_negative_replacement_v2"},
              "v29_negative_replacement_draft_jcs": draft}
    fresh = db.execute("SELECT photo.v32_frozen_preview_config(%s,NULL)", (Jsonb(config),)).fetchone()[0]
    assert fresh == dict(config, queue_class="interactive")
    assert fresh["v29_negative_replacement_draft_jcs"] == draft
    assert db.execute("SELECT photo.v32_frozen_preview_config(%s,%s)", (Jsonb(config), Jsonb(fresh))).fetchone()[0] == fresh
    assert db.execute("SELECT photo.v32_frozen_preview_config(%s,%s)", (Jsonb(config), Jsonb(config))).fetchone()[0] == config


@pytest.mark.parametrize("key", ["v29_auto_perspective_segments", "v29_perspective_replacement_detection_jcs"])
def test_replacement_auxiliary_evidence_survives_outer_queue(db, key):
    evidence = {"segments": []} if key.endswith("segments") else '{"schema_version":1,"operation":"optical.perspective_lines"}'
    config = {"project_id": "project", key: evidence}
    queued = db.execute("SELECT photo.v32_frozen_preview_config(%s,NULL)", (Jsonb(config),)).fetchone()[0]
    assert queued == dict(config, queue_class="interactive")
    assert db.execute("SELECT photo.v32_frozen_preview_config(%s,%s)", (Jsonb(config), Jsonb(queued))).fetchone()[0] == queued
