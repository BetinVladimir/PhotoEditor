"""Bounded verification of a proposal's physical uint16 label map."""
from dataclasses import dataclass
from collections.abc import Callable
import numpy as np
from photo_worker.contracts.defects import DefectCandidates

@dataclass(frozen=True)
class LabelEvidence:
    label:int
    pixels:int


def validate_labels(labels:np.ndarray,proposal:DefectCandidates,*,check_active:Callable[[],None]=lambda:None)->tuple[LabelEvidence,...]:
    """Verify exact candidate label set and minimal bounding rectangles.

    Artifact SHA must already be checked by the materialization layer. This
    verifies payload semantics without full-size index/binary arrays per label.
    """
    check_active();proposal.coherent()
    h,w=proposal.canvas.height,proposal.canvas.width
    if labels.shape!=(h,w) or labels.dtype!=np.uint16:raise ValueError('DEFECT_LABEL_LAYOUT_INVALID')
    candidates=sorted(proposal.candidates,key=lambda c:c.label);n=len(candidates)
    lookup=np.full(65536,-1,np.int32)
    for index,candidate in enumerate(candidates):lookup[candidate.label]=index
    counts=np.zeros(n,np.int64);xmin=np.full(n,w,np.int64);ymin=np.full(n,h,np.int64)
    xmax=np.full(n,-1,np.int64);ymax=np.full(n,-1,np.int64)
    if labels.flags.c_contiguous:
        flat=labels.reshape(-1)
        blocks=((flat[start:start+16384],start,None) for start in range(0,flat.size,16384))
    else:
        blocks=((labels[y,x:x+16384],x,y) for y in range(h) for x in range(0,w,16384))
    for block,start,row in blocks:
        check_active();active=block!=0
        if not np.any(active):continue
        positions=np.flatnonzero(active);indices=lookup[block[active]]
        if np.any(indices<0):raise ValueError('UNKNOWN_DEFECT_LABEL')
        absolute=positions+start
        xs=absolute%w if row is None else absolute
        ys=absolute//w if row is None else np.full(len(positions),row,np.int64)
        np.add.at(counts,indices,1)
        np.minimum.at(xmin,indices,xs);np.maximum.at(xmax,indices,xs)
        np.minimum.at(ymin,indices,ys);np.maximum.at(ymax,indices,ys)
    for index,candidate in enumerate(candidates):
        check_active();bbox=candidate.bbox
        if not counts[index]:raise ValueError('MISSING_DEFECT_LABEL')
        if (int(xmin[index]),int(ymin[index]),int(xmax[index]-xmin[index]+1),int(ymax[index]-ymin[index]+1))!=(bbox.x,bbox.y,bbox.width,bbox.height):
            raise ValueError('DEFECT_LABEL_BBOX_MISMATCH')
    check_active()
    return tuple(LabelEvidence(c.label,int(counts[i])) for i,c in enumerate(candidates))
