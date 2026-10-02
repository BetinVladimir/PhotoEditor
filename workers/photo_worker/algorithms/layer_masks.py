"""Source-bound probability mask DAG compilation and bounded tile evaluation."""
from dataclasses import dataclass
from collections.abc import Callable
import re
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes

@dataclass(frozen=True)
class MaskNode:
    sha:str
    operation:str
    ref:ArtifactRef|None
    children:tuple[str,...]

@dataclass(frozen=True)
class MaskPlan:
    root:str
    nodes:tuple[MaskNode,...]


def compile_mask(expression:dict,canvas_hash:str)->MaskPlan:
    if not isinstance(canvas_hash,str) or not re.fullmatch('[0-9a-f]{64}',canvas_hash):
        raise ValueError('MASK_CANVAS_INVALID')
    nodes=[];seen=set();count=0
    def walk(value,depth):
        nonlocal count
        count+=1
        if depth>16 or count>256 or type(value) is not dict:raise ValueError('MASK_EXPRESSION_LIMIT')
        op=value.get('op')
        expected={'ref':{'op','mask','canvas_hash'},'invert':{'op','operand'},
                  'union':{'op','left','right'},'intersect':{'op','left','right'},'subtract':{'op','left','right'}}
        if type(op) is not str or op not in expected or set(value)!=expected[op]:raise ValueError('MASK_EXPRESSION_INVALID')
        ref=None;children=()
        if op=='ref':
            if value['canvas_hash']!=canvas_hash or type(value['mask']) is not dict:raise ValueError('MASK_CANVAS_INVALID')
            ref=ArtifactRef.model_validate_json(canonical_bytes(value['mask']))
        elif op=='invert':children=(walk(value['operand'],depth+1),)
        else:children=(walk(value['left'],depth+1),walk(value['right'],depth+1))
        digest=canonical_hash(value)
        if digest not in seen:
            seen.add(digest);nodes.append(MaskNode(digest,op,ref,children))
        return digest
    root=walk(expression,0)
    return MaskPlan(root,tuple(nodes))


def evaluate_mask_tile(plan:MaskPlan,read_tile:Callable,*,x:int,y:int,width:int,height:int,
                       max_tile_bytes:int,check_active:Callable=lambda:None)->np.ndarray:
    """Resolver must verify scoped MaskRef/NPY and transform to the pinned canvas.

    Inputs are borrowed, never mutated. The explicit budget includes retained
    DAG float32 buffers plus conservative validation temporaries. Output is owned.
    """
    if (any(type(v) is not int for v in (x,y,width,height,max_tile_bytes)) or
        min(x,y)<0 or min(width,height)<1 or max(width,height)>1024 or max_tile_bytes<1 or
        not isinstance(plan,MaskPlan) or not 1<=len(plan.nodes)<=256):raise ValueError('MASK_TILE_INVALID')
    cache={};size=width*height*4
    for node in plan.nodes:
        check_active()
        if node.sha in cache or any(child not in cache for child in node.children):raise ValueError('MASK_DAG_INVALID')
        if (len(cache)+3)*size>max_tile_bytes:raise ValueError('MASK_RAM_LIMIT')
        if node.operation=='ref' and node.ref is not None and not node.children:
            value=read_tile(node.ref,x,y,width,height)
            if not isinstance(value,np.ndarray) or value.dtype!=np.float32 or value.shape!=(height,width):raise ValueError('MASK_TILE_INVALID')
            check_active()
            if not np.isfinite(value).all() or np.any(value<0) or np.any(value>1):raise ValueError('MASK_PROBABILITY_INVALID')
            result=np.array(value,dtype=np.float32,copy=True)
        elif node.operation=='invert' and len(node.children)==1:
            result=np.subtract(np.float32(1),cache[node.children[0]])
        elif node.operation in ('union','intersect','subtract') and len(node.children)==2:
            left,right=(cache[key] for key in node.children)
            if node.operation=='union':result=np.maximum(left,right)
            elif node.operation=='intersect':result=np.minimum(left,right)
            else:
                result=np.subtract(left,right);np.maximum(result,np.float32(0),out=result)
        else:raise ValueError('MASK_DAG_INVALID')
        cache[node.sha]=result
    check_active()
    if plan.root not in cache:raise ValueError('MASK_DAG_INVALID')
    return cache[plan.root]


def mask_tiles(plan:MaskPlan,read_tile:Callable,*,width:int,height:int,tile_size:int,
               max_tile_bytes:int,check_active:Callable=lambda:None):
    """Yield owned tiles, including partial canvas edges, without a full frame."""
    if (type(width) is not int or type(height) is not int or min(width,height)<1 or
            width*height>200000000 or type(tile_size) is not int or tile_size not in (256,512,1024)):
        raise ValueError('MASK_CANVAS_INVALID')
    from photo_worker.algorithms.mask_transform import TransformedMaskTile
    for y in range(0,height,tile_size):
        for x in range(0,width,tile_size):
            check_active()
            value=evaluate_mask_tile(plan,read_tile,x=x,y=y,width=min(tile_size,width-x),height=min(tile_size,height-y),
                max_tile_bytes=max_tile_bytes,check_active=check_active)
            yield TransformedMaskTile(x,y,value)
