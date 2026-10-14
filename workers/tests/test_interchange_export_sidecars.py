import pytest
from photo_worker.v2.interchange_export_sidecars import ExportImageEvidence, prepare_export_sidecars
from photo_worker.v2.interchange_mapping import MappingEntry
from photo_worker.v2.interchange_xmp import parse_xmp
from test_interchange_export_prepare import fixture


def prepare(plan, sources, orientation=1, media='image/tiff'):
    return prepare_export_sidecars(plan, qualify_image=lambda entry: ExportImageEvidence(
        sources[0].reference, sources[0].bytes, media, orientation))


@pytest.mark.parametrize('format_name', ['xmp', 'captureone_sidecar'])
@pytest.mark.parametrize('orientation', range(1, 9))
def test_sidecar_export_exact_metadata_roundtrip_and_reports_nonportable_recipe(format_name, orientation):
    from photo_worker.contracts.images import NormalizedRect
    plan, sources, _ = fixture()
    plan.format = format_name
    plan.entries[0].crop = NormalizedRect(x0=.1, y0=.2, x1=.8, y1=.9)
    prepared = prepare(plan, sources, orientation)
    decoded = parse_xmp(prepared.sidecars[0].payload)
    assert decoded.rating == 4 and decoded.pick == 'picked' and decoded.orientation == orientation
    assert decoded.crop == pytest.approx((.1, .2, .8, .9))
    assert decoded.unsupported == ()
    assert prepared.sidecars[0].path == 'images/one.xmp'
    report = MappingEntry.model_validate_json(prepared.mapping_jcs[0])
    statuses = {field.name: field.status for field in report.fields}
    assert statuses == dict(rating='exact', pick='exact', orientation='exact', crop='exact',
                           recipe='unsupported', raw_development='unsupported')
    assert prepared == prepare(plan, sources, orientation)


@pytest.mark.parametrize('mode', ['orientation', 'media', 'tiff_suffix', 'bytes', 'quota', 'foreign_ref'])
def test_sidecar_export_rejects_unqualified_images(mode):
    from uuid import uuid4
    plan, sources, _ = fixture();plan.format = 'captureone_sidecar'
    media, orientation, size = 'image/tiff', 1, sources[0].bytes
    ref = sources[0].reference.model_copy(deep=True)
    if mode == 'orientation': orientation = True
    elif mode == 'media': media = 'image/jpeg'
    elif mode == 'tiff_suffix': plan.entries[0].path = 'image.jpg'
    elif mode == 'bytes': size = 0
    elif mode == 'quota': plan.max_files = 2
    elif mode == 'foreign_ref': ref.artifact_id = uuid4()
    with pytest.raises(ValueError):
        prepare_export_sidecars(plan, qualify_image=lambda entry: ExportImageEvidence(ref, size, media, orientation))


def test_sidecar_export_rejects_casefold_colliding_sidecar_names():
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef
    plan, sources, _ = fixture();plan.format = 'xmp'
    second = plan.entries[0].model_copy(deep=True)
    second.entry_id = uuid4();second.external_id = 'two';second.path = 'images/ONE.jpg'
    second.content = ArtifactRef(artifact_id=uuid4(), sha256='d'*64)
    plan.entries.append(second)
    with pytest.raises(ValueError, match='PATH_COLLISION'):
        prepare_export_sidecars(plan, qualify_image=lambda entry: ExportImageEvidence(
            entry.content, 5, 'image/jpeg', 1))


def test_sidecar_export_qualification_cannot_mutate_owned_entry():
    from uuid import uuid4
    plan, _, _ = fixture();plan.format = 'xmp'
    expected_id = plan.entries[0].entry_id
    def qualify(entry):
        evidence = ExportImageEvidence(entry.content.model_copy(deep=True), 5, 'image/tiff', 1)
        entry.entry_id = uuid4();entry.rating = -1;entry.path = 'changed.tiff'
        return evidence
    prepared = prepare_export_sidecars(plan, qualify_image=qualify)
    assert prepared.images[0].path == 'images/one.tiff'
    assert parse_xmp(prepared.sidecars[0].payload).rating == 4
    assert MappingEntry.model_validate_json(prepared.mapping_jcs[0]).entry_id == expected_id
