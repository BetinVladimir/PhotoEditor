import numpy as np
import pytest

from photo_worker.video.lut import Lut3D, build_lut, fold_effects_to_lut, identity_lut, parse_cube, write_cube
from video_vectors import load_vector

COLOR = load_vector('color')
CUBE = load_vector('cube_cases')


def _identity_text(size, drop_rows=0, replace_row=None):
    n = size
    rows = [f'{r / (n - 1):.6f} {g / (n - 1):.6f} {b / (n - 1):.6f}' for b in range(n) for g in range(n) for r in range(n)]
    if drop_rows:
        rows = rows[:-drop_rows]
    if replace_row:
        rows[replace_row['index']] = replace_row['text']
    return f'LUT_3D_SIZE {n}\n' + '\n'.join(rows) + '\n'


def _text(gen):
    k = gen['kind']
    if k == 'identity':
        return _identity_text(gen['size'], gen.get('drop_rows', 0), gen.get('replace_row'))
    if k == 'header_only':
        return f'LUT_3D_SIZE {gen["size"]}\n'
    if k == 'pad_comment':
        tail = '\nLUT_3D_SIZE 17\n'
        return '#' + 'x' * (gen['bytes'] - 1 - len(tail)) + tail
    return gen['text']


def test_build_lut_matches_vector():
    v = COLOR['lut']
    lut = build_lut(v['size'], lambda r, g, b: (r * 0.5, g * 0.5, b * 0.5))
    assert lut.dtype == np.float32 and lut.shape == (27, 3)
    for s in v['samples']:
        assert lut[s['index']] == pytest.approx(s['value'], abs=1e-6)


def test_write_then_parse_round_trips():
    lut = identity_lut(17)
    back = parse_cube(write_cube(lut))
    assert back.size == 17 and back.table.shape == (4913, 3)
    assert back.table == pytest.approx(lut.table, abs=1e-6)


@pytest.mark.parametrize('case', CUBE['cases'], ids=lambda c: c['id'])
def test_cube_cases(case):
    text = _text(case['gen'])
    if case['expect']['ok']:
        lut = parse_cube(text)
        assert lut.size == case['expect']['size'] and len(lut.table) == 4913
        assert lut.table[16] == pytest.approx([1, 0, 0], abs=1e-6)
    else:
        with pytest.raises(ValueError, match=case['expect']['error']):
            parse_cube(text)


def test_cube_domain_and_title_accepted():
    body = _identity_text(17).replace('LUT_3D_SIZE 17', 'TITLE "x"\nDOMAIN_MIN 0 0 0\nDOMAIN_MAX 1 1 1\nLUT_3D_SIZE 17')
    assert parse_cube(body).size == 17


def test_fold_empty_is_identity_and_cdl_matches_vector():
    assert fold_effects_to_lut([], 17).table == pytest.approx(identity_lut(17).table, abs=1e-6)
    cdl = {'id': 'e', 'type': 'color.cdl', 'enabled': True,
           'params': {'slope': [0.5, 0.5, 0.5], 'offset': [0, 0, 0], 'power': [1, 1, 1], 'saturation': 1}}
    lut = fold_effects_to_lut([cdl], 17)
    assert lut.table[16] == pytest.approx([0.5, 0.0, 0.0], abs=1e-5)
    assert isinstance(lut, Lut3D)


def test_fold_rejects_unspecified_basic_sliders():
    e = {'id': 'e', 'type': 'color.basic', 'enabled': True, 'params': {'contrast': 10}}
    with pytest.raises(ValueError, match='LUT_EFFECT_UNSPECIFIED'):
        fold_effects_to_lut([e], 17)
