"""Independent metadata result verification over both immutable raster files."""
from dataclasses import dataclass
from pathlib import Path
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataDocument,MetadataParams
from photo_worker.runtime.image_fingerprint import decoded_image_fingerprint
from .delivery_encoded_verify import _verify_manifest_file,VerifiedEncodedRaster
from .delivery_metadata_result import validate_delivery_metadata_result,ValidatedDeliveryMetadataResult


@dataclass(frozen=True)
class VerifiedDeliveryMetadataFiles:
    documents: ValidatedDeliveryMetadataResult
    raster: VerifiedEncodedRaster


def verify_delivery_metadata_files(before_path,after_path,task,encoded_ref,params,before,after,report,
        raw,pin,binding,metadata_ref,source_raw,*,policy,approved_fields=(),max_bytes,max_pixels,
        check_active=lambda:None):
    check_active()
    documents=validate_delivery_metadata_result(task,encoded_ref,params,before,after,report,raw,pin,
        binding,metadata_ref,source_raw,policy=policy,approved_fields=approved_fields)
    before=EncodedImage.model_validate_json(documents.before_jcs)
    after=EncodedImage.model_validate_json(documents.after_jcs)
    params=MetadataParams.model_validate_json(documents.params_jcs)
    projection=MetadataDocument.model_validate_json(documents.projection_jcs)
    before_path,after_path=Path(before_path),Path(after_path)
    if before_path==after_path:raise ValueError('DELIVERY_METADATA_FILE_ALIAS')
    paths=(before_path,after_path)
    identity=lambda item:(item.st_dev,item.st_ino,item.st_size,item.st_mtime_ns,item.st_ctime_ns,item.st_mode,item.st_nlink)
    identities=[identity(path.stat(follow_symlinks=False)) for path in paths]
    _verify_manifest_file(before_path,before,max_bytes=max_bytes,max_pixels=max_pixels,check_active=check_active)
    raster=_verify_manifest_file(after_path,after,max_bytes=max_bytes,max_pixels=max_pixels,
        metadata_document=projection,metadata_policy=params.policy,check_active=check_active)
    mime={'jpeg':'image/jpeg','png':'image/png','tiff':'image/tiff'}[before.format]
    fingerprints=[]
    for path in paths:
        with path.open('rb') as stream:
            fingerprints.append(decoded_image_fingerprint(stream,mime,preserve_orientation=False,check_active=check_active))
    if fingerprints[0]!=fingerprints[1]:raise ValueError('DELIVERY_METADATA_PIXELS_CHANGED')
    check_active()
    if any(identity(path.stat(follow_symlinks=False))!=expected for path,expected in zip(paths,identities)):
        raise ValueError('DELIVERY_METADATA_FILE_CHANGED')
    return VerifiedDeliveryMetadataFiles(documents,raster)
