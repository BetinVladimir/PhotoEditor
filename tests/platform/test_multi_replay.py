from types import SimpleNamespace as NS
import numpy as np
import pytest
from photo_worker.v2.multi_replay import compare_multi_prepared
from photo_worker.v2.adoption import AdoptionInvalid


def fixture(tmp_path):
    rgb=np.full((7,9,3),2.,np.float32);alpha=np.ones((7,9),np.float32);mask=np.zeros((7,9),np.float32)
    paths=[]
    for name,value in [('rgb',rgb),('alpha',alpha),('mask',mask)]:
        path=tmp_path/(name+'.npy');np.save(path,value);paths.append(path)
    matrix=np.eye(3)
    plan=NS(shape=(7,9),registrations=[NS(source_to_output=matrix)]*2,result=NS(motion_fraction=0.))
    prepared=NS(automatic=NS(prepared=plan,registration_inconsistent=False),projections=(),
        encoded=NS(rgb=paths[0],alpha=paths[1],changed_mask=paths[2]))
    metadata=NS(image=NS(canvas=NS(height=7,width=9)),registrations=[NS(homography=matrix.reshape(-1).tolist()) for _ in range(2)],
        motion_fraction=0.,registration_inconsistent=False,report=NS(clipping_fraction=1.))
    return prepared,metadata,rgb,alpha,mask


def test_exact_arrays_and_registrations(tmp_path):
    prepared,metadata,rgb,alpha,mask=fixture(tmp_path);checks=[]
    compare_multi_prepared(prepared,metadata,rgb,alpha,mask,[],check_active=lambda:checks.append(True))
    assert len(checks)>=6


@pytest.mark.parametrize('change',['rgb','alpha','mask','registration','motion','cycle','field_count','clipping'])
def test_replay_refuses_independent_tampering(tmp_path,change):
    prepared,metadata,rgb,alpha,mask=fixture(tmp_path);fields=[]
    if change=='rgb':rgb[3,4,1]=np.nextafter(rgb[3,4,1],np.float32(3))
    if change=='alpha':alpha[2,2]=.5
    if change=='mask':mask[2,2]=np.float32(-0.) # bitwise comparison preserves signed-zero proof
    if change=='registration':metadata.registrations[1].homography[2]=.1
    if change=='motion':metadata.motion_fraction=.1
    if change=='cycle':metadata.registration_inconsistent=True
    if change=='field_count':fields=[np.zeros((7,9,2),np.float32)]
    if change=='clipping':metadata.report.clipping_fraction=0.
    with pytest.raises(AdoptionInvalid):
        compare_multi_prepared(prepared,metadata,rgb,alpha,mask,fields,check_active=lambda:None)


def test_cylindrical_field_bytes_are_replayed(tmp_path):
    prepared,metadata,rgb,alpha,mask=fixture(tmp_path)
    field=np.zeros((7,9,2),np.float32);path=tmp_path/'field.npy';np.save(path,field)
    prepared.projections=(NS(inverse_field=path),)
    compare_multi_prepared(prepared,metadata,rgb,alpha,mask,[field],check_active=lambda:None)
    field[1,2,0]=1.
    with pytest.raises(AdoptionInvalid,match='PIXEL_REPLAY_MISMATCH'):
        compare_multi_prepared(prepared,metadata,rgb,alpha,mask,[field],check_active=lambda:None)


def test_qualified_replay_checks_runtime_and_live_grants_around_work(monkeypatch):
    from test_multi_admission import installed
    import photo_worker.v2.multi_replay as module
    admission,ctx,params,images,records,calls,_=installed(monkeypatch)
    metadata=NS(params=params,sources=tuple(images))
    monkeypatch.setattr(module,'verify_multi_result_metadata',lambda *args,**kwargs:metadata)
    def replay(*args,**kwargs):
        assert len(calls)==2
        assert kwargs['operation']=='optical.panorama'
        assert kwargs['model_lock_sha']==admission.pin.model_lock_sha
        return metadata
    monkeypatch.setattr(module,'verify_multi_pixel_replay',replay)
    assert module.verify_admitted_multi_replay(None,None,None,ctx=ctx,admission=admission,
        scratch_root=None,max_bytes=1000,ram_max_bytes=1000) is metadata
    assert len(calls)==4


def test_qualification_revoked_during_replay_cannot_be_accepted(monkeypatch):
    from dataclasses import replace
    from test_multi_admission import installed
    from photo_worker.runtime.executor import OperationFailure
    import photo_worker.v2.multi_replay as module
    admission,ctx,params,images,records,calls,_=installed(monkeypatch)
    metadata=NS(params=params,sources=tuple(images))
    monkeypatch.setattr(module,'verify_multi_result_metadata',lambda *args,**kwargs:metadata)
    def replay(*args,**kwargs):
        records[0]=replace(records[0],params_jcs=b'{}')
        return metadata
    monkeypatch.setattr(module,'verify_multi_pixel_replay',replay)
    with pytest.raises(OperationFailure,match='SCOPE_STALE'):
        module.verify_admitted_multi_replay(None,None,None,ctx=ctx,admission=admission,
            scratch_root=None,max_bytes=1000,ram_max_bytes=1000)
