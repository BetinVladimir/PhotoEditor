"""Prepare the PhotoEditor bundle from typed/physically qualified inputs."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_plan import InterchangePlan
from photo_worker.v2.interchange_export_inventory import compile_qualified_export_inventory
from photo_worker.v2.interchange_export_masks import compile_export_masks
from photo_worker.v2.interchange_export_backgrounds import qualify_export_background_sources
from photo_worker.v2.interchange_manifest import BundleBackground, BundlePersonOrigin
from photo_worker.v2.render_background_binding import decode_frozen_background
from photo_worker.v2.interchange_export_prepare import ExportSource, prepare_export_bundle
from photo_worker.v2.interchange_mapping import MappingEntry, MappingField
from .export_adapter import ExportAuthority, PreparedExportWork
from .export_images import qualify_export_image
from .recipe_factory import PreparedImportRecipeFactory


class BundleExportPreparationFactory:
    def __init__(self, recipe_factory, *, occurrence_factory=None, person_origin_factory=None):
        if not isinstance(recipe_factory,PreparedImportRecipeFactory):
            raise ValueError('INTERCHANGE_EXPORT_BUNDLE_INSTALLATION')
        if occurrence_factory is not None and not callable(occurrence_factory):
            raise ValueError('INTERCHANGE_EXPORT_OCCURRENCE_INSTALLATION')
        if person_origin_factory is not None and not callable(person_origin_factory):
            raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_INSTALLATION')
        self.recipes=recipe_factory
        self.occurrences=occurrence_factory
        self.person_origins=person_origin_factory

    def __call__(self,ctx,artifacts,authority,plan,reader):
        ctx.check_active()
        plan=InterchangePlan.model_validate_json(canonical_bytes(plan))
        if (not isinstance(authority,ExportAuthority) or plan.direction!='export'
                or plan.format!='photoeditor_bundle' or plan.project_id!=authority.project_id
                or plan.snapshot_digest!=authority.snapshot_digest
                or type(authority.max_archive_bytes) is not int
                or not 1<=authority.max_archive_bytes<=64 << 30
                or plan.max_archive_bytes>authority.max_archive_bytes):
            raise ValueError('INTERCHANGE_EXPORT_BUNDLE_AUTHORITY')
        background_sources,background_receipts=qualify_export_background_sources(plan,reader,artifacts.scratch,active=ctx.check_active)
        background_pins=tuple(decode_frozen_background(raw) for _,raw in background_receipts)
        background_mappings=tuple((pin.style,pin.portable_asset_id,pin.source) for pin in background_pins)
        if self.occurrences is None:
            qualifier=self.recipes.build_for_plan(ctx,artifacts,authority,plan,reader,
                background_mappings=background_mappings)
        else:
            occurrence=self.occurrences(ctx,artifacts,authority,plan.model_copy(deep=True),reader)
            ctx.check_active()
            if not callable(occurrence):
                raise ValueError('INTERCHANGE_EXPORT_OCCURRENCE_INSTALLATION')
            qualifier=self.recipes.build_for_plan(ctx,artifacts,authority,plan,reader,
                qualify_occurrence=occurrence,background_mappings=background_mappings)
        inventory=compile_qualified_export_inventory(plan,reader,qualifier,active=ctx.check_active)
        all_sources={source.reference.artifact_id:source for source in inventory.sources}
        for source in background_sources:
            old=all_sources.get(source.reference.artifact_id)
            if old is not None and old!=source:raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_ALIAS')
            all_sources[source.reference.artifact_id]=source
        backgrounds={}
        for pin in background_pins:
            value=BundleBackground(style=pin.style,portable_asset_id=pin.portable_asset_id,
                path=all_sources[pin.source.artifact_id].path,sha256=pin.source.sha256)
            key=(pin.style.artifact_id,pin.portable_asset_id)
            if key in backgrounds and backgrounds[key]!=value:raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_MAPPING')
            backgrounds[key]=value
        for entry in plan.entries:
            qualify_export_image(entry.model_copy(deep=True),reader,artifacts.scratch,active=ctx.check_active)
        masks=compile_export_masks(inventory,reader,active=ctx.check_active)
        origin_mappings=()
        origin_qualifier=None
        if self.person_origins is not None:
            origin_sources, origin_mappings, origin_qualifier = self.person_origins(
                ctx,artifacts,authority,plan.model_copy(deep=True),reader,inventory)
            if (not isinstance(origin_sources,tuple) or not isinstance(origin_mappings,tuple)
                    or not callable(origin_qualifier)):
                raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_INSTALLATION')
            for source in origin_sources:
                if not isinstance(source,ExportSource):
                    raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_SOURCE')
                old=all_sources.get(source.reference.artifact_id)
                if old is not None and old!=source:
                    raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_ALIAS')
                all_sources[source.reference.artifact_id]=source
            for origin in origin_mappings:
                if not isinstance(origin,BundlePersonOrigin):
                    raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_MAPPING')
        mask_pins={mask.path:canonical_bytes(mask) for mask in masks}
        sources={source.path:source for source in inventory.sources}
        def qualify_mask(mask,source):
            ctx.check_active()
            # Physical mask checks already succeeded in the exact recipe closure.
            # Recheck immutable input pins before accepting its manifest receipt.
            reader.recheck()
            return mask_pins.get(mask.path)==canonical_bytes(mask) and sources.get(source.path)==source
        prepared=prepare_export_bundle(plan,tuple(sorted(all_sources.values(),key=lambda item:item.path)),masks,
            qualify_recipe=inventory.recipe_closure,qualify_mask=qualify_mask,
            tenant_archive_bytes=authority.max_archive_bytes,active=ctx.check_active,
            backgrounds=tuple(backgrounds.values()),person_origins=origin_mappings,
            qualify_person_origin=origin_qualifier)
        mappings=tuple(MappingEntry(entry_id=entry.entry_id,external_id=entry.external_id,
            asset_version_id=entry.binding.asset_version_id,proposed_document=None,
            fields=[MappingField(name=name,status='exact',reason=None) for name in
                    ('rating','pick','recipe',*(() if entry.crop is None else ('crop',)))])
            for entry in sorted(plan.entries,key=lambda value:value.path))
        reader.recheck();ctx.check_active()
        return PreparedExportWork(prepared,mappings)
