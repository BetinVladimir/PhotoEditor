"""CPU export execution; activation requires independent admission and preparation."""
from contextlib import contextmanager
from copy import deepcopy
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from photo_worker.v2.interchange_export_prepare import PreparedExportBundle, ExportSource, bind_prepared_export_bundle
from photo_worker.v2.interchange_archive import ArchiveLimits, verify_archive
from photo_worker.v2.interchange_manifest import verify_bundle_manifest
from photo_worker.v2.interchange_export_sidecars import PreparedSidecarExport, FrozenSidecar
from photo_worker.v2.interchange_export_result import publish_export_result
from photo_worker.v2.interchange_input_reader import PreparedImportInputReader
from photo_worker.v2.interchange_mapping import MappingEntry
from photo_worker.v2.interchange_plan import InterchangeExportParams, read_interchange_plan
from photo_worker.v2.interchange_read_budget import ImportReadBudget
from photo_worker.v2.interchange_sidecar_archive import write_sidecar_archive


@dataclass(frozen=True)
class ExportAuthority:
    workflow_id: UUID
    project_id: UUID
    snapshot_digest: str
    max_archive_bytes: int
    max_output_bytes: int
    max_input_json_bytes: int


@dataclass(frozen=True)
class PreparedExportWork:
    archive: PreparedExportBundle | PreparedSidecarExport
    mappings: tuple[MappingEntry, ...]


@dataclass(frozen=True)
class ExportInstallation:
    admission: object
    prepare: object


class _ChunkReader:
    """Bounded adapter for a cancellable, hash-verifying block iterator."""
    def __init__(self, blocks):
        self.blocks, self.pending = iter(blocks), b''

    def read(self, maximum):
        if type(maximum) is not int or not 1 <= maximum <= 1 << 20:
            raise ValueError('INTERCHANGE_EXPORT_STREAM_BUDGET')
        output = bytearray()
        while len(output) < maximum:
            if not self.pending:
                self.pending = next(self.blocks, b'')
                if not self.pending:
                    break
            take = min(maximum - len(output), len(self.pending))
            output.extend(self.pending[:take]);self.pending = self.pending[take:]
        return bytes(output)


