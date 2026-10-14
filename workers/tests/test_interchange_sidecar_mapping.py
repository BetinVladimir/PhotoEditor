from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_parse import ParsedFile
from photo_worker.v2.interchange_sidecar_mapping import sidecar_mapping_fields
from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp, CRS, XMP


def entry(path='a.tiff', rating=3, pick='picked', crop=None, label=None):
    return SimpleNamespace(image_path=path, rating=rating, pick=pick, crop=crop, label=label)


@pytest.mark.parametrize('unknown', [False, True])
def test_pinned_xmp_unsupported_claim_uses_complete_list_digest(unknown):
    raw = export_xmp(XMPMapping(3, 'picked', 1, None, ()))
    if unknown:
        raw = raw.replace(b'rdf:about=""', b'rdf:about="" crs:Exposure2012="2"')
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    files = {'a.xmp': ParsedFile(path='a.xmp', content=ref, contract='XMP')}
    reads = []

    def read(candidate, limit):
        reads.append((candidate, limit))
        return raw

    fields = sidecar_mapping_fields(entry(), files, read)
    assert reads == [(ref, 1 << 20)]
    if unknown:
        digest = sha256(canonical_bytes([CRS + '}Exposure2012'])).hexdigest()
        assert len(fields) == 1 and fields[0].status == 'unsupported'
        assert fields[0].reason == 'unsupported_xmp_properties:1:' + digest
    else:
        assert fields == []


def test_unpinned_xmp_bytes_refuse_mapping():
    ref = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    files = {'a.xmp': ParsedFile(path='a.xmp', content=ref, contract='XMP')}
    with pytest.raises(AdoptionInvalid):
        sidecar_mapping_fields(entry(), files, lambda ref, limit: b'changed')


def test_custom_color_label_is_reported_without_inventing_a_pick():
    raw = export_xmp(XMPMapping(3, None, 1, None, ())).replace(
        b'rdf:about=""', b'rdf:about="" xmp:Label="Custom Blue"')
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    files = {'a.xmp': ParsedFile(path='a.xmp', content=ref, contract='XMP')}
    fields = sidecar_mapping_fields(entry(pick='unmarked'), files,
                                    lambda candidate, limit: raw)
    digest = sha256(canonical_bytes([XMP + '}Label'])).hexdigest()
    assert len(fields) == 1
    assert fields[0].status == 'unsupported'
    assert fields[0].reason == 'unsupported_xmp_properties:1:' + digest

def test_standard_label_stays_unapplied_until_collection_contract_exists():
    raw = export_xmp(XMPMapping(3, None, 1, None, (), 'Blue'))
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    files = {'a.xmp': ParsedFile(path='a.xmp', content=ref, contract='XMP')}
    fields = sidecar_mapping_fields(entry(pick='unmarked', label='Blue'), files,
                                    lambda candidate, limit: raw)
    digest = sha256(canonical_bytes([XMP + '}Label'])).hexdigest()
    assert len(fields) == 1
    assert fields[0].reason == 'unsupported_xmp_properties:1:' + digest

    assert sidecar_mapping_fields(entry(pick='unmarked', label='Blue'),
        files, lambda candidate, limit: raw, applied_label=True) == []
    with pytest.raises(AdoptionInvalid, match='INTERCHANGE_XMP_LABEL_MISMATCH'):
        sidecar_mapping_fields(entry(pick='unmarked', label='Red'),
            files, lambda candidate, limit: raw, applied_label=True)


def test_no_xmp_dependency_does_not_read():
    def read(*args):
        pytest.fail('unexpected read')
    assert sidecar_mapping_fields(entry(), {}, read) == []


@pytest.mark.parametrize('field,value', [
    ('rating', 2), ('pick', 'rejected'), ('crop', SimpleNamespace(x0=0., y0=0., x1=.5, y1=.5)),
])
def test_xmp_replay_rejects_metadata_divergence(field, value):
    raw = export_xmp(XMPMapping(3, 'picked', 1, (.1, .2, .7, .8), ()))
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    files = {'a.xmp': ParsedFile(path='a.xmp', content=ref, contract='XMP')}
    entry = SimpleNamespace(image_path='a.tiff', rating=3, pick='picked', label=None,
                            crop=SimpleNamespace(x0=.1, y0=.2, x1=.7, y1=.8))
    setattr(entry, field, value)
    with pytest.raises(AdoptionInvalid, match='INTERCHANGE_XMP_METADATA_MISMATCH'):
        sidecar_mapping_fields(entry, files, lambda candidate, limit: raw)
