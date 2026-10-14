"""Reconstruct DeliveryPreparation independently from pinned input documents."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.delivery import V2DeliveryPrepareParams, DeliveryPreparation
from photo_worker.contracts.export_preset import validate_frozen_export_preset
from photo_worker.operations.delivery.preparation import prepare_delivery
from .adoption import AdoptionInvalid, ArtifactRef, require_adoption_active


@dataclass(frozen=True)
class VerifiedDeliveryPreparation:
    params_jcs: bytes
    result_jcs: bytes


def verify_delivery_preparation(reader, params_ref, root, *, check_active=lambda: None):
    if not callable(check_active):
        raise AdoptionInvalid('DELIVERY_PREPARATION_ACTIVITY_INVALID')
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    def read(ref, maximum, source='input'):
        check_active()
        ref = ArtifactRef(ref.artifact_id, ref.sha256)
        evidence = reader.evidence(ref)
        if (evidence.ref != ref or evidence.source != source
                or evidence.media_type != 'application/json'
                or type(evidence.bytes) is not int or not 2 <= evidence.bytes <= maximum
                or source == 'output' and evidence.role != 'document'):
            raise AdoptionInvalid('DELIVERY_PREPARATION_EVIDENCE_INVALID')
        raw = reader.read_json(ref, maximum)
        check_active()
        if (not isinstance(raw, bytes) or len(raw) != evidence.bytes
                or sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('DELIVERY_PREPARATION_BYTES_INVALID')
        return bytes(raw)

    try:
        params_raw = read(params_ref, 65536)
        params = V2DeliveryPrepareParams.model_validate_json(params_raw)
        if canonical_bytes(params) != params_raw:
            raise AdoptionInvalid('DELIVERY_PREPARATION_PARAMS_INVALID')
        if root.artifact_id in {params_ref.artifact_id, params.input.artifact_id}:
            raise AdoptionInvalid('DELIVERY_PREPARATION_OUTPUT_ALIAS')
        frozen_raw = read(params.input, 1048576)
        expected = prepare_delivery(params.input, frozen_raw,
            lambda ref: read(ref, 8388608), validate_frozen_export_preset,
            lambda: (check_active(), False)[1])
        result_raw = read(root, 8388608, 'output')
        # Exact bytes reject duplicate keys, ignored fields and noncanonical JSON.
        if canonical_bytes(expected) != result_raw:
            raise AdoptionInvalid('DELIVERY_PREPARATION_RECONSTRUCTION_MISMATCH')
        DeliveryPreparation.model_validate_json(result_raw)
        if reader.staged_outputs() != {root}:
            raise AdoptionInvalid('DELIVERY_PREPARATION_OUTPUT_CLOSURE_INVALID')
        check_active()
        return VerifiedDeliveryPreparation(params_raw, result_raw)
    except AdoptionInvalid:
        raise
    except ValueError as error:
        raise AdoptionInvalid('DELIVERY_PREPARATION_INPUT_INVALID') from error
