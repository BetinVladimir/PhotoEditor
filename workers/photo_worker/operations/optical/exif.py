"""Extract exact lens-match metadata from an immutable original image header."""
from fractions import Fraction
import math
import re
from .admission import LensMetadataEvidence
from photo_worker.runtime.executor import OperationFailure


def lens_metadata_from_header(image,binding):
    fields={name.lower():name for name in image.get_fields()}
    def text(names):
        for name in names:
            key=fields.get(name.lower())
            if key:
                value=str(image.get(key)).split(' (',1)[0].strip()
                if value:return value
        raise OperationFailure('LENS_METADATA_UNAVAILABLE')
    def numeric(names,suffix,low,high):
        value=text(names)
        if suffix and value.endswith(suffix):value=value[:-len(suffix)].strip()
        if len(value)>80:raise OperationFailure('LENS_METADATA_UNAVAILABLE')
        if not re.fullmatch(r'\d+(?:\.\d+)?(?:/\d+(?:\.\d+)?)?',value):
            raise OperationFailure('LENS_METADATA_UNAVAILABLE')
        try:
            parts=value.split('/')
            result=float(Fraction(parts[0])/(Fraction(parts[1]) if len(parts)==2 else 1))
        except (ValueError,ZeroDivisionError,OverflowError):raise OperationFailure('LENS_METADATA_UNAVAILABLE') from None
        if not math.isfinite(result) or not low<=result<=high:
            raise OperationFailure('LENS_METADATA_UNAVAILABLE')
        return result
    camera=text(['exif-ifd0-Model'])
    lens=text(['exif-ifd2-LensModel','exif-exif-LensModel'])
    if max(len(camera),len(lens))>120:raise OperationFailure('LENS_METADATA_UNAVAILABLE')
    focal=numeric(['exif-ifd2-FocalLength','exif-exif-FocalLength'],'mm',1,2000)
    aperture=numeric(['exif-ifd2-FNumber','exif-exif-FNumber'],'',.5,128)
    return LensMetadataEvidence(binding.asset_version_id,binding.source,camera,lens,focal,aperture)


def inspect_lens_metadata(path,binding,check_active=lambda:None):
    """Caller must materialize the original, SHA-verified asset for this binding."""
    import pyvips
    check_active()
    try:image=pyvips.Image.new_from_file(str(path),access='sequential',fail_on='error')
    except pyvips.Error as error:raise OperationFailure('LENS_METADATA_UNAVAILABLE') from error
    result=lens_metadata_from_header(image,binding)
    check_active()
    return result
