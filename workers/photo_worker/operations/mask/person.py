"""Exact torchvision LR-ASPP person probability adapter with guided full-size refinement."""
from __future__ import annotations
from contextlib import ExitStack
from typing import Protocol
import cv2
import numpy as np

from photo_worker.algorithms.mask_png import png16_bound,write_png16
from photo_worker.algorithms.mask_refine import refine_tiles
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.contracts.person import PersonParams,PersonReport
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


class PersonEngine(Protocol):
    def infer(self,normalized_rgb:np.ndarray)->np.ndarray:...


MEAN=np.array([.485,.456,.406],np.float32)
STD=np.array([.229,.224,.225],np.float32)


def _preview(rgb:np.ndarray,long_side:int)->np.ndarray:
    h,w=rgb.shape[:2];scale=long_side/max(h,w)
    size=(max(1,round(w*scale)),max(1,round(h*scale)))
    return cv2.resize(rgb,size,interpolation=cv2.INTER_AREA if scale<1 else cv2.INTER_LINEAR).astype(np.float32)


def _probability(logits:np.ndarray,height:int,width:int)->tuple[np.ndarray,float]:
    value=np.asarray(logits,np.float32)
    if value.ndim==4 and value.shape[0]==1:value=value[0]
    if value.ndim!=3 or value.shape[0]!=21 or value.shape[1:]!=(height,width) or not np.isfinite(value).all():
        raise ValueError('MODEL_OUTPUT_INVALID')
    maximum=np.max(value,axis=0);exp=np.exp(value-maximum);den=np.sum(exp,axis=0)
    if not np.isfinite(den).all() or np.any(den<=0):raise ValueError('MODEL_OUTPUT_INVALID')
    probabilities=exp/den
    person=probabilities[15].astype(np.float32)
    confidence=float(np.mean(np.max(probabilities,axis=0),dtype=np.float64))
    return person,confidence


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<1073741824:
        raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(PersonParams)
        if params.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        image=artifacts.contract(params.image,WorkingImage)
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color.encoding!='gamma24':raise ValueError('PERSON_IMAGE_INVALID')
        if image.pixels not in ctx.lease.command.inputs or image.alpha is not None and image.alpha not in ctx.lease.command.inputs:raise ValueError('PERSON_INPUT_UNDECLARED')
        h,w=image.canvas.height,image.canvas.width
        if h*w>200000000:raise ValueError('PERSON_PIXEL_LIMIT')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,5)
    try:
        with ExitStack() as stack:
            local=stack.enter_context(artifacts.input(image.pixels,suffix='.npy'))
            rgb=stack.enter_context(read_array(local.path,(h,w,3),'float32',ctx.check_active,unit_interval=True))
            alpha=None
            if image.alpha is not None:
                local=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'))
                alpha=stack.enter_context(read_array(local.path,(h,w),'uint16',ctx.check_active))
            alpha_excluded=float(np.count_nonzero(alpha==0)/(h*w)) if alpha is not None else 0.
            srgb,clipped=work_to_model(rgb);preview=_preview(srgb,params.long_side)
            ph,pw=preview.shape[:2];normalized=(preview-MEAN)/STD;ctx.progress(1,5)
            with pool.acquire(params.model_id,backend) as engine:logits=engine.infer(normalized)
            small,confidence=_probability(logits,ph,pw);ctx.check_active();ctx.progress(2,5)
            raw=cv2.resize(small,(w,h),interpolation=cv2.INTER_LINEAR).astype(np.float32)
            if alpha is not None:raw[alpha==0]=0
            png_bound=png16_bound(w,h)
            with artifacts.scratch.reserve(h*w*4+8192+png_bound) as reservation:
                mask_path=reservation.path('person-probability','.npy')
                target=np.lib.format.open_memmap(mask_path,mode='w+',dtype='<f4',shape=(h,w))
                try:
                    for tile in refine_tiles(raw,rgb,tile_size=params.guided_tile,check_active=ctx.check_active):target[tile.y:tile.y+tile.values.shape[0],tile.x:tile.x+tile.values.shape[1]]=tile.values
                    if alpha is not None:target[alpha==0]=0
                    target.flush();reservation.validate()
                    person_fraction=float(np.count_nonzero(target>=.5)/(h*w))
                    png_path=reservation.path('person-probability','.png')
                    with artifacts.scratch.open_writer(png_path,reservation) as stream:write_png16(target,stream,max_bytes=png_bound,check_active=ctx.check_active)
                finally:target._mmap.close()
                mask_ref=artifacts.stage(mask_path,reservation,'person_mask','application/octet-stream')
                png_ref=artifacts.stage(png_path,reservation,'person_mask_png','image/png')
                written=mask_path.stat().st_size+png_path.stat().st_size
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    ctx.progress(3,5)
    warnings=[]
    if confidence<.75:warnings.append('low_confidence')
    if person_fraction<.01:warnings.append('empty_person')
    source_clip_fraction=float(np.count_nonzero(clipped)/(h*w))
    if source_clip_fraction>0:warnings.append('source_srgb_clipped')
    mask=MaskRef(artifact=mask_ref,canvas=image.canvas,transform_chain=image.transform_chain,selector='person',confidence=confidence,model_lock_id=params.model_lock_id,protected=False)
    report=PersonReport(image=params.image,mask=mask_ref,preview_width=pw,preview_height=ph,model_id=params.model_id,model_lock_id=params.model_lock_id,backend=backend,preprocessing_id=params.preprocessing_id,confidence=confidence,person_fraction=person_fraction,source_clip_fraction=source_clip_fraction,alpha_excluded_fraction=alpha_excluded,warnings=warnings)
    mask_manifest=artifacts.stage_contract(mask,'person_mask_ref');ctx.progress(4,5)
    report_ref=artifacts.stage_contract(report,'person_report');ctx.progress(5,5)
    return OperationResult(outputs=[mask_ref,png_ref,mask_manifest,report_ref],review_required=bool(warnings),bytes_written=written+len(canonical_bytes(mask))+len(canonical_bytes(report)))


def person_operations(config:ArtifactConfig,pool:ModelPool,backend:str)->list[Operation]:
    def person(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'person_mask','person_mask_png','person_mask_ref','person_report'})) as artifacts:return execute(ctx,artifacts,pool,backend)
    return [Operation('mask.person','gpu',person)]
