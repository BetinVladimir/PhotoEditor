from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.interchange_export_prepare import ExportSource, prepare_export_bundle
from photo_worker.v2.interchange_manifest import BundleMask, BundlePersonOrigin
from photo_worker.v2.interchange_plan import InterchangeEntry, InterchangePlan


def test_bundle_preparation_requires_qualified_person_origin_sources():
    def ref(char):
        return ArtifactRef(artifact_id=uuid4(), sha256=char * 64)

    image, recipe, override, pixels = (ref(char) for char in 'abcd')
    mask_ref, receipt, source = (ref(char) for char in 'ef1')
    entry_id = uuid4()
    plan = InterchangePlan(schema_version=1, project_id=uuid4(), direction='export',
        format='photoeditor_bundle', snapshot_digest='2' * 64,
        entries=[InterchangeEntry(entry_id=entry_id, external_id='photo-1', path='photo.jpg',
            content=image, binding=VersionBinding(asset_version_id=uuid4(), source=source,
                canvas_hash='3' * 64), recipe=recipe, rating=0, pick='unmarked', crop=None)],
        max_archive_bytes=1 << 20, max_expanded_bytes=1 << 20, max_files=20)
    pixel_path = f'dependencies/{pixels.artifact_id}.bin'
    receipt_path = f'origins/{entry_id}/{mask_ref.artifact_id}.json'
    files = (
        ExportSource(image, 'photo.jpg', 'EncodedImage', 100, False),
        ExportSource(recipe, f'recipes/{recipe.artifact_id}.json', 'EditingRecipeV3', 100, True),
        ExportSource(override, f'dependencies/{override.artifact_id}.json', 'Override', 100, True),
        ExportSource(pixels, pixel_path, 'MaskPixels', 100, False),
        ExportSource(mask_ref, f'dependencies/{mask_ref.artifact_id}.json', 'MaskRef', 100, True),
        ExportSource(receipt, receipt_path, 'PersonOriginReceipt', 100, True),
    )
    origin = BundlePersonOrigin(entry_id=entry_id, recipe=recipe, override=override,
        mask_ref=mask_ref, mask_pixels=pixels, path=receipt_path, sha256=receipt.sha256)
    kwargs = dict(qualify_recipe=lambda _: (recipe, override, pixels),
        qualify_mask=lambda *_: True, tenant_archive_bytes=1 << 20,
        person_origins=(origin,))
    mask = BundleMask(path=pixel_path, sha256=pixels.sha256, canvas_hash='3' * 64)
    with pytest.raises(ValueError, match='PERSON_ORIGIN_INSTALLATION'):
        prepare_export_bundle(plan, files, (mask,), **kwargs)
    prepared = prepare_export_bundle(plan, files, (mask,),
        **(kwargs | {'qualify_person_origin': lambda *_: True}))
    assert any(item.reference == receipt and item.contract == 'PersonOriginReceipt'
               for item in prepared.sources)
    assert b'person_origins' in prepared.manifest_jcs
    with pytest.raises(ValueError, match='PERSON_ORIGIN_QUALIFICATION'):
        prepare_export_bundle(plan, files, (mask,),
            **(kwargs | {'qualify_person_origin': lambda *_: False}))
