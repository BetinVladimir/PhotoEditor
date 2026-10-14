"""Reconstruct the exact manifest from physically verified pinned assembly bytes."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.delivery import V2DeliveryAssembleParams, DeliveryAssemblyInput
from photo_worker.operations.delivery.assembly import assemble_delivery
from .adoption import AdoptionInvalid, ArtifactRef, require_adoption_active


@dataclass(frozen=True)
class VerifiedDeliveryAssembly:
    params_jcs: bytes
    result_jcs: bytes


def verify_delivery_assembly(reader, params_ref, root, *, check_active=lambda: None):
    if not callable(check_active):
        raise AdoptionInvalid('DELIVERY_ASSEMBLY_ACTIVITY_INVALID')
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
            raise AdoptionInvalid('DELIVERY_ASSEMBLY_EVIDENCE_INVALID')
        raw = reader.read_json(reference, maximum)
        check_active()
        if (not isinstance(raw, bytes) or len(raw) != evidence.bytes
                or sha256(raw).hexdigest() != reference.sha256):
            raise AdoptionInvalid('DELIVERY_ASSEMBLY_BYTES_INVALID')
        return bytes(raw)

    try:
        params_raw = read(params_ref, 65536)
        params = V2DeliveryAssembleParams.model_validate_json(params_raw)
        if canonical_bytes(params) != params_raw:
            raise AdoptionInvalid('DELIVERY_ASSEMBLY_PARAMS_INVALID')
        assembly_raw = read(params.assembly, 1048576)
        assembly = DeliveryAssemblyInput.model_validate_json(assembly_raw)
        input_ids = {params_ref.artifact_id, params.assembly.artifact_id}
        input_ids.update(ref.artifact_id for ref in assembly.shards)
        input_ids.update(preset.config.artifact_id for preset in assembly.presets)
        if root.artifact_id in input_ids:
            raise AdoptionInvalid('DELIVERY_ASSEMBLY_OUTPUT_ALIAS')
        expected = assemble_delivery(params.assembly, assembly_raw,
                                    lambda ref: read(ref, 1048576),
                                    lambda: (check_active(), False)[1])
        result_raw = read(root, 8388608, 'output')
        if canonical_bytes(expected) != result_raw:
            raise AdoptionInvalid('DELIVERY_ASSEMBLY_RECONSTRUCTION_MISMATCH')
        if reader.staged_outputs() != {root}:
            raise AdoptionInvalid('DELIVERY_ASSEMBLY_OUTPUT_CLOSURE_INVALID')
        check_active()
        return VerifiedDeliveryAssembly(params_raw, result_raw)
    except AdoptionInvalid:
        raise
    except ValueError as error:
        raise AdoptionInvalid('DELIVERY_ASSEMBLY_INPUT_INVALID') from error
