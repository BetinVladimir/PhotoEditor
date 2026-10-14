from types import SimpleNamespace
import pytest
from photo_worker.operations.interchange.export_adapter import _ChunkReader, execute_export, ExportInstallation
from photo_worker.runtime.executor import OperationFailure


def context():
    return SimpleNamespace(check_active=lambda: None, lease=SimpleNamespace(
        command=SimpleNamespace(operation='v2.interchange.export', operation_version='1', resource_class='cpu'),
        execution_profile={'backend': 'cpu', 'renderer_version': 'v2.2',
                           'contracts_sha': 'a'*64, 'max_input_json_bytes': 128 << 20,
                           'resource_estimate': {'vram_bytes': 0, 'ram_bytes': 1 << 30,
                                                 'scratch_bytes': 2 << 20, 'output_bytes': 2 << 20}}))


@pytest.mark.parametrize('mode', ['operation', 'version', 'resource', 'backend', 'renderer', 'vram', 'contracts', 'json', 'ram', 'scratch', 'output'])
def test_export_adapter_rejects_wrong_profile_before_artifact_io(mode):
    ctx = context()
    if mode == 'operation': ctx.lease.command.operation = 'v2.interchange.import'
    elif mode == 'version': ctx.lease.command.operation_version = '2'
    elif mode == 'resource': ctx.lease.command.resource_class = 'gpu'
    elif mode == 'backend': ctx.lease.execution_profile['backend'] = 'mps'
    elif mode == 'renderer': ctx.lease.execution_profile['renderer_version'] = 'unknown'
    elif mode == 'vram': ctx.lease.execution_profile['resource_estimate']['vram_bytes'] = True
    elif mode == 'contracts': ctx.lease.execution_profile['contracts_sha'] = 'invalid'
    elif mode == 'json': ctx.lease.execution_profile['max_input_json_bytes'] = True
    elif mode == 'ram': ctx.lease.execution_profile['resource_estimate']['ram_bytes'] = 1
    elif mode == 'scratch': ctx.lease.execution_profile['resource_estimate']['scratch_bytes'] = -1
    elif mode == 'output': ctx.lease.execution_profile['resource_estimate']['output_bytes'] = 2**53
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        execute_export(ctx, None, None)


def test_export_adapter_requires_explicit_authority_and_preparation():
    with pytest.raises(OperationFailure, match='INSTALLATION_REQUIRED'):
        execute_export(context(), None, ExportInstallation(object(), lambda *args: None))


def test_export_chunk_reader_preserves_boundaries_and_drains_generator_verification():
    finished = []
    def blocks():
        yield b'ab'
        yield b'cdef'
        finished.append(True)
    reader = _ChunkReader(blocks())
    assert reader.read(3) == b'abc'
    assert finished == []
    assert reader.read(3) == b'def'
    assert reader.read(1) == b''
    assert finished == [True]


@pytest.mark.parametrize('limit', [-1, 0, True, (1 << 20) + 1])
def test_export_chunk_reader_rejects_unbounded_reads(limit):
    with pytest.raises(ValueError):
        _ChunkReader(iter([b'data'])).read(limit)


