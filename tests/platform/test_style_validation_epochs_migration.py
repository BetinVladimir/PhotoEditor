from pathlib import Path
from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.wire import TaskCommand

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00172_style_validation_epochs.sql').read_text()
WORKFLOW=(ROOT/'backend/internal/workflow/style_analysis.go').read_text()
SERVICE=(ROOT/'backend/internal/style/service.go').read_text()


def test_graph_v3_adds_fenced_holdout_validate_package_epochs():
    for phase in ("'face_detect'","'face_semantics'","'face_skin'","'effects_fit'","'holdout_features'","'holdout_registration'","'validate'","'package'"):
        assert phase in SQL
    assert 'graph_version IN(1,2,3)' in SQL and 'SET DEFAULT 3' in SQL
    for function in ('style_analysis_holdout_inputs','style_analysis_holdout_outputs','style_analysis_single_output'):
        assert f'CREATE FUNCTION photo.{function}' in SQL
        assert f'REVOKE ALL ON FUNCTION photo.{function}' in SQL
        assert f'TO photo_scheduler' in SQL
    assert 't.canonical_result_id IS NOT NULL' in SQL


def test_candidate_waits_for_package_and_exact_validation_inputs():
    for token in ("pn.phase='package'","pt.state='succeeded'","package.output_role='style_package'","vn.phase='validate'","report.output_role='quality_report'","pt.inputs @> jsonb_build_array"):
        assert token in SQL
    assert "NOT (r.config_snapshot ? 'holdout_snapshot_id')" in SQL


def test_coordinator_materializes_real_worker_contracts():
    for token in ('"face.detect"','"face.landmarks"','"face.parse"','"mask.face_skin"','"pair.fit.beauty"','"pair.fit.defects"','"holdout_features"','"holdout_registration"','"style.validate"','"style.package"','"beauty_fits"','"defect_evidence"','"training_session_ids"','"threshold_profile_id"','"package_version"'):
        assert token in WORKFLOW
    assert 'HoldoutSnapshotDigest' in WORKFLOW and 'holdout_snapshot_digest' in SERVICE


def test_v3_effect_epochs_preserve_v1_v2_transition_and_require_exact_counts():
    assert "WHEN 'color_fit' THEN CASE WHEN b.graph_version>=3 THEN 'face_detect' ELSE 'aggregate' END" in SQL
    assert "WHEN 'aggregate' THEN CASE WHEN b.graph_version>=3 THEN 'effects_fit' ELSE 'color_fit' END" in SQL
    for token in ("WHEN 'face_detect' THEN b.pair_count*2", "WHEN 'face_semantics' THEN b.pair_count*4", "WHEN 'face_skin' THEN b.pair_count*2", "WHEN 'effects_fit' THEN b.pair_count*2"):
        assert token in SQL


def test_worst_case_64_pair_validation_command_fits_broker_envelope():
    # candidate + for each pair: two manifests, two pixels, two alpha planes,
    # and one alignment. This is the largest declared-input shape.
    refs=[ArtifactRef(artifact_id=uuid4(),sha256=f'{i:064x}') for i in range(1,450)]
    command=TaskCommand(schema_version=1,event_id=uuid4(),tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=1,operation='style.validate',operation_version='1',resource_class='cpu',correlation_id=uuid4(),inputs=refs,params=ArtifactRef(artifact_id=uuid4(),sha256='f'*64),model=None,timeout_seconds=1200,traceparent='')
    assert len(canonical_bytes(command.model_dump(mode='json'))) < 65536
    assert 'jsonb_array_length(t.inputs)>512' in SQL
