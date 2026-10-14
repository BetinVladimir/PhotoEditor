"""Independent adoption check for a signed Delivery assembly result."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.delivery import (V2DeliveryAssembleParams,
    SignedDeliveryAssemblyEnvelope, SignedDeliveryAssemblyClosure,
    DeliveryAssemblyInput)
from photo_worker.operations.delivery.assembly import assemble_signed_delivery
from .adoption import AdoptionInvalid, ArtifactRef, require_adoption_active


@dataclass(frozen=True)
class VerifiedSignedDeliveryAssembly:
    params_jcs: bytes
    result_jcs: bytes


def verify_signed_delivery_dependency_bytes(reader, closure_raw: bytes, *,
                                            check_active=lambda: None) -> None:
    """Re-read exact versioned V31 outputs declared as this task's inputs.

    V31's adopted receipt supplies cryptographic qualification; this check
    ensures its retained signed and C2PA objects still match the frozen refs.
    """
    if not callable(check_active):
        raise AdoptionInvalid('DELIVERY_SIGNED_ACTIVITY_INVALID')
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    check_active()
    try:
        closure = SignedDeliveryAssemblyClosure.model_validate_json(closure_raw)
        for entry in closure.entries:
            original_ref = ArtifactRef(entry.export.artifact_id, entry.export.sha256)
            check_active()
            original = reader.evidence(original_ref)
            check_active()
            if (original.ref != original_ref or original.source != 'input'
                    or original.media_type not in ('image/jpeg', 'image/png')
                    or type(original.bytes) is not int or not 1 <= original.bytes <= 512 << 20):
                raise AdoptionInvalid('DELIVERY_SIGNED_ORIGINAL_EVIDENCE_INVALID')
            for ref, media, maximum in ((entry.export, original.media_type, 512 << 20),
                                        (entry.signed, original.media_type, 512 << 20),
                                        (entry.manifest, 'application/c2pa', 8 << 20)):
                check_active()
                key = ArtifactRef(ref.artifact_id, ref.sha256)
                evidence = reader.evidence(key)
                check_active()
                if (evidence.ref != key or evidence.source != 'input'
                        or evidence.media_type != media or type(evidence.bytes) is not int
                        or not 1 <= evidence.bytes <= maximum):
                    raise AdoptionInvalid('DELIVERY_SIGNED_DEPENDENCY_EVIDENCE_INVALID')
                size, digest = 0, sha256()
                stream = reader.iter_bytes(key, evidence.bytes)
                try:
                    for block in stream:
                        check_active()
                        if not isinstance(block, bytes) or not block or len(block) > 1 << 20:
                            raise AdoptionInvalid('DELIVERY_SIGNED_DEPENDENCY_STREAM_INVALID')
                        size += len(block)
                        if size > evidence.bytes:
                            raise AdoptionInvalid('DELIVERY_SIGNED_DEPENDENCY_SIZE_INVALID')
                        digest.update(block)
                finally:
                    close = getattr(stream, 'close', None)
                    if callable(close):
                        close()
                check_active()
                if size != evidence.bytes or digest.hexdigest() != ref.sha256:
                    raise AdoptionInvalid('DELIVERY_SIGNED_DEPENDENCY_HASH_INVALID')
        check_active()
    except AdoptionInvalid:
        raise
    except ValueError as error:
        raise AdoptionInvalid('DELIVERY_SIGNED_DEPENDENCY_INPUT_INVALID') from error


def verify_signed_delivery_assembly(reader, params_ref: ArtifactRef, root: ArtifactRef,
                                    tenant: UUID, actor: UUID, parent: UUID,
                                    *, check_active=lambda: None) -> VerifiedSignedDeliveryAssembly:
    """Require exact task inputs and one staged output before trusting v2 manifest.

    Caller must obtain tenant/actor/parent from the current SQL parent lease,
    and independently repeat live V31 operator/physical evidence checks.
    """
    if not callable(check_active):
        raise AdoptionInvalid('DELIVERY_SIGNED_ACTIVITY_INVALID')
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    def read(ref, maximum, source='input'):
        check_active()
        reference = ArtifactRef(ref.artifact_id, ref.sha256)
        evidence = reader.evidence(reference)
        if (evidence.ref != reference or evidence.source != source
                or evidence.media_type != 'application/json'
                or type(evidence.bytes) is not int or not 2 <= evidence.bytes <= maximum
                or source == 'output' and evidence.role != 'document'):
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_EVIDENCE_INVALID')
        raw = reader.read_json(reference, maximum)
        check_active()
        if (not isinstance(raw, bytes) or len(raw) != evidence.bytes
                or sha256(raw).hexdigest() != reference.sha256):
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_BYTES_INVALID')
        return bytes(raw)

    try:
        if any(value.int == 0 for value in (tenant, actor, parent, params_ref.artifact_id, root.artifact_id)):
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_SCOPE_INVALID')
        params_raw = read(params_ref, 65536)
        params = V2DeliveryAssembleParams.model_validate_json(params_raw)
        if canonical_bytes(params) != params_raw:
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_PARAMS_INVALID')
        envelope_raw = read(params.assembly, 65536)
        envelope = SignedDeliveryAssemblyEnvelope.model_validate_json(envelope_raw)
        if canonical_bytes(envelope) != envelope_raw:
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_ENVELOPE_INVALID')
        assembly_raw = read(envelope.assembly, 1048576)
        closure_raw = read(envelope.signing_closure, 8388608)
        assembly = DeliveryAssemblyInput.model_validate_json(assembly_raw)
        closure = SignedDeliveryAssemblyClosure.model_validate_json(closure_raw)
        if (canonical_bytes(assembly) != assembly_raw or canonical_bytes(closure) != closure_raw
                or assembly.policy.provenance != 'required'):
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_INPUT_INVALID')
        input_ids = {params_ref.artifact_id, params.assembly.artifact_id,
                     envelope.assembly.artifact_id, envelope.signing_closure.artifact_id}
        input_ids.update(ref.artifact_id for ref in assembly.shards)
        input_ids.update(preset.config.artifact_id for preset in assembly.presets)
        input_ids.update(entry.export.artifact_id for entry in closure.entries)
        input_ids.update(entry.signed.artifact_id for entry in closure.entries)
        input_ids.update(entry.manifest.artifact_id for entry in closure.entries)
        if root.artifact_id in input_ids or len(input_ids) < 4 + len(assembly.shards):
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_ALIAS')
        expected = assemble_signed_delivery(params.assembly, envelope_raw,
            lambda ref: read(ref, 8388608 if ref.artifact_id == envelope.signing_closure.artifact_id else 1048576),
            tenant, actor, parent, lambda: (check_active(), False)[1])
        result_raw = read(root, 8388608, 'output')
        if canonical_bytes(expected) != result_raw:
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_RECONSTRUCTION_MISMATCH')
        if reader.staged_outputs() != {root}:
            raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_OUTPUT_CLOSURE_INVALID')
        check_active()
        return VerifiedSignedDeliveryAssembly(params_raw, result_raw)
    except AdoptionInvalid:
        raise
    except ValueError as error:
        raise AdoptionInvalid('DELIVERY_SIGNED_ASSEMBLY_INPUT_INVALID') from error
