from pathlib import Path
import json
import struct
import numpy as np
import pytest
from photo_worker.color.icc import ICCError, ICCRegistry, LittleCMS, validate_icc

ROOT = Path(__file__).resolve().parents[2]

@pytest.fixture(scope='module')
def engine():
    return LittleCMS()

@pytest.fixture(scope='module')
def profiles():
    registry = ICCRegistry(ROOT/'resources/icc')
    return {key:registry.resolve(key).data for key in ('work_rec2020_gamma24_v1','srgb_v1','linear_srgb_v1')}

def test_work_profile_trc_and_reproducibility(engine, profiles):
    data = profiles['work_rec2020_gamma24_v1']
    assert engine.generate('work_rec2020_gamma24_v1') == data
    tags = {signature:(offset,length) for signature,offset,length in
            (struct.unpack_from('>4sII',data,132+i*12) for i in range(int.from_bytes(data[128:132],'big')))}
    offset,_ = tags[b'rTRC']
    assert data[offset:offset+4] == b'para'
    gamma = struct.unpack_from('>i',data,offset+12)[0]/65536
    assert abs(gamma-2.4) <= 1/65536
    assert data != profiles['srgb_v1']

def test_gray_gamma_and_roundtrip(engine, profiles):
    linear = np.repeat(np.array([[0, .01, .18, .5, 1]], dtype=np.float64)[...,None],3,axis=-1)
    encoded = engine.convert(np.round(linear*65535).astype(np.uint16),profiles['linear_srgb_v1'],profiles['work_rec2020_gamma24_v1'])/65535
    expected = (np.round(linear*65535)/65535)**(1/2.4)
    assert np.max(np.abs(encoded-expected)) < 4/65535
    rgb = np.random.default_rng(12).integers(0,65536,size=(64,64,3),dtype=np.uint16)
    working = engine.convert(rgb,profiles['srgb_v1'],profiles['work_rec2020_gamma24_v1'])
    back = engine.convert(working,profiles['work_rec2020_gamma24_v1'],profiles['srgb_v1'])
    error = np.abs(back.astype(np.int32)-rgb.astype(np.int32))
    assert np.percentile(error,99) <= 8
    assert error.max() <= 20

def test_identity_is_exact(engine,profiles):
    rgb = np.random.default_rng(9).integers(0,65536,size=(32,32,3),dtype=np.uint16)
    assert np.array_equal(rgb,engine.convert(rgb,profiles['work_rec2020_gamma24_v1'],profiles['work_rec2020_gamma24_v1']))

def test_reject_invalid_profile_and_hash(profiles,tmp_path):
    for raw in (b'not-icc',profiles['srgb_v1'][:-1]):
        with pytest.raises(ICCError):validate_icc(raw)
    source = ROOT/'resources/icc'
    manifest = json.loads((source/'profiles.lock.json').read_text())
    (tmp_path/'profiles.lock.json').write_text(json.dumps(manifest))
    entry = manifest['profiles'][0]
    (tmp_path/entry['path']).write_bytes(b'changed')
    with pytest.raises(ICCError,match='HASH_MISMATCH'):
        ICCRegistry(tmp_path).resolve(entry['id'])


def test_identical_profile_keeps_all_codes_and_owned_noncontiguous_input(engine,profiles):
    values=np.arange(65536,dtype=np.uint16)
    rgb=np.repeat(values[:,None,None],3,axis=2)[::-1]
    assert not rgb.flags.c_contiguous
    result=engine.convert(rgb,profiles['work_rec2020_gamma24_v1'],profiles['work_rec2020_gamma24_v1'],preserve_identity=True)
    assert result.flags.c_contiguous and not np.shares_memory(result,rgb)
    assert np.array_equal(result,rgb)
    result[0,0,0]=0
    assert rgb[0,0,0]==65535


def test_identical_invalid_profile_still_refused(engine):
    with pytest.raises(ICCError):
        engine.convert(np.zeros((1,1,3),np.uint16),b'invalid',b'invalid',preserve_identity=True)


@pytest.mark.parametrize('identity',[None,False,True])
def test_identity_copy_is_explicit_optin_default_keeps_real_lcms_transform(engine,profiles,monkeypatch,identity):
    original=engine.lib.cmsCreateTransform;calls=[]
    def transform(*args):
        calls.append(args)
        return original(*args)
    monkeypatch.setattr(engine.lib,'cmsCreateTransform',transform)
    rgb=np.random.default_rng(19).integers(0,65536,(8,9,3),dtype=np.uint16)
    kwargs={} if identity is None else {'preserve_identity':identity}
    result=engine.convert(rgb,profiles['work_rec2020_gamma24_v1'],profiles['work_rec2020_gamma24_v1'],**kwargs)
    assert len(calls)==(0 if identity is True else 1)
    assert np.array_equal(result,rgb)
