"""Exact closure validation for the internal capture preparation result."""
from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.capture.contracts import CapturePrepareParams, CapturePrepared
from photo_worker.operations.capture.adapter import validate_proxy
from photo_worker.operations.session.contracts import FaceObservations
from .adoption import (AdoptionInvalid, ArtifactRef, PreparedAdoption,
                       MAX_JSON_BYTES, require_adoption_active)


def prepare_capture_inputs_adoption(task_id, reader, params_ref, root,
                                    check_active=lambda: None):
    inventory, outputs, ids = [], set(), set()
    def add(ref, path, source, role=None, maximum=MAX_JSON_BYTES, media='application/json'):
        require_adoption_active(check_active)
        ref = ArtifactRef(ref.artifact_id, ref.sha256)
        evidence = reader.evidence(ref)
        require_adoption_active(check_active)
        if (evidence.ref != ref or evidence.source != source or evidence.media_type != media
                or evidence.bytes < 1 or evidence.bytes > maximum
                or source == 'output' and evidence.role != role or ref.artifact_id in ids):
            raise AdoptionInvalid('CAPTURE_PREPARATION_EVIDENCE_INVALID')
        if media == 'application/octet-stream':
            digest = sha256()
            size = 0
            require_adoption_active(check_active)
            for block in reader.iter_bytes(ref, maximum):
                require_adoption_active(check_active)
                if not isinstance(block, bytes) or not block:
                    raise AdoptionInvalid('CAPTURE_PREPARATION_BYTES_INVALID')
                size += len(block)
                if size > evidence.bytes:
                    raise AdoptionInvalid('CAPTURE_PREPARATION_BYTES_INVALID')
                digest.update(block)
            require_adoption_active(check_active)
            if size != evidence.bytes or digest.hexdigest() != ref.sha256:
                raise AdoptionInvalid('CAPTURE_PREPARATION_BYTES_INVALID')
        ids.add(ref.artifact_id)
        inventory.append(dict(artifact_id=str(ref.artifact_id), sha256=ref.sha256,
            purpose=sha256(f'tasks/{task_id}/{path}'.encode()).hexdigest(), ordinal=len(inventory)))
        if source == 'output': outputs.add(ref)
        return ref, evidence
    def document(ref, path, source, contract, role=None, maximum=MAX_JSON_BYTES):
        ref, evidence = add(ref, path, source, role, maximum)
        require_adoption_active(check_active)
        raw = reader.read_json(ref, maximum)
        require_adoption_active(check_active)
        if not isinstance(raw, bytes) or len(raw) != evidence.bytes or sha256(raw).hexdigest() != ref.sha256:
            raise AdoptionInvalid('CAPTURE_PREPARATION_BYTES_INVALID')
        try:
            value = contract.model_validate_json(raw)
            if canonical_bytes(value) != raw: raise ValueError('noncanonical')
        except ValueError as error:
            raise AdoptionInvalid('CAPTURE_PREPARATION_CONTRACT_INVALID') from error
        return value
    params = document(params_ref, '$params', 'input', CapturePrepareParams)
    result = document(root, '$', 'output', CapturePrepared, 'document')
    if result.binding != params.binding:
        raise AdoptionInvalid('CAPTURE_PREPARATION_BINDING_INVALID')
    source = document(params.binding.source, '$/binding/source', 'input', WorkingImage, maximum=1048576)
    proxy = document(result.proxy, '$/proxy', 'output', WorkingImage, 'working_image', 1048576)
    faces = document(result.faces, '$/faces', 'output', FaceObservations, 'faces')
    try:
        validate_proxy(source, proxy, result.binding)
    except ValueError as error:
        raise AdoptionInvalid('CAPTURE_PREPARATION_PROXY_INVALID') from error
    if faces.binding != result.binding or any(face.person_id is not None or face.embedding is not None for face in faces.faces):
        raise AdoptionInvalid('CAPTURE_PREPARATION_IDENTITY_INVALID')
    add(proxy.pixels, '$/proxy/pixels', 'output', 'working_rgb', 50339840, 'application/octet-stream')
    if proxy.alpha is not None:
        add(proxy.alpha, '$/proxy/alpha', 'output', 'working_alpha', 16785408, 'application/octet-stream')
    for index, step in enumerate(proxy.transform_chain.steps):
        if step.kind == 'dense_warp':
            ref = step.inverse_field
            if ref.artifact_id not in ids:
                add(ref, f'$/proxy/transform_chain/steps/{index}/inverse_field', 'input',
                    maximum=1600008192, media='application/octet-stream')
    require_adoption_active(check_active)
    staged_outputs = reader.staged_outputs()
    require_adoption_active(check_active)
    if outputs != staged_outputs:
        raise AdoptionInvalid('CAPTURE_PREPARATION_OUTPUT_CLOSURE_INVALID')
    require_adoption_active(check_active)
    return PreparedAdoption(tuple(inventory), sha256(canonical_bytes(inventory)).hexdigest())
