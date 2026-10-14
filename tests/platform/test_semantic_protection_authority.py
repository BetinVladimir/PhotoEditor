from copy import deepcopy
import pytest
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.semantic.protection_authority import semantic_protection_authority
from photo_worker.v2.adoption import AdoptionInvalid
from test_semantic_source_authority import fixture


@pytest.mark.parametrize('variant',['valid','unprotected','geometry','revoked','truthy'])
def test_protection_requires_exact_geometry_and_literal_canonical_origin(tmp_path,variant):
    _,_,params,image,_,_,_=fixture(tmp_path)
    mask=MaskRef(artifact=params.intent,canvas=deepcopy(image.canvas),transform_chain=deepcopy(image.transform_chain),
        selector='manual',confidence=1.,model_lock_id=None,protected=variant!='unprotected')
    if variant=='geometry':mask.canvas.width+=1
    calls=[]
    def origin(binding,root,child):
        calls.append((deepcopy(binding),deepcopy(root),deepcopy(child)))
        binding.source.sha256='0'*64
        return 1 if variant=='truthy' else variant!='revoked'
    verify=semantic_protection_authority(lambda ref,model:mask,origin)
    before=deepcopy(params)
    if variant=='valid':
        verify(params,image,image,params.intent,params.calibration)
        assert len(calls)==2
        assert calls[0]==(params.reference,params.intent,mask.artifact)
        assert calls[1]==(params.target,params.calibration,mask.artifact)
    else:
        with pytest.raises(AdoptionInvalid):verify(params,image,image,params.intent,params.calibration)
        if variant in ('unprotected','geometry'):assert calls==[]
    assert params==before


def test_protection_has_no_implicit_provenance():
    with pytest.raises(ValueError):semantic_protection_authority(lambda ref,model:None,None)


def test_sql_origin_uses_owned_lease_and_exact_root_child(tmp_path):
    from types import SimpleNamespace
    from photo_worker.operations.semantic.protection_authority import sql_semantic_protection_authority
    _,_,params,image,lease,raw,_=fixture(tmp_path)
    mask=MaskRef(artifact=params.intent,canvas=image.canvas,transform_chain=image.transform_chain,
        selector='manual',confidence=1.,model_lock_id=None,protected=True)
    calls=[]
    def origin(owner,binding,root,child,*,target):
        calls.append((owner.command.params.sha256,root,child,target));return True
    db=SimpleNamespace(semantic_protection_origin=origin)
    verify=sql_semantic_protection_authority(db,lease,raw,lambda ref,model:mask)
    frozen_sha=lease.command.params.sha256;lease.command.params.sha256='0'*64
    verify(params,image,image,params.intent,params.calibration)
    assert len(calls)==2 and all(value[0]==frozen_sha for value in calls)
    assert calls[0][1:3]==(params.intent,mask.artifact)
    changed=params.model_copy(update={'model_lock_sha':'0'*64})
    with pytest.raises(AdoptionInvalid,match='PARAMS_CHANGED'):
        verify(changed,image,image,params.intent,params.calibration)
    assert len(calls)==2
