from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import zipfile
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.interchange_plan import InterchangePlan, InterchangeEntry
from photo_worker.v2.interchange_export_prepare import ExportSource, prepare_export_bundle


def fixture():
    image, recipe = b'image', b'{}'
    image_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(image).hexdigest())
    recipe_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(recipe).hexdigest())
    binding = VersionBinding(asset_version_id=uuid4(), source=ArtifactRef(
        artifact_id=uuid4(), sha256='a'*64), canvas_hash='b'*64)
    plan = InterchangePlan(schema_version=1, project_id=uuid4(), direction='export',
        format='photoeditor_bundle', snapshot_digest='c'*64,
        entries=[InterchangeEntry(entry_id=uuid4(), external_id='one', path='images/one.tiff',
            content=image_ref, binding=binding, recipe=recipe_ref, rating=4, pick='picked', crop=None)],
        max_archive_bytes=1 << 20, max_expanded_bytes=1 << 20, max_files=10)
    sources = (ExportSource(image_ref, 'images/one.tiff', 'EncodedImage', len(image), False),
               ExportSource(recipe_ref, 'recipes/one.json', 'EditingRecipeV3', len(recipe), True))
    return plan, sources, {image_ref.artifact_id: image, recipe_ref.artifact_id: recipe}


def prepare(plan, sources, **kwargs):
    return prepare_export_bundle(plan, sources, (), qualify_recipe=kwargs.pop('qualify_recipe',
        lambda entry: (entry.recipe,)), qualify_mask=lambda *args: True,
        tenant_archive_bytes=1 << 20, **kwargs)


def test_prepared_export_writes_exact_deterministic_inventory_and_owns_refs():
    plan, sources, bodies = fixture()
    prepared = prepare(plan, sources)
    sources[0].reference.artifact_id = uuid4()
    opened = []
    @contextmanager
    def open_source(ref):
        opened.append(ref.artifact_id)
        body = bodies[ref.artifact_id]
        ref.artifact_id = uuid4()
        yield BytesIO(body)
    first, second = BytesIO(), BytesIO()
    prepared.write(first, open_source)
    prepared.write(second, open_source)
    assert first.getvalue() == second.getvalue()
    with zipfile.ZipFile(BytesIO(first.getvalue())) as archive:
        assert archive.namelist() == ['images/one.tiff', 'manifest.json', 'recipes/one.json']
        assert archive.read('images/one.tiff') == b'image'
    assert len(opened) == 4


@pytest.mark.parametrize('change', ['wrong_content', 'missing_recipe', 'extra_file', 'collision', 'quota', 'format'])
def test_prepared_export_rejects_invalid_inventory_before_io(change):
    plan, sources, _ = fixture()
    if change == 'wrong_content':
        plan.entries[0].content = plan.entries[0].content.model_copy(deep=True)
        plan.entries[0].content.sha256 = 'd'*64
    elif change == 'missing_recipe':
        sources = sources[:1]
    elif change == 'extra_file':
        sources += (ExportSource(ArtifactRef(artifact_id=uuid4(), sha256='e'*64), 'extra.bin', 'MaskArray', 4, False),)
    elif change == 'collision':
        sources += (ExportSource(ArtifactRef(artifact_id=uuid4(), sha256='e'*64), 'IMAGES/ONE.TIFF', 'EncodedImage', 4, False),)
    elif change == 'quota':
        plan.max_expanded_bytes = 8
    elif change == 'format':
        plan.format = 'captureone_sidecar'
    with pytest.raises(ValueError):
        prepare(plan, sources)


@pytest.mark.parametrize('closure', [(), None, True])
def test_prepared_export_requires_recipe_qualification(closure):
    plan, sources, _ = fixture()
    with pytest.raises(ValueError, match='RECIPE_QUALIFICATION'):
        prepare(plan, sources, qualify_recipe=lambda entry: closure)


def test_prepared_export_modified_receipt_fails_before_source_open():
    plan, sources, _ = fixture()
    prepared = prepare(plan, sources)
    prepared.sources[0].reference.artifact_id = uuid4()
    with pytest.raises(ValueError, match='PREPARED_CHANGED'):
        prepared.write(BytesIO(), lambda ref: pytest.fail('source opened'))


