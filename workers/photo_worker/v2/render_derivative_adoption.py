"""Scoped evidence and encoded-byte validation before derivative commit."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.media import MediaParams, DerivativeManifest
from .adoption import AdoptionInvalid
from .lens_bypass import _ref
from .render_derivative import verify_render_derivative_encoded


def verify_render_derivative_result(reader, params_ref, manifest_ref, *,
                                    target_icc_sha256, working_icc_sha256,
                                    check_active=lambda: None, additional_working_icc_sha256=()):
    """Reader must authorize task/attempt/fence and pin versions on every read.

    This produces validation evidence only, without database mutation. Both ICC
    hashes must originate in the trusted deployed registry.
    """
    if (type(additional_working_icc_sha256) is not tuple or len(additional_working_icc_sha256)>2
            or any(not isinstance(value,str) for value in additional_working_icc_sha256)
            or len(set(additional_working_icc_sha256))!=len(additional_working_icc_sha256)):
        raise AdoptionInvalid('RENDER_DERIVATIVE_ICC_REQUIRED')
    for digest in (target_icc_sha256, working_icc_sha256, *additional_working_icc_sha256):
        if not isinstance(digest, str) or len(digest) != 64 or any(c not in '0123456789abcdef' for c in digest):
            raise AdoptionInvalid('RENDER_DERIVATIVE_ICC_REQUIRED')
    def document(ref, model, source, role=None):
        check_active()
        meta=reader.evidence(ref)
        if meta.ref!=ref or meta.source!=source or meta.media_type!='application/json' or not 2<=meta.bytes<=1048576 or (role is not None and meta.role!=role):
            raise AdoptionInvalid('RENDER_DERIVATIVE_EVIDENCE_INVALID')
        raw=reader.read_json(ref,1048576)
        check_active()
        if not isinstance(raw,bytes) or len(raw)!=meta.bytes or sha256(raw).hexdigest()!=ref.sha256:
            raise AdoptionInvalid('RENDER_DERIVATIVE_BYTES_INVALID')
        try:
            value=model.model_validate_json(raw)
            if canonical_bytes(value)!=raw:raise ValueError('noncanonical document')
        except ValueError as exc:
            raise AdoptionInvalid('RENDER_DERIVATIVE_CONTRACT_INVALID') from exc
        return value
    params=document(params_ref,MediaParams,'input','task_params')
    manifest=document(manifest_ref,DerivativeManifest,'output','derivative_manifest')
    image=document(_ref(params.image),WorkingImage,'input')
    if image.color.icc_sha256 not in (working_icc_sha256,*additional_working_icc_sha256):
        raise AdoptionInvalid('RENDER_DERIVATIVE_ICC_INVALID')
    derivative_ref=_ref(manifest.derivative)
    check_active()
    evidence=reader.evidence(derivative_ref)
    if (evidence.ref!=derivative_ref or evidence.source!='output' or evidence.role!='derivative'
            or evidence.media_type!=manifest.media_type or not 2<=evidence.bytes<=52*1048576
            or reader.staged_outputs()!={manifest_ref,derivative_ref}):
        raise AdoptionInvalid('RENDER_DERIVATIVE_OUTPUT_INVALID')
    iterator=iter(reader.iter_bytes(derivative_ref,evidence.bytes))
    body=bytearray()
    stream_failed=False
    try:
        for block in iterator:
            check_active()
            if not isinstance(block,bytes) or not block or len(block)>65536 or len(body)+len(block)>evidence.bytes:
                raise AdoptionInvalid('RENDER_DERIVATIVE_STREAM_INVALID')
            body.extend(block)
    except BaseException:
        stream_failed=True
        raise
    finally:
        close=getattr(iterator,'close',None)
        if close is not None:
            try:close()
            except BaseException:
                if not stream_failed:raise
    check_active()
    if len(body)!=evidence.bytes:
        raise AdoptionInvalid('RENDER_DERIVATIVE_STREAM_INVALID')
    try:
        verify_render_derivative_encoded(manifest,params,image,bytes(body),srgb_sha256=target_icc_sha256)
    except (ValueError,OSError) as exc:
        raise AdoptionInvalid('RENDER_DERIVATIVE_ENCODING_INVALID') from exc
    check_active()
    return manifest
