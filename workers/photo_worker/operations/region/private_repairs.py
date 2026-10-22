"""Fresh conservative defect inventory, with no fabricated artifact references."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from hashlib import sha256
from types import SimpleNamespace
import math
from photo_worker.contracts.base import canonical_bytes
from photo_worker.algorithms.conservative import select_defect_regions
from photo_worker.operations.defects.propose_prepare import prepare_proposals
from photo_worker.operations.defects.prepare import prepare_repair
from photo_worker.runtime.arrays import read_array


@dataclass(frozen=True)
class PrivateDefect:
    id: str
    label: int
    bbox: object
    diameter: float
    score: float


@dataclass(frozen=True)
class PrivateDefectInventory:
    canvas: object
    frame_sha: str
    candidates: tuple

    def coherent(self):
        if (len(self.frame_sha)!=64 or any(c not in '0123456789abcdef' for c in self.frame_sha)
                or len(self.candidates)>4096):raise ValueError('REGION_PRIVATE_DEFECT_INVENTORY_INVALID')
        labels=set();ids=set()
        for candidate in self.candidates:
            b=candidate.bbox
            if (type(candidate.label) is not int or not 1<=candidate.label<=65535
                    or candidate.label in labels or candidate.id in ids
                    or not math.isfinite(candidate.score) or not 0<=candidate.score<=1
                    or not math.isfinite(candidate.diameter) or candidate.diameter<=0
                    or min(b.x,b.y)<0 or min(b.width,b.height)<1
                    or b.x+b.width>self.canvas.width or b.y+b.height>self.canvas.height):
                raise ValueError('REGION_PRIVATE_DEFECT_INVENTORY_INVALID')
            payload={'frame_sha':self.frame_sha,'label':candidate.label,'bbox':b.model_dump(mode='json'),
                'diameter':candidate.diameter,'score':candidate.score}
            if candidate.id!='region-private:'+sha256(canonical_bytes(payload)).hexdigest():
                raise ValueError('REGION_PRIVATE_DEFECT_ID_INVALID')
            labels.add(candidate.label);ids.add(candidate.id)
        return self


def private_inventory(canvas,frame_sha,candidates):
    def identifier(candidate):
        payload={'frame_sha':frame_sha,'label':candidate.label,'bbox':candidate.bbox.model_dump(mode='json'),
            'diameter':candidate.diameter,'score':candidate.score}
        return 'region-private:'+sha256(canonical_bytes(payload)).hexdigest()
    value=PrivateDefectInventory(canvas,frame_sha,tuple(PrivateDefect(identifier(c),c.label,c.bbox,c.diameter,c.score) for c in candidates))
    return value.coherent()


@contextmanager
def prepare_private_defects(rgb,alpha,image,faces,protections,scratch,*,ledger,owner,stage,
                            original_source,strength,ram_bytes,check_active):
    """Recompute DoG and all strict thresholds on this attempt's fresh frame.

    Authenticated original authored/permanent masks are passed by the installed
    assembly; fresh Face19/landmarks provide feature protection. The native
    metadata view deliberately has no pixels/image ArtifactRef. No review or
    DefectCandidates document is manufactured, persisted or relabeled here.
    """
    frame=ledger.require_next(stage,rgb,alpha,image,owner=owner,source=original_source)
    h,w=image.canvas.height,image.canvas.width
    if stage.operation!='defects.auto_repair' or type(ram_bytes) is not int or ram_bytes<max(1<<30,164*h*w+256*1048576):
        raise ValueError('REGION_PRIVATE_DEFECT_RESOURCE_INVALID')
    metadata=SimpleNamespace(canvas=image.canvas,transform_chain=image.transform_chain,dtype='float32',color=image.color)
    with ExitStack() as scopes:
        proposal=scopes.enter_context(prepare_proposals(rgb,metadata,faces,scratch,protections=protections,
            max_roi_pixels=min(4194304,ram_bytes//256),check_active=check_active))
        labels=scopes.enter_context(read_array(proposal.labels,(h,w),'uint16',check_active))
        protected=scopes.enter_context(read_array(proposal.protection,(h,w),'float32',check_active,unit_interval=True))
        inventory=private_inventory(image.canvas,frame.pixels_sha,proposal.candidates)
        selected=select_defect_regions(inventory,rgb,labels,protected,check_active=check_active)
        repair=scopes.enter_context(prepare_repair(rgb,labels,protected,inventory,selected,scratch,strength=strength,
            max_roi_pixels=min(4194304,ram_bytes//256),check_active=check_active))
        output=scopes.enter_context(read_array(repair.image,(h,w,3),'float32',check_active,unit_interval=True))
        ledger.require_next(stage,rgb,alpha,image,owner=owner,source=original_source)
        check_active();yield output,inventory,repair.candidates;check_active()
