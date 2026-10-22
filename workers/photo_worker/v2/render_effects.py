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
from .render_detail import RenderDetailStage
from .render_denoise import RenderDenoiseStage
from .render_dust import RenderDustStage
from .render_background import RenderBlurStage,RenderComposeStage
from photo_worker.operations.portrait.prepare import FaceEvidence
from photo_worker.operations.defects.prepare import RepairEvidence

RenderEffectStage=RenderBeautyStage|RenderDefectStage|RenderDetailStage|RenderDenoiseStage|RenderDustStage|RenderPortraitBase|RenderBlurStage|RenderComposeStage


@dataclass(frozen=True)
class RenderEffectChain:
    """Ordered stage outcomes plus final scoped pixels, without nested chains."""
    pixels:AdjustmentPixels
    effects:tuple[RenderEffectStage,...]


@dataclass(frozen=True)
class RenderLayerEffect:
    """Pure outcome metadata; no expired proposal pixel mappings are retained."""
    layer_id: UUID
    operation: str
    operation_version: str
    runtime_sha256: str
    candidates: tuple[RepairEvidence,...]
    review_required: bool
    regions: tuple = ()
    denoise_report: object = None
    dust_report: object = None
    conservative_report: object = None


@dataclass(frozen=True)
class RenderCombinedEffects:
    base: object
    layers: tuple[RenderLayerEffect,...]


