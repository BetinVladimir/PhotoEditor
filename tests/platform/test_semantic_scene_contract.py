"""Frozen semantic scene evidence contract scenarios, authored not executed."""
from copy import deepcopy
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_hash, canonical_bytes
from photo_worker.contracts.semantic_scene import SemanticSceneEvidence,SemanticScenePolicy


def ref():return {'artifact_id':str(uuid4()),'sha256':'a'*64}


def evidence():
    policy={'algorithm':'farneback_depth_plane_v1','background_labels':[0,1],
        'proxy_edge':512,'min_support':64,'flow_error_px':.5,'homography_error_px':.5,'depth_plane_error':.01}
    def source():return {'binding':{'asset_version_id':str(uuid4()),'source':ref(),'canvas_hash':'b'*64},'labels':ref(),'labels_params':ref(),'labels_pixels':ref(),'depth':ref(),'depth_params':ref(),'depth_pixels':ref(),'depth_invalid':None}
    return dict(schema_version=1,reference=source(),target=source(),intent=ref(),calibration=ref(),
        policy=policy,policy_sha=canonical_hash(policy),model_lock_sha='c'*64,runtime_lock_sha='d'*64,
        device_profile_sha='e'*64,dataset_sha='f'*64)


def test_exact_owned_evidence_roundtrip():
    raw=evidence();model=SemanticSceneEvidence.model_validate_json(canonical_bytes(raw))
    assert model.model_dump(mode='json')==raw
    raw['policy']['background_labels'].append(8)
    assert model.policy.background_labels==[0,1]


@pytest.mark.parametrize('change',[
    lambda v:v['policy'].__setitem__('flow_error_px',1),
    lambda v:v['target'].__setitem__('depth',deepcopy(v['reference']['depth'])),
    lambda v:v['target']['binding'].__setitem__('asset_version_id',v['reference']['binding']['asset_version_id']),
    lambda v:v.__setitem__('planar',True),
])
def test_mutated_or_self_asserted_evidence_refuses(change):
    value=evidence();change(value)
    with pytest.raises(ValueError):SemanticSceneEvidence.model_validate_json(canonical_bytes(value))


def test_policy_has_no_background_or_threshold_defaults():
    with pytest.raises(ValueError):SemanticScenePolicy.model_validate({'algorithm':'farneback_depth_plane_v1'})


def test_reference_also_present_in_target_snapshot_can_reuse_exact_analysis():
    value=evidence();value['target']=deepcopy(value['reference'])
    assert SemanticSceneEvidence.model_validate_json(canonical_bytes(value)).target==SemanticSceneEvidence.model_validate_json(canonical_bytes(value)).reference
