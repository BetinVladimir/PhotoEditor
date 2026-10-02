from uuid import uuid4
import numpy as np
import pytest
from photo_worker.algorithms.layer_masks import compile_mask,evaluate_mask_tile

CANVAS='a'*64

def leaf():return {'op':'ref','mask':{'artifact_id':str(uuid4()),'sha256':'b'*64},'canvas_hash':CANVAS}

def evaluate(plan,reader,**kwargs):return evaluate_mask_tile(plan,reader,x=0,y=0,width=2,height=1,max_tile_bytes=4096,**kwargs)

@pytest.mark.parametrize('op,expected',[('union',[.7,.8]),('intersect',[.2,.3]),('subtract',[0,.5])])
def test_probability_semantics_and_owned_output(op,expected):
    a,b=leaf(),leaf();plan=compile_mask({'op':op,'left':a,'right':b},CANVAS)
    values={a['mask']['artifact_id']:np.array([[.2,.8]],np.float32),b['mask']['artifact_id']:np.array([[.7,.3]],np.float32)}
    before={k:v.copy() for k,v in values.items()}
    result=evaluate(plan,lambda ref,*_:values[str(ref.artifact_id)])
    np.testing.assert_allclose(result,[expected],atol=1e-7,rtol=0)
    for key in values:np.testing.assert_array_equal(values[key],before[key])

def test_reuse_invert_and_budget_before_io():
    a=leaf();plan=compile_mask({'op':'union','left':a,'right':{'op':'invert','operand':a}},CANVAS)
    calls=[]
    def read(ref,*_):calls.append(ref);return np.array([[.2,.8]],np.float32)
    np.testing.assert_allclose(evaluate(plan,read),[[.8,.8]],atol=1e-7,rtol=0)
    assert len(calls)==1 and len(plan.nodes)==3
    calls.clear()
    with pytest.raises(ValueError,match='RAM_LIMIT'):evaluate_mask_tile(plan,read,x=0,y=0,width=2,height=1,max_tile_bytes=1)
    assert not calls

def test_canvas_depth_invalid_probability_and_cancel():
    a=leaf();wrong=dict(a,canvas_hash='c'*64)
    with pytest.raises(ValueError):compile_mask(wrong,CANVAS)
    deep=a
    for _ in range(17):deep={'op':'invert','operand':deep}
    with pytest.raises(ValueError):compile_mask(deep,CANVAS)
    plan=compile_mask(a,CANVAS)
    for values in [[[np.nan,0]],[[1.1,0]],[[-.1,0]]]:
        with pytest.raises(ValueError):evaluate(plan,lambda *_:np.array(values,np.float32))
    calls=[]
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):evaluate(plan,lambda *_:calls.append(1),check_active=cancel)
    assert not calls


def test_partial_tiles_and_no_full_canvas_allocation():
    from photo_worker.algorithms.layer_masks import mask_tiles
    a=leaf();plan=compile_mask({'op':'invert','operand':a},CANVAS)
    source=np.linspace(0,1,514,dtype=np.float32).reshape(2,257)
    reads=[]
    def read(ref,x,y,width,height):
        reads.append((x,y,width,height));return source[y:y+height,x:x+width]
    tiles=list(mask_tiles(plan,read,width=257,height=2,tile_size=256,max_tile_bytes=65536))
    assert reads==[(0,0,256,2),(256,0,1,2)]
    np.testing.assert_array_equal(np.concatenate([t.values for t in tiles],axis=1),np.float32(1)-source)
    assert all(not np.shares_memory(tile.values,source) for tile in tiles)
