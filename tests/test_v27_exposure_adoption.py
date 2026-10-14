from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.exposure_adoption import verify_exposure_documents
from test_v27_exposure_source import source_fixture


def fixture(change=None):
    edit, image, source_raw = source_fixture()
    output = parse_json(source_raw)
    output['pixels'] = dict(artifact_id=str(uuid4()), sha256='f'*64)
    if change == 'color':output['color']['icc_sha256'] = '0'*64
    if change == 'source_pixels':output['pixels'] = image['pixels']
    raw = canonical_bytes(output)
    params_raw = canonical_bytes(edit)
    params = ArtifactRef(uuid4(), sha256(params_raw).hexdigest())
    source = ArtifactRef(edit['source']['source']['artifact_id'], sha256(source_raw).hexdigest())
    # Source fixture uses JSON UUID strings; the evidence protocol uses UUIDs.
    from uuid import UUID
    source = ArtifactRef(UUID(str(source.artifact_id)), source.sha256)
    root = ArtifactRef(uuid4(), sha256(raw).hexdigest())
    pixels = ArtifactRef(UUID(output['pixels']['artifact_id']), output['pixels']['sha256'])
    docs = {params:params_raw, source:source_raw, root:raw}
    evidence = {ref:ArtifactEvidence(ref, 'application/json', len(body),
        'working_image' if ref == root else None, 'output' if ref == root else 'input')
        for ref, body in docs.items()}
    evidence[pixels] = ArtifactEvidence(pixels,
        'application/octet-stream' if change == 'mime' else 'application/x-npy', 100,
        'pixels', 'input' if change == 'input_pixels' else 'output')
    class Reader:
        def evidence(self, ref):return evidence[ref]
        def read_json(self, ref, limit):return docs[ref]
    return Reader(), params, root


def test_exact_source_metadata_and_attempt_pixels():
    reader, params, root = fixture()
    edit, source, output = verify_exposure_documents(reader, params, root, check_active=lambda:None)
    assert edit.kind == 'local_exposure'
    assert source.canvas == output.canvas and source.color == output.color
    assert source.pixels != output.pixels


@pytest.mark.parametrize('change', ['color', 'source_pixels', 'mime', 'input_pixels'])
def test_changed_metadata_or_unowned_pixels_denied(change):
    reader, params, root = fixture(change)
    with pytest.raises(AdoptionInvalid):
        verify_exposure_documents(reader, params, root, check_active=lambda:None)


def test_duplicate_params_key_denied_even_when_sha_matches():
    reader,params,root=fixture()
    raw=reader.read_json(params,1048576)
    duplicate=b'{"schema_version":1,'+raw[1:]
    replacement=ArtifactRef(params.artifact_id,sha256(duplicate).hexdigest())
    class Reader:
        def evidence(self,ref):
            if ref==replacement:return ArtifactEvidence(ref,'application/json',len(duplicate),None,'input')
            return reader.evidence(ref)
        def read_json(self,ref,limit):
            return duplicate if ref==replacement else reader.read_json(ref,limit)
    with pytest.raises(AdoptionInvalid,match='CONTRACT_INVALID'):
        verify_exposure_documents(Reader(),replacement,root,check_active=lambda:None)
