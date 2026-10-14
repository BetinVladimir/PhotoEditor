import hashlib
import json
import pytest
import numpy as np
from uuid import uuid4
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.portrait_open_eyes_stage import prepare_and_stage_open_eyes_patch
from test_portrait_donor_material import case
from test_portrait_donor_images import manifest_arguments
from test_portrait_local_result import Artifacts


def stage_case():
    params,raw,reader=case();args=manifest_arguments()
    proof=json.loads(args[3]);proof['source']=params.base.model_dump(mode='json')
    proof['donor']=params.donor.model_dump(mode='json');proof_raw=canonical_bytes(proof)
    params.confirmation.sha256=hashlib.sha256(proof_raw).hexdigest()
    faces=[]
    for body,binding,ref in zip(args[4:6],(params.base,params.donor),(params.source_faces,params.donor_faces)):
        doc=json.loads(body);doc['binding']=binding.model_dump(mode='json');body=canonical_bytes(doc)
        ref.sha256=hashlib.sha256(body).hexdigest();faces.append(body)
    return params,raw,reader,args,proof_raw,faces


def test_physical_rgb_to_six_role_staged_closure(tmp_path):
    params,raw,reader,args,proof_raw,faces=stage_case()
    root=tmp_path/'output';root.mkdir();material=tmp_path/'input';material.mkdir()
    artifacts=Artifacts();calls=[]
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        result=prepare_and_stage_open_eyes_patch(params,raw,raw,proof_raw,*faces,reader,
            args[8],args[9],artifacts,scratch,source_origin=(0,0),min_face_edge_px=40,
            material_scratch_root=material,material_max_bytes=1_000_000,
            check_active=lambda:None,authority=lambda p,r:calls.append(1) or True,
            alpha_mode='require_float32')
    assert len(calls)>=12 and len(result.outputs)==6 and result.review_required
    assert artifacts.roles['report'][1].status=='review'
    assert artifacts.roles['document'][1].generated_mask is None
    assert not list(material.iterdir())


def test_callback_mutation_cannot_rewrite_snapshotted_request_and_masks(tmp_path,monkeypatch):
    import photo_worker.v2.portrait_open_eyes_stage as module
    params,raw,reader,args,proof_raw,faces=stage_case()
    target=args[8].copy();guard=args[9].copy();expected=target.copy();seen=[]
    original_writer=module.write_donor_patch_files
    def writer(*a,**kw):
        np.testing.assert_array_equal(kw['target'],expected)
        np.testing.assert_array_equal(kw['protected'],0)
        seen.append(1)
        return original_writer(*a,**kw)
    monkeypatch.setattr(module,'write_donor_patch_files',writer)
    def authority(p,r):
        assert p.strength==1
        p.strength=0;params.strength=0;target[:]=0;guard[:]=1
        return True
    root=tmp_path/'output';root.mkdir();material=tmp_path/'input';material.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        result=prepare_and_stage_open_eyes_patch(params,raw,raw,proof_raw,*faces,reader,
            target,guard,Artifacts(),scratch,source_origin=(0,0),min_face_edge_px=40,
            material_scratch_root=material,material_max_bytes=1_000_000,
            check_active=lambda:None,authority=authority,alpha_mode='require_float32')
    assert seen==[1] and result.review_required


@pytest.mark.parametrize('value',[np.nan,-.1,1.1])
def test_bad_mask_rejects_before_external_callback(value):
    params,raw,reader,args,proof_raw,faces=stage_case();args[8][0,0]=value
    def forbidden(*a):
        pytest.fail('Invalid mask reached external callback')
    with pytest.raises(ValueError,match='DONOR_MASK'):
        prepare_and_stage_open_eyes_patch(params,raw,raw,proof_raw,*faces,None,
            args[8],args[9],None,None,source_origin=(0,0),min_face_edge_px=40,
            material_scratch_root=None,material_max_bytes=1_000_000,
            check_active=forbidden,authority=forbidden,alpha_mode='require_float32')


@pytest.mark.parametrize('deny_role',[None,'pixels','document'])
def test_revocation_before_or_during_staging_returns_no_result(tmp_path,deny_role):
    params,raw,reader,args,proof_raw,faces=stage_case()
    root=tmp_path/'output';root.mkdir();material=tmp_path/'input';material.mkdir()
    artifacts=Artifacts();calls=0
    def authority(p,r):
        nonlocal calls
        calls+=1
        return calls<5 if deny_role is None else deny_role not in artifacts.roles
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='DONOR_AUTHORITY'):
            prepare_and_stage_open_eyes_patch(params,raw,raw,proof_raw,*faces,reader,
                args[8],args[9],artifacts,scratch,source_origin=(0,0),min_face_edge_px=40,
                material_scratch_root=material,material_max_bytes=1_000_000,
                check_active=lambda:None,authority=authority,alpha_mode='require_float32')
    if deny_role is None:assert not artifacts.roles
    if deny_role=='pixels':assert set(artifacts.roles)=={'pixels'}
    assert not list(material.iterdir())
