from hashlib import sha256
from uuid import uuid4
import pytest
from test_interchange_parse import make_archive, Artifacts
from photo_worker.contracts.base import ArtifactRef as ContractRef, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.interchange_archive import ArchiveLimits
from photo_worker.v2.interchange_parse import stage_photoeditor_bundle
from photo_worker.v2.interchange_parse_adoption import verify_parsed_interchange, prepare_parsed_interchange_adoption


def test_parse_physical_verification_false_activity_refuses_before_object_read():
    class ForbiddenReader:
        def evidence(self, _ref):
            raise AssertionError('cancelled parse read object evidence')
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_parsed_interchange(ForbiddenReader(), object(), object(), object(),
                                  check_active=lambda: False)


class Reader:
    def __init__(self):
        self.items = {}
        self.outputs = set()

    def add(self, raw, source, media, role=None, reference=None):
        reference = reference or ArtifactRef(uuid4(), sha256(raw).hexdigest())
        self.items[reference] = [raw, ArtifactEvidence(reference, media, len(raw), role, source)]
        if source == "output":
            self.outputs.add(reference)
        return reference

    def evidence(self, ref):
        return self.items[ref][1]

    def read_json(self, ref, maximum):
        assert len(self.items[ref][0]) <= maximum
        return self.items[ref][0]

    def iter_bytes(self, ref, maximum):
        raw = self.items[ref][0]
        for offset in range(0, len(raw), 7):
            yield raw[offset:offset + 7]

    def staged_outputs(self):
        return self.outputs.copy()


@pytest.mark.parametrize("mode", ["valid", "corrupt-output", "extra-output", "altered-manifest", "altered-file-contract", "cancelled"])
def test_parse_adoption_reconstructs_source_and_output_bytes(tmp_path, mode):
    path = tmp_path / "source.zip"
    make_archive(path)
    reader = Reader()
    bundle = reader.add(path.read_bytes(), "input", "application/zip")
    params = reader.add(canonical_bytes({"schema_version": 1, "request": {
        "project_id": str(uuid4()), "direction": "import", "format": "photoeditor_bundle",
        "bundle": {"artifact_id": str(bundle.artifact_id), "sha256": bundle.sha256}},
        "max_expanded_bytes": 1 << 20, "max_files": 30}), "input", "application/json")
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        artifacts = Artifacts(scratch, path)
        staged = stage_photoeditor_bundle(artifacts, ContractRef(artifact_id=bundle.artifact_id, sha256=bundle.sha256), ArchiveLimits(1 << 20, 1 << 20, 30), lambda: None)
        for reference, raw in zip(staged.outputs, artifacts.uploads):
            reader.add(raw, "output", "application/octet-stream", "imported_file", ArtifactRef(reference.artifact_id, reference.sha256))
        document = staged.document
        if mode == "altered-manifest":
            document = document.model_copy(deep=True)
            document.manifest.entries[0].rating = 5
        if mode == "altered-file-contract":
            document = document.model_copy(deep=True)
            document.files[0].contract = 'OtherContract'
        root = reader.add(canonical_bytes(document), "output", "application/json", "document")
        if mode == "corrupt-output":
            file_ref = next(value for value in reader.outputs if value != root)
            reader.items[file_ref][0] = b"substitution"
        if mode == "extra-output":
            reader.add(b"unexpected", "output", "application/octet-stream", "imported_file")
        def active():
            if mode == "cancelled":
                raise RuntimeError("cancelled")
        call = lambda: verify_parsed_interchange(reader, params, root, scratch, check_active=active)
        if mode == "valid":
            verified = call()
            assert verified.result_jcs == canonical_bytes(document)
            assert verified.bytes_written == len(verified.result_jcs) + staged.bytes_written
            prepared = prepare_parsed_interchange_adoption(task_id=uuid4(), reader=reader,
                params_ref=params, root=root, scratch=scratch)
            inventory = list(prepared.adoption.inventory)
            assert len(inventory) == 1 + len(staged.outputs)
            assert inventory[0]["artifact_id"] == str(root.artifact_id)
            assert [item["ordinal"] for item in inventory] == list(range(len(inventory)))
            assert prepared.adoption.digest == sha256(canonical_bytes(inventory)).hexdigest()
            assert {item["artifact_id"] for item in inventory} == {str(item.artifact_id) for item in reader.outputs}
        elif mode == "cancelled":
            with pytest.raises(RuntimeError, match="cancelled"):
                call()
        else:
            with pytest.raises(AdoptionInvalid):
                call()


