"""Retain typed base-stage evidence through composition and publication."""
from dataclasses import dataclass
from math import isfinite
from numbers import Real
from uuid import UUID
import re
from .render_adjustments import AdjustmentPixels
from .render_beauty import RenderBeautyStage
from .render_defects import RenderDefectStage
from .render_portrait import RenderPortraitBase
from photo_worker.operations.portrait.prepare import FaceEvidence
from photo_worker.operations.defects.prepare import RepairEvidence

RenderEffectStage=RenderBeautyStage|RenderDefectStage|RenderPortraitBase


@dataclass(frozen=True)
class RenderEffectChain:
    """Ordered stage outcomes plus final scoped pixels, without nested chains."""
    pixels:AdjustmentPixels
    effects:tuple[RenderEffectStage,...]


RenderEffectEvidence=RenderEffectStage|RenderEffectChain


def _finite(value,minimum,maximum=None):
    return (isinstance(value,Real) and not isinstance(value,bool) and isfinite(value)
        and value>=minimum and (maximum is None or value<=maximum))


def _reason(value):
    return value is None or isinstance(value,str) and 1<=len(value)<=200


def _validate_faces(faces):
    if (not isinstance(faces,tuple) or len(faces)>64
            or any(not isinstance(face,FaceEvidence) for face in faces)):
        raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
    for face in faces:
        if (not isinstance(face.object_id,UUID) or type(face.applied) is not bool or not _reason(face.reason)
                or face.applied and face.reason is not None
                or not _finite(face.below_zero,0,1) or not _finite(face.above_one,0,1)
                or face.texture_energy_ratio is not None and not _finite(face.texture_energy_ratio,0)):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')

    if len({face.object_id for face in faces})!=len(faces):
        raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')

def _validate_repairs(candidates):
    if (not isinstance(candidates,tuple) or len(candidates)>4096
            or any(not isinstance(item,RepairEvidence) for item in candidates)):
        raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
    for item in candidates:
        if (not isinstance(item.candidate_id,str) or re.fullmatch('[0-9a-f]{64}',item.candidate_id) is None
                or type(item.label) is not int or not 1<=item.label<=65535
                or type(item.applied_pixels) is not int or not 0<=item.applied_pixels<=200000000
                or not _reason(item.reason) or not _finite(item.radius,1,5)
                or not _finite(item.below_zero,0,1) or not _finite(item.above_one,0,1)):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
    if (len({item.candidate_id for item in candidates})!=len(candidates)
            or len({item.label for item in candidates})!=len(candidates)):
        raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')


def effect_review_required(evidence):
    """Do not let a declared pass erase skipped-stage evidence.

    These scoped records are not substitute measurements or published parents.
    Full report/model/identity checks remain mandatory independently.
    """
    if evidence is None:return False
    if isinstance(evidence,RenderEffectChain):
        if (not isinstance(evidence.pixels,AdjustmentPixels) or not isinstance(evidence.effects,tuple)
                or not 1<=len(evidence.effects)<=32
                or any(not isinstance(item,(RenderBeautyStage,RenderDefectStage,RenderPortraitBase)) for item in evidence.effects)):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        # Validate every stage even when an earlier one already requires review.
        reviews=tuple(effect_review_required(item) for item in evidence.effects)
        return any(reviews)
    if not isinstance(evidence,(RenderBeautyStage,RenderDefectStage,RenderPortraitBase)):
        raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
    if type(evidence.review_required) is not bool:
        raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
    if isinstance(evidence,(RenderBeautyStage,RenderPortraitBase)):
        faces=evidence.faces if isinstance(evidence,RenderBeautyStage) else evidence.beauty_evidence
        _validate_faces(faces)
        return (evidence.review_required or isinstance(evidence,RenderBeautyStage) and not faces
            or any(not face.applied and face.reason!='zero_strength' for face in faces))
    if isinstance(evidence,RenderDefectStage):
        _validate_repairs(evidence.candidates)
        return evidence.review_required or not evidence.candidates or any(item.reason is not None for item in evidence.candidates)
    raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')


def unpack_render_base(base):
    """Accept raw pixel providers or exact supported typed effect results."""
    if isinstance(base,AdjustmentPixels):return base,None
    if not isinstance(base,(RenderBeautyStage,RenderDefectStage,RenderPortraitBase,RenderEffectChain)):
        raise ValueError('RENDER_BASE_RESULT_INVALID')
    effect_review_required(base)
    if not isinstance(base.pixels,AdjustmentPixels):raise ValueError('RENDER_BASE_RESULT_INVALID')
    return base.pixels,base
