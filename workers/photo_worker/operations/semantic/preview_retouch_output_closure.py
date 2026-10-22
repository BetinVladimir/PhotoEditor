"""Exact retouch attempt output set; IO/provenance and canonical adoption are external."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from .preview_retouch_result_binding import bind_retouch_preview_result


ROLES = frozenset({'pixels', 'working_image', 'preview_evidence', 'before', 'after',
                   'affected', 'protected', 'preview_presentation'})
DOCUMENTS = frozenset({'working_image', 'preview_evidence', 'preview_presentation'})


class RetouchOutputClosure:
    """Record already verified stage results, then prove the complete root closure.

    This ledger cannot replace physical file checks, version-pinned uploads,
    live authority or server-side output adoption. Its inputs are internal stage
    results; no client may supply these records as provenance evidence.
    """
    def __init__(self, declared_inputs, *, max_output_bytes):
        if (not isinstance(declared_inputs, list) or not 1 <= len(declared_inputs) <= 512
                or type(max_output_bytes) is not int or not 1 <= max_output_bytes <= 2**53-1):
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        refs = [ArtifactRef.model_validate_json(canonical_bytes(ref)) for ref in declared_inputs]
        if any(ref.artifact_id.int == 0 for ref in refs) or len({ref.artifact_id for ref in refs}) != len(refs):
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        self._inputs = {ref.artifact_id: ref.sha256 for ref in refs}
        self._outputs = {}
        self._limit = max_output_bytes
        self._bytes = 0

    def record(self, role, ref, size, *, document=None):
        ref = ArtifactRef.model_validate_json(canonical_bytes(ref))
        if (role not in ROLES or role in self._outputs or ref.artifact_id.int == 0
                or ref.artifact_id in self._inputs
                or any(ref.artifact_id == value[0].artifact_id for value in self._outputs.values())
                or type(size) is not int or not 1 <= size <= self._limit-self._bytes):
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        if role in DOCUMENTS:
            if (not isinstance(document, bytes) or not 2 <= len(document) <= 1048576
                    or size != len(document) or sha256(document).hexdigest() != ref.sha256
                    or canonical_bytes(parse_json(document)) != document):
                raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        elif document is not None:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        self._outputs[role] = (ref.model_copy(deep=True), size, document)
        self._bytes += size

    def verify(self, params, source_raw, *, presentation_ref, expected_bytes):
        if set(self._outputs) != ROLES or type(expected_bytes) is not int or expected_bytes != self._bytes:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        rows = self._outputs
        display_ref, _, display_raw = rows['preview_presentation']
        evidence_ref, _, evidence_raw = rows['preview_evidence']
        image_ref, _, image_raw = rows['working_image']
        if presentation_ref != display_ref:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        _, display, evidence, image = bind_retouch_preview_result(params, display_ref, display_raw,
            evidence_ref, evidence_raw, image_ref, image_raw, source_raw)
        if (image.pixels != rows['pixels'][0] or any(getattr(display, role) != rows[role][0]
                for role in ('before', 'after', 'affected', 'protected'))):
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        allowed = dict(self._inputs)
        allowed.update({row[0].artifact_id:row[0].sha256 for row in rows.values()})
        reached = set()
        def visit(value):
            if isinstance(value, dict):
                if set(value) == {'artifact_id', 'sha256'}:
                    ref = ArtifactRef.model_validate_json(canonical_bytes(value))
                    if allowed.get(ref.artifact_id) != ref.sha256:
                        raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
                    reached.add(ref.artifact_id)
                else:
                    for child in value.values():visit(child)
            elif isinstance(value, list):
                for child in value:visit(child)
        for raw in (display_raw, evidence_raw, image_raw):visit(parse_json(raw))
        reached.add(display_ref.artifact_id)
        if any(row[0].artifact_id not in reached for row in rows.values()):
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        return self._bytes


class RecordingRetouchArtifacts:
    """Record actual delegated uploads; retain bounded documents, not pixel data."""
    def __init__(self, artifacts, ledger, *, check_active):
        if not isinstance(ledger, RetouchOutputClosure) or not callable(check_active):
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        self._artifacts, self._ledger, self._active = artifacts, ledger, check_active

    def __getattr__(self, name):
        return getattr(self._artifacts, name)

    def stage(self, path, reservation, role, media_type, **kwargs):
        if role not in ROLES-DOCUMENTS or media_type != ('application/x-npy' if role == 'pixels' else 'image/png'):
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        self._active()
        def identity():
            size = 0
            digest = sha256()
            with path.open('rb') as stream:
                while block := stream.read(65536):
                    self._active()
                    size += len(block)
                    if size > self._ledger._limit:
                        raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
                    digest.update(block)
            return size, digest.hexdigest()
        size, digest = identity()
        ref = self._artifacts.stage(path, reservation, role, media_type, **kwargs)
        self._active()
        owned = ArtifactRef.model_validate_json(canonical_bytes(ref))
        if identity() != (size, digest) or owned.sha256 != digest:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        self._ledger.record(role, owned, size)
        self._active()
        return owned

    def stage_contract(self, value, role):
        if role not in DOCUMENTS:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        self._active()
        raw = canonical_bytes(value)
        if not 2 <= len(raw) <= 1048576:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        owned = type(value).model_validate_json(raw)
        ref = self._artifacts.stage_contract(owned, role)
        self._active()
        if canonical_bytes(owned) != raw:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
        self._ledger.record(role, ref, len(raw), document=raw)
        self._active()
        return ArtifactRef.model_validate_json(canonical_bytes(ref))
