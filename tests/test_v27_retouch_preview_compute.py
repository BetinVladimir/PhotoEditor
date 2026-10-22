"""Numeric adapter boundaries with an isolated existing-beauty stage double."""
from contextlib import contextmanager
from types import SimpleNamespace
from dataclasses import replace
from photo_worker.operations.portrait.prepare import FaceEvidence
from uuid import UUID,uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import parse_json
from photo_worker.contracts.images import WorkingImage,Face68
from photo_worker.operations.semantic import preview_retouch_compute as module
from test_v27_retouch_preview_adapter_binding import fixture
from test_v27_exposure_geometry import geometry_fixture


@pytest.mark.parametrize('mode',[None,'outside','protected','hdr','budget','admission','nil_face','alpha_mismatch','manual_canvas','strength_bool',
    'missing_reports','duplicate_reports','wrong_reports','nan_output','source_document','source_color','mutate_rgb','mutate_masks','rgb_dtype','mask_dtype','mask_shape','protected_dtype','report_bool','report_reason','report_missing_reason','report_nan','report_clipping','zero_strength_changed'])
def test_retouch_compute_scales_once_and_independently_checks_preservation(tmp_path,monkeypatch,mode):
    params,_,_,_,raw,_=fixture();context=parse_json(raw)
    edit,_,_,source_raw,*_=geometry_fixture();source=parse_json(source_raw)
    params['source']=edit['source'];context['source']=edit['source']
    image=WorkingImage.model_validate_json(source_raw)
    face=Face68.model_validate(dict(object_id=uuid4(),points=[dict(x=.5,y=.5,confidence=.9) for _ in range(68)],
        canvas=image.canvas,transform_chain=image.transform_chain))
    if mode=='nil_face':face.object_id=UUID(int=0)
    if mode=='source_document':image.pixels.sha256='0'*64
    if mode=='source_color':image.color.icc_sha256='0'*64
    if mode=='manual_canvas':context['faces'][0]['manual']['canvas']['source_sha256']='0'*64
    rgb=np.full((8,8,3),.5,dtype=np.float32)
    if mode=='hdr':rgb[3,3,0]=2.
    mask=np.ones((8,8),dtype=np.float32);mask[:,0]=0
    protected=np.zeros((8,8),dtype=np.float32);protected[0,1]=.1
    if mode=='rgb_dtype':rgb=rgb.astype(np.float64)
    if mode=='mask_dtype':mask=mask.astype(np.float64)
    if mode=='mask_shape':mask=mask[:7]
    if mode=='protected_dtype':protected=protected.astype(np.int32)
    calls=[]
    @contextmanager
    def beauty(source,image,faces,effective,scratch,**kwargs):
        calls.append(True)
        if mode in ('mutate_rgb','mutate_masks'):
            assert source[3,3,0]==np.float32(.5) and faces[0].manual[3,3]==1
        assert effective.smoothing==(0 if mode=='zero_strength_changed' else .25)
        assert effective.tone_evenness==(0 if mode=='zero_strength_changed' else .1) and effective.texture_keep==.8
        assert not source.flags.writeable and kwargs['permanent'][0,1]==1 and kwargs['permanent'][0,0]==1
        output=source.copy();output[3,3]=.6
        if mode=='outside':output[0,0]=.6
        if mode=='protected':output[0,1]=.6
        if mode=='nan_output':output[3,3]=np.nan
        reports=(FaceEvidence(faces[0].face.object_id,True,None,1.,0.,0.),)
        if mode=='missing_reports':reports=()
        if mode=='duplicate_reports':reports=reports*2
        if mode=='wrong_reports':reports=(FaceEvidence(uuid4(),True,None),)
        if mode=='report_bool':reports=(replace(reports[0],applied=1),)
        if mode=='report_reason':reports=(replace(reports[0],reason='empty_skin'),)
        if mode=='report_missing_reason':reports=(replace(reports[0],applied=False),)
        if mode=='report_nan':reports=(replace(reports[0],texture_energy_ratio=float('nan')),)
        if mode=='report_clipping':reports=(replace(reports[0],above_one=1.1),)
        path=tmp_path/'retouch.npy';np.save(path,output,allow_pickle=False)
        try:yield SimpleNamespace(image=path,faces=reports)
        finally:path.unlink(missing_ok=True)
    monkeypatch.setattr(module,'prepare_beauty',beauty)
    mutated=False
    def admission(*args):
        nonlocal mutated
        if not mutated:
            mutated=True
            if mode=='mutate_rgb':rgb[:]=np.nan
            if mode=='mutate_masks':
                mask[:]=np.nan;protected[:]=np.nan
        return mode!='admission'
    def run():
        with module.compute_retouch_preview(params,context,image,rgb,mask,protected,(face,),object(),
            strength=True if mode=='strength_bool' else 0. if mode=='zero_strength_changed' else .5,alpha=np.ones((8,8),dtype=np.float32) if mode=='alpha_mismatch' else None,
            max_ram_bytes=1 if mode=='budget' else 2**31,
            verify_admission=admission,check_active=lambda:None) as result:
            assert result.preservation.changed_pixels==1 and result.preservation.alpha_unchanged
            np.testing.assert_array_equal(result.pixels[0,0],np.full(3,.5,np.float32))
    if mode in (None,'mutate_rgb','mutate_masks'):run()
    else:
        with pytest.raises(ValueError):run()
    assert len(calls)==(0 if mode in ('hdr','budget','admission','nil_face','alpha_mismatch','manual_canvas','strength_bool','source_document','source_color','rgb_dtype','mask_dtype','mask_shape','protected_dtype') else 1)
    assert not (tmp_path/'retouch.npy').exists()
