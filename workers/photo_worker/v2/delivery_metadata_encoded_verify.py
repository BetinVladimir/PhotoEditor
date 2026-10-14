"""Verify physical encoded output against task-pinned frozen metadata.

The coordinator authenticates task/source/recipe lineage and immutable input
artifacts. This verifier never derives permission from an output manifest.
"""
from .delivery_metadata_freeze import verify_frozen_delivery_metadata
from .delivery_encoded_verify import verify_delivery_encoded_file


def verify_delivery_metadata_encoded_file(path,params,source,manifest,raw,pin,binding,metadata_ref,
        source_raw,*,policy,approved_fields=(),max_bytes,max_pixels,check_active=lambda:None):
    check_active()
    if policy not in ('copyright','approved_exif'):
        raise ValueError('DELIVERY_METADATA_ENCODE_POLICY')
    frozen=verify_frozen_delivery_metadata(raw,pin,binding,metadata_ref,source_raw,
        policy=policy,approved_fields=approved_fields)
    check_active()
    result=verify_delivery_encoded_file(path,params,source,manifest,max_bytes=max_bytes,
        max_pixels=max_pixels,check_active=check_active,metadata_document=frozen.projection,
        metadata_policy='copyright_only' if policy=='copyright' else 'safe_camera')
    check_active()
    return result
