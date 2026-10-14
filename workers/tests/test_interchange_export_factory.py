from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.operations.interchange.export_factory import ExportPreparationFactory
from photo_worker.operations.interchange.recipe_factory import PreparedImportRecipeFactory
from photo_worker.operations.interchange.installation import (resolve_interchange_export_installation,
    sql_prepared_export_installation)
from photo_worker.operations.interchange.export_adapter import ExportInstallation
from test_interchange_export_prepare import fixture


def recipes():
    raw=b'{"$defs":{}}'
    return PreparedImportRecipeFactory(schemas_jcs=raw,schemas_sha=sha256(raw).hexdigest(),
        work_icc_sha='e'*64,qualify_runtime=lambda *args:True)


@pytest.mark.parametrize('format',['photoeditor_bundle','xmp','captureone_sidecar'])
def test_export_factory_routes_owned_plan_to_concrete_format(format):
    factory=ExportPreparationFactory(recipes());plan,_,_=fixture();plan.format=format
    selected=[];sentinel=object()
    def call(ctx,artifacts,authority,owned,reader):
        selected.append(owned.format);owned.entries[0].rating=0;return sentinel
    def unexpected(*args):pytest.fail('wrong format preparer')
    factory.bundle=call if format=='photoeditor_bundle' else unexpected
    factory.sidecars=unexpected if format=='photoeditor_bundle' else call
    assert factory(SimpleNamespace(check_active=lambda:None),None,None,plan,None) is sentinel
    assert selected==[format] and plan.entries[0].rating==4


def test_export_installation_is_explicit_and_requires_complete_ports():
    assert resolve_interchange_export_installation() is None
    valid=ExportInstallation(SimpleNamespace(admit=lambda *args:None),ExportPreparationFactory(recipes()))
    assert resolve_interchange_export_installation(valid) is valid
    for invalid in [object(),ExportInstallation(None,lambda *args:None),ExportInstallation(valid.admission,None)]:
        with pytest.raises(ValueError):resolve_interchange_export_installation(invalid)
    raw=b'{"$defs":{}}'
    database=SimpleNamespace(interchange_export_context=lambda *args:None)
    installed=sql_prepared_export_installation(database,schemas_jcs=raw,schemas_sha=sha256(raw).hexdigest(),
        work_icc_sha='e'*64,qualify_runtime=lambda *args:True)
    assert resolve_interchange_export_installation(installed) is installed
    assert isinstance(installed.prepare,ExportPreparationFactory)
    origin_factory=lambda *args: ((),(),lambda *args:True)
    with_origins=sql_prepared_export_installation(database,schemas_jcs=raw,
        schemas_sha=sha256(raw).hexdigest(),work_icc_sha='e'*64,
        qualify_runtime=lambda *args:True,person_origin_factory=origin_factory)
    assert with_origins.prepare.bundle.person_origins is origin_factory
    with pytest.raises(ValueError,match='PERSON_ORIGIN_INSTALLATION'):
        sql_prepared_export_installation(database,schemas_jcs=raw,
            schemas_sha=sha256(raw).hexdigest(),work_icc_sha='e'*64,
            qualify_runtime=lambda *args:True,person_origin_factory=object())


@pytest.mark.parametrize('with_occurrences', [False, True])
def test_bundle_factory_composes_verified_inventory_and_real_manifest(monkeypatch, with_occurrences):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.interchange.export_adapter import ExportAuthority
    from photo_worker.operations.interchange.export_bundle_factory import BundleExportPreparationFactory
    from photo_worker.v2.interchange_export_inventory import QualifiedExportInventory
    from photo_worker.v2.interchange_export_sidecars import ExportImageEvidence
    from photo_worker.v2.interchange_manifest import BundleManifest
    import photo_worker.operations.interchange.export_bundle_factory as module
    from uuid import uuid4
    plan,sources,_=fixture()
    inventory=QualifiedExportInventory(sources,((str(plan.entries[0].entry_id),(canonical_bytes(plan.entries[0].recipe),)),))
    recipe_factory=recipes();qualifier=object();events=[]
    occurrence=lambda event:True
    def build(ctx,artifacts,authority,owned,reader,**kwargs):
        assert kwargs==({'qualify_occurrence':occurrence} if with_occurrences else {})
        events.append('recipes');return qualifier
    def occurrences(ctx,artifacts,authority,owned,reader):
        assert owned==plan and owned is not plan
        events.append('occurrences');return occurrence
    recipe_factory.build_for_plan=build
    def compile_inventory(owned,reader,selected,*,active):
        assert selected is qualifier;active();events.append('inventory');return inventory
    def image(entry,reader,scratch,*,active):
        active();events.append('image');return ExportImageEvidence(entry.content,5,'image/tiff',1)
    def masks(selected,reader,*,active):
        assert selected is inventory;active();events.append('masks');return ()
    monkeypatch.setattr(module,'compile_qualified_export_inventory',compile_inventory)
    monkeypatch.setattr(module,'qualify_export_image',image)
    monkeypatch.setattr(module,'compile_export_masks',masks)
    reader=SimpleNamespace(recheck=lambda:events.append('recheck'))
    authority=ExportAuthority(uuid4(),plan.project_id,plan.snapshot_digest,1 << 20,2 << 20,128 << 20)
    result=BundleExportPreparationFactory(recipe_factory,occurrence_factory=occurrences if with_occurrences else None)(SimpleNamespace(check_active=lambda:None),
        SimpleNamespace(scratch=object()),authority,plan,reader)
    manifest=BundleManifest.model_validate_json(result.archive.manifest_jcs)
    assert events==(['occurrences'] if with_occurrences else [])+['recipes','inventory','image','masks','recheck']
    assert manifest.entries[0].entry_id==plan.entries[0].entry_id and len(manifest.inventory)==2
    assert result.mappings[0].asset_version_id==plan.entries[0].binding.asset_version_id
    assert {field.name for field in result.mappings[0].fields}=={'rating','pick','recipe'}


@pytest.mark.parametrize('value', [False, 1, object()])
def test_export_factory_rejects_noncallable_occurrence_factory(value):
    with pytest.raises(ValueError, match='OCCURRENCE_INSTALLATION'):
        ExportPreparationFactory(recipes(), occurrence_factory=value)


def test_bundle_stops_before_inventory_when_occurrence_installation_fails(monkeypatch):
    from uuid import uuid4
    from photo_worker.operations.interchange.export_adapter import ExportAuthority
    from photo_worker.operations.interchange.export_bundle_factory import BundleExportPreparationFactory
    plan, _, _ = fixture()
    authority = ExportAuthority(uuid4(), plan.project_id, plan.snapshot_digest, 1 << 20, 2 << 20, 128 << 20)
    factory = recipes()
    factory.build_for_plan = lambda *args, **kwargs: pytest.fail('recipe qualification started')
    bundle = BundleExportPreparationFactory(factory, occurrence_factory=lambda *args: None)
    with pytest.raises(ValueError, match='OCCURRENCE_INSTALLATION'):
        bundle(SimpleNamespace(check_active=lambda: None), None, authority, plan, None)
