import json

import pytest

from photo_worker.v2.delivery_metadata_raw_extract import extract_raw_delivery_metadata


def record(**fields):
    return json.dumps([fields], separators=(',', ':')).encode()


def test_allowlisted_raw_exif_and_explicit_gps_consent():
    raw = record(**{'IFD0:Artist': 'Photographer', 'IFD0:Make': 'Camera',
                    'ExifIFD:DateTimeOriginal': '2025:02:03 04:05:06',
                    'GPS:GPSLatitude': 42.5, 'GPS:GPSLatitudeRef': 'S',
                    'GPS:GPSLongitude': 23.25, 'GPS:GPSLongitudeRef': 'E',
                    'GPS:GPSAltitude': 100, 'GPS:GPSAltitudeRef': 1,
                    'MakerNotes:SerialNumber': 'private'})
    without = extract_raw_delivery_metadata(raw)
    assert without.author == 'Photographer' and without.camera_make == 'Camera'
    assert without.gps is None and 'private' not in str(without)
    with_gps = extract_raw_delivery_metadata(raw, include_gps=True)
    assert with_gps.gps.latitude == -42.5
    assert with_gps.gps.longitude == 23.25
    assert with_gps.gps.altitude_m == -100


def test_valid_camera_alternative_ifd_locations():
    raw=record(**{'ExifIFD:Artist':'A','ExifIFD:Make':'M',
                  'ExifIFD:Model':'N','ExifIFD:LensModel':'L',
                  'IFD0:DateTimeOriginal':'2025:02:03 04:05:06'})
    result=extract_raw_delivery_metadata(raw)
    assert (result.author,result.camera_make,result.camera_model,result.lens,
            result.datetime_original)==('A','M','N','L','2025:02:03 04:05:06')


@pytest.mark.parametrize('raw', [
    b'[]', b'[{},{}]', b'{}', b'[1]', b'[{"IFD0:Artist":"a","IFD0:Artist":"b"}]',
    b'[{"IFD0:Artist":1}]', b'[{"IFD0:Artist":"a","GPS:GPSLatitude":NaN}]',
    b'[{"IFD0:Artist":"a","ExifIFD:Artist":"b"}]',
])
def test_refuses_ambiguous_or_invalid_exif(raw):
    with pytest.raises(ValueError):
        extract_raw_delivery_metadata(raw, include_gps=True)


@pytest.mark.parametrize('gps', [
    {'GPS:GPSLatitude': 42},
    {'GPS:GPSAltitude': 12, 'GPS:GPSAltitudeRef': 0},
    {'GPS:GPSLatitude': 91, 'GPS:GPSLatitudeRef': 'N',
     'GPS:GPSLongitude': 20, 'GPS:GPSLongitudeRef': 'E'},
    {'GPS:GPSLatitude': 42, 'GPS:GPSLatitudeRef': 'N',
     'GPS:GPSLongitude': 20, 'GPS:GPSLongitudeRef': 'E',
     'GPS:GPSAltitude': -1, 'GPS:GPSAltitudeRef': 0},
])
def test_refuses_incomplete_or_invalid_gps(gps):
    with pytest.raises(ValueError):
        extract_raw_delivery_metadata(record(**gps), include_gps=True)
