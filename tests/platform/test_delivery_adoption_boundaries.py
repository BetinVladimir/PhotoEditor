"""Shared physical evidence and cancellation refusals for both V30 result families."""
from dataclasses import replace
from uuid import UUID
import pytest
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.delivery_prepare_adoption import verify_delivery_preparation
from photo_worker.v2.delivery_assembly_adoption import verify_delivery_assembly
from test_delivery_prepare_adoption import adoption_fixture as prepare_fixture
from test_delivery_assembly_adoption import adoption_fixture as assembly_fixture


@pytest.fixture(params=[(prepare_fixture, verify_delivery_preparation),
                        (assembly_fixture, verify_delivery_assembly)], ids=['prepare', 'assemble'])
def case(request):
    factory, verify = request.param
    reader, params, root = factory()
    return reader, params, root, verify


@pytest.mark.parametrize('target', ['params', 'result'])
@pytest.mark.parametrize('change', ['bool_size', 'float_size', 'too_small', 'too_large',
                                    'wrong_ref', 'wrong_sha', 'mime', 'source'])
def test_evidence_must_match_exact_bound_artifact(case, target, change):
    reader, params, root, verify = case
    reference = params if target == 'params' else root
    evidence = reader.metadata[reference.artifact_id]
    updates = {
        'bool_size': {'bytes': True},
        'float_size': {'bytes': float(evidence.bytes)},
        'too_small': {'bytes': 1},
        'too_large': {'bytes': (65536 if target == 'params' else 8388608) + 1},
        'wrong_ref': {'ref': ArtifactRef(UUID(int=999), reference.sha256)},
        'wrong_sha': {'ref': ArtifactRef(reference.artifact_id, '0' * 64)},
        'mime': {'media_type': 'application/octet-stream'},
        'source': {'source': 'output' if target == 'params' else 'input'},
    }
    reader.metadata[reference.artifact_id] = replace(evidence, **updates[change])
    with pytest.raises(AdoptionInvalid):
        verify(reader, params, root)


def test_cancellation_after_output_read_cannot_return_commit_proof(case):
    reader, params, root, verify = case
    original_read = reader.read_json
    output_read = []

    def read(reference, maximum):
        raw = original_read(reference, maximum)
        if reference == root:
            output_read.append(True)
        return raw

    class Cancelled(Exception):
        pass

    def active():
        if output_read:
            raise Cancelled()

    reader.read_json = read
    with pytest.raises(Cancelled):
        verify(reader, params, root, check_active=active)
    assert output_read == [True]


@pytest.mark.parametrize('raw', [bytearray(b'{}'), '{}', None])
def test_nonbytes_storage_payload_cannot_be_adopted(case, raw):
    reader, params, root, verify = case
    original_read = reader.read_json
    reader.read_json = lambda reference, maximum: raw if reference == root else original_read(reference, maximum)
    with pytest.raises(AdoptionInvalid):
        verify(reader, params, root)
