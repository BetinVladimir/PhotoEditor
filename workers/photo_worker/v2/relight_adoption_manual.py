"""Completed manual producer discovery and original document binding.

This is an origin proof only. Pixel/vector raster replay remains mandatory before
adoption; discovering a canonical producer does not authorize a result commit.
"""
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes, canonical_hash, ArtifactRef as ContractRef
from photo_worker.contracts.manual_mask import MaskRasterParams, RasterizedMask, ManualMask
from .adoption import AdoptionInvalid, ArtifactRef
from .relight_adoption import _document
from .relight_adoption_source import load_relight_adoption_source, relight_adoption_source_checkpoint


def resolve_relight_adoption_manual(connection, reader, tenant_id, task_id, context,
                                    params, mask, *, protected, check_active):
    context = deepcopy(context)
    if type(protected) is not bool or mask.selector != 'manual' or mask.protected is not protected:
        raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_INVALID')
    selected = params.protected if protected else params.subject
    if canonical_hash(mask) != selected.sha256:
        raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_MISMATCH')
    body, mask_body = canonical_bytes(params), canonical_bytes(mask)
    source = load_relight_adoption_source(connection, tenant_id, task_id, context, body,
                                          check_active=check_active)
    checkpoint = relight_adoption_source_checkpoint(connection, tenant_id, task_id, context,
                                                     body, source, check_active=check_active)
    def lookup(root):
        checkpoint(); check_active()
        row = connection.execute(
            'SELECT photo.v28_relight_adoption_manual_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (tenant_id, task_id, context.attempt, context.fence, context.result_id,
             body.decode(), mask_body.decode(), root, protected)).fetchone()
        check_active()
        if row is None or row[0] is None:
            raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_STALE')
        value = row[0]
        if not isinstance(value, dict) or set(value) != {'root', 'params'}:
            raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_INVALID')
        return {key: ArtifactRef.parse(value[key]) for key in ('root', 'params')}
    record = lookup(None)
    root = _document(reader, record['root'], RasterizedMask, 'input')
    original = _document(reader, record['params'], MaskRasterParams, 'input')
    if original.image != params.base.source or root.image != params.base.source:
        raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_SOURCE_MISMATCH')
    if canonical_bytes(root.protected if protected else root.target) != mask_body:
        raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_ROOT_MISMATCH')
    expected_vectors = original.vectors or ContractRef(artifact_id=record['params'].artifact_id,
                                                    sha256=record['params'].sha256)
    if root.vectors != expected_vectors:
        raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_VECTORS_MISMATCH')
    if original.vectors is not None:
        vectors_ref = ArtifactRef.parse(original.vectors.model_dump(mode='json'))
        _document(reader, vectors_ref, ManualMask, 'input')
    if lookup(canonical_bytes(root).decode()) != record:
        raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_CHANGED')
    checkpoint()
    return record, root, original


def verify_relight_adoption_manual(connection, reader, tenant_id, task_id, context,
                                   params, mask, *, protected, scratch_root, max_bytes,
                                   check_active):
    """Replay both original companions, even when only the guard is selected."""
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.operations.portrait.local_manual_proof import verify_manual_pair
    from .array_evidence import ArrayEvidenceSession
    from .lens_bypass import _ref
    context = deepcopy(context)
    frozen = canonical_hash([params.model_dump(mode='json'), mask.model_dump(mode='json')])
    def resolve():
        check_active()
        if canonical_hash([params.model_dump(mode='json'), mask.model_dump(mode='json')]) != frozen:
            raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_CHANGED')
        return resolve_relight_adoption_manual(connection, reader, tenant_id, task_id,
            context, params, mask, protected=protected, check_active=check_active)
    record, root, original = resolve()
    source = _document(reader, _ref(params.base.source), WorkingImage, 'input')
    vectors_raw = None
    if original.vectors is not None:
        vectors = _document(reader, _ref(original.vectors), ManualMask, 'input')
        vectors_raw = canonical_bytes(vectors)
    shape = (source.canvas.height, source.canvas.width)
    if shape[0] * shape[1] > 200_000_000:
        raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_CANVAS_INVALID')
    with ArrayEvidenceSession(reader, scratch_root, max_bytes, check_active) as arrays:
        target = arrays.array(_ref(root.target.artifact), shape, source='input', unit=True,
                              legacy_input_npy=True)
        guard = arrays.array(_ref(root.protected.artifact), shape, source='input', unit=True,
                             legacy_input_npy=True)
        verify_manual_pair(params.base.source, source,
            ContractRef(artifact_id=record['params'].artifact_id, sha256=record['params'].sha256),
            canonical_bytes(original), root, root.target, root.protected, target, guard,
            vectors_raw=vectors_raw, check_active=check_active)
        again = resolve()
        if (again[0] != record or canonical_bytes(again[1]) != canonical_bytes(root)
                or canonical_bytes(again[2]) != canonical_bytes(original)):
            raise AdoptionInvalid('RELIGHT_ADOPTION_MANUAL_CHANGED')
    check_active()
    return True
