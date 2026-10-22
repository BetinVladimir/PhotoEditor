"""Canonical result discovery remains fenced and read-only."""
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]


def test_shown_params_require_committed_child_and_canonical_attempt():
    sql=(ROOT/'backend/db/migrations/02236_v27_shown_exposure_adoption_params.sql').read_text()
    for fragment in ("'semantic.adopt_shown_exposure'", "'shown_semantic'", 'r.graph_sealed',
                     'result.id=t.canonical_result_id', 'result.attempt=t.attempt',
                     'result.fencing_token=t.fencing_token', 't.attempt=p_attempt',
                     't.fencing_token=p_fence', "a.state='ready'", "tenant.status='active'"):
        assert fragment in sql
    assert 'TO photo_scheduler' in sql
    assert 'TO photo_worker' not in sql


def test_shown_discovery_does_not_restart_or_adopt_results():
    sql=(ROOT/'backend/db/migrations/02237_v27_shown_exposure_adoption_discovery.sql').read_text()
    assert 'z.attempt=t.attempt AND z.fencing_token=t.fencing_token' in sql
    assert 'v27_shown_exposure_adoption_params' in sql
    assert "i.consumer='v2-adopt'" in sql
    assert 'LIMIT 64' in sql
    assert 'UPDATE photo.' not in sql
    assert 'INSERT INTO' not in sql


def test_shown_inventory_is_private_and_preserves_atomic_retention():
    sql=(ROOT/'backend/db/migrations/02239_v27_shown_exposure_adoption_inventory.sql').read_text()
    for fragment in ("'shown_semantic'", "'semantic.adopt_shown_exposure'", 'FOR UPDATE',
                     'V2_ADOPTION_RESULT_CONFLICT', 'V2_ADOPTION_UNDECLARED_INPUT',
                     'INSERT INTO photo.v2_artifact_links', 'INSERT INTO photo.inbox',
                     'INSERT INTO photo.run_events', 'INSERT INTO photo.outbox'):
        assert fragment in sql
    assert 'GRANT EXECUTE' not in sql


def test_generic_sql_adoption_cannot_publish_shown_copy():
    sql=(ROOT/'backend/db/migrations/02240_v27_shown_exposure_generic_adoption_guard.sql').read_text()
    assert "operation='semantic.adopt_shown_exposure') THEN RETURN false" in sql
    assert 'RETURN photo.adopt_v2_plan_before_shown_exposure' in sql
    assert 'FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway' in sql


def test_shown_output_proof_requires_identical_pixels_and_metadata():
    sql=(ROOT/'backend/db/migrations/02241_v27_shown_exposure_output_binding.sql').read_text()
    for fragment in ("(shown-'pixels') IS DISTINCT FROM (output-'pixels')",
                     'copied.sha256 IS DISTINCT FROM original.sha256',
                     'copied.bytes IS DISTINCT FROM original.bytes',
                     'copied.produced_fence IS DISTINCT FROM p_fence',
                     'root_row.produced_attempt IS DISTINCT FROM p_attempt',
                     'v27_shown_exposure_adoption_ready', 'photo.output_uploads'):
        assert fragment in sql
    assert 'GRANT EXECUTE' not in sql


def test_shown_commit_repeats_proof_around_atomic_inventory():
    sql=(ROOT/'backend/db/migrations/02242_v27_shown_exposure_adoption_commit.sql').read_text()
    assert sql.count('photo.shown_exposure_output_bound(')==2
    assert 'photo.adopt_shown_exposure_inventory(' in sql
    assert 'SEMANTIC_SHOWN_ADOPTION_STALE' in sql
    assert 'ORDER BY r.id FOR UPDATE' in sql
    assert 'ORDER BY id FOR UPDATE' in sql
    assert 'TO photo_scheduler' in sql


