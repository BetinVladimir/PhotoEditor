"""RAW option wiring tests; no claim of scientific camera qualification."""
from pathlib import Path
from types import SimpleNamespace
import sys
import numpy as np
import pytest
from pydantic import ValidationError
from photo_worker.contracts.assets import RAWDevelopment
from photo_worker.operations.asset.decode import decode_raw
from photo_worker.operations.asset.verify import VerifiedSource, AssetError

VERSION='rawpy-0.27.1/libraw-0.21.4'

def policy(**changes):
    return RAWDevelopment(demosaic='ahd',wb_source='camera',exposure_ev=0.0,
        highlight_policy='clip',decoder_version=VERSION,**changes)

@pytest.fixture
def decoder(monkeypatch):
    calls=[]
    class Raw:
        sizes=SimpleNamespace(raw_width=16,raw_height=12)
        camera_whitebalance=[2,1,1.5,1]
        entered=False
        closed=False
        def __enter__(self):self.entered=True;return self
        def __exit__(self,*args):self.closed=True
        def postprocess(self,**kwargs):
            calls.append(kwargs)
            return np.zeros((12,16,3),dtype=np.uint16)
    raw=Raw()
    module=SimpleNamespace(__version__='0.27.1',libraw_version=(0,21,4),
        ColorSpace=SimpleNamespace(sRGB='linear-srgb'),
        DemosaicAlgorithm=SimpleNamespace(AHD='ahd',DCB='dcb'),
        HighlightMode=SimpleNamespace(Clip=0,Blend=2,ReconstructDefault=5),
        LibRawError=type('LibRawError',(Exception,),{}),
        imread=lambda path:raw)
    monkeypatch.setitem(sys.modules,'rawpy',module)
    return raw,calls

def source():return VerifiedSource(Path('fixture.nef'),'raw',123,'a'*64)

def test_legacy_options_remain_unchanged(decoder):
    _,calls=decoder
    decode_raw(source())
    assert calls==[dict(use_camera_wb=True,use_auto_wb=False,no_auto_bright=True,
        gamma=(1,1),output_bps=16,output_color='linear-srgb',demosaic_algorithm='ahd')]

@pytest.mark.parametrize('wb', ['camera','daylight','auto'])
@pytest.mark.parametrize('highlight,mode', [('clip',0),('blend',2),('reconstruct',5)])
def test_explicit_raw_settings_are_forwarded(decoder,wb,highlight,mode):
    _,calls=decoder
    p=policy().model_copy(update=dict(wb_source=wb,demosaic='dcb',exposure_ev=2.0,highlight_policy=highlight))
    result=decode_raw(source(),p)
    assert result.dtype==np.uint16
    assert calls[0]['use_camera_wb']==(wb=='camera')
    assert calls[0]['use_auto_wb']==(wb=='auto')
    assert calls[0]['exp_shift']==4.0
    assert calls[0]['highlight_mode']==mode
    assert calls[0]['demosaic_algorithm']=='dcb'
    assert calls[0]['no_auto_bright'] is True
    assert calls[0]['exp_preserve_highlights']==0.0

def test_decoder_version_mismatch_refuses_before_processing(decoder):
    _,calls=decoder
    p=policy().model_copy(update={'decoder_version':'rawpy-0.27.0/libraw-0.21.4'})
    with pytest.raises(AssetError,match='RAW_DECODER_VERSION_MISMATCH'):decode_raw(source(),p)
    assert calls==[]

def test_missing_camera_wb_does_not_silently_use_daylight(decoder):
    raw,calls=decoder
    raw.camera_whitebalance=[0,0,0,0]
    with pytest.raises(AssetError,match='RAW_CAMERA_WB_UNAVAILABLE'):decode_raw(source(),policy())
    assert calls==[]

@pytest.mark.parametrize('change',[{'exposure_ev':float('nan')},{'exposure_ev':-2.1},
    {'exposure_ev':3.1},{'wb_source':'guess'},{'demosaic':'unknown'},
    {'decoder_version':'unlocked'},{'highlight_policy':'unknown'}])
def test_raw_policy_rejects_unsupported_parameters(change):
    values=policy().model_dump();values.update(change)
    with pytest.raises(ValidationError):RAWDevelopment.model_validate(values)

@pytest.mark.parametrize('change',[{'exposure_ev':float('nan')},{'exposure_ev':4},
    {'wb_source':'guess'},{'demosaic':'unknown'},{'highlight_policy':'unknown'}])
def test_unvalidated_model_copy_refused_before_native_processing(decoder,change):
    _,calls=decoder
    malformed=policy().model_copy(update=change)
    with pytest.raises(ValidationError):decode_raw(source(),malformed)
    assert calls==[]

@pytest.mark.parametrize('wb',[None,[],[1,1,1],[1,1,1,float('nan')],
    [1,1,1,-1],['x',1,1,0],[[1],[1],[1],[0]]])
def test_invalid_native_camera_wb_refused(decoder,wb):
    raw,calls=decoder;raw.camera_whitebalance=wb
    with pytest.raises(AssetError,match='RAW_CAMERA_WB_UNAVAILABLE'):decode_raw(source(),policy())
    assert calls==[]

def test_zero_fourth_camera_wb_channel_is_supported(decoder):
    raw,calls=decoder;raw.camera_whitebalance=[2,1,1.5,0]
    decode_raw(source(),policy());assert len(calls)==1

@pytest.mark.parametrize('rgb',[np.zeros((12,16,3),dtype=np.uint8),
    np.zeros((12,16),dtype=np.uint16),np.zeros((12,16,4),dtype=np.uint16),
    np.zeros((0,16,3),dtype=np.uint16),None])
@pytest.mark.parametrize('explicit',[False,True])
def test_native_output_contract_before_icc(decoder,rgb,explicit):
    raw,_=decoder;raw.postprocess=lambda **kwargs:rgb
    with pytest.raises(AssetError,match='RAW_DECODE_CONTRACT_ERROR'):
        decode_raw(source(),policy() if explicit else None)

@pytest.mark.parametrize('phase',[1,2,3,4,5])
def test_raw_cancellation_fences_native_calls_and_closes_handle(decoder,phase):
    raw,calls=decoder;checks=0
    def active():
        nonlocal checks
        checks+=1
        if checks==phase:raise RuntimeError('task cancelled')
    with pytest.raises(RuntimeError,match='task cancelled'):
        decode_raw(source(),policy(),check_active=active)
    assert len(calls)==(1 if phase==5 else 0)
    assert raw.entered==(phase>=3)
    assert raw.closed==(phase>=3)

@pytest.mark.parametrize('width,height,code',[(0,12,'RAW_DECODE_CONTRACT_ERROR'),
    (-1,12,'RAW_DECODE_CONTRACT_ERROR'),(True,12,'RAW_DECODE_CONTRACT_ERROR'),
    (16.0,12,'RAW_DECODE_CONTRACT_ERROR'),(None,12,'RAW_DECODE_CONTRACT_ERROR'),
    (np.int64(2**62),np.int64(4),'PIXEL_LIMIT_EXCEEDED')])
def test_native_dimensions_refused_before_processing(decoder,width,height,code):
    raw,calls=decoder;raw.sizes=SimpleNamespace(raw_width=width,raw_height=height)
    with pytest.raises(AssetError,match=code):decode_raw(source(),policy())
    assert calls==[] and raw.closed
