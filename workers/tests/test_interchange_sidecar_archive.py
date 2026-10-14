from contextlib import contextmanager
from io import BytesIO
import zipfile
import pytest
from photo_worker.v2.interchange_export_sidecars import ExportImageEvidence, prepare_export_sidecars
from photo_worker.v2.interchange_sidecar_archive import write_sidecar_archive
from photo_worker.v2.interchange_manifest import verify_bundle_manifest
from photo_worker.v2.interchange_archive import ArchiveLimits, verify_archive
from test_interchange_export_prepare import fixture


def setup():
    plan, sources, bodies = fixture();plan.format = 'captureone_sidecar'
    prepared = prepare_export_sidecars(plan, qualify_image=lambda entry: ExportImageEvidence(
        entry.content, sources[0].bytes, 'image/tiff', 1))
    return plan, prepared, bodies


def test_sidecar_archive_is_deterministic_and_has_verified_normalized_manifest():
    plan, prepared, bodies = setup()
    @contextmanager
    def open_image(ref):
        yield BytesIO(bodies[ref.artifact_id])
    outputs = []
    for _ in range(2):
        output = BytesIO()
        size = write_sidecar_archive(output, plan, prepared, open_image, tenant_archive_bytes=1 << 20)
        assert size == len(output.getvalue())
        outputs.append(output.getvalue())
    assert outputs[0] == outputs[1]
    source = BytesIO(outputs[0])
    inventory = verify_archive(source, ArchiveLimits(1 << 20, 1 << 20, 10))
    with zipfile.ZipFile(BytesIO(outputs[0])) as archive:
        assert archive.namelist() == ['images/one.tiff', 'images/one.xmp', 'manifest.json']
        manifest = verify_bundle_manifest(archive.read('manifest.json'), inventory)
    assert manifest.entries[0].recipe_path is None
    assert {item.contract for item in manifest.inventory} == {'EncodedImage', 'XMP'}


@pytest.mark.parametrize('mode', ['metadata', 'identity', 'quota', 'hash'])
def test_sidecar_archive_rejects_changed_preparation_or_bytes(mode):
    from uuid import uuid4
    plan, prepared, bodies = setup();opened = []
    if mode == 'metadata': plan.entries[0].rating = -1
    elif mode == 'identity': prepared.images[0].reference.artifact_id = uuid4()
    elif mode == 'quota': plan.max_files = 2
    @contextmanager
    def open_image(ref):
        opened.append(ref)
        yield BytesIO(b'wrong' if mode == 'hash' else bodies[ref.artifact_id])
    with pytest.raises(ValueError):
        write_sidecar_archive(BytesIO(), plan, prepared, open_image, tenant_archive_bytes=1 << 20)
    if mode != 'hash':
        assert opened == []


@pytest.mark.parametrize('mode', ['roundtrip', 'reordered_inventory', 'rating', 'contract'])
def test_own_sidecar_export_import_and_independent_adoption_roundtrip(tmp_path, mode):
    import json
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef as WireRef, canonical_bytes
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.v2.adoption import ArtifactRef
    from photo_worker.v2.interchange_sidecar_parse import stage_sidecar_archive
    from photo_worker.v2.interchange_parse_adoption import verify_parsed_interchange
    from test_interchange_parse import Artifacts
    from test_interchange_parse_adoption import Reader
    plan, prepared, bodies = setup()

    @contextmanager
    def open_image(ref):
        yield BytesIO(bodies[ref.artifact_id])

    output = BytesIO()
    write_sidecar_archive(output, plan, prepared, open_image, tenant_archive_bytes=1<<20)
    raw = output.getvalue()
    with zipfile.ZipFile(BytesIO(raw)) as archive:
        members = {name: archive.read(name) for name in archive.namelist()}
    original_manifest = members['manifest.json']
    if mode != 'roundtrip':
        manifest = json.loads(original_manifest)
        if mode == 'rating':
            manifest['entries'][0]['rating'] = 5 if manifest['entries'][0]['rating'] != 5 else 0
        elif mode == 'contract':
            manifest['inventory'][0]['contract'] = 'WorkingImage'
        else:
            manifest['inventory'].reverse()
        members['manifest.json'] = canonical_bytes(manifest)
        if mode == 'reordered_inventory':
            original_manifest = members['manifest.json']
        output = BytesIO()
        with zipfile.ZipFile(output, 'w') as archive:
            for name,content in sorted(members.items()):
                archive.writestr(name, content)
        raw = output.getvalue()
    path = tmp_path / 'roundtrip.zip'
    path.write_bytes(raw)
    reader = Reader()
    bundle = reader.add(raw, 'input', 'application/zip')
    params = reader.add(canonical_bytes({'schema_version':1,'request':{
        'project_id':str(plan.project_id),'direction':'import','format':'captureone_sidecar',
        'bundle':{'artifact_id':str(bundle.artifact_id),'sha256':bundle.sha256}},
        'max_expanded_bytes':1<<20,'max_files':30}), 'input', 'application/json')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        artifacts = Artifacts(scratch,path)
        call = lambda:stage_sidecar_archive(artifacts,
            WireRef(artifact_id=bundle.artifact_id,sha256=bundle.sha256),
            ArchiveLimits(1<<20,1<<20,30),format='captureone_sidecar',active=lambda:None)
        if mode in ('rating', 'contract'):
            with pytest.raises(ValueError,match='MANIFEST_MISMATCH'):
                call()
            assert artifacts.uploads == []
        else:
            staged = call()
            assert canonical_bytes(staged.document.manifest) == original_manifest
            assert staged.document.manifest.entries[0].entry_id == plan.entries[0].entry_id
            assert len(staged.outputs) == 2
            assert [item.path for item in staged.document.files] == sorted(item.path for item in staged.document.files)
            for reference,content in zip(staged.outputs,artifacts.uploads):
                reader.add(content,'output','application/octet-stream','imported_file',ArtifactRef(reference.artifact_id,reference.sha256))
            root = reader.add(canonical_bytes(staged.document),'output','application/json','document')
            verified = verify_parsed_interchange(reader,params,root,scratch)
            assert verified.result_jcs == canonical_bytes(staged.document)
            assert verified.bytes_written == staged.bytes_written+len(verified.result_jcs)