@pytest.mark.parametrize('format', ['xmp', 'captureone_sidecar'])
@pytest.mark.parametrize('tamper', [False, True])
def test_sidecar_parse_adoption_reconstructs_normalized_manifest_without_bundle_manifest(tmp_path, format, tamper):
    import zipfile
    from photo_worker.v2.interchange_sidecar_parse import stage_sidecar_archive
    from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp
    path = tmp_path / 'sidecar.zip'
    xmp = export_xmp(XMPMapping(4, 'picked', 6, (0.1,0.2,0.7,0.8), ()))
    with zipfile.ZipFile(path, 'w') as archive:
        archive.writestr('a.tiff', b'untrusted image pending decode')
        archive.writestr('a.xmp', xmp)
    reader = Reader()
    bundle = reader.add(path.read_bytes(), 'input', 'application/zip')
    params = reader.add(canonical_bytes({'schema_version':1,'request':{
        'project_id':str(uuid4()),'direction':'import','format':format,
        'bundle':{'artifact_id':str(bundle.artifact_id),'sha256':bundle.sha256}},
        'max_expanded_bytes':1<<20,'max_files':30}), 'input', 'application/json')
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1<<20, min_free_bytes=0) as scratch:
        artifacts = Artifacts(scratch, path)
        staged = stage_sidecar_archive(artifacts, ContractRef(artifact_id=bundle.artifact_id,sha256=bundle.sha256),
            ArchiveLimits(1<<20,1<<20,30), format=format, active=lambda:None)
        for reference,raw in zip(staged.outputs,artifacts.uploads):
            reader.add(raw,'output','application/octet-stream','imported_file',ArtifactRef(reference.artifact_id,reference.sha256))
        document = staged.document.model_copy(deep=True)
        if tamper:
            document.manifest.entries[0].rating = 0
        root = reader.add(canonical_bytes(document),'output','application/json','document')
        if tamper:
            with pytest.raises(AdoptionInvalid,match='MANIFEST_SUBSTITUTED'):
                verify_parsed_interchange(reader,params,root,scratch)
        else:
            proof = verify_parsed_interchange(reader,params,root,scratch)
            assert proof.result_jcs == canonical_bytes(document)
            assert proof.bytes_written == len(proof.result_jcs)+staged.bytes_written
            prepared = prepare_parsed_interchange_adoption(task_id=uuid4(), reader=reader,
                params_ref=params, root=root, scratch=scratch)
            import json
            ingest = json.loads(prepared.ingest_items_jcs)
            assert len(ingest) == 1 and ingest[0]['ordinal'] == 0
            assert ingest[0]['payload']['suffix'] == '.tiff'
            image = next(item for item in document.files if item.path == 'a.tiff')
            xmp_file = next(item for item in document.files if item.path == 'a.xmp')
            assert ingest[0]['artifact_id'] == str(image.content.artifact_id)
            assert all(item['artifact_id'] != str(xmp_file.content.artifact_id) for item in ingest)
            assert {item['artifact_id'] for item in prepared.adoption.inventory} == {
                str(root.artifact_id), str(image.content.artifact_id), str(xmp_file.content.artifact_id)}


