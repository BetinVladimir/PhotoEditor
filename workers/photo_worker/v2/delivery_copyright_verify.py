"""Independent copyright projection from an opened encoded image.

Caller verifies immutable bytes, format/dimensions/ICC/pixels and file stability.
This helper checks actual EXIF and refuses other metadata containers; manifest
claims and writer reports are never inputs to metadata acceptance.
"""
from photo_worker.contracts.metadata import MetadataDocument
from .delivery_metadata_projection import verify_delivery_metadata_projection


def verify_delivery_copyright_image(image,source,*,check_active=lambda:None):
    check_active()
    if (getattr(image,'text',{}) or any(name in image.info for name in
            ('xmp','XML:com.adobe.xmp','iptc','photoshop','comment'))):
        raise ValueError('DELIVERY_METADATA_EXTRA_CONTAINER')
    exif=image.getexif()
    # Structural raster fields plus exactly Artist/Copyright. GPS, thumbnail,
    # MakerNote, serial numbers and camera identity are never copyright fields.
    allowed={256,257,274,282,283,296,315,33432,34665}
    if image.format=='TIFF':
        # Storage/layout and ICC tags are checked independently by tifffile.
        # Software/ImageDescription remain forbidden metadata, not layout.
        allowed|={254,258,259,262,273,277,278,279,284,317,322,323,324,325,338,339,34675}
    if set(exif)-allowed:
        raise ValueError('DELIVERY_METADATA_EXTRA_FIELD')
    if exif.get(274,1)!=1:
        raise ValueError('DELIVERY_METADATA_ORIENTATION')
    for tag,want in ((256,image.width),(257,image.height)):
        if tag in exif and exif[tag]!=want:
            raise ValueError('DELIVERY_METADATA_DIMENSIONS')
    if 34665 in exif:
        nested=exif.get_ifd(34665)
        # ExifVersion, FlashpixVersion, ColorSpace and actual raster dimensions.
        if set(nested)-{36864,40960,40961,40962,40963}:
            raise ValueError('DELIVERY_METADATA_EXTRA_FIELD')
        for tag,want in ((40962,image.width),(40963,image.height)):
            if tag in nested and nested[tag]!=want:
                raise ValueError('DELIVERY_METADATA_DIMENSIONS')
    values={}
    for tag,name in ((315,'author'),(33432,'copyright')):
        if tag in exif:
            value=exif[tag]
            if not isinstance(value,str):
                raise ValueError('DELIVERY_METADATA_TEXT')
            values[name]=value
    actual=MetadataDocument.model_validate(values)
    verify_delivery_metadata_projection(actual,source,'copyright')
    check_active()
    return True
