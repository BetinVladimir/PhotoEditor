import numpy as np
import pytest
from photo_worker.algorithms.perspective_lines import detect_perspective_lines
from photo_worker.algorithms import perspective_lines as module

class Detector:
    def __init__(self,segments):self.segments=segments
    def detect(self,gray):
        assert gray.dtype==np.uint8 and max(gray.shape)<=1600
        return np.array(self.segments,dtype=np.float32).reshape(-1,1,4),None,None,None

SEGMENTS=[[10,20,180,20],[10,150,180,150],[20,10,20,180],[150,10,150,180]]
def test_normalized_owned_candidates_and_hdr_source_unchanged(monkeypatch):
    rgb=np.full((200,200,3),2.,dtype=np.float32);original=rgb.copy()
    monkeypatch.setattr(module.cv2,'createLineSegmentDetector',lambda *_:Detector(SEGMENTS))
    result=detect_perspective_lines(rgb)
    assert result is not None and len(result.lines)==4
    assert {v.family for v in result.lines}=={'horizontal','vertical'}
    assert all(0<=p.x<=1 and 0<=p.y<=1 for line in result.lines for p in [line.a,line.b])
    assert np.array_equal(rgb,original)

def test_single_family_and_transparency_refuse(monkeypatch):
    rgb=np.ones((200,200,3),dtype=np.float32)
    monkeypatch.setattr(module.cv2,'createLineSegmentDetector',lambda *_:Detector(SEGMENTS[:2]))
    assert detect_perspective_lines(rgb) is None
    monkeypatch.setattr(module.cv2,'createLineSegmentDetector',lambda *_:Detector(SEGMENTS))
    assert detect_perspective_lines(rgb,np.zeros((200,200),dtype=np.float32)) is None

def test_invalid_and_cancelled_inputs():
    rgb=np.zeros((32,32,3),dtype=np.float32)
    with pytest.raises(ValueError):detect_perspective_lines(rgb,max_edge=9999)
    with pytest.raises(ValueError):detect_perspective_lines(rgb.astype(np.float64))
    with pytest.raises(ValueError):detect_perspective_lines(rgb,np.full((32,32),2.,dtype=np.float32))
    class Stop(Exception):pass
    def stop():raise Stop()
    with pytest.raises(Stop):detect_perspective_lines(rgb,check_active=stop)


def test_global_cap_reserves_both_spatially_separated_families(monkeypatch):
    rgb=np.ones((200,200,3),dtype=np.float32)
    segments=[[1,y,198,y] for y in (10,40,70,100,130,160,190)]
    segments += [[20,60,20,140],[170,60,170,140]]
    monkeypatch.setattr(module.cv2,'createLineSegmentDetector',lambda *_:Detector(segments))
    result=detect_perspective_lines(rgb,max_lines=4)
    assert result is not None and len(result.lines)==4
    assert sum(line.family=='horizontal' for line in result.lines)==2
    assert sum(line.family=='vertical' for line in result.lines)==2


def test_crossing_directions_are_not_midpoint_duplicates(monkeypatch):
    rgb=np.ones((200,200,3),dtype=np.float32)
    segments=SEGMENTS+[[30,60,170,140],[30,140,170,60]]
    monkeypatch.setattr(module.cv2,'createLineSegmentDetector',lambda *_:Detector(segments))
    result=detect_perspective_lines(rgb)
    assert result is not None and len(result.lines)==6


def test_pathological_texture_returns_no_evidence(monkeypatch):
    rgb=np.ones((200,200,3),dtype=np.float32)
    monkeypatch.setattr(module.cv2,'createLineSegmentDetector',lambda *_:Detector(SEGMENTS*12501))
    assert detect_perspective_lines(rgb) is None


@pytest.mark.parametrize('dtype', [np.float32,np.uint16])
@pytest.mark.parametrize('offset', [(0,0),(1,1),(-1,-1)])
def test_narrow_transparent_gap_between_sparse_samples_refuses_line(monkeypatch,dtype,offset):
    rgb=np.ones((200,200,3),dtype=np.float32)
    alpha=np.full((200,200),1 if dtype==np.float32 else 65535,dtype=dtype)
    # x=13 lies between the former samples x=10 and x=15. Diagonal
    # offsets also belong to the full two-pixel transparency corridor.
    ox,oy=offset
    alpha[20+oy,13+ox]=0
    original=alpha.copy()
    monkeypatch.setattr(module.cv2,'createLineSegmentDetector',lambda *_:Detector(SEGMENTS))
    assert detect_perspective_lines(rgb,alpha) is None
    assert np.array_equal(alpha,original)


def test_outside_detector_segments_are_not_clamped_into_evidence(monkeypatch):
    rgb=np.ones((200,200,3),dtype=np.float32)
    segments=[[-100,20,180,20],*SEGMENTS[1:]]
    monkeypatch.setattr(module.cv2,'createLineSegmentDetector',lambda *_:Detector(segments))
    assert detect_perspective_lines(rgb) is None
