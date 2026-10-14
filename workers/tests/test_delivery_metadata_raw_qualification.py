from hashlib import sha256
import json
import sys

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.delivery_metadata_raw_qualification import qualify_raw_metadata_fixtures
from photo_worker.v2.delivery_metadata_raw_tool import RAW_METADATA_FILETYPES


def fixture_matrix(tmp_path):
    executable=tmp_path/'exiftool'
    executable.write_text('#!'+sys.executable+'\n'
        'import json,sys,pathlib\n'
        'kind=pathlib.Path(sys.argv[-1]).read_text()\n'
        'print(json.dumps([{"File:FileType":kind,"IFD0:Artist":"A",'
        '"GPS:GPSLatitude":42,"GPS:GPSLatitudeRef":"N",'
        '"GPS:GPSLongitude":23,"GPS:GPSLongitudeRef":"E"}]))\n')
    executable.chmod(0o700)
    fixtures=[]
    for mime,kind in RAW_METADATA_FILETYPES.items():
        name=kind.lower()+'.raw';(tmp_path/name).write_text(kind)
        body=(tmp_path/name).read_bytes()
        fixtures.append({'filename':name,'mime':mime,'original_sha256':sha256(body).hexdigest(),
                         'original_bytes':len(body),'expected_with_gps':{
                             'author':'A','gps':{'latitude':42,'longitude':23,'altitude_m':None}}})
    return executable,canonical_bytes({'schema_version':1,'fixtures':fixtures})


def test_raw_matrix_covers_all_formats_and_gps_consent(tmp_path):
    executable,manifest=fixture_matrix(tmp_path)
    pin=sha256(executable.read_bytes()).hexdigest()
    evidence=qualify_raw_metadata_fixtures(tmp_path,manifest,str(executable),pin)
    result=json.loads(evidence)
    assert result['manifest_sha256']==sha256(manifest).hexdigest()
    assert result['exiftool_sha256']==pin
    assert [row['mime'] for row in result['fixtures']]==sorted(RAW_METADATA_FILETYPES)
    assert all(row['metadata_sha256']!=row['gps_off_sha256'] for row in result['fixtures'])


def test_raw_matrix_refuses_missing_format_before_file_io(tmp_path):
    executable,manifest=fixture_matrix(tmp_path)
    value=json.loads(manifest)
    value['fixtures'][-1]['mime']=value['fixtures'][0]['mime']
    with pytest.raises(ValueError,match='MATRIX_CASE'):
        qualify_raw_metadata_fixtures(tmp_path,canonical_bytes(value),str(executable),
                                      sha256(executable.read_bytes()).hexdigest())
