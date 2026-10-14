from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, Canvas, ColorSpace, TransformChain
from photo_worker.contracts.base import ArtifactRef as Ref
from photo_worker.v2 import perspective_lines_replay as module
from photo_worker.v2.perspective_lines_validation import VerifiedPerspectiveLines
from photo_worker.v2.adoption import AdoptionInvalid


def setup(monkeypatch,tmp_path):
    image=WorkingImage(pixels=Ref(artifact_id=uuid4(),sha256='a'*64),alpha=None,dtype='float32',
        canvas=Canvas(width=4,height=3,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    raw=canonical_bytes(image)
    base=SimpleNamespace(source=Ref(artifact_id=uuid4(),sha256=sha256(raw).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    verified=VerifiedPerspectiveLines(b'base',b'result',None,None)
    calls=[]
    class Lock:
        @staticmethod
        def model_validate_json(raw):return raw
    monkeypatch.setattr(module,'PerspectiveLinesRuntimeLock',Lock)
    monkeypatch.setattr(module,'verify_installed_runtime',lambda *args:calls.append('runtime'))
    monkeypatch.setattr(module,'verify_perspective_lines_result',lambda *args,**kwargs:verified)
    class Arrays:
        def __init__(self,*args):calls.append('array_session')
        def __enter__(self):return self
        def __exit__(self,*args):calls.append('closed')
        def array(self,*args,**kwargs):calls.append('rgb');return 'verified NPY'
    monkeypatch.setattr(module,'ArrayEvidenceSession',Arrays)
    monkeypatch.setattr(module,'prepare_detected_perspective_lines',lambda *args,**kwargs:SimpleNamespace(lines=None,lines_sha256=None))
    reader=SimpleNamespace(read_json=lambda *args:raw)
    kwargs=dict(expected_base=base,working_icc_sha='c'*64,runtime_lock={'schema_version':1},
        runtime_sha='d'*64,scratch_root=tmp_path,max_bytes=1<<30,ram_max_bytes=1<<30,check_active=lambda:None)
    return reader,kwargs,calls,verified


def test_unsupported_replay_rechecks_runtime_and_closes_arrays(monkeypatch,tmp_path):
    reader,kwargs,calls,verified=setup(monkeypatch,tmp_path)
    assert module.verify_pinned_perspective_lines_replay(reader,None,None,**kwargs)==verified
    assert calls==['runtime','array_session','rgb','closed','runtime']


def test_changed_detected_lines_refused_and_arrays_closed(monkeypatch,tmp_path):
    reader,kwargs,calls,_=setup(monkeypatch,tmp_path)
    monkeypatch.setattr(module,'prepare_detected_perspective_lines',lambda *args,**kwargs:SimpleNamespace(lines=b'changed',lines_sha256='a'*64))
    with pytest.raises(AdoptionInvalid,match='PIXEL_REPLAY_MISMATCH'):
        module.verify_pinned_perspective_lines_replay(reader,None,None,**kwargs)
    assert calls[-1]=='closed'


@pytest.mark.parametrize('field',['max_bytes','ram_max_bytes'])
def test_replay_budget_refused_before_array_io(monkeypatch,tmp_path,field):
    reader,kwargs,calls,_=setup(monkeypatch,tmp_path)
    kwargs[field]=1
    with pytest.raises(AdoptionInvalid,match='RESOURCE_EXCEEDED'):
        module.verify_pinned_perspective_lines_replay(reader,None,None,**kwargs)
    assert calls==['runtime']


def test_source_bytes_changed_before_array_io(monkeypatch,tmp_path):
    reader,kwargs,calls,_=setup(monkeypatch,tmp_path)
    kwargs['expected_base'].source.sha256='e'*64
    with pytest.raises(AdoptionInvalid,match='SOURCE_INVALID'):
        module.verify_pinned_perspective_lines_replay(reader,None,None,**kwargs)
    assert calls==['runtime']