RenderEffectEvidence=RenderEffectStage|RenderEffectChain|RenderCombinedEffects


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
    if isinstance(evidence,RenderLayerEffect):
        if (not isinstance(evidence.layer_id,UUID) or evidence.layer_id.int==0
                or evidence.operation not in ('defects.repair','portrait.detail','image.denoise','sensor.dust_repair','defects.auto_repair','sensor.dust_auto_repair') or evidence.operation_version!='1'
                or not isinstance(evidence.runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',evidence.runtime_sha256) is None
                or type(evidence.review_required) is not bool):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        if evidence.operation in ('defects.auto_repair','sensor.dust_auto_repair'):
            from photo_worker.contracts.conservative import ConservativeRepairReport
            from photo_worker.contracts.base import canonical_bytes
            if evidence.candidates!=() or evidence.regions!=() or evidence.denoise_report is not None or evidence.dust_report is not None or not isinstance(evidence.conservative_report,ConservativeRepairReport):raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
            report=ConservativeRepairReport.model_validate_json(canonical_bytes(evidence.conservative_report))
            if report.operation!=evidence.operation:raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
            return evidence.review_required
        if evidence.conservative_report is not None:raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        if evidence.operation=='sensor.dust_repair':
            if evidence.candidates!=() or evidence.regions!=() or evidence.denoise_report is not None:
                raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
            return effect_review_required(RenderDustStage(None,evidence.dust_report,evidence.review_required))
        if evidence.dust_report is not None:raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        if evidence.operation=='image.denoise':
            if evidence.candidates!=() or evidence.regions!=():raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
            return effect_review_required(RenderDenoiseStage(None,evidence.denoise_report,evidence.review_required))
        if evidence.denoise_report is not None:raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        if evidence.operation=='portrait.detail':
            if evidence.candidates!=():raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
            return effect_review_required(RenderDetailStage(None,evidence.regions,evidence.review_required))
        if evidence.regions!=():raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        _validate_repairs(evidence.candidates)
        return evidence.review_required or not evidence.candidates or any(item.reason is not None for item in evidence.candidates)
    if isinstance(evidence,RenderCombinedEffects):
        if (evidence.base is not None and not isinstance(evidence.base,(RenderBeautyStage,RenderDefectStage,RenderDetailStage,RenderDenoiseStage,RenderDustStage,RenderPortraitBase,RenderBlurStage,RenderComposeStage,RenderEffectChain))
                or not isinstance(evidence.layers,tuple) or not 1<=len(evidence.layers)<=512
                or any(not isinstance(item,RenderLayerEffect) for item in evidence.layers)
                or len({item.layer_id for item in evidence.layers})!=len(evidence.layers)):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        reviews=(effect_review_required(evidence.base),*(effect_review_required(item) for item in evidence.layers))
        return any(reviews)
    if isinstance(evidence,RenderEffectChain):
        if (not isinstance(evidence.pixels,AdjustmentPixels) or not isinstance(evidence.effects,tuple)
                or not 1<=len(evidence.effects)<=32
                or any(not isinstance(item,(RenderBeautyStage,RenderDefectStage,RenderDetailStage,RenderDenoiseStage,RenderDustStage,RenderPortraitBase,RenderBlurStage,RenderComposeStage)) for item in evidence.effects)):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        # Validate every stage even when an earlier one already requires review.
        reviews=tuple(effect_review_required(item) for item in evidence.effects)
        return any(reviews)
    if not isinstance(evidence,(RenderBeautyStage,RenderDefectStage,RenderDetailStage,RenderDenoiseStage,RenderDustStage,RenderPortraitBase,RenderBlurStage,RenderComposeStage)):
        raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
    if type(evidence.review_required) is not bool:
        raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
    if isinstance(evidence,RenderDustStage):
        from photo_worker.contracts.dust import DustRepairReport
        from photo_worker.contracts.base import canonical_bytes
        if not isinstance(evidence.report,DustRepairReport):raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        try:report=DustRepairReport.model_validate_json(canonical_bytes(evidence.report))
        except ValueError as error:raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID') from error
        if len({item.candidate_id for item in report.candidates})!=len(report.candidates):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        if report.warnings!=([] if report.candidates else ['no_accepted_candidates']):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        return evidence.review_required or bool(report.warnings) or any(item.applied_pixels==0 for item in report.candidates)
    if isinstance(evidence,RenderDenoiseStage):
        from photo_worker.contracts.restoration import DenoiseReport
        from photo_worker.contracts.base import canonical_bytes
        if not isinstance(evidence.report,DenoiseReport):raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        try:report=DenoiseReport.model_validate_json(canonical_bytes(evidence.report))
        except ValueError as error:raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID') from error
        expected=[]
        if report.source_clip_fraction>0:expected.append('source_srgb_clipped')
        if report.gamut_excess_fraction>0:expected.append('delta_gamut_excess')
        if report.warnings!=expected:raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        return evidence.review_required or bool(expected) or report.disabled_delta_fraction>0
    if isinstance(evidence,RenderDetailStage):
        from photo_worker.contracts.portrait_detail import DetailEvidence
        from photo_worker.contracts.base import canonical_bytes
        if (not isinstance(evidence.regions,tuple) or len(evidence.regions)>128
                or any(not isinstance(region,DetailEvidence) for region in evidence.regions)
                or len({region.region_id for region in evidence.regions})!=len(evidence.regions)):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        for region in evidence.regions:
            try:DetailEvidence.model_validate_json(canonical_bytes(region))
            except ValueError as error:raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID') from error
            if (re.fullmatch('[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}',region.region_id) is None
                    or region.kind not in ('teeth','sclera','shine','under_eye','cloth','glasses')
                    or region.applied!=(region.reason is None)):
                raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        return evidence.review_required or any(region.reason not in (None,'zero_strength') for region in evidence.regions)
    if isinstance(evidence,RenderComposeStage):
        if (evidence.mode not in ('transparent','solid','asset')
                or not _finite(evidence.subject_confidence,.75,1)):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        return evidence.review_required or evidence.subject_confidence<.9
    if isinstance(evidence,RenderBlurStage):
        if (not _finite(evidence.subject_confidence,.75,1)
                or not _finite(evidence.subject_depth,0,1)
                or not _finite(evidence.strength,0,1)
                or type(evidence.maximum_radius) is not int or not 0<=evidence.maximum_radius<=100000):
            raise ValueError('RENDER_EFFECT_EVIDENCE_INVALID')
        return evidence.review_required or evidence.subject_confidence<.9
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
    if not isinstance(base,(RenderBeautyStage,RenderDefectStage,RenderDetailStage,RenderDenoiseStage,RenderDustStage,RenderPortraitBase,RenderBlurStage,RenderComposeStage,RenderEffectChain)):
        raise ValueError('RENDER_BASE_RESULT_INVALID')
    effect_review_required(base)
    if not isinstance(base.pixels,AdjustmentPixels):raise ValueError('RENDER_BASE_RESULT_INVALID')
    return base.pixels,base
