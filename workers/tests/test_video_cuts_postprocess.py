from photo_worker.video.cuts import postprocess_boundaries
from video_vectors import load_vector

V = load_vector('cuts')


def test_all_fifteen_cases_match_vector():
    assert len(V['cases']) == 15
    for c in V['cases']:
        i = c['input']
        # the vector abbreviates `sensitivity` as `sens` (CD-03); the product API uses the DS-12 name
        params = {('sensitivity' if k == 'sens' else k): v for k, v in i.get('params', {}).items()}
        got = postprocess_boundaries(i['single'], i['many'], i['luma'], params, V['frame_ticks'])
        assert got == c['expect'], c['id']


def test_empty_input_gives_no_boundaries():
    assert postprocess_boundaries([], [], [], None, 28224000) == []
