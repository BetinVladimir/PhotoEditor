from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.dust import (DustCandidate, DustCandidates,
    DustRepairParams, DustReview, SensorCrop, dust_candidate_id)
from photo_worker.operations.learning.dust_evidence import (
    DustHoldoutExpected, FrozenDustHoldoutEvidence,
    decode_frozen_dust_holdout_evidence,
    validate_frozen_dust_material,
)


def ref():
    return ArtifactRef(artifact_id=uuid4(), sha256='a'*64)


def evidence():
    image, stage_image, protection, proposal, params = ref(), ref(), ref(), ref(), ref()
    selection = DustReview(version='sensor_dust_review_v1', proposal=proposal,
        base=stage_image, revision=2, accepted_candidate_ids=['b'*64],
        rejected_candidate_ids=[])
    ids = {key:uuid4() for key in ('tenant_id','event_id','asset_version_id',
                                   'review_id','run_id','actor_id')}
    decision = {'version':'sensor_dust_decision_v1', 'tenant_id':str(ids['tenant_id']),
                'review_id':str(ids['review_id']), 'run_id':str(ids['run_id']),
                'actor_id':str(ids['actor_id']), 'decision':'accept',
                'selection':selection.model_dump(mode='json')}
    value = FrozenDustHoldoutEvidence(schema_version=1, **ids, decision='accept',
        route='repair', decision_hash=canonical_hash(decision), image=image,
        stage_image=stage_image,
        protection=protection, proposal=proposal, params=params, selection=selection)
    expected = DustHoldoutExpected(**ids, decision_hash=value.decision_hash,
        image=image, stage_image=stage_image, protection=protection,
        proposal=proposal, params=params)
    return value, expected


def test_frozen_dust_review_exact_external_binding():
    value, expected = evidence();raw=canonical_bytes(value)
    artifact=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    restored=decode_frozen_dust_holdout_evidence(raw,artifact,expected)
    assert restored.selection.accepted_candidate_ids==['b'*64]
    foreign=expected.model_copy(update={'actor_id':uuid4()})
    with pytest.raises(ValueError,match='STALE_FROZEN_DUST_HOLDOUT'):
        decode_frozen_dust_holdout_evidence(raw,artifact,foreign)
    with pytest.raises(ValueError,match='INVALID_FROZEN_DUST_HOLDOUT'):
        decode_frozen_dust_holdout_evidence(raw,artifact.model_copy(update={'sha256':'c'*64}),expected)


def test_frozen_dust_review_rejects_changed_selection_and_alias():
    value,_=evidence()
    with pytest.raises(ValueError):
        FrozenDustHoldoutEvidence.model_validate(value.model_dump(mode='python') |
            {'selection':value.selection.model_copy(update={'accepted_candidate_ids':[]})})
    with pytest.raises(ValueError):
        FrozenDustHoldoutEvidence.model_validate(value.model_dump(mode='python') |
            {'params':value.proposal})
    with pytest.raises(ValueError):
        FrozenDustHoldoutEvidence.model_validate(value.model_dump(mode='python') |
            {'selection':value.selection.model_copy(update={'base':value.params})})


def test_frozen_dust_material_requires_exact_candidate_and_review():
    value, _ = evidence()
    frames = [value.stage_image, *[ref() for _ in range(4)]]
    camera = 'raw-' + 'c'*64
    candidate_id = dust_candidate_id(camera, 64, 64, frames, 10.0, 11.0, 2.0)
    proposal = DustCandidates(frames=frames, camera_id=camera,
        sensor_width=64, sensor_height=64,
        raw_crop=SensorCrop(x=0, y=0, width=64, height=64),
        candidates=[DustCandidate(id=candidate_id, sensor_x=10.0,
            sensor_y=11.0, radius_sensor_px=2.0, observed_frames=5,
            eligible_frames=5, recurrence=1.0)], warnings=[])
    selection = value.selection.model_copy(update={'accepted_candidate_ids':[candidate_id]})
    decision = {'version':'sensor_dust_decision_v1',
        'tenant_id':str(value.tenant_id), 'review_id':str(value.review_id),
        'run_id':str(value.run_id), 'actor_id':str(value.actor_id),
        'decision':'accept', 'selection':selection.model_dump(mode='json')}
    frozen = value.model_copy(update={'selection':selection,
        'decision_hash':canonical_hash(decision)})
    params = DustRepairParams(image=value.stage_image, proposal=value.proposal,
        review=selection)
    assert validate_frozen_dust_material(frozen, params, proposal) == (candidate_id,)
    wrong = params.model_copy(update={'image':ref()})
    with pytest.raises(ValueError, match='HOLDOUT_DUST_MATERIAL_CHANGED'):
        validate_frozen_dust_material(frozen, wrong, proposal)
    absent = proposal.model_copy(update={'candidates':[], 'warnings':['no_repeated_dust']})
    with pytest.raises(ValueError, match='UNKNOWN_DUST_CANDIDATE'):
        validate_frozen_dust_material(frozen, params, absent)