@pytest.mark.parametrize('change', ['missing', 'duplicate', 'contract', 'size', 'canonical', 'compression'])
def test_prepared_export_write_rechecks_frozen_inventory_before_io(change):
    from dataclasses import replace
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.v2.interchange_manifest import BundleManifest
    plan, sources, _ = fixture()
    prepared = prepare(plan, sources)
    if change == 'missing':
        prepared = replace(prepared, sources=prepared.sources[:1])
    elif change == 'duplicate':
        prepared = replace(prepared, sources=(*prepared.sources, prepared.sources[0]))
    elif change == 'contract':
        prepared = replace(prepared, sources=(replace(prepared.sources[0], contract='Other'), *prepared.sources[1:]))
    elif change == 'size':
        prepared = replace(prepared, sources=(replace(prepared.sources[0], bytes=0), *prepared.sources[1:]))
    elif change == 'compression':
        prepared = replace(prepared, sources=(replace(prepared.sources[0], sidecar=True), *prepared.sources[1:]))
    elif change == 'canonical':
        manifest = BundleManifest.model_validate_json(prepared.manifest_jcs)
        prepared = replace(prepared, manifest_jcs=b' ' + canonical_bytes(manifest))
    output = BytesIO()
    with pytest.raises(ValueError, match='PREPARED_CHANGED'):
        prepared.write(output, lambda ref: pytest.fail('source opened'))
    assert output.getvalue() == b''


@pytest.mark.parametrize('change', ['project', 'snapshot', 'binding', 'rating', 'path', 'recipe'])
def test_prepared_export_binding_covers_entire_frozen_plan(change):
    from photo_worker.v2.interchange_export_prepare import bind_prepared_export_bundle
    plan, sources, _ = fixture();prepared = prepare(plan, sources)
    candidate = plan.model_copy(deep=True)
    if change == 'project': candidate.project_id = uuid4()
    elif change == 'snapshot': candidate.snapshot_digest = 'f'*64
    elif change == 'binding': candidate.entries[0].binding.asset_version_id = uuid4()
    elif change == 'rating': candidate.entries[0].rating = -1
    elif change == 'path': candidate.entries[0].path = 'other.tiff'
    elif change == 'recipe': candidate.entries[0].recipe.artifact_id = uuid4()
    with pytest.raises(ValueError, match='PREPARED_PLAN'):
        bind_prepared_export_bundle(candidate, prepared)


def test_prepared_export_binding_copies_refs_before_further_callbacks():
    from photo_worker.v2.interchange_export_prepare import bind_prepared_export_bundle
    plan, sources, _ = fixture();prepared = prepare(plan, sources)
    bound = bind_prepared_export_bundle(plan, prepared)
    expected = bound.sources[0].reference.model_copy(deep=True)
    prepared.sources[0].reference.artifact_id = uuid4()
    assert bound.sources[0].reference == expected


@pytest.mark.parametrize('change', ['manifest_metadata', 'contract', 'limits'])
def test_prepared_export_binding_rechecks_manifest_and_limits(change):
    from dataclasses import replace
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.v2.interchange_manifest import BundleManifest
    from photo_worker.v2.interchange_archive import ArchiveLimits
    from photo_worker.v2.interchange_export_prepare import bind_prepared_export_bundle
    plan, sources, _ = fixture();prepared = prepare(plan, sources)
    if change == 'manifest_metadata':
        manifest = BundleManifest.model_validate_json(prepared.manifest_jcs)
        manifest.entries[0].rating = -1
        prepared = replace(prepared, manifest_jcs=canonical_bytes(manifest))
    elif change == 'contract':
        modified = replace(prepared.sources[0], contract='OtherContract')
        prepared = replace(prepared, sources=(modified, *prepared.sources[1:]))
    elif change == 'limits':
        prepared = replace(prepared, limits=ArchiveLimits(2 << 20, 1 << 20, 10))
    with pytest.raises(ValueError, match='PREPARED'):
        bind_prepared_export_bundle(plan, prepared)