def execute_export(ctx, artifacts, installation):
    ctx.check_active()
    command, profile = ctx.lease.command, deepcopy(ctx.lease.execution_profile)
    estimate = profile.get('resource_estimate', {})
    if (command.operation != 'v2.interchange.export' or command.operation_version != '1'
            or command.resource_class != 'cpu' or profile.get('backend') != 'cpu'
            or profile.get('renderer_version') != 'v2.2' or not isinstance(estimate, dict)
            or type(estimate.get('vram_bytes')) is not int or estimate['vram_bytes'] != 0
            or any(type(estimate.get(name)) is not int or not 1 <= estimate[name] <= 2**53-1
                   for name in ('ram_bytes', 'scratch_bytes', 'output_bytes'))
            or not isinstance(profile.get('contracts_sha'), str) or len(profile['contracts_sha']) != 64
            or any(c not in '0123456789abcdef' for c in profile['contracts_sha'])
            or type(profile.get('max_input_json_bytes')) is not int
            or not 1 <= profile['max_input_json_bytes'] <= 128 << 20
            or estimate['ram_bytes'] < profile['max_input_json_bytes']):
        raise OperationFailure('CONFIG_INVALID')
    if (not isinstance(installation, ExportInstallation)
            or not callable(getattr(installation.admission, 'admit', None))
            or not callable(installation.prepare)):
        raise OperationFailure('INTERCHANGE_EXPORT_INSTALLATION_REQUIRED')
    try:
        params = artifacts.params(InterchangeExportParams)
        if sha256(canonical_bytes(params)).hexdigest() != command.params.sha256:
            raise ValueError('INTERCHANGE_EXPORT_PARAMS_HASH')
        authority = installation.admission.admit(ctx, params.model_copy(deep=True))
        if (not isinstance(authority, ExportAuthority)
                or any(not isinstance(value, UUID) or value.int == 0 for value in (authority.workflow_id, authority.project_id))
                or not isinstance(authority.snapshot_digest, str) or len(authority.snapshot_digest) != 64
                or any(c not in '0123456789abcdef' for c in authority.snapshot_digest)
                or type(authority.max_archive_bytes) is not int or not 1 <= authority.max_archive_bytes <= 64 << 30
                or type(authority.max_output_bytes) is not int or not authority.max_archive_bytes <= authority.max_output_bytes <= 2**53-1
                or type(authority.max_input_json_bytes) is not int
                or authority.max_input_json_bytes != profile['max_input_json_bytes']
                or authority.max_archive_bytes > estimate['scratch_bytes']
                or authority.max_output_bytes > estimate['output_bytes']):
            raise OperationFailure('INTERCHANGE_EXPORT_AUTHORITY_INVALID')
        budget = ImportReadBudget(authority.max_input_json_bytes)
        reader = PreparedImportInputReader(artifacts, read_budget=budget, check_active=ctx.check_active)
        plan_entry = reader.entries.get((params.plan.artifact_id, params.plan.sha256))
        if plan_entry is None or plan_entry.contract != 'InterchangePlan':
            raise ValueError('INTERCHANGE_EXPORT_PLAN_CONTRACT')
        plan = read_interchange_plan(reader.read_json(params.plan, 8 << 20), project_id=authority.project_id,
            direction='export', snapshot_digest=authority.snapshot_digest)

        def current():
            ctx.check_active()
            if installation.admission.admit(ctx, params.model_copy(deep=True)) != authority:
                raise OperationFailure('INTERCHANGE_SOURCE_STALE')
            reader.recheck()
            ctx.check_active()

        work = installation.prepare(ctx, artifacts, authority, plan.model_copy(deep=True), reader)
        if (not isinstance(work, PreparedExportWork) or not isinstance(work.mappings, tuple)
                or plan.format == 'photoeditor_bundle' and not isinstance(work.archive, PreparedExportBundle)
                or plan.format != 'photoeditor_bundle' and not isinstance(work.archive, PreparedSidecarExport)):
            raise ValueError('INTERCHANGE_EXPORT_PREPARATION_INVALID')
        mappings = tuple(MappingEntry.model_validate_json(canonical_bytes(item)) for item in work.mappings)
        if isinstance(work.archive, PreparedExportBundle):
            archive = bind_prepared_export_bundle(plan, work.archive)
        else:
            candidate = work.archive
            if candidate.plan_sha256 != sha256(canonical_bytes(plan)).hexdigest():
                raise ValueError('INTERCHANGE_EXPORT_PREPARED_PLAN')
            archive = PreparedSidecarExport(tuple(ExportSource(
                item.reference.model_copy(deep=True), item.path, item.contract, item.bytes, item.sidecar)
                for item in candidate.images), tuple(FrozenSidecar(item.path, bytes(item.payload))
                for item in candidate.sidecars), tuple(bytes(raw) for raw in candidate.mapping_jcs), candidate.plan_sha256)
        work = PreparedExportWork(archive, mappings)
        current()
        written = 0
        @contextmanager
        def open_source(ref):
            metadata = reader.evidence(ref)
            blocks = reader.iter_bytes(ref, metadata.bytes)
            try:
                yield _ChunkReader(blocks)
            finally:
                blocks.close()
        with artifacts.scratch.reserve(authority.max_archive_bytes) as reservation:
            path = reservation.path('bundle', '.zip')
            with artifacts.scratch.open_writer(path, reservation) as output:
                if isinstance(work.archive, PreparedExportBundle):
                    if work.archive.limits.archive_bytes > authority.max_archive_bytes:
                        raise ValueError('INTERCHANGE_EXPORT_ARCHIVE_QUOTA')
                    size = work.archive.write(output, open_source, active=ctx.check_active)
                else:
                    size = write_sidecar_archive(output, plan, work.archive, open_source,
                        tenant_archive_bytes=authority.max_archive_bytes, active=ctx.check_active)
            reservation.validate_file(path)
            digest, observed = sha256(), 0
            with path.open('rb') as source:
                for block in iter(lambda: source.read(1 << 20), b''):
                    ctx.check_active();observed += len(block);digest.update(block)
            if observed != size or size > authority.max_archive_bytes:
                raise ValueError('INTERCHANGE_EXPORT_ARCHIVE_SIZE')
            # Read back the completed ZIP before exposing any bundle receipt.
            import zipfile
            def cancelled():
                ctx.check_active()
                return False
            limits = ArchiveLimits(min(plan.max_archive_bytes, authority.max_archive_bytes),
                                   plan.max_expanded_bytes, plan.max_files)
            with path.open('rb') as source:
                physical = verify_archive(source, limits, cancelled=cancelled)
                with zipfile.ZipFile(source, 'r') as archive:
                    info = archive.getinfo('manifest.json')
                    if info.file_size > 8 << 20:
                        raise ValueError('INTERCHANGE_EXPORT_MANIFEST_SIZE')
                    with archive.open(info) as content:
                        manifest_bytes = content.read((8 << 20) + 1)
                    verify_bundle_manifest(manifest_bytes, physical)
            current()
            bundle = artifacts.stage(path, reservation, 'bundle', 'application/zip')
            if bundle.sha256 != digest.hexdigest():
                raise ValueError('INTERCHANGE_EXPORT_BUNDLE_HASH')
            written = size
            current()
        outputs = [bundle]
        def stage(document, role):
            nonlocal written
            current()
            size = len(canonical_bytes(document))
            if written + size > authority.max_output_bytes:
                raise ValueError('INTERCHANGE_EXPORT_OUTPUT_QUOTA')
            result = artifacts.stage_contract(document, role)
            written += size;outputs.append(result)
            current()
            return result
        def publish(body, contract, ordinal):
            from photo_worker.v2.interchange_mapping import MappingShard, MappingReport
            model, role = (MappingShard, 'mapping_shard') if contract == 'MappingShard' else (MappingReport, 'report')
            return stage(model.model_validate_json(body), role)
        inputs = tuple(entry.ref.model_copy(deep=True) for entry in reader.entries.values())
        result = publish_export_result(plan, bundle, digest.hexdigest(), mappings, inputs, publish,
            lambda document: stage(document, 'document'), max_report_bytes=params.max_report_bytes,
            check_active=current)
        current()
        return OperationResult(outputs=[result.root, *[ref for ref in outputs if ref != result.root]],
            bytes_read=budget.input_bytes, bytes_written=written, peak_device_bytes=0)
    except ValueError as error:
        raise OperationFailure('INPUT_INVALID') from error


def interchange_export_operation(config, installation):
    if not isinstance(installation, ExportInstallation):
        raise ValueError('INTERCHANGE_EXPORT_INSTALLATION_REQUIRED')
    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'bundle', 'report', 'mapping_shard', 'document'})) as artifacts:
            return execute_export(ctx, artifacts, installation)
    return Operation('v2.interchange.export', 'cpu', run)
