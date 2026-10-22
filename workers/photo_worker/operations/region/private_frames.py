"""Attempt-private pixel lineage; deliberately never an adopted ArtifactRef."""
from dataclasses import dataclass,replace
from hashlib import sha256
from uuid import UUID
from copy import deepcopy
import numpy as np
from photo_worker.contracts.base import canonical_hash


def pixel_digest(rgb,alpha,canvas,check_active):
    h,w=canvas.height,canvas.width
    if not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.shape!=(h,w,3):raise ValueError('REGION_PRIVATE_RGB_INVALID')
    if alpha is not None and (not isinstance(alpha,np.ndarray) or alpha.dtype!=np.float32 or alpha.shape!=(h,w)):
        raise ValueError('REGION_PRIVATE_ALPHA_INVALID')
    digest=sha256();digest.update(b'region_private_pixels_v1\x00')
    digest.update(canonical_hash(canvas).encode());digest.update(b'\x00alpha' if alpha is not None else b'\x00opaque')
    for values in (rgb,alpha):
        if values is None:continue
        for y in range(0,h,256):
            for x in range(0,w,256):
                check_active();tile=values[y:y+256,x:x+256]
                if not np.isfinite(tile).all() or values is alpha and (np.any(tile<0) or np.any(tile>1)):
                    raise ValueError('REGION_PRIVATE_VALUES_INVALID')
                digest.update(np.ascontiguousarray(tile,dtype='<f4').tobytes())
    check_active();return digest.hexdigest()


@dataclass(frozen=True)
class PrivateFrame:
    index: int
    pixels_sha: str
    canvas_sha: str
    chain_sha: str
    parent_sha: str | None
    policy_sha: str
    stage_task_id: UUID | None


