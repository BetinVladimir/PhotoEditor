"""Independent source-bound scalar evidence reconciliation for render freeze."""
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.operations.session.contracts import CorrectionPlan,CorrectionVector
from .render_inputs import read_render_document
from .render_learning import load_render_learning_prediction

FIELDS=('ev','log_r','log_g','log_b','saturation_delta','retouch_delta')

@dataclass(frozen=True)
class ResolvedRenderOverride:
    ref:ArtifactRef
    delta:CorrectionVector


def load_render_session_delta(artifacts,frozen,*,check_active=lambda:None):
    ref=frozen.recipe.session_corrections
    if ref is None:return None
    raw=read_render_document(artifacts,ref,check_active)
    if canonical_bytes(parse_json(raw))!=raw:raise ValueError('RENDER_CORRECTION_DOCUMENT_CHANGED')
    plan=CorrectionPlan.model_validate_json(raw);seen=set();matched=None
    for ordinal,item in enumerate(plan.items):
        check_active()
        if (item.ordinal!=ordinal or item.binding.asset_version_id.int==0
                or item.binding.source.artifact_id.int==0 or item.binding.asset_version_id in seen):
            raise ValueError('RENDER_CORRECTION_PLAN_INVALID')
        seen.add(item.binding.asset_version_id)
        if item.disposition!='apply' and any(getattr(item.delta,k)!=0 for k in FIELDS):
            raise ValueError('RENDER_CORRECTION_LOCKED_NONZERO')
        if item.reason is not None and len(item.reason.encode())>80:
            raise ValueError('RENDER_CORRECTION_REASON_INVALID')
        if item.binding==frozen.recipe.source:matched=item.delta
    if matched is None:raise ValueError('RENDER_CORRECTION_SOURCE_CHANGED')
    return matched


def verify_render_scalar_evidence(artifacts,frozen,overrides:tuple[ResolvedRenderOverride,...],*,check_active=lambda:None)->CorrectionVector:
    """Overrides must already have passed typed source/style resolution.

    This function validates ordered refs and scalar evidence, not the full
    OverrideV1 module merge. Arbitrary module overrides cannot be replaced by
    their scalar contribution; full composition must preserve them separately.
    """
    if tuple(x.ref for x in overrides)!=tuple(frozen.recipe.overrides):
        raise ValueError('RENDER_OVERRIDE_EVIDENCE_CHANGED')
    parts=[]
    session=load_render_session_delta(artifacts,frozen,check_active=check_active)
    if session is not None:parts.append(session)
    learned=load_render_learning_prediction(artifacts,frozen,check_active=check_active)
    if learned is not None:parts.append(learned)
    parts.extend(x.delta for x in overrides);parts.append(frozen.recipe.parameter_delta)
    values={k:0.0 for k in FIELDS}
    for part in parts:
        check_active()
        for k in FIELDS:values[k]+=getattr(part,k)
    delta=CorrectionVector.model_validate_json(canonical_bytes(values))
    if delta!=frozen.scalars.effective_delta:raise ValueError('RENDER_EFFECTIVE_DELTA_CHANGED')
    check_active();return delta
