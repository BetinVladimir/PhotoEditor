import numpy as np
import pytest
import json
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash
import photo_worker.v2.portrait_open_eyes_manual_stage as module
from test_portrait_donor_manual import manual_case
from test_portrait_donor_images import manifest_arguments
from photo_worker.operations.portrait.donor_resources import donor_portrait_resources
from photo_worker.runtime.executor import OperationFailure
from test_portrait_donor_runtime_lock import current_runtime


def run(tmp_path,producer_authority,authority,*,origin=(0,0),size=(100,100),profile=None):
    args,reader=manual_case()
    params,source,target,guard,root_ref,root,producer_ref,producer=args[:8]
    lock=current_runtime();runtime_sha=canonical_hash(lock)
    configured=profile if profile is not None else {'backend':'cpu','renderer_version':'v2.2',
        'resource_estimate':donor_portrait_resources(100,100,100,100,100,100)}
    configured=dict(configured);configured.setdefault('portrait_runtime_sha',runtime_sha)
    evidence=manifest_arguments();proof=json.loads(evidence[3])
    proof['source']=params.base.model_dump(mode='json');proof['donor']=params.donor.model_dump(mode='json')
    proof_raw=canonical_bytes(proof);params.confirmation.sha256=sha256(proof_raw).hexdigest()
    faces=[]
    for body,binding,pin in zip(evidence[4:6],(params.base,params.donor),(params.source_faces,params.donor_faces)):
        doc=json.loads(body);doc['binding']=binding.model_dump(mode='json');body=canonical_bytes(doc)
        pin.sha256=sha256(body).hexdigest();faces.append(body)
    return module.prepare_and_stage_manual_open_eyes(params,source,source,proof_raw,*faces,
        target,guard,root_ref,root,producer_ref,producer,reader,None,None,
        source_origin=origin,patch_size=size,min_face_edge_px=40,
        mask_scratch_root=tmp_path,mask_max_bytes=1_000_000,material_scratch_root=tmp_path,
        material_max_bytes=1_000_000,check_active=lambda:None,authority=authority,
        producer_authority=producer_authority,alpha_mode='require_float32',
        profile=configured,runtime_lock=lock,expected_runtime_sha=runtime_sha)


def test_manual_replay_and_owned_masks_reach_coordinator(tmp_path,monkeypatch):
    calls=[]
    def coordinator(*args,**kwargs):
        np.testing.assert_array_equal(args[7],1)
        np.testing.assert_array_equal(args[8],0)
        assert not args[7].flags.writeable
        assert kwargs['authority'](args[0],args[3]) is True
        calls.append(1)
        return 'staged'
    monkeypatch.setattr(module,'prepare_and_stage_open_eyes_patch',coordinator)
    assert run(tmp_path,lambda p,r,q:True,lambda p,r:True)=='staged'
    assert calls==[1] and not list(tmp_path.iterdir())


@pytest.mark.parametrize('gate',['donor','producer'])
def test_initial_authority_denial_prevents_mask_io_and_staging(tmp_path,monkeypatch,gate):
    def forbidden(*a,**kw):pytest.fail('Denied authority reached coordinator')
    monkeypatch.setattr(module,'prepare_and_stage_open_eyes_patch',forbidden)
    with pytest.raises(ValueError,match='DONOR_AUTHORITY'):
        run(tmp_path,lambda p,r,q:gate!='producer',lambda p,r:gate!='donor')
    assert not list(tmp_path.iterdir())


def test_auto_plan_uses_verified_face_and_full_mask_support(tmp_path,monkeypatch):
    def coordinator(*args,**kwargs):
        assert kwargs['source_origin']==(0,0) and args[7].shape==(100,100)
        return 'auto'
    monkeypatch.setattr(module,'prepare_and_stage_open_eyes_patch',coordinator)
    assert run(tmp_path,lambda p,r,q:True,lambda p,r:True,origin=None,size=None)=='auto'


def test_truncated_halo_rejects_before_rgb_coordinator(tmp_path,monkeypatch):
    def forbidden(*a,**kw):pytest.fail('Truncated mask reached RGB coordinator')
    monkeypatch.setattr(module,'prepare_and_stage_open_eyes_patch',forbidden)
    with pytest.raises(ValueError,match='DONOR_MASK_PATCH_HALO'):
        run(tmp_path,lambda p,r,q:True,lambda p,r:True,size=(99,100))
    assert not list(tmp_path.iterdir())


def test_ram_admission_denies_before_rgb_processing(tmp_path,monkeypatch):
    def forbidden(*a,**kw):pytest.fail('Underfunded operation reached RGB processing')
    monkeypatch.setattr(module,'prepare_and_stage_open_eyes_patch',forbidden)
    resources=donor_portrait_resources(100,100,100,100,100,100);resources['ram_bytes']-=1
    with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):
        run(tmp_path,lambda p,r,q:True,lambda p,r:True,
            profile={'backend':'cpu','renderer_version':'v2.2','resource_estimate':resources})
    assert not list(tmp_path.iterdir())


def test_profile_runtime_mismatch_denied_before_authority(tmp_path):
    def forbidden(*a):pytest.fail('Mismatched runtime reached authority')
    profile={'backend':'cpu','renderer_version':'v2.2','portrait_runtime_sha':'0'*64,
             'resource_estimate':donor_portrait_resources(100,100,100,100,100,100)}
    with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
        run(tmp_path,forbidden,forbidden,profile=profile)
    assert not list(tmp_path.iterdir())


def test_late_runtime_change_denies_result_after_coordinator(tmp_path,monkeypatch):
    actual=module.verify_installed_runtime;calls=[]
    def verify(lock,digest):
        calls.append(1)
        if len(calls)==2:raise OperationFailure('RUNTIME_BYTES_MISMATCH')
        return actual(lock,digest)
    monkeypatch.setattr(module,'verify_installed_runtime',verify)
    monkeypatch.setattr(module,'prepare_and_stage_open_eyes_patch',lambda *a,**kw:'staged')
    with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):
        run(tmp_path,lambda p,r,q:True,lambda p,r:True)
    assert len(calls)==2 and not list(tmp_path.iterdir())


@pytest.mark.parametrize('kind',['aggregate','missing_output','bool_output'])
def test_full_scratch_estimate_rejected_before_authority_and_mask_io(tmp_path,kind):
    resources=donor_portrait_resources(100,100,100,100,100,100)
    if kind=='aggregate':
        resources['scratch_bytes']=sum(resources[k] for k in
            ('mask_scratch_bytes','material_scratch_bytes','output_scratch_bytes'))-1
    elif kind=='missing_output':del resources['output_scratch_bytes']
    else:resources['output_scratch_bytes']=True
    def forbidden(*args):pytest.fail('Invalid aggregate budget reached authority')
    with pytest.raises(OperationFailure,match='SCRATCH_ADMISSION_EXCEEDED' if kind=='aggregate' else 'CONFIG_INVALID'):
        run(tmp_path,forbidden,forbidden,profile={'backend':'cpu','renderer_version':'v2.2','resource_estimate':resources})
    assert not list(tmp_path.iterdir())
