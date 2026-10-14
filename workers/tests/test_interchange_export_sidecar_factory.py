from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.interchange.export_adapter import ExportAuthority
from photo_worker.operations.interchange.export_sidecar_factory import SidecarExportPreparationFactory
from photo_worker.v2.interchange_export_sidecars import ExportImageEvidence
from photo_worker.v2.interchange_read_budget import ImportReadBudget
from test_interchange_export_prepare import fixture


def setup_factory_case():
    plan, _, _ = fixture();plan.format = 'xmp'
    image = WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(), sha256='d'*64),
        canvas=Canvas(width=32, height=24, orientation=1, space_id='canonical', source_sha256='a'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1', icc_sha256='e'*64, encoding='gamma24'),
        dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
    raw = canonical_bytes(image)
    plan.entries[0].binding.source = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    plan.entries[0].binding.canvas_hash = canonical_hash(image.canvas)
    authority = ExportAuthority(uuid4(), plan.project_id, plan.snapshot_digest, 1 << 20, 2 << 20, 128 << 20)
    key = (plan.entries[0].binding.source.artifact_id, plan.entries[0].binding.source.sha256)
    class Reader:
        entries = {key: SimpleNamespace(contract='WorkingImage')}
        read_budget = ImportReadBudget(128 << 20)
        reads = 0
        rechecks = 0
        body = raw
        def read_json(self, reference, maximum):
            self.reads += 1
            assert maximum <= self.read_budget.remaining_json_bytes
            return self.body
        def recheck(self):self.rechecks += 1
    return plan, authority, Reader(), image


@pytest.mark.parametrize('format', ['xmp', 'captureone_sidecar'])
def test_sidecar_factory_qualifies_master_image_and_returns_mapping(monkeypatch, format):
    import photo_worker.operations.interchange.export_sidecar_factory as module
    plan, authority, reader, _ = setup_factory_case();plan.format = format
    original = canonical_bytes(plan);calls = []
    def qualify(entry, selected_reader, scratch, *, active):
        active();assert selected_reader is reader;calls.append(entry.content)
        # Mutation of caller's plan during physical qualification cannot alter
        # the already frozen factory plan and its preparation receipt.
        plan.entries[0].rating = 1
        return ExportImageEvidence(entry.content, 5, 'image/tiff', 6)
    monkeypatch.setattr(module, 'qualify_export_image', qualify)
    result = SidecarExportPreparationFactory('e'*64)(SimpleNamespace(check_active=lambda: None),
        SimpleNamespace(scratch=object()), authority, plan, reader)
    assert reader.reads == 1 and reader.rechecks == 1 and len(calls) == 1
    assert result.archive.plan_sha256 == sha256(original).hexdigest()
    assert len(result.mappings) == 1 and result.mappings[0].entry_id == plan.entries[0].entry_id
    assert result.archive.sidecars[0].path == 'images/one.xmp'


@pytest.mark.parametrize('mode', ['project', 'snapshot', 'archive', 'direction', 'contract', 'canvas', 'icc', 'hash'])
def test_sidecar_factory_rejects_authority_and_master_mismatch_before_image_io(monkeypatch, mode):
    import photo_worker.operations.interchange.export_sidecar_factory as module
    plan, authority, reader, image = setup_factory_case()
    if mode == 'project':authority = ExportAuthority(authority.workflow_id, uuid4(), authority.snapshot_digest, authority.max_archive_bytes, authority.max_output_bytes, authority.max_input_json_bytes)
    elif mode == 'snapshot':plan.snapshot_digest = 'f'*64
    elif mode == 'archive':plan.max_archive_bytes = authority.max_archive_bytes + 1
    elif mode == 'direction':plan.direction = 'import'
    elif mode == 'contract':next(iter(reader.entries.values())).contract = 'Artifact'
    elif mode == 'canvas':plan.entries[0].binding.canvas_hash = 'f'*64
    elif mode == 'icc':image.color.icc_sha256 = 'f'*64;reader.body = canonical_bytes(image);plan.entries[0].binding.source.sha256 = sha256(reader.body).hexdigest();reader.entries = {(plan.entries[0].binding.source.artifact_id, plan.entries[0].binding.source.sha256):SimpleNamespace(contract='WorkingImage')}
    elif mode == 'hash':reader.body = canonical_bytes(image) + b' '
    def unexpected(*args, **kwargs):pytest.fail('invalid authority reached physical image IO')
    monkeypatch.setattr(module, 'qualify_export_image', unexpected)
    with pytest.raises(ValueError):SidecarExportPreparationFactory('e'*64)(SimpleNamespace(check_active=lambda: None), SimpleNamespace(scratch=object()), authority, plan, reader)
    if mode in ('project', 'snapshot', 'archive', 'direction', 'contract'):assert reader.reads == 0