class RegionPrivateFrames:
    """Owned by trusted installed native assembly, never a client callback.

    Original Params/source refs stay unchanged. A private frame proves only
    this attempt's measured native pixels and policy sequence. Publication and
    independent adopter qualification are separate mandatory boundaries.
    """
    def __init__(self,*,task_id,attempt,fence,recipe,source,rgb,alpha,execution,check_active):
        if (not isinstance(task_id,UUID) or task_id.int==0 or type(attempt) is not int or attempt<1
                or type(fence) is not int or fence<1 or execution is None
                or execution.source!=recipe.source.source or canonical_hash(source.canvas)!=execution.source_canvas_hash):
            raise ValueError('REGION_PRIVATE_ORIGIN_INVALID')
        self._owner=(task_id,attempt,fence);self._check=check_active
        self._recipe_sha=canonical_hash(recipe);self._execution_sha=canonical_hash(execution)
        self._stages=tuple(execution.stages);self._cursor=0;self._closed=False
        self._chain=source.transform_chain
        self._current=self._frame(rgb,alpha,source,policy_sha=self._recipe_sha,parent=None,task=None)
        self._arrays=(rgb,alpha,deepcopy(source))

    def _frame(self,rgb,alpha,image,*,policy_sha,parent,task):
        return PrivateFrame(self._cursor,pixel_digest(rgb,alpha,image.canvas,self._check),canonical_hash(image.canvas),
            canonical_hash(image.transform_chain),parent,policy_sha,task)

    @property
    def current(self):return self._current

    def require_current(self,rgb,alpha,image,*,owner):
        self._check()
        if self._closed or tuple(owner)!=self._owner:raise ValueError('REGION_PRIVATE_ATTEMPT_MISMATCH')
        if (canonical_hash(image.canvas)!=self._current.canvas_sha or canonical_hash(image.transform_chain)!=self._current.chain_sha
                or pixel_digest(rgb,alpha,image.canvas,self._check)!=self._current.pixels_sha):
            raise ValueError('REGION_PRIVATE_FRAME_CHANGED')
        return self._current

    def require_next(self,stage,rgb,alpha,image,*,owner,source):
        """Authorize fresh pixels without rewriting the adopted stage source.

        Original source is checked separately from measured private pixel state;
        the actual descriptor position cannot be skipped or replayed twice.
        """
        frame=self.require_current(rgb,alpha,image,owner=owner)
        if (self._cursor>=len(self._stages) or stage!=self._stages[self._cursor]
                or stage.source!=source or stage.source_canvas!=image.canvas):
            raise ValueError('REGION_PRIVATE_STAGE_INPUT_INVALID')
        return frame

    def advance(self,stage,rgb,alpha,image,*,input_frame,policy,owner):
        self._check()
        if self._closed or tuple(owner)!=self._owner or input_frame is not self._current:raise ValueError('REGION_PRIVATE_PARENT_MISMATCH')
        self.require_current(*self._arrays,owner=owner)
        if self._cursor>=len(self._stages) or stage!=self._stages[self._cursor]:raise ValueError('REGION_PRIVATE_STAGE_ORDER_INVALID')
        from photo_worker.contracts.images import TransformChain
        chain=self._chain if stage.transform is None else TransformChain.create([*self._chain.steps,stage.transform])
        if image.canvas!=stage.target_canvas or image.transform_chain!=chain or (alpha is not None)!=stage.target_has_alpha:
            raise ValueError('REGION_PRIVATE_STAGE_GEOMETRY_INVALID')
        parent=self._current.pixels_sha
        frame=replace(self._frame(rgb,alpha,image,policy_sha=canonical_hash(policy),parent=parent,task=stage.task_id),index=self._current.index+1)
        self._cursor+=1;self._chain=chain;self._current=frame;self._arrays=(rgb,alpha,deepcopy(image))
        return self._current

    def advance_retouch(self,rgb,alpha,image,*,input_frame,policy,owner):
        """Recipe-authorized new beauty phase; never a synthetic adopted task."""
        if self._closed or tuple(owner)!=self._owner or input_frame is not self._current:
            raise ValueError('REGION_PRIVATE_PARENT_MISMATCH')
        self.require_current(*self._arrays,owner=owner)
        if canonical_hash(image.canvas)!=self._current.canvas_sha or canonical_hash(image.transform_chain)!=self._current.chain_sha:
            raise ValueError('REGION_PRIVATE_STAGE_GEOMETRY_INVALID')
        self._current=replace(self._frame(rgb,alpha,image,policy_sha=canonical_hash(policy),
            parent=self._current.pixels_sha,task=None),index=self._current.index+1)
        self._arrays=(rgb,alpha,deepcopy(image));return self._current

    def finish(self,*,owner,recipe,execution):
        self._check()
        if (self._closed or tuple(owner)!=self._owner or self._cursor!=len(self._stages)
                or canonical_hash(recipe)!=self._recipe_sha or canonical_hash(execution)!=self._execution_sha):
            raise ValueError('REGION_PRIVATE_EXECUTION_INCOMPLETE')
        self.require_current(*self._arrays,owner=owner)
        self._closed=True;return self._current


@dataclass(frozen=True)
class PrivateStageInput:
    """In-process capability tied to one measured predecessor and descriptor slot."""
    ledger: RegionPrivateFrames
    owner: tuple
    stage: object
    source: object
    metadata: object
    frame: PrivateFrame

    def require(self,operation,source,rgb,alpha):
        if (operation!=self.stage.operation or source!=self.source
                or self.ledger.require_next(self.stage,rgb,alpha,self.metadata,
                    owner=self.owner,source=self.source) is not self.frame):
            raise ValueError('REGION_PRIVATE_STAGE_INPUT_INVALID')
        return self.frame


