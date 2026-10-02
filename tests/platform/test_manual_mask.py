import numpy as np
import pytest
from pydantic import ValidationError
from photo_worker.contracts.manual_mask import ManualMask,Stroke,Polygon,MaskPoint
from photo_worker.contracts.images import Canvas
from photo_worker.algorithms.manual_mask import rasterize


def point(x,y):return MaskPoint(x=float(x),y=float(y))
def document(shapes,w=601,h=301):
    return ManualMask(source_sha256='a'*64,canvas=Canvas(width=w,height=h,orientation=1,space_id='oriented:test',source_sha256='a'*64),shapes=shapes)
def collect(doc,tile):
    target=np.zeros((doc.canvas.height,doc.canvas.width),np.float32);protected=np.zeros_like(target)
    for t in rasterize(doc,'a'*64,tile_size=tile):
        h,w=t.target.shape;target[t.y:t.y+h,t.x:t.x+w]=t.target;protected[t.y:t.y+h,t.x:t.x+w]=t.protected
        assert not t.target.flags.writeable and not t.protected.flags.writeable
    return target,protected

def test_polygon_sequence_and_protection():
    full=Polygon(kind='polygon',sequence=0,mode='add',points=[point(0,0),point(1,0),point(1,1),point(0,1)])
    erase=Stroke(kind='stroke',sequence=1,mode='subtract',points=[point(.5,.5)],radius=.1,hardness=1.)
    protect=erase.model_copy(update={'sequence':2,'mode':'protect'})
    doc=document([protect,erase,full],w=100,h=100)
    target,protected=collect(doc,256)
    assert target[50,50]==0 and protected[50,50]==1
    assert target[0,0]==1 and protected[0,0]==0


def test_tiles_identical_and_soft_stroke_monotonic():
    stroke=Stroke(kind='stroke',sequence=0,mode='add',points=[point(.1,.2),point(.9,.8)],radius=.1,hardness=.4)
    polygon=Polygon(kind='polygon',sequence=1,mode='subtract',points=[point(.2,.1),point(.8,.7),point(.3,.9)])
    doc=document([stroke,polygon])
    a=collect(doc,256);b=collect(doc,512);c=collect(doc,1024)
    assert all(np.array_equal(x,y) for x,y in zip(a,b)) and all(np.array_equal(x,y) for x,y in zip(a,c))
    assert np.any((a[0]>0)&(a[0]<1))


def test_stale_duplicate_and_cancel():
    stroke=Stroke(kind='stroke',sequence=0,mode='add',points=[point(.5,.5)],radius=.1,hardness=1.)
    with pytest.raises(ValidationError):document([stroke,stroke])
    with pytest.raises(ValueError,match='OVERRIDE_SOURCE_MISMATCH'):list(rasterize(document([]),'b'*64))
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):next(rasterize(document([stroke]),'a'*64,check_active=cancel))
