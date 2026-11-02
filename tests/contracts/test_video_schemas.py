"""JSON schemas for the video timeline (DS-09) and cut report (DS-12)."""
import copy
import json
from pathlib import Path

import pytest
from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[2]
SHA = 'a' * 64
CANONICAL = ROOT / 'docs' / 'video' / 'vectors' / 'canonical.json'


def _validator(name):
    schema = json.loads((ROOT / 'schemas' / name).read_text(encoding='utf-8'))
    Draft202012Validator.check_schema(schema)
    return Draft202012Validator(schema)


def _doc():
    """Section 3 example of docs/video/07 with the elided identifiers and hashes filled in."""
    return {
        'schema_version': 1, 'sequence_id': '2f6c', 'base_revision': 12,
        'timebase': {'ticks_per_second': 705600000},
        'video': {'fps': {'num': 30000, 'den': 1001}, 'width': 1920, 'height': 1080,
                  'color': {'space': 'rec709', 'range': 'limited'}, 'background': '#000000'},
        'audio': {'sample_rate': 48000, 'channels': 2},
        'tracks': [
            {'id': 'V1', 'kind': 'video', 'locked': False, 'visible': True, 'clips': [
                {'id': 'c1', 'source': {'kind': 'video', 'media_id': 'a1', 'sha256': SHA},
                 'at': 0, 'src_in': 705600000, 'src_out': 15240960000, 'speed': {'num': 1, 'den': 1},
                 'fit': 'fit',
                 'transform': {'scale': 1.0, 'position': [0, 0], 'rotate': 0.0, 'crop': [0, 0, 0, 0]},
                 'audio': {'gain_db': 0.0, 'fade_in': 0, 'fade_out': 0, 'mute': False},
                 'transition_in': {'kind': 'crossfade', 'duration': 21168000, 'align': 'center'},
                 'effects': [{'id': 'e1', 'type': 'color.basic', 'enabled': True,
                              'params': {'exposure_ev': 0.3, 'temperature': -8, 'tint': 0,
                                         'contrast': 12, 'saturation': 5}}]},
                {'id': 'c2', 'source': {'kind': 'photo', 'asset_version_id': 'b7', 'sha256': SHA},
                 'at': 14540000000, 'duration': 105840000000,
                 'motion': {'kind': 'pan_zoom', 'from': {'scale': 1.0, 'center': [0.5, 0.5]},
                            'to': {'scale': 1.15, 'center': [0.55, 0.5]}, 'easing': 'ease_in_out'},
                 'effects': []}]},
            {'id': 'A1', 'kind': 'audio', 'muted': False, 'gain_db': 0.0, 'clips': [
                {'id': 'c3', 'source': {'kind': 'audio', 'media_id': 'd4', 'sha256': SHA},
                 'at': 0, 'src_in': 0, 'src_out': 211680000000,
                 'audio': {'gain_db': -6.0, 'fade_in': 705600000, 'fade_out': 1411200000, 'mute': False},
                 'effects': [{'id': 'e2', 'type': 'audio.duck', 'enabled': True,
                              'params': {'sidechain_tracks': ['A2'], 'threshold_db': -30, 'depth_db': 12,
                                         'attack_ms': 30, 'release_ms': 400}}]}]}],
        'markers': [{'id': 'm1', 'at': 7056000000, 'kind': 'cut', 'label': ''}],
        'master': {'effects': [], 'audio': {'loudness': {'target_lufs': -14.0, 'true_peak_db': -1.0}}},
        'rights': {'music_declared': True, 'declared_at': '2026-10-22T10:00:00Z'},
    }


def test_spec_example_is_valid():
    assert list(_validator('video-timeline.schema.json').iter_errors(_doc())) == []


def _extra_key(d):
    d['tracks'][0]['clips'][0]['surprise'] = 1


def _fractional_at(d):
    d['tracks'][0]['clips'][0]['at'] = 1.5


def _schema_v2(d):
    d['schema_version'] = 2


def _unknown_effect(d):
    d['tracks'][0]['clips'][0]['effects'][0]['type'] = 'color.magic'


@pytest.mark.parametrize('edit', [_extra_key, _fractional_at, _schema_v2, _unknown_effect])
def test_invalid_documents_are_rejected(edit):
    d = copy.deepcopy(_doc())
    edit(d)
    assert list(_validator('video-timeline.schema.json').iter_errors(d))


def _report():
    return {'schema_version': 1, 'media_id': 'm1', 'source_sha256': SHA, 'fps': {'num': 25, 'den': 1},
            'frame_count': 150, 'method': 'classic', 'model': None, 'backend_used': 'cpu',
            'params': {'mode': 'all', 'sensitivity': 3, 'min_shot_frames': 5, 'include_flash': False},
            'boundaries': [{'id': 'b1', 'kind': 'cut', 'frame_in': 50, 'frame_out': 50,
                            'start_ticks': 1411200000, 'end_ticks': 1411200000, 'confidence': 0.9}]}


def test_cut_report_valid_and_closed():
    v = _validator('video-cut-report.schema.json')
    assert list(v.iter_errors(_report())) == []
    bad = _report()
    bad['boundaries'][0]['confidence'] = 1.5
    assert list(v.iter_errors(bad))
    bad = _report()
    bad['extra'] = 1
    assert list(v.iter_errors(bad))


def test_canonical_vector_is_referenced_not_copied():
    assert CANONICAL.is_file()
    assert not (ROOT / 'tests' / 'contracts' / 'video_canonical.json').exists()
