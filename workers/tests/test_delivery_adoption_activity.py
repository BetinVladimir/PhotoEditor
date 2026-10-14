"""Direct V30 delivery adoption paths refuse revoked activity before IO."""
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.delivery_prepare_adoption import verify_delivery_preparation
from photo_worker.v2.delivery_assembly_adoption import verify_delivery_assembly
from photo_worker.v2.delivery_signed_assembly_adoption import (
    verify_signed_delivery_assembly, verify_signed_delivery_dependency_bytes,
)
from photo_worker.v2.delivery_metadata_adoption_input import load_metadata_adoption_input


class ForbiddenReader:
    def evidence(self, _ref):
        raise AssertionError('cancelled delivery adoption read evidence')


@pytest.mark.parametrize('verify', [verify_delivery_preparation, verify_delivery_assembly])
def test_delivery_reconstruction_false_activity_prevents_object_read(verify):
    reference = ArtifactRef(uuid4(), 'a'*64)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify(ForbiddenReader(), reference, reference, check_active=lambda: False)


def test_signed_delivery_false_activity_prevents_dependency_and_result_reads():
    reference = ArtifactRef(uuid4(), 'a'*64)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_signed_delivery_dependency_bytes(ForbiddenReader(), b'{}', check_active=lambda: False)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_signed_delivery_assembly(ForbiddenReader(), reference, reference,
                                        uuid4(), uuid4(), uuid4(), check_active=lambda: False)


def test_metadata_adoption_false_activity_prevents_sql():
    def forbidden(*_args, **_kwargs):
        raise AssertionError('cancelled metadata adoption reached SQL')
    connection = SimpleNamespace(autocommit=True, execute=forbidden, transaction=forbidden)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        load_metadata_adoption_input(connection, scope=object(), check_active=lambda: False)
