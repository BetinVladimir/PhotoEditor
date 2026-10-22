"""Actual same-workflow SQL producer/snapshot/target rows, metadata-only IO."""
import json
from functools import partial

import pytest
from photo_worker.contracts.semantic import SemanticPlan
from photo_worker.operations.session.contracts import InputSnapshot
from background_cleanup_preview_documents_fixture import (
    working_image_builder,protection_builder,transfer_params_builder,candidate_builder,
    review_calibration_body,review_snapshot_builder,review_plan_builder,
)
from test_v27_background_cleanup_preview_saved_authority import saved_authority_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('alpha',[False,True])
def test_two_actual_retouch_targets_share_review_but_have_distinct_producers(database,alpha):
    args,parent,scope,_=saved_authority_fixture(database,metadata_bodies=True,target_count=2,
        source_builder=working_image_builder(with_alpha=alpha),protection_builder=protection_builder(database),
        transfer_params_builder=partial(transfer_params_builder,kind='retouch_region'),
        transfer_output_builder=candidate_builder(database),calibration_body=review_calibration_body(),
        snapshot_builder=review_snapshot_builder,assembly_output_builder=review_plan_builder)
    targets=scope['review_targets']
    assert len(targets)==2 and len({row['target'] for row in targets})==2
    snapshot=InputSnapshot.model_validate_json(scope['snapshot_body'])
    plan=SemanticPlan.model_validate_json(scope['plan_body'])
    assert len(plan.candidates)==len(snapshot.items)==2
    assert {item.binding.asset_version_id for item in snapshot.items}=={row['version'] for row in targets}
    assert database.execute('SELECT item_count FROM photo.snapshots WHERE id=%s',(scope['snapshot'],)).fetchone()==(2,)
    sources=database.execute('SELECT contract,artifact_id,sha256 FROM photo.semantic_review_plan_sources(%s,%s,%s,%s)',
        (args[0],json.loads(args[5])['workflow_id'],args[2],args[3])).fetchall()
    candidates={row[1]:row[2] for row in sources if row[0]=='TransferCandidate'}
    assert set(candidates)=={row['candidate'] for row in targets}
    plan_sha=next(row[2] for row in sources if row[0]=='SemanticPlan')
    revision=database.execute('SELECT revision FROM photo.runs WHERE id=%s',(parent,)).fetchone()[0]
    for row in targets:
        assert database.execute('SELECT photo.semantic_preapply_preview_current('+','.join(['%s']*9)+')',
            (args[0],args[1],row['target'],args[2],args[3],revision,row['revision'],plan_sha,candidates[row['candidate']])).fetchone()[0] is True
    producers=scope['target_producers']
    assert len({row['producer'] for row in producers})==2
    assert database.execute("SELECT count(*) FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND operation='semantic.transfer' AND state='succeeded'",
        (args[0],parent)).fetchone()==(2,)
