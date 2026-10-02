"""Bounded mask composition; geometry/provenance belong to artifact adapters."""
from collections.abc import Callable
import numpy as np

BLOCK_PIXELS = 16384


def compose(target: np.ndarray, exclude: np.ndarray | None = None,
            protected: np.ndarray | None = None, *, out: np.ndarray | None = None,
            check_active: Callable[[], None] = lambda: None) -> np.ndarray:
    """clamp(target)*(1-clamp(exclude))*(1-clamp(protected)).

    An explicit float32 output may be a scratch-owned memmap. Inputs are never
    mutated or aliased. Missing exclusion/protection means zero probability.
    Cancellation or invalid data may leave output partially written; callers
    must discard its scratch reservation, never publish it.
    """
    check_active()
    sources=[np.asarray(a) if a is not None else None for a in (target,exclude,protected)]
    target=sources[0]
    if target.ndim != 2 or min(target.shape)<1 or target.size>200000000:
        raise ValueError('Expected nonempty mask of at most200MP')
    for source in sources:
        if source is not None and (source.shape != target.shape or source.dtype.kind != 'f'):
            raise ValueError('Expected matching floating mask arrays')
    if out is None:
        out=np.empty(target.shape,dtype=np.float32)
    elif (out.shape != target.shape or out.dtype != np.float32 or not out.flags.writeable
          or not out.flags.c_contiguous or any(np.may_share_memory(out,s) for s in sources if s is not None)):
        raise ValueError('Output must be disjoint writable contiguous float32')
    contiguous=all(s is None or s.flags.c_contiguous for s in sources)
    if contiguous:
        views=[None if s is None else s.reshape(-1) for s in sources]
        destination=out.reshape(-1)
        chunks=((slice(start,start+BLOCK_PIXELS),) for start in range(0,target.size,BLOCK_PIXELS))
    else:
        views=sources;destination=out
        chunks=((row,slice(start,start+BLOCK_PIXELS)) for row in range(target.shape[0]) for start in range(0,target.shape[1],BLOCK_PIXELS))
    for chunk in chunks:
        check_active()
        value=None
        for index,source in enumerate(views):
            if source is None:continue
            block=source[chunk]
            if not np.isfinite(block).all():raise ValueError('Non-finite mask')
            bounded=np.clip(block,0,1).astype(np.float32)
            if index==0:value=bounded
            else:value*=1-bounded
        destination[chunk]=value
    check_active()
    return out
