"""Pinned scene12_v1 prediction for an authored frozen render recipe."""
from math import isfinite
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.learning.contracts import CandidateProfile
from photo_worker.operations.learning.model import scene12_features
from photo_worker.operations.session.contracts import DescriptorShard, CorrectionVector
from .render_inputs import read_render_document


def load_render_learning_prediction(artifacts, frozen, *, check_active=lambda: None) -> CorrectionVector | None:
    """Resolve immutable model/descriptor through the admitted task closure.

    SQL admission must independently prove approved model/training provenance,
    analysis ownership and shard membership. Document contents alone cannot
    prove those relationships. Return only the learned contribution; the full
    scalar compiler must combine session/overrides/authored delta once.
    """
    evidence=frozen.learning
    if evidence is None:return None
    check_active()
    def document(ref,contract):
        raw=read_render_document(artifacts,ref,check_active)
        if canonical_bytes(parse_json(raw))!=raw:raise ValueError('RENDER_LEARNING_DOCUMENT_CHANGED')
        return contract.model_validate_json(raw)
    model=document(evidence.model,CandidateProfile)
    shard=document(evidence.descriptor,DescriptorShard)
    if model.profile_id.int==0:raise ValueError('RENDER_LEARNING_MODEL_INVALID')
    ordinals=[frame.ordinal for frame in shard.frames]
    if len(set(ordinals))!=len(ordinals):raise ValueError('RENDER_LEARNING_DESCRIPTOR_INVALID')
    frames=[frame for frame in shard.frames if frame.ordinal==evidence.ordinal]
    if len(frames)!=1 or frames[0].binding!=frozen.recipe.source:
        raise ValueError('RENDER_LEARNING_DESCRIPTOR_CHANGED')
    frame=frames[0]
    features=scene12_features(tuple(frame.luma_quantiles),tuple(frame.neutral_rgb) if frame.neutral_rgb is not None else None,frame.clipping_fraction,frame.sharpness,frame.person_fraction)
    # Match Go PredictCorrection's ordered scalar accumulation, avoiding a
    # BLAS-dependent reduction order at freeze verification boundaries.
    limits=(1,.223144,.223144,.223144,.15,.15);values=[]
    for column,limit in enumerate(limits):
        check_active();value=model.intercept[column]
        for row,feature in enumerate(features):
            value+=((feature-model.means[row])/model.scales[row])*model.coefficients[row][column]
        if not isfinite(value):raise ValueError('RENDER_LEARNING_PREDICTION_INVALID')
        values.append(max(-limit,min(limit,value)))
    check_active()
    return CorrectionVector.model_validate_json(canonical_bytes(dict(zip(('ev','log_r','log_g','log_b','saturation_delta','retouch_delta'),values))))
