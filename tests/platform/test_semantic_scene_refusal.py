"""Scientific refusal remains explicit; authority/IO failures are never proposals."""
import numpy as np
import pytest
from photo_worker.operations.semantic.scene_refusal import scientific_scene_outcome,SceneUnsupported,SceneDecision
from photo_worker.operations.semantic.prepare_transfer import prepare_planar_transfer


@pytest.mark.parametrize('reason',['SEMANTIC_SCENE_NONPLANAR','SEMANTIC_SCENE_FOREGROUND',
    'SEMANTIC_SCENE_PROTECTED','SEMANTIC_SCENE_PARALLAX_OR_OCCLUSION'])
def test_completed_scene_safety_refusal_stages_explicit_unsupported_without_mask(reason):
    rgb=np.zeros((32,32,3),np.float32);mask=np.ones((32,32),np.float32)
    class NoScratch:
        def reserve(self,*args):raise AssertionError('unsupported must not allocate or stage a mask')
    def verifier():
        with scientific_scene_outcome():raise ValueError(reason)
    profile={'resource_estimate':dict(ram_bytes=2**30,scratch_bytes=2**30,vram_bytes=0,output_bytes=2**30)}
    with prepare_planar_transfer(rgb,rgb,mask,source_protected=mask*0,target_protected=mask*0,
            scratch=NoScratch(),execution_profile=profile,match_error_px_max=1,
            match_inlier_min=.9,verify_scene=verifier) as prepared:
        assert prepared.mask is None and prepared.match.reference_to_target is None
        assert prepared.match.reason==reason


@pytest.mark.parametrize('reason',['SEMANTIC_SCENE_POLICY_INVALID','SEMANTIC_SCENE_ARRAY_INVALID',
    'SEMANTIC_SCENE_SCOPE_CHANGED','SEMANTIC_SCENE_DOCUMENT_BYTES_INVALID'])
def test_missing_authority_or_corrupt_inputs_are_not_scientific_refusal(reason):
    with pytest.raises(ValueError) as caught:
        with scientific_scene_outcome():raise ValueError(reason)
    assert not isinstance(caught.value,SceneUnsupported)


def test_scene_decision_cannot_encode_arbitrary_infrastructure_failure():
    assert SceneDecision(None).reason is None
    with pytest.raises(ValueError):SceneDecision('SEMANTIC_SCENE_SCOPE_CHANGED')
