"""Normalize allowlisted EXIF fields from one bounded ExifTool JSON record.

The caller must authenticate and hash the original file, bound the ExifTool
process/output, and verify the file identity after extraction. This parser does
not infer authority from ExifTool's SourceFile field or copy maker notes.
"""
import json
import math

from photo_worker.contracts.metadata import GPSMetadata, MetadataDocument


_FIELDS = {
    'author': ('IFD0:Artist', 'ExifIFD:Artist'),
    'copyright': ('IFD0:Copyright', 'ExifIFD:Copyright'),
    'camera_make': ('IFD0:Make', 'ExifIFD:Make'),
    'camera_model': ('IFD0:Model', 'ExifIFD:Model'),
    'lens': ('ExifIFD:LensModel',),
    'datetime_original': ('ExifIFD:DateTimeOriginal', 'IFD0:DateTimeOriginal'),
}
_GPS = ('GPS:GPSLatitude', 'GPS:GPSLatitudeRef', 'GPS:GPSLongitude',
        'GPS:GPSLongitudeRef', 'GPS:GPSAltitude', 'GPS:GPSAltitudeRef')
MAX_RAW_EXIF_JSON_BYTES = 65536


def extract_raw_delivery_metadata(raw: bytes, *, include_gps: bool = False,
                                  expected_filetype: str | None = None) -> MetadataDocument:
    """Decode ``exiftool -j -G1 -n`` output for one previously verified original."""
    if (type(raw) is not bytes or not 2 <= len(raw) <= MAX_RAW_EXIF_JSON_BYTES
            or type(include_gps) is not bool
            or (expected_filetype is not None and type(expected_filetype) is not str)):
        raise ValueError('DELIVERY_RAW_METADATA_INPUT')

    def unique_pairs(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('DELIVERY_RAW_METADATA_DUPLICATE')
            result[key] = value
        return result

    try:
        records = json.loads(raw.decode('utf-8'), object_pairs_hook=unique_pairs,
                             parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    except (UnicodeError, json.JSONDecodeError, ValueError) as exc:
        raise ValueError('DELIVERY_RAW_METADATA_JSON') from exc
    if type(records) is not list or len(records) != 1 or type(records[0]) is not dict:
        raise ValueError('DELIVERY_RAW_METADATA_RECORD')
    record = records[0]
    if expected_filetype is not None and record.get('File:FileType') != expected_filetype:
        raise ValueError('DELIVERY_RAW_METADATA_FILETYPE')
    values = {}
    for field, tags in _FIELDS.items():
        present = [tag for tag in tags if tag in record]
        if len(present) > 1:
            raise ValueError('DELIVERY_RAW_METADATA_AMBIGUOUS')
        if present:
            if type(record[present[0]]) is not str:
                raise ValueError('DELIVERY_RAW_METADATA_TEXT')
            values[field] = record[present[0]]
    if include_gps:
        present = [name in record for name in _GPS[:4]]
        if any(present):
            if not all(present):
                raise ValueError('DELIVERY_RAW_METADATA_GPS')

            def coordinate(value, reference, positive, negative, maximum):
                if type(value) not in (int, float) or not math.isfinite(value):
                    raise ValueError('DELIVERY_RAW_METADATA_GPS')
                if type(reference) is not str or reference not in (positive, negative):
                    raise ValueError('DELIVERY_RAW_METADATA_GPS')
                if not 0 <= value <= maximum:
                    raise ValueError('DELIVERY_RAW_METADATA_GPS')
                return -value if reference == negative else value

            lat = coordinate(record[_GPS[0]], record[_GPS[1]], 'N', 'S', 90)
            lon = coordinate(record[_GPS[2]], record[_GPS[3]], 'E', 'W', 180)
            altitude = None
            if _GPS[4] in record or _GPS[5] in record:
                if _GPS[4] not in record or _GPS[5] not in record:
                    raise ValueError('DELIVERY_RAW_METADATA_GPS')
                magnitude, reference = record[_GPS[4]], record[_GPS[5]]
                if (type(magnitude) not in (int, float) or not math.isfinite(magnitude)
                        or magnitude < 0 or type(reference) is not int or reference not in (0, 1)):
                    raise ValueError('DELIVERY_RAW_METADATA_GPS')
                altitude = -magnitude if reference == 1 else magnitude
            values['gps'] = GPSMetadata(latitude=lat, longitude=lon, altitude_m=altitude)
        elif _GPS[4] in record or _GPS[5] in record:
            raise ValueError('DELIVERY_RAW_METADATA_GPS')
    return MetadataDocument.model_validate(values)
