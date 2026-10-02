from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CONTRACT = (ROOT / "backend/db/migrations/00184_candidate_review_contract.sql").read_text()
FLOW = (ROOT / "backend/db/migrations/00185_apply_v4_candidate_reviews.sql").read_text()


def test_candidate_review_contract_is_immutable_and_fenced():
    assert "'candidate','base','skip'" in CONTRACT
    assert "reviews_one_candidate_task" in CONTRACT
    assert "t.canonical_result_id" in CONTRACT
    assert "result_outputs @>" in CONTRACT
    assert "t.inputs @>" in CONTRACT
    assert "SECURITY DEFINER" in CONTRACT
    assert "REVOKE ALL ON FUNCTION photo.create_candidate_review" in CONTRACT


def test_apply_candidate_review_barrier_is_per_item_and_fail_closed():
    assert "background_review','inpaint_review','generation_review" in FLOW
    assert "review_count<>b.item_count" in FLOW
    assert "v.state<>'decided'" in FLOW
    assert "decision_route='candidate'" in FLOW
    assert "CASE WHEN v.decision_route='candidate' THEN v.candidate_artifact_id ELSE v.base_artifact_id END" in FLOW
    assert "GRANT EXECUTE ON FUNCTION photo.process_apply_v4_candidate_reviews" in FLOW
    assert "GRANT EXECUTE ON FUNCTION photo.apply_v4_candidate_outputs" in FLOW
