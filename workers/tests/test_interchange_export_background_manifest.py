from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.interchange_manifest import BundleBackground, BundleManifest
from photo_worker.v2.interchange_export_prepare import ExportSource, prepare_export_bundle
from test_interchange_export_prepare import fixture


@pytest.mark.parametrize('mode', ['valid', 'style_unused', 'sha', 'contract', 'duplicate', 'missing_mapping', 'unmapped_closure'])
def test_background_manifest_requires_qualified_style_and_exact_source(mode):
    plan, sources, _ = fixture()
    style = ArtifactRef(artifact_id=uuid4(), sha256='d' * 64)
    background = ArtifactRef(artifact_id=uuid4(), sha256='e' * 64)
    path = f'dependencies/{background.artifact_id}.bin'
    sources += (ExportSource(style, 'dependencies/style.json', 'StyleV2', 2, True),
        ExportSource(background, path, 'EncodedImage' if mode == 'contract' else 'StyleBackgroundAsset', 5, False))
    mapping = BundleBackground(style=style, portable_asset_id='studio', path=path,
        sha256='f' * 64 if mode == 'sha' else background.sha256)
    mappings = (mapping, mapping) if mode == 'duplicate' else () if mode in ('missing_mapping', 'unmapped_closure') else (mapping,)
    def closure(entry):
        return (entry.recipe,) if mode == 'style_unused' else (entry.recipe, style, background) if mode == 'unmapped_closure' else (entry.recipe, style)
    def prepare():
        return prepare_export_bundle(plan, sources, (), qualify_recipe=closure,
            qualify_mask=lambda *args: True, tenant_archive_bytes=1 << 20, backgrounds=mappings)
    if mode == 'valid':
        prepared = prepare()
        manifest = BundleManifest.model_validate_json(prepared.manifest_jcs)
        assert len(manifest.backgrounds) == 1
        assert manifest.backgrounds[0] == mapping
        assert len(prepared.sources) == 4
    else:
        with pytest.raises(ValueError): prepare()
