"""Allowlisted metadata from a caller-verified immutable original image.

No source ACL, SHA verification or approval is inferred here. Unknown original
EXIF is ignored rather than copied. GPS extraction is explicitly opt-in.
"""
import math
from photo_worker.contracts.metadata import MetadataDocument,GPSMetadata


def extract_delivery_metadata(image,*,include_gps=False,check_active=lambda:None):
    check_active()
    if type(include_gps) is not bool:
        raise ValueError('DELIVERY_METADATA_GPS_APPROVAL')
    exif=image.getexif();values={}
    def text(fields,tag,name):
        if tag in fields:
            value=fields[tag]
            if not isinstance(value,str):raise ValueError('DELIVERY_METADATA_TEXT')
            values[name]=value
    for tag,name in ((315,'author'),(33432,'copyright'),(271,'camera_make'),(272,'camera_model')):
        text(exif,tag,name)
    if 34665 in exif:
        nested=exif.get_ifd(34665)
        text(nested,42036,'lens');text(nested,36867,'datetime_original')
    if include_gps and 34853 in exif:
        gps=exif.get_ifd(34853)
        def number(value):
            if isinstance(value,bool):raise ValueError('DELIVERY_METADATA_GPS')
            try:result=float(value)
            except (TypeError,ValueError,OverflowError,ZeroDivisionError):raise ValueError('DELIVERY_METADATA_GPS') from None
            if not math.isfinite(result):raise ValueError('DELIVERY_METADATA_GPS')
            return result
        def coordinate(tag,reference,positive,negative,maximum):
            parts=gps.get(tag);direction=gps.get(reference)
            if not isinstance(parts,(tuple,list)) or len(parts)!=3 or direction not in (positive,negative):
                raise ValueError('DELIVERY_METADATA_GPS')
            degrees,minutes,seconds=map(number,parts)
            if not 0<=degrees<=maximum or not 0<=minutes<60 or not 0<=seconds<60:
                raise ValueError('DELIVERY_METADATA_GPS')
            value=degrees+minutes/60+seconds/3600
            if value>maximum:raise ValueError('DELIVERY_METADATA_GPS')
            return -value if direction==negative else value
        latitude=coordinate(2,1,'N','S',90);longitude=coordinate(4,3,'E','W',180)
        altitude=None
        if 6 in gps:
            reference=gps.get(5,0)
            if isinstance(reference,bytes) and len(reference)==1:reference=reference[0]
            if type(reference) is not int or reference not in (0,1):raise ValueError('DELIVERY_METADATA_GPS')
            altitude=number(gps[6])
            if altitude<0:raise ValueError('DELIVERY_METADATA_GPS')
            if reference==1:altitude=-altitude
        values['gps']=GPSMetadata(latitude=latitude,longitude=longitude,altitude_m=altitude)
    result=MetadataDocument.model_validate(values)
    check_active()
    return result
