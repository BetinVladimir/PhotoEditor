from pathlib import Path
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.defects import DefectCandidates


def test_go_review_fixture_matches_current_worker_contract():
    root=Path(__file__).resolve().parents[2]
    raw=(root/'backend/internal/review/testdata/defect-proposal-python.json').read_bytes()
    proposal=DefectCandidates.model_validate_json(raw)
    assert canonical_bytes(proposal)==raw
    assert len(proposal.candidates)==1
    assert parse_json(raw)['candidates'][0]['decision']=='pending'


def test_go_repair_params_and_selection_match_worker():
    from photo_worker.contracts.defects import DefectReview,DefectRepairParams
    from photo_worker.contracts.base import canonical_hash
    root=Path(__file__).resolve().parents[2]/'backend/internal/review/testdata'
    selection_raw=(root/'defect-selection-python.json').read_bytes()
    params_raw=(root/'defect-repair-params-python.json').read_bytes()
    selection=DefectReview.model_validate_json(selection_raw)
    params=DefectRepairParams.model_validate_json(params_raw)
    assert canonical_bytes(selection)==selection_raw
    assert canonical_bytes(params)==params_raw
    assert params.review.sha256==canonical_hash(selection)
    assert params.proposal==selection.proposal and params.image==selection.image


def test_backend_working_image_fixtures_match_worker():
    from photo_worker.contracts.images import WorkingImage
    import json
    root=Path(__file__).resolve().parents[2]/'backend/internal/review/testdata'
    raw=(root/'working-images-python.json').read_bytes()
    images=[WorkingImage.model_validate_json(json.dumps(v)) for v in parse_json(raw)]
    assert len(images)==5
    assert canonical_bytes([v.model_dump(mode='json') for v in images])==raw


def test_backend_resolved_binding_fixture_matches_worker():
    from photo_worker.contracts.resolved import ResolvedStyle
    root=Path(__file__).resolve().parents[2]/'backend/internal/review/testdata'
    raw=(root/'resolved-binding-python.json').read_bytes()
    resolved=ResolvedStyle.model_validate_json(raw)
    assert canonical_bytes(resolved)==raw
    assert resolved.mode=='skip' and resolved.params.strict_ood


def test_backend_proposal_report_fixture_matches_worker():
    from photo_worker.contracts.defects import DefectProposeReport
    root=Path(__file__).resolve().parents[2]/'backend/internal/review/testdata'
    raw=(root/'proposal-report-python.json').read_bytes()
    report=DefectProposeReport.model_validate_json(raw)
    assert canonical_bytes(report)==raw
    assert report.protection.protected
