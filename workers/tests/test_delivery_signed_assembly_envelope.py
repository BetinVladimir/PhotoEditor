from uuid import uuid4

import pytest

from photo_worker.contracts.delivery import SignedDeliveryAssemblyEnvelope


def test_signed_assembly_envelope_requires_two_distinct_documents():
    assembly, closure = uuid4(), uuid4()
    sha = 'a' * 64
    envelope = SignedDeliveryAssemblyEnvelope.model_validate({
        'schema_version': 2,
        'assembly': {'artifact_id': assembly, 'sha256': sha},
        'signing_closure': {'artifact_id': closure, 'sha256': sha},
    })
    assert envelope.assembly.artifact_id == assembly
    with pytest.raises(ValueError):
        SignedDeliveryAssemblyEnvelope.model_validate({
            'schema_version': 2,
            'assembly': {'artifact_id': assembly, 'sha256': sha},
            'signing_closure': {'artifact_id': assembly, 'sha256': sha},
        })
    with pytest.raises(ValueError):
        SignedDeliveryAssemblyEnvelope.model_validate({
            'schema_version': 1,
            'assembly': {'artifact_id': assembly, 'sha256': sha},
            'signing_closure': {'artifact_id': closure, 'sha256': sha},
        })