def executable_fixture(tmp_path, format_name='photoeditor_bundle'):
    from contextlib import contextmanager
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef, canonical_bytes
    from photo_worker.runtime.gateway import ArtifactMetadata
    from photo_worker.v2.input_closure import InputClosureEntry
    from photo_worker.v2.interchange_export_prepare import prepare_export_bundle
    from photo_worker.v2.interchange_export_sidecars import prepare_export_sidecars, ExportImageEvidence
    from photo_worker.v2.interchange_mapping import MappingEntry, MappingField
    from photo_worker.v2.interchange_plan import InterchangeExportParams
    from photo_worker.operations.interchange.export_adapter import ExportAuthority, PreparedExportWork
    from test_interchange_export_prepare import fixture
    from test_interchange_input_reader import Stream
    plan, sources, bodies = fixture();plan.format = format_name
    plan_bytes = canonical_bytes(plan)
    plan_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(plan_bytes).hexdigest())
    params = InterchangeExportParams(schema_version=1, plan=plan_ref, max_report_bytes=8388608)
    bodies[plan_ref.artifact_id] = plan_bytes
    ctx = context();ctx.lease.command.params = ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(params)).hexdigest())
    ctx.lease.execution_profile['resource_estimate']['output_bytes'] = 2 << 20
    authority = ExportAuthority(uuid4(), plan.project_id, plan.snapshot_digest, 1 << 20, 2 << 20, 128 << 20)
    class Admission:
        stale = False
        def admit(self, ctx, params):
            if self.stale:
                return ExportAuthority(uuid4(), authority.project_id, authority.snapshot_digest,
                    authority.max_archive_bytes, authority.max_output_bytes, authority.max_input_json_bytes)
            return authority
    admission = Admission()
    class Reservation:
        def path(self, role, suffix):
            path = tmp_path / (role + suffix);path.touch();return path
        def validate_file(self, path):
            assert path.is_file() and path.stat().st_size <= authority.max_archive_bytes
    class Scratch:
        @contextmanager
        def reserve(self, size):
            assert size == authority.max_archive_bytes
            yield Reservation()
        def open_writer(self, path, reservation):
            return path.open('wb')
    class Artifacts:
        scratch = Scratch()
        bad_bundle_hash = False
        revoke_after_bundle = False
        def __init__(self):
            self.published = []
        def params(self, model):
            return model.model_validate_json(canonical_bytes(params))
        def declared_input_entries(self, maximum):
            return (InputClosureEntry(ref=plan_ref, contract='InterchangePlan'),
                    *(InputClosureEntry(ref=source.reference, contract=source.contract) for source in sources))
        def input_metadata(self, ref):
            media = 'application/json' if ref.artifact_id in (plan_ref.artifact_id, sources[1].reference.artifact_id) else 'image/tiff'
            return ArtifactMetadata(len(bodies[ref.artifact_id]), media, 'a'*64)
        def stream(self, ref):
            return Stream(bodies[ref.artifact_id], self.input_metadata(ref))
        def stage(self, path, reservation, role, media):
            body = path.read_bytes();self.published.append((role, body))
            if self.revoke_after_bundle: admission.stale = True
            return ArtifactRef(artifact_id=uuid4(), sha256='0'*64 if self.bad_bundle_hash else sha256(body).hexdigest())
        def stage_contract(self, document, role):
            body = canonical_bytes(document);self.published.append((role, body))
            return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    artifacts = Artifacts()
    def prepare(ctx, artifacts, authority, frozen_plan, reader):
        if format_name == 'photoeditor_bundle':
            archive = prepare_export_bundle(frozen_plan, sources, (), qualify_recipe=lambda entry: (entry.recipe,),
                qualify_mask=lambda *args: True, tenant_archive_bytes=authority.max_archive_bytes)
            entry = frozen_plan.entries[0]
            mappings = (MappingEntry(entry_id=entry.entry_id, external_id=entry.external_id,
                asset_version_id=entry.binding.asset_version_id, proposed_document=None,
                fields=[MappingField(name='recipe', status='exact', reason=None)]),)
        else:
            archive = prepare_export_sidecars(frozen_plan, qualify_image=lambda entry: ExportImageEvidence(
                entry.content, sources[0].bytes, 'image/tiff', 1))
            mappings = tuple(MappingEntry.model_validate_json(raw) for raw in archive.mapping_jcs)
        return PreparedExportWork(archive, mappings)
    return ctx, artifacts, ExportInstallation(admission, prepare), bodies, plan_ref, sources


@pytest.mark.parametrize('format_name', ['photoeditor_bundle', 'xmp', 'captureone_sidecar'])
def test_export_adapter_full_archive_and_result_publication(tmp_path, format_name):
    from io import BytesIO
    import zipfile
    from photo_worker.v2.interchange_import_result import InterchangeResult
    ctx, artifacts, installation, bodies, plan_ref, sources = executable_fixture(tmp_path, format_name)
    result = execute_export(ctx, artifacts, installation)
    assert [role for role, _ in artifacts.published] == ['bundle', 'mapping_shard', 'report', 'document']
    assert len(result.outputs) == 4 and result.peak_device_bytes == 0
    root = InterchangeResult.model_validate_json(artifacts.published[-1][1])
    assert root.bundle == result.outputs[1] and root.report == result.outputs[3]
    assert root.draft_shards == [] and root.imported_files == []
    with zipfile.ZipFile(BytesIO(artifacts.published[0][1])) as archive:
        assert archive.read('images/one.tiff') == b'image'
    expected_read = len(bodies[plan_ref.artifact_id]) + sources[0].bytes
    if format_name == 'photoeditor_bundle': expected_read += sources[1].bytes
    assert result.bytes_read == expected_read
    assert result.bytes_written == sum(len(body) for _, body in artifacts.published)


@pytest.mark.parametrize('failure', ['hash', 'revoked'])
def test_export_adapter_does_not_publish_report_after_invalid_bundle(tmp_path, failure):
    ctx, artifacts, installation, _, _, _ = executable_fixture(tmp_path)
    artifacts.bad_bundle_hash = failure == 'hash'
    artifacts.revoke_after_bundle = failure == 'revoked'
    with pytest.raises(OperationFailure, match='INPUT_INVALID' if failure == 'hash' else 'SOURCE_STALE'):
        execute_export(ctx, artifacts, installation)
    assert [role for role, _ in artifacts.published] == ['bundle']


@pytest.mark.parametrize('change', ['json', 'scratch', 'output'])
def test_export_adapter_rejects_admission_budget_mismatch_before_reader(tmp_path, monkeypatch, change):
    import photo_worker.operations.interchange.export_adapter as module
    ctx, artifacts, installation, _, _, _ = executable_fixture(tmp_path)
    if change == 'json':ctx.lease.execution_profile['max_input_json_bytes'] = 64 << 20
    elif change == 'scratch':ctx.lease.execution_profile['resource_estimate']['scratch_bytes'] = 1
    elif change == 'output':ctx.lease.execution_profile['resource_estimate']['output_bytes'] = 1
    def unexpected(*args, **kwargs):pytest.fail('mismatched admission reached input reader')
    monkeypatch.setattr(module, 'PreparedImportInputReader', unexpected)
    with pytest.raises(OperationFailure, match='AUTHORITY_INVALID'):
        execute_export(ctx, artifacts, installation)
    assert artifacts.published == []
