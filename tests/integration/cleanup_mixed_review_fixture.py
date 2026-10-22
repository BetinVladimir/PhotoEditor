"""Own multi-target cleanup review before preview admission.

Actual transfer/assembly/source SQL with explicit background cleanup intent. Shared image/mask
builders retain metadata-only limitations; no preview or qualification is implied.
"""
from functools import partial
from hashlib import sha256
import json

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
from test_v27_background_cleanup_preview_saved_authority import saved_authority_fixture
from background_cleanup_preview_documents_fixture import (
    working_image_builder, protection_builder, transfer_params_builder,
    candidate_builder, review_calibration_body, review_snapshot_builder, review_plan_builder,
)


def prepared_cleanup_reviews(db, *, target_count=3, with_alpha=False, dense_warps=0):
    args, parent, scope, _ = saved_authority_fixture(db, metadata_bodies=True,
        target_count=target_count, kind='background_cleanup',
        source_builder=working_image_builder(with_alpha=with_alpha, dense_warps=dense_warps),
        protection_builder=protection_builder(db),
        transfer_params_builder=partial(transfer_params_builder, kind='background_cleanup'),
        transfer_output_builder=candidate_builder(db), calibration_body=review_calibration_body(),
        snapshot_builder=review_snapshot_builder, assembly_output_builder=review_plan_builder)
    intent = json.loads(scope['intent_body'])
    assert intent['kind'] == 'background_cleanup' and intent['ev'] is None
    common = dict(SemanticPlan=scope['plan_body'], Params=scope['assembly_params_body'],
        InputSnapshot=scope['snapshot_body'], EditIntent=scope['intent_body'], Calibration=scope['calibration_body'])
    producers = scope.get('target_producers') or [dict(scope=scope, candidate=json.loads(args[5])['candidate']['artifact_id'])]
    candidates = {str(entry['candidate']): entry['scope']['candidate_body'] for entry in producers}
    identity = json.loads(args[5])
    rows = db.execute('SELECT contract,artifact_id,sha256,bytes FROM photo.semantic_review_plan_sources(%s,%s,%s,%s)',
        (args[0], identity['workflow_id'], args[2], args[3])).fetchall()
    assert len(rows) == 5 + target_count
    documents = []
    for contract, artifact, digest, size in rows:
        raw = candidates[str(artifact)] if contract == 'TransferCandidate' else common[contract]
        assert len(raw) == size and sha256(raw).hexdigest() == digest
        documents.append(dict(contract=contract, artifact_id=str(artifact), sha256=digest, body=raw.decode()))
    prepared = []
    shared = ('root', 'snapshot_sha', 'snapshot_body', 'plan_body', 'assembly_params_body', 'assembly', 'calibration_body', 'intent_body', 'intent_ref')
    for target, producer in zip(scope['review_targets'], producers):
        own = {**scope, **producer['scope']}
        own.update({key: scope[key] for key in shared if key in scope})
        image = WorkingImage.model_validate_json(own['source_body'])
        current = dict(identity, context_pixels=128, feather_pixels=8, target_id=str(target['target']), target_revision=str(target['revision']),
            source=dict(asset_version_id=str(target['version']), source=dict(artifact_id=str(own['source']), sha256=own['source_sha']), canvas_hash=canonical_hash(image.canvas)),
            candidate=dict(artifact_id=str(target['candidate']), sha256=sha256(own['candidate_body']).hexdigest()),
            protection=own['protection']['target_protection'],
            model_lock_sha='1'*64, runtime_lock_sha='2'*64, device_profile_sha='3'*64)
        typed = SemanticBackgroundCleanupPreviewInput.model_validate_json(canonical_bytes(current))
        raw = canonical_bytes(typed).decode()
        own.update(review_documents=documents, cleanup_params=typed, cleanup_params_body=raw,
                   cleanup_params_sha=sha256(raw.encode()).hexdigest())
        prepared.append(((*args[:4], target['target'], raw, own['cleanup_params_sha']), parent, own, target['target']))
    assert len(prepared) == target_count
    return prepared