@pytest.mark.parametrize('format', ['xmp', 'captureone_sidecar'])
def test_leased_sidecar_parse_through_independent_adoption_and_ingest(tmp_path, format):
    import json
    import zipfile
    from types import SimpleNamespace
    from photo_worker.operations.interchange.parse import InterchangeParseParams, ParseInstallation, execute_parse
    from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp
    path = tmp_path / 'leased-sidecar.zip'
    xmp = export_xmp(XMPMapping(2, 'picked', 1, None, ()))
    with zipfile.ZipFile(path, 'w') as archive:
        archive.writestr('a.tiff', b'opaque image for canonical decoder')
        archive.writestr('a.xmp', xmp)
    reader = Reader()
    bundle = reader.add(path.read_bytes(), 'input', 'application/zip')
    raw = canonical_bytes({'schema_version':1,'request':{
        'project_id':str(uuid4()),'direction':'import','format':format,
        'bundle':{'artifact_id':str(bundle.artifact_id),'sha256':bundle.sha256}},
        'max_expanded_bytes':1<<20,'max_files':30})
    params = reader.add(raw, 'input', 'application/json')
    task = uuid4()
    events = []
    ctx = SimpleNamespace(check_active=lambda:None, lease=SimpleNamespace(
        command=SimpleNamespace(operation='v2.interchange.parse',operation_version='1',resource_class='cpu',
            task_id=task, params=ContractRef(artifact_id=params.artifact_id,sha256=params.sha256)),
        execution_profile={'backend':'cpu','renderer_version':'v2.2'}))
    quota = ArchiveLimits(1<<20,1<<20,30)

    def admit(context, candidate):
        assert canonical_bytes(candidate) == raw
        events.append('admit')
        return quota

    with ScratchSpace(tmp_path,uuid4(),task,1,1<<20,min_free_bytes=0) as scratch:
        class LeasedArtifacts(Artifacts):
            def params(self, model):
                assert model is InterchangeParseParams
                events.append('params')
                return model.model_validate_json(raw)
            def require_declared_input(self, candidate):
                assert candidate.artifact_id == bundle.artifact_id and candidate.sha256 == bundle.sha256
                events.append('declare')
            def stage(self, *args):
                candidate = super().stage(*args)
                reader.add(self.uploads[-1],'output','application/octet-stream','imported_file',
                    ArtifactRef(candidate.artifact_id,candidate.sha256))
                events.append('file')
                return candidate
            def stage_contract(self, document, role):
                assert role == 'document'
                reference = reader.add(canonical_bytes(document),'output','application/json','document')
                events.append('root')
                return ContractRef(artifact_id=reference.artifact_id,sha256=reference.sha256)

        result = execute_parse(ctx, LeasedArtifacts(scratch,path), ParseInstallation(SimpleNamespace(admit=admit)))
        assert events == ['params','declare','admit','file','file','admit','root','admit']
        assert len(result.outputs)==3 and result.review_required is False
        root = ArtifactRef(result.outputs[0].artifact_id,result.outputs[0].sha256)
        prepared = prepare_parsed_interchange_adoption(task_id=task,reader=reader,params_ref=params,root=root,scratch=scratch)
        assert prepared.verified.bytes_written == result.bytes_written
        assert {item['artifact_id'] for item in prepared.adoption.inventory} == {str(ref.artifact_id) for ref in result.outputs}
        ingest = json.loads(prepared.ingest_items_jcs)
        assert len(ingest)==1 and ingest[0]['payload']['suffix']=='.tiff'


@pytest.mark.parametrize("boundary", ["evidence", "document", "stream", "invalid-stream"])
def test_parse_adoption_cancellation_closes_source_before_further_io(tmp_path, boundary):
    path = tmp_path / "cancel-source.zip"
    make_archive(path)
    reader = Reader()
    bundle = reader.add(path.read_bytes(), "input", "application/zip")
    params = reader.add(canonical_bytes({"schema_version": 1, "request": {
        "project_id": str(uuid4()), "direction": "import", "format": "photoeditor_bundle",
        "bundle": {"artifact_id": str(bundle.artifact_id), "sha256": bundle.sha256}},
        "max_expanded_bytes": 1 << 20, "max_files": 30}), "input", "application/json")
    cancelled = False
    events = []
    original_evidence = reader.evidence
    original_read = reader.read_json
    def evidence(reference):
        nonlocal cancelled
        result = original_evidence(reference)
        if boundary == "evidence":
            cancelled = True
        return result
    def read(reference, maximum):
        nonlocal cancelled
        events.append("read")
        if boundary == "document":
            cancelled = True
            return b"invalid document after cancellation"
        return original_read(reference, maximum)
    def stream(reference, maximum):
        nonlocal cancelled
        try:
            events.append("stream")
            cancelled = boundary == "stream"
            yield b"" if boundary == "invalid-stream" else reader.items[reference][0][:7]
            events.append("unexpected-next")
        finally:
            events.append("closed")
    def active():
        if cancelled:
            raise RuntimeError("lease lost")
    reader.evidence = evidence
    reader.read_json = read
    reader.iter_bytes = stream
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        artifacts = Artifacts(scratch, path)
        staged = stage_photoeditor_bundle(artifacts,
            ContractRef(artifact_id=bundle.artifact_id, sha256=bundle.sha256),
            ArchiveLimits(1 << 20, 1 << 20, 30), lambda: None)
        root = reader.add(canonical_bytes(staged.document), "output", "application/json", "document")
        error = AdoptionInvalid if boundary == "invalid-stream" else RuntimeError
        message = "INTERCHANGE_STREAM_INVALID" if boundary == "invalid-stream" else "lease lost"
        with pytest.raises(error, match=message):
            verify_parsed_interchange(reader, params, root, scratch, check_active=active)
    if boundary == "evidence":
        assert events == []
    elif boundary == "document":
        assert events == ["read"]
    else:
        assert events == ["read", "read", "stream", "closed"]
