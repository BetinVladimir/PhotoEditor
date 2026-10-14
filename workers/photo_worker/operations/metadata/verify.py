"""Verify allowlisted metadata on actual rewritten bytes before staging."""
import math
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.metadata import MetadataDocument
from photo_worker.v2.delivery_metadata_extract import extract_delivery_metadata


def verify_written_metadata(image,document,params,*,check_active=lambda:None):
    check_active()
    exif=image.getexif()
    if exif.get(274,1)!=1:raise ValueError('METADATA_ORIENTATION')
    for tag,wanted in ((256,image.width),(257,image.height)):
        if tag in exif and (type(exif[tag]) is not int or exif[tag]!=wanted):
            raise ValueError('METADATA_DIMENSIONS')
    # Structural TIFF/container fields do not grant permission to private tags.
    allowed={254,256,257,258,259,262,273,274,277,278,279,282,283,284,296,317,
        322,323,324,325,338,339,34675,34665,315,33432}
    if params.policy=='safe_camera':allowed.update({271,272})
    if params.policy=='safe_camera' and params.copy_gps:allowed.add(34853)
    if set(exif)-allowed:raise ValueError('METADATA_UNEXPECTED_EXIF')
    if 34665 in exif:
        nested=exif.get_ifd(34665)
        for tag,wanted in ((40962,image.width),(40963,image.height)):
            if tag in nested and (type(nested[tag]) is not int or nested[tag]!=wanted):
                raise ValueError('METADATA_DIMENSIONS')
        nested_allowed={36864,40960,40961,40962,40963}
        if params.policy=='safe_camera':nested_allowed.update({42036,36867})
        if set(nested)-nested_allowed:raise ValueError('METADATA_UNEXPECTED_EXIF_IFD')
    if 34853 in exif and set(exif.get_ifd(34853))-{0,1,2,3,4,5,6}:
        raise ValueError('METADATA_UNEXPECTED_GPS')
    if any(key in image.info for key in ('xmp','XML:com.adobe.xmp','iptc','photoshop','comment')) or getattr(image,'text',{}):
        raise ValueError('METADATA_UNEXPECTED_CONTAINER')
    values={'author':document.author,'copyright':document.copyright}
    if params.policy=='safe_camera':
        values.update({name:getattr(document,name) for name in ('camera_make','camera_model','lens','datetime_original')})
        if params.copy_gps:values['gps']=document.gps
    expected=MetadataDocument.model_validate(values)
    actual=extract_delivery_metadata(image,include_gps=True,check_active=check_active)
    if canonical_bytes(actual.model_copy(update={'gps':None}))!=canonical_bytes(expected.model_copy(update={'gps':None})):
        raise ValueError('METADATA_TEXT_MISMATCH')
    if (actual.gps is None)!=(expected.gps is None):raise ValueError('METADATA_GPS_MISMATCH')
    if actual.gps is not None:
        # EXIF rational encoding introduces bounded numeric rounding only.
        for field,tolerance in (('latitude',1e-7),('longitude',1e-7),('altitude_m',1e-3)):
            observed,wanted=getattr(actual.gps,field),getattr(expected.gps,field)
            if (observed is None)!=(wanted is None) or observed is not None and not math.isclose(observed,wanted,rel_tol=0,abs_tol=tolerance):
                raise ValueError('METADATA_GPS_MISMATCH')
    check_active()
