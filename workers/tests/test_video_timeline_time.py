import pytest

from photo_worker.video.timeline import (
    TICKS_PER_SECOND, canonicalize, clip_duration, document_sha256, frame_ticks, frames_to_ticks, ticks_to_frame,
)
from video_vectors import load_vector

TIME = load_vector('time')


def _fps(key):
    n, d = key.split('/')
    return int(n), int(d)


def test_frame_ticks_vector_and_unsupported():
    for key, ticks in TIME['frame_ticks'].items():
        assert frame_ticks(*_fps(key)) == ticks
    with pytest.raises(ValueError, match='FPS_UNSUPPORTED'):
        frame_ticks(11, 1)  # card TK-301 names 7/1, but 705600000 % 7 == 0
    with pytest.raises(ValueError, match='FPS_UNSUPPORTED'):
        frame_ticks(0, 1)


def test_sample_ticks_and_limits():
    for rate, ticks in TIME['sample_ticks'].items():
        assert TICKS_PER_SECOND // int(rate) == ticks
    assert 3 * 3600 * TICKS_PER_SECOND == TIME['max_ticks_3h']


def test_frames_and_ticks_round_trip_vectors():
    for c in TIME['frames_to_ticks']:
        assert frames_to_ticks(*_fps(c['fps']), c['frame']) == c['ticks']
    for c in TIME['ticks_to_frame']:
        assert ticks_to_frame(*_fps(c['fps']), c['ticks'], 'floor') == c['floor']
        assert ticks_to_frame(*_fps(c['fps']), c['ticks'], 'round') == c['round']
    with pytest.raises(ValueError, match='FRAME_NEGATIVE'):
        frames_to_ticks(25, 1, -1)
    with pytest.raises(ValueError, match='TICKS_NEGATIVE'):
        ticks_to_frame(25, 1, -1, 'floor')


def test_clip_duration():
    assert clip_duration({'source': {'kind': 'photo'}, 'duration': 99}) == 99
    assert clip_duration({'source': {'kind': 'video'}, 'src_in': 100, 'src_out': 700}) == 600
    assert clip_duration({'source': {'kind': 'video'}, 'src_in': 0, 'src_out': 600, 'speed': {'num': 1, 'den': 4}}) == 2400


def test_canonical_vectors():
    for c in load_vector('canonical'):
        assert canonicalize(c['input']).decode('utf-8') == c['canonical']
        assert document_sha256(c['input']) == c['sha256']
