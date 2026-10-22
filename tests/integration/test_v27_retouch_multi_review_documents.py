"""Plural document builder coverage; projected bindings are not SQL authority."""
from copy import deepcopy
from uuid import uuid4

import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.semantic import SemanticTransferParams,TransferCandidate,SemanticPlan
from photo_worker.operations.session.contracts import InputSnapshot
from background_cleanup_preview_documents_fixture import review_snapshot_builder,review_plan_builder
from retouch_apply_documents_fixture import prepared_retouch_review_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def plural_documents(db):
    _,_,scope,_=prepared_retouch_review_fixture(db)
    scope=deepcopy(scope)
    first=SemanticTransferParams.model_validate_json(scope['transfer_params_body'])
    second=first.model_copy(deep=True)
    second.target.asset_version_id=uuid4()
    candidate=TransferCandidate.model_validate_json(scope['candidate_body'])
    other=candidate.model_copy(deep=True)
    other.target=second.target
    scope['transfer_params_bodies']=[canonical_bytes(first),canonical_bytes(second)]
    scope['candidate_bodies']=[canonical_bytes(candidate),canonical_bytes(other)]
    scope['snapshot_body']=review_snapshot_builder(scope,None)
    return scope


def test_plural_review_documents_keep_both_target_bindings_and_snapshot_digest(database):
    scope=plural_documents(database)
    snapshot=InputSnapshot.model_validate_json(scope['snapshot_body'])
    plan=SemanticPlan.model_validate_json(review_plan_builder(scope,None,None))
    assert [item.ordinal for item in snapshot.items]==[0,1]
    assert len({item.binding.asset_version_id for item in snapshot.items})==2
    assert snapshot.snapshot_digest==canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    assert plan.snapshot_digest==snapshot.snapshot_digest
    assert [candidate.target for candidate in plan.candidates]==[item.binding for item in snapshot.items]


@pytest.mark.parametrize('mode',['empty','duplicate','mixed_intent','missing_candidate','reordered_candidates'])
def test_plural_review_documents_refuse_incomplete_or_mixed_bindings(database,mode):
    scope=plural_documents(database)
    if mode=='empty':scope['transfer_params_bodies']=[]
    elif mode=='duplicate':scope['transfer_params_bodies'][1]=scope['transfer_params_bodies'][0]
    elif mode=='mixed_intent':
        second=SemanticTransferParams.model_validate_json(scope['transfer_params_bodies'][1])
        second.intent.artifact_id=uuid4()
        scope['transfer_params_bodies'][1]=canonical_bytes(second)
    elif mode=='missing_candidate':scope['candidate_bodies'].pop()
    else:scope['candidate_bodies'].reverse()
    with pytest.raises(ValueError):
        review_plan_builder(scope,None,None)
