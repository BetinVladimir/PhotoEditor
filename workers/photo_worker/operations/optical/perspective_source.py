"""Exact original WorkingImage child types in the verified task closure."""
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import DenseWarpTransform


def verify_perspective_source_closure(artifacts,params,source,lines):
    if (source.dtype!='float32' or canonical_hash(source)!=params.base.source.sha256 or
        canonical_hash(source.canvas)!=params.base.canvas_hash):
        raise ValueError('PERSPECTIVE_SOURCE_MISMATCH')
    if params.base.source not in artifacts.declared_contract_refs('WorkingImage',512):
        raise ValueError('PERSPECTIVE_SOURCE_MISMATCH')
    arrays=artifacts.declared_contract_refs('NpyArray',512)
    required=[source.pixels]
    if source.alpha is not None:required.append(source.alpha)
    required.extend(step.inverse_field for step in source.transform_chain.steps if isinstance(step,DenseWarpTransform))
    if any(ref not in arrays for ref in required):raise ValueError('PERSPECTIVE_SOURCE_CHILD_UNDECLARED')
    if source.alpha is not None and source.alpha.artifact_id==source.pixels.artifact_id:
        raise ValueError('PERSPECTIVE_SOURCE_CHILD_ALIAS')
    if params.lines is not None:
        if (lines is None or canonical_hash(lines)!=params.lines.sha256 or
            params.lines not in artifacts.declared_contract_refs('LineSegments',512)):
            raise ValueError('PERSPECTIVE_LINES_INVALID')
