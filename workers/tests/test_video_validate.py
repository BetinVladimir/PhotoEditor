import copy

from photo_worker.video.validate import validate_document
from video_vectors import load_vector

V = load_vector('validate_cases')


def _walk(root, path):
    cur = root
    for p in [x for x in path.split('/') if x]:
        cur = cur[int(p)] if isinstance(cur, list) else cur[p]
    return cur


def _apply(doc, patch):
    parts = [x for x in patch['path'].split('/') if x]
    if patch['op'] == 'append':
        _walk(doc, patch['path']).append(patch['value'])
        return
    parent = _walk(doc, '/' + '/'.join(parts[:-1]))
    if isinstance(parent, list):
        parent[int(parts[-1])] = patch['value']
    else:
        parent[parts[-1]] = patch['value']


def test_all_validate_cases_match_vector():
    assert len(V['cases']) == 21
    for c in V['cases']:
        doc, ctx = copy.deepcopy(V['base_doc']), copy.deepcopy(V['base_ctx'])
        for p in c['patches']:
            _apply(doc, p)
        ctx.update(c['ctx_patch'])
        assert validate_document(doc, ctx) == c['expect'], c['id']


def test_input_unchanged_and_issue_cap():
    doc = copy.deepcopy(V['base_doc'])
    doc['tracks'][0]['clips'] = [{**doc['tracks'][0]['clips'][0], 'id': f'x{i}', 'at': 0} for i in range(150)]
    before = copy.deepcopy(doc)
    assert len(validate_document(doc, V['base_ctx'])) <= 100
    assert doc == before


def test_effect_and_clip_property_ranges():
    doc = copy.deepcopy(V['base_doc'])
    doc['tracks'][0]['clips'][0]['transform'] = {'scale': 9, 'position': [0, 0], 'rotate': 0, 'crop': [0, 0, 0, 0]}
    doc['tracks'][0]['clips'][0]['effects'] = [{'id': 'e1', 'type': 'color.cdl', 'enabled': True,
                                                'params': {'slope': [1, 1, 1], 'offset': [0, 0, 0], 'power': [1, 1, 1], 'saturation': 5}}]
    assert [i['pointer'] for i in validate_document(doc, V['base_ctx'])] == [
        '/tracks/0/clips/0/transform/scale', '/tracks/0/clips/0/effects/0/params/saturation']
