"""Compile bundle mask declarations from a successfully qualified inventory."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import MaskRef
from .interchange_manifest import BundleMask
from .interchange_export_inventory import QualifiedExportInventory


def compile_export_masks(inventory, reader, *, active=lambda: None):
    if not isinstance(inventory, QualifiedExportInventory):
        raise ValueError('INTERCHANGE_EXPORT_MASK_INVENTORY')
    by_id={item.reference.artifact_id:item for item in inventory.sources}
    masks={}
    for source in inventory.sources:
        active()
        if source.contract!='MaskRef':continue
        raw=reader.read_json(source.reference,min(8 << 20,reader.read_budget.remaining_json_bytes))
        mask=MaskRef.model_validate_json(raw)
        if canonical_bytes(mask)!=raw or sha256(raw).hexdigest()!=source.reference.sha256:
            raise ValueError('INTERCHANGE_EXPORT_MASK_DOCUMENT')
        binary=by_id.get(mask.artifact.artifact_id)
        if (binary is None or binary.reference!=mask.artifact
                or binary.contract not in ('MaskArray','MaskPixels') or binary.sidecar):
            raise ValueError('INTERCHANGE_EXPORT_MASK_BINARY')
        declaration=BundleMask(path=binary.path,sha256=binary.reference.sha256,canvas_hash=canonical_hash(mask.canvas))
        old=masks.get(binary.path)
        if old is not None and old!=declaration:
            raise ValueError('INTERCHANGE_EXPORT_MASK_CANVAS_CONFLICT')
        masks[binary.path]=declaration
    expected={item.path for item in inventory.sources if item.contract in ('MaskArray','MaskPixels')}
    if set(masks)!=expected or len(masks)>5000:
        raise ValueError('INTERCHANGE_EXPORT_MASK_WITNESS')
    reader.recheck();active()
    return tuple(masks[path] for path in sorted(masks))
