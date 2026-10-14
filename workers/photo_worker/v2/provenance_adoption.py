"""Physical signer result verification before fenced SQL adoption."""
from dataclasses import dataclass
from hashlib import sha256

from photo_worker.contracts.provenance import (
    ProvenanceSignParams, SignedResult, SigningPolicy, SigningLineage, decode_provenance,
)
from photo_worker.operations.provenance.verify_files import verify_signed_files
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid, ArtifactRef, PreparedAdoption


def require_signing_adoption_active(check_active):
    if check_active() is False:
        raise AdoptionInvalid('SIGNING_ADOPTION_INACTIVE')


@dataclass(frozen=True)
class VerifiedSigningResult:
    params_jcs: bytes
    result_jcs: bytes
    policy_jcs: bytes
    lineage_jcs: bytes
    validation_sha: str
    bytes_written: int


def verify_signing_result(reader, params_ref, root, scratch, operator, *, sdk=None, check_active=lambda: None):
    """Reader must be scoped to exact canonical result/attempt/fence by caller.

    No object reads occur in a final SQL transaction. This result is physical
    evidence only; it neither changes export nor inserts provenance metadata.
    """
    observed = {}
    def ref(value):
        return ArtifactRef(value.artifact_id, value.sha256)
    def evidence(value, source, media, role=None, maximum=8 << 20):
        value = ref(value)
        require_signing_adoption_active(check_active)
        item = reader.evidence(value)
        if (item.ref != value or item.source != source or item.media_type not in media
                or source == 'output' and item.role != role
                or type(item.bytes) is not int or not 1 <= item.bytes <= maximum):
            raise AdoptionInvalid('SIGNING_EVIDENCE_INVALID')
        if value in observed and observed[value] != item:
            raise AdoptionInvalid('SIGNING_EVIDENCE_CHANGED')
        observed[value] = item
        return item
    def document(value, source, model, maximum):
        item = evidence(value, source, {'application/json'}, 'document', maximum)
        raw = reader.read_json(ref(value), maximum)
        if not isinstance(raw, bytes) or len(raw) != item.bytes or sha256(raw).hexdigest() != value.sha256:
            raise AdoptionInvalid('SIGNING_DOCUMENT_HASH')
        return raw, decode_provenance(raw, model, maximum)
    def stream(value, item, writer):
        size, digest = 0, sha256()
        for block in reader.iter_bytes(ref(value), item.bytes):
            require_signing_adoption_active(check_active)
            if not isinstance(block, bytes) or not block or len(block) > 1 << 20:
                raise AdoptionInvalid('SIGNING_STREAM_INVALID')
            size += len(block)
            if size > item.bytes:
                raise AdoptionInvalid('SIGNING_STREAM_LIMIT')
            digest.update(block)
            if writer.write(block) != len(block):
                raise AdoptionInvalid('SIGNING_SHORT_WRITE')
        if size != item.bytes or digest.hexdigest() != value.sha256:
            raise AdoptionInvalid('SIGNING_FILE_HASH')
    try:
        require_signing_adoption_active(check_active)
        if not callable(getattr(sdk, 'Reader', None)):
            raise AdoptionInvalid('SIGNING_ADOPTION_SDK_REQUIRED')
        params_raw, params = document(params_ref, 'input', ProvenanceSignParams, 65536)
        policy_raw, policy = document(params.policy, 'input', SigningPolicy, 1 << 20)
        lineage_raw, _ = document(params.lineage, 'input', SigningLineage, 8 << 20)
        if policy.id != params.signing_policy_id:
            raise AdoptionInvalid('SIGNING_POLICY_BINDING')
        result_raw, result = document(root, 'output', SignedResult, 8 << 20)
        input_ids = {value.artifact_id for value in (params_ref, params.export, params.policy, params.lineage)}
        output_refs = {ref(root), ref(result.signed_export), ref(result.manifest)}
        output_ids = {value.artifact_id for value in output_refs}
        if len(input_ids) != 4 or len(output_ids) != 3 or input_ids & output_ids or any(value.int == 0 for value in input_ids | output_ids):
            raise AdoptionInvalid('SIGNING_OUTPUT_ALIAS')
        if reader.staged_outputs() != output_refs:
            raise AdoptionInvalid('SIGNING_OUTPUT_CLOSURE')
        original = evidence(params.export, 'input', set(policy.allowed_mime), maximum=512 << 20)
        signed = evidence(result.signed_export, 'output', {original.media_type}, 'signed_export', 512 << 20)
        manifest = evidence(result.manifest, 'output', {'application/c2pa'}, 'manifest')
        # Use bounded scratch for binary manifest as well, avoiding an unbounded
        # reader-specific bytes convenience method.
        with scratch.reserve(original.bytes + signed.bytes + manifest.bytes) as reservation:
            paths = []
            for value, item, name in ((params.export, original, 'original'),
                                      (result.signed_export, signed, 'signed'),
                                      (result.manifest, manifest, 'c2pa')):
                path = reservation.path(name, '.bin')
                with scratch.open_writer(path, reservation) as writer:
                    stream(value, item, writer)
                reservation.validate_file(path)
                paths.append(path)
            binary_manifest = paths[2].read_bytes()
            validation_sha = verify_signed_files(paths[0], paths[1], original.media_type,
                params.export.sha256, result.signed_export.sha256, binary_manifest,
                result.manifest.sha256, lineage_raw, policy_raw, operator,
                sdk=sdk, check_active=check_active)
        for value, expected in observed.items():
            require_signing_adoption_active(check_active)
            if reader.evidence(value) != expected:
                raise AdoptionInvalid('SIGNING_EVIDENCE_CHANGED')
        if reader.staged_outputs() != output_refs:
            raise AdoptionInvalid('SIGNING_OUTPUT_CLOSURE_CHANGED')
        require_signing_adoption_active(check_active)
        return VerifiedSigningResult(params_raw, result_raw, policy_raw, lineage_raw,
                                     validation_sha, len(result_raw) + signed.bytes + manifest.bytes)
    except AdoptionInvalid:
        raise
    except (ValueError, OSError) as exc:
        raise AdoptionInvalid('SIGNING_PHYSICAL_VERIFICATION_FAILED') from exc


@dataclass(frozen=True)
class PreparedSigningAdoption:
    verified: VerifiedSigningResult
    adoption: PreparedAdoption


def prepare_signing_adoption(*, task_id, reader, params_ref, root, scratch,
                             operator, sdk=None, check_active=lambda: None):
    from uuid import UUID
    if not isinstance(task_id, UUID) or task_id.int == 0:
        raise AdoptionInvalid('SIGNING_ADOPTION_TASK')
    verified = verify_signing_result(reader, params_ref, root, scratch, operator,
                                     sdk=sdk, check_active=check_active)
    result = decode_provenance(verified.result_jcs, SignedResult, 8 << 20)
    inventory = []
    for ordinal, (reference, location) in enumerate(((root, '$'),
            (result.signed_export, '$/signed_export'), (result.manifest, '$/manifest'))):
        require_signing_adoption_active(check_active)
        inventory.append({'artifact_id': str(reference.artifact_id), 'sha256': reference.sha256,
            'purpose': sha256(('tasks/' + str(task_id) + '/' + location).encode()).hexdigest(),
            'ordinal': ordinal})
    digest = sha256(canonical_bytes(inventory)).hexdigest()
    return PreparedSigningAdoption(verified, PreparedAdoption(tuple(inventory), digest))
