"""Owned full dependency refs from the verified V2 index, never transport roots."""
from photo_worker.contracts.base import ArtifactRef, canonical_bytes


def removal_declared_inputs(artifacts, *, check_active):
    discover = getattr(artifacts, 'declared_input_refs', None)
    if not callable(discover) or not callable(check_active):
        raise ValueError('SEMANTIC_REMOVAL_INPUT_INDEX_REQUIRED')
    check_active()
    values = discover(maximum=512)
    check_active()
    if not isinstance(values, tuple) or not 1 <= len(values) <= 512:
        raise ValueError('SEMANTIC_REMOVAL_INPUTS_INVALID')
    refs = tuple(ArtifactRef.model_validate_json(canonical_bytes(value)) for value in values)
    if any(ref.artifact_id.int == 0 for ref in refs) or len({ref.artifact_id for ref in refs}) != len(refs):
        raise ValueError('SEMANTIC_REMOVAL_INPUTS_INVALID')
    check_active()
    return refs
