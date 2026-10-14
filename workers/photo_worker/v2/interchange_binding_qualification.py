"""Rebind source identities only through verified recipes and canonical ingest."""
from dataclasses import dataclass
from hashlib import sha256
from pydantic import ValidationError
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid
from .interchange_import_sources import ImportSources
from .interchange_parse import ParsedInterchange
from .interchange_remap import UnsupportedDependency
from .render_recipe import EditingRecipeV3


@dataclass(frozen=True)
class QualifiedBinding:
    original_jcs: bytes
    canonical_jcs: bytes


class CanonicalImportBindings:
    """Old WorkingImage is identity evidence only; its pixels are never adopted.

    The replacement always comes from the existing canonical ingest result.
    Ambiguous old identities fail rather than choosing an arbitrary new asset.
    """
    def __init__(self,parsed,sources,read_document,*,check_active=lambda:None):
        self.active=check_active
        if not callable(read_document) or not callable(check_active):
            raise ValueError('INTERCHANGE_BINDING_INSTALLATION')
        parsed=ParsedInterchange.model_validate_json(canonical_bytes(parsed))
        sources=ImportSources.model_validate_json(canonical_bytes(sources))
        if canonical_hash(parsed)!=sources.parsed.sha256 or len(parsed.manifest.entries)!=len(sources.entries):
            raise AdoptionInvalid('INTERCHANGE_BINDING_SOURCE_SCOPE')
        files={item.path:item for item in parsed.files}
        inventory={item.artifact_id:item for item in parsed.manifest.inventory}
        if len(files)!=len(parsed.files) or len(inventory)!=len(parsed.manifest.inventory):
            raise AdoptionInvalid('INTERCHANGE_BINDING_DUPLICATE_INVENTORY')
        self.bindings={}
        self.entry_failures={}
        self.ambiguous=set()
        version_entries={}

        def read(reference,maximum):
            check_active()
            raw=read_document(reference,maximum)
            if not isinstance(raw,bytes) or not 2<=len(raw)<=maximum or sha256(raw).hexdigest()!=reference.sha256:
                raise AdoptionInvalid('INTERCHANGE_BINDING_DOCUMENT_HASH')
            check_active()
            return raw

        for ordinal,(entry,source) in enumerate(zip(parsed.manifest.entries,sources.entries)):
            if source.ordinal!=ordinal or source.entry_id!=entry.entry_id:
                raise AdoptionInvalid('INTERCHANGE_BINDING_ENTRY_SCOPE')
            if source.state!='adopted' or entry.recipe_path is None:
                continue
            recipe_file=files.get(entry.recipe_path)
            if recipe_file is None or recipe_file.content.sha256!=entry.recipe_sha256:
                raise AdoptionInvalid('INTERCHANGE_BINDING_RECIPE_INVENTORY')
            try:
                recipe_raw=read(recipe_file.content,8<<20)
                recipe=EditingRecipeV3.model_validate_json(recipe_raw)
                if canonical_bytes(recipe)!=recipe_raw:
                    raise UnsupportedDependency('INTERCHANGE_BINDING_RECIPE_FIELDS')
                original=recipe.source
                image_item=inventory.get(original.source.artifact_id)
                image_file=None if image_item is None else files.get(image_item.path)
                if (image_item is None or image_item.contract!='WorkingImage' or image_file is None
                        or image_item.sha256!=original.source.sha256 or image_file.content.sha256!=original.source.sha256):
                    raise UnsupportedDependency('INTERCHANGE_BINDING_ORIGINAL_IMAGE_MISSING')
                old_raw=read(image_file.content,1<<20)
                old_image=WorkingImage.model_validate_json(old_raw)
                if (canonical_bytes(old_image)!=old_raw or old_image.canvas.source_sha256!=entry.image_sha256
                        or canonical_hash(old_image.canvas)!=original.canvas_hash):
                    raise UnsupportedDependency('INTERCHANGE_BINDING_ORIGINAL_CANVAS')
                new_raw=read(source.master,1<<20)
                try:
                    new_image=WorkingImage.model_validate_json(new_raw)
                except ValidationError as error:
                    raise AdoptionInvalid('INTERCHANGE_BINDING_CANONICAL_IMAGE_INVALID') from error
                if (canonical_bytes(new_image)!=new_raw or new_image.canvas.source_sha256!=entry.image_sha256
                        or new_image.dtype!='float32' or new_image.color.profile_id!='work_rec2020_gamma24_v1'):
                    raise AdoptionInvalid('INTERCHANGE_BINDING_CANONICAL_IMAGE')
                target=VersionBinding(asset_version_id=source.asset_version_id,source=source.master,
                                      canvas_hash=canonical_hash(new_image.canvas))
                qualified=QualifiedBinding(canonical_bytes(original),canonical_bytes(target))
                if original.asset_version_id in self.ambiguous:
                    raise UnsupportedDependency('INTERCHANGE_BINDING_AMBIGUOUS_IDENTITY')
                previous=self.bindings.get(original.asset_version_id)
                if previous is not None and previous!=qualified:
                    self.ambiguous.add(original.asset_version_id)
                    self.bindings.pop(original.asset_version_id,None)
                    for prior_entry in version_entries.get(original.asset_version_id,[]):
                        self.entry_failures[prior_entry]='INTERCHANGE_BINDING_AMBIGUOUS_IDENTITY'
                    raise UnsupportedDependency('INTERCHANGE_BINDING_AMBIGUOUS_IDENTITY')
                self.bindings[original.asset_version_id]=qualified
                version_entries.setdefault(original.asset_version_id,[]).append(entry.entry_id)
            except (UnsupportedDependency,ValidationError) as error:
                self.entry_failures[entry.entry_id]=str(error) if isinstance(error,UnsupportedDependency) else 'INTERCHANGE_BINDING_TYPED_DOCUMENT_INVALID'
        check_active()

    def __call__(self,original):
        self.active()
        receipt=self.bindings.get(original.asset_version_id)
        if receipt is None or canonical_bytes(original)!=receipt.original_jcs:
            raise UnsupportedDependency('INTERCHANGE_BINDING_UNQUALIFIED')
        return VersionBinding.model_validate_json(receipt.canonical_jcs)
