"""Validate untrusted bundle metadata against physically verified ZIP bytes."""
from __future__ import annotations

from hashlib import sha256
from typing import Annotated, Literal
from uuid import UUID

from pydantic import Field, model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, parse_json, canonical_bytes
from photo_worker.contracts.images import NormalizedRect
from .interchange_archive import VerifiedArchiveFile, interchange_path


class BundleEntry(Contract):
    entry_id: UUID
    external_id: Annotated[str, Field(min_length=1, max_length=240)]
    image_path: Annotated[str, Field(min_length=1, max_length=240)]
    image_sha256: SHA256
    recipe_path: str | None
    recipe_sha256: SHA256 | None
    rating: Annotated[int, Field(ge=-1, le=5)]
    pick: Literal["picked", "rejected", "unmarked"]
    crop: NormalizedRect | None
    label: Literal['Red', 'Yellow', 'Green', 'Blue', 'Purple'] | None = Field(
        default=None, exclude_if=lambda value: value is None)

    @model_validator(mode="after")
    def paired_recipe(self):
        if self.entry_id.int == 0 or (self.recipe_path is None) != (self.recipe_sha256 is None):
            raise ValueError("invalid bundle entry identity or recipe pair")
        interchange_path(self.image_path)
        if self.recipe_path is not None:
            interchange_path(self.recipe_path)
        return self


class BundleInventoryEntry(Contract):
    artifact_id: UUID
    path: Annotated[str, Field(min_length=1, max_length=240)]
    sha256: SHA256
    contract: Annotated[str, Field(min_length=1, max_length=120)]


class BundleMask(Contract):
    path: Annotated[str, Field(min_length=1, max_length=240)]
    sha256: SHA256
    canvas_hash: SHA256


class BundleBackground(Contract):
    style: ArtifactRef
    portable_asset_id: Annotated[str, Field(min_length=1, max_length=120)]
    path: Annotated[str, Field(min_length=1, max_length=240)]
    sha256: SHA256

    @model_validator(mode='after')
    def valid_identity(self):
        if (self.style.artifact_id.int == 0 or self.portable_asset_id.strip() != self.portable_asset_id
                or len(self.portable_asset_id.encode('utf-8')) > 120):
            raise ValueError('invalid portable background identity')
        interchange_path(self.path)
        return self


class BundleManifest(Contract):
    schema_version: Literal[1]
    renderer_version: Literal["v2.2"]
    entries: Annotated[list[BundleEntry], Field(min_length=1, max_length=5000)]
    masks: Annotated[list[BundleMask], Field(max_length=5000)]
    inventory: Annotated[list[BundleInventoryEntry], Field(min_length=1, max_length=30000)]
    backgrounds: Annotated[list[BundleBackground], Field(min_length=1, max_length=5000)] | None = Field(default=None, exclude_if=lambda value: value is None)


def verify_bundle_manifest(raw: bytes, files: tuple[VerifiedArchiveFile, ...]) -> BundleManifest:
    """Hashes bind original bytes before any tenant UUID remapping.

    manifest.json is excluded from its own inventory (a self hash is impossible).
    Every other physical file must be inventoried exactly once. Contract strings
    are untrusted declarations; downstream type validation/remap and canonical
    ingest are mandatory before using any file as recipe/image authority.
    """
    value = parse_json(raw, max_bytes=8 << 20)
    stack = [(value, 1)]
    while stack:
        node, depth = stack.pop()
        if depth > 32:
            raise ValueError("bundle JSON depth exceeded")
        if isinstance(node, dict):
            stack.extend((child, depth + 1) for child in node.values())
        elif isinstance(node, list):
            stack.extend((child, depth + 1) for child in node)
    manifest = BundleManifest.model_validate_json(canonical_bytes(value))
    physical: dict[str, VerifiedArchiveFile] = {}
    folded: set[str] = set()
    for item in files:
        path = interchange_path(item.path)
        if path.casefold() in folded:
            raise ValueError("duplicate physical inventory")
        folded.add(path.casefold())
        physical[path] = item
    metadata = physical.pop("manifest.json", None)
    if metadata is None or metadata.bytes != len(raw) or metadata.sha256 != sha256(raw).hexdigest():
        raise ValueError("manifest physical bytes mismatch")
    inventory: dict[str, BundleInventoryEntry] = {}
    identifiers: set[UUID] = set()
    for item in manifest.inventory:
        path = interchange_path(item.path)
        if path in inventory or item.artifact_id.int == 0 or item.artifact_id in identifiers:
            raise ValueError("duplicate bundle inventory identity")
        observed = physical.get(path)
        if observed is None or observed.sha256 != item.sha256:
            raise ValueError("bundle inventory hash mismatch")
        identifiers.add(item.artifact_id)
        inventory[path] = item
    if set(inventory) != set(physical):
        raise ValueError("unlisted archive files")

    def reference(path: str, digest: str) -> None:
        item = inventory.get(path)
        if item is None or item.sha256 != digest:
            raise ValueError("bundle reference outside exact inventory")

    entry_ids: set[UUID] = set()
    external_ids: set[str] = set()
    image_paths: set[str] = set()
    for entry in manifest.entries:
        if entry.entry_id in entry_ids or entry.external_id in external_ids or entry.image_path in image_paths:
            raise ValueError("duplicate bundle entry")
        entry_ids.add(entry.entry_id)
        external_ids.add(entry.external_id)
        image_paths.add(entry.image_path)
        reference(entry.image_path, entry.image_sha256)
        if entry.recipe_path is not None:
            reference(entry.recipe_path, entry.recipe_sha256)
    masks: set[str] = set()
    for mask in manifest.masks:
        interchange_path(mask.path)
        if mask.path in masks:
            raise ValueError("duplicate mask path")
        masks.add(mask.path)
        reference(mask.path, mask.sha256)
    background_ids = set()
    by_id = {item.artifact_id: item for item in manifest.inventory}
    for background in manifest.backgrounds or ():
        key = (background.style.artifact_id, background.portable_asset_id)
        style = by_id.get(background.style.artifact_id)
        source = inventory.get(background.path)
        if (key in background_ids or style is None or style.contract != 'StyleV2'
                or style.sha256 != background.style.sha256 or source is None
                or source.contract != 'StyleBackgroundAsset' or source.sha256 != background.sha256):
            raise ValueError('invalid portable background mapping')
        background_ids.add(key)
    return manifest
