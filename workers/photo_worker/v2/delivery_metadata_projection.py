"""Closed metadata projection for Delivery freeze and independent verification.

Source ownership and approved_fields must be established by authenticated frozen
admission. This pure projection grants neither approval nor task availability.
"""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.metadata import MetadataDocument

_FIELDS=frozenset({'author','copyright','camera_make','camera_model','lens','datetime_original','gps'})


def project_delivery_metadata(document,policy,*,approved_fields=()):
    if not isinstance(document,MetadataDocument):
        raise ValueError('DELIVERY_METADATA_SOURCE')
    if (not isinstance(approved_fields,tuple) or len(approved_fields)>7
            or any(not isinstance(field,str) or field not in _FIELDS for field in approved_fields)
            or len(set(approved_fields))!=len(approved_fields)):
        raise ValueError('DELIVERY_METADATA_APPROVAL')
    owned=MetadataDocument.model_validate_json(canonical_bytes(document))
    if policy=='strip':
        selected=set()
    elif policy=='copyright':
        selected={'author','copyright'}
    elif policy=='approved_exif':
        selected=set(approved_fields)
    else:
        raise ValueError('DELIVERY_METADATA_POLICY')
    values=owned.model_dump(mode='json')
    return MetadataDocument.model_validate({name:values[name] for name in sorted(selected)})


def verify_delivery_metadata_projection(actual,source,policy,*,approved_fields=()):
    """Verifier input must come from actual encoded-byte metadata extraction."""
    if not isinstance(actual,MetadataDocument):
        raise ValueError('DELIVERY_METADATA_RESULT')
    expected=project_delivery_metadata(source,policy,approved_fields=approved_fields)
    observed=MetadataDocument.model_validate_json(canonical_bytes(actual))
    if canonical_bytes(observed)!=canonical_bytes(expected):
        raise ValueError('DELIVERY_METADATA_RESULT')
    return True