def test_shown_report_evidence_requires_exact_completed_result():
    sql=(ROOT/'backend/db/migrations/02243_v27_shown_exposure_adoption_report.sql').read_text()
    assert 'v27_shown_exposure_adoption_ready' in sql
    assert 'IS DISTINCT FROM true THEN RETURN NULL' in sql
    assert "'object_version',report.object_version" in sql
    assert "'required_checks',p.required_checks" in sql
    assert 'TO photo_scheduler' in sql
    assert 'TO photo_worker' not in sql


def test_shown_claim_preserves_chain_and_starts_only_with_live_capacity():
    sql=(ROOT/'backend/db/migrations/02244_v27_shown_exposure_claim_start.sql').read_text()
    for fragment in ('claim_task_before_shown_exposure', "claimed.outcome='claimed'",
                     'v27_downstream_profile_ready', 'SEMANTIC_SHOWN_CLAIM_QUOTA_STALE',
                     'q.bytes=expected::bigint', "SET status='running',revision=revision+1",
                     'revision<9007199254740991', 'SEMANTIC_SHOWN_CLAIM_INPUTS_STALE'):
        assert fragment in sql


def test_shown_child_completion_waits_for_every_canonical_adoption():
    sql=(ROOT/'backend/db/migrations/02245_v27_shown_exposure_child_finalization.sql').read_text()
    for fragment in ("'shown_semantic'", "'semantic.adopt_shown_exposure:'",
                     "i.consumer='v2-adopt'", 'photo.v2_adoption_event_id(t.id,t.canonical_result_id)',
                     'photo.settle_semantic_child', 'reserved_bytes=reserved_bytes-q.bytes',
                     'finalize_next_semantic_apply_before_shown', 'finalize_next_shown_semantic_apply_child'):
        assert fragment in sql
    assert "next_state:='completed'" in sql
    assert 'INSERT INTO photo.outbox' in sql


def test_applied_image_read_requires_completed_adopted_retained_output():
    sql=(ROOT/'backend/db/migrations/02246_v27_shown_exposure_applied_image.sql').read_text()
    for fragment in ("target.state='applied'", "r.status='completed'", 'v2_editor_authorized',
                     "i.consumer='v2-adopt'", 'photo.v2_artifact_links',
                     'a.produced_attempt=t.attempt', 'a.produced_fence=t.fencing_token'):
        assert fragment in sql
    assert 'TO photo_api' in sql
    assert 'INSERT INTO' not in sql


def test_prechild_publication_locks_selected_receipt_and_approved_document():
    sql=(ROOT/'backend/db/migrations/02247_v27_shown_exposure_publication_scope.sql').read_text()
    assert 'photo.lock_semantic_decision_publication' in sql
    assert 'photo.lock_semantic_shown_exposure_params' in sql
    assert "d.purpose='task_params'" in sql
    assert "'semantic.edit:'||p_target::text" in sql
    assert "'semantic.adopt_shown_exposure:'||p_target::text" in sql
    assert 'GRANT EXECUTE' not in sql
    assert 'INSERT INTO' not in sql


def test_shown_document_tickets_repeat_authority_and_exact_params():
    sql=(ROOT/'backend/db/migrations/02248_v27_shown_exposure_transport_documents.sql').read_text()
    assert sql.count('photo.lock_semantic_shown_publication(')==2
    assert sql.count("p_sha IS DISTINCT FROM p_params_sha")==2
    assert "CASE WHEN p_purpose='task_params' THEN 'Params' ELSE p_purpose END" in sql
    assert 'UPDATE photo.backend_documents SET token=fence' in sql
    assert 'd.token IS DISTINCT FROM p_token' in sql
    assert 'DOCUMENT_QUOTA_ACCOUNTING' in sql
    assert 'photo.lock_semantic_approved_params_publication' not in sql


def test_shown_review_report_does_not_accept_render_operation():
    sql=(ROOT/'backend/db/migrations/02249_v27_shown_exposure_review_report.sql').read_text()
    assert "'semantic.adopt_shown_exposure','1','cpu',p_pin) IS DISTINCT FROM true" in sql
    assert "'semantic.local_exposure'" not in sql
    assert 'semantic_review_identity' in sql
    assert 'TO photo_api' in sql
