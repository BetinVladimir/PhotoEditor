from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00120_apply_builds.sql').read_text()
SERVICE=(ROOT/'backend/internal/run/apply.go').read_text()


def test_apply_build_has_durable_bounded_state():
    assert 'CREATE TABLE photo.apply_builds' in SQL
    assert 'item_count BETWEEN 1 AND 5000' in SQL
    assert 'photo.claim_apply_build()' in SQL
    assert 'photo.renew_apply_build' in SQL
    assert 'photo.advance_apply_build' in SQL
    assert 'photo.fail_apply_build' in SQL
    assert "('mask','stats','resolve','color','local','adopt','complete','failed')" in SQL
    assert '(lease_token IS NULL)=(lease_until IS NULL)' in SQL


def test_apply_creation_is_atomic_and_version_pinned():
    assert 'FOR UPDATE' in SERVICE
    assert 'v.config_hash' in SERVICE and 'canonical.Hash(json.RawMessage(styleConfig))' in SERVICE
    assert "'apply','queued'" in SERVICE
    assert 'INSERT INTO photo.apply_builds' in SERVICE
    assert 'INSERT INTO photo.quota_reservations' in SERVICE
    assert 'input_snapshot_digest' in SERVICE
    assert 'all_change_selected' in SERVICE


def test_apply_routes_share_one_service_method():
    routes=(ROOT/'backend/internal/httpapi/catalog.go').read_text()
    assert 'collections/{id}/apply-profile' in routes
    assert 'runs.Apply(' in routes
    assert 'runs.Create(' in routes

def test_style_acceptance_pins_the_verified_candidate_artifact_for_apply():
    style=(ROOT/'backend/internal/style/service.go').read_text()
    assert 'c.config_artifact_id' in style
    assert 'a.sha256=c.config_hash' in style
    assert 'export_artifact_id)' in style
    assert 'configArtifactID' in style
    assert '+ ".json"' in style
