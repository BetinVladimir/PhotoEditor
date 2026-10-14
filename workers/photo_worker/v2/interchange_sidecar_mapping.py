"""Recover unsupported XMP claims from pinned opaque parse outputs."""
from hashlib import sha256
from pathlib import PurePosixPath

from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid
from .interchange_mapping import MappingField
from .interchange_xmp import XMP, parse_xmp


def sidecar_mapping_fields(entry, files, read_document, *, applied_label=False,
                           check_active=lambda: None):
    check_active()
    path = str(PurePosixPath(entry.image_path).with_suffix('.xmp'))
    file = files.get(path)
    if file is None or file.contract != 'XMP':
        return []
    ref = file.content.model_copy(deep=True)
    raw = read_document(ref.model_copy(deep=True), 1 << 20)
    check_active()
    if not isinstance(raw, bytes) or not 1 <= len(raw) <= 1 << 20 or sha256(raw).hexdigest() != ref.sha256:
        raise AdoptionInvalid('INTERCHANGE_XMP_HASH')
    mapping = parse_xmp(raw)
    check_active()
    if not all(hasattr(entry, name) for name in ('rating', 'pick', 'crop', 'label')):
        raise AdoptionInvalid('INTERCHANGE_XMP_METADATA_MISMATCH')
    if mapping.label != entry.label:
        raise AdoptionInvalid('INTERCHANGE_XMP_LABEL_MISMATCH')
    if entry.rating != (mapping.rating if mapping.rating is not None else 0):
        raise AdoptionInvalid('INTERCHANGE_XMP_METADATA_MISMATCH')
    if entry.pick != (mapping.pick or 'unmarked'):
        raise AdoptionInvalid('INTERCHANGE_XMP_METADATA_MISMATCH')
    expected = mapping.crop
    actual = entry.crop
    if (actual is None) != (expected is None):
        raise AdoptionInvalid('INTERCHANGE_XMP_METADATA_MISMATCH')
    if expected is not None and any(abs(getattr(actual, name)-value) > 1e-12
                                    for name, value in zip(('x0', 'y0', 'x1', 'y1'), expected)):
        raise AdoptionInvalid('INTERCHANGE_XMP_METADATA_MISMATCH')
    reported = tuple(sorted((*mapping.unsupported,
        *((XMP + '}Label',) if mapping.label is not None and not applied_label else ()))))
    if not reported:
        return []
    # Preserve every original property in imported XMP. The bounded report binds
    # the complete ordered unsupported list without truncating its identity.
    digest = sha256(canonical_bytes(list(reported))).hexdigest()
    return [MappingField(name='xmp_properties', status='unsupported',
        reason=f'unsupported_xmp_properties:{len(reported)}:{digest}')]