class RegionPrivateSequence:
    """Explicit native operator sequence; missing/extra stages cannot seal."""
    def __init__(self,*,owner,recipe,source,rgb,alpha,execution,verified,check_active,base_style=None,effective_style=None):
        self._base_style=base_style;self._effective_style=effective_style;self._retouch_recorded=False
        self.owner=tuple(owner);self.recipe=recipe;self.execution=execution
        self.verified=tuple(verified);self.position=0;self.metadata=source
        if tuple(entry[0] for entry in self.verified)!=tuple(execution.stages):
            raise ValueError('REGION_PRIVATE_VERIFIED_SEQUENCE_INVALID')
        self.ledger=RegionPrivateFrames(task_id=owner[0],attempt=owner[1],fence=owner[2],
            recipe=recipe,source=source,rgb=rgb,alpha=alpha,execution=execution,check_active=check_active)

    def _entry(self,operation,phase):
        matches=lambda entry:entry[0].operation==operation and (phase is None or entry[0].phase==phase)
        if self.position<len(self.verified) and matches(self.verified[self.position]):return self.verified[self.position]
        if any(matches(entry) for entry in self.verified[self.position:]):
            raise ValueError('REGION_PRIVATE_NATIVE_STAGE_ORDER_INVALID')
        if any(matches(entry) for entry in self.verified[:self.position]):
            raise ValueError('REGION_PRIVATE_NATIVE_STAGE_REPEATED')
        return None

    def input(self,operation,rgb,alpha,*,phase=None):
        entry=self._entry(operation,phase)
        if entry is None:return None
        stage,_,original,_,_=entry
        frame=self.ledger.require_next(stage,rgb,alpha,self.metadata,owner=self.owner,source=stage.source)
        if original.canvas!=self.metadata.canvas or original.transform_chain!=self.metadata.transform_chain:
            raise ValueError('REGION_PRIVATE_NATIVE_SOURCE_GEOMETRY_INVALID')
        return PrivateStageInput(self.ledger,self.owner,stage,stage.source,deepcopy(self.metadata),frame)

    def record(self,operation,rgb,alpha,*,phase=None):
        entry=self._entry(operation,phase)
        if entry is None:
            if operation=='portrait.beauty' and self._effective_style is not None and self._effective_style.beauty.enabled:
                if (self._base_style is None or self._base_style.beauty.enabled or self._retouch_recorded
                        or self.recipe.effective_delta.retouch_delta<=0 or self.position<1
                        or self.verified[self.position-1][0].operation not in ('image.color','image.local','sensor.dust_auto_repair','sensor.dust_repair')
                        or self.position<len(self.verified) and self.verified[self.position][0].operation in ('image.denoise','geometry.body','image.color','image.local','sensor.dust_auto_repair','sensor.dust_repair')):
                    raise ValueError('REGION_PRIVATE_RETOUCH_ORDER_INVALID')
                self.ledger.advance_retouch(rgb,alpha,self.metadata,input_frame=self.ledger.current,
                    policy={'effective_beauty':self._effective_style.beauty.model_dump(mode='json'),
                        'frozen_recipe_sha':canonical_hash(self.recipe)},owner=self.owner)
                self._retouch_recorded=True
            return
        stage,params,_,target,policy=entry
        self.ledger.advance(stage,rgb,alpha,target,input_frame=self.ledger.current,
            policy={'original_params':params,'effective_recipe_sha':canonical_hash(self.recipe)},owner=self.owner)
        self.position+=1;self.metadata=target

    def require_base(self,source_ref,rgb,alpha):
        if self.position<1 or self.verified[self.position-1][0].output!=source_ref:
            raise ValueError('REGION_PRIVATE_BASE_SOURCE_INVALID')
        return self.ledger.require_current(rgb,alpha,self.metadata,owner=self.owner)

    def finish(self):
        if (self._effective_style is not None and self._effective_style.beauty.enabled and self._base_style is not None
                and not self._base_style.beauty.enabled and not self._retouch_recorded):
            raise ValueError('REGION_PRIVATE_RETOUCH_INCOMPLETE')
        return self.ledger.finish(owner=self.owner,recipe=self.recipe,execution=self.execution)
