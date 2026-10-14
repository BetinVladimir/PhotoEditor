"""Writer preparation from externally pinned Delivery metadata authority.

This does not advertise a worker operation or grant SQL publication authority.
The caller must run the qualified tool and verify bytes before staging.
"""
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataParams
from photo_worker.operations.metadata.write import build_args
from .delivery_metadata_freeze import verify_frozen_delivery_metadata


def delivery_metadata_write_args(executable,path,encoded_ref,encoded,raw,pin,binding,metadata_ref,
        source_raw,*,policy,approved_fields=(),check_active=lambda:None):
    check_active()
    if (not isinstance(encoded_ref,ArtifactRef) or encoded_ref.artifact_id.int==0
            or not isinstance(encoded,EncodedImage) or encoded.metadata_policy!='strip'
            or policy not in ('copyright','approved_exif')):
        raise ValueError('DELIVERY_METADATA_WRITE_INPUT')
    frozen=verify_frozen_delivery_metadata(raw,pin,binding,metadata_ref,source_raw,
        policy=policy,approved_fields=approved_fields)
    # Only the frozen projection is passed to the existing allowlisted writer.
    params=MetadataParams(encoded=encoded_ref,document=metadata_ref,
        policy='copyright_only' if policy=='copyright' else 'safe_camera',
        copy_gps=policy=='approved_exif' and 'gps' in frozen.approved_fields)
    args,copied=build_args(executable,path,encoded,frozen.projection,params)
    check_active()
    return args,tuple(copied)
