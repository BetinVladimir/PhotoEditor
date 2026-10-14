from pathlib import Path
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.v2.render_background_binding import decode_frozen_background

ROOT=Path(__file__).parents[2]

def test_go_freeze_wire_decodes_exactly_in_python():
    raw=(ROOT/'backend/internal/v2draft/testdata/frozen-background.json').read_bytes()
    pin=decode_frozen_background(raw)
    assert canonical_bytes(pin)==raw
    assert pin.decode_params().source==pin.source
    assert pin.decode_params().assume_profile is None

@pytest.mark.parametrize('field,value',[('schema_version',True),('expected_bytes',0),
    ('expected_bytes',100.5),('suffix','.jpg'),('assume_profile','auto'),
    ('portable_asset_id',' studio'),('portable_asset_id',''),('extra',1)])
def test_background_binding_rejects_substituted_fields(field,value):
    raw=(ROOT/'backend/internal/v2draft/testdata/frozen-background.json').read_bytes()
    data=parse_json(raw);data[field]=value
    with pytest.raises(ValueError):decode_frozen_background(canonical_bytes(data))
